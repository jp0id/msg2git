package telegram

import (
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/msg2git/msg2git/internal/consts"
)

// Main command router and basic commands

func (b *Bot) handleCommand(message *tgbotapi.Message) error {
	command := strings.TrimSpace(message.Text)

	switch command {
	// Basic commands
	case "/start":
		return b.handleStartCommand(message)
	case "/help":
		return b.handleHelpCommand(message)

	// Setup commands (implemented in commands_setup.go)
	case "/repo":
		return b.handleRepoCommand(message)
	case "/llm":
		return b.handleLLMCommand(message)

	// Information commands (implemented in commands_info.go)
	case "/sync":
		return b.handleSyncCommand(message)
	case "/insight":
		return b.handleInsightCommand(message)
	case "/stats":
		return b.handleStatsCommand(message)

	// Content management commands (implemented in commands_content.go)
	case "/todo":
		return b.handleTodoCommand(message, 0) // Start with offset 0
	case "/issue":
		return b.handleIssueCommand(message, 0) // Start with offset 0
	case "/customfile":
		return b.handleCustomFileCommand(message)

	// Premium commands (implemented in commands_premium.go)
	case "/coffee":
		return b.handleCoffeeCommand(message)
	case "/resetusage":
		return b.handleResetUsageCommand(message)

	default:
		return fmt.Errorf("unknown command: %s", message.Text)
	}
}

func (b *Bot) handleStartCommand(message *tgbotapi.Message) error {
	// Build website links if BASE_URL is configured
	var websiteLinks string
	if b.config.BaseURL != "" {
		websiteLinks = fmt.Sprintf(`

<b>🌐 Learn More:</b>
• <a href="%s">Visit our homepage</a>
• <a href="%s/privacy">Privacy Policy</a>`, b.config.BaseURL, b.config.BaseURL)
	}

	welcomeMsg := fmt.Sprintf(`🤖 <b>Welcome to Gitted Messages!</b>

A minimalist Telegram bot that turns your messages into GitHub commits.

<b>🚀 Quick Setup:</b>
1. /repo - Setup your GitHub repository, make sure following are settled:
	- your repository
	- your repository auth
	- committer
2. /llm - Configure AI features (optional)
3. Start sending messages!

<b>📝 How it works:</b>
• Send any message → Choose location → Message prepended to chosen file → Auto-committed to GitHub
• Supports text, photos, and captions
• Locations: NOTE, TODO, ISSUE, IDEA, INBOX, TOOL

<b>Need help?</b> Use /help for all commands and features.%s

<i>Ready to get started? Set up your repository first!</i>`, websiteLinks)

	msg := tgbotapi.NewMessage(message.Chat.ID, welcomeMsg)
	msg.ParseMode = consts.ParseModeHTML
	if _, err := b.rateLimitedSend(message.Chat.ID, msg); err != nil {
		return fmt.Errorf("failed to send start message: %w", err)
	}
	return nil
}

func (b *Bot) handleHelpCommand(message *tgbotapi.Message) error {
	// Build website links if BASE_URL is configured
	var websiteLinks string
	if b.config.BaseURL != "" {
		websiteLinks = fmt.Sprintf(`<b>🌐 Resources:</b>
• <a href="%s">Homepage & Documentation</a>
• <a href="%s/privacy">Privacy Policy</a>
• <a href="%s/refund">Refund Policy</a>
• <a href="%s/terms">Terms of Service</a>`, b.config.BaseURL, b.config.BaseURL, b.config.BaseURL, b.config.BaseURL)
	}

	helpMsg := fmt.Sprintf(`📚 <b>Gitted Messages Help</b>

<b>🔧 Setup Commands:</b>
• /repo - View repository information and settings
• /llm - Configure and control AI processing

<b>📊 Information Commands:</b>
• /sync - Synchronize issue statuses from GitHub
• /insight - View usage statistics and repository status
• /stats - View global bot statistics
• /todo - Show latest TODO items
• /issue - Show latest open issues

<b>📁 File Management:</b>
• /customfile - Manage custom files and folders

<b>💎 Premium Commands:</b>
• /coffee - Support project and unlock premium features
• /resetusage - Reset usage counters (paid service)

<b>💡 Pro Tips:</b>
• Use TODO for task items with checkboxes
• Use ISSUE to create GitHub issues automatically
• Send photos with captions for rich content
• Use /insight to monitor repository status

%s

<b>🆘 Need Support?</b>
Use /coffee to support the project and get priority help!`, websiteLinks)

	msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf(helpMsg))
	msg.DisableWebPagePreview = true
	msg.ParseMode = consts.ParseModeHTML
	if _, err := b.rateLimitedSend(message.Chat.ID, msg); err != nil {
		return fmt.Errorf("failed to send help message: %w", err)
	}
	return nil
}

func (b *Bot) handleEditCommand(message *tgbotapi.Message) error {
	// TODO: Implement edit functionality
	// This would require tracking message IDs and their corresponding file locations
	b.sendResponse(message.Chat.ID, consts.SuccessSaved)
	return nil
}

func (b *Bot) handleDoneCommand(message *tgbotapi.Message) error {
	// TODO: Implement done functionality for TODO items
	// This would require finding the specific TODO item and marking it as completed
	b.sendResponse(message.Chat.ID, consts.SuccessCompleted)
	return nil
}
