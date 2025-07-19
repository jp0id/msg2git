package telegram

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/msg2git/msg2git/internal/cache"
	"github.com/msg2git/msg2git/internal/config"
	"github.com/msg2git/msg2git/internal/consts"
	"github.com/msg2git/msg2git/internal/database"
	"github.com/msg2git/msg2git/internal/file"
	"github.com/msg2git/msg2git/internal/github"
	"github.com/msg2git/msg2git/internal/llm"
	"github.com/msg2git/msg2git/internal/logger"
	"github.com/msg2git/msg2git/internal/stripe"
	"golang.org/x/time/rate"
)

type Bot struct {
	api             *tgbotapi.BotAPI
	fileManager     *file.Manager
	githubManager   *github.Manager        // Default GitHub manager (from .env) - DEPRECATED: Use githubFactory
	githubFactory   github.ProviderFactory // New: Factory for creating GitHub providers
	llmClient       *llm.Client            // Default LLM client (from .env)
	stripeManager   *stripe.Manager        // Stripe payment manager
	pendingMessages map[string]string      // messageID -> content
	config          *config.Config         // Store config for runtime updates
	db              *database.DB           // Database for multi-user support
	cache           *cache.Cache           // Cache for storing frequently accessed data

	// Rate limiting
	globalLimiter  *rate.Limiter           // Global rate limiter (30 msg/sec)
	userLimiters   map[int64]*rate.Limiter // Per-user rate limiters (1 msg/user/sec)
	userLimitersMu sync.RWMutex            // Protects userLimiters map
	cleanupStarted bool                    // Track if cleanup goroutine is started

	// Callback deduplication
	processedCallbacks map[string]time.Time // callback_id -> timestamp
	callbacksMu        sync.RWMutex         // Protects processedCallbacks map

	// Worker pool for concurrent processing
	workerPool *WorkerPool // Handles concurrent message and callback processing
}

func NewBot(cfg *config.Config) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.TelegramBotToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create Telegram bot: %w", err)
	}

	// Initialize database (optional)
	var db *database.DB
	if cfg.HasDatabaseConfig() {
		db, err = database.NewDB(cfg.PostgreDSN, cfg.TokenPassword)
		if err != nil {
			logger.Warn("Failed to initialize database", map[string]interface{}{
				"error": err.Error(),
			})
			logger.InfoMsg("Continuing without database support...")
		} else {
			logger.InfoMsg("Database initialized successfully")
		}
	} else {
		logger.InfoMsg("No database configured, using single-user mode")
	}

	// No default GitHub manager or LLM client - everything is database-controlled

	// Initialize Stripe manager (optional)
	var stripeManager *stripe.Manager
	stripeManager = stripe.NewManager(cfg.BaseURL)
	if err := stripeManager.Initialize(); err != nil {
		logger.Warn("Failed to initialize Stripe manager", map[string]interface{}{
			"error": err.Error(),
		})
		logger.InfoMsg("Continuing without Stripe payment support...")
		stripeManager = nil
	} else {
		logger.InfoMsg("Stripe payment manager initialized successfully")
	}

	return &Bot{
		api:             api,
		fileManager:     file.NewManager(),
		githubManager:   nil,
		githubFactory:   github.NewProviderFactory(), // Initialize GitHub provider factory
		llmClient:       nil,
		stripeManager:   stripeManager,
		pendingMessages: make(map[string]string),
		config:          cfg,
		db:              db,
		cache:           cache.NewWithConfig(1000, 30*time.Minute, 5*time.Minute), // Large cache with 30-minute expiry

		// Initialize rate limiters
		globalLimiter:  rate.NewLimiter(rate.Limit(5000), 5000), // 5000 messages per second with burst of 5000
		userLimiters:   make(map[int64]*rate.Limiter),
		userLimitersMu: sync.RWMutex{},
		cleanupStarted: false,

		// Initialize callback deduplication
		processedCallbacks: make(map[string]time.Time),
		callbacksMu:        sync.RWMutex{},

		// Worker pool will be initialized in Start() method
		workerPool: nil,
	}, nil
}

func (b *Bot) Start() error {
	logger.Info("Bot authorized and starting", map[string]interface{}{
		"username":          b.api.Self.UserName,
		"global_rate_limit": "5000 msg/sec",
		"user_rate_limit":   "30 msg/user/sec",
	})

	// Initialize and start worker pool
	b.workerPool = NewWorkerPool(b, DefaultWorkerPoolConfig())
	if err := b.workerPool.Start(); err != nil {
		return fmt.Errorf("failed to start worker pool: %w", err)
	}

	// Start webhook server for Stripe payments
	b.StartWebhookServer()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	u.AllowedUpdates = []string{"message", "edited_message", "callback_query"}

	updates := b.api.GetUpdatesChan(u)

	for update := range updates {
		logger.Debug("Received update", map[string]interface{}{
			"update_id":    update.UpdateID,
			"has_message":  update.Message != nil,
			"has_callback": update.CallbackQuery != nil,
		})

		if update.CallbackQuery != nil {
			// Submit callback to worker pool for concurrent processing
			if err := b.workerPool.SubmitCallback(update.CallbackQuery); err != nil {
				logger.Error("Failed to submit callback to worker pool", map[string]interface{}{
					"error":       err.Error(),
					"chat_id":     update.CallbackQuery.Message.Chat.ID,
					"callback_id": update.CallbackQuery.ID,
				})
				// Fallback to direct processing if worker pool is full
				// if err := b.handleCallbackQuery(update.CallbackQuery); err != nil {
				// 	logger.Error("Error handling callback query", map[string]interface{}{
				// 		"error":   err.Error(),
				// 		"chat_id": update.CallbackQuery.Message.Chat.ID,
				// 	})
				// 	b.sendErrorResponse(update.CallbackQuery.Message.Chat.ID, err)
				// }
			}
			continue
		}

		if update.Message == nil {
			logger.Debug("Update has no message, skipping", nil)
			continue
		}

		logger.Debug("Received message from user", map[string]interface{}{
			"username": update.Message.From.UserName,
			"chat_id":  update.Message.Chat.ID,
		})

		// Submit message to worker pool for concurrent processing
		if err := b.workerPool.SubmitMessage(update.Message); err != nil {
			logger.Error("Failed to submit message to worker pool", map[string]interface{}{
				"error":    err.Error(),
				"username": update.Message.From.UserName,
				"chat_id":  update.Message.Chat.ID,
			})
			// Fallback to direct processing if worker pool is full
			// if err := b.handleMessage(update.Message); err != nil {
			// 	logger.Error("Error handling message", map[string]interface{}{
			// 		"error":    err.Error(),
			// 		"username": update.Message.From.UserName,
			// 		"chat_id":  update.Message.Chat.ID,
			// 	})
			// 	b.sendErrorResponse(update.Message.Chat.ID, err)
			// }
		}
	}

	return nil
}

