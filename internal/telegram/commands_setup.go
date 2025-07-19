package telegram

import (
	"fmt"
	"html"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/msg2git/msg2git/internal/consts"
	"github.com/msg2git/msg2git/internal/database"
	"github.com/msg2git/msg2git/internal/github"
	"github.com/msg2git/msg2git/internal/logger"
)

// Setup command handlers

// parseGitHubRepoURL parses a GitHub repository URL and returns owner and repo name
func parseGitHubRepoURL(repoURL string) (owner, repo string, err error) {
	// Remove .git suffix if present
	repoURL = strings.TrimSuffix(repoURL, ".git")

	// Handle HTTPS URLs: https://github.com/owner/repo
	if strings.HasPrefix(repoURL, "https://github.com/") {
		path := strings.TrimPrefix(repoURL, "https://github.com/")
		parts := strings.Split(path, "/")
		if len(parts) >= 2 {
			return parts[0], parts[1], nil
		}
	}

	// Handle SSH URLs: git@github.com:owner/repo
	if strings.HasPrefix(repoURL, "git@github.com:") {
		path := strings.TrimPrefix(repoURL, "git@github.com:")
		parts := strings.Split(path, "/")
		if len(parts) >= 2 {
			return parts[0], parts[1], nil
		}
	}

	return "", "", fmt.Errorf("invalid GitHub repository URL format")
}

func (b *Bot) handleSetRepoCommand(message *tgbotapi.Message) error {
	instructionMsg := `🔧 <b>Set GitHub Repository</b>

To set your GitHub repository, reply to this message with your repository URL in the format:

<code>https://github.com/username/repository</code>

Example:
<code>https://github.com/johndoe/my-notes</code>

⚠️ <b>Note:</b> Recommend to start with an empty repository unless you already know the bot's behaviour.`

	msg := tgbotapi.NewMessage(message.Chat.ID, instructionMsg)
	msg.ParseMode = consts.ParseModeHTML
	msg.ReplyMarkup = tgbotapi.ForceReply{ForceReply: true, Selective: true}

	if _, err := b.rateLimitedSend(message.Chat.ID, msg); err != nil {
		return fmt.Errorf("failed to send setrepo instruction: %w", err)
	}

	return nil
}

func (b *Bot) handleRepoTokenCommand(message *tgbotapi.Message) error {
	instructionMsg := `🔑 <b>Set GitHub Personal Access Token</b>

To set your GitHub token, reply to this message with your personal access token.

<b>How to create a token:</b>
1. Go to GitHub → Settings → Developer settings -> Personal access tokens → Fine-grained tokens (🔗<a href="https://github.com/settings/personal-access-tokens">link</a>)
2. Generate new token with <code>your-repo</code> selected, make sure Read and Write permission have been granted for:
	- Codespaces
	- Commit statuses
	- Contents
	- Issues
3. Copy the token and reply to this message

Example reply:
<code>ghp_1234567890abcdef1234567890abcdef12345678</code>

⚠️ <b>Security:</b> Your token will be stored securely and used only for repository operations. You’re free to revoke them at any time. Never share your token publicly!`

	msg := tgbotapi.NewMessage(message.Chat.ID, instructionMsg)
	msg.ParseMode = consts.ParseModeHTML
	msg.DisableWebPagePreview = true
	msg.ReplyMarkup = tgbotapi.ForceReply{ForceReply: true, Selective: true}

	if _, err := b.rateLimitedSend(message.Chat.ID, msg); err != nil {
		return fmt.Errorf("failed to send repotoken instruction: %w", err)
	}

	return nil
}

func (b *Bot) handleLLMTokenCommand(message *tgbotapi.Message) error {
	instructionMsg := `🧠 <b>Set LLM API Token</b>

To enable AI features (automatic titles and hashtags), reply to this message with your LLM API token.

<b>Supported providers:</b>
• Deepseek (recommended)
• Gemini (Google AI)

<b>Format:</b> <code>provider:token:model</code>

<b>Examples:</b>
<code>deepseek:sk-1234567890abcdef:deepseek-chat</code>
<code>gemini:AIzaSy1234567890abcdef:gemini-2.5-flash</code>

<b>For backward compatibility, you can also use just the token:</b>
<code>sk-1234567890abcdef</code> (defaults to deepseek:deepseek-chat)

<b>To reset/clear your LLM token:</b>
<code>reset</code>

⚠️ <b>Note:</b> LLM features are optional. The bot works without AI, but messages will have generic titles.`

	msg := tgbotapi.NewMessage(message.Chat.ID, instructionMsg)
	msg.ParseMode = consts.ParseModeHTML
	msg.ReplyMarkup = tgbotapi.ForceReply{ForceReply: true, Selective: true}

	if _, err := b.rateLimitedSend(message.Chat.ID, msg); err != nil {
		return fmt.Errorf("failed to send llmtoken instruction: %w", err)
	}

	return nil
}

