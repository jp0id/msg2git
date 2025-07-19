package stripe

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"

	"github.com/msg2git/msg2git/internal/logger"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/webhook"
)

// PaymentData represents processed payment information
type PaymentData struct {
	UserID         int64   `json:"user_id"`
	Amount         float64 `json:"amount,omitempty"`
	SessionID      string  `json:"session_id,omitempty"`
	PaymentType    string  `json:"payment_type"`
	TierName       string  `json:"tier_name,omitempty"`
	PremiumLevel   int     `json:"premium_level,omitempty"`
	BillingPeriod  string  `json:"billing_period,omitempty"`  // monthly/annually
	SubscriptionID string  `json:"subscription_id,omitempty"` // For subscription events
	CustomerID     string  `json:"customer_id,omitempty"`     // Stripe customer ID
	EventType      string  `json:"event_type,omitempty"`      // subscription_created/deleted/etc
	InvoiceID      string  `json:"invoice_id,omitempty"`      // Stripe invoice ID

	// Future plan information (for scheduled changes)
	FutureTierName      string `json:"future_tier_name,omitempty"`
	FuturePremiumLevel  int    `json:"future_premium_level,omitempty"`
	FutureBillingPeriod string `json:"future_billing_period,omitempty"`
	ScheduledChangeDate int64  `json:"scheduled_change_date,omitempty"` // Unix timestamp

	// Renewal date information (for subscription payments)
	RenewalDate int64 `json:"renewal_date,omitempty"` // Unix timestamp for subscription renewal
	
	// Refund information
	ReceiptEmail string `json:"receipt_email,omitempty"` // Email from receipt for refunds
}

// VerifyWebhookSignature verifies Stripe webhook signature and returns the event
func (sm *Manager) VerifyWebhookSignature(body []byte, signature string) (*stripe.Event, error) {
	event, err := webhook.ConstructEventWithOptions(body, signature, sm.webhookSecret, webhook.ConstructEventOptions{
		IgnoreAPIVersionMismatch: true,
	})
	if err != nil {
		return nil, fmt.Errorf("webhook signature verification failed: %w", err)
	}
	return &event, nil
}

// ProcessWebhookEvent processes Stripe webhook events
func (sm *Manager) ProcessWebhookEvent(event *stripe.Event) (*PaymentData, error) {
	switch event.Type {
	case "checkout.session.completed":
		return sm.handleCheckoutSessionCompleted(event)
	case "customer.subscription.created":
		return sm.handleSubscriptionCreated(event)
	case "customer.subscription.updated":
		// Handle subscription.updated events, but be selective about what we process
		// to avoid conflicts with schedule events
		return sm.handleSubscriptionUpdatedSelectively(event)
	case "customer.subscription.deleted":
		return sm.handleSubscriptionDeleted(event)
	case "customer.subscription_schedule.updated", "subscription_schedule.updated":
		return sm.handleSubscriptionScheduleUpdated(event)
	case "invoice.payment_succeeded":
		return sm.handleInvoicePaymentSucceeded(event)
	case "invoice.payment_failed":
		logger.Warn("Invoice payment failed", map[string]interface{}{
			"event_id": event.ID,
		})
		return nil, nil // Handle failed payments if needed
	case "payment_intent.succeeded":
		logger.Debug("Payment intent succeeded", map[string]interface{}{
			"event_id": event.ID,
		})
		return nil, nil // No action needed for this event
	case "payment_intent.payment_failed":
		logger.Warn("Payment intent failed", map[string]interface{}{
			"event_id": event.ID,
		})
		return nil, nil // No action needed for this event
	case "refund.created":
		return sm.handleRefundCreated(event)
	case "charge.refunded":
		return sm.handleChargeRefunded(event)
	default:
		logger.Debug("Unhandled event type", map[string]interface{}{
			"event_type": event.Type,
		})
		return nil, nil
	}
}

