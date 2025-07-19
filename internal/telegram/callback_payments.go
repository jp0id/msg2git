package telegram

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/msg2git/msg2git/internal/consts"
	"github.com/msg2git/msg2git/internal/logger"
)

// handleCoffeeCallback handles coffee payment button callbacks
func (b *Bot) handleCoffeeCallback(callback *tgbotapi.CallbackQuery) error {
	logger.Debug("Handling coffee callback", map[string]interface{}{
		"callback_data": callback.Data,
		"chat_id":       callback.Message.Chat.ID,
	})

	if callback.Data == "coffee_cancel" {
		// User cancelled
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID,
			PaymentCancelledMessage)
		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			logger.Error("Failed to edit cancel message", map[string]interface{}{
				"error": err.Error(),
			})
		}
		return nil
	}

	// Handle simulated payment
	if strings.HasPrefix(callback.Data, "coffee_simulate_") {
		if callback.Data == "coffee_simulate_reset" {
			return b.handleSimulatedResetPayment(callback)
		}

		parts := strings.Split(callback.Data, "_")
		if len(parts) == 4 {
			level, err1 := strconv.Atoi(parts[2])
			amount, err2 := strconv.ParseFloat(parts[3], 64)
			if err1 == nil && err2 == nil {
				return b.handleSimulatedPayment(callback, level, amount)
			}
		}
		b.sendResponse(callback.Message.Chat.ID, consts.ErrorInvalidFormat)
		return nil
	}

	// Handle subscription callbacks
	if strings.HasPrefix(callback.Data, "subscription_") {
		return b.handleSubscriptionCallback(callback)
	}

	// Handle legacy payment and other callbacks
	switch callback.Data {
	case "coffee_payment_reset":
		return b.handleResetUsagePayment(callback)
	case "manage_subscription":
		return b.handleManageSubscriptionCallback(callback)
	default:
		b.sendResponse(callback.Message.Chat.ID, consts.ErrorInvalidFormat)
		return nil
	}
}

// handleSubscriptionCallback handles subscription tier selection
func (b *Bot) handleSubscriptionCallback(callback *tgbotapi.CallbackQuery) error {
	logger.Debug("Handling subscription callback", map[string]interface{}{
		"callback_data": callback.Data,
		"chat_id":       callback.Message.Chat.ID,
	})

	// Parse subscription callback data: subscription_{tier}_{period}
	parts := strings.Split(callback.Data, "_")
	if len(parts) != 3 {
		b.sendResponse(callback.Message.Chat.ID, consts.ErrorInvalidFormat)
		return nil
	}

	tier := parts[1]   // coffee, cake, sponsor
	period := parts[2] // monthly, annual
	isAnnual := period == "annual"

	var premiumLevel int
	var tierName string

	switch tier {
	case "coffee":
		premiumLevel = consts.PremiumLevelCoffee
		tierName = consts.TierCoffee
	case "cake":
		premiumLevel = consts.PremiumLevelCake
		tierName = consts.TierCake
	case "sponsor":
		premiumLevel = consts.PremiumLevelSponsor
		tierName = consts.TierSponsor
	default:
		b.sendResponse(callback.Message.Chat.ID, consts.ErrorInvalidFormat)
		return nil
	}

	billingPeriod := "monthly"
	if isAnnual {
		billingPeriod = "annually"
	}

	// Check if Stripe is available
	if b.stripeManager == nil {
		// Show demo message for subscription
		multiplier := getRepositoryMultiplier(premiumLevel)
		paymentMsg := fmt.Sprintf(`💳 <b>Subscription Simulation</b>

<b>Selected:</b> %s (%s)

<i>%s</i>

<b>Premium Benefits:</b>
🚀 %dx repo size limits
🌇 %dx photo and issue limits
📁 %dx custom files
🧠 %dx free LLM tokens
🎯 Priority support
✨ Recurring subscription with automatic renewal

<b>Billing:</b> %s subscription`, tierName, billingPeriod, consts.DemoWarning, multiplier, multiplier, multiplier, multiplier, strings.Title(billingPeriod))

		// Create buttons for simulated subscription
		row1 := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(consts.EmojiSuccess+" Simulate Subscription", fmt.Sprintf("simulate_subscription_%s_%s", tier, period)),
		)
		row2 := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(consts.ButtonCancel, "coffee_cancel"),
		)

		keyboard := tgbotapi.NewInlineKeyboardMarkup(row1, row2)

		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, paymentMsg)
		editMsg.ParseMode = consts.ParseModeHTML
		editMsg.ReplyMarkup = &keyboard

		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			logger.Error("Failed to edit subscription demo message", map[string]interface{}{
				"error": err.Error(),
			})
			return err
		}
		return nil
	}

	// Create Stripe subscription checkout session
	session, err := b.stripeManager.CreateSubscriptionSession(callback.From.ID, tierName, premiumLevel, isAnnual)
	if err != nil {
		logger.Error("Failed to create Stripe subscription session", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
			"tier":    tier,
			"period":  period,
		})
		b.editMessage(callback.Message.Chat.ID, callback.Message.MessageID, "❌ Failed to create subscription session. Please try again later.")
		return nil
	}

	// Show Stripe subscription payment link
	multiplier := getRepositoryMultiplier(premiumLevel)
	paymentMsg := fmt.Sprintf(`💳 <b>Stripe Subscription</b>

<b>Selected:</b> %s (%s)

<b>Premium Benefits:</b>
🚀 %dx repo size limits
🌇 %dx photo and issue limits
📁 %dx custom files
🧠 %dx free LLM tokens
🎯 Priority support
✨ Automatic renewal (%s billing)

Click the button below to complete your subscription securely via Stripe.

⚡ <i>Your premium features will be activated immediately after subscription confirmation.</i>`, tierName, billingPeriod, multiplier, multiplier, multiplier, multiplier, billingPeriod)

	// Create subscription link button
	row1 := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonURL(fmt.Sprintf("💳 Subscribe %s", strings.Title(billingPeriod)), session.URL),
	)
	row2 := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData(consts.ButtonCancel, "coffee_cancel"),
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(row1, row2)

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, paymentMsg)
	editMsg.ParseMode = consts.ParseModeHTML
	editMsg.ReplyMarkup = &keyboard

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		logger.Error("Failed to edit subscription message", map[string]interface{}{
			"error": err.Error(),
		})
		return err
	}

	return nil
}

