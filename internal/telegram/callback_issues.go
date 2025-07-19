package telegram

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/msg2git/msg2git/internal/consts"
	"github.com/msg2git/msg2git/internal/database"
	"github.com/msg2git/msg2git/internal/github"
	"github.com/msg2git/msg2git/internal/logger"
)

// Issue-related callback handlers

func (b *Bot) handleIssueMore(callback *tgbotapi.CallbackQuery) error {
	// Parse offset from callback data
	parts := strings.Split(callback.Data, "_")
	if len(parts) != 3 {
		return fmt.Errorf("invalid callback data format")
	}

	offset, err := strconv.Atoi(parts[2])
	if err != nil {
		return fmt.Errorf("invalid offset: %w", err)
	}

	// Use the existing message for editing instead of creating new
	return b.handleIssueCommandWithMessageID(callback.Message.Chat.ID, callback.Message.MessageID, offset)
}

func (b *Bot) handleIssueOpen(callback *tgbotapi.CallbackQuery) error {
	// This is a simple callback that doesn't need special handling
	// since we use URL buttons that open directly in browser
	return nil
}

// handleIssueComment handles the comment button click with force reply
func (b *Bot) handleIssueComment(callback *tgbotapi.CallbackQuery) error {
	// Parse issue number from callback data
	parts := strings.Split(callback.Data, "_")
	if len(parts) != 3 {
		return fmt.Errorf("invalid callback data format")
	}

	issueNumber, err := strconv.Atoi(parts[2])
	if err != nil {
		return fmt.Errorf("invalid issue number: %w", err)
	}

	// Send force reply message to get comment
	forceReplyMsg := fmt.Sprintf("💬 <b>Add comment to issue #%d</b>\n\nPlease reply to this message with your comment:", issueNumber)
	msg := tgbotapi.NewMessage(callback.Message.Chat.ID, forceReplyMsg)
	msg.ParseMode = "html"
	msg.ReplyMarkup = tgbotapi.ForceReply{
		ForceReply:            true,
		InputFieldPlaceholder: "Type your comment here...",
		Selective:             true,
	}

	sentMsg, err := b.rateLimitedSend(callback.Message.Chat.ID, msg)
	if err != nil {
		logger.Error("Failed to send force reply message", map[string]interface{}{
			"error": err.Error(),
		})
		return err
	}

	// Store the issue number with the sent message ID for later processing
	messageKey := fmt.Sprintf("comment_%d_%d", callback.Message.Chat.ID, sentMsg.MessageID)
	b.pendingMessages[messageKey] = fmt.Sprintf("issue_comment_%d", issueNumber)

	return nil
}