// Stop gracefully shuts down the bot and its worker pool
func (b *Bot) Stop() error {
	logger.InfoMsg("Stopping bot...")

	if b.workerPool != nil {
		if err := b.workerPool.Stop(); err != nil {
			logger.Error("Error stopping worker pool", map[string]interface{}{
				"error": err.Error(),
			})
			return err
		}
	}

	logger.InfoMsg("Bot stopped successfully")
	return nil
}

// GetWorkerPoolStats returns current worker pool statistics
func (b *Bot) GetWorkerPoolStats() map[string]interface{} {
	if b.workerPool == nil {
		return map[string]interface{}{
			"worker_pool": "not initialized",
		}
	}

	return b.workerPool.GetStats()
}

func (b *Bot) handleMessage(message *tgbotapi.Message) error {
	// Handle reply commands first (including photo replies to issue comments)
	if message.ReplyToMessage != nil {
		return b.handleReplyMessage(message)
	}

	// Handle photo messages (only if not a reply)
	if len(message.Photo) > 0 {
		return b.handlePhotoMessage(message)
	}

	if message.Text == "" {
		return fmt.Errorf("empty message received")
	}

	// Handle commands
	if strings.HasPrefix(message.Text, "/") {
		return b.handleCommand(message)
	}

	// Regular message - show file selection buttons
	return b.showFileSelectionButtons(message)
}

func (b *Bot) handlePhotoMessage(message *tgbotapi.Message) error {
	logger.Debug("Processing photo message from user", map[string]interface{}{
		"username":    message.From.UserName,
		"chat_id":     message.Chat.ID,
		"photo_count": len(message.Photo),
	})

	// Ensure user exists in database if database is configured
	_, err := b.ensureUser(message)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	// Get user-specific GitHub provider
	userGitHubProvider, err := b.getUserGitHubProvider(message.Chat.ID)
	if err != nil {
		errorMsg := "❌ " + err.Error()
		if b.db != nil {
			errorMsg += ". " + consts.GitHubSetupPrompt
		}
		b.sendResponse(message.Chat.ID, errorMsg)
		return nil
	}

	// Send status message with initial progress
	statusMsg := "📷 Processing photo..."
	statusMessageID := b.sendResponseAndGetMessageID(message.Chat.ID, statusMsg)

	// Ensure repository exists with premium-aware setup (includes capacity checking)
	premiumLevel := b.getPremiumLevel(message.Chat.ID)

	// Show appropriate progress message based on whether repo needs cloning
	if userGitHubProvider.NeedsClone() {
		b.updateProgressMessage(message.Chat.ID, statusMessageID, 10, "📊 Checking remote repository size...")
	} else {
		b.updateProgressMessage(message.Chat.ID, statusMessageID, 10, "📊 Checking repository capacity...")
	}

	// Ensure repository with premium-aware cloning (includes size verification)
	if err := userGitHubProvider.EnsureRepositoryWithPremium(premiumLevel); err != nil {
		logger.Error("Failed to ensure repository for photo upload", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": message.Chat.ID,
		})

		// Get user-friendly formatted error message
		errorMsg := b.formatRepositorySetupError(err, "upload photos")

		// Send formatted error message
		editMsg := tgbotapi.NewEditMessageText(message.Chat.ID, statusMessageID, errorMsg)
		editMsg.ParseMode = "html"
		if _, sendErr := b.rateLimitedSend(message.Chat.ID, editMsg); sendErr != nil {
			logger.Error("Failed to edit message with formatted error", map[string]interface{}{
				"error": sendErr.Error(),
			})
			// Fallback to simple message
			b.editMessage(message.Chat.ID, statusMessageID, fmt.Sprintf("❌ Repository setup failed: %v", err))
		}
		return nil // Return nil instead of propagating the error to avoid duplicate messages
	}

	// Check repository capacity and image limits before uploading photo
	// b.updateProgressMessage(message.Chat.ID, statusMessageID, 15, "📊 Checking limits...")

	logger.Info("Checking repository capacity and image limits before photo upload", map[string]interface{}{
		"premium_level": premiumLevel,
		"chat_id":       message.Chat.ID,
	})

	// Check repository capacity
	isNearCapacity, percentage, err := userGitHubProvider.IsRepositoryNearCapacityWithPremium(premiumLevel)
	if err != nil {
		logger.Warn("Failed to check repository capacity before photo upload", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": message.Chat.ID,
		})
		// Continue anyway if capacity check fails - don't block upload
	} else if isNearCapacity {
		logger.Info("Photo upload blocked due to repository capacity", map[string]interface{}{
			"percentage":    percentage,
			"premium_level": premiumLevel,
			"chat_id":       message.Chat.ID,
		})

		errorMsg := fmt.Sprintf(RepoPhotoUploadLimitTemplate, percentage)

		editMsg := tgbotapi.NewEditMessageText(message.Chat.ID, statusMessageID, errorMsg)
		editMsg.ParseMode = "html"
		if _, sendErr := b.rateLimitedSend(message.Chat.ID, editMsg); sendErr != nil {
			logger.Error("Failed to edit message with capacity error", map[string]interface{}{
				"error": sendErr.Error(),
			})
			// Fallback to simple message
			b.editMessage(message.Chat.ID, statusMessageID, RepoCapacityLimitSimple)
		}
		return nil
	}

	// Check image upload limits
	if b.db != nil {
		canUpload, currentCount, imageLimit, err := b.db.CheckUsageImageLimit(message.Chat.ID, premiumLevel)
		if err != nil {
			logger.Warn("Failed to check image limit before photo upload", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": message.Chat.ID,
			})
			// Continue anyway if image limit check fails - don't block upload
		} else if !canUpload {
			logger.Info("Photo upload blocked due to image limit", map[string]interface{}{
				"current_count": currentCount,
				"image_limit":   imageLimit,
				"premium_level": premiumLevel,
				"chat_id":       message.Chat.ID,
			})

			tierNames := map[int]string{0: "free", 1: "☕ Coffee", 2: "🍰 Cake", 3: "🎁 Sponsor"}
			currentTier := tierNames[premiumLevel]

			var upgradeHint string
			if premiumLevel < 3 {
				nextLimit := database.GetImageLimit(premiumLevel + 1)
				nextTier := tierNames[premiumLevel+1]
				upgradeHint = fmt.Sprintf("\n\n💡 <b>Upgrade to %s tier</b> to get <b>%d images</b> (%dx more)!", nextTier, nextLimit, database.GetImageMultiplier(premiumLevel+1))
			} else {
				upgradeHint = "\n\n🎉 You're already on the highest tier with maximum image limits!"
			}

			errorMsg := fmt.Sprintf(ImageLimitReachedDetailedTemplate, currentCount, imageLimit, currentTier, upgradeHint)

			editMsg := tgbotapi.NewEditMessageText(message.Chat.ID, statusMessageID, errorMsg)
			editMsg.ParseMode = "html"
			if _, sendErr := b.rateLimitedSend(message.Chat.ID, editMsg); sendErr != nil {
				logger.Error("Failed to edit message with image limit error", map[string]interface{}{
					"error": sendErr.Error(),
				})
				// Fallback to simple message
				b.editMessage(message.Chat.ID, statusMessageID, fmt.Sprintf(ImageLimitReachedTemplate, currentCount, imageLimit))
			}
			return nil
		}

		logger.Info("Image limit check passed", map[string]interface{}{
			"current_count": currentCount,
			"image_limit":   imageLimit,
			"premium_level": premiumLevel,
			"chat_id":       message.Chat.ID,
		})
	}

	logger.Info("All limits check passed, proceeding with photo upload", map[string]interface{}{
		"repo_percentage": percentage,
		"premium_level":   premiumLevel,
		"chat_id":         message.Chat.ID,
	})

	// Get the largest photo (last in array)
	photo := message.Photo[len(message.Photo)-1]

	// Download the photo with progress
	b.updateProgressMessage(message.Chat.ID, statusMessageID, 40, "⬇️ Downloading photo...")
	photoData, filename, err := b.downloadPhoto(photo.FileID)
	if err != nil {
		logger.Error("Failed to download photo", map[string]interface{}{
			"error":   err.Error(),
			"file_id": photo.FileID,
			"chat_id": message.Chat.ID,
		})
		b.editMessage(message.Chat.ID, statusMessageID, fmt.Sprintf("❌ Failed to download photo: %v", err))
		return fmt.Errorf("failed to download photo: %w", err)
	}

	// Multimodal analysis will be performed later after user selects location

	// Generate a unique filename with timestamp, microseconds, and random component
	photoFilename := b.generateUniquePhotoFilename(filename)

	// Upload photo to GitHub CDN with progress
	b.updateProgressMessage(message.Chat.ID, statusMessageID, 70, "📝 Uploading photo to GitHub CDN...")

	// Upload to GitHub CDN and get the URL
	photoURL, err := userGitHubProvider.UploadImageToCDN(photoFilename, photoData)
	if err != nil {
		logger.Error("Failed to upload photo to GitHub CDN", map[string]interface{}{
			"error":    err.Error(),
			"filename": photoFilename,
			"size":     len(photoData),
			"chat_id":  message.Chat.ID,
		})
		// Check if it's an authorization error and provide helpful message
		if strings.Contains(err.Error(), "GitHub authorization failed") {
			b.editMessage(message.Chat.ID, statusMessageID, "❌ "+err.Error())
		} else {
			b.editMessage(message.Chat.ID, statusMessageID, fmt.Sprintf("❌ Failed to upload photo: %v", err))
		}
		return fmt.Errorf("failed to upload photo to GitHub CDN: %w", err)
	}

	// Increment image count after successful photo upload
	if b.db != nil {
		if err := b.db.IncrementImageCount(message.Chat.ID); err != nil {
			logger.Error("Failed to increment image count", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": message.Chat.ID,
			})
		}
		// Also increment usage count for current period tracking
		if err := b.db.IncrementUsageImageCount(message.Chat.ID); err != nil {
			logger.Error("Failed to increment usage image count", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": message.Chat.ID,
			})
		}
	}

	logger.Info("Photo uploaded to CDN successfully, showing file selection buttons", map[string]interface{}{
		"filename":    photoFilename,
		"url":         photoURL,
		"chat_id":     message.Chat.ID,
		"has_caption": message.Caption != "",
	})

	if err := b.showFileSelectionButtonsForPhoto(message, photoURL, statusMessageID, photoData); err != nil {
		logger.Error("Failed to show file selection buttons for photo", map[string]interface{}{
			"error":   err.Error(),
			"url":     photoURL,
			"chat_id": message.Chat.ID,
		})
		b.editMessage(message.Chat.ID, statusMessageID, "❌ Failed to show photo options")
		return fmt.Errorf("failed to show file selection buttons: %w", err)
	}

	logger.Info("File selection buttons sent successfully", map[string]interface{}{
		"chat_id": message.Chat.ID,
	})

	// Don't show success message yet, wait for user selection
	return nil
}