// handleCheckoutSessionCompleted handles successful checkout sessions
func (sm *Manager) handleCheckoutSessionCompleted(event *stripe.Event) (*PaymentData, error) {
	var session stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &session); err != nil {
		return nil, fmt.Errorf("error parsing checkout session: %w", err)
	}

	logger.Info("Checkout session completed", map[string]interface{}{
		"session_id": session.ID,
	})

	// Extract metadata
	userIDStr, exists := session.Metadata["user_id"]
	if !exists {
		return nil, fmt.Errorf("user_id not found in session metadata")
	}

	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid user_id: %w", err)
	}

	paymentType := session.Metadata["payment_type"]
	amountStr := session.Metadata["amount"]

	// Create base payment data
	paymentData := &PaymentData{
		UserID:      userID,
		SessionID:   session.ID,
		PaymentType: paymentType,
		EventType:   "checkout_completed",
	}

	// For subscription payments, extract additional metadata
	if paymentType == "subscription" {
		tierName := session.Metadata["tier_name"]
		premiumLevelStr := session.Metadata["premium_level"]
		billingPeriod := session.Metadata["billing_period"]

		if premiumLevelStr != "" {
			premiumLevel, err := strconv.Atoi(premiumLevelStr)
			if err == nil {
				paymentData.PremiumLevel = premiumLevel
			}
		}

		paymentData.TierName = tierName
		paymentData.BillingPeriod = billingPeriod
		paymentData.CustomerID = session.Customer.ID

		// For subscriptions, we'll get the actual subscription ID from the subscription.created event
	} else if paymentType == "premium" || paymentType == "reset_usage" {
		// Handle one-time payments
		// Get amount from session data (works with both metadata and Price ID approach)
		if amountStr != "" {
			// Legacy: amount from metadata (for backwards compatibility)
			if amount, err := strconv.ParseFloat(amountStr, 64); err == nil {
				paymentData.Amount = amount
			}
		} else if session.AmountTotal > 0 {
			// New: amount from Stripe session (works with Price ID)
			// Convert from cents to dollars
			paymentData.Amount = float64(session.AmountTotal) / 100.0
		}

		if paymentType == "premium" {
			tierName := session.Metadata["tier_name"]
			premiumLevelStr := session.Metadata["premium_level"]

			if premiumLevelStr != "" {
				premiumLevel, err := strconv.Atoi(premiumLevelStr)
				if err == nil {
					paymentData.PremiumLevel = premiumLevel
				}
			}
			paymentData.TierName = tierName
		}
	}

	return paymentData, nil
}

