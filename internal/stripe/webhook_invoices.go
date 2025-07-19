package stripe

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/msg2git/msg2git/internal/logger"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/customer"
)

// handleInvoicePaymentSucceeded handles successful invoice payments (subscription renewals)
func (sm *Manager) handleInvoicePaymentSucceeded(event *stripe.Event) (*PaymentData, error) {
	var invoice stripe.Invoice
	if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
		return nil, fmt.Errorf("error parsing invoice: %w", err)
	}

	logger.Info("Invoice payment succeeded", map[string]interface{}{
		"invoice_id":  invoice.ID,
		"customer_id": invoice.Customer.ID,
	})

	// Log detailed invoice structure for debugging
	logger.Debug("Parsed invoice structure", map[string]interface{}{
		"invoice_id":  invoice.ID,
		"customer_id": invoice.Customer.ID,
		"subscription_id": func() string {
			if invoice.Parent != nil && invoice.Parent.SubscriptionDetails != nil && invoice.Parent.SubscriptionDetails.Subscription != nil {
				return invoice.Parent.SubscriptionDetails.Subscription.ID
			} else {
				return "nil"
			}
		}(),
		"total":             invoice.Total,
		"amount_paid":       invoice.AmountPaid,
		"billing_reason":    invoice.BillingReason,
		"collection_method": invoice.CollectionMethod,
		"line_items_count":  len(invoice.Lines.Data),
		"status":            invoice.Status,
	})

	// Log each line item in detail
	for i, line := range invoice.Lines.Data {
		logger.Debug("Invoice line item details", map[string]interface{}{
			"invoice_id":       invoice.ID,
			"line_index":       i,
			"line_id":          line.ID,
			"line_amount":      line.Amount,
			"line_description": line.Description,
			"line_subscription_id": func() string {
				if line.Subscription != nil {
					return line.Subscription.ID
				} else {
					return "nil"
				}
			}(),
			"line_price_id": func() string {
				if line.Pricing != nil && line.Pricing.PriceDetails != nil {
					return line.Pricing.PriceDetails.Price
				} else {
					return "nil"
				}
			}(),
			"line_price_type": func() string {
				if line.Pricing != nil {
					return string(line.Pricing.Type)
				} else {
					return "nil"
				}
			}(),
			"line_price_recurring": func() string {
				if line.Pricing != nil {
					return string(line.Pricing.Type)
				} else {
					return "nil"
				}
			}(),
		})
	}

	// Check if this is a subscription-related invoice
	// For plan upgrades, Stripe sometimes doesn't populate invoice.Subscription properly
	// but we can identify subscription invoices by checking line items
	isSubscriptionInvoice := false
	var subscriptionID string

	logger.Debug("Analyzing invoice for subscription detection", map[string]interface{}{
		"invoice_id": invoice.ID,
		"invoice_subscription_id": func() string {
			if invoice.Parent != nil && invoice.Parent.SubscriptionDetails != nil && invoice.Parent.SubscriptionDetails.Subscription != nil {
				return invoice.Parent.SubscriptionDetails.Subscription.ID
			} else {
				return "nil"
			}
		}(),
		"line_items_count": len(invoice.Lines.Data),
	})

	if invoice.Parent != nil && invoice.Parent.SubscriptionDetails != nil && invoice.Parent.SubscriptionDetails.Subscription != nil {
		// Direct subscription reference
		isSubscriptionInvoice = true
		subscriptionID = invoice.Parent.SubscriptionDetails.Subscription.ID
		logger.Debug("Found direct subscription reference in invoice", map[string]interface{}{
			"invoice_id":      invoice.ID,
			"subscription_id": subscriptionID,
		})
	} else if len(invoice.Lines.Data) > 0 {
		// Check if any line item is subscription-related
		for i, line := range invoice.Lines.Data {
			logger.Debug("Analyzing invoice line item", map[string]interface{}{
				"invoice_id": invoice.ID,
				"line_index": i,
				"line_subscription_id": func() string {
					if line.Subscription != nil {
						return line.Subscription.ID
					} else {
						return "nil"
					}
				}(),
				"line_price_type": func() string {
					if line.Pricing != nil {
						return string(line.Pricing.Type)
					} else {
						return "nil"
					}
				}(),
				"line_price_id": func() string {
					if line.Pricing != nil && line.Pricing.PriceDetails != nil {
						return line.Pricing.PriceDetails.Price
					} else {
						return "nil"
					}
				}(),
			})

			if line.Subscription != nil && line.Subscription.ID != "" {
				isSubscriptionInvoice = true
				subscriptionID = line.Subscription.ID
				logger.Debug("Found subscription reference in line item", map[string]interface{}{
					"invoice_id":      invoice.ID,
					"subscription_id": subscriptionID,
					"line_index":      i,
				})
				break
			}
			// Also check if the line item has a subscription price (recurring price)
			if line.Pricing != nil && line.Pricing.Type == "recurring" {
				isSubscriptionInvoice = true
				// Try to get subscription ID from line item subscription field
				if line.Subscription != nil && line.Subscription.ID != "" {
					subscriptionID = line.Subscription.ID
				}
				logger.Debug("Found recurring price in line item", map[string]interface{}{
					"invoice_id":      invoice.ID,
					"subscription_id": subscriptionID,
					"line_index":      i,
					"price_id":        func() string {
						if line.Pricing.PriceDetails != nil {
							return line.Pricing.PriceDetails.Price
						}
						return "nil"
					}(),
				})
				// If still no subscription ID, we'll try to find it by looking for recent subscriptions for this customer
				break
			}
		}
	}

	// Check for subscription reference in invoice parent (newer Stripe invoice structure)
	if !isSubscriptionInvoice && invoice.Total > 0 {
		// For newer Stripe invoices, subscription info might be in the parent field
		// We can also detect by billing_reason = "subscription_cycle"
		if invoice.BillingReason == "subscription_cycle" {
			isSubscriptionInvoice = true
			logger.Debug("Detected subscription invoice by billing_reason", map[string]interface{}{
				"invoice_id":     invoice.ID,
				"billing_reason": invoice.BillingReason,
			})

			// Note: Will extract subscription ID from raw JSON below if needed
		}
	}

	// If we detected a subscription invoice but don't have the subscription ID, try to find it
	if isSubscriptionInvoice && subscriptionID == "" {
		logger.Debug("Subscription invoice detected but no subscription ID found, attempting to locate", map[string]interface{}{
			"invoice_id":  invoice.ID,
			"customer_id": invoice.Customer.ID,
		})

		// Try to extract subscription ID from the raw JSON using a helper function
		extractedID := sm.extractSubscriptionIDFromRawInvoice(event.Data.Raw)
		if extractedID != "" {
			subscriptionID = extractedID
			logger.Debug("Extracted subscription ID from parent structure", map[string]interface{}{
				"invoice_id":      invoice.ID,
				"subscription_id": subscriptionID,
			})
		} else {
			logger.Debug("Failed to extract subscription ID from parent structure", map[string]interface{}{
				"invoice_id": invoice.ID,
			})
		}

		// This is a subscription invoice
		isSubscriptionInvoice = true
	}

	logger.Debug("Invoice subscription detection result", map[string]interface{}{
		"invoice_id":              invoice.ID,
		"is_subscription_invoice": isSubscriptionInvoice,
		"subscription_id":         subscriptionID,
	})

	if !isSubscriptionInvoice {
		logger.Debug("Invoice is not for a subscription, skipping", map[string]interface{}{
			"invoice_id": invoice.ID,
		})
		return nil, nil
	}

	// Safety check: if we detected a subscription invoice but have no subscription ID, skip processing to avoid panic
	if subscriptionID == "" {
		logger.Warn("Subscription invoice detected but no subscription ID found, skipping to avoid errors", map[string]interface{}{
			"invoice_id":     invoice.ID,
			"customer_id":    invoice.Customer.ID,
			"billing_reason": invoice.BillingReason,
		})
		return nil, nil
	}

	logger.Info("Processing subscription invoice", map[string]interface{}{
		"invoice_id":      invoice.ID,
		"subscription_id": subscriptionID,
	})

	// Extract user ID from customer metadata with fallback
	userIDStr, exists := invoice.Customer.Metadata["telegram_user_id"]
	var userID int64
	var err error

	if exists {
		userID, err = strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid telegram_user_id: %w", err)
		}
	} else {
		// Fallback: fetch the full customer details or extract from email
		fullCustomer, err := customer.Get(invoice.Customer.ID, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch customer details for invoice %s: %w", invoice.ID, err)
		}

		userIDStr, exists = fullCustomer.Metadata["telegram_user_id"]
		if !exists {
			// Try to extract from customer email (user_123@telegram.local format)
			if strings.HasPrefix(fullCustomer.Email, "user_") && strings.HasSuffix(fullCustomer.Email, "@telegram.local") {
				emailUserID := strings.TrimPrefix(fullCustomer.Email, "user_")
				emailUserID = strings.TrimSuffix(emailUserID, "@telegram.local")
				userID, err = strconv.ParseInt(emailUserID, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("unable to determine telegram_user_id from customer %s for invoice %s", invoice.Customer.ID, invoice.ID)
				}
				logger.Debug("Extracted user ID from customer email for invoice", map[string]interface{}{
					"user_id":        userID,
					"customer_email": fullCustomer.Email,
					"invoice_id":     invoice.ID,
				})
			} else {
				return nil, fmt.Errorf("telegram_user_id not found in customer metadata and email doesn't match expected format for invoice %s", invoice.ID)
			}
		} else {
			userID, err = strconv.ParseInt(userIDStr, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid telegram_user_id: %w", err)
			}
		}
	}

	// Determine event type based on invoice characteristics
	eventType := "subscription_renewed"

	// Check billing reason to detect different invoice types
	switch invoice.BillingReason {
	case "subscription_create":
		eventType = "subscription_created"
		logger.Debug("Detected subscription creation invoice", map[string]interface{}{
			"invoice_id":     invoice.ID,
			"billing_reason": invoice.BillingReason,
		})
	case "subscription_update":
		eventType = "subscription_plan_upgrade"
		logger.Debug("Detected plan upgrade invoice", map[string]interface{}{
			"invoice_id":     invoice.ID,
			"billing_reason": invoice.BillingReason,
		})
	case "subscription_cycle":
		eventType = "subscription_renewed"
		logger.Debug("Detected subscription renewal invoice", map[string]interface{}{
			"invoice_id":     invoice.ID,
			"billing_reason": invoice.BillingReason,
		})
	default:
		// For unknown billing reasons, skip to avoid unexpected behavior
		logger.Debug("Unknown billing reason, skipping invoice processing", map[string]interface{}{
			"invoice_id":     invoice.ID,
			"billing_reason": invoice.BillingReason,
		})
		return nil, nil
	}

	paymentData := &PaymentData{
		UserID:         userID,
		PaymentType:    "subscription",
		EventType:      eventType,
		SubscriptionID: subscriptionID, // Use the subscription ID we found
		CustomerID:     invoice.Customer.ID,
		Amount:         float64(invoice.AmountPaid) / 100, // Convert from cents
		InvoiceID:      invoice.ID, // Stripe invoice ID
	}

	// Extract tier information from subscription line items
	if len(invoice.Lines.Data) > 0 {
		// For plan upgrades, find the line item with positive amount (new tier)
		// For regular renewals, use the first line item
		var targetLine *stripe.InvoiceLineItem
		var targetLineIndex int
		
		if eventType == "subscription_plan_upgrade" && len(invoice.Lines.Data) > 1 {
			// Find the line item with positive amount (the new tier charge)
			for i, line := range invoice.Lines.Data {
				if line.Amount > 0 {
					targetLine = line
					targetLineIndex = i
					logger.Debug("Using positive amount line item for plan upgrade", map[string]interface{}{
						"invoice_id":   invoice.ID,
						"line_index":   i,
						"line_amount":  line.Amount,
						"description":  line.Description,
					})
					break
				}
			}
		}
		
		if targetLine == nil {
			// Fallback to first line for regular renewals or if no positive line found
			targetLine = invoice.Lines.Data[0]
			targetLineIndex = 0
			logger.Debug("Using first line item (default behavior)", map[string]interface{}{
				"invoice_id":  invoice.ID,
				"line_index":  0,
				"line_amount": targetLine.Amount,
			})
		}

		// Check if price information is available in the new v82 structure
		if targetLine.Pricing != nil && targetLine.Pricing.PriceDetails != nil && targetLine.Pricing.PriceDetails.Price != "" {
			priceID := targetLine.Pricing.PriceDetails.Price
			paymentData.TierName, paymentData.PremiumLevel, paymentData.BillingPeriod = sm.getPriceTierInfo(priceID)
			logger.Debug("Extracted tier info from price ID", map[string]interface{}{
				"invoice_id":     invoice.ID,
				"line_index":     targetLineIndex,
				"price_id":       priceID,
				"tier_name":      paymentData.TierName,
				"premium_level":  paymentData.PremiumLevel,
				"billing_period": paymentData.BillingPeriod,
			})
		} else {
			logger.Debug("No price information available in line item, will use defaults", map[string]interface{}{
				"invoice_id":  invoice.ID,
				"line_index":  targetLineIndex,
			})
			// Set defaults - we can't determine tier without price info, but we know it's a subscription
			paymentData.TierName = "☕ Coffee"     // Default tier
			paymentData.PremiumLevel = 1          // Default level
			paymentData.BillingPeriod = "monthly" // Default period
		}

		// Extract renewal date from the line item period end
		if targetLine.Period != nil && targetLine.Period.End != 0 {
			paymentData.RenewalDate = targetLine.Period.End
			logger.Debug("Extracted renewal date from invoice line item", map[string]interface{}{
				"invoice_id":   invoice.ID,
				"line_index":   targetLineIndex,
				"renewal_date": paymentData.RenewalDate,
				"renewal_time": time.Unix(paymentData.RenewalDate, 0).Format("2006-01-02 15:04:05"),
			})
		} else {
			logger.Debug("No period information available in line item", map[string]interface{}{
				"invoice_id": invoice.ID,
			})
		}
	}

	return paymentData, nil
}

