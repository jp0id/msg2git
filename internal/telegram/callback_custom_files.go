package telegram

import (
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/msg2git/msg2git/internal/consts"
	"github.com/msg2git/msg2git/internal/database"
	"github.com/msg2git/msg2git/internal/logger"
)

// Custom file callback query handlers for inline keyboard interactions

// handleCustomFileSelection handles custom file selection for regular messages
func (b *Bot) handleCustomFileSelection(callback *tgbotapi.CallbackQuery, messageKey string) error {
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
	return b.showCustomFileSelectionInterface(callback, messageKey, user, false)
}

// handleCustomFileChoice handles selection of an existing custom file for regular messages
func (b *Bot) handleCustomFileChoice(callback *tgbotapi.CallbackQuery) error {
	// Parse callback data: custom_file_{messageKey}_{fileIndex}
	// Remove the "custom_file_" prefix first
	prefix := "custom_file_"
	if !strings.HasPrefix(callback.Data, prefix) {
		return fmt.Errorf("invalid custom file callback data format")
	}

	remainder := strings.TrimPrefix(callback.Data, prefix)

	// Find the last underscore to separate messageKey from fileIndex
	lastUnderscoreIndex := strings.LastIndex(remainder, "_")
	if lastUnderscoreIndex == -1 {
		return fmt.Errorf("invalid custom file callback data: missing file index")
	}

	messageKey := remainder[:lastUnderscoreIndex]
	fileIndexStr := remainder[lastUnderscoreIndex+1:]

	fileIndex, err := strconv.Atoi(fileIndexStr)
	if err != nil {
		return fmt.Errorf("invalid file index: %w", err)
	}

	logger.Debug("Parsed custom file selection", map[string]interface{}{
		"callback_data": callback.Data,
		"message_key":   messageKey,
		"file_index":    fileIndex,
		"chat_id":       callback.Message.Chat.ID,
	})

	err = b.processCustomFileSelection(callback, messageKey, fileIndex, false)
	if err != nil {
		logger.Error("Failed to process custom file selection", map[string]interface{}{
			"error":         err.Error(),
			"callback_data": callback.Data,
			"message_key":   messageKey,
			"file_index":    fileIndex,
			"chat_id":       callback.Message.Chat.ID,
		})
	}
	return err
}

// handleBackToFiles handles the back button to return to main file selection
func (b *Bot) handleBackToFiles(callback *tgbotapi.CallbackQuery) error {
	// Parse callback data: back_to_files_{messageKey}
	messageKey := strings.TrimPrefix(callback.Data, "back_to_files_")

	// Recreate the original file selection interface
	messageData, exists := b.pendingMessages[messageKey]
	if !exists {
		return fmt.Errorf("original message not found")
	}

	// Check if it's a photo message by looking at the data format
	isPhoto := strings.Count(messageData, "|") == 2 // photo messages have 3 parts: content|messageID|photoURL

	var promptText string
	var buttons [][]tgbotapi.InlineKeyboardButton

	if isPhoto {
		promptText = "Please choose where to save the photo reference:"

		// Recreate photo file selection buttons
		row1 := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📝 NOTE", fmt.Sprintf("photo_NOTE_%s", messageKey)),
			tgbotapi.NewInlineKeyboardButtonData("❓ ISSUE", fmt.Sprintf("photo_ISSUE_%s", messageKey)),
		)
		dataParts := strings.SplitN(messageData, "|||DELIM|||", 3)
		if len(dataParts) >= 1 && !strings.Contains(dataParts[0], "\n") {
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
		buttons = [][]tgbotapi.InlineKeyboardButton{row1, row2, row3}
	} else {
		promptText = "Please choose a location:"

		// Recreate file selection buttons with correct prefix based on message type
		var prefix string
		if isPhoto {
			prefix = "photo_"
		} else {
			prefix = "file_"
		}

		row1 := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📝 NOTE", fmt.Sprintf("%sNOTE_%s", prefix, messageKey)),
			tgbotapi.NewInlineKeyboardButtonData("❓ ISSUE", fmt.Sprintf("%sISSUE_%s", prefix, messageKey)),
		)
		dataParts := strings.SplitN(messageData, "|||DELIM|||", 2)
		if len(dataParts) >= 1 && !strings.Contains(dataParts[0], "\n") {
			row1 = append(row1, tgbotapi.NewInlineKeyboardButtonData("✅ TODO", fmt.Sprintf("%sTODO_%s", prefix, messageKey)))
		}
		row2 := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💡 IDEA", fmt.Sprintf("%sIDEA_%s", prefix, messageKey)),
			tgbotapi.NewInlineKeyboardButtonData("📥 INBOX", fmt.Sprintf("%sINBOX_%s", prefix, messageKey)),
			tgbotapi.NewInlineKeyboardButtonData("🔧 TOOL", fmt.Sprintf("%sTOOL_%s", prefix, messageKey)),
		)

		// Add pinned custom files row if any exist (for non-photo messages)
		buttons = [][]tgbotapi.InlineKeyboardButton{row1, row2}

		if !isPhoto && b.db != nil {
			// Get user's pinned custom files (first 2 items in custom_files array)
			user, err := b.ensureUserFromCallback(callback)
			if err == nil && user != nil {
				customFiles := user.GetCustomFiles()
				// Take up to 2 pinned files (first 2 items in the array)
				pinnedCount := len(customFiles)
				if pinnedCount > 2 {
					pinnedCount = 2
				}

				if pinnedCount > 0 {
					pinnedRow := []tgbotapi.InlineKeyboardButton{}
					for i := 0; i < pinnedCount; i++ {
						filePath := customFiles[i]
						// Get display name (remove .md extension and truncate if needed)
						displayName := strings.TrimSuffix(filePath, ".md")
						if len(displayName) > 15 {
							displayName = displayName[:12] + "..."
						}
						pinnedRow = append(pinnedRow, tgbotapi.NewInlineKeyboardButtonData(
							fmt.Sprintf("📌 %s", displayName),
							fmt.Sprintf("file_PINNED_%d_%s", i, messageKey),
						))
					}
					buttons = append(buttons, pinnedRow)
				}
			}
		}

		row3 := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📁 CUSTOM", fmt.Sprintf("%sCUSTOM_%s", prefix, messageKey)),
			tgbotapi.NewInlineKeyboardButtonData("❌ CANCEL", fmt.Sprintf("cancel_%s", messageKey)),
		)
		buttons = append(buttons, row3)
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, promptText)
	editMsg.ReplyMarkup = &keyboard

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		return fmt.Errorf("failed to show file selection: %w", err)
	}

	return nil
}