func (b *Bot) handleRepoCommand(message *tgbotapi.Message) error {
	// Ensure user exists in database if database is configured
	_, err := b.ensureUser(message)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	// Get repository usage status
	var repoStatusSection string
	var repoURL string
	var committer string
	var user *database.User

	if b.db != nil {
		// Get user from database
		var err error
		user, err = b.db.GetUserByChatID(message.Chat.ID)
		if err != nil {
			b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ Failed to get user: %v", err))
			return nil
		}

		if user != nil {
			repoURL = user.GitHubRepo
			committer = user.Committer
		}
	} else {
		// Single-user mode - use global config
		repoURL = b.config.GitHubRepo
		committer = b.config.CommitAuthor
	}

	// Format repository URL for display (username/reponame)
	var repoDisplayText string
	if repoURL != "" {
		owner, repo, err := parseGitHubRepoURL(repoURL)
		if err == nil {
			repoDisplayText = fmt.Sprintf(`<a href="%s">%s/%s</a>`, html.EscapeString(repoURL), html.EscapeString(owner), html.EscapeString(repo))
		} else {
			repoDisplayText = "❌ Invalid repository URL"
		}
	} else {
		repoDisplayText = "❌ Not configured"
	}

	// Get repository usage status with progress bar
	userGitHubProvider, err := b.getUserGitHubProvider(message.Chat.ID)
	if err != nil || repoURL == "" {
		// Repository not configured
		repoStatusSection = `🔴 <b>Repository not configured</b>
📊 Usage: Unknown
📁 Repository: ❌ Not set`
	} else {
		// Get premium level for proper calculations
		premiumLevel := 0
		if b.db != nil {
			premiumUser, err := b.db.GetPremiumUser(message.Chat.ID)
			if err == nil && premiumUser != nil && premiumUser.IsPremiumUser() {
				premiumLevel = premiumUser.Level
			}
		}

		// Get repository size information with caching
		var statusEmoji, sizeSource string

		sizeMB, percentage, fromCache, cacheExpiry, err := b.getRepositorySizeWithCache(message.Chat.ID, userGitHubProvider, premiumLevel)

		// Determine source based on provider type and cache status
		if fromCache {
			// Calculate time remaining until cache expires
			timeLeft := time.Until(cacheExpiry)
			var timeLeftStr string

			if timeLeft < 0 {
				// Cache has already expired (edge case)
				timeLeftStr = "expired"
			} else if timeLeft >= time.Minute {
				// More than 1 minute: show in minutes
				minutes := int(timeLeft.Minutes())
				if minutes == 1 {
					timeLeftStr = "1 min"
				} else {
					timeLeftStr = fmt.Sprintf("%d mins", minutes)
				}
			} else {
				// Less than 1 minute: show in seconds
				seconds := int(timeLeft.Seconds())
				if seconds <= 1 {
					timeLeftStr = "1 sec"
				} else {
					timeLeftStr = fmt.Sprintf("%d secs", seconds)
				}
			}

			sizeSource = fmt.Sprintf("(Cached, %s left)", timeLeftStr)
		} else if userGitHubProvider.GetProviderType() == github.ProviderTypeAPI {
			sizeSource = "(GitHub API)"
		} else if userGitHubProvider.NeedsClone() {
			sizeSource = "(Remote API)"
		} else {
			sizeSource = "(Actual cloned size)"
		}

		// Get max size for display
		var maxSizeMB float64
		if premiumLevel > 0 {
			maxSizeMB = userGitHubProvider.GetRepositoryMaxSizeWithPremium(premiumLevel)
		} else {
			maxSizeMB = userGitHubProvider.GetRepositoryMaxSize()
		}

		if err != nil {
			repoStatusSection = "❌ Failed to get repository size info"
		} else {
			// Create progress bar
			progressBar := createProgressBarWithLen(percentage, 12)

			// Format status emoji based on usage
			if percentage < 50 {
				statusEmoji = "🟢"
			} else if percentage < 80 {
				statusEmoji = "🟡"
			} else {
				statusEmoji = "🔴"
			}

			repoStatusSection = fmt.Sprintf(`%s <b>%.1f%% used</b>
📊 %.2f MB / %.1f MB
%s
<i>Size Source: %s</i>`, statusEmoji, percentage, sizeMB, maxSizeMB, progressBar, sizeSource)
		}
	}

	// Format committer info
	var committerText string
	if committer != "" {
		committerText = html.EscapeString(committer)
	} else {
		defaultCommitter := b.config.CommitAuthor // Show default from config
		if defaultCommitter == "" {
			committerText = "❌ Not configured"
		} else {
			committerText = fmt.Sprintf("%s <i>(default)</i>", html.EscapeString(defaultCommitter))
		}
	}

	// Format GitHub token status
	var tokenStatusText string
	var githubToken string

	if b.db != nil {
		// Database mode - get token from user record
		if user != nil {
			githubToken = user.GitHubToken
		}
	} else {
		// Single-user mode - use global config
		githubToken = b.config.GitHubToken
	}

	if githubToken != "" {
		tokenStatusText = "✅ <b>Configured</b>"
	} else {
		tokenStatusText = "❌ <b>Not configured</b>"
	}

	// Build website links if BASE_URL is configured
	var websiteLinks string
	if b.config.BaseURL != "" {
		websiteLinks = fmt.Sprintf(`

<b>🌐 Resources:</b>
• <a href="%s">Homepage</a> | <a href="%s/privacy">Privacy</a>`, b.config.BaseURL, b.config.BaseURL)
	}

	// Create the main message
	repoMsg := fmt.Sprintf(`📁 <b>Repository Information</b>

<b>📊 Usage Status:</b>
%s

<b>🔗 Repository:</b>
%s

<b>🔑 GitHub Token:</b>
%s

<b>👤 Committer:</b>
%s%s`,
		repoStatusSection,
		repoDisplayText,
		tokenStatusText,
		committerText,
		websiteLinks)

	// Create inline keyboard - include OAuth button only if configured
	var keyboardRows [][]tgbotapi.InlineKeyboardButton

	// Add GitHub OAuth button if configured
	authRow := make([]tgbotapi.InlineKeyboardButton, 0, 2)
	if b.config.HasGitHubOAuthConfig() {
		authRow = append(authRow, tgbotapi.NewInlineKeyboardButtonData(consts.ButtonGitHubOAuth, "github_oauth"))
	}
	authRow = append(authRow, tgbotapi.NewInlineKeyboardButtonData(consts.ButtonSetRepoToken, "repo_set_token"))

	// Add manual setup buttons
	keyboardRows = append(keyboardRows,
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(consts.ButtonSetRepo, "repo_set_repo"),
			tgbotapi.NewInlineKeyboardButtonData(consts.ButtonSetCommitter, "repo_set_committer"),
		),
		tgbotapi.NewInlineKeyboardRow(authRow...),
	)

	// Add revoke auth button if GitHub token is configured
	if githubToken != "" {
		keyboardRows = append(keyboardRows,
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(consts.ButtonRevokeAuth, "repo_revoke_auth"),
			),
		)
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)

	msg := tgbotapi.NewMessage(message.Chat.ID, repoMsg)
	msg.ParseMode = consts.ParseModeHTML
	msg.ReplyMarkup = keyboard
	msg.DisableWebPagePreview = true

	if _, err := b.rateLimitedSend(message.Chat.ID, msg); err != nil {
		return fmt.Errorf("failed to send repo info message: %w", err)
	}

	return nil
}

func (b *Bot) handleCommitterCommand(message *tgbotapi.Message) error {
	// Ensure user exists in database if database is configured
	_, err := b.ensureUser(message)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	if b.db == nil {
		b.sendResponse(message.Chat.ID, "❌ Committer feature requires database configuration")
		return nil
	}

	// Show committer configuration (available to all users)
	instructionMsg := `👤 <b>Custom Commit Author</b>

You can customize the author information for your commits.

Reply to this message with your desired commit author in the format:
<code>Name &lt;email@example.com&gt;</code>

Examples:
<code>John Doe &lt;john@example.com&gt;</code>
<code>Jane Smith &lt;jane.smith@company.com&gt;</code>

<i>This will be used for all future commits from your account.</i>`

	msg := tgbotapi.NewMessage(message.Chat.ID, instructionMsg)
	msg.ParseMode = consts.ParseModeHTML
	msg.ReplyMarkup = tgbotapi.ForceReply{ForceReply: true, Selective: true}

	if _, err := b.rateLimitedSend(message.Chat.ID, msg); err != nil {
		return fmt.Errorf("failed to send committer instruction: %w", err)
	}

	return nil
}

// Reply handlers for configuration commands

