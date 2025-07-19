package telegram

import (
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/msg2git/msg2git/internal/consts"
	"github.com/msg2git/msg2git/internal/logger"
	"github.com/msg2git/msg2git/internal/stripe"
)

// sendSubscriptionSuccessNotification sends notification when subscription is successfully created
func (b *Bot) sendSubscriptionSuccessNotification(chatID int64, paymentData *stripe.PaymentData) {
	multiplier := getRepositoryMultiplier(paymentData.PremiumLevel)

	successText := fmt.Sprintf(`🎉 <b>Subscription Activated!</b>

<b>Tier:</b> %s
<b>Billing:</b> %s
<b>Subscription ID:</b> <code>%s</code>

<b>Benefits Unlocked:</b>
🚀 %dx repo size limits
🌇 %dx photo and issue limits
📁 %dx custom files
🧠 %dx free LLM tokens
🎯 Priority support
✨ Automatic renewal (%s)

<i>Thank you for supporting the project! 🙏</i>

Use /insight to see your new limits!
Use /coffee to manage your subscription.`,
		paymentData.TierName, strings.Title(paymentData.BillingPeriod),
		paymentData.SubscriptionID, multiplier, multiplier, multiplier, multiplier, paymentData.BillingPeriod)

	msg := tgbotapi.NewMessage(chatID, successText)
	msg.ParseMode = "html"

	if _, err := b.rateLimitedSend(chatID, msg); err != nil {
		logger.Error("Failed to send subscription success notification", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
	}
}

// sendSubscriptionCancelledNotification sends notification when subscription is cancelled
func (b *Bot) sendSubscriptionCancelledNotification(chatID int64, paymentData *stripe.PaymentData) {
	cancelText := fmt.Sprintf(`😢 <b>Subscription Cancelled</b>

Your premium subscription has been cancelled.

<b>Subscription ID:</b> <code>%s</code>

You'll continue to have access to premium features until the end of your current billing period. After that, your account will revert to the free tier.

<i>Thank you for your support! You can resubscribe anytime using /coffee.</i>`,
		paymentData.SubscriptionID)

	msg := tgbotapi.NewMessage(chatID, cancelText)
	msg.ParseMode = "html"

	if _, err := b.rateLimitedSend(chatID, msg); err != nil {
		logger.Error("Failed to send subscription cancelled notification", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
	}
}

// sendSubscriptionRenewalNotification sends notification when subscription is renewed
func (b *Bot) sendSubscriptionRenewalNotification(chatID int64, paymentData *stripe.PaymentData) {
	renewalText := fmt.Sprintf(`🔄 <b>Subscription Renewed</b>

Your %s subscription has been automatically renewed.

<b>Billing Details:</b>
• Amount: $%.2f
• Period: %s
• Next billing: %s

<i>Your premium features continue without interruption. Thank you for your continued support! 🙏</i>`,
		paymentData.TierName,
		paymentData.Amount,
		strings.Title(paymentData.BillingPeriod),
		func() string {
			if paymentData.BillingPeriod == "monthly" {
				return time.Now().AddDate(0, 1, 0).Format("2006-01-02")
			} else {
				return time.Now().AddDate(1, 0, 0).Format("2006-01-02")
			}
		}())

	msg := tgbotapi.NewMessage(chatID, renewalText)
	msg.ParseMode = "html"

	if _, err := b.rateLimitedSend(chatID, msg); err != nil {
		logger.Error("Failed to send subscription renewal notification", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
	}
}

// sendLegacySubscriptionRenewalNotification sends notification for legacy subscription renewals
func (b *Bot) sendLegacySubscriptionRenewalNotification(chatID int64, paymentData *stripe.PaymentData) {
	legacyRenewalText := fmt.Sprintf(`%s

<b>Notice:</b> We received a renewal payment for a previous subscription.

<b>Payment Details:</b>
• Amount: $%.2f
• Period: %s
• Subscription ID: <code>%s</code>

<i>This appears to be from a legacy subscription. Your current subscription status remains unchanged.</i>

<b>Action Required:</b>
If you have multiple active subscriptions, please review your billing settings and cancel any unwanted subscriptions to avoid duplicate charges.`,
		consts.LegacySubscriptionRenewedMsg,
		paymentData.Amount,
		strings.Title(paymentData.BillingPeriod),
		paymentData.SubscriptionID)

	msg := tgbotapi.NewMessage(chatID, legacyRenewalText)
	msg.ParseMode = "html"

	if _, err := b.rateLimitedSend(chatID, msg); err != nil {
		logger.Error("Failed to send legacy subscription renewal notification", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
	}
}

// sendPremiumPaymentSuccessNotification sends notification for one-time premium payments
func (b *Bot) sendPremiumPaymentSuccessNotification(chatID int64, paymentData *stripe.PaymentData, tierName string, premiumLevel int) {
	multiplier := getRepositoryMultiplier(premiumLevel)

	successText := fmt.Sprintf(`🎉 <b>Premium Activated!</b>

<b>Tier:</b> %s
<b>Amount:</b> $%.2f
<b>Transaction ID:</b> <code>%s</code>

<b>Benefits Unlocked:</b>
🚀 %dx repo size limits
🌇 %dx photo and issue limits
📁 %dx custom files
🧠 %dx free LLM tokens
🎯 Priority support

<i>Thank you for supporting the project! 🙏</i>

Use /insight to see your new limits!`,
		tierName, paymentData.Amount, paymentData.SessionID, multiplier, multiplier, multiplier, multiplier)

	msg := tgbotapi.NewMessage(chatID, successText)
	msg.ParseMode = "html"

	if _, err := b.rateLimitedSend(chatID, msg); err != nil {
		logger.Error("Failed to send premium payment success notification", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
	}
}

// sendSubscriptionPlanChangeNotification sends notification when subscription plan changes
func (b *Bot) sendSubscriptionPlanChangeNotification(chatID int64, paymentData *stripe.PaymentData) {
	multiplier := getRepositoryMultiplier(paymentData.PremiumLevel)

	changeText := fmt.Sprintf(`🔄 <b>Subscription Plan Changed</b>

<b>New Plan:</b> %s (%s)
<b>Subscription ID:</b> <code>%s</code>

<b>Updated Benefits:</b>
🚀 %dx repo size limits
🌇 %dx photo and issue limits
📁 %dx custom files
🧠 %dx free LLM tokens
🎯 Priority support

<i>Your new plan benefits are now active! 🚀</i>

Use /insight to see your updated limits.`,
		paymentData.TierName, strings.Title(paymentData.BillingPeriod),
		paymentData.SubscriptionID, multiplier, multiplier, multiplier, multiplier)

	msg := tgbotapi.NewMessage(chatID, changeText)
	msg.ParseMode = "html"

	if _, err := b.rateLimitedSend(chatID, msg); err != nil {
		logger.Error("Failed to send plan change notification", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
	}
}

// sendSubscriptionPlanUpgradeNotification sends notification when subscription plan is upgraded with prorated charge
func (b *Bot) sendSubscriptionPlanUpgradeNotification(chatID int64, paymentData *stripe.PaymentData) {
	multiplier := getRepositoryMultiplier(paymentData.PremiumLevel)

	upgradeText := fmt.Sprintf(`🎉 <b>Plan Upgraded Successfully!</b>

<b>New Plan:</b> %s (%s)
<b>Prorated Charge:</b> $%.2f
<b>Subscription ID:</b> <code>%s</code>

<b>Upgraded Benefits:</b>
🚀 %dx repo size limits
🌇 %dx photo and issue limits
📁 %dx custom files
🧠 %dx free LLM tokens
🎯 Priority support

<i>Your upgraded plan benefits are now active! The prorated charge covers the remaining billing period. 🚀</i>

Use /insight to see your updated limits.`,
		paymentData.TierName, strings.Title(paymentData.BillingPeriod),
		paymentData.Amount, paymentData.SubscriptionID, multiplier, multiplier, multiplier, multiplier)

	msg := tgbotapi.NewMessage(chatID, upgradeText)
	msg.ParseMode = "html"

	if _, err := b.rateLimitedSend(chatID, msg); err != nil {
		logger.Error("Failed to send plan upgrade notification", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
	}
}

// sendSubscriptionCancelScheduledNotification sends notification when subscription cancellation is scheduled
func (b *Bot) sendSubscriptionCancelScheduledNotification(chatID int64, paymentData *stripe.PaymentData) {
	cancelText := fmt.Sprintf(`⚠️ <b>Subscription Cancellation Scheduled</b>

Your subscription has been set to cancel at the end of the current billing period.

<b>Subscription ID:</b> <code>%s</code>

<b>What happens next:</b>
• You'll keep premium access until your billing period ends
• No further charges will be made
• You can reactivate anytime before the period ends

<i>Want to reactivate? Use /coffee and manage your subscription through the Customer Portal.</i>`,
		paymentData.SubscriptionID)

	msg := tgbotapi.NewMessage(chatID, cancelText)
	msg.ParseMode = "html"

	if _, err := b.rateLimitedSend(chatID, msg); err != nil {
		logger.Error("Failed to send scheduled cancellation notification", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
	}
}

// sendSubscriptionImmediateCancellationNotification sends notification when subscription is cancelled immediately
func (b *Bot) sendSubscriptionImmediateCancellationNotification(chatID int64, paymentData *stripe.PaymentData) {
	cancelText := fmt.Sprintf(`❌ <b>Subscription Cancelled</b>

Your subscription has been cancelled and access has been revoked immediately.

<b>Subscription ID:</b> <code>%s</code>

<b>What happened:</b>
• Premium access has been removed
• No further charges will be made
• You can subscribe again anytime using /coffee

<i>Thank you for your past support! 🙏</i>`,
		paymentData.SubscriptionID)

	msg := tgbotapi.NewMessage(chatID, cancelText)
	msg.ParseMode = "html"

	if _, err := b.rateLimitedSend(chatID, msg); err != nil {
		logger.Error("Failed to send immediate cancellation notification", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
	}
}

// sendSubscriptionReactivatedNotification sends notification when subscription is reactivated
func (b *Bot) sendSubscriptionReactivatedNotification(chatID int64, paymentData *stripe.PaymentData) {
	multiplier := getRepositoryMultiplier(paymentData.PremiumLevel)

	reactivateText := fmt.Sprintf(`🎉 <b>Subscription Reactivated!</b>

Great news! Your subscription has been reactivated and will continue automatically.

<b>Plan:</b> %s (%s)
<b>Subscription ID:</b> <code>%s</code>

<b>Your Benefits:</b>
🚀 %dx repo size limits
🌇 %dx photo and issue limits
📁 %dx custom files
🧠 %dx free LLM tokens
🎯 Priority support
✨ Automatic renewal continues

<i>Welcome back! Your premium features are active again. 🚀</i>

Use /insight to see your limits.`,
		paymentData.TierName, strings.Title(paymentData.BillingPeriod),
		paymentData.SubscriptionID, multiplier, multiplier, multiplier, multiplier)

	msg := tgbotapi.NewMessage(chatID, reactivateText)
	msg.ParseMode = "html"

	if _, err := b.rateLimitedSend(chatID, msg); err != nil {
		logger.Error("Failed to send reactivation notification", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
	}
}

// sendSubscriptionScheduleCancelledNotification sends notification when scheduled plan change is cancelled
func (b *Bot) sendSubscriptionScheduleCancelledNotification(chatID int64, paymentData *stripe.PaymentData) {
	multiplier := getRepositoryMultiplier(paymentData.PremiumLevel)

	// Build the message with conditional subscription ID
	var cancelText string
	if paymentData.SubscriptionID != "" {
		cancelText = fmt.Sprintf(`✅ <b>Scheduled Plan Change Cancelled</b>

Your scheduled plan change has been successfully cancelled.

<b>Current Plan:</b> %s (%s) - Continues unchanged
<b>Subscription ID:</b> <code>%s</code>

<b>Your Current Benefits Continue:</b>
🚀 %dx repo size limits
🌇 %dx photo and issue limits
📁 %dx custom files
🧠 %dx free LLM tokens
🎯 Priority support
✨ Automatic renewal

<i>No changes will occur to your subscription. Your current plan remains active. 🎯</i>

Use /insight to see your current limits.`,
			paymentData.TierName, strings.Title(paymentData.BillingPeriod),
			paymentData.SubscriptionID, multiplier, multiplier, multiplier, multiplier)
	} else {
		cancelText = fmt.Sprintf(`✅ <b>Scheduled Plan Change Cancelled</b>

Your scheduled plan change has been successfully cancelled.

<b>Current Plan:</b> %s (%s) - Continues unchanged

<b>Your Current Benefits Continue:</b>
🚀 %dx repo size limits
🌇 %dx photo and issue limits
📁 %dx custom files
🧠 %dx free LLM tokens
🎯 Priority support
✨ Automatic renewal

<i>No changes will occur to your subscription. Your current plan remains active. 🎯</i>

Use /insight to see your current limits.`,
			paymentData.TierName, strings.Title(paymentData.BillingPeriod), multiplier, multiplier, multiplier, multiplier)
	}

	msg := tgbotapi.NewMessage(chatID, cancelText)
	msg.ParseMode = "HTML"

	if _, err := b.rateLimitedSend(chatID, msg); err != nil {
		logger.Error("Failed to send schedule cancellation notification", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
	}
}

// sendSubscriptionReplacedNotification sends notification when a subscription is replaced
func (b *Bot) sendSubscriptionReplacedNotification(chatID int64, replacedSubscriptionID string) {
	logger.Info("Starting to send subscription replacement notification", map[string]interface{}{
		"chat_id":                  chatID,
		"replaced_subscription_id": replacedSubscriptionID,
	})

	replacedText := fmt.Sprintf(consts.SubscriptionReplacedNotification, replacedSubscriptionID)

	logger.Debug("Generated subscription replacement notification text", map[string]interface{}{
		"chat_id":           chatID,
		"notification_text": replacedText,
		"text_length":       len(replacedText),
	})

	msg := tgbotapi.NewMessage(chatID, replacedText)
	msg.ParseMode = "html"

	logger.Debug("Attempting to send subscription replacement notification", map[string]interface{}{
		"chat_id": chatID,
	})

	if _, err := b.rateLimitedSend(chatID, msg); err != nil {
		logger.Error("Failed to send subscription replaced notification", map[string]interface{}{
			"error":                    err.Error(),
			"chat_id":                  chatID,
			"replaced_subscription_id": replacedSubscriptionID,
		})
	} else {
		logger.Info("Successfully sent subscription replacement notification", map[string]interface{}{
			"chat_id":                  chatID,
			"replaced_subscription_id": replacedSubscriptionID,
		})
	}
}

// sendSubscriptionUpdateNotification sends notification for general subscription updates
func (b *Bot) sendSubscriptionUpdateNotification(chatID int64, paymentData *stripe.PaymentData) {
	logger.Debug("Sending standard update notification", map[string]interface{}{
		"chat_id":        chatID,
		"premium_level":  paymentData.PremiumLevel,
		"tier_name":      paymentData.TierName,
		"billing_period": paymentData.BillingPeriod,
	})

	multiplier := getRepositoryMultiplier(paymentData.PremiumLevel)

	updateText := fmt.Sprintf(`🔄 <b>Subscription Updated</b>

Your subscription has been updated successfully.

<b>Current Plan:</b> %s (%s)
<b>Subscription ID:</b> <code>%s</code>

<b>Active Benefits:</b>
🚀 %dx repo size limits
🌇 %dx photo and issue limits
📁 %dx custom files
🧠 %dx free LLM tokens
🎯 Priority support

<i>Your subscription continues with updated settings. 📝</i>

Use /insight to see your current limits.`,
		paymentData.TierName, strings.Title(paymentData.BillingPeriod),
		paymentData.SubscriptionID, multiplier, multiplier, multiplier, multiplier)

	msg := tgbotapi.NewMessage(chatID, updateText)
	msg.ParseMode = "html"

	if _, err := b.rateLimitedSend(chatID, msg); err != nil {
		logger.Error("Failed to send subscription update notification", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
	}
}

// sendSubscriptionDowngradeNotification sends notification when subscription is downgraded
func (b *Bot) sendSubscriptionDowngradeNotification(chatID int64, paymentData *stripe.PaymentData, currentPremiumLevel int, currentBillingPeriod string) {
	logger.Debug("Sending downgrade notification", map[string]interface{}{
		"chat_id":               chatID,
		"current_premium_level": currentPremiumLevel,
		"new_premium_level":     paymentData.PremiumLevel,
		"current_billing":       currentBillingPeriod,
		"new_billing":           paymentData.BillingPeriod,
	})

	// Get tier names for current and future plans
	tierNames := []string{"Free", "☕ Coffee", "🍰 Cake", "🎁 Sponsor"}
	currentTierName := "Premium"
	if currentPremiumLevel < len(tierNames) {
		currentTierName = tierNames[currentPremiumLevel]
	}

	currentMultiplier := getRepositoryMultiplier(currentPremiumLevel)
	futureMultiplier := getRepositoryMultiplier(paymentData.PremiumLevel)

	downgradeText := fmt.Sprintf(`🔄 <b>Subscription Plan Downgrade Scheduled</b>

Your subscription plan will be downgraded at the end of your current billing period.

<b>Current Plan:</b> %s (%s) - <i>Active until period ends</i>
<b>Next Plan:</b> %s (%s) - <i>Takes effect at renewal</i>
<b>Subscription ID:</b> <code>%s</code>

<b>Current Active Benefits:</b>
🚀 %dx repo size limits
🌇 %dx photo and issue limits
📁 %dx custom files
🧠 %dx free LLM tokens
🎯 Priority support

<b>Benefits After Downgrade:</b>
🚀 %dx repo size limits
🌇 %dx photo and issue limits
📁 %dx custom files
🧠 %dx free LLM tokens
🎯 Priority support

<i>Your current plan benefits remain active until the end of this billing period. The downgrade will take effect at your next renewal date. 📝</i>

Use /insight to see your current limits.`,
		currentTierName, strings.Title(currentBillingPeriod),
		paymentData.TierName, strings.Title(paymentData.BillingPeriod),
		paymentData.SubscriptionID,
		currentMultiplier, currentMultiplier, currentMultiplier, currentMultiplier,
		futureMultiplier, futureMultiplier, futureMultiplier, futureMultiplier)

	msg := tgbotapi.NewMessage(chatID, downgradeText)
	msg.ParseMode = "html"

	if _, err := b.rateLimitedSend(chatID, msg); err != nil {
		logger.Error("Failed to send subscription downgrade notification", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
	}
}

// sendScheduledDowngradeNotification sends notification for scheduled downgrades
func (b *Bot) sendScheduledDowngradeNotification(chatID int64, paymentData *stripe.PaymentData) {
	logger.Info("Sending scheduled downgrade notification", map[string]interface{}{
		"chat_id":       chatID,
		"current_tier":  paymentData.TierName,
		"current_level": paymentData.PremiumLevel,
		"future_tier":   paymentData.FutureTierName,
		"future_level":  paymentData.FuturePremiumLevel,
	})

	currentMultiplier := getRepositoryMultiplier(paymentData.PremiumLevel)
	futureMultiplier := getRepositoryMultiplier(paymentData.FuturePremiumLevel)

	// Format the scheduled change date
	changeDate := time.Unix(paymentData.ScheduledChangeDate, 0).Format("2006-01-02")

	downgradeText := fmt.Sprintf(`🔄 <b>Subscription Plan Downgrade Scheduled</b>

Your subscription plan will be downgraded at the end of your current billing period.

<b>Current Plan:</b> %s (%s) - <i>Active until period ends</i>
<b>Next Plan:</b> %s (%s) - <i>Takes effect %s</i>
<b>Subscription ID:</b> <code>%s</code>

<b>Current Active Benefits:</b>
🚀 %dx repo size limits
🌇 %dx photo and issue limits
📁 %dx custom files
🧠 %dx free LLM tokens
🎯 Priority support

<b>Benefits After Downgrade:</b>
🚀 %dx repo size limits
🌇 %dx photo and issue limits
📁 %dx custom files
🧠 %dx free LLM tokens
🎯 Priority support

<i>Your current plan benefits remain active until the end of this billing period. The downgrade will take effect on %s. 📝</i>

Use /insight to see your current limits.`,
		paymentData.TierName, strings.Title(paymentData.BillingPeriod),
		paymentData.FutureTierName, strings.Title(paymentData.FutureBillingPeriod),
		changeDate,
		paymentData.SubscriptionID,
		currentMultiplier, currentMultiplier, currentMultiplier, currentMultiplier,
		futureMultiplier, futureMultiplier, futureMultiplier, futureMultiplier,
		changeDate)

	msg := tgbotapi.NewMessage(chatID, downgradeText)
	msg.ParseMode = "html"

	if _, err := b.rateLimitedSend(chatID, msg); err != nil {
		logger.Error("Failed to send scheduled downgrade notification", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
	}
}

// sendScheduledUpgradeNotification sends notification for scheduled upgrades
func (b *Bot) sendScheduledUpgradeNotification(chatID int64, paymentData *stripe.PaymentData) {
	logger.Info("Sending scheduled upgrade notification", map[string]interface{}{
		"chat_id":       chatID,
		"current_tier":  paymentData.TierName,
		"current_level": paymentData.PremiumLevel,
		"future_tier":   paymentData.FutureTierName,
		"future_level":  paymentData.FuturePremiumLevel,
	})

	currentMultiplier := getRepositoryMultiplier(paymentData.PremiumLevel)
	futureMultiplier := getRepositoryMultiplier(paymentData.FuturePremiumLevel)

	// Format the scheduled change date
	changeDate := time.Unix(paymentData.ScheduledChangeDate, 0).Format("2006-01-02")

	upgradeText := fmt.Sprintf(`🎉 <b>Subscription Plan Upgrade Scheduled</b>

Your subscription plan will be upgraded at the end of your current billing period.

<b>Current Plan:</b> %s (%s) - <i>Active until period ends</i>
<b>Next Plan:</b> %s (%s) - <i>Takes effect %s</i>
<b>Subscription ID:</b> <code>%s</code>

<b>Current Benefits:</b>
🚀 %dx repo size limits
🌇 %dx photo and issue limits
📁 %dx custom files
🧠 %dx free LLM tokens
🎯 Priority support

<b>Enhanced Benefits After Upgrade:</b>
🚀 %dx repo size limits
🌇 %dx photo and issue limits
📁 %dx custom files
🧠 %dx free LLM tokens
🎯 Priority support

<i>Your current plan remains active until the end of this billing period. The upgrade will take effect on %s. 🚀</i>

Use /insight to see your current limits.`,
		paymentData.TierName, strings.Title(paymentData.BillingPeriod),
		paymentData.FutureTierName, strings.Title(paymentData.FutureBillingPeriod),
		changeDate,
		paymentData.SubscriptionID,
		currentMultiplier, currentMultiplier, currentMultiplier, currentMultiplier,
		futureMultiplier, futureMultiplier, futureMultiplier, futureMultiplier,
		changeDate)

	msg := tgbotapi.NewMessage(chatID, upgradeText)
	msg.ParseMode = "html"

	if _, err := b.rateLimitedSend(chatID, msg); err != nil {
		logger.Error("Failed to send scheduled upgrade notification", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
	}
}

// sendScheduledChangeNotification sends notification for scheduled plan changes
func (b *Bot) sendScheduledChangeNotification(chatID int64, paymentData *stripe.PaymentData) {
	logger.Info("Sending scheduled change notification", map[string]interface{}{
		"chat_id":       chatID,
		"current_tier":  paymentData.TierName,
		"current_level": paymentData.PremiumLevel,
		"future_tier":   paymentData.FutureTierName,
		"future_level":  paymentData.FuturePremiumLevel,
	})

	currentMultiplier := getRepositoryMultiplier(paymentData.PremiumLevel)
	futureMultiplier := getRepositoryMultiplier(paymentData.FuturePremiumLevel)

	// Format the scheduled change date
	changeDate := time.Unix(paymentData.ScheduledChangeDate, 0).Format("2006-01-02")

	changeText := fmt.Sprintf(`🔄 <b>Subscription Plan Change Scheduled</b>

Your subscription plan will be changed at the end of your current billing period.

<b>Current Plan:</b> %s (%s) - <i>Active until period ends</i>
<b>Next Plan:</b> %s (%s) - <i>Takes effect %s</i>
<b>Subscription ID:</b> <code>%s</code>

<b>Current Benefits:</b>
🚀 %dx repo size limits
🌇 %dx photo and issue limits
📁 %dx custom files
🧠 %dx free LLM tokens
🎯 Priority support

<b>Updated Benefits:</b>
🚀 %dx repo size limits
🌇 %dx photo and issue limits
📁 %dx custom files
🧠 %dx free LLM tokens
🎯 Priority support

<i>Your current plan remains active until the end of this billing period. The change will take effect on %s. 📝</i>

Use /insight to see your current limits.`,
		paymentData.TierName, strings.Title(paymentData.BillingPeriod),
		paymentData.FutureTierName, strings.Title(paymentData.FutureBillingPeriod),
		changeDate,
		paymentData.SubscriptionID,
		currentMultiplier, currentMultiplier, currentMultiplier, currentMultiplier,
		futureMultiplier, futureMultiplier, futureMultiplier, futureMultiplier,
		changeDate)

	msg := tgbotapi.NewMessage(chatID, changeText)
	msg.ParseMode = "html"

	if _, err := b.rateLimitedSend(chatID, msg); err != nil {
		logger.Error("Failed to send scheduled change notification", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
	}
}

// sendSubscriptionPaymentIssueNotification sends notification when there are payment issues
func (b *Bot) sendSubscriptionPaymentIssueNotification(chatID int64, paymentData *stripe.PaymentData) {
	issueText := fmt.Sprintf(`⚠️ <b>Subscription Payment Issue</b>

There's an issue with your subscription payment.

<b>Subscription ID:</b> <code>%s</code>
<b>Status:</b> %s

<b>Action Required:</b>
Please update your payment method to continue enjoying premium features.

<b>How to fix:</b>
1. Use /coffee to access subscription management
2. Click "Customer Portal" to update payment method
3. Or contact support if you need assistance

<i>Your premium access remains active while we resolve this issue.</i>`,
		paymentData.SubscriptionID,
		strings.ReplaceAll(paymentData.EventType, "subscription_", ""))

	msg := tgbotapi.NewMessage(chatID, issueText)
	msg.ParseMode = "html"

	if _, err := b.rateLimitedSend(chatID, msg); err != nil {
		logger.Error("Failed to send payment issue notification", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
	}
}
