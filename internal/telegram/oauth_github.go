package telegram

import (
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/msg2git/msg2git/internal/consts"
	"github.com/msg2git/msg2git/internal/logger"
)

// GitHub OAuth callback handlers for better iteration and maintenance
//
// This file contains all GitHub OAuth-related functionality for the Telegram bot.
// It provides a cleaner separation of concerns and makes it easier to iterate
// on OAuth features without affecting other parts of the codebase.
//
// Setup Instructions:
// 1. Create a GitHub OAuth App at https://github.com/settings/applications/new
// 2. Set the Authorization callback URL to your webhook endpoint
// 3. Set environment variables:
//    - GITHUB_OAUTH_CLIENT_ID=your_client_id
//    - GITHUB_OAUTH_CLIENT_SECRET=your_client_secret
//    - GITHUB_OAUTH_REDIRECT_URI=https://your-domain.com/auth/github/callback
// 4. Implement a web server to handle the OAuth callback
// 5. Call processGitHubOAuthCallback when the callback is received

const (
	// GitHub OAuth scopes - this is the only constant we need
	GitHubOAuthScopes = "repo" // Required scope for repository access
)

// handleGitHubOAuthPrivacyConfirmation shows privacy policy confirmation for GitHub OAuth
func (b *Bot) handleGitHubOAuthPrivacyConfirmation(callback *tgbotapi.CallbackQuery) error {
	logger.Info("GitHub OAuth privacy confirmation requested", map[string]interface{}{
		"chat_id":  callback.Message.Chat.ID,
		"user_id":  callback.From.ID,
		"username": callback.From.UserName,
	})

	// Check if OAuth is configured
	if !b.config.HasGitHubOAuthConfig() {
		notConfiguredMsg := `❌ <b>GitHub OAuth Not Configured</b>

GitHub OAuth is not set up on this bot. You can still setup manually using /repo`

		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, notConfiguredMsg)
		editMsg.ParseMode = consts.ParseModeHTML

		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			return fmt.Errorf("failed to send OAuth not configured message: %w", err)
		}
		return nil
	}

	// Generate OAuth URL directly
	state := fmt.Sprintf("telegram_%d_%d", callback.Message.Chat.ID, callback.From.ID)
	authURL := fmt.Sprintf(
		"https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s&scope=%s&state=%s",
		b.config.GitHubOAuthClientID,
		b.config.GitHubOAuthRedirectURI,
		GitHubOAuthScopes,
		state,
	)

	// Build privacy policy link if BASE_URL is configured
	var privacyLink string
	if b.config.BaseURL != "" {
		privacyLink = fmt.Sprintf(`

📋 <a href="%s/privacy">Read our Privacy Policy</a>`, b.config.BaseURL)
	} else {
		privacyLink = `

📋 Privacy Policy: See /help for website link`
	}

	var privacyText string
	if b.config.BaseURL != "" {
		privacyText = fmt.Sprintf(`<a href="%s/privacy">Privacy Policy</a>`, b.config.BaseURL)
	} else {
		privacyText = "Privacy Policy"
	}

	confirmationMsg := fmt.Sprintf(`🔐 <b>GitHub OAuth Setup</b>

⚠️ <b>Privacy Notice:</b>
By proceeding, you agree to our %s regarding secure storage of your GitHub oauth token.

<b>What we'll store:</b>
• Your GitHub oauth token (encrypted)
• Repository permissions you grant
• Basic account information

<b>How we protect it:</b>
• Encrypted storage
• Used only for repository operations
• Never shared with third parties%s

<b>What happens next:</b>
1. You'll be redirected to GitHub for authorization
2. GitHub will ask you to grant repository permissions
3. After approval, you'll be redirected back with credentials configured

<i>🔒 Your credentials are stored securely and only used for repository operations, You’re free to revoke them at any time.</i>`, privacyText, privacyLink)

	// Create confirmation keyboard with direct OAuth URL
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("✅ I Accept & Continue", authURL),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "oauth_cancel"),
		),
	)

	// Edit the original message to show privacy confirmation
	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, confirmationMsg)
	editMsg.ParseMode = consts.ParseModeHTML
	editMsg.DisableWebPagePreview = true
	editMsg.ReplyMarkup = &keyboard

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		logger.Error("Failed to edit message with OAuth privacy confirmation", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		return fmt.Errorf("failed to send OAuth privacy confirmation: %w", err)
	}

	return nil
}

