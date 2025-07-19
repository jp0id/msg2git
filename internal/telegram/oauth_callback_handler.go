package telegram

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/msg2git/msg2git/internal/logger"
)

// GitHubOAuthResponse represents the response from GitHub's OAuth token endpoint
type GitHubOAuthResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

// GitHubUser represents the authenticated GitHub user
type GitHubUser struct {
	Login string `json:"login"`
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// GitHubRepo represents a GitHub repository
type GitHubRepo struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
	Private  bool   `json:"private"`
}

// HandleGitHubOAuthCallback handles the GitHub OAuth callback
// This should be called by your web server when it receives the callback
func (b *Bot) HandleGitHubOAuthCallback(w http.ResponseWriter, r *http.Request) {
	logger.Info("GitHub OAuth callback received", map[string]interface{}{
		"url":    r.URL.String(),
		"method": r.Method,
	})

	// Parse query parameters
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errorParam := r.URL.Query().Get("error")

	// Handle error cases
	if errorParam != "" {
		logger.Warn("GitHub OAuth error received", map[string]interface{}{
			"error": errorParam,
			"state": state,
		})

		// Redirect to error page
		redirectURL := "/auth-error?error=" + url.QueryEscape(errorParam)
		if b.config.BaseURL != "" {
			redirectURL = b.config.BaseURL + redirectURL
		}
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	}

	// Handle cancellation
	if code == "" {
		logger.Info("GitHub OAuth cancelled by user", map[string]interface{}{
			"state": state,
		})

		// Redirect to cancel page
		redirectURL := "/auth-cancel"
		if b.config.BaseURL != "" {
			redirectURL = b.config.BaseURL + redirectURL
		}
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	}

	// Validate state parameter and extract chat info
	chatID, _, err := b.parseOAuthState(state)
	if err != nil {
		logger.Error("Invalid OAuth state parameter", map[string]interface{}{
			"error": err.Error(),
			"state": state,
		})

		redirectURL := "/auth-error?error=invalid_state"
		if b.config.BaseURL != "" {
			redirectURL = b.config.BaseURL + redirectURL
		}
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	}

	// Exchange code for access token
	accessToken, err := b.exchangeOAuthCode(code)
	if err != nil {
		logger.Error("Failed to exchange OAuth code for token", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})

		redirectURL := "/auth-error?error=token_exchange_failed"
		if b.config.BaseURL != "" {
			redirectURL = b.config.BaseURL + redirectURL
		}
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	}

	// Get GitHub user info
	githubUser, err := b.getGitHubUser(accessToken)
	if err != nil {
		logger.Error("Failed to get GitHub user info", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})

		redirectURL := "/auth-error?error=user_info_failed"
		if b.config.BaseURL != "" {
			redirectURL = b.config.BaseURL + redirectURL
		}
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	}

	// Save access token to database
	err = b.saveGitHubTokenToDatabase(chatID, accessToken, githubUser)
	if err != nil {
		logger.Error("Failed to save GitHub token to database", map[string]interface{}{
			"error":       err.Error(),
			"chat_id":     chatID,
			"github_user": githubUser.Login,
		})

		redirectURL := "/auth-error?error=database_save_failed"
		if b.config.BaseURL != "" {
			redirectURL = b.config.BaseURL + redirectURL
		}
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	}

	// Send success notification to Telegram
	go b.sendOAuthSuccessNotification(chatID, githubUser)

	// Redirect to success page
	successURL := fmt.Sprintf("/auth-success?user=%s", url.QueryEscape(githubUser.Login))
	if b.config.BaseURL != "" {
		successURL = b.config.BaseURL + successURL
	}
	http.Redirect(w, r, successURL, http.StatusFound)
}

