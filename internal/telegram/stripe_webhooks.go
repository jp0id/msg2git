package telegram

import (
	"net/http"
	"os"

	"github.com/msg2git/msg2git/internal/logger"
)

// StartWebhookServer starts an HTTP server for Stripe webhooks
func (b *Bot) StartWebhookServer() {
	if b.stripeManager == nil {
		logger.Info("Stripe not configured, webhook server not started", nil)
		return
	}

	port := os.Getenv("WEBHOOK_PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/stripe/webhook", b.handleStripeWebhook)
	http.HandleFunc("/health", b.handleHealth)
	http.HandleFunc("/github/oauth", b.HandleGitHubOAuthCallback)
	
	// Note: Auth pages are served by BASE_URL service (nginx), no handlers needed in container
	
	// Add a root handler to help debug 404s
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		logger.Info("HTTP request received", map[string]interface{}{
			"path":   r.URL.Path,
			"method": r.Method,
		})
		if r.URL.Path == "/" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Webhook server is running. Available endpoints:\n/stripe/webhook\n/health\n/github/oauth\n\nNote: Auth pages are served by BASE_URL service"))
		} else {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("Not Found"))
		}
	})

	go func() {
		logger.Info("Webhook server starting", map[string]interface{}{
			"port": port,
			"endpoints": []string{"/stripe/webhook", "/health", "/github/oauth"},
		})
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			logger.Error("Webhook server error", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}()
}

// handleStripeWebhook processes Stripe webhook events
func (b *Bot) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	logger.Info("Stripe webhook received", map[string]interface{}{
		"method": r.Method,
		"path":   r.URL.Path,
		"remote": r.RemoteAddr,
		"headers": map[string]interface{}{
			"content_type":      r.Header.Get("Content-Type"),
			"stripe_signature":  r.Header.Get("Stripe-Signature") != "",
			"user_agent":        r.Header.Get("User-Agent"),
		},
	})

	if b.stripeManager == nil {
		logger.Error("Stripe webhook received but Stripe not configured", nil)
		http.Error(w, "Stripe not configured", http.StatusServiceUnavailable)
		return
	}

	// Use Stripe manager to handle webhook and get payment data
	paymentData, err := b.stripeManager.HandleWebhook(w, r)
	if err != nil {
		logger.Error("Webhook processing failed", map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	// If no payment data returned, webhook was handled but no action needed
	if paymentData == nil {
		logger.Info("Webhook processed but no payment data returned", map[string]interface{}{
			"reason": "event was handled or ignored",
		})
		return
	}

	// Log detailed payment data for debugging
	logger.Info("Payment data received from webhook", map[string]interface{}{
		"payment_type":    paymentData.PaymentType,
		"event_type":      paymentData.EventType,
		"user_id":         paymentData.UserID,
		"subscription_id": paymentData.SubscriptionID,
		"tier_name":       paymentData.TierName,
		"premium_level":   paymentData.PremiumLevel,
		"billing_period":  paymentData.BillingPeriod,
		"amount":          paymentData.Amount,
	})

	// Log future plan information if present
	if paymentData.FutureTierName != "" {
		logger.Info("Scheduled plan change detected in webhook", map[string]interface{}{
			"current_tier":         paymentData.TierName,
			"current_level":        paymentData.PremiumLevel,
			"future_tier":          paymentData.FutureTierName,
			"future_level":         paymentData.FuturePremiumLevel,
			"scheduled_change_date": paymentData.ScheduledChangeDate,
			"event_type":           paymentData.EventType,
		})
	}

	// Process the payment based on type
	switch paymentData.PaymentType {
	case "reset_usage":
		b.processResetUsagePayment(paymentData)
	case "premium":
		b.processPremiumPayment(paymentData)
	case "subscription":
		b.processSubscriptionEvent(paymentData)
	case "refund":
		b.processRefundEvent(paymentData)
	default:
		logger.Warn("Unknown payment type received", map[string]interface{}{
			"payment_type": paymentData.PaymentType,
			"user_id":      paymentData.UserID,
		})
	}
}

// handleHealth handles health check requests
func (b *Bot) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}