func (b *Bot) handleSetRepoReply(message *tgbotapi.Message) error {
	repoURL := strings.TrimSpace(message.Text)

	// Basic validation
	if !strings.HasPrefix(repoURL, "https://github.com/") {
		b.sendResponse(message.Chat.ID, fmt.Sprintf("%s Invalid repository URL. Please use format: https://github.com/username/repository", consts.EmojiError))
		return nil
	}

	// Extract username and repo name from URL
	parts := strings.Split(strings.TrimPrefix(repoURL, "https://github.com/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		b.sendResponse(message.Chat.ID, fmt.Sprintf("%s Invalid repository URL format. Please use: https://github.com/username/repository", consts.EmojiError))
		return nil
	}

	username := parts[0]
	repoName := parts[1]

	// Ensure user exists in database if database is configured
	_, err := b.ensureUser(message)
	if err != nil && b.db != nil {
		b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ Failed to get user: %v", err))
		return nil
	}

	if b.db != nil {
		// Update user's GitHub repository in database
		currentUser, err := b.db.GetUserByChatID(message.Chat.ID)
		if err != nil {
			b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ Failed to get user: %v", err))
			return nil
		}

		// Get current token to preserve it
		currentToken := ""
		if currentUser != nil {
			currentToken = currentUser.GitHubToken
		}

		// Update GitHub config in database
		if err := b.db.UpdateUserGitHubConfig(message.Chat.ID, currentToken, repoURL); err != nil {
			b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ Failed to update repository configuration: %v", err))
			return nil
		}

		// Invalidate cached GitHub provider since repository configuration changed
		cacheKey := fmt.Sprintf("github_provider_%d", message.Chat.ID)
		b.cache.Delete(cacheKey)

		logger.Info("Repository configuration updated for user", map[string]interface{}{
			"chat_id":   message.Chat.ID,
			"repo_url":  repoURL,
			"username":  username,
			"repo_name": repoName,
		})
		successMsg := fmt.Sprintf("%s Repository updated to: %s/%s\n\n%s Configuration saved to database.", consts.EmojiSuccess, username, repoName, consts.EmojiPremium)
		b.sendResponse(message.Chat.ID, successMsg)
	} else {
		// Fallback to single-user mode (update global config)
		if err := b.updateGitHubRepo(repoURL, username, message.Chat.ID); err != nil {
			b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ Failed to update repository configuration: %v", err))
			return nil
		}

		logger.Info("Repository configuration updated globally", map[string]interface{}{
			"repo_url":  repoURL,
			"username":  username,
			"repo_name": repoName,
		})
		successMsg := fmt.Sprintf("%s Repository updated to: %s/%s\n\n%s Note: Configuration is stored temporarily. For permanent storage, update your .env file:\nGITHUB_REPO=%s\nGITHUB_USERNAME=%s", consts.EmojiSuccess, username, repoName, consts.EmojiWarning, repoURL, username)
		b.sendResponse(message.Chat.ID, successMsg)
	}

	return nil
}

func (b *Bot) handleRepoTokenReply(message *tgbotapi.Message) error {
	token := strings.TrimSpace(message.Text)

	// Basic validation
	if len(token) < 20 || (!strings.HasPrefix(token, "ghp_") && !strings.HasPrefix(token, "github_pat_")) {
		b.sendResponse(message.Chat.ID, fmt.Sprintf("%s Invalid GitHub token format. Token should start with 'ghp_' or 'github_pat_' and be at least 20 characters long.", consts.EmojiError))
		return nil
	}

	// Validate the token by making a test API call
	if err := b.validateGitHubToken(token); err != nil {
		b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ Invalid GitHub token: %v", err))
		return nil
	}

	// Ensure user exists in database if database is configured
	_, err := b.ensureUser(message)
	if err != nil && b.db != nil {
		b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ Failed to get user: %v", err))
		return nil
	}

	if b.db != nil {
		// Update user's GitHub token in database
		currentUser, err := b.db.GetUserByChatID(message.Chat.ID)
		if err != nil {
			b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ Failed to get user: %v", err))
			return nil
		}

		// Get current repo to preserve it
		currentRepo := ""
		if currentUser != nil {
			currentRepo = currentUser.GitHubRepo
		}

		// Update GitHub config in database
		if err := b.db.UpdateUserGitHubConfig(message.Chat.ID, token, currentRepo); err != nil {
			b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ Failed to update GitHub configuration: %v", err))
			return nil
		}

		// Invalidate cached GitHub provider since token configuration changed
		cacheKey := fmt.Sprintf("github_provider_%d", message.Chat.ID)
		b.cache.Delete(cacheKey)

		successMsg := fmt.Sprintf("%s GitHub token has been updated and validated!\n\n%s Configuration saved to database.", consts.EmojiSuccess, consts.EmojiPremium)
		b.sendResponse(message.Chat.ID, successMsg)
	} else {
		// Fallback to single-user mode (update global config)
		if err := b.updateGitHubToken(token, message.Chat.ID); err != nil {
			b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ Failed to update GitHub configuration: %v", err))
			return nil
		}

		successMsg := fmt.Sprintf("%s GitHub token has been updated and validated!\n\n%s Note: Configuration is stored temporarily. For permanent storage, update your .env file:\nGITHUB_TOKEN=%s...", consts.EmojiSuccess, consts.EmojiWarning, token[:8])
		b.sendResponse(message.Chat.ID, successMsg)
	}

	return nil
}

func (b *Bot) handleLLMTokenReply(message *tgbotapi.Message) error {
	input := strings.TrimSpace(message.Text)

	// Check for reset command
	if strings.ToLower(input) == "reset" {
		return b.handleLLMTokenReset(message)
	}

	// Parse input - can be just token or provider:token:model format
	var provider, token, model, endpoint string

	if strings.Contains(input, ":") {
		parts := strings.Split(input, ":")
		if len(parts) >= 2 {
			provider = parts[0]
			token = parts[1]
			if len(parts) >= 3 {
				model = parts[2]
			}
		}
	} else {
		token = input
		// Default to deepseek if not specified
		provider = "deepseek"
		model = "deepseek-chat"
	}

	// Validate provider
	providerLower := strings.ToLower(provider)
	if providerLower != "deepseek" && providerLower != "gemini" {
		b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ Unsupported provider: %s\n\nSupported providers:\n• deepseek\n• gemini", provider))
		return nil
	}

	// Set default model based on provider
	switch providerLower {
	case "deepseek":
		if model == "" {
			model = "deepseek-chat"
		}
		endpoint = "https://api.deepseek.com/v1"
	case "gemini":
		if model == "" {
			model = "gemini-2.5-flash"
		}
		endpoint = "https://generativelanguage.googleapis.com/v1beta"
	}

	// Basic validation
	if len(token) < 10 {
		b.sendResponse(message.Chat.ID, fmt.Sprintf("%s Invalid token format. Token should be at least 10 characters long.", consts.EmojiError))
		return nil
	}

	// Validate the token by making a test API call
	if err := b.validateLLMToken(provider, endpoint, token, model); err != nil {
		b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ Invalid LLM token or configuration: %v, %v", err, endpoint))
		return nil
	}

	// Ensure user exists in database if database is configured
	_, err := b.ensureUser(message)
	if err != nil && b.db != nil {
		b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ Failed to get user: %v", err))
		return nil
	}

	if b.db != nil {
		// Create full token format for storage: provider:token:model
		fullTokenFormat := fmt.Sprintf("%s:%s:%s", provider, token, model)

		// Update user's LLM config in database
		if err := b.db.UpdateUserLLMConfig(message.Chat.ID, fullTokenFormat); err != nil {
			b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ Failed to update LLM configuration: %v", err))
			return nil
		}

		// Auto-enable LLM switch when user sets personal LLM token
		if err := b.db.UpdateUserLLMSwitch(message.Chat.ID, true); err != nil {
			logger.Error("Failed to auto-enable LLM switch", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": message.Chat.ID,
			})
			// Don't fail the whole operation, just log the error
		}

		var successMsg string
		if provider != "" && model != "" {
			successMsg = fmt.Sprintf("%s LLM configuration updated and validated!\nProvider: %s\nModel: %s\nToken: %s...\n\n%s Configuration saved to database.\n✅ LLM processing automatically enabled!",
				consts.EmojiSuccess, provider, model, token[:8], consts.EmojiPremium)
		} else {
			successMsg = fmt.Sprintf("%s LLM token has been updated!\nToken: %s...\n\n%s Configuration saved to database.\n✅ LLM processing automatically enabled!",
				consts.EmojiSuccess, token[:8], consts.EmojiPremium)
		}
		b.sendResponse(message.Chat.ID, successMsg)
	} else {
		// Fallback to single-user mode (update global config)
		if err := b.updateLLMConfig(provider, endpoint, token, model); err != nil {
			b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ Failed to update LLM configuration: %v", err))
			return nil
		}

		var successMsg string
		if provider != "" && model != "" {
			successMsg = fmt.Sprintf("%s LLM configuration updated and validated!\nProvider: %s\nModel: %s\nToken: %s...\n\n%s Note: Configuration is stored temporarily. For permanent storage, update your .env file:\nLLM_PROVIDER=%s\nLLM_ENDPOINT=%s\nLLM_TOKEN=%s...\nLLM_MODEL=%s",
				consts.EmojiSuccess, provider, model, token[:8], consts.EmojiWarning, provider, endpoint, token[:8], model)
		} else {
			successMsg = fmt.Sprintf("%s LLM token has been updated!\nToken: %s...\n\n%s Note: Configuration is stored temporarily. For permanent storage, update your .env file:\nLLM_TOKEN=%s...",
				consts.EmojiSuccess, token[:8], consts.EmojiWarning, token[:8])
		}
		b.sendResponse(message.Chat.ID, successMsg)
	}
	return nil
}

