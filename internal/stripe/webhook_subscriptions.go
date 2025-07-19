package stripe

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/msg2git/msg2git/internal/logger"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/customer"
)

// handleSubscriptionCreated handles subscription creation events
func (sm *Manager) handleSubscriptionCreated(event *stripe.Event) (*PaymentData, error) {
	var subscription stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
		return nil, fmt.Errorf("error parsing subscription: %w", err)
	}

	logger.Info("Subscription created - skipping processing (handled by invoice webhook)", map[string]interface{}{
		"subscription_id": subscription.ID,
		"customer_id":     subscription.Customer.ID,
		"status":          subscription.Status,
	})

	// Skip processing - all subscription creation logic is now handled by invoice.payment_succeeded
	// with billing_reason: "subscription_create" to ensure proper expiry date setting
	return nil, nil
}

// handleSubscriptionUpdatedSelectively handles specific subscription update events (cancellations, reactivations)
// while avoiding conflicts with schedule-based plan changes
func (sm *Manager) handleSubscriptionUpdatedSelectively(event *stripe.Event) (*PaymentData, error) {
	var subscription stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
		return nil, fmt.Errorf("error parsing subscription update: %w", err)
	}

	logger.Info("Subscription updated - checking if action needed", map[string]interface{}{
		"subscription_id": subscription.ID,
		"customer_id":     subscription.Customer.ID,
		"status":          subscription.Status,
	})

	// Only process specific status changes that should trigger notifications
	// Ignore plan changes which are better handled by schedule events
	switch subscription.Status {
	case "canceled":
		// Subscription was cancelled
		logger.Info("Processing subscription cancellation", map[string]interface{}{
			"subscription_id": subscription.ID,
			"status":          subscription.Status,
		})
		return sm.handleSubscriptionStatusChange(event, "subscription_canceled")
	case "active":
		// Check if this was a reactivation from a cancelled state
		// Debug log the previous attributes to understand what we're getting
		if os.Getenv("STRIPE_DEBUG") == "true" {
			logger.Debug("Checking for reactivation", map[string]interface{}{
				"subscription_id":     subscription.ID,
				"current_status":      subscription.Status,
				"has_previous_attrs":  event.Data.PreviousAttributes != nil,
				"previous_attributes": event.Data.PreviousAttributes,
			})
		}

		if event.Data.PreviousAttributes != nil {
			if prevStatus, exists := event.Data.PreviousAttributes["status"]; exists {
				logger.Info("Found previous status in subscription update", map[string]interface{}{
					"subscription_id": subscription.ID,
					"previous_status": prevStatus,
					"current_status":  subscription.Status,
				})

				if prevStatus == "canceled" {
					logger.Info("Processing subscription reactivation", map[string]interface{}{
						"subscription_id": subscription.ID,
						"previous_status": prevStatus,
						"current_status":  subscription.Status,
					})
					return sm.handleSubscriptionStatusChange(event, "subscription_reactivated")
				}
			} else {
				logger.Debug("No status field in previous attributes", map[string]interface{}{
					"subscription_id": subscription.ID,
					"previous_attrs_keys": func() []string {
						keys := make([]string, 0, len(event.Data.PreviousAttributes))
						for k := range event.Data.PreviousAttributes {
							keys = append(keys, k)
						}
						return keys
					}(),
				})
			}
		}

		// For active subscriptions without reactivation indicators, we need to be more careful
		// Let's check if this might be a cancellation reversal by looking at other indicators

		// Check if previous attributes show cancellation fields were changed (any of these scenarios)
		if event.Data.PreviousAttributes != nil {
			reactivationDetected := false
			var reactivationReason string

			// Scenario 1: cancel_at field was cleared
			if prevCancelAt, hasCancelAt := event.Data.PreviousAttributes["cancel_at"]; hasCancelAt {
				if prevCancelAt != nil && subscription.CancelAt == 0 {
					reactivationDetected = true
					reactivationReason = "cancel_at cleared"
				}
			}

			// Scenario 2: cancel_at_period_end was set to false
			if prevCancelAtEnd, hasCancelAtEnd := event.Data.PreviousAttributes["cancel_at_period_end"]; hasCancelAtEnd {
				if prevCancelAtEnd == true && !subscription.CancelAtPeriodEnd {
					reactivationDetected = true
					reactivationReason = "cancel_at_period_end disabled"
				}
			}

			if reactivationDetected {
				logger.Info("Processing subscription reactivation based on cancellation field changes", map[string]interface{}{
					"subscription_id":    subscription.ID,
					"reason":             reactivationReason,
					"prev_cancel_at":     event.Data.PreviousAttributes["cancel_at"],
					"prev_cancel_at_end": event.Data.PreviousAttributes["cancel_at_period_end"],
					"curr_cancel_at":     subscription.CancelAt,
					"curr_cancel_at_end": subscription.CancelAtPeriodEnd,
				})
				return sm.handleSubscriptionStatusChange(event, "subscription_reactivated")
			}
		}

		// Check if this is a cancellation scheduling (cancel_at_period_end set to true)
		if event.Data.PreviousAttributes != nil {
			if prevCancelAtEnd, hasCancelAtEnd := event.Data.PreviousAttributes["cancel_at_period_end"]; hasCancelAtEnd {
				if prevCancelAtEnd == false && subscription.CancelAtPeriodEnd == true {
					logger.Info("Processing subscription cancellation scheduling", map[string]interface{}{
						"subscription_id":      subscription.ID,
						"cancel_at_period_end": subscription.CancelAtPeriodEnd,
						"prev_cancel_at_end":   prevCancelAtEnd,
					})
					return sm.handleSubscriptionStatusChange(event, "subscription_cancel_scheduled")
				}
			}
		}

		// If not a reactivation or cancellation, skip to avoid conflicts with schedule events
		logger.Debug("Skipping active subscription update - likely handled by schedule events", map[string]interface{}{
			"subscription_id": subscription.ID,
		})
		return nil, nil
	default:
		// For other statuses, skip to avoid conflicts with schedule events
		logger.Debug("Skipping subscription update - not a cancellation or reactivation", map[string]interface{}{
			"subscription_id": subscription.ID,
			"status":          subscription.Status,
		})
		return nil, nil
	}
}