func (b *Bot) downloadPhoto(fileID string) ([]byte, string, error) {
	// Get file info from Telegram
	fileConfig := tgbotapi.FileConfig{FileID: fileID}
	file, err := b.api.GetFile(fileConfig)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get file info: %w", err)
	}

	// Download the file
	fileURL := file.Link(b.api.Token)
	logger.Debug("Downloading photo from Telegram", map[string]interface{}{
		"file_id":   fileID,
		"file_url":  fileURL,
		"file_size": file.FileSize,
	})

	resp, err := http.Get(fileURL)
	if err != nil {
		return nil, "", fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("failed to download file: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read file data: %w", err)
	}

	// Extract filename from the file path or use a default
	filename := filepath.Base(file.FilePath)
	if filename == "." || filename == "" {
		filename = "photo.jpg"
	}

	logger.Debug("Photo downloaded successfully", map[string]interface{}{
		"filename": filename,
		"size":     len(data),
	})

	return data, filename, nil
}

func (b *Bot) handleReplyMessage(message *tgbotapi.Message) error {

	if strings.HasPrefix(message.Text, "/edit ") {
		return b.handleEditCommand(message)
	}

	if strings.TrimSpace(message.Text) == "/done" {
		return b.handleDoneCommand(message)
	}

	// Check for custom file addition pending state first
	stateKey := fmt.Sprintf("add_custom_%d", message.Chat.ID)
	if stateData, exists := b.pendingMessages[stateKey]; exists {
		// Remove the pending state and handle as custom file addition
		delete(b.pendingMessages, stateKey)
		return b.handleCustomFilePathReply(message, stateData)
	}

	// Check for issue comment pending state
	commentStateKey := fmt.Sprintf("comment_%d_%d", message.Chat.ID, message.ReplyToMessage.MessageID)
	if commentData, exists := b.pendingMessages[commentStateKey]; exists {
		// Remove the pending state and handle as issue comment
		delete(b.pendingMessages, commentStateKey)
		return b.handleIssueCommentReply(message, commentData)
	}

	// Check for LLM token setup pending state
	llmTokenStateKey := fmt.Sprintf("llm_token_%d_%d", message.Chat.ID, message.ReplyToMessage.MessageID)
	if llmTokenData, exists := b.pendingMessages[llmTokenStateKey]; exists {
		// Remove the pending state and handle as LLM token setup
		delete(b.pendingMessages, llmTokenStateKey)
		return b.handleLLMTokenSetupReply(message, llmTokenData)
	}

	// Check if this is a reply to one of our command prompts
	if message.ReplyToMessage != nil && message.ReplyToMessage.Text != "" {
		replyText := message.ReplyToMessage.Text

		if strings.Contains(replyText, "Set GitHub Repository") {
			return b.handleSetRepoReply(message)
		}

		if strings.Contains(replyText, "Set GitHub Personal Access Token") {
			return b.handleRepoTokenReply(message)
		}

		if strings.Contains(replyText, "Set Personal LLM Token") {
			return b.handleLLMTokenSetupReply(message, "llm_token_setup")
		}

		if strings.Contains(replyText, "Custom Commit Author") {
			return b.handleCommitterReply(message)
		}
	}

	return fmt.Errorf("unknown reply command")
}