// handleAddCustomFile handles adding a new custom file for regular messages
func (b *Bot) handleAddCustomFile(callback *tgbotapi.CallbackQuery) error {
	messageKey := strings.TrimPrefix(callback.Data, "add_custom_")
	return b.showAddCustomFilePrompt(callback, messageKey, false)
}

// handleRemoveCustomFile handles removing a custom file from the user's list
func (b *Bot) handleRemoveCustomFile(callback *tgbotapi.CallbackQuery) error {
	// Parse callback data: remove_custom_file_{index} or remove_custom_file_{index}_{messageKey}
	remainder := strings.TrimPrefix(callback.Data, "remove_custom_file_")

	var fileIndex int
	var messageKey string
	var err error

	// Simple parsing: if it contains underscore, it has messageKey
	if strings.Contains(remainder, "_") {
		// Format: {index}_{messageKey}
		parts := strings.SplitN(remainder, "_", 2)
		fileIndex, err = strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("invalid file index: %w", err)
		}
		messageKey = parts[1]
	} else {
		// Format: {index} (from /customfile command)
		fileIndex, err = strconv.Atoi(remainder)
		if err != nil {
			return fmt.Errorf("invalid file index: %w", err)
		}
		messageKey = ""
	}

	// Get user
	user, err := b.ensureUserFromCallback(callback)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	if b.db == nil {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Custom files require database configuration.")
		return nil
	}

	// Get current custom files
	customFiles := user.GetCustomFiles()

	if fileIndex < 0 || fileIndex >= len(customFiles) {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Invalid file selection. Please try again.")
		return nil
	}

	fileToRemove := customFiles[fileIndex]

	// Remove the file from the list
	if err := user.RemoveCustomFile(fileToRemove); err != nil {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, fmt.Sprintf("❌ Failed to remove custom file: %v", err))
		return nil
	}

	// Update user in database
	if err := b.db.UpdateUserCustomFiles(callback.Message.Chat.ID, user.CustomFiles); err != nil {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, fmt.Sprintf("❌ Failed to save changes: %v", err))
		return nil
	}

	// Show success message and refresh the list
	successMsg := fmt.Sprintf("✅ Removed custom file: <code>%s</code>\n\n", fileToRemove)

	// Get updated file list
	updatedFiles := user.GetCustomFiles()

	if len(updatedFiles) == 0 {
		// Determine context based on whether messageKey was provided
		if messageKey != "" {
			// We're in message-saving workflow - show empty state with Add New File button
			var premiumLevel int = 0
			premiumUser, err := b.db.GetPremiumUser(callback.Message.Chat.ID)
			if err == nil && premiumUser != nil && premiumUser.IsPremiumUser() {
				premiumLevel = premiumUser.Level
			}

			customFileLimit := database.GetCustomFileLimit(premiumLevel)
			successMsg += fmt.Sprintf(`📁 <b>Custom Files (0/%d)</b>

All custom files removed. You can add a new custom file to save your message.

<i>Click 'Add New File' to create a new custom file for your message.</i>`, customFileLimit)

			// Create buttons for empty state with Back button
			var callbackData string
			// Check if it's a photo message by looking at the pending message data
			messageData, exists := b.pendingMessages[messageKey]
			isPhoto := false
			if exists {
				// Photo messages have 3 parts: content|messageID|photoURL
				isPhoto = strings.Count(messageData, "|") == 2
			}

			if isPhoto {
				callbackData = fmt.Sprintf("photo_add_custom_%s", messageKey)
			} else {
				callbackData = fmt.Sprintf("add_custom_%s", messageKey)
			}

			row1 := tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("➕ Add New File", callbackData),
			)
			row2 := tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🔙 Back", fmt.Sprintf("back_to_files_%s", messageKey)),
			)

			keyboard := tgbotapi.NewInlineKeyboardMarkup(row1, row2)

			editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, successMsg)
			editMsg.ParseMode = "HTML"
			editMsg.ReplyMarkup = &keyboard

			if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
				return fmt.Errorf("failed to edit message: %w", err)
			}
			return nil
		} else {
			// We're in standalone /customfile management - show empty state with Add New File button
			var premiumLevel int = 0
			premiumUser, err := b.db.GetPremiumUser(callback.Message.Chat.ID)
			if err == nil && premiumUser != nil && premiumUser.IsPremiumUser() {
				premiumLevel = premiumUser.Level
			}

			customFileLimit := database.GetCustomFileLimit(premiumLevel)
			tierNames := []string{"Free", "☕ Coffee", "🍰 Cake", "🎁 Sponsor"}
			currentTier := "Free"
			if premiumLevel < len(tierNames) {
				currentTier = tierNames[premiumLevel]
			}

			successMsg += fmt.Sprintf(`📂 <b>Custom Files (0/%d)</b>
<i>%s tier</i>

No custom files configured.

You can add custom files by:
1. Sending a message to the bot
2. Selecting the CUSTOM button
3. Adding a new file path

<i>Custom files allow you to organize messages into specific folders or projects.</i>`, customFileLimit, currentTier)

			// Create buttons for empty state
			row1 := tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("➕ Add New File", "customfile_add_new"),
			)
			row2 := tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Done", "customfile_done"),
			)

			keyboard := tgbotapi.NewInlineKeyboardMarkup(row1, row2)

			editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, successMsg)
			editMsg.ParseMode = "HTML"
			editMsg.ReplyMarkup = &keyboard

			if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
				return fmt.Errorf("failed to edit message: %w", err)
			}
			return nil
		}
	}

	// Determine context based on whether messageKey was provided
	if messageKey != "" {
		// We're in message-saving workflow - return to custom file selection interface
		successMsg += fmt.Sprintf(`📁 <b>Custom Files</b> (%d/%d)

File removed successfully. Choose a file to save your message:`, len(updatedFiles), database.GetCustomFileLimit(b.getPremiumLevel(callback.Message.Chat.ID)))

		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, successMsg)
		editMsg.ParseMode = "HTML"

		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			return fmt.Errorf("failed to edit message: %w", err)
		}

		// Check if it's a photo message by looking at the pending message data
		messageData, exists := b.pendingMessages[messageKey]
		isPhoto := false
		if exists {
			// Photo messages have 3 parts: content|messageID|photoURL
			isPhoto = strings.Count(messageData, "|") == 2
		}

		// Now show the updated custom file selection interface
		return b.showCustomFileSelectionInterface(callback, messageKey, user, isPhoto)
	} else {
		// We're in standalone /customfile management - refresh management interface
		return b.refreshCustomFilesList(callback, user)
	}
}