// handleIssueClose handles the close button click directly without confirmation
func (b *Bot) handleIssueClose(callback *tgbotapi.CallbackQuery) error {
	// Parse issue number from callback data
	parts := strings.Split(callback.Data, "_")
	if len(parts) != 3 {
		return fmt.Errorf("invalid callback data format")
	}

	issueNumber, err := strconv.Atoi(parts[2])
	if err != nil {
		return fmt.Errorf("invalid issue number: %w", err)
	}

	// Get user-specific GitHub manager
	userGitHubProvider, err := b.getUserGitHubProvider(callback.Message.Chat.ID)
	if err != nil {
		b.sendResponse(callback.Message.Chat.ID, "❌ GitHub not configured. Please use /repo to settle repo first.")
		return nil
	}

	// Send progress message
	progressMsg := fmt.Sprintf("🔄 Closing issue #%d...", issueNumber)
	progressMessage := tgbotapi.NewMessage(callback.Message.Chat.ID, progressMsg)
	sentMsg, err := b.rateLimitedSend(callback.Message.Chat.ID, progressMessage)
	var progressMessageID int
	if err != nil {
		logger.Error("Failed to send progress message", map[string]interface{}{
			"error": err.Error(),
		})
		progressMessageID = 0
	} else {
		progressMessageID = sentMsg.MessageID
	}

	// Close the GitHub issue
	err = userGitHubProvider.CloseIssue(issueNumber)
	if err != nil {
		logger.Error("Failed to close GitHub issue", map[string]interface{}{
			"error":        err.Error(),
			"issue_number": issueNumber,
			"chat_id":      callback.Message.Chat.ID,
		})

		errorMsg := fmt.Sprintf("❌ Failed to close issue #%d: %v", issueNumber, err)
		if progressMessageID > 0 {
			editErrorMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, progressMessageID, errorMsg)
			if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editErrorMsg); err != nil {
				b.sendResponse(callback.Message.Chat.ID, errorMsg)
			}
		} else {
			b.sendResponse(callback.Message.Chat.ID, errorMsg)
		}
		return nil
	}

	// Increment issue close count in insights
	if b.db != nil {
		if err := b.db.IncrementIssueCloseCount(callback.Message.Chat.ID); err != nil {
			logger.Error("Failed to increment issue close count", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
		}
	}

	// Update local issue.md file to reflect the closed status
	// Get file lock manager and acquire lock before reading
	flm := github.GetFileLockManager()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Get user ID and repository URL for locking
	userID, err := b.getUserIDForLocking(callback.Message.Chat.ID)
	if err != nil {
		logger.Error("Failed to get user ID for locking", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		// Continue without locking for backward compatibility
	} else {
		repoURL, err := b.getRepositoryURL(callback.Message.Chat.ID)
		if err != nil {
			logger.Error("Failed to get repository URL for locking", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
			// Continue without locking for backward compatibility
		} else {
			// Acquire lock for issue.md
			issueHandle, err := flm.AcquireFileLock(ctx, userID, repoURL, "issue.md", true)
			if err != nil {
				logger.Error("Failed to acquire lock for issue.md during close", map[string]interface{}{
					"error":   err.Error(),
					"chat_id": callback.Message.Chat.ID,
				})
				// Continue without locking for backward compatibility
			} else {
				defer issueHandle.Release()
				logger.Debug("Acquired file lock for issue close operation", map[string]interface{}{
					"chat_id": callback.Message.Chat.ID,
					"file":    "issue.md",
				})
			}
		}
	}

	issueContent, err := userGitHubProvider.ReadFile("issue.md")
	if err != nil {
		logger.Error("Failed to read issue.md for status update", map[string]interface{}{
			"error": err.Error(),
		})
	} else {
		// Update the specific issue status from 🟢 (open) to 🔴 (closed)
		// Use regex to find and replace the specific issue line
		lines := strings.Split(issueContent, "\n")
		var updatedLines []string

		for _, line := range lines {
			// Look for lines that contain this specific issue number
			if strings.Contains(line, fmt.Sprintf("#%d", issueNumber)) && strings.Contains(line, "🟢") {
				// Replace only this specific issue's status
				updatedLine := strings.Replace(line, "🟢", "🔴", 1)
				updatedLines = append(updatedLines, updatedLine)
				logger.Debug("Updated issue status in issue.md", map[string]interface{}{
					"issue_number": issueNumber,
					"old_line":     line,
					"new_line":     updatedLine,
				})
			} else {
				updatedLines = append(updatedLines, line)
			}
		}

		updatedContent := strings.Join(updatedLines, "\n")

		// Only commit if we actually made changes
		if updatedContent != issueContent {
			// Commit the status update using REPLACE (not prepend) to avoid duplication
			commitMsg := fmt.Sprintf("Update issue #%d status to closed via Telegram", issueNumber)
			committerInfo := b.getCommitterInfo(callback.Message.Chat.ID)
			premiumLevel := b.getPremiumLevel(callback.Message.Chat.ID)
			if err := userGitHubProvider.ReplaceFileWithAuthorAndPremium("issue.md", updatedContent, commitMsg, committerInfo, premiumLevel); err != nil {
				logger.Error("Failed to update issue.md status", map[string]interface{}{
					"error": err.Error(),
				})
			} else {
				logger.Info("Successfully updated issue.md status", map[string]interface{}{
					"issue_number": issueNumber,
				})
			}
		}
	}

	// Show success message
	successMsg := fmt.Sprintf("✅ Issue #%d has been closed successfully!", issueNumber)
	if progressMessageID > 0 {
		editSuccessMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, progressMessageID, successMsg)
		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editSuccessMsg); err != nil {
			b.sendResponse(callback.Message.Chat.ID, successMsg)
		}
	} else {
		b.sendResponse(callback.Message.Chat.ID, successMsg)
	}

	return nil
}