func (b *Bot) handleLLMTokenReset(message *tgbotapi.Message) error {
	// Ensure user exists in database if database is configured
	_, err := b.ensureUser(message)
	if err != nil && b.db != nil {
		b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ Failed to get user: %v", err))
		return nil
	}

	if b.db != nil {
		// Clear user's LLM token in database
		if err := b.db.UpdateUserLLMConfig(message.Chat.ID, ""); err != nil {
			b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ Failed to reset LLM configuration: %v", err))
			return nil
		}

		successMsg := fmt.Sprintf(`%s LLM token has been reset successfully!

✨ Personal AI is now disabled, using free tokens instead. To re-enable, use /llm with a new token.

%s Configuration cleared from database.`, consts.EmojiSuccess, consts.EmojiPremium)
		b.sendResponse(message.Chat.ID, successMsg)
	} else {
		// Fallback to single-user mode (clear global config)
		if err := b.updateLLMConfig("", "", "", ""); err != nil {
			b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ Failed to reset LLM configuration: %v", err))
			return nil
		}

		successMsg := fmt.Sprintf(`%s LLM token has been reset successfully!

✨ Personal AI is now disabled, using free tokens instead. To re-enable, use /llm with a new token.`, consts.EmojiSuccess)
		b.sendResponse(message.Chat.ID, successMsg)
	}

	return nil
}

func (b *Bot) handleCommitterReply(message *tgbotapi.Message) error {
	committerInput := strings.TrimSpace(message.Text)

	// Basic validation of the format: "Name <email@example.com>"
	if !strings.Contains(committerInput, "<") || !strings.Contains(committerInput, ">") || !strings.Contains(committerInput, "@") {
		b.sendResponse(message.Chat.ID, fmt.Sprintf("%s Invalid format. Please use: Name <email@example.com>", consts.EmojiError))
		return nil
	}

	// Extract name and email
	parts := strings.Split(committerInput, "<")
	if len(parts) != 2 {
		b.sendResponse(message.Chat.ID, fmt.Sprintf("%s Invalid format. Please use: Name <email@example.com>", consts.EmojiError))
		return nil
	}

	name := strings.TrimSpace(parts[0])
	emailPart := strings.TrimSpace(parts[1])

	if !strings.HasSuffix(emailPart, ">") {
		b.sendResponse(message.Chat.ID, fmt.Sprintf("%s Invalid format. Please use: Name <email@example.com>", consts.EmojiError))
		return nil
	}

	email := strings.TrimSuffix(emailPart, ">")

	// Basic email validation
	if !strings.Contains(email, "@") || !strings.Contains(email, ".") {
		b.sendResponse(message.Chat.ID, fmt.Sprintf("%s Invalid email format", consts.EmojiError))
		return nil
	}

	if len(name) < 1 {
		b.sendResponse(message.Chat.ID, fmt.Sprintf("%s Name cannot be empty", consts.EmojiError))
		return nil
	}

	// Ensure user exists
	_, err := b.ensureUser(message)
	if err != nil {
		b.sendResponse(message.Chat.ID, fmt.Sprintf("❌ Failed to get user: %v", err))
		return nil
	}

	if b.db == nil {
		b.sendResponse(message.Chat.ID, "❌ Committer feature requires database configuration")
		return nil
	}

	// Format the committer string and save to database
	committerString := fmt.Sprintf("%s <%s>", name, email)

	// Update the user's committer field in users table
	if err := b.db.UpdateUserCommitter(message.Chat.ID, committerString); err != nil {
		logger.Error("Failed to update committer in database", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": message.Chat.ID,
		})
		b.sendResponse(message.Chat.ID, "❌ Failed to save committer setting")
		return nil
	}

	successMsg := fmt.Sprintf(`✅ <b>Commit Author Updated</b>

<b>Name:</b> %s
<b>Email:</b> %s

<i>This setting will be used for all future commits from your account.</i>

✨ <b>Setting saved to database successfully!</b>`, name, email)

	msg := tgbotapi.NewMessage(message.Chat.ID, successMsg)
	msg.ParseMode = consts.ParseModeHTML

	if _, err := b.rateLimitedSend(message.Chat.ID, msg); err != nil {
		logger.Error("Failed to send committer success message", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": message.Chat.ID,
		})
		b.sendResponse(message.Chat.ID, "❌ Failed to update commit author setting")
	}

	return nil
}

func (b *Bot) handleLLMCommand(message *tgbotapi.Message) error {
	if b.db == nil {
		b.sendResponse(message.Chat.ID, "❌ LLM status feature requires database configuration")
		return nil
	}

	// Get current user
	user, err := b.ensureUser(message)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	// Generate LLM status message and keyboard using helper function
	statusMsg, keyboard := b.generateLLMStatusMessage(user, message.Chat.ID)

	msg := tgbotapi.NewMessage(message.Chat.ID, statusMsg)
	msg.ParseMode = consts.ParseModeHTML
	msg.ReplyMarkup = keyboard

	if _, err := b.rateLimitedSend(message.Chat.ID, msg); err != nil {
		logger.Error("Failed to send LLM status message", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": message.Chat.ID,
		})
		b.sendResponse(message.Chat.ID, "❌ Failed to send LLM status")
	}

	return nil
}

