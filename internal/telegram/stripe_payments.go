package telegram

import (
	"fmt"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/msg2git/msg2git/internal/consts"
	"github.com/msg2git/msg2git/internal/logger"
	"github.com/msg2git/msg2git/internal/stripe"
)

// processResetUsagePayment processes reset usage payments and updates database
func (b *Bot) processResetUsagePayment(paymentData *stripe.PaymentData) {
	logger.Info("Processing reset usage payment", map[string]interface{}{
		"user_id":    paymentData.UserID,
		"amount":     paymentData.Amount,
		"session_id": paymentData.SessionID,
	})

	// Validate payment amount for reset usage (should be positive)
	if paymentData.Amount <= 0 {
		logger.Warn("Invalid reset usage payment amount, skipping processing", map[string]interface{}{
			"user_id":    paymentData.UserID,
			"amount":     paymentData.Amount,
			"session_id": paymentData.SessionID,
			"note":       "Amount must be positive",
		})
		return
	}

	if b.db == nil {
		logger.Error("Database not available for payment processing", map[string]interface{}{
			"user_id": paymentData.UserID,
		})
		return
	}

	// Convert Telegram user ID to chat ID (they're the same for private chats)
	chatID := paymentData.UserID

	// Get user info for payment recording
	user, err := b.db.GetUserByChatID(chatID)
	if err != nil {
		logger.Error("Failed to get user for payment recording", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return
	}

	// Check if user exists
	if user == nil {
		logger.Warn("Payment received for non-existent user, skipping processing", map[string]interface{}{
			"chat_id":    chatID,
			"session_id": paymentData.SessionID,
			"amount":     paymentData.Amount,
		})
		return
	}

	// Get current usage before reset to log it
	currentUsage, err := b.db.GetUserUsage(chatID)
	if err != nil {
		logger.Error("Failed to get current usage for reset log", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return
	}

	var currentImages, currentIssues, currentTokenInput, currentTokenOutput int64 = 0, 0, 0, 0
	if currentUsage != nil {
		currentImages = currentUsage.ImageCnt
		currentIssues = currentUsage.IssueCnt
		currentTokenInput = currentUsage.TokenInput
		currentTokenOutput = currentUsage.TokenOutput
	}

	// Record the payment in database
	topupLog, err := b.db.CreateTopupLog(chatID, user.Username, paymentData.Amount, consts.ServiceReset, paymentData.SessionID, "")
	if err != nil {
		logger.Error("Failed to record reset payment", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return
	}

	// Create reset log entry
	_, err = b.db.CreateResetLog(chatID, currentIssues, currentImages, currentTokenInput, currentTokenOutput, topupLog.ID)
	if err != nil {
		logger.Error("Failed to create reset log", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		// Don't fail the reset for this, just log the error
	}

	// Reset usage statistics
	if err := b.db.ResetUserUsage(chatID); err != nil {
		logger.Error("Failed to reset user usage", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return
	}

	// Increment reset count in user insights
	if err := b.db.IncrementResetCount(chatID); err != nil {
		logger.Error("Failed to increment reset count in insights", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		// Don't fail the reset for this, just log the error since the main operation succeeded
	}

	// Send success notification to user via Telegram
	b.sendPaymentSuccessNotification(chatID, paymentData)

	logger.Info("Usage reset completed successfully", map[string]interface{}{
		"chat_id": chatID,
		"amount":  paymentData.Amount,
	})
}

// sendPaymentSuccessNotification sends a success message to the user after payment
func (b *Bot) sendPaymentSuccessNotification(chatID int64, paymentData *stripe.PaymentData) {
	successText := fmt.Sprintf(`✅ <b>Payment Successful!</b>

<b>Amount Paid:</b> $%.2f
<b>Transaction ID:</b> <code>%s</code>

🚀 <b>Your usage limit has been reset!</b>

You can now continue using premium features. Thank you for your payment!`,
		paymentData.Amount, paymentData.SessionID)

	msg := tgbotapi.NewMessage(chatID, successText)
	msg.ParseMode = "html"

	if _, err := b.rateLimitedSend(chatID, msg); err != nil {
		logger.Error("Failed to send payment success notification", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
	}
}

// processPremiumPayment processes premium tier payments and updates database
func (b *Bot) processPremiumPayment(paymentData *stripe.PaymentData) {
	logger.Info("Processing premium payment", map[string]interface{}{
		"user_id":    paymentData.UserID,
		"amount":     paymentData.Amount,
		"session_id": paymentData.SessionID,
	})

	if b.db == nil {
		logger.Error("Database not available for premium payment processing", map[string]interface{}{
			"user_id": paymentData.UserID,
		})
		return
	}

	// Convert Telegram user ID to chat ID
	chatID := paymentData.UserID

	// Get user info for payment recording
	user, err := b.db.GetUserByChatID(chatID)
	if err != nil {
		logger.Error("Failed to get user for premium payment recording", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return
	}

	// Check if user exists
	if user == nil {
		logger.Warn("Premium payment received for non-existent user, skipping processing", map[string]interface{}{
			"chat_id":    chatID,
			"session_id": paymentData.SessionID,
			"amount":     paymentData.Amount,
		})
		return
	}

	// Extract premium level and tier name from Stripe metadata
	premiumLevel := paymentData.PremiumLevel
	tierName := paymentData.TierName
	var serviceName string

	// If metadata is missing, determine from amount as fallback
	if premiumLevel == 0 || tierName == "" {
		switch paymentData.Amount {
		case 5.0:
			premiumLevel = consts.PremiumLevelCoffee
			tierName = consts.TierCoffee
			serviceName = consts.ServiceCoffee
		case 15.0:
			premiumLevel = consts.PremiumLevelCake
			tierName = consts.TierCake
			serviceName = consts.ServiceCake
		case 50.0:
			premiumLevel = consts.PremiumLevelSponsor
			tierName = consts.TierSponsor
			serviceName = consts.ServiceSponsor
		default:
			logger.Warn("Unknown premium payment amount", map[string]interface{}{
				"amount":  paymentData.Amount,
				"user_id": paymentData.UserID,
			})
			premiumLevel = consts.PremiumLevelCoffee // Default to Coffee
			tierName = consts.TierCoffee
			serviceName = consts.ServiceCoffee
		}
	} else {
		// Map premium level to service name
		switch premiumLevel {
		case consts.PremiumLevelCoffee:
			serviceName = consts.ServiceCoffee
		case consts.PremiumLevelCake:
			serviceName = consts.ServiceCake
		case consts.PremiumLevelSponsor:
			serviceName = consts.ServiceSponsor
		default:
			serviceName = consts.ServiceCoffee
		}
	}

	// Set expiry time - all premium tiers now have 1 year expiry
	expireAt := time.Now().AddDate(1, 0, 0).Unix() // 1 year expiry for all tiers

	// Create or upgrade premium user
	_, err = b.db.CreatePremiumUser(chatID, user.Username, premiumLevel, expireAt)
	if err != nil {
		logger.Error("Failed to create premium user", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return
	}

	// Record the payment in database
	_, err = b.db.CreateTopupLog(chatID, user.Username, paymentData.Amount, serviceName, paymentData.SessionID, "")
	if err != nil {
		logger.Error("Failed to record premium payment", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return
	}

	// Send success notification to user
	b.sendPremiumPaymentSuccessNotification(chatID, paymentData, tierName, premiumLevel)

	logger.Info("Premium payment processed successfully", map[string]interface{}{
		"chat_id":       chatID,
		"amount":        paymentData.Amount,
		"tier":          tierName,
		"premium_level": premiumLevel,
		"session_id":    paymentData.SessionID,
	})
}

// processSubscriptionEvent processes subscription events (created, updated, deleted)
func (b *Bot) processSubscriptionEvent(paymentData *stripe.PaymentData) {
	logger.Info("Processing subscription event", map[string]interface{}{
		"user_id":         paymentData.UserID,
		"event_type":      paymentData.EventType,
		"subscription_id": paymentData.SubscriptionID,
		"tier_name":       paymentData.TierName,
		"billing_period":  paymentData.BillingPeriod,
	})

	if b.db == nil {
		logger.Error("Database not available for subscription event processing", map[string]interface{}{
			"user_id": paymentData.UserID,
		})
		return
	}

	// Convert Telegram user ID to chat ID
	chatID := paymentData.UserID

	// Get user info
	user, err := b.db.GetUserByChatID(chatID)
	if err != nil {
		logger.Error("Failed to get user for subscription event processing", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return
	}

	if user == nil {
		logger.Warn("Subscription event received for non-existent user, skipping processing", map[string]interface{}{
			"chat_id":         chatID,
			"subscription_id": paymentData.SubscriptionID,
			"event_type":      paymentData.EventType,
		})
		return
	}

	// Prevent duplicate notifications during subscription creation flow
	// When Stripe creates a subscription, it often fires multiple events (created + updated) rapidly
	if paymentData.SubscriptionID != "" {
		notificationCacheKey := fmt.Sprintf("subscription_notification_%s", paymentData.SubscriptionID)

		// For subscription creation, check if we've already sent a notification recently
		if paymentData.EventType == "subscription_created" || paymentData.EventType == "checkout_completed" {
			// Mark that we're sending a creation notification
			b.cache.SetWithExpiry(notificationCacheKey, "creation_notified", 5*time.Minute)
		} else if paymentData.EventType == "subscription_updated" || paymentData.EventType == "subscription_plan_or_reactivation" {
			// Check if we just sent a creation notification for this subscription
			if _, exists := b.cache.Get(notificationCacheKey); exists {
				logger.Info("Skipping subscription update notification - creation notification already sent recently", map[string]interface{}{
					"subscription_id": paymentData.SubscriptionID,
					"event_type":      paymentData.EventType,
					"chat_id":         chatID,
				})
				return
			}
		}
		// Note: subscription_plan_upgrade events are NOT cached because they represent real charges
		// that users should be notified about regardless of recent creation notifications
	}

	switch paymentData.EventType {
	case "subscription_created", "checkout_completed":
		b.processSubscriptionCreated(chatID, user, paymentData)
	case "subscription_renewed":
		b.processSubscriptionRenewed(chatID, user, paymentData)
	case "subscription_deleted":
		b.processSubscriptionCancelled(chatID, paymentData)
	case "subscription_plan_changed":
		b.processSubscriptionPlanChanged(chatID, user, paymentData)
	case "subscription_plan_upgrade":
		b.processSubscriptionPlanUpgrade(chatID, user, paymentData)
	case "subscription_plan_or_reactivation":
		b.processSubscriptionPlanOrReactivation(chatID, user, paymentData)
	case "subscription_downgrade_scheduled":
		b.processSubscriptionDowngradeScheduled(chatID, user, paymentData)
	case "subscription_upgrade_scheduled":
		b.processSubscriptionUpgradeScheduled(chatID, user, paymentData)
	case "subscription_change_scheduled":
		b.processSubscriptionChangeScheduled(chatID, user, paymentData)
	case "subscription_cancel_scheduled":
		b.processSubscriptionCancelScheduled(chatID, user, paymentData)
	case "subscription_schedule_cancelled":
		b.processSubscriptionScheduleCancelled(chatID, user, paymentData)
	case "subscription_canceled":
		b.processSubscriptionCancellation(chatID, user, paymentData)
	case "subscription_reactivated":
		b.processSubscriptionReactivated(chatID, user, paymentData)
	case "subscription_updated":
		b.processSubscriptionUpdated(chatID, user, paymentData)
	case "subscription_past_due", "subscription_unpaid":
		b.processSubscriptionPaymentIssue(chatID, user, paymentData)
	default:
		logger.Warn("Unknown subscription event type", map[string]interface{}{
			"event_type": paymentData.EventType,
			"user_id":    paymentData.UserID,
		})
	}
}

// processRefundEvent processes refund events and inserts record into user_topup_log
func (b *Bot) processRefundEvent(paymentData *stripe.PaymentData) {
	logger.Info("Processing refund event", map[string]interface{}{
		"amount":        paymentData.Amount,
		"event_type":    paymentData.EventType,
		"session_id":    paymentData.SessionID,
		"receipt_email": paymentData.ReceiptEmail,
		"invoice_id":    paymentData.InvoiceID,
	})

	// Check if database is configured
	if b.db == nil {
		logger.Warn("Database not configured, cannot record refund", map[string]interface{}{
			"amount":        paymentData.Amount,
			"receipt_email": paymentData.ReceiptEmail,
		})
		return
	}

	// Validate payment amount for refund (should be positive)
	if paymentData.Amount <= 0 {
		logger.Warn("Invalid refund amount, skipping database recording", map[string]interface{}{
			"amount":        paymentData.Amount,
			"receipt_email": paymentData.ReceiptEmail,
			"note":          "Amount must be positive",
		})
		return
	}

	// Use receipt_email as username, session_id (charge_id) as transaction_id
	// No user_id required for refunds
	username := paymentData.ReceiptEmail
	if username == "" {
		username = "unknown" // Fallback if no receipt email
	}

	// Insert refund record into user_topup_log
	// Use 0 as user_id, receipt_email as username, charge_id as transaction_id, receipt_number as invoice_id
	_, err := b.db.CreateTopupLog(0, username, paymentData.Amount, consts.ServiceRefund, paymentData.SessionID, paymentData.InvoiceID)
	if err != nil {
		logger.Error("Failed to insert refund record into database", map[string]interface{}{
			"error":         err.Error(),
			"amount":        paymentData.Amount,
			"receipt_email": paymentData.ReceiptEmail,
			"session_id":    paymentData.SessionID,
			"invoice_id":    paymentData.InvoiceID,
		})
		return
	}

	logger.Info("Refund record inserted into database", map[string]interface{}{
		"amount":         paymentData.Amount,
		"username":       username,
		"transaction_id": paymentData.SessionID,
		"invoice_id":     paymentData.InvoiceID,
		"service":        "REFUND",
	})
}