// handleShowAllCustomFiles handles showing all custom files when there are more than 10
func (b *Bot) handleShowAllCustomFiles(callback *tgbotapi.CallbackQuery) error {
	// Get user
	user, err := b.ensureUserFromCallback(callback)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	if b.db == nil {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Custom files require database configuration.")
		return nil
	}

	customFiles := user.GetCustomFiles()

	if len(customFiles) == 0 {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "📂 No custom files found.")
		return nil
	}

	// Build message text with all files
	var msgText strings.Builder
	msgText.WriteString(fmt.Sprintf("📂 <b>All Custom Files (%d)</b>\n\n", len(customFiles)))

	for i, filePath := range customFiles {
		msgText.WriteString(fmt.Sprintf("%d. <code>%s</code>\n", i+1, filePath))
	}

	msgText.WriteString("\n<i>Use /customfile to manage these files with removal options.</i>")

	// Create back button
	backButton := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔙 Back to Management", "refresh_custom_files"),
	)
	keyboard := tgbotapi.NewInlineKeyboardMarkup(backButton)

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, msgText.String())
	editMsg.ParseMode = "HTML"
	editMsg.ReplyMarkup = &keyboard

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		return fmt.Errorf("failed to edit message: %w", err)
	}

	return nil
}

// handleRefreshCustomFiles handles refreshing the custom files list
func (b *Bot) handleRefreshCustomFiles(callback *tgbotapi.CallbackQuery) error {
	// Get user
	user, err := b.ensureUserFromCallback(callback)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	if b.db == nil {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Custom files require database configuration.")
		return nil
	}

	return b.refreshCustomFilesList(callback, user)
}