func (b *Bot) handleLLMOnCommand(message *tgbotapi.Message) error {
	if b.db == nil {
		b.sendResponse(message.Chat.ID, "❌ LLM switch feature requires database configuration")
		return nil
	}

	// Get current user
	user, err := b.ensureUser(message)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	// Check if already enabled
	if user.LLMSwitch {
		statusMsg := `✅ <b>AI Processing Already ON</b>

💬 AI is already generating titles and hashtags for your messages!

<b>Controls:</b> /llm for status, /llmoff to disable`
		b.sendResponse(message.Chat.ID, statusMsg)
		return nil
	}

	// Enable LLM switch
	if err := b.db.UpdateUserLLMSwitch(message.Chat.ID, true); err != nil {
		logger.Error("Failed to enable LLM switch", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": message.Chat.ID,
		})
		b.sendResponse(message.Chat.ID, "❌ Failed to enable AI processing")
		return nil
	}

	// Get token usage info
	tokenInfoText, tokenPercentage := b.getDetailedTokenUsageInfo(message.Chat.ID)
	progressBar := createProgressBarWithLen(tokenPercentage, 6)

	successMsg := fmt.Sprintf(`✅ <b>AI Processing Enabled!</b>

🎉 <b>What's new:</b>
• AI will generate smart titles for your messages
• Automatic hashtags will be added
• Better organization and searchability

📊 <b>Your Usage:</b>
%s %s

<i>Use /llm to check status, /llmoff to disable</i>`,
		tokenInfoText, progressBar)

	msg := tgbotapi.NewMessage(message.Chat.ID, successMsg)
	msg.ParseMode = consts.ParseModeHTML

	if _, err := b.rateLimitedSend(message.Chat.ID, msg); err != nil {
		logger.Error("Failed to send LLM enable message", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": message.Chat.ID,
		})
		b.sendResponse(message.Chat.ID, "❌ Failed to send LLM enable confirmation")
	}

	return nil
}

func (b *Bot) handleLLMOffCommand(message *tgbotapi.Message) error {
	if b.db == nil {
		b.sendResponse(message.Chat.ID, "❌ LLM switch feature requires database configuration")
		return nil
	}

	// Get current user
	user, err := b.ensureUser(message)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	// Check if already disabled
	if !user.LLMSwitch {
		statusMsg := `❌ <b>AI Processing Already OFF</b>

💬 AI processing is already disabled. Messages are stored without AI enhancements.

<b>Controls:</b> /llm for status, /llmon to enable`
		b.sendResponse(message.Chat.ID, statusMsg)
		return nil
	}

	// Disable LLM switch
	if err := b.db.UpdateUserLLMSwitch(message.Chat.ID, false); err != nil {
		logger.Error("Failed to disable LLM switch", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": message.Chat.ID,
		})
		b.sendResponse(message.Chat.ID, "❌ Failed to disable AI processing")
		return nil
	}

	successMsg := `❌ <b>AI Processing Disabled</b>

💾 <b>What changed:</b>
• No AI-generated titles
• No automatic hashtags
• Messages stored as-is

📊 <b>Token usage:</b> Paused (no consumption)

<i>Use /llm to check status, /llmon to enable</i>`

	msg := tgbotapi.NewMessage(message.Chat.ID, successMsg)
	msg.ParseMode = consts.ParseModeHTML

	if _, err := b.rateLimitedSend(message.Chat.ID, msg); err != nil {
		logger.Error("Failed to send LLM disable message", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": message.Chat.ID,
		})
		b.sendResponse(message.Chat.ID, "❌ Failed to send LLM disable confirmation")
	}

	return nil
}

func getEffectText(enabled bool) string {
	if enabled {
		return "AI will generate titles and hashtags for your messages"
	}
	return "Messages will be stored without AI processing"
}

func (b *Bot) getTokenUsageInfo(chatID int64) string {
	if b.db == nil {
		return ""
	}

	// Get user's current token usage
	usage, err := b.db.GetUserUsage(chatID)
	if err != nil {
		return ""
	}

	// Get user's premium level
	premiumLevel := 0
	premiumUser, err := b.db.GetPremiumUser(chatID)
	if err == nil && premiumUser != nil && premiumUser.IsPremiumUser() {
		premiumLevel = premiumUser.Level
	}

	// Calculate token limit
	tokenLimit := database.GetTokenLimit(premiumLevel)

	var currentUsage int64
	if usage != nil {
		currentUsage = usage.TokenInput + usage.TokenOutput
	}

	// Format token usage with millions format
	usageText := formatTokenCount(currentUsage)
	limitText := formatTokenCount(tokenLimit)

	return fmt.Sprintf(` %s/%s tokens`, usageText, limitText)
}

// getDetailedTokenUsageInfo returns both usage text and percentage for detailed display
func (b *Bot) getDetailedTokenUsageInfo(chatID int64) (string, float64) {
	if b.db == nil {
		return "No database connection", 0
	}

	// Get user's current token usage
	usage, err := b.db.GetUserUsage(chatID)
	if err != nil {
		return "Error getting usage", 0
	}

	// Get user's premium level
	premiumLevel := 0
	premiumUser, err := b.db.GetPremiumUser(chatID)
	if err == nil && premiumUser != nil && premiumUser.IsPremiumUser() {
		premiumLevel = premiumUser.Level
	}

	// Calculate token limit
	tokenLimit := database.GetTokenLimit(premiumLevel)

	var currentUsage int64
	if usage != nil {
		currentUsage = usage.TokenInput + usage.TokenOutput
	}

	// Calculate percentage
	percentage := float64(currentUsage) / float64(tokenLimit) * 100

	// Format token usage with millions format
	usageText := formatTokenCount(currentUsage)
	limitText := formatTokenCount(tokenLimit)

	return fmt.Sprintf("%s / %s tokens", usageText, limitText), percentage
}