// HandleWebhook is an HTTP handler for Stripe webhooks
func (sm *Manager) HandleWebhook(w http.ResponseWriter, r *http.Request) (*PaymentData, error) {
	// Only accept POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return nil, fmt.Errorf("invalid HTTP method: %s", r.Method)
	}

	// Read and verify webhook
	const MaxBodyBytes = int64(65536)
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		logger.Error("Error reading webhook body", map[string]interface{}{
			"error": err.Error(),
		})
		http.Error(w, "Error reading request body", http.StatusServiceUnavailable)
		return nil, err
	}

	// Debug log the raw webhook (only when needed for debugging)
	if os.Getenv("STRIPE_DEBUG") == "true" {
		logger.Debug("Raw Stripe webhook received", map[string]interface{}{
			"headers":      fmt.Sprintf("%+v", r.Header),
			"body_length":  len(body),
			"body_content": string(body),
		})
	}

	// Verify webhook signature
	signatureHeader := r.Header.Get("Stripe-Signature")
	if signatureHeader == "" {
		logger.Error("Webhook signature verification failed: missing Stripe-Signature header", map[string]interface{}{})
		http.Error(w, "Missing webhook signature", http.StatusBadRequest)
		return nil, fmt.Errorf("missing webhook signature header")
	}

	event, err := sm.VerifyWebhookSignature(body, signatureHeader)
	if err != nil {
		logger.Error("Webhook signature verification failed", map[string]interface{}{
			"error": err.Error(),
		})
		if os.Getenv("STRIPE_DEBUG") == "true" {
			logger.Debug("Webhook signature debug info", map[string]interface{}{
				"secret_length":    len(sm.webhookSecret),
				"signature_header": signatureHeader,
			})
		}
		http.Error(w, "Webhook signature verification failed", http.StatusBadRequest)
		return nil, err
	}

	// Log basic event info
	logger.Info("Stripe webhook received", map[string]interface{}{
		"event_type": event.Type,
		"event_id":   event.ID,
	})

	// Process the event
	paymentData, err := sm.ProcessWebhookEvent(event)
	if err != nil {
		logger.Error("Error processing webhook event", map[string]interface{}{
			"error": err.Error(),
		})
		http.Error(w, "Error processing webhook", http.StatusInternalServerError)
		return nil, err
	}

	// Log processed payment data
	if paymentData != nil {
		logger.Info("Webhook event processed", map[string]interface{}{
			"event_type": paymentData.EventType,
			"user_id":    paymentData.UserID,
		})
		if paymentData.FutureTierName != "" {
			logger.Info("Scheduled plan change detected", map[string]interface{}{
				"current_tier":        paymentData.TierName,
				"future_tier":         paymentData.FutureTierName,
				"effective_timestamp": paymentData.ScheduledChangeDate,
			})
		}

		// Debug detailed info only when enabled
		if os.Getenv("STRIPE_DEBUG") == "true" {
			logger.Debug("Full payment data details", map[string]interface{}{
				"user_id":         paymentData.UserID,
				"payment_type":    paymentData.PaymentType,
				"event_type":      paymentData.EventType,
				"tier_name":       paymentData.TierName,
				"premium_level":   paymentData.PremiumLevel,
				"billing_period":  paymentData.BillingPeriod,
				"subscription_id": paymentData.SubscriptionID,
				"amount":          paymentData.Amount,
			})
		}
	}

	// Return success
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))

	return paymentData, nil
}

// handleRefundCreated handles refund.created events
func (sm *Manager) handleRefundCreated(event *stripe.Event) (*PaymentData, error) {
	var refund stripe.Refund
	if err := json.Unmarshal(event.Data.Raw, &refund); err != nil {
		return nil, fmt.Errorf("error parsing refund: %w", err)
	}

	logger.Info("Refund created", map[string]interface{}{
		"refund_id":      refund.ID,
		"charge_id":      refund.Charge.ID,
		"amount":         float64(refund.Amount) / 100.0, // Convert cents to dollars
		"currency":       refund.Currency,
		"status":         refund.Status,
		"reason":         refund.Reason,
		"description":    refund.Description,
		"receipt_number": refund.ReceiptNumber,
	})

	// Return nil, nil as we only need to log this event
	return nil, nil
}

// handleChargeRefunded handles charge.refunded events
func (sm *Manager) handleChargeRefunded(event *stripe.Event) (*PaymentData, error) {
	var charge stripe.Charge
	if err := json.Unmarshal(event.Data.Raw, &charge); err != nil {
		return nil, fmt.Errorf("error parsing charge: %w", err)
	}

	logger.Info("Charge refunded", map[string]interface{}{
		"charge_id":       charge.ID,
		"amount_refunded": float64(charge.AmountRefunded) / 100.0, // Convert cents to dollars
		"currency":        charge.Currency,
		"description":     charge.Description,
		"receipt_email":   charge.ReceiptEmail,
		"receipt_number":  charge.ReceiptNumber,
	})

	// Create payment data for refund record using charge_id as transaction_id
	// and receipt_email as username (no user_id required)
	paymentData := &PaymentData{
		UserID:       0, // No user ID required for refunds
		Amount:       float64(charge.AmountRefunded) / 100.0, // Convert cents to dollars
		SessionID:    charge.ID, // Use charge_id as transaction_id
		PaymentType:  "refund",
		EventType:    "charge_refunded",
		ReceiptEmail: charge.ReceiptEmail, // Use receipt_email as username
		InvoiceID:    charge.ReceiptNumber, // Use receipt_number as invoice_id
	}

	return paymentData, nil
}