// refreshCustomFilesList refreshes the custom files list display
func (b *Bot) refreshCustomFilesList(callback *tgbotapi.CallbackQuery, user *database.User) error {
	customFiles := user.GetCustomFiles()

	if len(customFiles) == 0 {
		// Get premium status for file limits
		var premiumLevel int = 0
		premiumUser, err := b.db.GetPremiumUser(callback.Message.Chat.ID)
		if err == nil && premiumUser != nil && premiumUser.IsPremiumUser() {
			premiumLevel = premiumUser.Level
		}

		customFileLimit := database.GetCustomFileLimit(premiumLevel)
		tierNames := []string{"Free", "☕ Coffee", "🍰 Cake", "🎁 Sponsor"}
		currentTier := "Free"
		if premiumLevel < len(tierNames) {
			currentTier = tierNames[premiumLevel]
		}

		noFilesMsg := fmt.Sprintf(`📂 <b>Custom Files (0/%d)</b>
<i>%s tier</i>

%s

<i>Custom files allow you to organize messages into specific folders or projects.</i>`, customFileLimit, currentTier, consts.NoCustomFilesConfigured)

		// Create buttons for empty state
		row1 := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Add New File", "customfile_add_new"),
		)
		row2 := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Done", "customfile_done"),
		)

		keyboard := tgbotapi.NewInlineKeyboardMarkup(row1, row2)

		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, noFilesMsg)
		editMsg.ParseMode = "HTML"
		editMsg.ReplyMarkup = &keyboard

		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			return fmt.Errorf("failed to edit message: %w", err)
		}
		return nil
	}

	// Get premium status for file limits
	var premiumLevel int = 0
	premiumUser, err := b.db.GetPremiumUser(callback.Message.Chat.ID)
	if err == nil && premiumUser != nil && premiumUser.IsPremiumUser() {
		premiumLevel = premiumUser.Level
	}

	customFileLimit := database.GetCustomFileLimit(premiumLevel)
	tierNames := []string{"Free", "☕ Coffee", "🍰 Cake", "🎁 Sponsor"}
	currentTier := "Free"
	if premiumLevel < len(tierNames) {
		currentTier = tierNames[premiumLevel]
	}

	// Build message text
	var msgText strings.Builder
	msgText.WriteString(fmt.Sprintf(`📂 <b>Custom Files (%d/%d)</b>
<i>%s tier</i>

`, len(customFiles), customFileLimit, currentTier))

	// Add each custom file with removal option
	for i, filePath := range customFiles {
		msgText.WriteString(fmt.Sprintf("%d. <code>%s</code>\n", i+1, filePath))
	}

	msgText.WriteString("\n<i>Choose an action below:</i>\n")
	msgText.WriteString("<i>💡 Note: Remove only removes from list, files remain in repository</i>")

	// Create action buttons
	row1 := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("➕ Add New File", "customfile_add_new"),
		tgbotapi.NewInlineKeyboardButtonData("🗑️ Remove File", "customfile_remove"),
	)
	row2 := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📌 Pin File", "customfile_pin"),
		tgbotapi.NewInlineKeyboardButtonData("✅ Done", "customfile_done"),
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(row1, row2)

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, msgText.String())
	editMsg.ParseMode = "HTML"
	if keyboard.InlineKeyboard != nil {
		editMsg.ReplyMarkup = &keyboard
	}

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		return fmt.Errorf("failed to edit message: %w", err)
	}

	return nil
}

// handleCustomFileAction handles the main action buttons for /customfile command
func (b *Bot) handleCustomFileAction(callback *tgbotapi.CallbackQuery) error {
	action := strings.TrimPrefix(callback.Data, "customfile_")

	switch action {
	case "add_new":
		return b.handleCustomFileAddNew(callback)
	case "remove":
		return b.handleCustomFileRemoveSelect(callback)
	case "pin":
		return b.handleCustomFilePinSelect(callback)
	case "back":
		user, err := b.ensureUserFromCallback(callback)
		if err != nil {
			return fmt.Errorf("failed to get user: %w", err)
		}
		return b.refreshCustomFilesList(callback, user)
	case "done":
		return b.handleCustomFileDone(callback)
	case "cancel": // Keep cancel for backward compatibility with existing flows
		return b.handleCustomFileDone(callback)
	default:
		return fmt.Errorf("unknown custom file action: %s", action)
	}
}

