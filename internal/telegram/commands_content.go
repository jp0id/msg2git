package telegram

import (
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/msg2git/msg2git/internal/consts"
	"github.com/msg2git/msg2git/internal/database"
	"github.com/msg2git/msg2git/internal/github"
	"github.com/msg2git/msg2git/internal/logger"
)

// Content management command handlers

func (b *Bot) handleTodoCommand(message *tgbotapi.Message, offset int) error {
	return b.handleTodoCommandWithMessageID(message.Chat.ID, 0, offset)
}

func (b *Bot) handleTodoCommandWithMessageID(chatID int64, messageID int, offset int) error {
	// Ensure user exists in database if database is configured
	_, err := b.ensureUserFromChatID(chatID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	// Get user-specific GitHub manager
	userGitHubProvider, err := b.getUserGitHubProvider(chatID)
	if err != nil {
		errorMsg := "❌ " + err.Error()
		if b.db != nil {
			errorMsg += ". " + consts.GitHubSetupPrompt
		}
		b.sendResponse(chatID, errorMsg)
		return nil
	}

	// Get premium level for the user
	premiumLevel := b.getPremiumLevel(chatID)

	// Ensure repository exists
	if err := userGitHubProvider.EnsureRepositoryWithPremium(premiumLevel); err != nil {
		errorMsg := b.formatRepositorySetupError(err, "access TODO items")
		b.sendResponse(chatID, errorMsg)
		return nil
	}

	// Read TODO file
	content, err := userGitHubProvider.ReadFile("todo.md")
	if err != nil {
		b.sendResponse(chatID, "❌ Failed to read TODO file, can add a todo item first")
		return nil
	}

	// Parse TODOs
	todos := b.parseTodoItems(content)

	// Filter undone TODOs for the current chat
	var undoneTodos []TodoItem
	for _, todo := range todos {
		if !todo.Done && (todo.ChatID == chatID || todo.ChatID == 0) {
			undoneTodos = append(undoneTodos, todo)
		}
	}

	if len(undoneTodos) == 0 {
		msg := "✅ <b>No pending TODO items!</b>\n\n<i>All tasks completed or no TODOs found.</i>"
		if messageID > 0 {
			editMsg := tgbotapi.NewEditMessageText(chatID, messageID, msg)
			editMsg.ParseMode = consts.ParseModeHTML
			if _, err := b.rateLimitedSend(chatID, editMsg); err != nil {
				return fmt.Errorf("failed to edit message: %w", err)
			}
		} else {
			responseMsg := tgbotapi.NewMessage(chatID, msg)
			responseMsg.ParseMode = consts.ParseModeHTML
			if _, err := b.rateLimitedSend(chatID, responseMsg); err != nil {
				return fmt.Errorf("failed to send message: %w", err)
			}
		}
		return nil
	}

	// Pagination
	const itemsPerPage = 5
	totalPages := (len(undoneTodos) + itemsPerPage - 1) / itemsPerPage
	currentPage := (offset / itemsPerPage) + 1

	start := offset
	end := offset + itemsPerPage
	if end > len(undoneTodos) {
		end = len(undoneTodos)
	}

	// Build response message
	msg := fmt.Sprintf("✅ <b>TODO Items</b> (Page %d/%d)\n\n", currentPage, totalPages)

	for i := start; i < end; i++ {
		todo := undoneTodos[i]
		indexNumber := i + 1 // Use 1-based indexing for display
		msg += fmt.Sprintf("%d. %s\n<i>Added: %s</i>\n\n", indexNumber, todo.Content, todo.Date)
	}

	// Create navigation buttons
	var keyboard tgbotapi.InlineKeyboardMarkup
	var navButtons []tgbotapi.InlineKeyboardButton

	// Previous button
	if offset > 0 {
		prevOffset := offset - itemsPerPage
		if prevOffset < 0 {
			prevOffset = 0
		}
		navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData("◀️ Previous", fmt.Sprintf("todo_more_%d", prevOffset)))
	}

	// Next button
	if end < len(undoneTodos) {
		nextOffset := offset + itemsPerPage
		navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData("Next ▶️", fmt.Sprintf("todo_more_%d", nextOffset)))
	}

	if len(navButtons) > 0 {
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, navButtons)
	}

	// Add action buttons for each TODO item
	for i := start; i < end; i++ {
		todo := undoneTodos[i]
		indexNumber := i + 1 // Use 1-based indexing for display
		doneButton := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("✅ Mark %d Done", indexNumber),
			fmt.Sprintf("todo_done_%d", todo.MessageID),
		)
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []tgbotapi.InlineKeyboardButton{doneButton})
	}

	// Send or edit message
	if messageID > 0 {
		editMsg := tgbotapi.NewEditMessageText(chatID, messageID, msg)
		editMsg.ParseMode = consts.ParseModeHTML
		editMsg.ReplyMarkup = &keyboard
		if _, err := b.rateLimitedSend(chatID, editMsg); err != nil {
			return fmt.Errorf("failed to edit message: %w", err)
		}
	} else {
		responseMsg := tgbotapi.NewMessage(chatID, msg)
		responseMsg.ParseMode = consts.ParseModeHTML
		responseMsg.ReplyMarkup = keyboard
		if _, err := b.rateLimitedSend(chatID, responseMsg); err != nil {
			return fmt.Errorf("failed to send message: %w", err)
		}
	}

	return nil
}