// handleLLMEnableCallback handles the enable AI processing button click
func (b *Bot) handleLLMEnableCallback(callback *tgbotapi.CallbackQuery) error {
	if b.db == nil {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ LLM switch feature requires database configuration")
		return nil
	}

	// Enable LLM switch (idempotent operation)
	if err := b.db.UpdateUserLLMSwitch(callback.Message.Chat.ID, true); err != nil {
		logger.Error("Failed to enable LLM switch", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Failed to enable AI processing")
		return nil
	}

	// Get fresh user data after update
	updatedUser, err := b.db.GetUserByChatID(callback.Message.Chat.ID)
	if err != nil {
		logger.Error("Failed to get updated user", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Failed to get user information")
		return nil
	}

	// Generate the LLM status message and keyboard
	statusMsg, keyboard := b.generateLLMStatusMessage(updatedUser, callback.Message.Chat.ID)

	// Edit message to show main /llm menu
	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, statusMsg)
	editMsg.ParseMode = consts.ParseModeHTML
	editMsg.ReplyMarkup = &keyboard

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		logger.Error("Failed to edit message after enabling LLM", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
	}

	return nil
}

// handleLLMDisableCallback handles the disable AI processing button click
func (b *Bot) handleLLMDisableCallback(callback *tgbotapi.CallbackQuery) error {
	if b.db == nil {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ LLM switch feature requires database configuration")
		return nil
	}

	// Disable LLM switch (idempotent operation)
	if err := b.db.UpdateUserLLMSwitch(callback.Message.Chat.ID, false); err != nil {
		logger.Error("Failed to disable LLM switch", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Failed to disable AI processing")
		return nil
	}

	// Get fresh user data after update
	updatedUser, err := b.db.GetUserByChatID(callback.Message.Chat.ID)
	if err != nil {
		logger.Error("Failed to get updated user", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Failed to get user information")
		return nil
	}

	// Generate the LLM status message and keyboard
	statusMsg, keyboard := b.generateLLMStatusMessage(updatedUser, callback.Message.Chat.ID)

	// Edit message to show main /llm menu
	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, statusMsg)
	editMsg.ParseMode = consts.ParseModeHTML
	editMsg.ReplyMarkup = &keyboard

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		logger.Error("Failed to edit message after disabling LLM", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
	}

	return nil
}

// handleLLMSetTokenCallback handles the set personal LLM token button click
func (b *Bot) handleLLMSetTokenCallback(callback *tgbotapi.CallbackQuery) error {
	if b.db == nil {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ LLM token feature requires database configuration")
		return nil
	}

	// Send force reply message to get LLM token
	forceReplyMsg := `🔑 <b>Set Personal LLM Token</b>

To enable AI features with your own token, reply to this message with your LLM API token.

<b>Supported providers:</b>
• Deepseek (recommended)
• Gemini (Google AI)

<b>Format:</b> <code>provider:token:model</code>

<b>Examples:</b>
<code>deepseek:sk-1234567890abcdef:deepseek-chat</code>
<code>gemini:AIzaSy1234567890abcdef:gemini-2.5-flash</code>

<b>You can also directly use deepseek token:</b>
<code>sk-1234567890abcdef</code> (defaults to deepseek:deepseek-chat)

<b>To reset/clear personal token:</b> Send <code>reset</code>

⚠️ <b>Note:</b> Your token will be stored securely and used only for AI processing.`

	msg := tgbotapi.NewMessage(callback.Message.Chat.ID, forceReplyMsg)
	msg.ParseMode = consts.ParseModeHTML
	msg.ReplyMarkup = tgbotapi.ForceReply{
		ForceReply:            true,
		InputFieldPlaceholder: "Enter your LLM token...",
		Selective:             true,
	}

	sentMsg, err := b.rateLimitedSend(callback.Message.Chat.ID, msg)
	if err != nil {
		logger.Error("Failed to send LLM token setup message", map[string]interface{}{
			"error": err.Error(),
		})
		return err
	}

	// Store the message context for later processing
	messageKey := fmt.Sprintf("llm_token_%d_%d", callback.Message.Chat.ID, sentMsg.MessageID)
	b.pendingMessages[messageKey] = "llm_token_setup"

	return nil
}

// handleLLMResetTokenCallback handles the reset personal LLM token button click
func (b *Bot) handleLLMResetTokenCallback(callback *tgbotapi.CallbackQuery) error {
	if b.db == nil {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ LLM token feature requires database configuration")
		return nil
	}

	// Get current user
	user, err := b.ensureUser(callback.Message)
	if err != nil {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Failed to get user information")
		return nil
	}

	// Clear user's LLM token in database
	if err := b.db.UpdateUserLLMConfig(callback.Message.Chat.ID, ""); err != nil {
		logger.Error("Failed to reset LLM token", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Failed to reset LLM token")
		return nil
	}

	// Get token usage info
	tokenInfoText, tokenPercentage := b.getDetailedTokenUsageInfo(callback.Message.Chat.ID)
	progressBar := createProgressBarWithLen(tokenPercentage, 6)

	// Update status after reset
	personalLLMStatus := "❌ <b>No personal LLM token</b>"
	usingText := "\n\n💡 <i>Using shared default LLM service</i>"

	// Determine current LLM switch status
	var statusMsg string
	var keyboardRows [][]tgbotapi.InlineKeyboardButton

	if user.LLMSwitch {
		statusMsg = fmt.Sprintf(`🧠 <b>AI Processing: ON</b> ✅

📊 <b>Default LLM Usage:</b>
%s %s

🔑 <b>Token Status:</b>
%s%s

✅ <b>LLM token reset successfully!</b>

<i>💬 AI generates titles and hashtags for your messages</i>`,
			tokenInfoText, progressBar, personalLLMStatus, usingText)

		keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Disable AI Processing", "llm_disable"),
		))
	} else {
		statusMsg = fmt.Sprintf(`🧠 <b>AI Processing: OFF</b> ❌

📊 <b>Default LLM Usage:</b>
%s %s

🔑 <b>Token Status:</b>
%s%s

✅ <b>LLM token reset successfully!</b>

<i>💬 Messages stored without AI processing</i>`,
			tokenInfoText, progressBar, personalLLMStatus, usingText)

		keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Enable AI Processing", "llm_enable"),
		))
	}

	// Add set token button (since token was reset)
	keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔑 Set Personal LLM Token", "llm_set_token"),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, statusMsg)
	editMsg.ParseMode = consts.ParseModeHTML
	editMsg.ReplyMarkup = &keyboard

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		logger.Error("Failed to edit message after resetting LLM token", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
	}

	return nil
}

// Repository callback handlers

func (b *Bot) handleRepoSetRepoCallback(callback *tgbotapi.CallbackQuery) error {
	// Trigger the same flow as /setrepo command
	message := &tgbotapi.Message{
		Chat:      callback.Message.Chat,
		MessageID: callback.Message.MessageID,
		From:      callback.From,
	}
	return b.handleSetRepoCommand(message)
}

func (b *Bot) handleRepoSetTokenCallback(callback *tgbotapi.CallbackQuery) error {
	// Delete the privacy confirmation message first
	deleteMsg := tgbotapi.NewDeleteMessage(callback.Message.Chat.ID, callback.Message.MessageID)
	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, deleteMsg); err != nil {
		logger.Warn("Failed to delete privacy confirmation message", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		// Continue even if delete fails
	}

	// Trigger the same flow as /repotoken command
	message := &tgbotapi.Message{
		Chat:      callback.Message.Chat,
		MessageID: callback.Message.MessageID,
		From:      callback.From,
	}
	return b.handleRepoTokenCommand(message)
}

func (b *Bot) handleRepoSetCommitterCallback(callback *tgbotapi.CallbackQuery) error {
	// Trigger the same flow as /committer command
	message := &tgbotapi.Message{
		Chat:      callback.Message.Chat,
		MessageID: callback.Message.MessageID,
		From:      callback.From,
	}
	return b.handleCommitterCommand(message)
}

// handleLLMTokenSetupReply handles LLM token setup reply from the interactive button
func (b *Bot) handleLLMTokenSetupReply(message *tgbotapi.Message, context string) error {
	input := strings.TrimSpace(message.Text)

	// Check for cancel command
	if strings.ToLower(input) == "cancel" {
		b.sendResponse(message.Chat.ID, "❌ LLM token setup cancelled")
		return nil
	}

	// Use the existing LLM token reply handler logic
	return b.handleLLMTokenReply(message)
}

// handleRepoTokenPrivacyConfirmation shows privacy policy confirmation for repo token setup
func (b *Bot) handleRepoTokenPrivacyConfirmation(callback *tgbotapi.CallbackQuery) error {
	logger.Info("Repo token privacy confirmation requested", map[string]interface{}{
		"chat_id":  callback.Message.Chat.ID,
		"user_id":  callback.From.ID,
		"username": callback.From.UserName,
	})

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

	confirmationMsg := fmt.Sprintf(`🔑 <b>Set GitHub Personal Access Token</b>

⚠️ <b>Privacy Notice:</b>
By proceeding, you agree to our %s regarding secure storage of your GitHub personal access token.

<b>What we'll store:</b>
• Your GitHub personal access token (encrypted)
• Repository permissions you grant
• Token for repository operations only

<b>How we protect it:</b>
• Encrypted storage
• Used only for repository operations
• Never shared with third parties%s

<b>Ready to continue?</b>`, privacyText, privacyLink)

	// Create confirmation keyboard
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ I Accept & Continue", "repo_set_token_confirmed"),
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
		logger.Error("Failed to edit message with repo token privacy confirmation", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		return fmt.Errorf("failed to send repo token privacy confirmation: %w", err)
	}

	return nil
}

