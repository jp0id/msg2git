package telegram

import (
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/msg2git/msg2git/internal/consts"
	"github.com/msg2git/msg2git/internal/logger"
)

// File selection callback handlers

func (b *Bot) handleFileSelection(callback *tgbotapi.CallbackQuery) error {
	parts := strings.SplitN(callback.Data, "_", 3)
	if len(parts) < 3 {
		return fmt.Errorf("invalid callback data format")
	}

	fileType := strings.ToLower(parts[1])
	messageKey := parts[2]

	// Handle PINNED type specially (format: file_PINNED_index_messageKey)
	if fileType == "pinned" {
		return b.handlePinnedFileSelection(callback)
	}

	// Handle ISSUE type specially
	if fileType == "issue" {
		return b.handleIssueCreation(callback, messageKey)
	}

	// Handle CUSTOM type specially
	if fileType == "custom" {
		return b.handleCustomFileSelection(callback, messageKey)
	}

	filename := fileType + ".md"

	// Retrieve the original message content and ID
	messageData, exists := b.pendingMessages[messageKey]
	if !exists {
		return fmt.Errorf("original message not found")
	}

	// Parse the stored data (content|||DELIM|||messageID)
	dataParts := strings.SplitN(messageData, "|||DELIM|||", 2)
	if len(dataParts) != 2 {
		return fmt.Errorf("invalid message data format")
	}

	content := dataParts[0]
	originalMessageIDStr := dataParts[1]

	// Convert message ID back to int
	originalMessageID, err := strconv.Atoi(originalMessageIDStr)
	if err != nil {
		logger.Warn("Failed to parse message ID, using 0", map[string]interface{}{
			"error": err.Error(),
		})
		originalMessageID = 0
	}

	// Clean up
	delete(b.pendingMessages, messageKey)

	// Ensure user exists in database if database is configured
	_, err = b.ensureUser(callback.Message)
	if err != nil {
		errorMsg := fmt.Sprintf("❌ Failed to get user: %v", err)
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
			logger.Error("Failed to edit message", map[string]interface{}{
				"error": sendErr.Error(),
			})
			b.sendResponse(callback.Message.Chat.ID, errorMsg)
		}
		return nil
	}

	// Get user-specific GitHub provider (new interface-based approach)
	userGitHubProvider, err := b.getUserGitHubProvider(callback.Message.Chat.ID)
	if err != nil {
		errorMsg := "❌ " + err.Error()
		if b.db != nil {
			errorMsg += ". " + consts.GitHubSetupPrompt
		}
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
			logger.Error("Failed to edit message", map[string]interface{}{
				"error": sendErr.Error(),
			})
			b.sendResponse(callback.Message.Chat.ID, errorMsg)
		}
		return nil
	}

	// Get user-specific LLM client
	userLLMClient, isUsingDefaultLLM := b.getUserLLMClientWithUsageTracking(callback.Message.Chat.ID, content)

	var formattedContent string
	var title string

	// Start progress tracking
	b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 0, "🔄 Starting process...")

	if filename == "todo.md" {
		// TODO.md uses simple format without LLM processing
		// Check if content has line breaks (not allowed for TODOs)
		if strings.Contains(content, "\n") {
			// Update the message to show error
			errorMsg := "❌ TODOs cannot contain line breaks. Please use a different file type."
			editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
			if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
				logger.Error("Failed to edit message", map[string]interface{}{
					"error": err.Error(),
				})
				b.sendResponse(callback.Message.Chat.ID, errorMsg)
			}
			return fmt.Errorf("content contains line breaks, cannot save to TODO.md")
		}

		// Show processing status with progress
		b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 30, "🔄 Processing TODO...")

		formattedContent = b.formatTodoContent(content, originalMessageID, callback.Message.Chat.ID)
		title = "todo"
	} else {
		// Step 1: Ensure repository exists (with double confirmation if cloning needed)
		premiumLevel := b.getPremiumLevel(callback.Message.Chat.ID)
		b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 30, "📊 Checking repository capacity...")

		// Show appropriate progress message based on whether repo needs cloning
		if b.needsRepositoryClone(userGitHubProvider) {

			// Ensure repository with premium-aware cloning (includes double confirmation)
			if err := userGitHubProvider.EnsureRepositoryWithPremium(premiumLevel); err != nil {
				logger.Error("Failed to ensure repository", map[string]interface{}{
					"error":   err.Error(),
					"chat_id": callback.Message.Chat.ID,
				})
				errorMsg := b.formatRepositorySetupError(err, "save content")
				editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
				editMsg.ParseMode = "html"
				if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
					logger.Error("Failed to edit message", map[string]interface{}{
						"error": sendErr.Error(),
					})
					// Fallback to simple message
					b.sendResponse(callback.Message.Chat.ID, fmt.Sprintf("❌ Repository setup failed: %v", err))
				}
				return nil
			}
		}

		isNearCapacity, percentage, err := userGitHubProvider.IsRepositoryNearCapacityWithPremium(premiumLevel)
		if err != nil {
			logger.Warn("Failed to check repository capacity", map[string]interface{}{
				"error": err.Error(),
			})
		} else if isNearCapacity {
			errorMsg := fmt.Sprintf(RepoAlmostFullTemplate, percentage)

			editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
			editMsg.ParseMode = "html"
			if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
				logger.Error("Failed to edit message", map[string]interface{}{
					"error": sendErr.Error(),
				})
				b.sendResponse(callback.Message.Chat.ID, errorMsg)
			}
			return nil
		}

		// Show LLM processing status with progress
		b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 60, "🧠 LLM processing...")

		// Other files use LLM processing for title and hashtags (if available)
		var tags string
		if userLLMClient != nil {
			llmResponse, usage, err := userLLMClient.ProcessMessage(content)
			if err != nil {
				logger.Warn("LLM processing failed, using content-based title", map[string]interface{}{
					"error": err.Error(),
				})
				title = b.generateTitleFromContent(content)
				tags = ""
			} else {
				title, tags = b.parseTitleAndTags(llmResponse, content)

				// Record token usage in database based on LLM type
				if usage != nil && b.db != nil {
					if isUsingDefaultLLM {
						// Default LLM: record in both user_insights and user_usage
						if err := b.db.IncrementTokenUsageAll(callback.Message.Chat.ID, int64(usage.PromptTokens), int64(usage.CompletionTokens)); err != nil {
							logger.Warn("Failed to record token usage (default LLM)", map[string]interface{}{
								"error":             err.Error(),
								"chat_id":           callback.Message.Chat.ID,
								"prompt_tokens":     usage.PromptTokens,
								"completion_tokens": usage.CompletionTokens,
							})
						}
					} else {
						// Personal LLM: record only in user_insights
						if err := b.db.IncrementTokenUsageInsights(callback.Message.Chat.ID, int64(usage.PromptTokens), int64(usage.CompletionTokens)); err != nil {
							logger.Warn("Failed to record token usage (personal LLM)", map[string]interface{}{
								"error":             err.Error(),
								"chat_id":           callback.Message.Chat.ID,
								"prompt_tokens":     usage.PromptTokens,
								"completion_tokens": usage.CompletionTokens,
							})
						}
					}
				}
			}
		} else {
			logger.Debug("No LLM client available, using content-based title", nil)
			title = b.generateTitleFromContent(content)
			tags = ""
		}
		formattedContent = b.formatMessageContentWithTitleAndTags(content, filename, originalMessageID, callback.Message.Chat.ID, title, tags)
	}

	// Show GitHub commit status with progress
	b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 80, "📝 Saving to GitHub...")

	// Commit to GitHub with custom committer info and premium level
	commitMsg := fmt.Sprintf("Add %s to %s via Telegram", title, filename)
	committerInfo := b.getCommitterInfo(callback.Message.Chat.ID)
	premiumLevel := b.getPremiumLevel(callback.Message.Chat.ID)
	if err := userGitHubProvider.CommitFileWithAuthorAndPremium(filename, formattedContent, commitMsg, committerInfo, premiumLevel); err != nil {
		// Check if it's an authorization error and provide helpful message
		if strings.Contains(err.Error(), "GitHub authorization failed") {
			// Update the message to show auth error with helpful instructions
			errorMsg := "❌ " + err.Error()
			editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
			if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
				logger.Error("Failed to edit message", map[string]interface{}{
					"error": sendErr.Error(),
				})
				b.sendResponse(callback.Message.Chat.ID, errorMsg)
			}
			return nil // Don't return error to avoid double error handling
		}
		// Edit the existing message to show the error instead of sending a new one
		errorMsg := fmt.Sprintf("❌ Failed to save: %v", err)
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
			logger.Error("Failed to edit message", map[string]interface{}{
				"error": sendErr.Error(),
			})
			b.sendResponse(callback.Message.Chat.ID, errorMsg)
		}
		return nil // Don't return error to avoid double error handling
	}

	// Increment commit count and update repo size
	if b.db != nil {
		if err := b.db.IncrementCommitCount(callback.Message.Chat.ID); err != nil {
			logger.Error("Failed to increment commit count", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
		}

		// Update repo size
		if sizeMB, _, sizeErr := userGitHubProvider.GetRepositorySizeInfoWithPremium(premiumLevel); sizeErr == nil {
			if updateErr := b.db.UpdateRepoSize(callback.Message.Chat.ID, sizeMB); updateErr != nil {
				logger.Error("Failed to update repo size", map[string]interface{}{
					"error":   updateErr.Error(),
					"chat_id": callback.Message.Chat.ID,
					"size_mb": sizeMB,
				})
			}
		} else {
			logger.Warn("Failed to get repo size for update", map[string]interface{}{
				"error":   sizeErr.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
		}
	}

	// Update the message to show success with GitHub menu button
	githubURL, err := userGitHubProvider.GetGitHubFileURLWithBranch(filename)
	successMsg := fmt.Sprintf("✅ Saved to %s", strings.ToUpper(parts[1]))

	// Create inline keyboard with GitHub link button
	var keyboard *tgbotapi.InlineKeyboardMarkup
	if err != nil {
		logger.Warn("Failed to generate GitHub file URL", map[string]interface{}{
			"error":    err.Error(),
			"filename": filename,
		})
		// No keyboard if URL generation fails
	} else {
		row := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("🔗 View on GitHub", githubURL),
		)
		keyboardValue := tgbotapi.NewInlineKeyboardMarkup(row)
		keyboard = &keyboardValue
	}

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, successMsg)
	if keyboard != nil {
		editMsg.ReplyMarkup = keyboard
	}
	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		logger.Error("Failed to edit message", map[string]interface{}{
			"error": err.Error(),
		})
		// Fallback: send new message
		b.sendResponse(callback.Message.Chat.ID, successMsg)
	}

	return nil
}