func (b *Bot) handleIssueCommand(message *tgbotapi.Message, offset int) error {
	logger.Debug("Handling /issue command", map[string]interface{}{
		"offset":  offset,
		"chat_id": message.Chat.ID,
	})

	// Send status message and get message ID for later editing
	statusMessageID := b.sendResponseAndGetMessageID(message.Chat.ID, "🔄 Fetching latest issues...")
	return b.handleIssueCommandWithMessageID(message.Chat.ID, statusMessageID, offset)
}

func (b *Bot) handleIssueCommandWithMessageID(chatID int64, messageID int, offset int) error {
	logger.Debug("Handling issue command with message ID", map[string]interface{}{
		"offset":     offset,
		"chat_id":    chatID,
		"message_id": messageID,
	})

	// Get user-specific GitHub manager
	userGitHubProvider, err := b.getUserGitHubProvider(chatID)
	if err != nil {
		logger.Error("Failed to get GitHub manager for user", map[string]interface{}{
			"chat_id": chatID,
			"error":   err.Error(),
		})
		if messageID > 0 {
			b.editMessage(chatID, messageID, "❌ GitHub not configured. Please use /repo to settle repo first.")
		} else {
			b.sendResponse(chatID, "❌ GitHub not configured. Please use /repo to settle repo first.")
		}
		return nil
	}

	// Read issue.md file
	issueContent, err := userGitHubProvider.ReadFile("issue.md")
	if err != nil {
		logger.Error("Failed to read issue.md", map[string]interface{}{
			"error": err.Error(),
		})
		
		// Check if it's an authorization error and provide helpful message
		var errorMsg string
		if strings.Contains(err.Error(), "GitHub authorization failed") {
			errorMsg = "❌ " + err.Error()
		} else {
			errorMsg = "❌ Failed to read issue.md file"
		}
		
		if messageID > 0 {
			b.editMessage(chatID, messageID, errorMsg)
		} else {
			b.sendResponse(chatID, errorMsg)
		}
		return nil
	}

	// Parse issue statuses directly from issue.md content (no API calls needed!)
	statuses := b.parseIssueStatusesFromContent(issueContent, userGitHubProvider)
	if len(statuses) == 0 {
		if messageID > 0 {
			b.editMessage(chatID, messageID, "🐛 No issues found")
		} else {
			b.sendResponse(chatID, "🐛 No issues found")
		}
		return nil
	}

	logger.Debug("Found issues from local content", map[string]interface{}{
		"count": len(statuses),
	})

	// Filter open issues
	var openIssues []*github.IssueStatus
	for _, status := range statuses {
		if strings.ToLower(status.State) == "open" {
			openIssues = append(openIssues, status)
		}
	}

	logger.Debug("Found open issues", map[string]interface{}{
		"open_count":  len(openIssues),
		"total_count": len(statuses),
	})

	// Sort by issue number (descending - latest first)
	for i := 0; i < len(openIssues); i++ {
		for j := i + 1; j < len(openIssues); j++ {
			if openIssues[i].Number < openIssues[j].Number {
				openIssues[i], openIssues[j] = openIssues[j], openIssues[i]
			}
		}
	}

	// Apply offset and limit
	start := offset
	if start >= len(openIssues) {
		var noIssuesMsg string
		if offset == 0 {
			noIssuesMsg = "🐛 No open issues found"
		} else {
			noIssuesMsg = "🐛 No more open issues"
		}

		if messageID > 0 {
			b.editMessage(chatID, messageID, noIssuesMsg)
		} else {
			b.sendResponse(chatID, noIssuesMsg)
		}
		return nil
	}

	end := start + 5
	if end > len(openIssues) {
		end = len(openIssues)
	}

	// Create message with issue items
	var msgText strings.Builder
	msgText.WriteString("🐛 **Latest Open Issues**\n\n")

	// Add issue titles to message text
	for i, issue := range openIssues[start:end] {
		msgText.WriteString(fmt.Sprintf("%d. **#%d** %s\n", i+1, issue.Number, issue.Title))
	}

	// Create keyboard with issue item buttons and More button
	var keyboardRows [][]tgbotapi.InlineKeyboardButton

	// Add buttons for each issue item (link, comment, close in one row)
	for _, issue := range openIssues[start:end] {
		// Single row: Issue link, Comment, and Close buttons
		issueRow := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL(fmt.Sprintf("🔗 #%d", issue.Number), issue.HTMLURL),
			tgbotapi.NewInlineKeyboardButtonData("💬", fmt.Sprintf("issue_comment_%d", issue.Number)),
			tgbotapi.NewInlineKeyboardButtonData("✅", fmt.Sprintf("issue_close_%d", issue.Number)),
		)
		keyboardRows = append(keyboardRows, issueRow)
	}

	// Add navigation buttons (Prev/Next) if needed
	var navButtons []tgbotapi.InlineKeyboardButton

	// Add Prev button if not on first page
	if start > 0 {
		prevOffset := start - 5
		if prevOffset < 0 {
			prevOffset = 0
		}
		navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData("⬅️ Prev", fmt.Sprintf("issue_more_%d", prevOffset)))
	}

	// Add Next button if there are more items
	if end < len(openIssues) {
		navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData("➡️ Next", fmt.Sprintf("issue_more_%d", end)))
	}

	// Add navigation row if we have navigation buttons
	if len(navButtons) > 0 {
		navRow := tgbotapi.NewInlineKeyboardRow(navButtons...)
		keyboardRows = append(keyboardRows, navRow)
	}

	var keyboard tgbotapi.InlineKeyboardMarkup
	if len(keyboardRows) > 0 {
		keyboard = tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)
	}

	// Edit the existing message instead of deleting and creating new
	if messageID > 0 {
		editMsg := tgbotapi.NewEditMessageText(chatID, messageID, msgText.String())
		editMsg.ParseMode = "Markdown"
		if keyboard.InlineKeyboard != nil {
			editMsg.ReplyMarkup = &keyboard
		}

		if _, err := b.rateLimitedSend(chatID, editMsg); err != nil {
			logger.Error("Failed to edit issue message", map[string]interface{}{
				"error": err.Error(),
			})
			// Fallback: send new message if editing fails
			msg := tgbotapi.NewMessage(chatID, msgText.String())
			msg.ParseMode = "Markdown"
			if keyboard.InlineKeyboard != nil {
				msg.ReplyMarkup = keyboard
			}
			if _, fallbackErr := b.rateLimitedSend(chatID, msg); fallbackErr != nil {
				logger.Error("Failed to send fallback issue message", map[string]interface{}{
					"error": fallbackErr.Error(),
				})
				return fallbackErr
			}
		}
	} else {
		// No message ID provided, send new message
		msg := tgbotapi.NewMessage(chatID, msgText.String())
		msg.ParseMode = "Markdown"
		if keyboard.InlineKeyboard != nil {
			msg.ReplyMarkup = keyboard
		}

		if _, err := b.rateLimitedSend(chatID, msg); err != nil {
			logger.Error("Failed to send issue message", map[string]interface{}{
				"error": err.Error(),
			})
			return err
		}
	}

	return nil
}