func (b *Bot) sendResponse(chatID int64, text string) {
	logger.Debug("Sending response to chat", map[string]interface{}{
		"chat_id": chatID,
	})
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "html"
	if _, err := b.rateLimitedSend(chatID, msg); err != nil {
		logger.Error("Failed to send message", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
	} else {
		logger.Debug("Message sent successfully", map[string]interface{}{
			"chat_id": chatID,
		})
	}
}

func (b *Bot) sendResponseAndGetMessageID(chatID int64, text string) int {
	logger.Debug("Sending response to chat and getting message ID", map[string]interface{}{
		"chat_id": chatID,
	})
	msg := tgbotapi.NewMessage(chatID, text)
	response, err := b.rateLimitedSend(chatID, msg)
	if err != nil {
		logger.Error("Failed to send message", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return 0
	}
	logger.Debug("Message sent successfully", map[string]interface{}{
		"chat_id":    chatID,
		"message_id": response.MessageID,
	})
	return response.MessageID
}

func (b *Bot) editMessage(chatID int64, messageID int, text string) {
	logger.Debug("Editing message", map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
	})
	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	if _, err := b.rateLimitedSend(chatID, edit); err != nil {
		logger.Error("Failed to edit message", map[string]interface{}{
			"error":      err.Error(),
			"chat_id":    chatID,
			"message_id": messageID,
		})
	} else {
		logger.Debug("Message edited successfully", map[string]interface{}{
			"chat_id":    chatID,
			"message_id": messageID,
		})
	}
}

func (b *Bot) deleteMessage(chatID int64, messageID int) {
	logger.Debug("Deleting message", map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
	})
	delete := tgbotapi.NewDeleteMessage(chatID, messageID)
	if _, err := b.rateLimitedSend(chatID, delete); err != nil {
		logger.Error("Failed to delete message", map[string]interface{}{
			"error":      err.Error(),
			"chat_id":    chatID,
			"message_id": messageID,
		})
	} else {
		logger.Debug("Message deleted successfully", map[string]interface{}{
			"chat_id":    chatID,
			"message_id": messageID,
		})
	}
}

func (b *Bot) sendErrorResponse(chatID int64, err error) {
	errorMsg := fmt.Sprintf("❌ Error: %v", err)
	b.sendResponse(chatID, errorMsg)
}

// shouldPerformMultimodalAnalysis checks if multimodal analysis should be performed
func (b *Bot) shouldPerformMultimodalAnalysis(chatID int64, user *database.User) bool {
	if b.db == nil || user == nil {
		return false
	}

	// Check if user has LLM enabled, multimodal enabled, and is using Gemini
	if !user.LLMSwitch || !user.LLMMultimodalSwitch {
		return false
	}

	// Get user's LLM client to check if it supports multimodal
	userLLMClient, _ := b.getUserLLMClientWithUsageTracking(chatID, "")
	if userLLMClient == nil || !userLLMClient.SupportsMultimodal() {
		return false
	}

	return true
}