// handleGitHubOAuthCallback handles GitHub OAuth button clicks after privacy confirmation
func (b *Bot) handleGitHubOAuthCallback(callback *tgbotapi.CallbackQuery) error {
	logger.Info("GitHub OAuth button clicked", map[string]interface{}{
		"chat_id":  callback.Message.Chat.ID,
		"user_id":  callback.From.ID,
		"username": callback.From.UserName,
	})

	// Check if OAuth is configured
	if !b.config.HasGitHubOAuthConfig() {
		notConfiguredMsg := `❌ <b>GitHub OAuth Not Configured</b>

GitHub OAuth is not set up on this bot. You still can manual setup by /repo`

		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, notConfiguredMsg)
		editMsg.ParseMode = consts.ParseModeHTML

		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			return fmt.Errorf("failed to send OAuth not configured message: %w", err)
		}
		return nil
	}

	// Generate state parameter for security (CSRF protection)
	state := fmt.Sprintf("telegram_%d_%d", callback.Message.Chat.ID, callback.From.ID)

	// Build GitHub OAuth authorization URL using config
	authURL := fmt.Sprintf(
		"https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s&scope=%s&state=%s",
		b.config.GitHubOAuthClientID,
		b.config.GitHubOAuthRedirectURI,
		GitHubOAuthScopes,
		state,
	)

	// Create response message with OAuth URL
	responseMsg := fmt.Sprintf(`🔐 <b>GitHub OAuth Setup</b>

Click the button below to authorize Msg2Git to access your GitHub repositories:

<i>⚠️ This will grant Gitted Messages permission to read and write to your repositories.</i>

<b>What happens next:</b>
1. You'll be redirected to GitHub for authorization
2. GitHub will ask you to grant repository permissions
3. After approval, you'll be redirected back with your credentials automatically configured

<i>🔒 Your credentials are stored securely and only used for repository operations, You’re free to revoke them at any time.</i>`)

	// Create inline keyboard with OAuth URL
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("🔗 Authorize on GitHub", authURL),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(consts.ButtonOAuthCancel, "oauth_cancel"),
		),
	)

	// Edit the original message to show OAuth options
	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, responseMsg)
	editMsg.ParseMode = consts.ParseModeHTML
	editMsg.DisableWebPagePreview = true
	editMsg.ReplyMarkup = &keyboard

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		logger.Error("Failed to edit message with OAuth options", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})

		// Fallback: send new message
		fallbackMsg := tgbotapi.NewMessage(callback.Message.Chat.ID, responseMsg)
		fallbackMsg.ParseMode = consts.ParseModeHTML
		fallbackMsg.DisableWebPagePreview = true
		fallbackMsg.ReplyMarkup = keyboard

		if _, fallbackErr := b.rateLimitedSend(callback.Message.Chat.ID, fallbackMsg); fallbackErr != nil {
			return fmt.Errorf("failed to send OAuth message: %w", fallbackErr)
		}
	}

	return nil
}

// handleOAuthCancelCallback handles OAuth cancellation
func (b *Bot) handleOAuthCancelCallback(callback *tgbotapi.CallbackQuery) error {
	logger.Info("GitHub OAuth cancelled", map[string]interface{}{
		"chat_id":  callback.Message.Chat.ID,
		"user_id":  callback.From.ID,
		"username": callback.From.UserName,
	})

	// Edit message to show cancellation
	cancelMsg := `❌ <b>GitHub OAuth Cancelled</b>, You can retry or setup access manually using /repo`

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, cancelMsg)
	editMsg.ParseMode = consts.ParseModeHTML

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		return fmt.Errorf("failed to edit cancel message: %w", err)
	}

	return nil
}

// processGitHubOAuthCallback processes the OAuth callback from GitHub
// This would typically be called by a web server endpoint, but for now it's a placeholder
func (b *Bot) processGitHubOAuthCallback(chatID int64, code, state string) error {
	logger.Info("Processing GitHub OAuth callback", map[string]interface{}{
		"chat_id": chatID,
		"state":   state,
	})

	// Validate state parameter
	expectedStatePrefix := fmt.Sprintf("telegram_%d_", chatID)
	if !strings.HasPrefix(state, expectedStatePrefix) {
		return fmt.Errorf("invalid OAuth state parameter")
	}

	// TODO: Exchange code for access token
	// 1. Make POST request to https://github.com/login/oauth/access_token
	// 2. Parse the access token from response
	// 3. Store token in database for the user
	// 4. Optionally fetch user repositories and let them choose one
	// 5. Send confirmation message to Telegram

	// Placeholder implementation
	confirmMsg := fmt.Sprintf(`✅ <b>GitHub OAuth Setup Complete!</b>

Your GitHub account has been successfully linked to Msg2Git.

<b>Next steps:</b>
• Use /repo to settle your repo url
• Start sending messages to create content in your repository

<i>🎉 You're all set to use Msg2Git with GitHub!</i>`)

	msg := tgbotapi.NewMessage(chatID, confirmMsg)
	msg.ParseMode = consts.ParseModeHTML

	if _, err := b.rateLimitedSend(chatID, msg); err != nil {
		return fmt.Errorf("failed to send OAuth success message: %w", err)
	}

	return nil
}

// Helper function to validate GitHub OAuth configuration
func (b *Bot) isGitHubOAuthConfigured() bool {
	return b.config.HasGitHubOAuthConfig()
}

// generateGitHubOAuthURL generates a GitHub OAuth authorization URL
func (b *Bot) generateGitHubOAuthURL(chatID int64, userID int64) string {
	state := fmt.Sprintf("telegram_%d_%d", chatID, userID)

	return fmt.Sprintf(
		"https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s&scope=%s&state=%s",
		b.config.GitHubOAuthClientID,
		b.config.GitHubOAuthRedirectURI,
		GitHubOAuthScopes,
		state,
	)
}