func (b *Bot) handleCustomFileCommand(message *tgbotapi.Message) error {
	// Ensure user exists in database if database is configured
	user, err := b.ensureUser(message)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	if b.db == nil {
		b.sendResponse(message.Chat.ID, "❌ Custom files require database configuration. Please contact the administrator.")
		return nil
	}

	// Get user's custom files
	customFiles := user.GetCustomFiles()

	if len(customFiles) == 0 {
		// Get premium status for file limits
		var premiumLevel int = 0
		premiumUser, err := b.db.GetPremiumUser(message.Chat.ID)
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

		msg := tgbotapi.NewMessage(message.Chat.ID, noFilesMsg)
		msg.ParseMode = "HTML"
		msg.ReplyMarkup = keyboard
		if _, err := b.rateLimitedSend(message.Chat.ID, msg); err != nil {
			return fmt.Errorf("failed to send no custom files message: %w", err)
		}
		return nil
	}

	// Get premium status for file limits
	var premiumLevel int = 0
	premiumUser, err := b.db.GetPremiumUser(message.Chat.ID)
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

	msg := tgbotapi.NewMessage(message.Chat.ID, msgText.String())
	msg.ParseMode = "HTML"
	if keyboard.InlineKeyboard != nil {
		msg.ReplyMarkup = keyboard
	}

	if _, err := b.rateLimitedSend(message.Chat.ID, msg); err != nil {
		return fmt.Errorf("failed to send custom files message: %w", err)
	}

	return nil
}