// getUserGitHubManager gets or creates a GitHub manager for a specific user
// getUserGitHubProvider creates a GitHub provider for the user using the factory pattern
func (b *Bot) getUserGitHubProvider(chatID int64) (github.GitHubProvider, error) {
	if b.db == nil {
		return nil, fmt.Errorf("database is required for GitHub configuration")
	}

	// Get user from database
	user, err := b.db.GetUserByChatID(chatID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	if user == nil || !user.HasGitHubConfig() {
		return nil, fmt.Errorf("user not configured or missing GitHub settings")
	}

	// Get premium level for the user
	premiumLevel := b.getPremiumLevel(chatID)

	// Create user-specific config adapter
	userConfig := github.NewConfigAdapter(&config.Config{
		GitHubToken:    user.GitHubToken,
		GitHubRepo:     user.GitHubRepo,
		GitHubUsername: b.config.GitHubUsername, // Use default from .env
		CommitAuthor:   b.config.CommitAuthor,   // Use default from .env
	})

	// Create provider config
	providerConfig := &github.ProviderConfig{
		Config:       userConfig,
		PremiumLevel: premiumLevel,
		UserID:       fmt.Sprintf("user_%d", chatID),
	}

	// Determine provider type (for now, always use clone, but this can be made configurable)
	providerType := b.getProviderType(chatID, premiumLevel)

	// Check if we have a cached provider for this user
	cacheKey := fmt.Sprintf("github_provider_%d", chatID)
	if cachedProvider, exists := b.cache.Get(cacheKey); exists {
		if provider, ok := cachedProvider.(github.GitHubProvider); ok {
			logger.Debug("Using cached GitHub provider", map[string]interface{}{
				"chat_id":       chatID,
				"provider_type": provider.GetProviderType(),
			})
			return provider, nil
		}
	}

	// Create provider using factory
	provider, err := b.githubFactory.CreateProvider(providerType, providerConfig)
	if err != nil {
		return nil, err
	}

	// Cache the provider for 30 minutes
	b.cache.SetWithExpiry(cacheKey, provider, 30*time.Minute)

	logger.Debug("Created and cached new GitHub provider", map[string]interface{}{
		"chat_id":       chatID,
		"provider_type": provider.GetProviderType(),
	})

	return provider, nil
}

// getRepositorySizeWithCache gets repository size info with caching
func (b *Bot) getRepositorySizeWithCache(chatID int64, userGitHubProvider github.GitHubProvider, premiumLevel int) (sizeMB, percentage float64, fromCache bool, cacheExpiry time.Time, err error) {
	// Generate cache key based on user and premium level
	cacheKey := fmt.Sprintf("repo_size_%d_%d", chatID, premiumLevel)

	// Check cache first
	if cachedData, exists := b.cache.Get(cacheKey); exists {
		if sizeData, ok := cachedData.(map[string]interface{}); ok {
			if sizeMBVal, sizeMBOk := sizeData["sizeMB"].(float64); sizeMBOk {
				if percentageVal, percentageOk := sizeData["percentage"].(float64); percentageOk {
					if expiryVal, expiryOk := sizeData["expiry"].(time.Time); expiryOk {
						logger.Debug("Using cached repository size info", map[string]interface{}{
							"chat_id":      chatID,
							"cache_key":    cacheKey,
							"cache_expiry": expiryVal,
						})
						return sizeMBVal, percentageVal, true, expiryVal, nil
					}
				}
			}
		}
	}

	// Cache miss, get from provider
	if premiumLevel > 0 {
		sizeMB, percentage, err = userGitHubProvider.GetRepositorySizeInfoWithPremium(premiumLevel)
	} else {
		sizeMB, percentage, err = userGitHubProvider.GetRepositorySizeInfo()
	}

	if err != nil {
		return 0, 0, false, time.Time{}, err
	}

	// Cache the result for 5 minutes
	expiry := time.Now().Add(5 * time.Minute)
	cacheData := map[string]interface{}{
		"sizeMB":     sizeMB,
		"percentage": percentage,
		"expiry":     expiry,
	}
	b.cache.SetWithExpiry(cacheKey, cacheData, 5*time.Minute)

	logger.Debug("Cached repository size info", map[string]interface{}{
		"chat_id":      chatID,
		"cache_key":    cacheKey,
		"size_mb":      sizeMB,
		"percentage":   percentage,
		"cache_expiry": expiry,
	})

	return sizeMB, percentage, false, time.Time{}, nil
}

// getUserGitHubManager provides backward compatibility for existing code
// DEPRECATED: Use getUserGitHubProvider instead
func (b *Bot) getUserGitHubManager(chatID int64) (*github.Manager, error) {
	provider, err := b.getUserGitHubProvider(chatID)
	if err != nil {
		return nil, err
	}

	// Try to extract the underlying manager for backward compatibility
	if adapter, ok := provider.(*github.CloneBasedAdapter); ok {
		return adapter.GetUnderlyingManager(), nil
	}

	return nil, fmt.Errorf("provider type does not support Manager extraction")
}

// getProviderType determines which GitHub provider to use for a user
func (b *Bot) getProviderType(chatID int64, premiumLevel int) github.ProviderType {
	// For now, always return clone provider for stability
	// In the future, this can be made configurable via:
	// - Database user preferences
	// - A/B testing logic
	// - Feature flags
	// - Performance-based selection

	// Example future logic for A/B testing:
	// if b.isAPITestUser(chatID) {
	//     return github.ProviderTypeAPI
	// }

	// Example logic for performance-based selection:
	// userProfile := b.getUserProfile(chatID)
	// recommendedType := github.GetRecommendedProvider(
	//     1, // userCount
	//     userProfile.AvgFilesPerCommit,
	//     userProfile.AvgCommitsPerDay,
	// )
	// return recommendedType

	return github.ProviderTypeAPI
}

// isAPITestUser determines if a user should be part of API provider testing
// This can be used for gradual rollout of the new API provider
func (b *Bot) isAPITestUser(chatID int64) bool {
	// Example implementation for A/B testing
	// Could use user ID, percentage rollout, whitelist, etc.

	// For example, enable for users ending in certain digits
	// return chatID%100 < 10 // 10% of users

	// Or use a whitelist of test users
	// testUsers := []int64{123456789, 987654321}
	// for _, testUser := range testUsers {
	//     if chatID == testUser {
	//         return true
	//     }
	// }

	return false // Disabled for now
}

// parseLLMToken parses the LLM token from either "provider:token:model" format or just "token"
func (b *Bot) parseLLMToken(llmToken string) (provider, token, model string) {
	if strings.Contains(llmToken, ":") {
		parts := strings.Split(llmToken, ":")
		if len(parts) >= 2 {
			provider = parts[0]
			token = parts[1]
			if len(parts) >= 3 {
				model = parts[2]
			}
		}
	} else {
		// Backward compatibility: just token provided
		token = llmToken
		provider = "deepseek"
		model = "deepseek-chat"
	}

	// Set default models if not specified
	if model == "" {
		switch strings.ToLower(provider) {
		case "gemini":
			model = "gemini-2.5-flash"
		case "deepseek":
			model = "deepseek-chat"
		default:
			model = "deepseek-chat"
		}
	}

	return provider, token, model
}

// getUserLLMClient gets or creates an LLM client for a specific user
func (b *Bot) getUserLLMClient(chatID int64) *llm.Client {
	if b.db == nil {
		return nil // No database, no LLM client
	}

	// Get user from database
	user, err := b.db.GetUserByChatID(chatID)
	if err != nil {
		logger.Error("Failed to get user for LLM client", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return nil
	}

	if user == nil {
		return nil // No user found
	}

	// Check if user has LLM processing enabled
	if !user.LLMSwitch {
		logger.Debug("User has disabled LLM processing", map[string]interface{}{
			"chat_id":    chatID,
			"llm_switch": user.LLMSwitch,
		})
		return nil // User has disabled LLM processing
	}

	// If user has their own LLM config, use it
	if user.HasLLMConfig() {
		// Parse user's LLM token format: provider:token:model or just token
		provider, token, model := b.parseLLMToken(user.LLMToken)

		// Set endpoint based on provider
		var endpoint string
		switch strings.ToLower(provider) {
		case "gemini":
			// For Gemini, we don't need a base endpoint since the LLM client handles it
			endpoint = "https://generativelanguage.googleapis.com/v1beta"
		case "deepseek":
			endpoint = "https://api.deepseek.com/v1"
		default:
			// Fallback to deepseek for backward compatibility
			endpoint = "https://api.deepseek.com/v1"
			provider = "deepseek"
		}

		// Create user-specific config with parsed values
		userConfig := &config.Config{
			LLMProvider: provider,
			LLMEndpoint: endpoint,
			LLMToken:    token,
			LLMModel:    model,
		}

		return llm.NewClient(userConfig)
	}

	// User doesn't have their own LLM config, check if they can use default LLM
	// We'll use a more accurate estimation in the actual processing
	estimatedTokens := int64(100) // Default estimation for processing

	canUseDefault, err := b.db.CanUseDefaultLLM(chatID, estimatedTokens)
	if err != nil {
		logger.Error("Failed to check if user can use default LLM", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return nil
	}

	if !canUseDefault {
		// User has exceeded their token limit for default LLM
		return nil
	}

	// User can use default LLM, check if bot has system-wide LLM config
	if !b.config.HasLLMConfig() {
		logger.Debug("No system-wide LLM config available", map[string]interface{}{
			"chat_id":      chatID,
			"llm_provider": b.config.LLMProvider,
			"llm_endpoint": b.config.LLMEndpoint,
			"llm_token":    b.config.LLMToken != "",
			"llm_model":    b.config.LLMModel,
		})
		return nil // No system-wide LLM config available
	}

	// Return client with bot's default LLM config
	logger.Info("Using default LLM config for user", map[string]interface{}{
		"chat_id":      chatID,
		"llm_provider": b.config.LLMProvider,
		"llm_model":    b.config.LLMModel,
	})
	return llm.NewClient(b.config)
}

// getUserLLMClientWithMessage gets LLM client with accurate token estimation for the message
func (b *Bot) getUserLLMClientWithMessage(chatID int64, message string) *llm.Client {
	if b.db == nil {
		return nil // No database, no LLM client
	}

	// Get user from database
	user, err := b.db.GetUserByChatID(chatID)
	if err != nil {
		logger.Error("Failed to get user for LLM client", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return nil
	}

	if user == nil {
		return nil // No user found
	}

	// Check if user has LLM processing enabled
	if !user.LLMSwitch {
		logger.Debug("User has disabled LLM processing", map[string]interface{}{
			"chat_id":    chatID,
			"llm_switch": user.LLMSwitch,
		})
		return nil // User has disabled LLM processing
	}

	// If user has their own LLM config, use it
	if user.HasLLMConfig() {
		// Parse user's LLM token format: provider:token:model or just token
		provider, token, model := b.parseLLMToken(user.LLMToken)

		// Set endpoint based on provider
		var endpoint string
		switch strings.ToLower(provider) {
		case "gemini":
			// For Gemini, we don't need a base endpoint since the LLM client handles it
			endpoint = "https://generativelanguage.googleapis.com/v1beta"
		case "deepseek":
			endpoint = "https://api.deepseek.com/v1"
		default:
			// Fallback to deepseek for backward compatibility
			endpoint = "https://api.deepseek.com/v1"
			provider = "deepseek"
		}

		// Create user-specific config with parsed values
		userConfig := &config.Config{
			LLMProvider: provider,
			LLMEndpoint: endpoint,
			LLMToken:    token,
			LLMModel:    model,
		}

		return llm.NewClient(userConfig)
	}

	// User doesn't have their own LLM config, check if they can use default LLM
	// Estimate token usage based on actual message content
	estimatedTokens := b.estimateTokenUsage(message)

	canUseDefault, err := b.db.CanUseDefaultLLM(chatID, estimatedTokens)
	if err != nil {
		logger.Error("Failed to check if user can use default LLM", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return nil
	}

	if !canUseDefault {
		// User has exceeded their token limit for default LLM
		logger.Info("User cannot use default LLM - token limit exceeded", map[string]interface{}{
			"chat_id":          chatID,
			"estimated_tokens": estimatedTokens,
		})
		return nil
	}

	// User can use default LLM, check if bot has system-wide LLM config
	if !b.config.HasLLMConfig() {
		logger.Debug("No system-wide LLM config available", map[string]interface{}{
			"chat_id":      chatID,
			"llm_provider": b.config.LLMProvider,
			"llm_endpoint": b.config.LLMEndpoint,
			"llm_token":    b.config.LLMToken != "",
			"llm_model":    b.config.LLMModel,
		})
		return nil // No system-wide LLM config available
	}

	// Return client with bot's default LLM config
	logger.Info("Using default LLM config for user", map[string]interface{}{
		"chat_id":      chatID,
		"llm_provider": b.config.LLMProvider,
		"llm_model":    b.config.LLMModel,
	})
	return llm.NewClient(b.config)
}

// estimateTokenUsage estimates the number of tokens that will be used for processing a message
func (b *Bot) estimateTokenUsage(message string) int64 {
	// Rough estimation: 4 characters per token (GPT-style tokenization)
	// Add extra tokens for prompt formatting and system messages
	messageTokens := int64(len(message) / 4)
	systemPromptTokens := int64(50) // Estimated system prompt tokens
	responseTokens := int64(20)     // Estimated response tokens

	return messageTokens + systemPromptTokens + responseTokens
}

// getUserLLMClientWithUsageTracking gets LLM client and returns whether it's using default LLM for proper token tracking
func (b *Bot) getUserLLMClientWithUsageTracking(chatID int64, message string) (*llm.Client, bool) {
	if b.db == nil {
		return nil, false // No database, no LLM client
	}

	// Get user from database
	user, err := b.db.GetUserByChatID(chatID)
	if err != nil {
		logger.Error("Failed to get user for LLM client", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return nil, false
	}

	if user == nil {
		return nil, false // No user found
	}

	// Check if user has LLM processing enabled
	if !user.LLMSwitch {
		logger.Debug("User has disabled LLM processing", map[string]interface{}{
			"chat_id":    chatID,
			"llm_switch": user.LLMSwitch,
		})
		return nil, false // User has disabled LLM processing
	}

	// If user has their own LLM config, use it (personal LLM)
	if user.HasLLMConfig() {
		// Parse user's LLM token format: provider:token:model or just token
		provider, token, model := b.parseLLMToken(user.LLMToken)

		// Set endpoint based on provider
		var endpoint string
		switch strings.ToLower(provider) {
		case "gemini":
			// For Gemini, we don't need a base endpoint since the LLM client handles it
			endpoint = "https://generativelanguage.googleapis.com/v1beta"
		case "deepseek":
			endpoint = "https://api.deepseek.com/v1"
		default:
			// Fallback to deepseek for backward compatibility
			endpoint = "https://api.deepseek.com/v1"
			provider = "deepseek"
		}

		// Create user-specific config with parsed values
		userConfig := &config.Config{
			LLMProvider: provider,
			LLMEndpoint: endpoint,
			LLMToken:    token,
			LLMModel:    model,
		}

		return llm.NewClient(userConfig), false // false = not using default LLM
	}

	// User doesn't have their own LLM config, check if they can use default LLM
	// Estimate token usage based on actual message content
	estimatedTokens := b.estimateTokenUsage(message)

	canUseDefault, err := b.db.CanUseDefaultLLM(chatID, estimatedTokens)
	if err != nil {
		logger.Error("Failed to check if user can use default LLM", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return nil, false
	}

	if !canUseDefault {
		// User has exceeded their token limit for default LLM
		logger.Info("User cannot use default LLM - token limit exceeded", map[string]interface{}{
			"chat_id":          chatID,
			"estimated_tokens": estimatedTokens,
		})
		return nil, false
	}

	// User can use default LLM, check if bot has system-wide LLM config
	if !b.config.HasLLMConfig() {
		logger.Debug("No system-wide LLM config available", map[string]interface{}{
			"chat_id":      chatID,
			"llm_provider": b.config.LLMProvider,
			"llm_endpoint": b.config.LLMEndpoint,
			"llm_token":    b.config.LLMToken != "",
			"llm_model":    b.config.LLMModel,
		})
		return nil, false // No system-wide LLM config available
	}

	// Return client with bot's default LLM config
	logger.Info("Using default LLM config for user", map[string]interface{}{
		"chat_id":      chatID,
		"llm_provider": b.config.LLMProvider,
		"llm_model":    b.config.LLMModel,
	})
	return llm.NewClient(b.config), true // true = using default LLM
}

// generateUniquePhotoFilename generates a unique filename for photo uploads
// Format: photo_YYYYMMDD_HHMMSS_microseconds_random.ext
func (b *Bot) generateUniquePhotoFilename(originalFilename string) string {
	now := time.Now()
	
	// Get file extension
	extension := filepath.Ext(originalFilename)
	if extension == "" {
		extension = ".jpg" // Default extension
	}
	
	// Generate timestamp with microseconds
	timestamp := now.Format("20060102_150405")
	microseconds := now.Nanosecond() / 1000 // Convert to microseconds
	
	// Generate random component for additional uniqueness
	randBytes := make([]byte, 3) // 3 bytes = 6 hex chars
	rand.Read(randBytes)
	randomHex := hex.EncodeToString(randBytes)
	
	return fmt.Sprintf("photo_%s_%06d_%s%s", timestamp, microseconds, randomHex, extension)
}

// getUserIDForLocking extracts user ID for file locking
func (b *Bot) getUserIDForLocking(chatID int64) (int64, error) {
	return chatID, nil
}

// getRepositoryURL gets the repository URL for a user
func (b *Bot) getRepositoryURL(chatID int64) (string, error) {
	if b.db == nil {
		return "", fmt.Errorf("database is required for repository URL")
	}

	// Get user from database
	user, err := b.db.GetUserByChatID(chatID)
	if err != nil {
		return "", fmt.Errorf("failed to get user: %w", err)
	}

	if user == nil || user.GitHubRepo == "" {
		return "", fmt.Errorf("user not found or repository not configured")
	}

	return user.GitHubRepo, nil
}

// Rate limiting methods

// getUserRateLimiter gets or creates a rate limiter for a specific user
func (b *Bot) getUserRateLimiter(chatID int64) *rate.Limiter {
	b.userLimitersMu.RLock()
	limiter, exists := b.userLimiters[chatID]
	b.userLimitersMu.RUnlock()

	if !exists {
		b.userLimitersMu.Lock()
		// Double-check in case another goroutine created it
		if limiter, exists = b.userLimiters[chatID]; !exists {
			limiter = rate.NewLimiter(rate.Limit(30), 30) // 30 messages per second with burst of 30
			b.userLimiters[chatID] = limiter

			// Start cleanup goroutine only once
			if !b.cleanupStarted {
				b.cleanupStarted = true
				go b.cleanupUserLimiters()
			}
		}
		b.userLimitersMu.Unlock()
	}

	return limiter
}

// cleanupUserLimiters removes inactive user limiters to prevent memory leaks
func (b *Bot) cleanupUserLimiters() {
	ticker := time.NewTicker(10 * time.Minute) // Cleanup every 10 minutes
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			b.userLimitersMu.Lock()
			// Remove limiters that haven't been used recently
			// This is a simple approach - in production you might want more sophisticated cleanup
			if len(b.userLimiters) > 1000 { // Only cleanup if we have many limiters
				logger.Debug("Cleaning up user rate limiters", map[string]interface{}{
					"limiter_count": len(b.userLimiters),
				})
				// Keep only the first 100 limiters (most recently created)
				// This is a simple LRU-like approach
				newLimiters := make(map[int64]*rate.Limiter)
				count := 0
				for chatID, limiter := range b.userLimiters {
					if count < 100 {
						newLimiters[chatID] = limiter
						count++
					}
				}
				b.userLimiters = newLimiters
			}
			b.userLimitersMu.Unlock()
		}
	}
}

// rateLimitedSend sends a message with rate limiting
func (b *Bot) rateLimitedSend(chatID int64, msg tgbotapi.Chattable) (tgbotapi.Message, error) {
	// Wait for global rate limiter
	if err := b.globalLimiter.Wait(context.Background()); err != nil {
		return tgbotapi.Message{}, fmt.Errorf("global rate limiter error: %w", err)
	}

	// Wait for user-specific rate limiter
	userLimiter := b.getUserRateLimiter(chatID)
	if err := userLimiter.Wait(context.Background()); err != nil {
		return tgbotapi.Message{}, fmt.Errorf("user rate limiter error: %w", err)
	}

	logger.Debug("Sending rate-limited message", map[string]interface{}{
		"chat_id": chatID,
	})

	return b.api.Send(msg)
}

// rateLimitedRequest sends a request with rate limiting
func (b *Bot) rateLimitedRequest(chatID int64, req tgbotapi.CallbackConfig) (*tgbotapi.APIResponse, error) {
	// Wait for global rate limiter
	if err := b.globalLimiter.Wait(context.Background()); err != nil {
		return nil, fmt.Errorf("global rate limiter error: %w", err)
	}

	// Wait for user-specific rate limiter
	userLimiter := b.getUserRateLimiter(chatID)
	if err := userLimiter.Wait(context.Background()); err != nil {
		return nil, fmt.Errorf("user rate limiter error: %w", err)
	}

	logger.Debug("Sending rate-limited request", map[string]interface{}{
		"chat_id": chatID,
	})

	return b.api.Request(req)
}

// showFileSelectionButtonsForPhoto shows file selection buttons for photos (with or without captions)
func (b *Bot) showFileSelectionButtonsForPhoto(message *tgbotapi.Message, photoURL string, statusMessageID int, photoData []byte) error {
	logger.Debug("Showing file selection buttons for photo", nil)

	// Handle both caption and no-caption scenarios
	var markdownContent string
	var promptText string

	if message.Caption != "" {
		// Convert caption to markdown format
		markdownContent = b.telegramToMarkdown(message.Caption, message.CaptionEntities)
		promptText = "Please choose where to save the photo with caption:"
	} else {
		// No caption, just create a simple photo reference
		markdownContent = fmt.Sprintf("Photo: %s", photoURL)
		promptText = "Please choose where to save the photo reference:"
	}

	// Store the formatted message content with photo URL and image data for later use
	messageKey := fmt.Sprintf("%d_%d", message.Chat.ID, message.MessageID)
	// Encode image data as base64 for safe storage
	imageDataBase64 := base64.StdEncoding.EncodeToString(photoData)
	messageData := fmt.Sprintf("%s|||DELIM|||%d|||DELIM|||%s|||DELIM|||%s", markdownContent, message.MessageID, photoURL, imageDataBase64)
	b.pendingMessages[messageKey] = messageData

	// Get user's pinned files
	var pinnedFiles []string
	if b.db != nil {
		user, err := b.ensureUser(message)
		if err == nil {
			pinnedFiles = user.GetPinnedFiles()
		}
	}

	// Build keyboard rows
	var rows [][]tgbotapi.InlineKeyboardButton

	// Add pinned files row if any exist
	if len(pinnedFiles) > 0 {
		pinnedRow := []tgbotapi.InlineKeyboardButton{}
		for i, filePath := range pinnedFiles {
			displayName := strings.TrimSuffix(filePath, ".md")
			pinnedRow = append(pinnedRow, tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("📌 %s", displayName),
				fmt.Sprintf("photo_PINNED_%d_%s", i, messageKey)))
		}
		rows = append(rows, pinnedRow)
	}

	// Create main file type options
	row1 := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📝 NOTE", fmt.Sprintf("photo_NOTE_%s", messageKey)),
		tgbotapi.NewInlineKeyboardButtonData("❓ ISSUE", fmt.Sprintf("photo_ISSUE_%s", messageKey)),
	)
	// Only show TODO option if content doesn't contain line breaks (works for both caption and no-caption)
	if !strings.Contains(markdownContent, "\n") {
		row1 = append(row1, tgbotapi.NewInlineKeyboardButtonData("✅ TODO", fmt.Sprintf("photo_TODO_%s", messageKey)))
	}

	row2 := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("💡 IDEA", fmt.Sprintf("photo_IDEA_%s", messageKey)),
		tgbotapi.NewInlineKeyboardButtonData("📥 INBOX", fmt.Sprintf("photo_INBOX_%s", messageKey)),
		tgbotapi.NewInlineKeyboardButtonData("🔧 TOOL", fmt.Sprintf("photo_TOOL_%s", messageKey)),
	)

	row3 := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📁 CUSTOM", fmt.Sprintf("photo_CUSTOM_%s", messageKey)),
		tgbotapi.NewInlineKeyboardButtonData("❌ CANCEL", fmt.Sprintf("cancel_%s", messageKey)),
	)

	rows = append(rows, row1, row2, row3)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	// Edit the existing status message to show the file selection buttons
	editMsg := tgbotapi.NewEditMessageText(message.Chat.ID, statusMessageID, promptText)
	editMsg.ReplyMarkup = &keyboard

	if _, err := b.rateLimitedSend(message.Chat.ID, editMsg); err != nil {
		return fmt.Errorf("failed to edit message with file selection buttons: %w", err)
	}

	return nil
}

// ensureUser ensures the user exists in the database
func (b *Bot) ensureUser(message *tgbotapi.Message) (*database.User, error) {
	if b.db == nil {
		return nil, nil // No database configured
	}

	username := ""
	if message.From != nil && message.From.UserName != "" {
		username = message.From.UserName
	}

	// Use Chat.ID instead of From.ID for multi-user support
	chatID := message.Chat.ID

	return b.db.GetOrCreateUser(chatID, username)
}

// ensureUserFromCallback ensures the user exists in the database using callback data
func (b *Bot) ensureUserFromCallback(callback *tgbotapi.CallbackQuery) (*database.User, error) {
	if b.db == nil {
		return nil, nil // No database configured
	}

	username := ""
	if callback.From != nil && callback.From.UserName != "" {
		username = callback.From.UserName
	}

	// Use Chat.ID from callback message
	chatID := callback.Message.Chat.ID

	return b.db.GetOrCreateUser(chatID, username)
}

// getCommitterInfo returns the committer string for a user (custom or env default)
func (b *Bot) getCommitterInfo(chatID int64) string {
	if b.db == nil {
		// No database, use env default
		return b.config.CommitAuthor
	}

	// Check if user has custom committer
	user, err := b.db.GetUserByChatID(chatID)
	if err != nil {
		logger.Warn("Failed to get user for committer info", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return b.config.CommitAuthor
	}

	if user != nil && user.Committer != "" {
		return user.Committer
	}

	// Default to env committer
	return b.config.CommitAuthor
}

// getPremiumLevel returns the premium level for a user (0 for free/expired users)
func (b *Bot) getPremiumLevel(chatID int64) int {
	if b.db == nil {
		return 0 // No database, free tier
	}

	premiumUser, err := b.db.GetPremiumUser(chatID)
	if err != nil {
		logger.Warn("Failed to get premium user for level check", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return 0
	}

	if premiumUser != nil && premiumUser.IsPremiumUser() {
		return premiumUser.Level
	}

	return 0 // Free tier
}

// needsRepositoryClone checks if the repository needs to be cloned (doesn't exist locally)
func (b *Bot) needsRepositoryClone(githubProvider github.GitHubProvider) bool {
	// Use the interface method to check if repository needs cloning
	return githubProvider.NeedsClone()
}

// formatRepositorySetupError creates user-friendly error messages for repository setup failures
func (b *Bot) formatRepositorySetupError(err error, context string) string {
	errStr := err.Error()

	// Check if it's a repository size limit error
	if strings.Contains(errStr, "exceeds your tier limit") {
		return fmt.Sprintf(`🚫 <b>Repository capacity exceeded</b>

Cannot %s when repository exceeds size limits.

Please:
• Clean up your repository to free space
• Use /coffee to upgrade to premium for higher limits
• Use /insight to check current repository status

<i>Note: You can still view existing content with commands like /todo and /issue</i>`, context)
	} else if strings.Contains(errStr, "GitHub authorization failed") {
		return fmt.Sprintf(`🔑 <b>GitHub authorization failed</b>

Cannot access your repository to %s. Please:
• Check your GitHub token with /repo command
• Ensure the token has repository access permissions
• Use /coffee if you need premium features

<i>Contact support if the issue persists</i>`, context)
	} else if strings.Contains(errStr, "repository not found") || strings.Contains(errStr, "404") {
		return fmt.Sprintf(`📂 <b>Repository not found</b>

Cannot find your repository to %s. Please:
• Check your repository URL with /repo command
• Ensure the repository exists and is accessible
• Verify your GitHub token permissions

<i>Use /insight to check your current setup</i>`, context)
	} else {
		// Generic repository setup error
		return fmt.Sprintf(`⚠️ <b>Repository setup failed</b>

Cannot set up repository to %s.

Error: %v

Please:
• Use /insight to check your repository status
• Use /coffee to upgrade to premium if needed
• Contact support if the issue persists`, context, err)
	}
}

// isDuplicateCallback checks if a callback has already been processed recently
func (b *Bot) isDuplicateCallback(callbackID string) bool {
	b.callbacksMu.RLock()
	_, exists := b.processedCallbacks[callbackID]
	b.callbacksMu.RUnlock()
	return exists
}

// markCallbackProcessed marks a callback as processed and starts cleanup timer
func (b *Bot) markCallbackProcessed(callbackID string) {
	b.callbacksMu.Lock()
	b.processedCallbacks[callbackID] = time.Now()
	b.callbacksMu.Unlock()

	// Clean up old callbacks after 30 seconds to prevent memory leak
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("Panic in callback cleanup", map[string]interface{}{
					"error": r,
				})
			}
		}()

		time.Sleep(30 * time.Second)
		b.callbacksMu.Lock()
		delete(b.processedCallbacks, callbackID)
		b.callbacksMu.Unlock()
	}()
}
