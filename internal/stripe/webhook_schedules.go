package stripe

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/msg2git/msg2git/internal/logger"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/customer"
)

// handleSubscriptionScheduleUpdated handles subscription schedule updates (plan changes, downgrades, upgrades)
func (sm *Manager) handleSubscriptionScheduleUpdated(event *stripe.Event) (*PaymentData, error) {
	var schedule stripe.SubscriptionSchedule
	if err := json.Unmarshal(event.Data.Raw, &schedule); err != nil {
		return nil, fmt.Errorf("error parsing subscription schedule: %w", err)
	}

	logger.Info("Subscription schedule updated", map[string]interface{}{
		"schedule_id": schedule.ID,
		"customer_id": schedule.Customer.ID,
		"status":      schedule.Status,
	})

	// Extract user ID from customer metadata with fallback
	userIDStr, exists := schedule.Customer.Metadata["telegram_user_id"]
	var userID int64
	var err error

	if exists {
		userID, err = strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid telegram_user_id: %w", err)
		}
	} else {
		// Fallback: fetch the full customer details or extract from email
		fullCustomer, err := customer.Get(schedule.Customer.ID, nil)
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
					return nil, fmt.Errorf("unable to determine telegram_user_id from customer %s", schedule.Customer.ID)
				}
				logger.Debug("Extracted user ID from customer email for schedule update", map[string]interface{}{
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

	// Find current and future phases
	var currentTierName, currentBillingPeriod string
	var currentPremiumLevel int
	var futureTierName, futureBillingPeriod string
	var futurePremiumLevel int
	var scheduledChangeDate int64

	currentTime := time.Now().Unix()

	if os.Getenv("STRIPE_DEBUG") == "true" {
		logger.Debug("Schedule processing started", map[string]interface{}{
			"phases_count":      len(schedule.Phases),
			"current_timestamp": currentTime,
		})
	}

	for i, phase := range schedule.Phases {
		endStatus := "ongoing"
		if phase.EndDate != 0 {
			endStatus = time.Unix(phase.EndDate, 0).Format("2006-01-02 15:04:05")
		}
		logger.Debug("Processing schedule phase", map[string]interface{}{
			"phase_index": i,
			"start_date":  phase.StartDate,
			"start_time":  time.Unix(phase.StartDate, 0).Format("2006-01-02 15:04:05"),
			"end_date":    phase.EndDate,
			"end_time":    endStatus,
			"items_count": len(phase.Items),
		})

		if len(phase.Items) > 0 && phase.Items[0].Price != nil {
			priceID := phase.Items[0].Price.ID
			tierName, premiumLevel, billingPeriod := sm.getPriceTierInfo(priceID)

			logger.Debug("Phase price analysis", map[string]interface{}{
				"phase_index":    i,
				"price_id":       priceID,
				"tier_name":      tierName,
				"premium_level":  premiumLevel,
				"billing_period": billingPeriod,
			})

			// Current phase (active now)
			if phase.StartDate <= currentTime && (phase.EndDate == 0 || phase.EndDate > currentTime) {
				currentTierName = tierName
				currentPremiumLevel = premiumLevel
				currentBillingPeriod = billingPeriod
				logger.Debug("Current phase identified", map[string]interface{}{
					"phase_index":   i,
					"tier_name":     currentTierName,
					"premium_level": currentPremiumLevel,
					"start_date":    phase.StartDate,
					"end_date":      phase.EndDate,
					"current_time":  currentTime,
				})
			}

			// Future phase (starts after current time)
			if phase.StartDate > currentTime {
				if futureTierName == "" {
					futureTierName = tierName
					futurePremiumLevel = premiumLevel
					futureBillingPeriod = billingPeriod
					scheduledChangeDate = phase.StartDate
					logger.Debug("Future phase identified", map[string]interface{}{
						"phase_index":   i,
						"tier_name":     futureTierName,
						"premium_level": futurePremiumLevel,
						"start_date":    scheduledChangeDate,
						"start_time":    time.Unix(scheduledChangeDate, 0).Format("2006-01-02 15:04:05"),
					})
				} else {
					logger.Debug("Additional future phase found - ignoring", map[string]interface{}{
						"phase_index":   i,
						"tier_name":     tierName,
						"premium_level": premiumLevel,
						"start_date":    phase.StartDate,
					})
				}
			}
		} else {
			logger.Debug("Phase has no items or price information", map[string]interface{}{
				"phase_index": i,
				"items_count": len(phase.Items),
				"has_price": func() bool {
					if len(phase.Items) > 0 {
						return phase.Items[0].Price != nil
					}
					return false
				}(),
			})
		}
	}

	// Check if schedule was cancelled or completed
	// "released" status means the schedule was cancelled and subscription continues without scheduled changes
	// "canceled" status means the schedule was cancelled entirely
	// "completed" status means the schedule finished its phases
	if schedule.Status == "canceled" || schedule.Status == "cancelled" || schedule.Status == "released" {
		
		// Simple approach: For manual testing, let's allow all "released" status notifications
		// This avoids additional API calls and lets you see the behavior
		// In production, you can adjust this based on patterns you observe
		if schedule.Status == "released" {
			logger.Info("Processing schedule cancellation - allowing all 'released' status for testing", map[string]interface{}{
				"schedule_id": schedule.ID,
				"customer_id": schedule.Customer.ID,
				"user_id":     userID,
				"note":        "Allowing notification to test user-initiated cancellations",
			})
		}
		var statusMeaning string
		switch string(schedule.Status) {
		case "released":
			statusMeaning = "Schedule cancelled - subscription continues without changes"
		case "canceled", "cancelled":
			statusMeaning = "Schedule cancelled entirely"
		default:
			statusMeaning = "Unknown cancellation type"
		}

		logger.Info("Subscription schedule cancelled", map[string]interface{}{
			"schedule_id":    schedule.ID,
			"customer_id":    schedule.Customer.ID,
			"user_id":        userID,
			"status":         string(schedule.Status),
			"status_meaning": statusMeaning,
			"current_tier":   currentTierName,
			"current_level":  currentPremiumLevel,
			"phases_count":   len(schedule.Phases),
		})

		// Debug: log all phases for troubleshooting
		if os.Getenv("STRIPE_DEBUG") == "true" {
			logger.Debug("Schedule cancellation - phases details", map[string]interface{}{
				"schedule_id":  schedule.ID,
				"phases_count": len(schedule.Phases),
				"cancelled_schedule_phases": func() []map[string]interface{} {
					phases := make([]map[string]interface{}, len(schedule.Phases))
					for i, phase := range schedule.Phases {
						var priceID string
						if len(phase.Items) > 0 && phase.Items[0].Price != nil {
							priceID = phase.Items[0].Price.ID
						}
						phases[i] = map[string]interface{}{
							"index":      i,
							"start_date": phase.StartDate,
							"end_date":   phase.EndDate,
							"price_id":   priceID,
						}
					}
					return phases
				}(),
			})
		}

		// Handle potential nil subscription (schedule cancellation case)
		var subscriptionID string
		if schedule.Subscription != nil {
			subscriptionID = schedule.Subscription.ID
			logger.Info("Schedule cancellation - subscription found", map[string]interface{}{
				"subscription_id": subscriptionID,
			})
		} else {
			logger.Info("Schedule cancellation - no subscription attached, will get current subscription info from user database", map[string]interface{}{
				"schedule_id": schedule.ID,
			})
		}

		// For schedule cancellations, if we have tier info from phases but no current tier,
		// use the phase information as it represents what was scheduled (now cancelled)
		var tierName string
		var premiumLevel int
		var billingPeriod string
		
		if currentTierName != "" {
			// Use current tier if available
			tierName = currentTierName
			premiumLevel = currentPremiumLevel
			billingPeriod = currentBillingPeriod
		} else if futureTierName != "" {
			// Use first phase tier info if no current tier (common for cancellations)
			tierName = futureTierName
			premiumLevel = futurePremiumLevel
			billingPeriod = futureBillingPeriod
			logger.Debug("Using phase tier info for schedule cancellation", map[string]interface{}{
				"tier_name":      tierName,
				"premium_level":  premiumLevel,
				"billing_period": billingPeriod,
			})
		}
		// If still no tier info, leave empty and telegram bot will fill from database

		paymentData := &PaymentData{
			UserID:         userID,
			PaymentType:    "subscription",
			EventType:      "subscription_schedule_cancelled",
			SubscriptionID: subscriptionID,
			CustomerID:     schedule.Customer.ID,
			TierName:       tierName,
			PremiumLevel:   premiumLevel,
			BillingPeriod:  billingPeriod,
		}

		logger.Info("Schedule cancellation payment data created", map[string]interface{}{
			"event_type":      paymentData.EventType,
			"tier_name":       paymentData.TierName,
			"premium_level":   paymentData.PremiumLevel,
			"subscription_id": paymentData.SubscriptionID,
		})

		return paymentData, nil
	}

	// For subscription schedules, if we have future phases but no current phase,
	// treat the first future phase as "current" and second as "future" for comparison
	if currentTierName == "" && futureTierName != "" && len(schedule.Phases) >= 2 {
		logger.Debug("Schedule has multiple future phases - treating first as current, second as future", map[string]interface{}{
			"phase_0_tier":    futureTierName,
			"phase_0_level":   futurePremiumLevel,
			"phases_count":    len(schedule.Phases),
			"schedule_status": schedule.Status,
		})
		
		// Shift the phases: Phase 0 becomes "current", Phase 1 becomes "future"
		currentTierName = futureTierName
		currentPremiumLevel = futurePremiumLevel
		currentBillingPeriod = futureBillingPeriod
		
		// Now get Phase 1 as the future phase
		if len(schedule.Phases) > 1 {
			phase1 := schedule.Phases[1]
			if len(phase1.Items) > 0 && phase1.Items[0].Price != nil {
				priceID := phase1.Items[0].Price.ID
				futureTierName, futurePremiumLevel, futureBillingPeriod = sm.getPriceTierInfo(priceID)
				scheduledChangeDate = phase1.StartDate
				
				logger.Debug("Updated future phase to Phase 1", map[string]interface{}{
					"future_tier":       futureTierName,
					"future_level":      futurePremiumLevel,
					"scheduled_date":    scheduledChangeDate,
					"scheduled_time":    time.Unix(scheduledChangeDate, 0).Format("2006-01-02 15:04:05"),
				})
			}
		}
	}
	
	// If we don't have relevant plan change info, this might not be a relevant schedule update
	if futureTierName == "" || currentTierName == "" {
		logger.Debug("No relevant plan change found in schedule", map[string]interface{}{
			"current_tier":    currentTierName,
			"current_level":   currentPremiumLevel,
			"future_tier":     futureTierName,
			"future_level":    futurePremiumLevel,
			"phases_count":    len(schedule.Phases),
			"schedule_status": schedule.Status,
		})
		return nil, nil
	}

	// Determine event type based on plan level comparison
	var eventType string
	if futurePremiumLevel < currentPremiumLevel {
		eventType = "subscription_downgrade_scheduled"
	} else if futurePremiumLevel > currentPremiumLevel {
		eventType = "subscription_upgrade_scheduled"
	} else {
		eventType = "subscription_change_scheduled"
	}

	logger.Info("Subscription schedule change detected", map[string]interface{}{
		"schedule_id":    schedule.ID,
		"customer_id":    schedule.Customer.ID,
		"user_id":        userID,
		"current_tier":   currentTierName,
		"current_level":  currentPremiumLevel,
		"future_tier":    futureTierName,
		"future_level":   futurePremiumLevel,
		"event_type":     eventType,
		"status":         schedule.Status,
		"phases_count":   len(schedule.Phases),
		"scheduled_date": scheduledChangeDate,
		"scheduled_time": time.Unix(scheduledChangeDate, 0).Format("2006-01-02 15:04:05"),
	})

	// Additional debug for schedule changes
	if os.Getenv("STRIPE_DEBUG") == "true" {
		logger.Debug("Schedule change analysis", map[string]interface{}{
			"comparison": map[string]interface{}{
				"current_higher_than_future": currentPremiumLevel > futurePremiumLevel,
				"future_higher_than_current": futurePremiumLevel > currentPremiumLevel,
				"levels_equal":               currentPremiumLevel == futurePremiumLevel,
			},
			"decision": map[string]interface{}{
				"is_downgrade": eventType == "subscription_downgrade_scheduled",
				"is_upgrade":   eventType == "subscription_upgrade_scheduled",
				"is_change":    eventType == "subscription_change_scheduled",
			},
		})
	}

	// Handle potential nil subscription (schedule cancellation case)
	var subscriptionID string
	if schedule.Subscription != nil {
		subscriptionID = schedule.Subscription.ID
	}

	paymentData := &PaymentData{
		UserID:         userID,
		PaymentType:    "subscription",
		EventType:      eventType,
		SubscriptionID: subscriptionID,
		CustomerID:     schedule.Customer.ID,
		TierName:       currentTierName,
		PremiumLevel:   currentPremiumLevel,
		BillingPeriod:  currentBillingPeriod,

		// Future plan information
		FutureTierName:      futureTierName,
		FuturePremiumLevel:  futurePremiumLevel,
		FutureBillingPeriod: futureBillingPeriod,
		ScheduledChangeDate: scheduledChangeDate,
	}

	return paymentData, nil
}