// handleRepoRevokeAuthCallback handles the revoke auth button click with confirmation
func (b *Bot) handleRepoRevokeAuthCallback(callback *tgbotapi.CallbackQuery) error {
	logger.Info("Repo revoke auth button clicked", map[string]interface{}{
		"chat_id":  callback.Message.Chat.ID,
		"user_id":  callback.From.ID,
		"username": callback.From.UserName,
	})

	// Check if database is configured
	if b.db == nil {
		notConfiguredMsg := "❌ <b>Database Required</b>\n\nRevoke auth feature requires database configuration."
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, notConfiguredMsg)
		editMsg.ParseMode = consts.ParseModeHTML

		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			return fmt.Errorf("failed to send database not configured message: %w", err)
		}
		return nil
	}

	// Get user to check if they have a token to revoke
	user, err := b.db.GetUserByChatID(callback.Message.Chat.ID)
	if err != nil {
		errorMsg := "❌ <b>Error</b>\n\nFailed to check your authentication status. Please try again."
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		editMsg.ParseMode = consts.ParseModeHTML

		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			return fmt.Errorf("failed to send error message: %w", err)
		}
		return nil
	}

	if user == nil || user.GitHubToken == "" {
		noTokenMsg := "❌ <b>No Authentication Found</b>\n\nYou don't have any GitHub authentication configured to revoke."
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, noTokenMsg)
		editMsg.ParseMode = consts.ParseModeHTML

		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			return fmt.Errorf("failed to send no token message: %w", err)
		}
		return nil
	}

	confirmationMsg := `🚫 <b>Revoke GitHub Authentication</b>

⚠️ <b>Warning:</b>
This action will remove your GitHub authentication from this bot.

<b>What will happen:</b>
• You will no longer be able to create commits
• You will need to re-authenticate to use repository features
• Your repository URL and other settings will remain unchanged

<b>This action cannot be undone.</b>

Are you sure you want to revoke your GitHub authentication?`

	// Create confirmation keyboard
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Yes, Revoke", "repo_revoke_auth_confirmed"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "repo_revoke_auth_cancel"),
		),
	)

	// Edit the original message to show revoke confirmation
	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, confirmationMsg)
	editMsg.ParseMode = consts.ParseModeHTML
	editMsg.ReplyMarkup = &keyboard

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		logger.Error("Failed to edit message with revoke auth confirmation", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		return fmt.Errorf("failed to send revoke auth confirmation: %w", err)
	}

	return nil
}

// handleRepoRevokeAuthConfirmed handles the actual revocation after confirmation
func (b *Bot) handleRepoRevokeAuthConfirmed(callback *tgbotapi.CallbackQuery) error {
	logger.Info("Repo revoke auth confirmed", map[string]interface{}{
		"chat_id":  callback.Message.Chat.ID,
		"user_id":  callback.From.ID,
		"username": callback.From.UserName,
	})

	// Check if database is configured
	if b.db == nil {
		notConfiguredMsg := "❌ <b>Database Required</b>\n\nRevoke auth feature requires database configuration."
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, notConfiguredMsg)
		editMsg.ParseMode = consts.ParseModeHTML

		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			return fmt.Errorf("failed to send database not configured message: %w", err)
		}
		return nil
	}

	// Get current user data to preserve repository URL
	user, err := b.db.GetUserByChatID(callback.Message.Chat.ID)
	if err != nil {
		errorMsg := "❌ <b>Error</b>\n\nFailed to revoke authentication. Please try again."
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		editMsg.ParseMode = consts.ParseModeHTML

		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			return fmt.Errorf("failed to send error message: %w", err)
		}
		return nil
	}

	if user == nil {
		noUserMsg := "❌ <b>User Not Found</b>\n\nUser record not found. Please try again."
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, noUserMsg)
		editMsg.ParseMode = consts.ParseModeHTML

		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			return fmt.Errorf("failed to send no user message: %w", err)
		}
		return nil
	}

	// Update user config with empty token (preserving repo URL) - idempotent operation
	err = b.db.UpdateUserGitHubConfig(callback.Message.Chat.ID, "", user.GitHubRepo)
	if err != nil {
		logger.Error("Failed to revoke GitHub authentication", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})

		errorMsg := "❌ <b>Revocation Failed</b>\n\nFailed to revoke your GitHub authentication. Please try again."
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		editMsg.ParseMode = consts.ParseModeHTML

		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			return fmt.Errorf("failed to send revocation failed message: %w", err)
		}
		return nil
	}

	// Invalidate cached GitHub provider since token has been revoked
	cacheKey := fmt.Sprintf("github_provider_%d", callback.Message.Chat.ID)
	b.cache.Delete(cacheKey)

	successMsg := `✅ <b>Authentication Revoked</b>

Your GitHub authentication has been successfully removed from this bot.

<b>What happened:</b>
• Repository access removed
• Repository URL preserved for future use

<b>To use repository features again:</b>
• Use /repo to re-authenticate with GitHub
• Choose either OAuth or manual token setup

<i>🔒 Your data has been securely removed.</i>`

	// Edit the original message to show success
	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, successMsg)
	editMsg.ParseMode = consts.ParseModeHTML

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		return fmt.Errorf("failed to send revocation success message: %w", err)
	}

	return nil
}

// handleRepoRevokeAuthCancel handles cancellation of revoke auth operation
func (b *Bot) handleRepoRevokeAuthCancel(callback *tgbotapi.CallbackQuery) error {
	logger.Info("Repo revoke auth cancelled", map[string]interface{}{
		"chat_id":  callback.Message.Chat.ID,
		"user_id":  callback.From.ID,
		"username": callback.From.UserName,
	})

	// Edit message to show cancellation
	cancelMsg := `❌ <b>Revoke Authentication Cancelled</b>

Your GitHub authentication remains active. No changes were made.

Use /repo to manage your repository settings.`

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, cancelMsg)
	editMsg.ParseMode = consts.ParseModeHTML

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		return fmt.Errorf("failed to edit cancel message: %w", err)
	}

	return nil
}