// handleCustomFileAddNew handles adding a new custom file
func (b *Bot) handleCustomFileAddNew(callback *tgbotapi.CallbackQuery) error {
	// Get user - use callback's From and Message.Chat
	user, err := b.ensureUserFromCallback(callback)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	if b.db == nil {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Custom files require database configuration.")
		return nil
	}

	// Check if user has reached custom file limit
	var premiumLevel int = 0
	premiumUser, err := b.db.GetPremiumUser(callback.Message.Chat.ID)
	if err == nil && premiumUser != nil && premiumUser.IsPremiumUser() {
		premiumLevel = premiumUser.Level
	}

	currentFiles := user.GetCustomFiles()
	customFileLimit := database.GetCustomFileLimit(premiumLevel)

	if len(currentFiles) >= customFileLimit {
		errorMsg := FormatCustomFileLimitMessage(premiumLevel, customFileLimit)
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		return nil
	}

	// Delete the old message first to make a clean transition (like existing implementation)
	deleteMsg := tgbotapi.NewDeleteMessage(callback.Message.Chat.ID, callback.Message.MessageID)
	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, deleteMsg); err != nil {
		logger.Error("Failed to delete old message before custom file setup", map[string]interface{}{
			"error": err.Error(),
		})
		// Don't return error - deletion failure is not critical, continue with new message
	}

	// Send a new message with ForceReply for better UX (like existing implementation)
	promptMsg := `📁 <b>Add New Custom File</b>

Reply to this message with a file path for your new custom file.

<b>Examples:</b>
• <code>projects/my-project.md</code>
• <code>work/meeting-notes.md</code>
• <code>personal/journal.md</code>
• <code>ideas/startup-ideas.md</code>

<i>The file will be created if it doesn't exist, or appended to if it does.</i>`

	msg := tgbotapi.NewMessage(callback.Message.Chat.ID, promptMsg)
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = tgbotapi.ForceReply{ForceReply: true, Selective: true}

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, msg); err != nil {
		return fmt.Errorf("failed to send custom file prompt: %w", err)
	}

	// Store state for reply handling (using the same format as existing implementation)
	stateKey := fmt.Sprintf("add_custom_%d", callback.Message.Chat.ID)
	stateData := fmt.Sprintf("customfile_standalone|||DELIM|||false") // Mark this as standalone customfile operation
	b.pendingMessages[stateKey] = stateData

	return nil
}

// handleCustomFileRemoveSelect shows file selection for removal
func (b *Bot) handleCustomFileRemoveSelect(callback *tgbotapi.CallbackQuery) error {
	// Get user
	user, err := b.ensureUserFromCallback(callback)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	if b.db == nil {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Custom files require database configuration.")
		return nil
	}

	customFiles := user.GetCustomFiles()

	if len(customFiles) == 0 {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ No custom files to remove.")
		return nil
	}

	// Build selection message
	var msgText strings.Builder
	msgText.WriteString("🗑️ <b>Remove Custom File</b>\n\n")
	msgText.WriteString("Select a file to remove:\n\n")

	for i, filePath := range customFiles {
		msgText.WriteString(fmt.Sprintf("%d. <code>%s</code>\n", i+1, filePath))
	}

	msgText.WriteString("\n<i>Click a button below to remove that file:</i>")

	// Create removal buttons (max 20 to fit in message)
	var keyboardRows [][]tgbotapi.InlineKeyboardButton
	maxButtons := len(customFiles)
	if maxButtons > 20 {
		maxButtons = 20
	}

	// Add files in rows of 2
	for i := 0; i < maxButtons; i += 2 {
		var row []tgbotapi.InlineKeyboardButton

		// First file in row
		buttonText := fmt.Sprintf("%d. %s", i+1, customFiles[i])
		if len(buttonText) > 30 {
			buttonText = fmt.Sprintf("%d. %s...", i+1, customFiles[i][:20])
		}
		row = append(row, tgbotapi.NewInlineKeyboardButtonData(buttonText, fmt.Sprintf("remove_custom_file_%d", i)))

		// Second file in row (if exists)
		if i+1 < maxButtons {
			buttonText2 := fmt.Sprintf("%d. %s", i+2, customFiles[i+1])
			if len(buttonText2) > 30 {
				buttonText2 = fmt.Sprintf("%d. %s...", i+2, customFiles[i+1][:20])
			}
			row = append(row, tgbotapi.NewInlineKeyboardButtonData(buttonText2, fmt.Sprintf("remove_custom_file_%d", i+1)))
		}

		keyboardRows = append(keyboardRows, row)
	}

	// Add "Show More" button if we have more than 20 files
	if len(customFiles) > 20 {
		moreButton := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("📋 Show All %d Files", len(customFiles)), "show_all_custom_files_remove"),
		)
		keyboardRows = append(keyboardRows, moreButton)
	}

	// Add back button
	backButton := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔙 Back", "refresh_custom_files"),
	)
	keyboardRows = append(keyboardRows, backButton)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, msgText.String())
	editMsg.ParseMode = "HTML"
	editMsg.ReplyMarkup = &keyboard

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		return fmt.Errorf("failed to edit message: %w", err)
	}

	return nil
}

