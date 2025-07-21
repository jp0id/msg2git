package telegram

import (
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/msg2git/msg2git/internal/consts"
	"github.com/msg2git/msg2git/internal/database"
	"github.com/msg2git/msg2git/internal/logger"
)

// Premium command handlers

func (b *Bot) handleCoffeeCommand(message *tgbotapi.Message) error {
	// Ensure user exists in database if database is configured
	_, err := b.ensureUser(message)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	if b.db == nil {
		b.sendResponse(message.Chat.ID, "❌ Premium features require database configuration. Please contact the administrator.")
		return nil
	}

	// Check if user already has premium
	premiumUser, err := b.db.GetPremiumUser(message.Chat.ID)
	if err != nil {
		logger.Error("Failed to check premium user status", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": message.Chat.ID,
		})
		b.sendResponse(message.Chat.ID, "❌ Failed to check premium status")
		return nil
	}

	if premiumUser != nil && premiumUser.IsPremiumUser() {
		// User is currently premium - show current status and management options only
		logs, err := b.db.GetUserTopupLogs(message.Chat.ID)
		if err != nil {
			logger.Error("Failed to get topup logs", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": message.Chat.ID,
			})
		}

		totalAmount := 0.0
		for _, log := range logs {
			totalAmount += log.Amount
		}

		// Get current premium level from user
		currentLevel := premiumUser.Level
		tierNames := []string{"Free", "☕ Coffee", "🍰 Cake", "🎁 Sponsor"}
		tierName := "☕ Coffee"
		if currentLevel < len(tierNames) {
			tierName = tierNames[currentLevel]
		}

		// Build status message based on subscription type
		var statusMsg string
		if premiumUser.IsSubscription {
			// Subscription user
			var billingInfo string
			if premiumUser.BillingPeriod != "" {
				billingInfo = fmt.Sprintf(" (%s)", strings.Title(premiumUser.BillingPeriod))
			}

			// Add expiry information for subscriptions
			var expireText string
			if premiumUser.ExpireAt == -1 {
				expireText = "Never expires (Lifetime)"
			} else {
				expireTime := time.Unix(premiumUser.ExpireAt, 0)
				expireText = fmt.Sprintf("Next renewal: %s", expireTime.Format("2006-01-02"))
			}

			statusMsg = fmt.Sprintf(`☕ <b>Premium Subscription Active</b>

🌟 You have an active premium subscription!
🏆 Current tier: %s%s
💰 Total contribution: $%.2f
📅 %s

<b>Premium features unlocked:</b>
🚀 %dx repo size limits
🌇 %dx photo and issue limits
📁 %dx custom files
🧠 %dx free LLM tokens
🎯 Priority support
✨ Automatic renewal

Thank you for supporting the project! 🙏

<b>Need help?</b> Contact support below.`,
				tierName, billingInfo, totalAmount, expireText, getRepositoryMultiplier(currentLevel), getRepositoryMultiplier(currentLevel), getRepositoryMultiplier(currentLevel), getRepositoryMultiplier(currentLevel))
		} else {
			// One-time payment user
			var expireText string
			if premiumUser.ExpireAt == -1 {
				expireText = "Never expires (Lifetime)"
			} else {
				expireTime := time.Unix(premiumUser.ExpireAt, 0)
				expireText = fmt.Sprintf("Expires: %s", expireTime.Format("2006-01-02"))
			}

			statusMsg = fmt.Sprintf(`☕ <b>Premium Access Active</b>

🌟 You have active premium access!
🏆 Current tier: %s
💰 Total contribution: $%.2f
📅 %s

<b>Premium features unlocked:</b>
🚀%dx repo size limits
🌇%dx photo and issue limits
📁%dx custom files
🤖%dx free LLM tokens
🎯Priority support

Want automatic renewal? Upgrade to a subscription plan!

Thank you for supporting the project! 🙏

<b>Need help?</b> Contact support below.`,
				tierName, totalAmount, expireText, getRepositoryMultiplier(currentLevel), getRepositoryMultiplier(currentLevel), getRepositoryMultiplier(currentLevel), getRepositoryMultiplier(currentLevel))
		}

		msg := tgbotapi.NewMessage(message.Chat.ID, statusMsg)
		msg.ParseMode = "HTML"

		// Create keyboard with management, contact, and cancel buttons
		var keyboardRows [][]tgbotapi.InlineKeyboardButton

		// Management button
		keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⚙️ Manage Subscription", "manage_subscription"),
		))

		// Contact buttons
		keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("💬 Priority Support", "https://t.me/pm_jp_bot"),
		))

		// Add website contact link if BASE_URL is configured
		if b.config.BaseURL != "" {
			keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonURL("🌐 Contact Us", b.config.BaseURL+"/contact"),
			))
		}

		keyboard := tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)
		msg.ReplyMarkup = keyboard

		if _, err := b.rateLimitedSend(message.Chat.ID, msg); err != nil {
			logger.Error("Failed to send premium status", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": message.Chat.ID,
			})
			b.sendResponse(message.Chat.ID, "❌ Failed to display premium status")
		}
		return nil
	}

	// Check if this is an expired user renewing
	var coffeeMsg string
	if premiumUser != nil && premiumUser.Level > 0 && premiumUser.IsExpired() {
		// Expired user - show renewal message
		tierNames := []string{"Free", "☕ Coffee", "🍰 Cake", "🎁 Sponsor"}
		expiredTierName := "Premium"
		if premiumUser.Level < len(tierNames) {
			expiredTierName = tierNames[premiumUser.Level]
		}

		coffeeMsg = fmt.Sprintf(`☕ <b>Renew your premium access!</b>

Your %s subscription has expired. Renew to restore premium features!

<b>Premium Benefits:</b>
🚀 Increased repo size
🌇 Increased photo and issue limits
📁 More custom files
🧠 Free LLM tokens
🎯 Priority support

<b>Available Tiers:</b>
☕ Coffee - 2x repo size, custom files, free tokens, etc
🍰 Cake - 4x repo size, custom files, free tokens, etc
🎁 Sponsor - 10x repo size, custom files, free tokens, etc

<i>Choose any tier to renew your premium access.</i>

<b>Need help?</b> Contact support below.

Select your preferred tier:`, expiredTierName)
	} else {
		// New user - show regular upgrade message
		coffeeMsg = `☕ <b>Buy me a coffee!</b>

Support the development of this bot and unlock premium features!

<b>Premium Benefits:</b>
🚀 Increased repo size
🌇 Increased photo and issue limits
📁 More custom files
🧠 Free LLM tokens
🎯 Priority support

<b>Available Tiers:</b>
☕ Coffee - 2x repo size, custom files, free tokens, etc
🍰 Cake - 4x repo size, custom files, free tokens, etc
🎁 Sponsor - 10x repo size, custom files, free tokens, etc

<b>Need help?</b> Contact support below.

<i>Choose your subscription tier:</i>`
	}

	// Create inline keyboard with subscription options and contact buttons
	var keyboardRows [][]tgbotapi.InlineKeyboardButton

	// Subscription options
	keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("☕ Coffee Monthly", "subscription_coffee_monthly"),
		tgbotapi.NewInlineKeyboardButtonData("☕ Coffee Annual", "subscription_coffee_annual"),
	))
	keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🍰 Cake Monthly", "subscription_cake_monthly"),
		tgbotapi.NewInlineKeyboardButtonData("🍰 Cake Annual", "subscription_cake_annual"),
	))
	keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🎁 Sponsor Monthly", "subscription_sponsor_monthly"),
		tgbotapi.NewInlineKeyboardButtonData("🎁 Sponsor Annual", "subscription_sponsor_annual"),
	))

	// Add website contact link if BASE_URL is configured
	contactRow := make([]tgbotapi.InlineKeyboardButton, 0, 2)
	if b.config.BaseURL != "" {
		contactRow = append(contactRow, tgbotapi.NewInlineKeyboardButtonURL("🌐 Contact Us", b.config.BaseURL+"/contact"))
	}
	contactRow = append(contactRow, tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "coffee_cancel"))

	// Cancel button
	keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(contactRow...))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)

	msg := tgbotapi.NewMessage(message.Chat.ID, coffeeMsg)
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = keyboard

	if _, err := b.rateLimitedSend(message.Chat.ID, msg); err != nil {
		return fmt.Errorf("failed to send coffee message: %w", err)
	}

	return nil
}