// handleSimulatedPayment handles the simulated payment completion
func (b *Bot) handleSimulatedPayment(callback *tgbotapi.CallbackQuery, premiumLevel int, amount float64) error {
	if b.db == nil {
		b.sendResponse(callback.Message.Chat.ID, consts.ErrorDatabaseNotConfigured)
		return nil
	}

	// Get user info
	user, err := b.db.GetUserByChatID(callback.Message.Chat.ID)
	if err != nil || user == nil {
		b.sendResponse(callback.Message.Chat.ID, consts.ErrorUserNotFound)
		return nil
	}

	username := ""
	if callback.From.UserName != "" {
		username = callback.From.UserName
	}

	// Set expiry time - all premium tiers now have 1 year expiry (v2 update)
	expireAt := time.Now().AddDate(1, 0, 0).Unix() // 1 year expiry for all tiers

	// Create or upgrade premium user
	_, err = b.db.CreatePremiumUser(callback.Message.Chat.ID, username, premiumLevel, expireAt)
	if err != nil {
		logger.Error("Failed to create premium user", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		b.sendResponse(callback.Message.Chat.ID, consts.ErrorOperationFailed)
		return nil
	}

	// Create topup log
	serviceNames := []string{"FREE", consts.ServiceCoffee, consts.ServiceCake, consts.ServiceSponsor}
	service := "PREMIUM" // Default fallback
	if premiumLevel < len(serviceNames) {
		service = serviceNames[premiumLevel]
	}
	_, err = b.db.CreateTopupLog(callback.Message.Chat.ID, username, amount, service, "", "")
	if err != nil {
		logger.Error("Failed to create topup log", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
	}

	// Show success message
	tierNames := []string{consts.TierFree, consts.TierCoffee, consts.TierCake, consts.TierSponsor}
	tierName := "Premium"
	if premiumLevel < len(tierNames) {
		tierName = tierNames[premiumLevel]
	}
	multiplier := getRepositoryMultiplier(premiumLevel)
	successMsg := fmt.Sprintf(`🎉 <b>Premium Activated!</b>

<b>Tier:</b> %s
<b>Amount:</b> $%.0f
<b>Benefits Unlocked:</b>
🚀 %dx repo size limits
🌇 %dx photo and issue limits
📁 %dx custom files
🧠 %dx free LLM tokens
🎯 Priority support

<i>Thank you for supporting the project! 🙏</i>

Use /insight to see your new limits!`, tierName, amount, multiplier, multiplier, multiplier, multiplier)

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, successMsg)
	editMsg.ParseMode = consts.ParseModeHTML

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		logger.Error("Failed to edit success message", map[string]interface{}{
			"error": err.Error(),
		})
		return err
	}

	return nil
}