// handleCustomFileDone closes the custom file management interface
func (b *Bot) handleCustomFileDone(callback *tgbotapi.CallbackQuery) error {
	// Clean up any pending state
	delete(b.pendingMessages, fmt.Sprintf("add_custom_file_%d", callback.Message.Chat.ID))

	doneMsg := "✅ Custom file management completed."
	b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, doneMsg)
	return nil
}

// handleCustomRemoveFile handles remove file button from CUSTOM interface (regular messages)
func (b *Bot) handleCustomRemoveFile(callback *tgbotapi.CallbackQuery) error {
	messageKey := strings.TrimPrefix(callback.Data, "remove_custom_")
	return b.showCustomRemoveFileInterface(callback, messageKey, false)
}

// showCustomRemoveFileInterface shows file selection interface for removal
func (b *Bot) showCustomRemoveFileInterface(callback *tgbotapi.CallbackQuery, messageKey string, isPhoto bool) error {
	// Get user
	user, err := b.ensureUserFromCallback(callback)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	if b.db == nil {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Custom files require database configuration.")
		return nil
	}

	customFiles := user.GetCustomFiles()

	if len(customFiles) == 0 {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ No custom files to remove.")
		return nil
	}

	// Build selection message
	var msgText strings.Builder
	msgText.WriteString("🗑️ <b>Remove Custom File</b>\n\n")
	msgText.WriteString("Select a file to remove from your custom files list:\n\n")

	for i, filePath := range customFiles {
		msgText.WriteString(fmt.Sprintf("%d. <code>%s</code>\n", i+1, filePath))
	}

	msgText.WriteString("\n<i>💡 Note: This only removes from your list, files remain in repository</i>\n")
	msgText.WriteString("<i>Click a button below to remove that file:</i>")

	// Create removal buttons (max 20 to fit in message)
	var keyboardRows [][]tgbotapi.InlineKeyboardButton
	maxButtons := len(customFiles)
	if maxButtons > 20 {
		maxButtons = 20
	}

	// Add files in rows of 2
	for i := 0; i < maxButtons; i += 2 {
		var row []tgbotapi.InlineKeyboardButton

		// First file in row
		buttonText := fmt.Sprintf("%d. %s", i+1, customFiles[i])
		if len(buttonText) > 30 {
			buttonText = fmt.Sprintf("%d. %s...", i+1, customFiles[i][:20])
		}
		// Include messageKey in callback data to distinguish from /customfile context
		row = append(row, tgbotapi.NewInlineKeyboardButtonData(buttonText, fmt.Sprintf("remove_custom_file_%d_%s", i, messageKey)))

		// Second file in row (if exists)
		if i+1 < maxButtons {
			buttonText2 := fmt.Sprintf("%d. %s", i+2, customFiles[i+1])
			if len(buttonText2) > 30 {
				buttonText2 = fmt.Sprintf("%d. %s...", i+2, customFiles[i+1][:20])
			}
			row = append(row, tgbotapi.NewInlineKeyboardButtonData(buttonText2, fmt.Sprintf("remove_custom_file_%d_%s", i+1, messageKey)))
		}

		keyboardRows = append(keyboardRows, row)
	}

	// Add "Show More" button if we have more than 20 files
	if len(customFiles) > 20 {
		moreButton := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("📋 Show All %d Files", len(customFiles)), "show_all_custom_files_remove"),
		)
		keyboardRows = append(keyboardRows, moreButton)
	}

	// Add back button
	backButton := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔙 Back", fmt.Sprintf("back_to_files_%s", messageKey)),
	)
	keyboardRows = append(keyboardRows, backButton)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, msgText.String())
	editMsg.ParseMode = "HTML"
	editMsg.ReplyMarkup = &keyboard

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		return fmt.Errorf("failed to edit message: %w", err)
	}

	return nil
}

