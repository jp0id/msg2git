package telegram

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/msg2git/msg2git/internal/consts"
	"github.com/msg2git/msg2git/internal/logger"
)

// Photo-related callback handlers

func (b *Bot) handlePhotoFileSelection(callback *tgbotapi.CallbackQuery) error {
	parts := strings.SplitN(callback.Data, "_", 3)
	if len(parts) != 3 {
		return fmt.Errorf("invalid callback data format")
	}

	fileType := strings.ToLower(parts[1])
	messageKey := parts[2]

	// Handle PINNED type specially (format: photo_PINNED_index_messageKey)
	if fileType == "pinned" {
		return b.handlePhotoPinnedFileSelection(callback)
	}

	// Handle ISSUE type specially
	if fileType == "issue" {
		return b.handlePhotoIssueCreation(callback, messageKey)
	}

	// Handle CUSTOM type specially
	if fileType == "custom" {
		return b.handlePhotoCustomFileSelection(callback, messageKey)
	}

	filename := fileType + ".md"

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

	// Get user-specific LLM client
	userLLMClient, isUsingDefaultLLM := b.getUserLLMClientWithUsageTracking(callback.Message.Chat.ID, content)

	var formattedContent string
	var title string

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

		// For photos, include the photo URL in the TODO content
		var todoContent string
		if strings.HasPrefix(content, "Photo: ") {
			// No caption case - just use photo reference
			todoContent = fmt.Sprintf("![Photo](%s)", photoURL)
		} else {
			// With caption case - include photo + caption
			todoContent = fmt.Sprintf("![Photo](%s) %s", photoURL, content)
		}

		formattedContent = b.formatTodoContent(todoContent, originalMessageID, callback.Message.Chat.ID)
		title = "todo"
	} else {
		// Create content with photo reference
		var photoContent string
		var tags string

		if strings.HasPrefix(content, "Photo: ") {
			// No caption case - try multimodal analysis if supported
			user, err := b.ensureUser(callback.Message)
			if err == nil && b.shouldPerformMultimodalAnalysis(callback.Message.Chat.ID, user) {
				// Show processing status with progress
				b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 30, "🔄 Analyzing photo...")
				
				// Decode image data from base64
				imageData, err := base64.StdEncoding.DecodeString(imageDataBase64)
				if err != nil {
					logger.Warn("Failed to decode image data for analysis", map[string]interface{}{
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
							logger.Warn("Multimodal analysis failed", map[string]interface{}{
								"error": err.Error(),
								"chat_id": callback.Message.Chat.ID,
							})
							// Fallback to simple title
							title = "New Photo"
							tags = ""
						} else {
							// Parse analysis result for title and tags
							title, tags = b.parseTitleAndTags(analysisResult, "Photo")
							logger.Info("Multimodal analysis completed for photo without caption", map[string]interface{}{
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
			photoContent = fmt.Sprintf("![Photo](%s)", photoURL)

			// Show processing status with progress
			b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 50, "🔄 Processing photo...")
		} else {
			// Step 1: Ensure repository exists (with double confirmation if cloning needed)
			premiumLevel := b.getPremiumLevel(callback.Message.Chat.ID)

			// Show appropriate progress message based on whether repo needs cloning
			if b.needsRepositoryClone(userGitHubProvider) {
				b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 30, "📊 Checking remote repository size...")

				// Ensure repository with premium-aware cloning (includes double confirmation)
				if err := userGitHubProvider.EnsureRepositoryWithPremium(premiumLevel); err != nil {
					logger.Error("Failed to ensure repository", map[string]interface{}{
						"error":   err.Error(),
						"chat_id": callback.Message.Chat.ID,
					})
					errorMsg := b.formatRepositorySetupError(err, "save photo content")
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

			// Step 2: With caption case - use LLM processing
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

			photoContent = fmt.Sprintf("![Photo](%s)\n\n%s", photoURL, content)
		}

		formattedContent = b.formatMessageContentWithTitleAndTags(photoContent, filename, originalMessageID, callback.Message.Chat.ID, title, tags)
	}

	// Show GitHub commit status with progress
	b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 70, "📝 Saving photo reference to GitHub...")

	// Commit to GitHub with custom committer info
	var commitMsg string
	if strings.HasPrefix(content, "Photo: ") {
		commitMsg = fmt.Sprintf("Add photo reference: %s to %s via Telegram", title, filename)
	} else {
		commitMsg = fmt.Sprintf("Add photo with caption: %s to %s via Telegram", title, filename)
	}
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
		errorMsg := fmt.Sprintf("❌ Failed to save photo: %v", err)
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
			logger.Error("Failed to edit message", map[string]interface{}{
				"error": sendErr.Error(),
			})
			b.sendResponse(callback.Message.Chat.ID, errorMsg)
		}
		return nil // Don't return error to avoid double error handling
	}

	// Increment commit count and update repo size after successful photo commit
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
		}
	}

	// Update the message to show success with GitHub menu button
	githubURL, urlErr := userGitHubProvider.GetGitHubFileURLWithBranch(filename)
	var successMsg string
	if strings.HasPrefix(content, "Photo: ") {
		successMsg = fmt.Sprintf("✅ Photo reference saved to %s", strings.ToUpper(parts[1]))
	} else {
		successMsg = fmt.Sprintf("✅ Photo and caption saved to %s", strings.ToUpper(parts[1]))
	}

	// Create inline keyboard with GitHub link button
	var keyboard *tgbotapi.InlineKeyboardMarkup
	if urlErr != nil {
		logger.Warn("Failed to generate GitHub file URL", map[string]interface{}{
			"error":    urlErr.Error(),
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

// handlePhotoCustomFileSelection handles custom file selection for photo messages
func (b *Bot) handlePhotoCustomFileSelection(callback *tgbotapi.CallbackQuery, messageKey string) error {
	logger.Info("Handling photo custom file selection", map[string]interface{}{
		"message_key": messageKey,
		"chat_id":     callback.Message.Chat.ID,
	})

	// Get user and their custom files
	user, err := b.ensureUser(callback.Message)
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

	// Show custom file selection interface
	return b.showCustomFileSelectionInterface(callback, messageKey, user, true)
}

// handlePhotoCustomFileChoice handles selection of an existing custom file for photo messages
func (b *Bot) handlePhotoCustomFileChoice(callback *tgbotapi.CallbackQuery) error {
	// Parse callback data: photo_custom_file_{messageKey}_{fileIndex}
	// Remove the "photo_custom_file_" prefix first
	prefix := "photo_custom_file_"
	if !strings.HasPrefix(callback.Data, prefix) {
		return fmt.Errorf("invalid photo custom file callback data format")
	}

	remainder := strings.TrimPrefix(callback.Data, prefix)

	// Find the last underscore to separate messageKey from fileIndex
	lastUnderscoreIndex := strings.LastIndex(remainder, "_")
	if lastUnderscoreIndex == -1 {
		return fmt.Errorf("invalid photo custom file callback data: missing file index")
	}

	messageKey := remainder[:lastUnderscoreIndex]
	fileIndexStr := remainder[lastUnderscoreIndex+1:]

	fileIndex, err := strconv.Atoi(fileIndexStr)
	if err != nil {
		return fmt.Errorf("invalid file index: %w", err)
	}

	logger.Debug("Parsed photo custom file selection", map[string]interface{}{
		"callback_data": callback.Data,
		"message_key":   messageKey,
		"file_index":    fileIndex,
		"chat_id":       callback.Message.Chat.ID,
	})

	err = b.processCustomFileSelection(callback, messageKey, fileIndex, true)
	if err != nil {
		logger.Error("Failed to process photo custom file selection", map[string]interface{}{
			"error":         err.Error(),
			"callback_data": callback.Data,
			"message_key":   messageKey,
			"file_index":    fileIndex,
			"chat_id":       callback.Message.Chat.ID,
		})
	}
	return err
}

// handlePhotoAddCustomFile handles adding a new custom file for photo messages
func (b *Bot) handlePhotoAddCustomFile(callback *tgbotapi.CallbackQuery) error {
	messageKey := strings.TrimPrefix(callback.Data, "photo_add_custom_")
	return b.showAddCustomFilePrompt(callback, messageKey, true)
}

// handlePhotoCustomRemoveFile handles remove file button from CUSTOM interface (photo messages)
func (b *Bot) handlePhotoCustomRemoveFile(callback *tgbotapi.CallbackQuery) error {
	messageKey := strings.TrimPrefix(callback.Data, "photo_remove_custom_")
	return b.showCustomRemoveFileInterface(callback, messageKey, true)
}

// handlePhotoPinnedFileSelection handles selection of pinned custom files for photo messages
func (b *Bot) handlePhotoPinnedFileSelection(callback *tgbotapi.CallbackQuery) error {
	// Parse callback data: photo_PINNED_index_messageKey
	parts := strings.SplitN(callback.Data, "_", 4)
	if len(parts) != 4 {
		return fmt.Errorf("invalid photo pinned file callback data format")
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

	// Update the progress message
	progressMsg := fmt.Sprintf("📌 Saving photo to pinned file: %s", selectedFile)
	b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 0, progressMsg)

	// Get user-specific GitHub manager
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
	if b.needsRepositoryClone(userGitHubProvider) {
		// Check repository capacity
		b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 30, "📊 Checking repository capacity...")

		// Ensure repository with premium-aware cloning
		if err := userGitHubProvider.EnsureRepositoryWithPremium(premiumLevel); err != nil {
			logger.Error("Failed to ensure repository", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
			errorMsg := b.formatRepositorySetupError(err, "save photo")
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
	
	// Check if this is a photo without caption and multimodal analysis is supported
	if strings.HasPrefix(content, "Photo: ") && b.shouldPerformMultimodalAnalysis(callback.Message.Chat.ID, user) {
		// Decode image data from base64
		imageData, err := base64.StdEncoding.DecodeString(imageDataBase64)
		if err != nil {
			logger.Warn("Failed to decode image data for analysis", map[string]interface{}{
				"error": err.Error(),
			})
			// Fallback to simple title
			title = "New Photo"
			tags = ""
		} else {
			// Get user's LLM client for multimodal analysis
			if userLLMClient != nil && userLLMClient.SupportsMultimodal() {
				// Perform multimodal analysis
				analysisResult, usage, err := userLLMClient.ProcessImageWithMessage(imageData, "")
				if err != nil {
					logger.Warn("Multimodal analysis failed", map[string]interface{}{
						"error": err.Error(),
						"chat_id": callback.Message.Chat.ID,
					})
					// Fallback to simple title
					title = "New Photo"
					tags = ""
				} else {
					// Parse analysis result for title and tags
					title, tags = b.parseTitleAndTags(analysisResult, "Photo")
					logger.Info("Multimodal analysis completed for pinned photo without caption", map[string]interface{}{
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
	} else if userLLMClient != nil {
		// Regular LLM processing for photos with captions or text content
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

	// Format content with photo URL
	var photoContent string
	if strings.HasPrefix(content, "Photo: ") {
		// No caption, just photo reference
		photoContent = fmt.Sprintf("![Photo](%s)", photoURL)
	} else {
		// Photo with caption
		photoContent = fmt.Sprintf("![Photo](%s)\n\n%s", photoURL, content)
	}

	formattedContent = b.formatMessageContentWithTitleAndTags(photoContent, selectedFile, originalMessageID, callback.Message.Chat.ID, title, tags)

	// Show GitHub commit status with progress
	b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 80, "📝 Saving to GitHub...")

	// Commit to GitHub with custom committer info and premium level
	commitMsg := fmt.Sprintf("Add photo %s to %s via Telegram", title, selectedFile)
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
		errorMsg := "❌ Failed to save photo to GitHub: " + err.Error()
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
			b.sendResponse(callback.Message.Chat.ID, errorMsg)
		}
		return nil
	}

	// Clean up pending message
	delete(b.pendingMessages, messageKey)

	// Increment image and commit count
	if b.db != nil {
		if err := b.db.IncrementImageCount(callback.Message.Chat.ID); err != nil {
			logger.Error("Failed to increment image count", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
		}

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
		}
	}

	// Success message with GitHub link
	githubURL, err := userGitHubProvider.GetGitHubFileURLWithBranch(selectedFile)
	successMsg := fmt.Sprintf("✅ Photo saved to pinned file: %s", selectedFile)

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

	return nil
}