// handleResetUsagePayment handles reset usage payment button callbacks
func (b *Bot) handleResetUsagePayment(callback *tgbotapi.CallbackQuery) error {
	resetAmount := consts.PriceReset

	// For now, simulate payment (in real implementation, integrate with Stripe or similar)
	paymentMsg := fmt.Sprintf(`💳 <b>Payment Simulation - Reset Usage</b>

<b>Selected:</b> Usage Reset ($%.2f)

<i>%s</i>

<b>Reset Benefits:</b>
🔄 Reset issue creation counter to 0
📷 Reset image upload counter to 0
🧠 Reset LLM token usage counter to 0
✨ Fresh start with your premium limits

This will allow you to create more issues, upload more images, and use more LLM tokens based on your current premium tier.`, resetAmount, consts.DemoWarning)

	// Create buttons for simulated payment
	row1 := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData(consts.EmojiSuccess+" Simulate Payment", "coffee_simulate_reset"),
	)
	row2 := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData(consts.ButtonCancel, "coffee_cancel"),
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(row1, row2)

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, paymentMsg)
	editMsg.ParseMode = consts.ParseModeHTML
	editMsg.ReplyMarkup = &keyboard

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		return fmt.Errorf("failed to edit message: %w", err)
	}

	return nil
}