// handleCustomFilePathReply handles the user's reply with a custom file path
func (b *Bot) handleCustomFilePathReply(message *tgbotapi.Message, stateData string) error {
	// Parse stateData format: messageKey|||DELIM|||isPhoto or customfile_standalone|||DELIM|||false
	parts := strings.SplitN(stateData, "|||DELIM|||", 2)
	if len(parts) != 2 {
		b.sendResponse(message.Chat.ID, "❌ Invalid state data. Please try again.")
		return nil
	}

	messageKey := parts[0]
	isPhotoStr := parts[1]
	isPhoto := isPhotoStr == "true"

	// Validate and clean the file path
	filePath := strings.TrimSpace(message.Text)
	if filePath == "" {
		b.sendResponse(message.Chat.ID, "❌ Please provide a valid file path.")
		return nil
	}

	// Basic validation
	if strings.Contains(filePath, "..") || strings.HasPrefix(filePath, "/") {
		b.sendResponse(message.Chat.ID, "❌ Invalid file path. Please use relative paths without '..' or leading '/'.")
		return nil
	}

	// Ensure it ends with .md
	if !strings.HasSuffix(filePath, ".md") {
		filePath += ".md"
	}

	// Get user
	user, err := b.ensureUser(message)
	if err != nil {
		b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ Failed to get user: %v", err))
		return nil
	}

	if b.db == nil {
		b.sendResponse(message.Chat.ID, "❌ Custom files require database configuration.")
		return nil
	}

	// Check if user has reached custom file limit
	var premiumLevel int = 0
	premiumUser, err := b.db.GetPremiumUser(message.Chat.ID)
	if err == nil && premiumUser != nil && premiumUser.IsPremiumUser() {
		premiumLevel = premiumUser.Level
	}

	currentFiles := user.GetCustomFiles()
	customFileLimit := database.GetCustomFileLimit(premiumLevel)

	if len(currentFiles) >= customFileLimit {
		errorMsg := FormatCustomFileLimitMessage(premiumLevel, customFileLimit)
		b.sendResponse(message.Chat.ID, errorMsg)
		return nil
	}

	// Check if file already exists in user's list
	for _, existingFile := range currentFiles {
		if existingFile == filePath {
			b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ File '%s' is already in your custom files list.", filePath))
			return nil
		}
	}

	// Add the file to user's custom files
	if err := user.AddCustomFile(filePath); err != nil {
		b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ Failed to add custom file: %v", err))
		return nil
	}

	// Update user in database
	if err := b.db.UpdateUserCustomFiles(message.Chat.ID, user.CustomFiles); err != nil {
		b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ Failed to save custom file: %v", err))
		return nil
	}

	// Handle different contexts
	if messageKey == "customfile_standalone" {
		// This is from /customfile command - show success and automatically display custom files list
		successMsg := fmt.Sprintf("✅ Added custom file: <code>%s</code>", filePath)

		// Send confirmation message first
		statusMessageID := b.sendResponseAndGetMessageID(message.Chat.ID, successMsg)

		// Create a dummy callback query to show the updated custom files list
		callbackQuery := &tgbotapi.CallbackQuery{
			Message: &tgbotapi.Message{
				Chat:      &tgbotapi.Chat{ID: message.Chat.ID},
				MessageID: statusMessageID,
			},
		}

		// Refresh user object to get updated custom files
		updatedUser, err := b.db.GetUserByChatID(message.Chat.ID)
		if err != nil {
			return fmt.Errorf("failed to get updated user: %w", err)
		}

		// Show the updated custom files list
		return b.refreshCustomFilesList(callbackQuery, updatedUser)
	} else {
		// This is from message saving workflow - add the file and return to custom file selection
		successMsg := fmt.Sprintf("✅ Added custom file: <code>%s</code>\n\nChoose a file to save your message:", filePath)

		// Send confirmation message
		statusMessageID := b.sendResponseAndGetMessageID(message.Chat.ID, successMsg)

		// Create a dummy callback query for the showCustomFileSelectionInterface function
		callbackQuery := &tgbotapi.CallbackQuery{
			Message: &tgbotapi.Message{
				Chat:      &tgbotapi.Chat{ID: message.Chat.ID},
				MessageID: statusMessageID,
			},
		}

		// Show the custom file selection interface with the updated file list
		return b.showCustomFileSelectionInterface(callbackQuery, messageKey, user, isPhoto)
	}
}