// parseOAuthState parses the state parameter to extract chat ID and user ID
func (b *Bot) parseOAuthState(state string) (chatID int64, userID int64, err error) {
	// Expected format: "telegram_{chatID}_{userID}"
	if !strings.HasPrefix(state, "telegram_") {
		return 0, 0, fmt.Errorf("invalid state format: missing telegram prefix")
	}

	parts := strings.Split(state, "_")
	if len(parts) != 3 {
		return 0, 0, fmt.Errorf("invalid state format: expected 3 parts, got %d", len(parts))
	}

	chatID, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid chat ID in state: %w", err)
	}

	userID, err = strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid user ID in state: %w", err)
	}

	return chatID, userID, nil
}

// exchangeOAuthCode exchanges the OAuth code for an access token
func (b *Bot) exchangeOAuthCode(code string) (string, error) {
	// Prepare token exchange request
	data := url.Values{}
	data.Set("client_id", b.config.GitHubOAuthClientID)
	data.Set("client_secret", b.config.GitHubOAuthClientSecret)
	data.Set("code", code)

	req, err := http.NewRequest("POST", "https://github.com/login/oauth/access_token", strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	// Make the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to exchange token: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var oauthResp GitHubOAuthResponse
	if err := json.Unmarshal(body, &oauthResp); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	if oauthResp.AccessToken == "" {
		return "", fmt.Errorf("no access token in response")
	}

	logger.Info("Successfully exchanged OAuth code for token", map[string]interface{}{
		"token_type": oauthResp.TokenType,
		"scope":      oauthResp.Scope,
	})

	return oauthResp.AccessToken, nil
}

// getGitHubUser gets the authenticated user's GitHub profile
func (b *Bot) getGitHubUser(accessToken string) (*GitHubUser, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/user", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create user request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read user response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("user info request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var user GitHubUser
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, fmt.Errorf("failed to parse user response: %w", err)
	}

	return &user, nil
}

// saveGitHubTokenToDatabase saves the GitHub auth token to the user's database record
func (b *Bot) saveGitHubTokenToDatabase(chatID int64, accessToken string, githubUser *GitHubUser) error {
	if b.db == nil {
		return fmt.Errorf("database not configured")
	}

	// Get or create user
	user, err := b.db.GetOrCreateUser(chatID, "")
	if err != nil {
		return fmt.Errorf("failed to get/create user: %w", err)
	}

	// Update user with GitHub token
	if err := b.db.UpdateUserGitHubConfig(chatID, accessToken, user.GitHubRepo); err != nil {
		return fmt.Errorf("failed to update GitHub token: %w", err)
	}

	// Invalidate cached GitHub provider since token configuration changed
	cacheKey := fmt.Sprintf("github_provider_%d", chatID)
	b.cache.Delete(cacheKey)

	// Optionally update committer info if not set
	if user.Committer == "" && githubUser.Name != "" && githubUser.Email != "" {
		committer := fmt.Sprintf("%s <%s>", githubUser.Name, githubUser.Email)
		if err := b.db.UpdateUserCommitter(chatID, committer); err != nil {
			logger.Warn("Failed to update committer info", map[string]interface{}{
				"chat_id": chatID,
				"error":   err.Error(),
			})
		}
	}

	logger.Info("Successfully saved GitHub token to database", map[string]interface{}{
		"chat_id":     chatID,
		"github_user": githubUser.Login,
		"github_id":   githubUser.ID,
	})

	return nil
}

// sendOAuthSuccessNotification sends a success notification to the user via Telegram
func (b *Bot) sendOAuthSuccessNotification(chatID int64, githubUser *GitHubUser) {
	successMsg := fmt.Sprintf(`✅ <b>GitHub OAuth Setup Complete!</b>

Your GitHub account <b>%s</b> has been successfully linked to Gitted Messages (Msg2Git).

<b>✨ What's configured:</b>
• GitHub Auth: ✅ Configured securely
• Account: <code>%s</code>
• Permissions: Repository read/write access

<b>🚀 Next steps:</b>
• Use /repo to settle your repo url
• Start sending messages to create content!

<i>🎉 You're all set to use Msg2Git with GitHub!</i>`, githubUser.Login, githubUser.Login)

	// Send notification message
	b.sendResponse(chatID, successMsg)
}