// extractSubscriptionIDFromRawInvoice extracts subscription ID from nested invoice structure
func (sm *Manager) extractSubscriptionIDFromRawInvoice(rawData []byte) string {
	var rawInvoice map[string]interface{}
	if err := json.Unmarshal(rawData, &rawInvoice); err != nil {
		logger.Debug("Failed to unmarshal raw invoice data", map[string]interface{}{
			"error": err.Error(),
		})
		return ""
	}

	// Navigate: lines.data[0].parent.subscription_item_details.subscription
	lines, ok := rawInvoice["lines"].(map[string]interface{})
	if !ok {
		logger.Debug("No 'lines' field found in raw invoice", map[string]interface{}{})
		return ""
	}

	data, ok := lines["data"].([]interface{})
	if !ok {
		logger.Debug("'lines.data' is not an array", map[string]interface{}{})
		return ""
	}
	if len(data) == 0 {
		logger.Debug("'lines.data' array is empty", map[string]interface{}{})
		return ""
	}

	firstLine, ok := data[0].(map[string]interface{})
	if !ok {
		logger.Debug("First line item is not an object", map[string]interface{}{})
		return ""
	}

	parent, ok := firstLine["parent"].(map[string]interface{})
	if !ok {
		logger.Debug("No 'parent' field in first line item", map[string]interface{}{})
		return ""
	}

	subDetails, ok := parent["subscription_item_details"].(map[string]interface{})
	if !ok {
		logger.Debug("No 'subscription_item_details' field in parent", map[string]interface{}{})
		return ""
	}

	subscriptionID, ok := subDetails["subscription"].(string)
	if !ok {
		logger.Debug("'subscription' field is not a string", map[string]interface{}{})
		return ""
	}
	if subscriptionID == "" {
		logger.Debug("'subscription' field is empty", map[string]interface{}{})
		return ""
	}

	logger.Debug("Successfully extracted subscription ID from raw data", map[string]interface{}{
		"subscription_id": subscriptionID,
	})
	return subscriptionID
}