// Helper function to ensure user from chat ID (for internal use)
func (b *Bot) ensureUserFromChatID(chatID int64) (*database.User, error) {
	if b.db == nil {
		return nil, nil // No database configured
	}

	// For internal calls where we only have chatID, use empty username
	return b.db.GetOrCreateUser(chatID, "")
}

// Helper function to parse offset from callback data
func (b *Bot) parseOffsetFromCallback(data string) int {
	parts := strings.Split(data, "_")
	if len(parts) < 3 {
		return 0
	}

	offsetStr := parts[2]
	offset, err := strconv.Atoi(offsetStr)
	if err != nil {
		logger.Warn("Failed to parse offset from callback data", map[string]interface{}{
			"data":  data,
			"error": err.Error(),
		})
		return 0
	}

	return offset
}

// Helper function to parse open issues from issue.md content
func (b *Bot) parseOpenIssuesFromContent(content string) []struct {
	Number  int
	Title   string
	State   string
	HTMLURL string
} {
	var openIssues []struct {
		Number  int
		Title   string
		State   string
		HTMLURL string
	}

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "- 🟢") {
			continue
		}

		// Parse pattern: - 🟢 owner/repo#123 [title]
		if strings.Contains(line, "#") && strings.Contains(line, "[") {
			// Extract issue number and title
			parts := strings.Split(line, "#")
			if len(parts) >= 2 {
				numberPart := strings.Split(parts[1], " ")[0]
				if number, err := strconv.Atoi(numberPart); err == nil {
					// Extract title from brackets
					if titleStart := strings.Index(line, "["); titleStart != -1 {
						if titleEnd := strings.Index(line[titleStart:], "]"); titleEnd != -1 {
							title := line[titleStart+1 : titleStart+titleEnd]

							// Create GitHub URL (this is a simple approximation)
							url := fmt.Sprintf("https://github.com/issues/%d", number)

							openIssues = append(openIssues, struct {
								Number  int
								Title   string
								State   string
								HTMLURL string
							}{
								Number:  number,
								Title:   title,
								State:   "open",
								HTMLURL: url,
							})
						}
					}
				}
			}
		}
	}

	return openIssues
}