// handleIssueCommentReply processes the user's comment reply (text or photo)
func (b *Bot) handleIssueCommentReply(message *tgbotapi.Message, commentData string) error {
	// Parse issue number from comment data (format: "issue_comment_123")
	parts := strings.Split(commentData, "_")
	if len(parts) != 3 {
		return fmt.Errorf("invalid comment data format")
	}

	issueNumber, err := strconv.Atoi(parts[2])
	if err != nil {
		return fmt.Errorf("invalid issue number: %w", err)
	}

	var commentText string
	var statusMsg string

	// Check if this is a photo message or text message
	if len(message.Photo) > 0 {
		// Photo comment
		statusMsg = fmt.Sprintf("📷 Adding photo comment to issue #%d...", issueNumber)
	} else {
		// Text comment
		commentText = strings.TrimSpace(message.Text)
		if commentText == "" {
			b.sendResponse(message.Chat.ID, "❌ Comment cannot be empty.")
			return nil
		}
		statusMsg = fmt.Sprintf("🔄 Adding comment to issue #%d...", issueNumber)
	}

	// Send initial status message
	statusMessageID := b.sendResponseAndGetMessageID(message.Chat.ID, statusMsg)

	// Get user-specific GitHub manager
	userGitHubProvider, err := b.getUserGitHubProvider(message.Chat.ID)
	if err != nil {
		errorMsg := "❌ GitHub not configured. Please use /repo to settle repo first."
		if statusMessageID > 0 {
			b.editMessage(message.Chat.ID, statusMessageID, errorMsg)
		} else {
			b.sendResponse(message.Chat.ID, errorMsg)
		}
		return nil
	}

	// Process photo if this is a photo comment
	if len(message.Photo) > 0 {
		// Ensure user exists in database
		_, err := b.ensureUser(message)
		if err != nil {
			errorMsg := fmt.Sprintf("❌ Failed to get user: %v", err)
			if statusMessageID > 0 {
				b.editMessage(message.Chat.ID, statusMessageID, errorMsg)
			} else {
				b.sendResponse(message.Chat.ID, errorMsg)
			}
			return nil
		}

		// Update progress message
		b.updateProgressMessage(message.Chat.ID, statusMessageID, 20, "⬇️ Downloading photo...")

		// Get the largest photo (last in array) - same as handlePhotoMessage
		photo := message.Photo[len(message.Photo)-1]

		// Download the photo
		photoData, filename, err := b.downloadPhoto(photo.FileID)
		if err != nil {
			logger.Error("Failed to download photo for issue comment", map[string]interface{}{
				"error":        err.Error(),
				"file_id":      photo.FileID,
				"chat_id":      message.Chat.ID,
				"issue_number": issueNumber,
			})
			errorMsg := fmt.Sprintf("❌ Failed to download photo: %v", err)
			if statusMessageID > 0 {
				b.editMessage(message.Chat.ID, statusMessageID, errorMsg)
			} else {
				b.sendResponse(message.Chat.ID, errorMsg)
			}
			return nil
		}

		// Generate a unique filename with timestamp, microseconds, and random component
		photoFilename := b.generateUniquePhotoFilename(filename)

		// Update progress message
		b.updateProgressMessage(message.Chat.ID, statusMessageID, 50, "📝 Uploading photo to GitHub CDN...")

		// Upload to GitHub CDN and get the URL - same as handlePhotoMessage
		photoURL, err := userGitHubProvider.UploadImageToCDN(photoFilename, photoData)
		if err != nil {
			logger.Error("Failed to upload photo to GitHub CDN for issue comment", map[string]interface{}{
				"error":        err.Error(),
				"filename":     photoFilename,
				"size":         len(photoData),
				"chat_id":      message.Chat.ID,
				"issue_number": issueNumber,
			})
			errorMsg := fmt.Sprintf("❌ Failed to upload photo: %v", err)
			if statusMessageID > 0 {
				b.editMessage(message.Chat.ID, statusMessageID, errorMsg)
			} else {
				b.sendResponse(message.Chat.ID, errorMsg)
			}
			return nil
		}

		// Update progress message
		b.updateProgressMessage(message.Chat.ID, statusMessageID, 70, "💬 Adding photo comment to issue...")

		// Create comment text with photo markdown and optional caption
		if message.Caption != "" {
			commentText = fmt.Sprintf("![Photo](%s)\n\n%s", photoURL, strings.TrimSpace(message.Caption))
		} else {
			commentText = fmt.Sprintf("![Photo](%s)", photoURL)
		}

		// Increment image count for photo upload
		if b.db != nil {
			if err := b.db.IncrementImageCount(message.Chat.ID); err != nil {
				logger.Error("Failed to increment image count for issue comment", map[string]interface{}{
					"error":   err.Error(),
					"chat_id": message.Chat.ID,
				})
			}
		}
	} else {
		// Update progress for text comment
		b.updateProgressMessage(message.Chat.ID, statusMessageID, 50, "💬 Adding comment to issue...")
	}

	// Add comment to GitHub issue
	commentURL, err := userGitHubProvider.AddIssueComment(issueNumber, commentText)
	if err != nil {
		logger.Error("Failed to add comment to GitHub issue", map[string]interface{}{
			"error":        err.Error(),
			"issue_number": issueNumber,
			"chat_id":      message.Chat.ID,
		})

		errorMsg := fmt.Sprintf("❌ Failed to add comment to issue #%d: %v", issueNumber, err)
		if statusMessageID > 0 {
			b.editMessage(message.Chat.ID, statusMessageID, errorMsg)
		} else {
			b.sendResponse(message.Chat.ID, errorMsg)
		}
		return nil
	}

	// Increment issue comment count in insights
	if b.db != nil {
		if err := b.db.IncrementIssueCommentCount(message.Chat.ID); err != nil {
			logger.Error("Failed to increment issue comment count", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": message.Chat.ID,
			})
		}
	}

	// Show success message
	var successMsg string
	if len(message.Photo) > 0 {
		if message.Caption != "" {
			successMsg = fmt.Sprintf("✅ Photo with caption added to issue #%d successfully!", issueNumber)
		} else {
			successMsg = fmt.Sprintf("✅ Photo added to issue #%d successfully!", issueNumber)
		}
	} else {
		successMsg = fmt.Sprintf("✅ Comment added to issue #%d successfully!", issueNumber)
	}

	// Create keyboard with direct comment link
	var keyboard *tgbotapi.InlineKeyboardMarkup
	if commentURL != "" {
		row := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("💬 View Comment", commentURL),
		)
		keyboardValue := tgbotapi.NewInlineKeyboardMarkup(row)
		keyboard = &keyboardValue
	}

	if statusMessageID > 0 {
		editMsg := tgbotapi.NewEditMessageText(message.Chat.ID, statusMessageID, successMsg)
		if keyboard != nil {
			editMsg.ReplyMarkup = keyboard
		}
		if _, err := b.rateLimitedSend(message.Chat.ID, editMsg); err != nil {
			b.sendResponse(message.Chat.ID, successMsg)
		}
	} else {
		msg := tgbotapi.NewMessage(message.Chat.ID, successMsg)
		if keyboard != nil {
			msg.ReplyMarkup = *keyboard
		}
		b.rateLimitedSend(message.Chat.ID, msg)
	}

	return nil
}

