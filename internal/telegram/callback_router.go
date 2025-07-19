package telegram

import (
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/msg2git/msg2git/internal/logger"
)

// getRepositoryMultiplier returns the correct repository size multiplier for a premium level
func getRepositoryMultiplier(level int) int {
	switch level {
	case 1:
		return 2 // Coffee: 2x
	case 2:
		return 4 // Cake: 4x
	case 3:
		return 10 // Sponsor: 10x
	default:
		return 1 // Free: 1x
	}
}

func (b *Bot) handleCallbackQuery(callback *tgbotapi.CallbackQuery) error {
	logger.Debug("Handling callback query", map[string]interface{}{
		"callback_data": callback.Data,
		"chat_id":       callback.Message.Chat.ID,
		"callback_id":   callback.ID,
	})

	// Check for duplicate callback processing
	if b.isDuplicateCallback(callback.ID) {
		logger.Debug("Duplicate callback detected, skipping", map[string]interface{}{
			"callback_id":   callback.ID,
			"callback_data": callback.Data,
		})
		// Still answer the callback to prevent timeout
		callbackConfig := tgbotapi.NewCallback(callback.ID, "")
		b.rateLimitedRequest(callback.Message.Chat.ID, callbackConfig)
		return nil
	}

	// Mark callback as processed
	b.markCallbackProcessed(callback.ID)

	// Answer the callback query first
	callbackConfig := tgbotapi.NewCallback(callback.ID, "")
	if _, err := b.rateLimitedRequest(callback.Message.Chat.ID, callbackConfig); err != nil {
		logger.Error("Failed to answer callback query", map[string]interface{}{
			"error": err.Error(),
		})
	}

	if strings.HasPrefix(callback.Data, "file_") {
		return b.handleFileSelection(callback)
	}

	if strings.HasPrefix(callback.Data, "photo_custom_file_") {
		return b.handlePhotoCustomFileChoice(callback)
	}

	if strings.HasPrefix(callback.Data, "photo_") {
		return b.handlePhotoFileSelection(callback)
	}

	if strings.HasPrefix(callback.Data, "cancel_") {
		return b.handleCancel(callback)
	}

	if strings.HasPrefix(callback.Data, "todo_more_") {
		return b.handleTodoMore(callback)
	}

	if strings.HasPrefix(callback.Data, "issue_more_") {
		return b.handleIssueMore(callback)
	}

	if strings.HasPrefix(callback.Data, "todo_done_") {
		return b.handleTodoDone(callback)
	}

	if strings.HasPrefix(callback.Data, "coffee_") {
		return b.handleCoffeeCallback(callback)
	}

	if strings.HasPrefix(callback.Data, "subscription_") {
		return b.handleSubscriptionCallback(callback)
	}

	if strings.HasPrefix(callback.Data, "issue_open_") {
		return b.handleIssueOpen(callback)
	}

	if strings.HasPrefix(callback.Data, "issue_comment_") {
		return b.handleIssueComment(callback)
	}

	if strings.HasPrefix(callback.Data, "issue_close_") {
		return b.handleIssueClose(callback)
	}

	if strings.HasPrefix(callback.Data, "custom_file_") {
		return b.handleCustomFileChoice(callback)
	}

	if strings.HasPrefix(callback.Data, "photo_custom_file_") {
		return b.handlePhotoCustomFileChoice(callback)
	}

	if strings.HasPrefix(callback.Data, "back_to_files_") {
		return b.handleBackToFiles(callback)
	}

	if strings.HasPrefix(callback.Data, "photo_add_custom_") {
		return b.handlePhotoAddCustomFile(callback)
	}

	if strings.HasPrefix(callback.Data, "add_custom_") {
		return b.handleAddCustomFile(callback)
	}

	if strings.HasPrefix(callback.Data, "remove_custom_file_") {
		return b.handleRemoveCustomFile(callback)
	}

	if strings.HasPrefix(callback.Data, "photo_remove_custom_") {
		return b.handlePhotoCustomRemoveFile(callback)
	}

	if strings.HasPrefix(callback.Data, "remove_custom_") {
		return b.handleCustomRemoveFile(callback)
	}

	if strings.HasPrefix(callback.Data, "show_all_custom_files") {
		return b.handleShowAllCustomFiles(callback)
	}

	if strings.HasPrefix(callback.Data, "refresh_custom_files") {
		return b.handleRefreshCustomFiles(callback)
	}

	if strings.HasPrefix(callback.Data, "customfile_") {
		return b.handleCustomFileAction(callback)
	}

	if strings.HasPrefix(callback.Data, "pin_file_") {
		return b.handlePinFileAction(callback)
	}

	if callback.Data == "github_oauth" {
		return b.handleGitHubOAuthPrivacyConfirmation(callback)
	}


	if callback.Data == "oauth_cancel" {
		return b.handleOAuthCancelCallback(callback)
	}

	if callback.Data == "confirm_reset_usage" {
		return b.handleResetUsageConfirmation(callback)
	}

	if callback.Data == "cancel_reset_usage" {
		return b.handleResetUsageCancellation(callback)
	}

	if callback.Data == "manage_subscription" {
		return b.handleManageSubscriptionCallback(callback)
	}

	if callback.Data == "llm_enable" {
		return b.handleLLMEnableCallback(callback)
	}

	if callback.Data == "llm_disable" {
		return b.handleLLMDisableCallback(callback)
	}

	if callback.Data == "llm_set_token" {
		return b.handleLLMSetTokenCallback(callback)
	}

	if callback.Data == "repo_set_repo" {
		return b.handleRepoSetRepoCallback(callback)
	}

	if callback.Data == "repo_set_token" {
		return b.handleRepoTokenPrivacyConfirmation(callback)
	}

	if callback.Data == "repo_set_token_confirmed" {
		return b.handleRepoSetTokenCallback(callback)
	}

	if callback.Data == "repo_set_committer" {
		return b.handleRepoSetCommitterCallback(callback)
	}

	if callback.Data == "repo_revoke_auth" {
		return b.handleRepoRevokeAuthCallback(callback)
	}

	if callback.Data == "repo_revoke_auth_confirmed" {
		return b.handleRepoRevokeAuthConfirmed(callback)
	}

	if callback.Data == "repo_revoke_auth_cancel" {
		return b.handleRepoRevokeAuthCancel(callback)
	}

	if callback.Data == "llm_multimodal_enable" {
		return b.handleLLMMultimodalEnableCallback(callback)
	}

	if callback.Data == "llm_multimodal_disable" {
		return b.handleLLMMultimodalDisableCallback(callback)
	}

	logger.Debug("Unhandled callback data", map[string]interface{}{
		"callback_data": callback.Data,
		"chat_id":       callback.Message.Chat.ID,
	})

	return nil
}