func (b *Bot) handleCancel(callback *tgbotapi.CallbackQuery) error {
	// Extract messageKey from callback data
	parts := strings.SplitN(callback.Data, "_", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid cancel callback data format")
	}

	messageKey := parts[1]

	// Clean up the pending message
	delete(b.pendingMessages, messageKey)

	// Update the message to show cancellation
	cancelMsg := "❌ Cancelled"
	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, cancelMsg)
	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		logger.Error("Failed to edit message", map[string]interface{}{
			"error": err.Error(),
		})
		// Fallback: send new message
		b.sendResponse(callback.Message.Chat.ID, cancelMsg)
	}

	return nil
}

// handlePinnedFileSelection handles selection of pinned custom files
func (b *Bot) handlePinnedFileSelection(callback *tgbotapi.CallbackQuery) error {
	// Parse callback data: file_PINNED_index_messageKey
	parts := strings.SplitN(callback.Data, "_", 4)
	if len(parts) != 4 {
		return fmt.Errorf("invalid pinned file callback data format")
	}

	pinnedIndexStr := parts[2]
	messageKey := parts[3]

	// Convert index to int
	pinnedIndex, err := strconv.Atoi(pinnedIndexStr)
	if err != nil {
		return fmt.Errorf("invalid pinned file index: %w", err)
	}

	// Get user's custom files
	user, err := b.ensureUserFromCallback(callback)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	customFiles := user.GetCustomFiles()
	if pinnedIndex >= len(customFiles) || pinnedIndex < 0 {
		return fmt.Errorf("pinned file index out of range")
	}

	// Get the selected pinned file
	selectedFile := customFiles[pinnedIndex]

	// Retrieve the original message content and ID
	messageData, exists := b.pendingMessages[messageKey]
	if !exists {
		return fmt.Errorf("original message not found")
	}

	// Parse the stored data (content|||DELIM|||messageID)
	dataParts := strings.SplitN(messageData, "|||DELIM|||", 2)
	if len(dataParts) != 2 {
		return fmt.Errorf("invalid message data format")
	}

	content := dataParts[0]
	originalMessageIDStr := dataParts[1]

	// Convert message ID back to int
	originalMessageID, err := strconv.Atoi(originalMessageIDStr)
	if err != nil {
		logger.Warn("Failed to parse message ID, using 0", map[string]interface{}{
			"error": err.Error(),
		})
		originalMessageID = 0
	}

	// Update the progress message
	progressMsg := fmt.Sprintf("📌 Saving to pinned file: %s", selectedFile)
	b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 0, progressMsg)

	// Get user-specific GitHub provider
	userGitHubProvider, err := b.getUserGitHubProvider(callback.Message.Chat.ID)
	if err != nil {
		errorMsg := "❌ " + err.Error()
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
			b.sendResponse(callback.Message.Chat.ID, errorMsg)
		}
		return nil
	}

	// Get premium level and ensure repository setup
	premiumLevel := b.getPremiumLevel(callback.Message.Chat.ID)

	// Show appropriate progress message based on whether repo needs cloning
	if userGitHubProvider.NeedsClone() {
		// Check repository capacity
		b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 30, "📊 Checking repository capacity...")

		// Ensure repository with premium-aware cloning
		if err := userGitHubProvider.EnsureRepositoryWithPremium(premiumLevel); err != nil {
			logger.Error("Failed to ensure repository", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
			errorMsg := b.formatRepositorySetupError(err, "save content")
			editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
			editMsg.ParseMode = "html"
			if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
				b.sendResponse(callback.Message.Chat.ID, fmt.Sprintf("❌ Repository setup failed: %v", err))
			}
			return nil
		}
	}

	isNearCapacity, percentage, err := userGitHubProvider.IsRepositoryNearCapacityWithPremium(premiumLevel)
	if err != nil {
		logger.Warn("Failed to check repository capacity", map[string]interface{}{
			"error": err.Error(),
		})
	} else if isNearCapacity {
		errorMsg := fmt.Sprintf(RepoAlmostFullTemplate, percentage)
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		editMsg.ParseMode = "html"
		if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
			b.sendResponse(callback.Message.Chat.ID, errorMsg)
		}
		return nil
	}

	// Get user LLM client for processing
	userLLMClient, isUsingDefaultLLM := b.getUserLLMClientWithUsageTracking(callback.Message.Chat.ID, content)

	// Process LLM if configured
	b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 60, "🧠 LLM processing...")

	// Process with LLM for title and hashtags (if available)
	var formattedContent string
	var title string
	var tags string
	if userLLMClient != nil {
		llmResponse, usage, err := userLLMClient.ProcessMessage(content)
		if err != nil {
			logger.Warn("LLM processing failed, using content-based title", map[string]interface{}{
				"error": err.Error(),
			})
			title = b.generateTitleFromContent(content)
			tags = ""
		} else {
			title, tags = b.parseTitleAndTags(llmResponse, content)

			// Record token usage in database based on LLM type
			if usage != nil && b.db != nil {
				if isUsingDefaultLLM {
					// Default LLM: record in both user_insights and user_usage
					if err := b.db.IncrementTokenUsageAll(callback.Message.Chat.ID, int64(usage.PromptTokens), int64(usage.CompletionTokens)); err != nil {
						logger.Warn("Failed to record token usage (default LLM)", map[string]interface{}{
							"error":             err.Error(),
							"chat_id":           callback.Message.Chat.ID,
							"prompt_tokens":     usage.PromptTokens,
							"completion_tokens": usage.CompletionTokens,
						})
					}
				} else {
					// Personal LLM: record only in user_insights
					if err := b.db.IncrementTokenUsageInsights(callback.Message.Chat.ID, int64(usage.PromptTokens), int64(usage.CompletionTokens)); err != nil {
						logger.Warn("Failed to record token usage (personal LLM)", map[string]interface{}{
							"error":             err.Error(),
							"chat_id":           callback.Message.Chat.ID,
							"prompt_tokens":     usage.PromptTokens,
							"completion_tokens": usage.CompletionTokens,
						})
					}
				}
			}
		}
	} else {
		logger.Debug("No LLM client available, using content-based title", nil)
		title = b.generateTitleFromContent(content)
		tags = ""
	}
	formattedContent = b.formatMessageContentWithTitleAndTags(content, selectedFile, originalMessageID, callback.Message.Chat.ID, title, tags)

	// Show GitHub commit status with progress
	b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 80, "📝 Saving to GitHub...")

	// Commit to GitHub with custom committer info and premium level
	commitMsg := fmt.Sprintf("Add %s to %s via Telegram", title, selectedFile)
	committerInfo := b.getCommitterInfo(callback.Message.Chat.ID)
	if err := userGitHubProvider.CommitFileWithAuthorAndPremium(selectedFile, formattedContent, commitMsg, committerInfo, premiumLevel); err != nil {
		// Check if it's an authorization error and provide helpful message
		if strings.Contains(err.Error(), "GitHub authorization failed") {
			errorMsg := "❌ " + err.Error()
			editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
			if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
				b.sendResponse(callback.Message.Chat.ID, errorMsg)
			}
			return nil
		}

		// Generic error handling
		errorMsg := "❌ Failed to save to GitHub: " + err.Error()
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
			b.sendResponse(callback.Message.Chat.ID, errorMsg)
		}
		return nil
	}

	// Clean up pending message
	delete(b.pendingMessages, messageKey)

	// Success message with GitHub link
	githubURL, err := userGitHubProvider.GetGitHubFileURLWithBranch(selectedFile)
	successMsg := fmt.Sprintf("✅ Saved to pinned file: %s", selectedFile)

	// Create inline keyboard with GitHub link button
	var keyboard *tgbotapi.InlineKeyboardMarkup
	if err != nil {
		logger.Warn("Failed to generate GitHub file URL for pinned file", map[string]interface{}{
			"error":    err.Error(),
			"filename": selectedFile,
		})
		// No keyboard if URL generation fails
	} else {
		row := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("🔗 View on GitHub", githubURL),
		)
		keyboardValue := tgbotapi.NewInlineKeyboardMarkup(row)
		keyboard = &keyboardValue
	}

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, successMsg)
	if keyboard != nil {
		editMsg.ReplyMarkup = keyboard
	}
	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		b.sendResponse(callback.Message.Chat.ID, successMsg)
	}

	// Increment commit count
	if b.db != nil {
		if err := b.db.IncrementCommitCount(callback.Message.Chat.ID); err != nil {
			logger.Error("Failed to increment commit count", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}

	return nil
}