// handleSimulatedResetPayment handles the simulated reset payment completion
func (b *Bot) handleSimulatedResetPayment(callback *tgbotapi.CallbackQuery) error {
	if b.db == nil {
		b.sendResponse(callback.Message.Chat.ID, consts.ErrorDatabaseNotConfigured)
		return nil
	}

	// Get user information
	user, err := b.ensureUser(&tgbotapi.Message{
		Chat: callback.Message.Chat,
		From: callback.From,
	})
	if err != nil {
		logger.Error("Failed to get user for reset payment", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		b.sendResponse(callback.Message.Chat.ID, consts.ErrorOperationFailed)
		return nil
	}

	// Reset the user's usage counters
	if err := b.db.ResetUserUsage(callback.Message.Chat.ID); err != nil {
		logger.Error("Failed to reset user usage", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		b.sendResponse(callback.Message.Chat.ID, consts.ErrorOperationFailed)
		return nil
	}

	// Increment reset count in insights
	if err := b.db.IncrementResetCount(callback.Message.Chat.ID); err != nil {
		logger.Error("Failed to increment reset count", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		// Don't fail the operation for this, just log it
	}

	// Create topup log for reset payment
	username := ""
	if user != nil {
		username = user.Username
	}
	if callback.From != nil && callback.From.UserName != "" {
		username = callback.From.UserName
	}

	_, err = b.db.CreateTopupLog(callback.Message.Chat.ID, username, consts.PriceReset, consts.ServiceReset, "", "")
	if err != nil {
		logger.Error("Failed to create reset topup log", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		// Don't fail the operation for this, just log it
	}

	// Get current token usage before reset for display purposes
	var currentTokensForDisplay int64
	if usage, err := b.db.GetUserUsage(callback.Message.Chat.ID); err == nil && usage != nil {
		currentTokensForDisplay = usage.TokenInput + usage.TokenOutput
	}

	// Success message
	successMsg := consts.EmojiSuccess + ` <b>Payment Successful!</b>

` + consts.EmojiReset + ` <b>Usage Reset Complete</b>

Your usage counters have been reset to 0:
📝 Issue creation: 0
📷 Image uploads: 0
🧠 LLM tokens: 0 (was ` + formatTokenCount(currentTokensForDisplay) + `)

You can now create more issues, upload more images, and use more LLM tokens based on your premium tier limits.

<i>Thank you for your payment!</i>`

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, successMsg)
	editMsg.ParseMode = consts.ParseModeHTML
	editMsg.ReplyMarkup = nil // Remove buttons

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		logger.Error("Failed to send reset payment success message", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		// Fallback to simple message
		b.sendResponse(callback.Message.Chat.ID, consts.SuccessPaymentComplete+" Your usage counters have been reset.")
	}

	logger.Info("User successfully reset usage with payment", map[string]interface{}{
		"chat_id":  callback.Message.Chat.ID,
		"username": username,
		"amount":   consts.PriceReset,
	})

	return nil
}

// handleManageSubscriptionCallback handles the manage subscription button callback
func (b *Bot) handleManageSubscriptionCallback(callback *tgbotapi.CallbackQuery) error {
	logger.Debug("Handling manage subscription callback", map[string]interface{}{
		"chat_id": callback.Message.Chat.ID,
	})

	// Check if user is premium
	if b.db == nil {
		b.sendResponse(callback.Message.Chat.ID, consts.ErrorDatabaseNotConfigured)
		return nil
	}

	premiumUser, err := b.db.GetPremiumUser(callback.Message.Chat.ID)
	if err != nil {
		logger.Error("Failed to check premium user status", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})
		b.sendResponse(callback.Message.Chat.ID, "❌ Failed to check premium status")
		return nil
	}

	if premiumUser == nil || !premiumUser.IsPremiumUser() {
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID,
			"❌ You don't have an active premium subscription to manage.")
		editMsg.ParseMode = consts.ParseModeHTML
		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			logger.Error("Failed to edit message", map[string]interface{}{
				"error": err.Error(),
			})
		}
		return nil
	}

	// Check if Stripe is configured
	if b.stripeManager == nil {
		// Show demo/development message for subscription management
		managementMsg := `⚙️ <b>Manage Subscription</b>

<b>Current Status:</b>
✅ You have an active premium subscription

<b>Subscription Management:</b>
In a production environment, this button would redirect you to the Stripe Customer Portal where you can:

• View billing history
• Update payment methods
• Download invoices
• Change subscription plans
• Cancel subscription

<b>Demo Note:</b>
This is a demo version. In production, you would be redirected to a secure Stripe portal to manage your subscription.

<i>Contact support if you need assistance with your subscription.</i>`

		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, managementMsg)
		editMsg.ParseMode = consts.ParseModeHTML
		editMsg.ReplyMarkup = nil // Remove buttons

		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			logger.Error("Failed to send subscription management message", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
		}
		return nil
	}

	// Create Stripe Customer Portal session if user has a customer ID
	logger.Debug("Checking customer portal access", map[string]interface{}{
		"customer_id":     premiumUser.CustomerID,
		"is_subscription": premiumUser.IsSubscription,
		"subscription_id": premiumUser.SubscriptionID,
	})

	if premiumUser.CustomerID != "" && premiumUser.IsSubscription {
		portalSession, err := b.stripeManager.CreateCustomerPortalSession(premiumUser.CustomerID, fmt.Sprintf("%s/account", b.config.BaseURL))
		if err != nil {
			logger.Error("Failed to create customer portal session", map[string]interface{}{
				"error":        err.Error(),
				"chat_id":      callback.Message.Chat.ID,
				"customer_id":  premiumUser.CustomerID,
				"stripe_error": fmt.Sprintf("%v", err),
			})

			// Fallback message with more helpful information
			tierNames := map[int]string{0: "Free", 1: "☕ Coffee", 2: "🍰 Cake", 3: "🎁 Sponsor"}
			tierName := tierNames[premiumUser.Level]
			if tierName == "" {
				tierName = fmt.Sprintf("Level %d", premiumUser.Level)
			}

			var managementMsg string
			if premiumUser.SubscriptionID != "" {
				managementMsg = fmt.Sprintf(`⚙️ <b>Manage Subscription</b>

<b>Current Status:</b>
✅ You have an active premium subscription

<b>Subscription Details:</b>
• Tier: %s
• Billing: %s
• Subscription ID: <code>%s</code>
• Customer ID: <code>%s</code>

<b>Issue:</b>
Unable to access Stripe Customer Portal. This might be because:
• Customer Portal is not enabled in Stripe dashboard
• There's a temporary connectivity issue

<b>What you can do:</b>
• Try again in a few minutes
• Contact support if the issue persists

<i>Your subscription is still active and working normally.</i>`,
					tierName, premiumUser.BillingPeriod, premiumUser.SubscriptionID, premiumUser.CustomerID)
			} else {
				managementMsg = fmt.Sprintf(`⚙️ <b>Manage Subscription</b>

<b>Current Status:</b>
✅ You have an active premium subscription

<b>Subscription Details:</b>
• Tier: %s
• Billing: %s
• Customer ID: <code>%s</code>

<b>Issue:</b>
Unable to access Stripe Customer Portal. This might be because:
• Customer Portal is not enabled in Stripe dashboard
• There's a temporary connectivity issue

<b>What you can do:</b>
• Try again in a few minutes
• Contact support if the issue persists

<i>Your subscription is still active and working normally.</i>`,
					tierName, premiumUser.BillingPeriod, premiumUser.CustomerID)
			}

			editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, managementMsg)
			editMsg.ParseMode = consts.ParseModeHTML
			editMsg.ReplyMarkup = nil

			b.rateLimitedSend(callback.Message.Chat.ID, editMsg)
			return nil
		}

		// Show customer portal link
		tierNames := map[int]string{0: "Free", 1: "☕ Coffee", 2: "🍰 Cake", 3: "🎁 Sponsor"}
		tierName := tierNames[premiumUser.Level]
		if tierName == "" {
			tierName = fmt.Sprintf("Level %d", premiumUser.Level)
		}

		var managementMsg string
		if premiumUser.SubscriptionID != "" {
			managementMsg = fmt.Sprintf(`⚙️ <b>Manage Subscription</b>

<b>Current Status:</b>
✅ You have an active premium subscription

<b>Subscription Details:</b>
• Tier: %s
• Billing: %s
• Subscription ID: <code>%s</code>

<b>Customer Portal:</b>
Click the button below to access your Stripe Customer Portal where you can:

• View billing history
• Update payment methods  
• Download invoices
• Change subscription plans
• Cancel subscription

<i>This will redirect you to a secure Stripe page.</i>`,
				tierName, premiumUser.BillingPeriod, premiumUser.SubscriptionID)
		} else {
			managementMsg = fmt.Sprintf(`⚙️ <b>Manage Subscription</b>

<b>Current Status:</b>
✅ You have an active premium subscription

<b>Subscription Details:</b>
• Tier: %s
• Billing: %s

<b>Customer Portal:</b>
Click the button below to access your Stripe Customer Portal where you can:

• View billing history
• Update payment methods  
• Download invoices
• Change subscription plans
• Cancel subscription

<i>This will redirect you to a secure Stripe page.</i>`,
				tierName, premiumUser.BillingPeriod)
		}

		// Create portal link button
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonURL("🔗 Open Customer Portal", portalSession.URL),
			),
		)

		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, managementMsg)
		editMsg.ParseMode = consts.ParseModeHTML
		editMsg.ReplyMarkup = &keyboard

		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			logger.Error("Failed to send customer portal message", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
		}
	} else {
		// User doesn't have a customer ID (legacy one-time payment user)
		managementMsg := `⚙️ <b>Manage Subscription</b>

<b>Current Status:</b>
✅ You have active premium access

<b>Note:</b>
Your premium access was purchased as a one-time payment (not a subscription). You don't have a recurring subscription to manage.

<b>Your Premium Details:</b>
• Tier: %s
• Type: One-time purchase
• Expires: %s

<b>Want to switch to a subscription?</b>
You can upgrade to a subscription plan anytime using /coffee for automatic renewal and billing management.

<i>Contact support if you need assistance.</i>`

		var expireText string
		if premiumUser.ExpireAt == -1 {
			expireText = "Never (Lifetime)"
		} else {
			expireTime := time.Unix(premiumUser.ExpireAt, 0)
			expireText = expireTime.Format("2006-01-02")
		}

		tierNames := map[int]string{0: "Free", 1: "☕ Coffee", 2: "🍰 Cake", 3: "🎁 Sponsor"}
		tierName := tierNames[premiumUser.Level]
		if tierName == "" {
			tierName = fmt.Sprintf("Level %d", premiumUser.Level)
		}

		finalMsg := fmt.Sprintf(managementMsg, tierName, expireText)

		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, finalMsg)
		editMsg.ParseMode = consts.ParseModeHTML
		editMsg.ReplyMarkup = nil

		if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
			logger.Error("Failed to send one-time payment message", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
		}
	}

	logger.Info("User accessed subscription management", map[string]interface{}{
		"chat_id":       callback.Message.Chat.ID,
		"premium_level": premiumUser.Level,
	})

	return nil
}