// handleSubscriptionStatusChange handles subscription cancellation and reactivation events
func (sm *Manager) handleSubscriptionStatusChange(event *stripe.Event, eventType string) (*PaymentData, error) {
	var subscription stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
		return nil, fmt.Errorf("error parsing subscription for status change: %w", err)
	}

	// Extract user ID from customer metadata with fallback
	userIDStr, exists := subscription.Customer.Metadata["telegram_user_id"]
	var userID int64
	var err error

	if exists {
		userID, err = strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid telegram_user_id: %w", err)
		}
	} else {
		// Fallback: fetch the full customer details or extract from email
		fullCustomer, err := customer.Get(subscription.Customer.ID, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch customer details: %w", err)
		}

		userIDStr, exists = fullCustomer.Metadata["telegram_user_id"]
		if !exists {
			// Try to extract from customer email (user_123@telegram.local format)
			if strings.HasPrefix(fullCustomer.Email, "user_") && strings.HasSuffix(fullCustomer.Email, "@telegram.local") {
				emailUserID := strings.TrimPrefix(fullCustomer.Email, "user_")
				emailUserID = strings.TrimSuffix(emailUserID, "@telegram.local")
				userID, err = strconv.ParseInt(emailUserID, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("unable to determine telegram_user_id from customer %s", subscription.Customer.ID)
				}
				logger.Debug("Extracted user ID from customer email for status change", map[string]interface{}{
					"user_id":        userID,
					"customer_email": fullCustomer.Email,
				})
			} else {
				return nil, fmt.Errorf("telegram_user_id not found in customer metadata and email doesn't match expected format")
			}
		} else {
			userID, err = strconv.ParseInt(userIDStr, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid telegram_user_id: %w", err)
			}
		}
	}

	// Get tier information from subscription
	tierName, premiumLevel, billingPeriod := sm.getSubscriptionTierInfo(&subscription)

	paymentData := &PaymentData{
		UserID:         userID,
		PaymentType:    "subscription",
		EventType:      eventType,
		SubscriptionID: subscription.ID,
		CustomerID:     subscription.Customer.ID,
		TierName:       tierName,
		PremiumLevel:   premiumLevel,
		BillingPeriod:  billingPeriod,
	}

	logger.Info("Subscription status change processed", map[string]interface{}{
		"event_type":      eventType,
		"subscription_id": subscription.ID,
		"user_id":         userID,
		"tier_name":       tierName,
		"premium_level":   premiumLevel,
	})

	return paymentData, nil
}

// handleSubscriptionDeleted handles subscription cancellation events
func (sm *Manager) handleSubscriptionDeleted(event *stripe.Event) (*PaymentData, error) {
	var subscription stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
		return nil, fmt.Errorf("error parsing subscription: %w", err)
	}

	logger.Info("Subscription deleted", map[string]interface{}{
		"subscription_id": subscription.ID,
		"customer_id":     subscription.Customer.ID,
	})

	// Extract user ID from customer metadata with fallback
	userIDStr, exists := subscription.Customer.Metadata["telegram_user_id"]
	var userID int64
	var err error

	if exists {
		userID, err = strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid telegram_user_id: %w", err)
		}
	} else {
		// Fallback: fetch the full customer details or extract from email
		fullCustomer, err := customer.Get(subscription.Customer.ID, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch customer details: %w", err)
		}

		userIDStr, exists = fullCustomer.Metadata["telegram_user_id"]
		if !exists {
			// Try to extract from customer email (user_123@telegram.local format)
			if strings.HasPrefix(fullCustomer.Email, "user_") && strings.HasSuffix(fullCustomer.Email, "@telegram.local") {
				emailUserID := strings.TrimPrefix(fullCustomer.Email, "user_")
				emailUserID = strings.TrimSuffix(emailUserID, "@telegram.local")
				userID, err = strconv.ParseInt(emailUserID, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("unable to determine telegram_user_id from customer %s", subscription.Customer.ID)
				}
				logger.Debug("Extracted user ID from customer email for subscription deletion", map[string]interface{}{
					"user_id":        userID,
					"customer_email": fullCustomer.Email,
				})
			} else {
				return nil, fmt.Errorf("telegram_user_id not found in customer metadata and email doesn't match expected format")
			}
		} else {
			userID, err = strconv.ParseInt(userIDStr, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid telegram_user_id: %w", err)
			}
		}
	}

	paymentData := &PaymentData{
		UserID:         userID,
		PaymentType:    "subscription",
		EventType:      "subscription_deleted",
		SubscriptionID: subscription.ID,
		CustomerID:     subscription.Customer.ID,
	}

	return paymentData, nil
}