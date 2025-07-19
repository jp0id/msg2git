package telegram

import (
	"fmt"
	"time"

	"github.com/msg2git/msg2git/internal/consts"
	"github.com/msg2git/msg2git/internal/database"
	"github.com/msg2git/msg2git/internal/logger"
	"github.com/msg2git/msg2git/internal/stripe"
)

// processSubscriptionCreated handles subscription creation events
func (b *Bot) processSubscriptionCreated(chatID int64, user *database.User, paymentData *stripe.PaymentData) {
	// Prevent duplicate processing of the same subscription event
	if paymentData.SubscriptionID != "" {
		cacheKey := fmt.Sprintf("subscription_processed_%s", paymentData.SubscriptionID)
		if _, exists := b.cache.Get(cacheKey); exists {
			logger.Info("Subscription event already processed recently, skipping", map[string]interface{}{
				"subscription_id": paymentData.SubscriptionID,
				"chat_id":         chatID,
			})
			return
		}
		// Mark this subscription as processed for 10 minutes
		b.cache.SetWithExpiry(cacheKey, "processed", 10*time.Minute)
	}

	// Handle different event types appropriately
	if paymentData.EventType == "checkout_completed" && paymentData.SubscriptionID == "" {
		// Checkout completed but subscription not created yet - create basic premium user
		// The subscription_created event will update with subscription details later
		logger.Info("Processing checkout completion, subscription details will be updated later", map[string]interface{}{
			"chat_id": chatID,
			"tier":    paymentData.TierName,
		})
		
		// Create basic premium user without subscription details for now
		subscriptionResult, err := b.db.CreateSubscriptionPremiumUser(
			chatID,
			user.Username,
			paymentData.PremiumLevel,
			"", // Empty subscription ID for now
			paymentData.CustomerID,
			paymentData.BillingPeriod,
		)
		if err != nil {
			logger.Error("Failed to create basic premium user", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": chatID,
			})
		} else {
			// Send notification if a subscription was replaced during checkout completion
			logger.Debug("Checking for subscription replacement notification in checkout completion", map[string]interface{}{
				"chat_id":                    chatID,
				"replaced_subscription_id":   subscriptionResult.ReplacedSubscriptionID,
				"should_send_notification":   subscriptionResult.ReplacedSubscriptionID != "",
			})
			if subscriptionResult.ReplacedSubscriptionID != "" {
				logger.Info("Sending subscription replacement notification from checkout completion", map[string]interface{}{
					"chat_id":                  chatID,
					"replaced_subscription_id": subscriptionResult.ReplacedSubscriptionID,
				})
				b.sendSubscriptionReplacedNotification(chatID, subscriptionResult.ReplacedSubscriptionID)
			} else {
				logger.Debug("No subscription replacement notification needed in checkout completion", map[string]interface{}{
					"chat_id": chatID,
				})
			}
		}
		return // Don't send notification or log payment yet
	}
	
	// Check if user already has an active premium subscription to avoid duplicate notifications
	existingPremiumUser, err := b.db.GetPremiumUser(chatID)
	if err != nil {
		logger.Error("Failed to check existing premium user", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		// Continue anyway - don't fail the subscription creation
	}
	
	// Determine if this is truly a new subscription or an update
	isNewSubscription := true
	if existingPremiumUser != nil && existingPremiumUser.IsPremiumUser() && existingPremiumUser.IsSubscription {
		// User already has an active subscription - this might be a plan change or duplicate event
		if existingPremiumUser.SubscriptionID != "" && existingPremiumUser.SubscriptionID != paymentData.SubscriptionID {
			// Different subscription ID - this is a new subscription (old one was cancelled)
			isNewSubscription = true
		} else if existingPremiumUser.SubscriptionID == paymentData.SubscriptionID {
			// Same subscription ID - this is likely a webhook retry or update, not a new subscription
			isNewSubscription = false
		} else if existingPremiumUser.SubscriptionID == "" && paymentData.SubscriptionID != "" {
			// Existing user with no subscription ID getting subscription ID - this is subscription activation from checkout
			isNewSubscription = true
			logger.Debug("Detected subscription activation (checkout → subscription creation)", map[string]interface{}{
				"chat_id":         chatID,
				"subscription_id": paymentData.SubscriptionID,
			})
		} else {
			// Other cases - treat as update
			isNewSubscription = false
		}
	}
	
	// This is a subscription_created event with actual subscription ID
	// Create or update subscription-based premium user
	logger.Info("Creating subscription premium user", map[string]interface{}{
		"chat_id":         chatID,
		"username":        user.Username,
		"premium_level":   paymentData.PremiumLevel,
		"subscription_id": paymentData.SubscriptionID,
		"customer_id":     paymentData.CustomerID,
		"billing_period":  paymentData.BillingPeriod,
	})
	subscriptionResult, err := b.db.CreateSubscriptionPremiumUser(
		chatID,
		user.Username,
		paymentData.PremiumLevel,
		paymentData.SubscriptionID,
		paymentData.CustomerID,
		paymentData.BillingPeriod,
	)
	if err != nil {
		logger.Error("Failed to create/update subscription premium user", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return
	}

	// Send notification if a subscription was replaced
	logger.Debug("Checking for subscription replacement notification", map[string]interface{}{
		"chat_id":                    chatID,
		"replaced_subscription_id":   subscriptionResult.ReplacedSubscriptionID,
		"should_send_notification":   subscriptionResult.ReplacedSubscriptionID != "",
	})
	if subscriptionResult.ReplacedSubscriptionID != "" {
		logger.Info("Sending subscription replacement notification", map[string]interface{}{
			"chat_id":                  chatID,
			"replaced_subscription_id": subscriptionResult.ReplacedSubscriptionID,
		})
		b.sendSubscriptionReplacedNotification(chatID, subscriptionResult.ReplacedSubscriptionID)
	} else {
		logger.Debug("No subscription replacement notification needed", map[string]interface{}{
			"chat_id": chatID,
		})
	}

	// Set the correct expiry date from Stripe invoice data
	if paymentData.RenewalDate > 0 {
		// Use actual renewal date from Stripe invoice
		err = b.db.SetSubscriptionExpiry(chatID, paymentData.RenewalDate)
		logger.Debug("Set subscription expiry from actual renewal date", map[string]interface{}{
			"chat_id":      chatID,
			"renewal_date": paymentData.RenewalDate,
			"renewal_time": time.Unix(paymentData.RenewalDate, 0).Format("2006-01-02 15:04:05"),
		})
	} else {
		// Fallback to calculated renewal date
		err = b.db.RenewSubscriptionPremiumUser(chatID, paymentData.BillingPeriod)
		logger.Debug("Set subscription expiry from calculated renewal date", map[string]interface{}{
			"chat_id":        chatID,
			"billing_period": paymentData.BillingPeriod,
		})
	}
	
	if err != nil {
		logger.Error("Failed to set subscription expiry date", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		// Don't return here - subscription creation was successful, just expiry setting failed
	}

	// Log subscription creation in topup_log
	var serviceName string
	var amount float64
	
	// Map premium level to service name
	switch paymentData.PremiumLevel {
	case consts.PremiumLevelCoffee:
		serviceName = consts.ServiceCoffee
	case consts.PremiumLevelCake:
		serviceName = consts.ServiceCake
	case consts.PremiumLevelSponsor:
		serviceName = consts.ServiceSponsor
	default:
		serviceName = consts.ServiceCoffee
	}
	
	// Use actual amount from Stripe if available, otherwise fallback to constants
	if paymentData.Amount > 0 {
		amount = paymentData.Amount
	} else {
		// Fallback to hardcoded amounts if Stripe amount is not available
		switch paymentData.PremiumLevel {
		case consts.PremiumLevelCoffee:
			if paymentData.BillingPeriod == "annually" {
				amount = consts.PriceCoffee * 12
			} else {
				amount = consts.PriceCoffee
			}
		case consts.PremiumLevelCake:
			if paymentData.BillingPeriod == "annually" {
				amount = consts.PriceCake * 12
			} else {
				amount = consts.PriceCake
			}
		case consts.PremiumLevelSponsor:
			if paymentData.BillingPeriod == "annually" {
				amount = consts.PriceSponsor * 12
			} else {
				amount = consts.PriceSponsor
			}
		default:
			amount = consts.PriceCoffee
		}
	}
	
	// Create topup log entry for subscription
	_, err = b.db.CreateTopupLog(
		chatID,
		user.Username,
		amount,
		serviceName,
		paymentData.SubscriptionID, // Use subscription ID as transaction ID
		paymentData.InvoiceID, // Stripe invoice ID
	)
	if err != nil {
		logger.Error("Failed to create subscription topup log", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		// Don't return here - subscription creation was successful, just logging failed
	}

	// Only send success notification for truly new subscriptions
	if isNewSubscription {
		b.sendSubscriptionSuccessNotification(chatID, paymentData)
		logger.Info("New subscription created successfully", map[string]interface{}{
			"chat_id":         chatID,
			"subscription_id": paymentData.SubscriptionID,
			"tier":            paymentData.TierName,
			"premium_level":   paymentData.PremiumLevel,
			"billing_period":  paymentData.BillingPeriod,
			"amount":          amount,
			"service":         serviceName,
		})
	} else {
		logger.Info("Subscription updated (no notification sent)", map[string]interface{}{
			"chat_id":         chatID,
			"subscription_id": paymentData.SubscriptionID,
			"tier":            paymentData.TierName,
			"premium_level":   paymentData.PremiumLevel,
			"billing_period":  paymentData.BillingPeriod,
			"reason":          "existing subscription or duplicate event",
		})
	}
}

// processSubscriptionRenewed handles subscription renewal events
func (b *Bot) processSubscriptionRenewed(chatID int64, user *database.User, paymentData *stripe.PaymentData) {
	// Check if premium user record exists first
	existingPremiumUser, err := b.db.GetPremiumUser(chatID)
	if err != nil {
		logger.Error("Failed to check existing premium user", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return
	}
	
	// Track if this is a new subscription creation (race condition) or existing renewal
	isNewSubscription := false
	
	// Handle race condition: invoice webhook arrives before subscription creation webhook
	if existingPremiumUser == nil || !existingPremiumUser.IsSubscription {
		logger.Warn("Invoice payment received but subscription not found - creating subscription user first", map[string]interface{}{
			"chat_id":         chatID,
			"subscription_id": paymentData.SubscriptionID,
			"customer_id":     paymentData.CustomerID,
			"renewal_date":    paymentData.RenewalDate,
		})
		
		isNewSubscription = true // This is a new subscription, not a renewal
		
		// Create the subscription user first with the invoice payment data
		subscriptionResult, err := b.db.CreateSubscriptionPremiumUser(
			chatID,
			user.Username,
			paymentData.PremiumLevel,
			paymentData.SubscriptionID,
			paymentData.CustomerID,
			paymentData.BillingPeriod,
		)
		if err != nil {
			logger.Error("Failed to create subscription user from invoice webhook", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": chatID,
			})
			return
		}

		// Send notification if a subscription was replaced
		logger.Debug("Checking for subscription replacement notification in renewal process", map[string]interface{}{
			"chat_id":                    chatID,
			"replaced_subscription_id":   subscriptionResult.ReplacedSubscriptionID,
			"should_send_notification":   subscriptionResult.ReplacedSubscriptionID != "",
		})
		if subscriptionResult.ReplacedSubscriptionID != "" {
			logger.Info("Sending subscription replacement notification from renewal process", map[string]interface{}{
				"chat_id":                  chatID,
				"replaced_subscription_id": subscriptionResult.ReplacedSubscriptionID,
			})
			b.sendSubscriptionReplacedNotification(chatID, subscriptionResult.ReplacedSubscriptionID)
		} else {
			logger.Debug("No subscription replacement notification needed in renewal process", map[string]interface{}{
				"chat_id": chatID,
			})
		}
		
		logger.Info("Created subscription user from invoice webhook (race condition handled)", map[string]interface{}{
			"chat_id":         chatID,
			"subscription_id": paymentData.SubscriptionID,
		})
	}
	
	// Validate subscription_id before updating expiry (but only for existing users, not new subscriptions)
	isLegacyRenewal := false
	if !isNewSubscription && existingPremiumUser != nil {
		if existingPremiumUser.SubscriptionID != "" && existingPremiumUser.SubscriptionID != paymentData.SubscriptionID {
			isLegacyRenewal = true
			logger.Warn("Subscription renewal ignored - subscription_id mismatch (legacy renewal)", map[string]interface{}{
				"chat_id":                    chatID,
				"webhook_subscription_id":    paymentData.SubscriptionID,
				"database_subscription_id":   existingPremiumUser.SubscriptionID,
			})
		} else {
			logger.Debug("Subscription ID validation passed for renewal", map[string]interface{}{
				"chat_id":         chatID,
				"subscription_id": paymentData.SubscriptionID,
			})
		}
	}

	// Only update expiry if this is not a legacy renewal
	if !isLegacyRenewal {
		// Now extend subscription expiry date using actual renewal date from Stripe if available
		if paymentData.RenewalDate > 0 {
			// Use actual renewal date from Stripe invoice
			err = b.db.SetSubscriptionExpiry(chatID, paymentData.RenewalDate)
			logger.Debug("Using actual renewal date from Stripe", map[string]interface{}{
				"chat_id":      chatID,
				"renewal_date": paymentData.RenewalDate,
				"renewal_time": time.Unix(paymentData.RenewalDate, 0).Format("2006-01-02 15:04:05"),
			})
		} else {
			// Fallback to calculated renewal date
			err = b.db.RenewSubscriptionPremiumUser(chatID, paymentData.BillingPeriod)
			logger.Debug("Using calculated renewal date (fallback)", map[string]interface{}{
				"chat_id":        chatID,
				"billing_period": paymentData.BillingPeriod,
			})
		}
		
		if err != nil {
			logger.Error("Failed to renew subscription premium user", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": chatID,
			})
			return
		}
	} else {
		logger.Info("Skipped expiry update for legacy subscription renewal", map[string]interface{}{
			"chat_id":         chatID,
			"subscription_id": paymentData.SubscriptionID,
		})
	}

	// Log subscription renewal in topup_log
	var serviceName string
	var amount = paymentData.Amount // Amount comes from Stripe invoice
	
	// Map premium level to service name
	switch paymentData.PremiumLevel {
	case consts.PremiumLevelCoffee:
		serviceName = consts.ServiceCoffee
	case consts.PremiumLevelCake:
		serviceName = consts.ServiceCake
	case consts.PremiumLevelSponsor:
		serviceName = consts.ServiceSponsor
	default:
		serviceName = consts.ServiceCoffee
	}
	
	// Create topup log entry for subscription renewal
	_, err = b.db.CreateTopupLog(
		chatID,
		user.Username,
		amount,
		serviceName,
		paymentData.SubscriptionID, // Use subscription ID as transaction ID
		paymentData.InvoiceID, // Stripe invoice ID
	)
	if err != nil {
		logger.Error("Failed to create subscription renewal topup log", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		// Don't return here - renewal was successful, just logging failed
	}

	// Send appropriate notification based on whether this is new subscription, legacy renewal, or regular renewal
	if isNewSubscription {
		// This is a new subscription created from invoice webhook (race condition)
		b.sendSubscriptionSuccessNotification(chatID, paymentData)
		logger.Info("Sent subscription activation notification (from invoice webhook)", map[string]interface{}{
			"chat_id":         chatID,
			"subscription_id": paymentData.SubscriptionID,
		})
	} else if isLegacyRenewal {
		// This is a legacy subscription renewal (mismatched subscription_id)
		b.sendLegacySubscriptionRenewalNotification(chatID, paymentData)
		logger.Info("Sent legacy subscription renewal notification", map[string]interface{}{
			"chat_id":         chatID,
			"subscription_id": paymentData.SubscriptionID,
		})
	} else {
		// This is a regular subscription renewal
		b.sendSubscriptionRenewalNotification(chatID, paymentData)
		logger.Info("Sent subscription renewal notification", map[string]interface{}{
			"chat_id":         chatID,
			"subscription_id": paymentData.SubscriptionID,
		})
	}

	logger.Info("Subscription renewed successfully", map[string]interface{}{
		"chat_id":         chatID,
		"subscription_id": paymentData.SubscriptionID,
		"tier":            paymentData.TierName,
		"premium_level":   paymentData.PremiumLevel,
		"billing_period":  paymentData.BillingPeriod,
		"amount":          amount,
		"service":         serviceName,
	})
}

// processSubscriptionCancelled handles subscription cancellation events
func (b *Bot) processSubscriptionCancelled(chatID int64, paymentData *stripe.PaymentData) {
	// Get current premium user to validate subscription_id
	premiumUser, err := b.db.GetPremiumUser(chatID)
	if err != nil {
		logger.Error("Failed to get premium user for subscription validation", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return
	}

	// Validate that we have a subscription to cancel
	if premiumUser == nil || !premiumUser.IsPremiumUser() || !premiumUser.IsSubscription {
		logger.Warn("Subscription cancellation ignored - no active subscription found", map[string]interface{}{
			"chat_id":                  chatID,
			"webhook_subscription_id":  paymentData.SubscriptionID,
			"user_has_premium":         premiumUser != nil && premiumUser.IsPremiumUser(),
			"user_has_subscription":    premiumUser != nil && premiumUser.IsSubscription,
		})
		return
	}

	// Validate that the subscription being cancelled matches the one in our database
	if paymentData.SubscriptionID != "" && premiumUser.SubscriptionID != "" {
		if premiumUser.SubscriptionID != paymentData.SubscriptionID {
			logger.Warn("Subscription cancellation ignored - subscription_id mismatch", map[string]interface{}{
				"chat_id":                    chatID,
				"webhook_subscription_id":    paymentData.SubscriptionID,
				"database_subscription_id":   premiumUser.SubscriptionID,
			})
			// Still log the termination for the webhook subscription but don't reset premium user
			_, logErr := b.db.CreateSubscriptionChangeLog(chatID, paymentData.SubscriptionID, consts.SubscriptionOperationTerminate)
			if logErr != nil {
				logger.Warn("Failed to create subscription termination log", map[string]interface{}{
					"user_id":         chatID,
					"subscription_id": paymentData.SubscriptionID,
					"error":           logErr.Error(),
				})
			}
			return
		}
	}

	// Log subscription termination for the matching subscription
	subscriptionIDToLog := paymentData.SubscriptionID
	if subscriptionIDToLog == "" {
		subscriptionIDToLog = premiumUser.SubscriptionID
	}
	
	if subscriptionIDToLog != "" {
		_, err = b.db.CreateSubscriptionChangeLog(chatID, subscriptionIDToLog, consts.SubscriptionOperationTerminate)
		if err != nil {
			logger.Warn("Failed to create subscription termination log", map[string]interface{}{
				"user_id":         chatID,
				"subscription_id": subscriptionIDToLog,
				"error":           err.Error(),
			})
		}
	}

	// Proceed with cancellation since validation passed
	err = b.db.CancelSubscriptionPremiumUser(chatID)
	if err != nil {
		logger.Error("Failed to cancel subscription premium user", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return
	}

	// Send cancellation notification to user
	b.sendSubscriptionCancelledNotification(chatID, paymentData)

	logger.Info("Subscription cancelled successfully", map[string]interface{}{
		"chat_id":         chatID,
		"webhook_subscription_id": paymentData.SubscriptionID,
		"database_subscription_id": premiumUser.SubscriptionID,
	})
}

// processSubscriptionPlanChanged handles subscription plan change events
func (b *Bot) processSubscriptionPlanChanged(chatID int64, user *database.User, paymentData *stripe.PaymentData) {
	// Update premium user with new plan details
	subscriptionResult, err := b.db.CreateSubscriptionPremiumUser(
		chatID,
		user.Username,
		paymentData.PremiumLevel,
		paymentData.SubscriptionID,
		paymentData.CustomerID,
		paymentData.BillingPeriod,
	)
	if err != nil {
		logger.Error("Failed to update premium user for plan change", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return
	}

	// Send notification if a subscription was replaced (shouldn't happen in plan change, but safety check)
	if subscriptionResult.ReplacedSubscriptionID != "" {
		b.sendSubscriptionReplacedNotification(chatID, subscriptionResult.ReplacedSubscriptionID)
	}

	// Update expiry date to reflect new billing period
	if paymentData.RenewalDate > 0 {
		// Use actual renewal date from Stripe invoice
		err = b.db.SetSubscriptionExpiry(chatID, paymentData.RenewalDate)
		logger.Debug("Updated subscription expiry for plan change (from Stripe)", map[string]interface{}{
			"chat_id":      chatID,
			"renewal_date": paymentData.RenewalDate,
			"renewal_time": time.Unix(paymentData.RenewalDate, 0).Format("2006-01-02 15:04:05"),
			"new_billing":  paymentData.BillingPeriod,
		})
	} else {
		// Fallback to calculated renewal date based on new billing period
		err = b.db.RenewSubscriptionPremiumUser(chatID, paymentData.BillingPeriod)
		logger.Debug("Updated subscription expiry for plan change (calculated)", map[string]interface{}{
			"chat_id":        chatID,
			"billing_period": paymentData.BillingPeriod,
		})
	}
	
	if err != nil {
		logger.Error("Failed to update subscription expiry date for plan change", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		// Don't return here - plan change was successful, just expiry update failed
	}

	// Send plan change notification
	b.sendSubscriptionPlanChangeNotification(chatID, paymentData)

	logger.Info("Subscription plan changed", map[string]interface{}{
		"chat_id":         chatID,
		"subscription_id": paymentData.SubscriptionID,
		"new_tier":        paymentData.TierName,
		"new_billing":     paymentData.BillingPeriod,
	})
}

// processSubscriptionPlanUpgrade handles subscription plan upgrade events with prorated charges
func (b *Bot) processSubscriptionPlanUpgrade(chatID int64, user *database.User, paymentData *stripe.PaymentData) {
	// Update premium user with new plan details from the subscription.updated event
	// The invoice.payment_succeeded event gives us the prorated amount
	subscriptionResult, err := b.db.CreateSubscriptionPremiumUser(
		chatID,
		user.Username,
		paymentData.PremiumLevel,
		paymentData.SubscriptionID,
		paymentData.CustomerID,
		paymentData.BillingPeriod,
	)
	if err != nil {
		logger.Error("Failed to update premium user for plan upgrade", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return
	}

	// Send notification if a subscription was replaced (shouldn't happen in upgrade, but safety check)
	if subscriptionResult.ReplacedSubscriptionID != "" {
		b.sendSubscriptionReplacedNotification(chatID, subscriptionResult.ReplacedSubscriptionID)
	}

	// Update expiry date to reflect new billing period
	if paymentData.RenewalDate > 0 {
		// Use actual renewal date from Stripe invoice
		err = b.db.SetSubscriptionExpiry(chatID, paymentData.RenewalDate)
		logger.Debug("Updated subscription expiry for plan upgrade (from Stripe)", map[string]interface{}{
			"chat_id":      chatID,
			"renewal_date": paymentData.RenewalDate,
			"renewal_time": time.Unix(paymentData.RenewalDate, 0).Format("2006-01-02 15:04:05"),
			"new_billing":  paymentData.BillingPeriod,
		})
	} else {
		// Fallback to calculated renewal date based on new billing period
		err = b.db.RenewSubscriptionPremiumUser(chatID, paymentData.BillingPeriod)
		logger.Debug("Updated subscription expiry for plan upgrade (calculated)", map[string]interface{}{
			"chat_id":        chatID,
			"billing_period": paymentData.BillingPeriod,
		})
	}
	
	if err != nil {
		logger.Error("Failed to update subscription expiry date for plan upgrade", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		// Don't return here - plan upgrade was successful, just expiry update failed
	}

	// Log the prorated upgrade charge in topup_log
	var serviceName string
	switch paymentData.PremiumLevel {
	case consts.PremiumLevelCoffee:
		serviceName = consts.ServiceCoffee
	case consts.PremiumLevelCake:
		serviceName = consts.ServiceCake
	case consts.PremiumLevelSponsor:
		serviceName = consts.ServiceSponsor
	default:
		serviceName = consts.ServiceCoffee
	}
	
	// Create topup log entry for the prorated upgrade charge
	_, err = b.db.CreateTopupLog(
		chatID,
		user.Username,
		paymentData.Amount, // Actual prorated amount from Stripe
		serviceName,
		paymentData.SubscriptionID,
		paymentData.InvoiceID, // Stripe invoice ID
	)
	if err != nil {
		logger.Error("Failed to create plan upgrade topup log", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		// Don't return here - upgrade was successful, just logging failed
	}

	// Send plan upgrade notification
	b.sendSubscriptionPlanUpgradeNotification(chatID, paymentData)

	logger.Info("Subscription plan upgraded with prorated charge", map[string]interface{}{
		"chat_id":         chatID,
		"subscription_id": paymentData.SubscriptionID,
		"new_tier":        paymentData.TierName,
		"prorated_amount": paymentData.Amount,
	})
}

// processSubscriptionCancelScheduled handles when user schedules cancellation at period end
func (b *Bot) processSubscriptionCancelScheduled(chatID int64, user *database.User, paymentData *stripe.PaymentData) {
	// Don't revoke access - subscription remains active until period end
	// Send cancellation scheduled notification
	b.sendSubscriptionCancelScheduledNotification(chatID, paymentData)

	logger.Info("Subscription cancellation scheduled by user", map[string]interface{}{
		"chat_id":         chatID,
		"subscription_id": paymentData.SubscriptionID,
	})
}

// processSubscriptionCancellation handles immediate subscription cancellation
func (b *Bot) processSubscriptionCancellation(chatID int64, user *database.User, paymentData *stripe.PaymentData) {
	// Get current premium user to validate subscription_id
	premiumUser, err := b.db.GetPremiumUser(chatID)
	if err != nil {
		logger.Error("Failed to get premium user for subscription validation", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return
	}

	// Validate that we have a subscription to cancel
	if premiumUser == nil || !premiumUser.IsPremiumUser() || !premiumUser.IsSubscription {
		logger.Warn("Immediate subscription cancellation ignored - no active subscription found", map[string]interface{}{
			"chat_id":                  chatID,
			"webhook_subscription_id":  paymentData.SubscriptionID,
			"user_has_premium":         premiumUser != nil && premiumUser.IsPremiumUser(),
			"user_has_subscription":    premiumUser != nil && premiumUser.IsSubscription,
		})
		return
	}

	// Validate that the subscription being cancelled matches the one in our database
	if paymentData.SubscriptionID != "" && premiumUser.SubscriptionID != "" {
		if premiumUser.SubscriptionID != paymentData.SubscriptionID {
			logger.Warn("Immediate subscription cancellation ignored - subscription_id mismatch", map[string]interface{}{
				"chat_id":                    chatID,
				"webhook_subscription_id":    paymentData.SubscriptionID,
				"database_subscription_id":   premiumUser.SubscriptionID,
			})
			// Still log the termination for the webhook subscription but don't reset premium user
			_, logErr := b.db.CreateSubscriptionChangeLog(chatID, paymentData.SubscriptionID, consts.SubscriptionOperationTerminate)
			if logErr != nil {
				logger.Warn("Failed to create subscription termination log", map[string]interface{}{
					"user_id":         chatID,
					"subscription_id": paymentData.SubscriptionID,
					"error":           logErr.Error(),
				})
			}
			return
		}
	}

	// Log subscription termination for the matching subscription
	subscriptionIDToLog := paymentData.SubscriptionID
	if subscriptionIDToLog == "" {
		subscriptionIDToLog = premiumUser.SubscriptionID
	}
	
	if subscriptionIDToLog != "" {
		_, err = b.db.CreateSubscriptionChangeLog(chatID, subscriptionIDToLog, consts.SubscriptionOperationTerminate)
		if err != nil {
			logger.Warn("Failed to create subscription termination log", map[string]interface{}{
				"user_id":         chatID,
				"subscription_id": subscriptionIDToLog,
				"error":           err.Error(),
			})
		}
	}

	// This is immediate cancellation - access should be revoked
	// Update the database to cancel the premium user
	if b.db != nil {
		err := b.db.CancelSubscriptionPremiumUser(chatID)
		if err != nil {
			logger.Error("Failed to cancel subscription premium user", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": chatID,
			})
			return
		}
	}
	
	// Send immediate cancellation notification
	b.sendSubscriptionImmediateCancellationNotification(chatID, paymentData)

	logger.Info("Subscription cancelled immediately", map[string]interface{}{
		"chat_id":         chatID,
		"webhook_subscription_id": paymentData.SubscriptionID,
		"database_subscription_id": premiumUser.SubscriptionID,
	})
}


// processSubscriptionScheduleCancelled handles when scheduled plan changes are cancelled
func (b *Bot) processSubscriptionScheduleCancelled(chatID int64, user *database.User, paymentData *stripe.PaymentData) {
	// Send schedule cancellation notification
	b.sendSubscriptionScheduleCancelledNotification(chatID, paymentData)

	logger.Info("Subscription schedule cancelled", map[string]interface{}{
		"chat_id":         chatID,
		"subscription_id": paymentData.SubscriptionID,
		"tier_name":       paymentData.TierName,
	})
}

// processSubscriptionReactivated handles subscription reactivation events
func (b *Bot) processSubscriptionReactivated(chatID int64, user *database.User, paymentData *stripe.PaymentData) {
	// Update premium user to ensure subscription continues
	subscriptionResult, err := b.db.CreateSubscriptionPremiumUser(
		chatID,
		user.Username,
		paymentData.PremiumLevel,
		paymentData.SubscriptionID,
		paymentData.CustomerID,
		paymentData.BillingPeriod,
	)
	if err != nil {
		logger.Error("Failed to update premium user for reactivation", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return
	}

	// Send notification if a subscription was replaced (shouldn't happen in reactivation, but safety check)
	if subscriptionResult.ReplacedSubscriptionID != "" {
		b.sendSubscriptionReplacedNotification(chatID, subscriptionResult.ReplacedSubscriptionID)
	}

	// Send reactivation notification
	b.sendSubscriptionReactivatedNotification(chatID, paymentData)

	logger.Info("Subscription reactivated", map[string]interface{}{
		"chat_id":         chatID,
		"subscription_id": paymentData.SubscriptionID,
	})
}

// processSubscriptionPlanOrReactivation handles complex subscription updates that could be plan changes or reactivations
func (b *Bot) processSubscriptionPlanOrReactivation(chatID int64, user *database.User, paymentData *stripe.PaymentData) {
	logger.Debug("Processing subscription plan or reactivation", map[string]interface{}{
		"chat_id":         chatID,
		"subscription_id": paymentData.SubscriptionID,
		"incoming_tier":   paymentData.TierName,
		"incoming_level":  paymentData.PremiumLevel,
		"billing_period":  paymentData.BillingPeriod,
	})

	// Get current premium user data BEFORE updating to detect downgrades
	var currentPremiumLevel int
	var currentBillingPeriod string
	var currentTierName string
	currentPremiumUser, err := b.db.GetPremiumUser(chatID)
	if err == nil && currentPremiumUser != nil && currentPremiumUser.IsPremiumUser() {
		currentPremiumLevel = currentPremiumUser.Level
		currentBillingPeriod = currentPremiumUser.BillingPeriod
		
		tierNames := []string{"Free", "☕ Coffee", "🍰 Cake", "🎁 Sponsor"}
		if currentPremiumLevel < len(tierNames) {
			currentTierName = tierNames[currentPremiumLevel]
		}
		
		logger.Debug("Found existing premium user", map[string]interface{}{
			"current_tier":    currentTierName,
			"current_level":   currentPremiumLevel,
			"current_billing": currentBillingPeriod,
			"subscription_id": currentPremiumUser.SubscriptionID,
		})
	} else {
		logger.Debug("No existing premium user found or user not premium", map[string]interface{}{
			"error":     err,
			"user_nil":  currentPremiumUser == nil,
			"is_premium": func() bool { 
				if currentPremiumUser != nil { 
					return currentPremiumUser.IsPremiumUser() 
				}
				return false 
			}(),
		})
	}

	// Update premium user with new details
	subscriptionResult, err := b.db.CreateSubscriptionPremiumUser(
		chatID,
		user.Username,
		paymentData.PremiumLevel,
		paymentData.SubscriptionID,
		paymentData.CustomerID,
		paymentData.BillingPeriod,
	)
	if err != nil {
		logger.Error("Failed to update premium user for plan/reactivation", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return
	}

	// Send notification if a subscription was replaced
	logger.Debug("Checking for subscription replacement notification", map[string]interface{}{
		"chat_id":                    chatID,
		"replaced_subscription_id":   subscriptionResult.ReplacedSubscriptionID,
		"should_send_notification":   subscriptionResult.ReplacedSubscriptionID != "",
	})
	if subscriptionResult.ReplacedSubscriptionID != "" {
		logger.Info("Sending subscription replacement notification", map[string]interface{}{
			"chat_id":                  chatID,
			"replaced_subscription_id": subscriptionResult.ReplacedSubscriptionID,
		})
		b.sendSubscriptionReplacedNotification(chatID, subscriptionResult.ReplacedSubscriptionID)
	} else {
		logger.Debug("No subscription replacement notification needed", map[string]interface{}{
			"chat_id": chatID,
		})
	}

	// Determine the type of change and send appropriate notification
	var notificationType string
	if currentPremiumLevel > 0 && currentPremiumLevel > paymentData.PremiumLevel {
		// This is a downgrade - send specialized downgrade notification
		notificationType = "DOWNGRADE"
		logger.Debug("Detected plan DOWNGRADE", map[string]interface{}{
			"from_tier":  currentTierName,
			"from_level": currentPremiumLevel,
			"to_tier":    paymentData.TierName,
			"to_level":   paymentData.PremiumLevel,
		})
		b.sendSubscriptionDowngradeNotification(chatID, paymentData, currentPremiumLevel, currentBillingPeriod)
	} else if currentPremiumLevel > 0 && currentPremiumLevel < paymentData.PremiumLevel {
		// This is an upgrade - send specialized upgrade notification
		notificationType = "UPGRADE"
		logger.Debug("Detected plan UPGRADE", map[string]interface{}{
			"from_tier":  currentTierName,
			"from_level": currentPremiumLevel,
			"to_tier":    paymentData.TierName,
			"to_level":   paymentData.PremiumLevel,
		})
		b.sendSubscriptionPlanChangeNotification(chatID, paymentData)
	} else {
		// Regular update or reactivation - send standard update notification
		notificationType = "UPDATE_OR_REACTIVATION"
		logger.Debug("Detected plan UPDATE or REACTIVATION", map[string]interface{}{
			"current_level": currentPremiumLevel,
			"new_level":     paymentData.PremiumLevel,
			"same_level":    currentPremiumLevel == paymentData.PremiumLevel,
		})
		b.sendSubscriptionUpdateNotification(chatID, paymentData)
	}

	logger.Info("Subscription updated (plan change or reactivation)", map[string]interface{}{
		"chat_id":           chatID,
		"subscription_id":   paymentData.SubscriptionID,
		"tier":              paymentData.TierName,
		"notification_type": notificationType,
		"current_level":     currentPremiumLevel,
		"new_level":         paymentData.PremiumLevel,
	})
}

// processSubscriptionUpdated handles general subscription update events
func (b *Bot) processSubscriptionUpdated(chatID int64, user *database.User, paymentData *stripe.PaymentData) {
	// Update premium user details in case anything changed
	subscriptionResult, err := b.db.CreateSubscriptionPremiumUser(
		chatID,
		user.Username,
		paymentData.PremiumLevel,
		paymentData.SubscriptionID,
		paymentData.CustomerID,
		paymentData.BillingPeriod,
	)
	if err != nil {
		logger.Error("Failed to update premium user for subscription update", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
		return
	}

	// Send notification if a subscription was replaced (shouldn't happen in update, but safety check)
	if subscriptionResult.ReplacedSubscriptionID != "" {
		b.sendSubscriptionReplacedNotification(chatID, subscriptionResult.ReplacedSubscriptionID)
	}

	logger.Info("Subscription updated", map[string]interface{}{
		"chat_id":         chatID,
		"subscription_id": paymentData.SubscriptionID,
		"tier":            paymentData.TierName,
	})
}

// processSubscriptionDowngradeScheduled handles scheduled subscription downgrades
func (b *Bot) processSubscriptionDowngradeScheduled(chatID int64, user *database.User, paymentData *stripe.PaymentData) {
	logger.Info("Processing scheduled subscription downgrade", map[string]interface{}{
		"chat_id":              chatID,
		"current_tier":         paymentData.TierName,
		"current_level":        paymentData.PremiumLevel,
		"future_tier":          paymentData.FutureTierName,
		"future_level":         paymentData.FuturePremiumLevel,
		"scheduled_change_date": paymentData.ScheduledChangeDate,
	})

	// Send the enhanced downgrade notification with future plan info
	b.sendScheduledDowngradeNotification(chatID, paymentData)

	logger.Info("Scheduled subscription downgrade processed", map[string]interface{}{
		"chat_id":         chatID,
		"subscription_id": paymentData.SubscriptionID,
		"current_tier":    paymentData.TierName,
		"future_tier":     paymentData.FutureTierName,
	})
}

// processSubscriptionUpgradeScheduled handles scheduled subscription upgrades
func (b *Bot) processSubscriptionUpgradeScheduled(chatID int64, user *database.User, paymentData *stripe.PaymentData) {
	logger.Info("Processing scheduled subscription upgrade", map[string]interface{}{
		"chat_id":              chatID,
		"current_tier":         paymentData.TierName,
		"current_level":        paymentData.PremiumLevel,
		"future_tier":          paymentData.FutureTierName,
		"future_level":         paymentData.FuturePremiumLevel,
		"scheduled_change_date": paymentData.ScheduledChangeDate,
	})

	// Send the enhanced upgrade notification with future plan info
	b.sendScheduledUpgradeNotification(chatID, paymentData)

	logger.Info("Scheduled subscription upgrade processed", map[string]interface{}{
		"chat_id":         chatID,
		"subscription_id": paymentData.SubscriptionID,
		"current_tier":    paymentData.TierName,
		"future_tier":     paymentData.FutureTierName,
	})
}

// processSubscriptionChangeScheduled handles scheduled subscription plan changes (generic)
func (b *Bot) processSubscriptionChangeScheduled(chatID int64, user *database.User, paymentData *stripe.PaymentData) {
	logger.Info("Processing scheduled subscription change", map[string]interface{}{
		"chat_id":              chatID,
		"current_tier":         paymentData.TierName,
		"current_level":        paymentData.PremiumLevel,
		"future_tier":          paymentData.FutureTierName,
		"future_level":         paymentData.FuturePremiumLevel,
		"scheduled_change_date": paymentData.ScheduledChangeDate,
	})

	// Send the enhanced change notification with future plan info
	b.sendScheduledChangeNotification(chatID, paymentData)

	logger.Info("Scheduled subscription change processed", map[string]interface{}{
		"chat_id":         chatID,
		"subscription_id": paymentData.SubscriptionID,
		"current_tier":    paymentData.TierName,
		"future_tier":     paymentData.FutureTierName,
	})
}

// processSubscriptionPaymentIssue handles subscription payment failures and issues
func (b *Bot) processSubscriptionPaymentIssue(chatID int64, user *database.User, paymentData *stripe.PaymentData) {
	// Don't revoke access immediately - give user time to resolve payment issues
	// Send payment issue notification
	b.sendSubscriptionPaymentIssueNotification(chatID, paymentData)

	logger.Warn("Subscription payment issue", map[string]interface{}{
		"chat_id":         chatID,
		"subscription_id": paymentData.SubscriptionID,
		"event_type":      paymentData.EventType,
	})
}