// handleLLMMultimodalEnableCallback handles the enable multimodal button click
func (b *Bot) handleLLMMultimodalEnableCallback(callback *tgbotapi.CallbackQuery) error {
	logger.Info("LLM multimodal enable button clicked", map[string]interface{}{
		"chat_id":  callback.Message.Chat.ID,
		"user_id":  callback.From.ID,
		"username": callback.From.UserName,
	})

	// Check if database is configured
	if b.db == nil {
		notConfiguredMsg := "❌ <b>Database Required</b>\n\nMultimodal feature requires database configuration."
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, notConfiguredMsg)
		editMsg.ParseMode = consts.ParseModeHTML

		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			return fmt.Errorf("failed to send database not configured message: %w", err)
		}
		return nil
	}

	// Update multimodal switch to enabled
	err := b.db.UpdateUserLLMMultimodalSwitch(callback.Message.Chat.ID, true)
	if err != nil {
		logger.Error("Failed to enable multimodal switch", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})

		errorMsg := "❌ <b>Failed to Enable Multimodal</b>\n\nPlease try again."
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		editMsg.ParseMode = consts.ParseModeHTML

		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			return fmt.Errorf("failed to send error message: %w", err)
		}
		return nil
	}

	// Get updated user data and show LLM status
	user, err := b.db.GetUserByChatID(callback.Message.Chat.ID)
	if err != nil {
		return fmt.Errorf("failed to get updated user data: %w", err)
	}

	// Generate and send updated LLM status message
	statusMsg, keyboard := b.generateLLMStatusMessage(user, callback.Message.Chat.ID)
	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, statusMsg)
	editMsg.ParseMode = consts.ParseModeHTML
	editMsg.DisableWebPagePreview = true
	editMsg.ReplyMarkup = &keyboard

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		return fmt.Errorf("failed to send updated LLM status: %w", err)
	}

	return nil
}

// handleLLMMultimodalDisableCallback handles the disable multimodal button click
func (b *Bot) handleLLMMultimodalDisableCallback(callback *tgbotapi.CallbackQuery) error {
	logger.Info("LLM multimodal disable button clicked", map[string]interface{}{
		"chat_id":  callback.Message.Chat.ID,
		"user_id":  callback.From.ID,
		"username": callback.From.UserName,
	})

	// Check if database is configured
	if b.db == nil {
		notConfiguredMsg := "❌ <b>Database Required</b>\n\nMultimodal feature requires database configuration."
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, notConfiguredMsg)
		editMsg.ParseMode = consts.ParseModeHTML

		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			return fmt.Errorf("failed to send database not configured message: %w", err)
		}
		return nil
	}

	// Update multimodal switch to disabled
	err := b.db.UpdateUserLLMMultimodalSwitch(callback.Message.Chat.ID, false)
	if err != nil {
		logger.Error("Failed to disable multimodal switch", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})

		errorMsg := "❌ <b>Failed to Disable Multimodal</b>\n\nPlease try again."
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		editMsg.ParseMode = consts.ParseModeHTML

		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			return fmt.Errorf("failed to send error message: %w", err)
		}
		return nil
	}

	// Get updated user data and show LLM status
	user, err := b.db.GetUserByChatID(callback.Message.Chat.ID)
	if err != nil {
		return fmt.Errorf("failed to get updated user data: %w", err)
	}

	// Generate and send updated LLM status message
	statusMsg, keyboard := b.generateLLMStatusMessage(user, callback.Message.Chat.ID)
	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, statusMsg)
	editMsg.ParseMode = consts.ParseModeHTML
	editMsg.DisableWebPagePreview = true
	editMsg.ReplyMarkup = &keyboard

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		return fmt.Errorf("failed to send updated LLM status: %w", err)
	}

	return nil
}

// generateLLMStatusMessage generates the LLM status message and keyboard for display
func (b *Bot) generateLLMStatusMessage(user *database.User, chatID int64) (string, tgbotapi.InlineKeyboardMarkup) {
	// Get current LLM switch status
	currentLLMSwitch := user.LLMSwitch
	currentMultimodalSwitch := user.LLMMultimodalSwitch

	// Get detailed token usage info for default LLM
	tokenInfoText, tokenPercentage := b.getDetailedTokenUsageInfo(chatID)

	// Check if user has personal LLM token
	var personalLLMStatus string
	var usingText string
	if user.LLMToken != "" {
		personalLLMStatus = "✅ <b>Personal LLM configured</b>"
		usingText = "\n\n💡 <i>You're using your personal LLM token</i>"
	} else {
		personalLLMStatus = "❌ <b>No personal LLM token</b>"
		usingText = "\n\n💡 <i>Using shared default LLM service</i>"
	}

	// Create progress bar for token usage
	progressBar := createProgressBarWithLen(tokenPercentage, 6)

	// Multimodal status text - check if actually usable
	var multimodalStatusText string
	if currentMultimodalSwitch {
		// Check if multimodal is actually available
		if b.shouldPerformMultimodalAnalysis(chatID, user) {
			multimodalStatusText = "📷 <b>Multimodal: ON</b> ✅ <i>(Available with Gemini)</i>"
		} else {
			// Switch is ON but not actually usable
			multimodalStatusText = "📷 <b>Multimodal: ON</b> ⚠️ <i>(Requires Gemini token)</i>"
		}
	} else {
		// Check if multimodal would be available if enabled
		userLLMClient, _ := b.getUserLLMClientWithUsageTracking(chatID, "")
		if userLLMClient != nil && userLLMClient.SupportsMultimodal() {
			multimodalStatusText = "📷 <b>Multimodal: OFF</b> ❌ <i>(Available with Gemini)</i>"
		} else {
			multimodalStatusText = "📷 <b>Multimodal: OFF</b> ❌ <i>(Requires Gemini token)</i>"
		}
	}

	var statusMsg string
	var keyboard tgbotapi.InlineKeyboardMarkup

	if currentLLMSwitch {
		statusMsg = fmt.Sprintf(`🧠 <b>AI Processing: ON</b> ✅

📊 <b>Default LLM Usage:</b>
%s %s

🔑 <b>Token Status:</b>
%s%s

%s

<i>💬 AI generates titles and hashtags for your messages</i>`,
			tokenInfoText,
			progressBar,
			personalLLMStatus,
			usingText,
			multimodalStatusText)

		// Create buttons: disable + multimodal toggle + token management
		var keyboardRows [][]tgbotapi.InlineKeyboardButton
		keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Disable AI Processing", "llm_disable"),
		))

		// Add multimodal toggle button
		if currentMultimodalSwitch {
			keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("📷 Disable Multimodal", "llm_multimodal_disable"),
			))
		} else {
			keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("📷 Enable Multimodal", "llm_multimodal_enable"),
			))
		}

		// Always show set personal LLM token button (users can reply with 'reset' to clear)
		keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔑 Set Personal LLM Token", "llm_set_token"),
		))

		keyboard = tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)
	} else {
		statusMsg = fmt.Sprintf(`🧠 <b>AI Processing: OFF</b> ❌

📊 <b>Default LLM Usage:</b>
%s %s

🔑 <b>Token Status:</b>
%s%s

%s

<i>💬 Messages stored without AI processing</i>`,
			tokenInfoText,
			progressBar,
			personalLLMStatus,
			usingText,
			multimodalStatusText)

		// Create buttons: enable + multimodal toggle + token management
		var keyboardRows [][]tgbotapi.InlineKeyboardButton
		keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Enable AI Processing", "llm_enable"),
		))

		// Add multimodal toggle button (available even when AI processing is off)
		if currentMultimodalSwitch {
			keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("📷 Disable Multimodal", "llm_multimodal_disable"),
			))
		} else {
			keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("📷 Enable Multimodal", "llm_multimodal_enable"),
			))
		}

		// Always show set personal LLM token button (users can reply with 'reset' to clear)
		keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔑 Set Personal LLM Token", "llm_set_token"),
		))

		keyboard = tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)
	}

	return statusMsg, keyboard
}
