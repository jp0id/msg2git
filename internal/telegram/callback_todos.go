package telegram

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/msg2git/msg2git/internal/logger"
)

// TODO-related callback query handlers for inline keyboard interactions

func (b *Bot) handleTodoMore(callback *tgbotapi.CallbackQuery) error {
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
	return b.handleTodoCommandWithMessageID(callback.Message.Chat.ID, callback.Message.MessageID, offset)
}

func (b *Bot) handleTodoDone(callback *tgbotapi.CallbackQuery) error {
	// Parse message ID from callback data
	parts := strings.Split(callback.Data, "_")
	if len(parts) != 3 {
		return fmt.Errorf("invalid callback data format")
	}

	messageIDStr := parts[2]
	messageID, err := strconv.Atoi(messageIDStr)
	if err != nil {
		return fmt.Errorf("invalid message ID: %w", err)
	}

	logger.Info("Marking TODO as done", map[string]interface{}{
		"message_id": messageID,
		"chat_id":    callback.Message.Chat.ID,
	})

	// Start progress tracking
	b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 30, "🔄 Processing TODO completion...")

	// Get user-specific GitHub manager
	userGitHubProvider, err := b.getUserGitHubProvider(callback.Message.Chat.ID)
	if err != nil {
		logger.Error("Failed to get GitHub manager for user", map[string]interface{}{
			"chat_id": callback.Message.Chat.ID,
			"error":   err.Error(),
		})
		// Edit message with error
		errorMsg := "❌ GitHub not configured"
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		return err
	}

	// Read TODO.md file
	todoContent, err := userGitHubProvider.ReadFile("todo.md")
	if err != nil {
		logger.Error("Failed to read todo.md", map[string]interface{}{
			"error": err.Error(),
		})
		// Edit message with error
		errorMsg := "❌ Failed to read TODO file, can add a todo item first"
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		return nil
	}

	// Parse TODO items and mark the specific one as done
	todos := b.parseTodoItems(todoContent)
	var updatedLines []string
	found := false
	currentChatID := callback.Message.Chat.ID

	for _, todo := range todos {
		// Only allow marking TODOs done if they belong to the current chat (or old format with ChatID 0)
		if todo.MessageID == messageID && !todo.Done && (todo.ChatID == currentChatID || todo.ChatID == 0) {
			// Mark as done: change - [ ] to - [x]
			// Use the new HTML comment format
			line := fmt.Sprintf("- [x] <!--[%d] [%d]--> %s (%s)", todo.MessageID, currentChatID, todo.Content, todo.Date)
			updatedLines = append(updatedLines, line)
			found = true
		} else {
			// Keep original format (preserve whatever format it was in)
			checkbox := "[ ]"
			if todo.Done {
				checkbox = "[x]"
			}

			var line string
			if todo.ChatID != 0 {
				// Check if this looks like it came from HTML comment format or old bracket format
				// For new items, use HTML comment format
				line = fmt.Sprintf("- %s <!--[%d] [%d]--> %s (%s)", checkbox, todo.MessageID, todo.ChatID, todo.Content, todo.Date)
			} else {
				// Old format without chat ID - keep as is for backward compatibility
				line = fmt.Sprintf("- %s [%d] %s (%s)", checkbox, todo.MessageID, todo.Content, todo.Date)
			}
			updatedLines = append(updatedLines, line)
		}
	}

	if !found {
		// Edit message with error
		errorMsg := "❌ TODO item not found"
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		return fmt.Errorf("TODO item with message ID %d not found", messageID)
	}

	// Show GitHub save progress
	b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 70, "📝 Saving to GitHub...")

	// Update the file with new content using custom committer info and premium level
	newContent := strings.Join(updatedLines, "\n") + "\n"
	commitMsg := fmt.Sprintf("Mark TODO #%d as completed via Telegram", messageID)
	committerInfo := b.getCommitterInfo(callback.Message.Chat.ID)
	premiumLevel := b.getPremiumLevel(callback.Message.Chat.ID)
	if err := userGitHubProvider.ReplaceFileWithAuthorAndPremium("todo.md", newContent, commitMsg, committerInfo, premiumLevel); err != nil {
		logger.Error("Failed to update todo.md", map[string]interface{}{
			"error": err.Error(),
		})

		// Check if it's an authorization error and provide helpful message
		var errorMsg string
		if strings.Contains(err.Error(), "GitHub authorization failed") {
			errorMsg = "❌ " + err.Error()
		} else {
			errorMsg = "❌ Failed to update TODO"
		}

		// Edit message with error
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		return err
	}

	// Show completion progress
	b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 100, "✅ TODO marked as completed!")

	// Small delay to show completion before refreshing
	time.Sleep(500 * time.Millisecond)

	// Update the original message to refresh the TODO list using message editing
	return b.handleTodoCommandWithMessageID(callback.Message.Chat.ID, callback.Message.MessageID, 0)
}