// handleCustomFilePinSelect shows file selection interface for pinning
func (b *Bot) handleCustomFilePinSelect(callback *tgbotapi.CallbackQuery) error {
	// Get user
	user, err := b.ensureUserFromCallback(callback)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	if b.db == nil {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Custom files require database configuration.")
		return nil
	}

	customFiles := user.GetCustomFiles()

	if len(customFiles) == 0 {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "📂 No custom files found to pin.")
		return nil
	}

	// Build message text
	var msgText strings.Builder
	msgText.WriteString("📌 <b>Pin a Custom File</b>\n\n")
	msgText.WriteString("Select a file to pin (move to top):\n")

	// Show current pin status
	pinnedFiles := user.GetPinnedFiles()
	if len(pinnedFiles) > 0 {
		msgText.WriteString("\n<b>Currently Pinned:</b>\n")
		for i, file := range pinnedFiles {
			msgText.WriteString(fmt.Sprintf("📌 %d. <code>%s</code>\n", i+1, file))
		}
	}

	msgText.WriteString("\n<b>All Files:</b>\n")
	for i, filePath := range customFiles {
		isPinned := i < 2 // First 2 are pinned
		if isPinned {
			msgText.WriteString(fmt.Sprintf("📌 %d. <code>%s</code> (pinned)\n", i+1, filePath))
		} else {
			msgText.WriteString(fmt.Sprintf("📄 %d. <code>%s</code>\n", i+1, filePath))
		}
	}

	msgText.WriteString("\n<i>💡 Click a file number to pin it (move to top of list)</i>")

	// Create buttons for each file (only show unpinned files as options)
	var rows [][]tgbotapi.InlineKeyboardButton

	// Create file selection buttons (max 2 per row)
	var currentRow []tgbotapi.InlineKeyboardButton
	buttonCount := 0

	for i, filePath := range customFiles {
		isPinned := i < 2 // First 2 are pinned
		if !isPinned {    // Only show unpinned files as options
			displayName := strings.TrimSuffix(filePath, ".md")
			if len(displayName) > 12 {
				displayName = displayName[:9] + "..."
			}

			buttonText := fmt.Sprintf("%d. %s", i+1, displayName)
			currentRow = append(currentRow, tgbotapi.NewInlineKeyboardButtonData(buttonText, fmt.Sprintf("pin_file_%d", i)))
			buttonCount++

			// Add row when we have 2 buttons or it's the last button
			if len(currentRow) == 2 {
				rows = append(rows, currentRow)
				currentRow = []tgbotapi.InlineKeyboardButton{}
			}
		}
	}

	// Add remaining button if any
	if len(currentRow) > 0 {
		rows = append(rows, currentRow)
	}

	// Add back button
	backRow := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔙 Back", "customfile_back"),
	)
	rows = append(rows, backRow)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, msgText.String())
	editMsg.ParseMode = "HTML"
	editMsg.ReplyMarkup = &keyboard

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		return fmt.Errorf("failed to edit pin selection message: %w", err)
	}

	return nil
}

// handlePinFileAction handles pinning a specific file
func (b *Bot) handlePinFileAction(callback *tgbotapi.CallbackQuery) error {
	// Parse callback data: pin_file_{index}
	indexStr := strings.TrimPrefix(callback.Data, "pin_file_")
	fileIndex, err := strconv.Atoi(indexStr)
	if err != nil {
		return fmt.Errorf("invalid file index: %w", err)
	}

	// Get user
	user, err := b.ensureUserFromCallback(callback)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	if b.db == nil {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Custom files require database configuration.")
		return nil
	}

	customFiles := user.GetCustomFiles()

	if fileIndex < 0 || fileIndex >= len(customFiles) {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Invalid file selection.")
		return nil
	}

	selectedFile := customFiles[fileIndex]

	// Pin the file (move to front of array)
	err = user.PinCustomFile(selectedFile)
	if err != nil {
		logger.Error("Failed to pin custom file", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
			"file":    selectedFile,
		})
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Failed to pin file.")
		return nil
	}

	// Save to database
	if err := b.db.UpdateUserCustomFiles(callback.Message.Chat.ID, user.CustomFiles); err != nil {
		logger.Error("Failed to save pinned custom files", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Failed to save pinned file.")
		return nil
	}

	// Show success message and return to main custom files list
	successMsg := fmt.Sprintf("✅ <b>File Pinned!</b>\n\n📌 <code>%s</code> has been moved to the top and will now appear in the main file selection.", selectedFile)

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, successMsg)
	editMsg.ParseMode = "HTML"

	// Add back button to return to custom files list
	backKeyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Back to Custom Files", "customfile_back"),
		),
	)
	editMsg.ReplyMarkup = &backKeyboard

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		return fmt.Errorf("failed to send pin success message: %w", err)
	}

	logger.Info("Custom file pinned successfully", map[string]interface{}{
		"chat_id": callback.Message.Chat.ID,
		"file":    selectedFile,
	})

	return nil
}