func (b *Bot) handleResetUsageCommand(message *tgbotapi.Message) error {
	// Ensure user exists in database if database is configured
	_, err := b.ensureUser(message)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	if b.db == nil {
		b.sendResponse(message.Chat.ID, "❌ Database not configured. Usage statistics require database to function.")
		return nil
	}

	// Get premium user level (if any) for limits
	premiumUser, err := b.db.GetPremiumUser(message.Chat.ID)
	if err != nil {
		logger.Warn("Failed to get premium user status, using free tier limits", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": message.Chat.ID,
		})
	}

	// Determine user level (default to free tier)
	var userLevel int = 0
	if premiumUser != nil && premiumUser.IsPremiumUser() {
		userLevel = premiumUser.Level
	}

	// Get current usage statistics
	usage, err := b.db.GetUserUsage(message.Chat.ID)
	if err != nil {
		logger.Error("Failed to get user usage for reset confirmation", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": message.Chat.ID,
		})
		b.sendResponse(message.Chat.ID, "❌ Failed to get usage statistics")
		return nil
	}

	// Get limits based on user level
	imageLimit := database.GetImageLimit(userLevel)
	issueLimit := database.GetIssueLimit(userLevel)
	tokenLimit := database.GetTokenLimit(userLevel)

	// Current usage counters
	var currentImages, currentIssues, currentTokens int64 = 0, 0, 0
	if usage != nil {
		currentImages = usage.ImageCnt
		currentIssues = usage.IssueCnt
		currentTokens = usage.TokenInput + usage.TokenOutput
	}

	// Calculate percentages
	imagePercentage := float64(currentImages) / float64(imageLimit) * 100
	issuePercentage := float64(currentIssues) / float64(issueLimit) * 100
	tokenPercentage := float64(currentTokens) / float64(tokenLimit) * 100

	// Create progress bars
	imageBar := b.formatUsageLine("📷 Images:", currentImages, imageLimit, imagePercentage)
	issueBar := b.formatUsageLine("📝 Issues:", currentIssues, issueLimit, issuePercentage)
	tokenBar := b.formatTokenUsageLine("🧠 Tokens:", currentTokens, tokenLimit, tokenPercentage)

	confirmMsg := fmt.Sprintf(`🔄 <b>Reset Usage Statistics</b>

<b>📈 Current Usage:</b>
%s
%s
%s

<b>⚠️ Warning:</b>
This will reset your current usage counters to zero. This action cannot be undone.

<b>Note:</b> Usage statistics are typically reset automatically at the beginning of each billing period.

Are you sure you want to proceed?`, imageBar, issueBar, tokenBar)

	// Create confirmation keyboard
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Yes, Reset Usage", "confirm_reset_usage"),
			tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "cancel_reset_usage"),
		),
	)

	responseMsg := tgbotapi.NewMessage(message.Chat.ID, confirmMsg)
	responseMsg.ParseMode = consts.ParseModeHTML
	responseMsg.ReplyMarkup = keyboard

	if _, err := b.rateLimitedSend(message.Chat.ID, responseMsg); err != nil {
		return fmt.Errorf("failed to send reset usage confirmation: %w", err)
	}

	return nil
}