func (b *Bot) handleIssueCreation(callback *tgbotapi.CallbackQuery, messageKey string) error {
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
	_ = originalMessageID

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

	// Get user-specific GitHub manager
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

	// Start progress tracking for issue creation
	premiumLevel := b.getPremiumLevel(callback.Message.Chat.ID)

	// Check issue creation limits
	if b.db != nil {
		canCreate, currentCount, limit, err := b.db.CheckUsageIssueLimit(callback.Message.Chat.ID, premiumLevel)
		if err != nil {
			logger.Error("Failed to check issue limit", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
		} else if !canCreate {
			// User has reached their issue limit
			tierNames := map[int]string{0: "free", 1: "☕ Coffee", 2: "🍰 Cake", 3: "🎁 Sponsor"}
			currentTier := tierNames[premiumLevel]

			var upgradeMsg string
			if premiumLevel < 3 {
				nextLimit := database.GetIssueLimit(premiumLevel + 1)
				upgradeMsg = fmt.Sprintf(IssueLimitUpgradeTemplate, nextLimit)
			}

			errorMsg := fmt.Sprintf(`🚫 <b>Issue creation limit reached</b>

You've used %d/%d issues on the %s tier.%s

<i>You can still view and manage existing issues with /issue command</i>`, currentCount, limit, currentTier, upgradeMsg)

			editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
			editMsg.ParseMode = "html"
			if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
				logger.Error("Failed to edit message with issue limit error", map[string]interface{}{
					"error": sendErr.Error(),
				})
				b.sendResponse(callback.Message.Chat.ID, fmt.Sprintf("❌ Issue creation limit reached: %d/%d", currentCount, limit))
			}
			return nil
		}
	}

	// Step 1: Ensure repository exists (with double confirmation if cloning needed)
	// Show appropriate progress message based on whether repo needs cloning
	if b.needsRepositoryClone(userGitHubProvider) {
		b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 10, "📊 Checking repository capacity...")

		// Ensure repository with premium-aware cloning (includes double confirmation)
		if err := userGitHubProvider.EnsureRepositoryWithPremium(premiumLevel); err != nil {
			logger.Error("Failed to ensure repository", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
			errorMsg := b.formatRepositorySetupError(err, "create GitHub issues")
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

	isNearCapacity, _, err := userGitHubProvider.IsRepositoryNearCapacityWithPremium(premiumLevel)
	if err != nil {
		logger.Warn("Failed to check repository capacity, proceeding with issue creation", map[string]interface{}{
			"error": err.Error(),
		})
	} else if isNearCapacity {
		// Repository is near/at capacity - reject issue creation
		// Issues use 100% threshold instead of 97% since they're creating GitHub issues, not local files
		sizeMB, actualPercentage, sizeErr := userGitHubProvider.GetRepositorySizeInfoWithPremium(premiumLevel)
		if sizeErr == nil && actualPercentage >= 100 {
			maxSizeMB := userGitHubProvider.GetRepositoryMaxSizeWithPremium(premiumLevel)
			errorMsg := fmt.Sprintf(RepoCapacityIssueTemplate, actualPercentage, sizeMB, maxSizeMB)

			editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
			editMsg.ParseMode = "html"
			if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
				logger.Error("Failed to edit message with size limit error", map[string]interface{}{
					"error": sendErr.Error(),
				})
				b.sendResponse(callback.Message.Chat.ID, errorMsg)
			}
			return nil
		} else if sizeErr == nil {
			// Near capacity but not completely full - allow for issues but warn
			logger.Debug("Repository near capacity but allowing GitHub issue creation", map[string]interface{}{
				"percentage": actualPercentage,
				"chat_id":    callback.Message.Chat.ID,
			})
		}
	}

	// Get user-specific LLM client
	userLLMClient, isUsingDefaultLLM := b.getUserLLMClientWithUsageTracking(callback.Message.Chat.ID, content)

	// Continue progress tracking for LLM processing
	b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 40, "🧠 LLM processing...")

	// Process with LLM to get title and hashtags (if available)
	var title, tags string
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
	_ = tags

	// Show issue creation status with progress
	b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 70, "❓ Creating GitHub issue...")

	// Create GitHub issue
	logger.Info("Attempting to create GitHub issue", map[string]interface{}{
		"title":   title,
		"chat_id": callback.Message.Chat.ID,
	})
	issueURL, issueNumber, err := userGitHubProvider.CreateIssue(title, content)
	if err != nil {
		logger.Error("Failed to create GitHub issue", map[string]interface{}{
			"error":   err.Error(),
			"title":   title,
			"content": content,
		})
		// Update the message to show file save
		failedMsg := "⚠️ Issue creation failed"
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, failedMsg)
		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			logger.Error("Failed to edit message", map[string]interface{}{
				"error": err.Error(),
			})
			b.sendResponse(callback.Message.Chat.ID, failedMsg)
		}
		return nil
	}

	// Increment issue count for successful issue creation
	if b.db != nil {
		if err := b.db.IncrementIssueCount(callback.Message.Chat.ID); err != nil {
			logger.Error("Failed to increment issue count", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
		}
		// Also increment usage count for current period tracking
		if err := b.db.IncrementUsageIssueCount(callback.Message.Chat.ID); err != nil {
			logger.Error("Failed to increment usage issue count", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
		}
	}

	// Create markdown format for issue.md with status tracking
	// Get repo info for the link format
	owner, repo, err := userGitHubProvider.GetRepoInfo()
	var linkContent string
	if err != nil {
		logger.Error("Failed to get repo info", map[string]interface{}{
			"error": err.Error(),
		})
		// Fallback to simple format
		linkContent = fmt.Sprintf("- 🟢 [%s](#%d)\n", title, issueNumber)
	} else {
		linkContent = fmt.Sprintf("- 🟢 %s/%s#%d [%s]\n", owner, repo, issueNumber, title)
	}

	// Save the link to issue.md with custom committer info
	// Get file lock manager and acquire lock before writing
	flm := github.GetFileLockManager()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Get user ID and repository URL for locking
	userID, err := b.getUserIDForLocking(callback.Message.Chat.ID)
	if err != nil {
		logger.Error("Failed to get user ID for locking during issue creation", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		// Continue without locking for backward compatibility
	} else {
		repoURL, err := b.getRepositoryURL(callback.Message.Chat.ID)
		if err != nil {
			logger.Error("Failed to get repository URL for locking during issue creation", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
			// Continue without locking for backward compatibility
		} else {
			// Acquire lock for issue.md
			issueHandle, err := flm.AcquireFileLock(ctx, userID, repoURL, "issue.md", true)
			if err != nil {
				logger.Error("Failed to acquire lock for issue.md during creation", map[string]interface{}{
					"error":   err.Error(),
					"chat_id": callback.Message.Chat.ID,
				})
				// Continue without locking for backward compatibility
			} else {
				defer issueHandle.Release()
				logger.Debug("Acquired file lock for issue creation operation", map[string]interface{}{
					"chat_id": callback.Message.Chat.ID,
					"file":    "issue.md",
				})
			}
		}
	}

	commitMsg := fmt.Sprintf("Add issue link: %s to issue.md via Telegram", title)
	committerInfo := b.getCommitterInfo(callback.Message.Chat.ID)
	premiumLevel = b.getPremiumLevel(callback.Message.Chat.ID)
	if err := userGitHubProvider.CommitFileWithAuthorAndPremium("issue.md", linkContent, commitMsg, committerInfo, premiumLevel); err != nil {
		logger.Error("Failed to save issue link", map[string]interface{}{
			"error": err.Error(),
		})
	}

	// Update repo size after saving issue link
	if b.db != nil {
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
			logger.Warn("Failed to get repo size for update after issue creation", map[string]interface{}{
				"error":   sizeErr.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
		}
	}

	// Update the message to show success with issue management buttons
	successMsg := fmt.Sprintf("✅ Issue created: #%d", issueNumber)

	// Create inline keyboard with issue link, comment, and close buttons
	row := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonURL(fmt.Sprintf("🔗 #%d", issueNumber), issueURL),
		tgbotapi.NewInlineKeyboardButtonData("💬", fmt.Sprintf("issue_comment_%d", issueNumber)),
		tgbotapi.NewInlineKeyboardButtonData("✅", fmt.Sprintf("issue_close_%d", issueNumber)),
	)
	keyboard := tgbotapi.NewInlineKeyboardMarkup(row)

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, successMsg)
	editMsg.ReplyMarkup = &keyboard
	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		logger.Error("Failed to edit message", map[string]interface{}{
			"error": err.Error(),
		})
		// Fallback: send new message
		b.sendResponse(callback.Message.Chat.ID, successMsg)
	}

	return nil
}

func (b *Bot) handlePhotoIssueCreation(callback *tgbotapi.CallbackQuery, messageKey string) error {
	// Retrieve the original message content, ID, and photo URL
	messageData, exists := b.pendingMessages[messageKey]
	if !exists {
		return fmt.Errorf("original message not found")
	}

	// Parse the stored data (content|||DELIM|||messageID|||DELIM|||photoURL|||DELIM|||imageDataBase64)
	dataParts := strings.SplitN(messageData, "|||DELIM|||", 4)
	if len(dataParts) != 4 {
		return fmt.Errorf("invalid message data format")
	}

	content := dataParts[0]
	originalMessageIDStr := dataParts[1]
	photoURL := dataParts[2]
	imageDataBase64 := dataParts[3]

	// Convert message ID back to int
	originalMessageID, err := strconv.Atoi(originalMessageIDStr)
	if err != nil {
		logger.Warn("Failed to parse message ID, using 0", map[string]interface{}{
			"error": err.Error(),
		})
		originalMessageID = 0
	}
	_ = originalMessageID

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

	// Get user-specific GitHub manager
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

	// Start progress tracking for photo issue creation
	premiumLevel := b.getPremiumLevel(callback.Message.Chat.ID)

	// Check issue creation limits
	if b.db != nil {
		canCreate, currentCount, limit, err := b.db.CheckUsageIssueLimit(callback.Message.Chat.ID, premiumLevel)
		if err != nil {
			logger.Error("Failed to check issue limit for photo", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
		} else if !canCreate {
			// User has reached their issue limit
			tierNames := map[int]string{0: "free", 1: "☕ Coffee", 2: "🍰 Cake", 3: "🎁 Sponsor"}
			currentTier := tierNames[premiumLevel]

			var upgradeMsg string
			if premiumLevel < 3 {
				nextLimit := database.GetIssueLimit(premiumLevel + 1)
				upgradeMsg = fmt.Sprintf(IssueLimitUpgradeTemplate, nextLimit)
			}

			errorMsg := fmt.Sprintf(`🚫 <b>Issue creation limit reached</b>

You've used %d/%d issues on the %s tier.%s

<i>You can still view and manage existing issues with /issue command</i>`, currentCount, limit, currentTier, upgradeMsg)

			editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
			editMsg.ParseMode = "html"
			if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
				logger.Error("Failed to edit message with photo issue limit error", map[string]interface{}{
					"error": sendErr.Error(),
				})
				b.sendResponse(callback.Message.Chat.ID, fmt.Sprintf("❌ Issue creation limit reached: %d/%d", currentCount, limit))
			}
			return nil
		}
	}

	// Step 1: Ensure repository exists (with double confirmation if cloning needed)
	// Show appropriate progress message based on whether repo needs cloning
	b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 30, "📊 Checking repository capacity...")
	if b.needsRepositoryClone(userGitHubProvider) {
		// Ensure repository with premium-aware cloning (includes double confirmation)
		if err := userGitHubProvider.EnsureRepositoryWithPremium(premiumLevel); err != nil {
			logger.Error("Failed to ensure repository", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
			errorMsg := b.formatRepositorySetupError(err, "create photo GitHub issues")
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

	isNearCapacity, _, err := userGitHubProvider.IsRepositoryNearCapacityWithPremium(premiumLevel)
	if err != nil {
		logger.Warn("Failed to check repository capacity, proceeding with photo issue creation", map[string]interface{}{
			"error": err.Error(),
		})
	} else if isNearCapacity {
		// Repository is near/at capacity - reject photo issue creation
		// Issues use 100% threshold instead of 97% since they're creating GitHub issues, not local files
		sizeMB, actualPercentage, sizeErr := userGitHubProvider.GetRepositorySizeInfoWithPremium(premiumLevel)
		if sizeErr == nil && actualPercentage >= 100 {
			maxSizeMB := userGitHubProvider.GetRepositoryMaxSizeWithPremium(premiumLevel)
			errorMsg := fmt.Sprintf(RepoCapacityIssueTemplate, actualPercentage, sizeMB, maxSizeMB)

			editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
			editMsg.ParseMode = "html"
			if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
				logger.Error("Failed to edit message with size limit error", map[string]interface{}{
					"error": sendErr.Error(),
				})
				b.sendResponse(callback.Message.Chat.ID, errorMsg)
			}
			return nil
		} else if sizeErr == nil {
			// Near capacity but not completely full - allow for issues but warn
			logger.Debug("Repository near capacity but allowing photo GitHub issue creation", map[string]interface{}{
				"percentage": actualPercentage,
				"chat_id":    callback.Message.Chat.ID,
			})
		}
	}

	// Process title and tags based on content type
	var title, tags string

	if strings.HasPrefix(content, "Photo: ") {
		// No caption case - try multimodal analysis if supported
		user, err := b.ensureUser(callback.Message)
		if err == nil && b.shouldPerformMultimodalAnalysis(callback.Message.Chat.ID, user) {
			// Show processing status with progress
			b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 50, "🔄 Analyzing photo...")
			
			// Decode image data from base64
			imageData, err := base64.StdEncoding.DecodeString(imageDataBase64)
			if err != nil {
				logger.Warn("Failed to decode image data for issue analysis", map[string]interface{}{
					"error": err.Error(),
				})
				// Fallback to simple title
				title = "New Photo"
				tags = ""
			} else {
				// Get user's LLM client for multimodal analysis
				userLLMClient, isUsingDefaultLLM := b.getUserLLMClientWithUsageTracking(callback.Message.Chat.ID, "")
				if userLLMClient != nil && userLLMClient.SupportsMultimodal() {
					// Perform multimodal analysis
					analysisResult, usage, err := userLLMClient.ProcessImageWithMessage(imageData, "")
					if err != nil {
						logger.Warn("Multimodal analysis failed for issue", map[string]interface{}{
							"error": err.Error(),
							"chat_id": callback.Message.Chat.ID,
						})
						// Fallback to simple title
						title = "New Photo"
						tags = ""
					} else {
						// Parse analysis result for title and tags
						title, tags = b.parseTitleAndTags(analysisResult, "Photo")
						logger.Info("Multimodal analysis completed for photo issue", map[string]interface{}{
							"title": title,
							"tags": tags,
							"chat_id": callback.Message.Chat.ID,
							"analysis": analysisResult,
						})

						// Record token usage in database based on LLM type
						if usage != nil && b.db != nil {
							if isUsingDefaultLLM {
								// Default LLM: record in both user_insights and user_usage
								if err := b.db.IncrementTokenUsageAll(callback.Message.Chat.ID, int64(usage.PromptTokens), int64(usage.CompletionTokens)); err != nil {
									logger.Warn("Failed to record token usage (default LLM)", map[string]interface{}{
										"error": err.Error(),
										"chat_id": callback.Message.Chat.ID,
										"prompt_tokens": usage.PromptTokens,
										"completion_tokens": usage.CompletionTokens,
									})
								}
							} else {
								// Personal LLM: record only in user_insights
								if err := b.db.IncrementTokenUsageInsights(callback.Message.Chat.ID, int64(usage.PromptTokens), int64(usage.CompletionTokens)); err != nil {
									logger.Warn("Failed to record token usage (personal LLM)", map[string]interface{}{
										"error": err.Error(),
										"chat_id": callback.Message.Chat.ID,
										"prompt_tokens": usage.PromptTokens,
										"completion_tokens": usage.CompletionTokens,
									})
								}
							}
						}
					}
				} else {
					// No multimodal support, use simple title
					title = "New Photo"
					tags = ""
				}
			}
		} else {
			// No multimodal analysis available, use simple title
			title = "New Photo"
			tags = ""
		}
	} else {
		// With caption case - use LLM processing
		userLLMClient, isUsingDefaultLLM := b.getUserLLMClientWithUsageTracking(callback.Message.Chat.ID, content)

		// Start progress tracking for photo issue creation
		b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 50, "🧠 LLM processing...")

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
	}
	_ = tags

	// Show issue creation status with progress
	b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 80, "❓ Creating GitHub issue with photo...")

	// Create content with photo reference for issue using CDN URL
	var issueContent string
	if strings.HasPrefix(content, "Photo: ") {
		// No caption case - just show the photo
		issueContent = fmt.Sprintf("![Photo](%s)", photoURL)
	} else {
		// With caption case
		issueContent = fmt.Sprintf("![Photo](%s)\n\n%s", photoURL, content)
	}

	// Create GitHub issue
	logger.Info("Attempting to create GitHub issue with photo", map[string]interface{}{
		"title":   title,
		"chat_id": callback.Message.Chat.ID,
	})
	issueURL, issueNumber, err := userGitHubProvider.CreateIssue(title, issueContent)
	if err != nil {
		logger.Error("Failed to create GitHub issue", map[string]interface{}{
			"error":   err.Error(),
			"title":   title,
			"content": issueContent,
		})
		failMsg := "⚠️ Issue creation failed"
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, failMsg)
		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			logger.Error("Failed to edit message", map[string]interface{}{
				"error": err.Error(),
			})
			b.sendResponse(callback.Message.Chat.ID, failMsg)
		}
		return nil
	}

	// Increment issue count for successful photo issue creation
	if b.db != nil {
		if err := b.db.IncrementIssueCount(callback.Message.Chat.ID); err != nil {
			logger.Error("Failed to increment issue count for photo", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
		}
		// Also increment usage count for current period tracking
		if err := b.db.IncrementUsageIssueCount(callback.Message.Chat.ID); err != nil {
			logger.Error("Failed to increment usage issue count for photo", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
		}
	}

	// Create markdown format for issue.md with status tracking
	// Get repo info for the link format
	owner, repo, repoErr := userGitHubProvider.GetRepoInfo()
	var linkContent string
	if repoErr != nil {
		logger.Error("Failed to get repo info", map[string]interface{}{
			"error": repoErr.Error(),
		})
		// Fallback to simple format
		linkContent = fmt.Sprintf("- 🟢 [%s](#%d)\n", title, issueNumber)
	} else {
		linkContent = fmt.Sprintf("- 🟢 %s/%s#%d [%s]\n", owner, repo, issueNumber, title)
	}

	// Save the link to issue.md with custom committer info
	// Get file lock manager and acquire lock before writing
	flm := github.GetFileLockManager()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Get user ID and repository URL for locking
	userID, err := b.getUserIDForLocking(callback.Message.Chat.ID)
	if err != nil {
		logger.Error("Failed to get user ID for locking during photo issue creation", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		// Continue without locking for backward compatibility
	} else {
		repoURL, err := b.getRepositoryURL(callback.Message.Chat.ID)
		if err != nil {
			logger.Error("Failed to get repository URL for locking during photo issue creation", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
			// Continue without locking for backward compatibility
		} else {
			// Acquire lock for issue.md
			issueHandle, err := flm.AcquireFileLock(ctx, userID, repoURL, "issue.md", true)
			if err != nil {
				logger.Error("Failed to acquire lock for issue.md during photo issue creation", map[string]interface{}{
					"error":   err.Error(),
					"chat_id": callback.Message.Chat.ID,
				})
				// Continue without locking for backward compatibility
			} else {
				defer issueHandle.Release()
				logger.Debug("Acquired file lock for photo issue creation operation", map[string]interface{}{
					"chat_id": callback.Message.Chat.ID,
					"file":    "issue.md",
				})
			}
		}
	}

	commitMsg := fmt.Sprintf("Add photo issue link: %s to issue.md via Telegram", title)
	committerInfo := b.getCommitterInfo(callback.Message.Chat.ID)
	premiumLevel = b.getPremiumLevel(callback.Message.Chat.ID)
	if err := userGitHubProvider.CommitFileWithAuthorAndPremium("issue.md", linkContent, commitMsg, committerInfo, premiumLevel); err != nil {
		logger.Error("Failed to save issue link", map[string]interface{}{
			"error": err.Error(),
		})
	}

	// Update repo size after saving photo issue link
	if b.db != nil {
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
			logger.Warn("Failed to get repo size for update after photo issue creation", map[string]interface{}{
				"error":   sizeErr.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
		}
	}

	// Update the message to show success with issue management buttons
	successMsg := fmt.Sprintf("✅ Photo issue created: #%d", issueNumber)

	// Create inline keyboard with issue link, comment, and close buttons
	row := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonURL(fmt.Sprintf("🔗 #%d", issueNumber), issueURL),
		tgbotapi.NewInlineKeyboardButtonData("💬", fmt.Sprintf("issue_comment_%d", issueNumber)),
		tgbotapi.NewInlineKeyboardButtonData("✅", fmt.Sprintf("issue_close_%d", issueNumber)),
	)
	keyboard := tgbotapi.NewInlineKeyboardMarkup(row)

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, successMsg)
	editMsg.ReplyMarkup = &keyboard
	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		logger.Error("Failed to edit message", map[string]interface{}{
			"error": err.Error(),
		})
		// Fallback: send new message
		b.sendResponse(callback.Message.Chat.ID, successMsg)
	}

	return nil
}