// Helper function to handle usage reset confirmation
func (b *Bot) handleResetUsageConfirmation(callback *tgbotapi.CallbackQuery) error {
	if !strings.HasPrefix(callback.Data, "confirm_reset_usage") {
		return fmt.Errorf("invalid callback data for reset usage")
	}

	// Ensure user exists
	_, err := b.ensureUserFromCallback(callback)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	if b.db == nil {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Database not configured")
		return nil
	}

	// Get premium user status for tier display
	premiumUser, err := b.db.GetPremiumUser(callback.Message.Chat.ID)
	if err != nil {
		logger.Warn("Failed to get premium user status for reset, using free tier", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
	}

	// Determine user level (default to free tier)
	var userLevel int = 0
	if premiumUser != nil && premiumUser.IsPremiumUser() {
		userLevel = premiumUser.Level
	}

	// Check if Stripe is available
	if b.stripeManager == nil {
		// Fallback to mock payment if Stripe not configured
		paymentMsg := `💳 <b>Payment Processing</b>

<b>Service:</b> Usage Reset

<i>⚠️ Stripe not configured - using demo payment</i>

<i>🔄 Processing mock payment...</i>

<i>✅ Payment successful!</i>

<i>🔄 Resetting your usage statistics...</i>`

		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, paymentMsg)
		editMsg.ParseMode = consts.ParseModeHTML
		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			return fmt.Errorf("failed to send payment processing message: %w", err)
		}
	} else {
		// Create Stripe checkout session using configured price ID
		session, err := b.stripeManager.CreateResetUsageSession(callback.From.ID)
		if err != nil {
			logger.Error("Failed to create Stripe checkout session", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
			b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Failed to create payment session. Please try again later.")
			return nil
		}

		// Show Stripe payment link
		paymentMsg := `💳 <b>Stripe Payment Required</b>

<b>Service:</b> Usage Reset

Click the button below to complete your payment securely via Stripe.

⚡ <i>Your usage will be reset immediately after successful payment.</i>`

		// Create payment link button
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonURL("💳 Complete Payment", session.URL),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "cancel_reset_usage"),
			),
		)

		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, paymentMsg)
		editMsg.ParseMode = consts.ParseModeHTML
		editMsg.ReplyMarkup = &keyboard

		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			return fmt.Errorf("failed to send Stripe payment message: %w", err)
		}

		// Answer callback query
		callbackConfig := tgbotapi.NewCallback(callback.ID, "Payment link generated!")
		if _, err := b.rateLimitedRequest(callback.Message.Chat.ID, callbackConfig); err != nil {
			logger.Warn("Failed to answer callback query", map[string]interface{}{
				"error": err.Error(),
			})
		}

		// Return here - the actual processing will happen via webhook
		return nil
	}

	// Get user info for payment recording
	user, err := b.db.GetUserByChatID(callback.Message.Chat.ID)
	if err != nil {
		logger.Error("Failed to get user for payment recording", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Failed to process payment")
		return nil
	}

	// Get current usage before reset to log it
	currentUsage, err := b.db.GetUserUsage(callback.Message.Chat.ID)
	if err != nil {
		logger.Error("Failed to get current usage for reset log", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Failed to get current usage")
		return nil
	}

	var currentImages, currentIssues, currentTokenInput, currentTokenOutput int64 = 0, 0, 0, 0
	if currentUsage != nil {
		currentImages = currentUsage.ImageCnt
		currentIssues = currentUsage.IssueCnt
		currentTokenInput = currentUsage.TokenInput
		currentTokenOutput = currentUsage.TokenOutput
	}

	// Record the payment in database (mock payment - no transaction ID)
	// Use default amount for mock payment since we don't have Stripe webhook data
	mockPaymentAmount := consts.PriceReset
	topupLog, err := b.db.CreateTopupLog(callback.Message.Chat.ID, user.Username, mockPaymentAmount, consts.ServiceReset, "", "")
	if err != nil {
		logger.Error("Failed to record reset payment", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Payment processing failed")
		return nil
	}

	// Create reset log entry
	_, err = b.db.CreateResetLog(callback.Message.Chat.ID, currentIssues, currentImages, currentTokenInput, currentTokenOutput, topupLog.ID)
	if err != nil {
		logger.Error("Failed to create reset log", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		// Don't fail the reset for this, just log the error
	}

	// Reset usage statistics
	if err := b.db.ResetUserUsage(callback.Message.Chat.ID); err != nil {
		logger.Error("Failed to reset user usage", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Failed to reset usage statistics")
		return nil
	}

	// Increment reset count in user insights
	if err := b.db.IncrementResetCount(callback.Message.Chat.ID); err != nil {
		logger.Error("Failed to increment reset count in insights", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		// Don't fail the reset for this, just log the error since the main operation succeeded
	}

	// Success message
	tierNames := map[int]string{0: "Free", 1: "☕ Coffee", 2: "🍰 Cake", 3: "🎁 Sponsor"}
	currentTier := tierNames[userLevel]
	imageLimit := database.GetImageLimit(userLevel)
	issueLimit := database.GetIssueLimit(userLevel)
	tokenLimit := database.GetTokenLimit(userLevel)

	successMsg := fmt.Sprintf(`✅ <b>Usage Reset Complete!</b>

<b>💳 Payment:</b> ✅ Paid
<b>📈 New Limits:</b>
📷 Images: 0/%d per month
📝 Issues: 0/%d per month
🧠 Tokens: 0/%s per month

<b>Tier:</b> %s

<i>You can now use your full allocation for this period!</i>`,
		imageLimit, issueLimit, formatTokenCount(tokenLimit), currentTier)

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, successMsg)
	editMsg.ParseMode = consts.ParseModeHTML

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		return fmt.Errorf("failed to send reset success message: %w", err)
	}

	// Answer callback query
	callbackConfig := tgbotapi.NewCallback(callback.ID, "✅ Payment successful! Usage reset complete!")
	if _, err := b.rateLimitedRequest(callback.Message.Chat.ID, callbackConfig); err != nil {
		logger.Warn("Failed to answer callback query", map[string]interface{}{
			"error": err.Error(),
		})
	}

	logger.Info("User usage statistics reset with payment", map[string]interface{}{
		"chat_id":        callback.Message.Chat.ID,
		"level":          userLevel,
		"amount":         mockPaymentAmount,
		"payment_method": "mock",
	})

	return nil
}

// Helper function to handle usage reset cancellation
func (b *Bot) handleResetUsageCancellation(callback *tgbotapi.CallbackQuery) error {
	cancelMsg := "❌ <b>Usage Reset Cancelled</b>\n\n<i>Your usage statistics remain unchanged.</i>"

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, cancelMsg)
	editMsg.ParseMode = consts.ParseModeHTML

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		return fmt.Errorf("failed to send reset cancellation message: %w", err)
	}

	// Answer callback query
	callbackConfig := tgbotapi.NewCallback(callback.ID, "Reset cancelled")
	if _, err := b.rateLimitedRequest(callback.Message.Chat.ID, callbackConfig); err != nil {
		logger.Warn("Failed to answer callback query", map[string]interface{}{
			"error": err.Error(),
		})
	}

	return nil
}

// Helper function to handle usage check
func (b *Bot) handleUsageCheck(callback *tgbotapi.CallbackQuery) error {
	if b.db == nil {
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Database not configured")
		return nil
	}

	// Get premium user info
	premiumUser, err := b.db.GetPremiumUser(callback.Message.Chat.ID)
	if err != nil {
		logger.Error("Failed to get premium user for usage check", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Failed to check usage")
		return nil
	}

	premiumLevel := 0

	if premiumUser != nil {
		if premiumUser.IsPremiumUser() {
			premiumLevel = premiumUser.Level
		}
	}

	// Get limits
	imageLimit := database.GetImageLimit(premiumLevel)
	tierNames := map[int]string{0: "Free", 1: "☕ Coffee", 2: "🍰 Cake", 3: "🎁 Sponsor"}
	currentTier := tierNames[premiumLevel]

	usageMsg := fmt.Sprintf(`📊 <b>Usage Statistics</b>

<b>Current Tier:</b> %s

<b>Limits:</b>
📸 Images: %d per month

<i>Usage tracking is active for your tier.</i>`,
		currentTier, imageLimit)

	// Add upgrade suggestion for free tier
	if premiumLevel == 0 {
		usageMsg += UpgradeTipMessage
	}

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, usageMsg)
	editMsg.ParseMode = consts.ParseModeHTML

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		return fmt.Errorf("failed to send usage check message: %w", err)
	}

	// Answer callback query
	callbackConfig := tgbotapi.NewCallback(callback.ID, "📊 Usage statistics updated")
	if _, err := b.rateLimitedRequest(callback.Message.Chat.ID, callbackConfig); err != nil {
		logger.Warn("Failed to answer callback query", map[string]interface{}{
			"error": err.Error(),
		})
	}

	return nil
}

// Helper functions for usage display
func getUsageIcon(percentage float64) string {
	if percentage < 50 {
		return "🟢"
	} else if percentage < 80 {
		return "🟡"
	}
	return "🔴"
}

func getUsageStatus(percentage float64) string {
	if percentage < 50 {
		return "Good"
	} else if percentage < 80 {
		return "Moderate"
	} else if percentage < 95 {
		return "High"
	}
	return "Near Limit"
}
