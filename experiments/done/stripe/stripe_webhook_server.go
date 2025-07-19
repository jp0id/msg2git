package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/webhook"
)

// WebhookServer handles Stripe webhooks for the msg2git application
type WebhookServer struct {
	stripeManager *StripeManager
	port          string
	server        *http.Server
}

// NewWebhookServer creates a new webhook server
func NewWebhookServer(port string) *WebhookServer {
	return &WebhookServer{
		stripeManager: NewStripeManager(),
		port:          port,
	}
}

// Start initializes and starts the webhook server
func (ws *WebhookServer) Start() error {
	// Initialize Stripe
	if err := ws.stripeManager.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize Stripe: %w", err)
	}

	// Setup routes
	mux := http.NewServeMux()
	mux.HandleFunc("/stripe/webhook", ws.handleStripeWebhook)
	mux.HandleFunc("/health", ws.handleHealth)
	mux.HandleFunc("/", ws.handleRoot)

	// Create server
	ws.server = &http.Server{
		Addr:         ":" + ws.port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	log.Printf("Stripe webhook server starting on port %s", ws.port)
	if err := ws.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Failed to start webhook server: %v", err)
	}

	// Wait for interrupt signal
	return nil
}

// waitForShutdown waits for interrupt signal and gracefully shuts down
func (ws *WebhookServer) waitForShutdown() error {
	// Create channel to receive OS signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Block until signal received
	<-quit
	log.Println("Shutting down webhook server...")

	// Create context with timeout for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown server
	if err := ws.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("webhook server forced to shutdown: %w", err)
	}

	log.Println("Webhook server stopped")
	return nil
}

// handleStripeWebhook processes Stripe webhook events
func (ws *WebhookServer) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	// Only accept POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read and verify webhook
	const MaxBodyBytes = int64(65536)
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading webhook body: %v", err)
		http.Error(w, "Error reading request body", http.StatusServiceUnavailable)
		return
	}

	// Verify webhook signature
	event, err := webhook.ConstructEvent(body, r.Header.Get("Stripe-Signature"), ws.stripeManager.webhookSecret)
	if err != nil {
		log.Printf("Webhook signature verification failed: %v", err)
		http.Error(w, "Webhook signature verification failed", http.StatusBadRequest)
		return
	}

	// Process the event
	if err := ws.processWebhookEvent(&event); err != nil {
		log.Printf("Error processing webhook event: %v", err)
		http.Error(w, "Error processing webhook", http.StatusInternalServerError)
		return
	}

	// Return success
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// processWebhookEvent processes different types of Stripe events
func (ws *WebhookServer) processWebhookEvent(event *stripe.Event) error {
	switch event.Type {
	case "checkout.session.completed":
		return ws.handleCheckoutSessionCompleted(event)
	case "payment_intent.succeeded":
		return ws.handlePaymentIntentSucceeded(event)
	case "payment_intent.payment_failed":
		return ws.handlePaymentIntentFailed(event)
	case "invoice.payment_succeeded":
		return ws.handleInvoicePaymentSucceeded(event)
	case "customer.subscription.created":
		return ws.handleSubscriptionCreated(event)
	case "customer.subscription.deleted":
		return ws.handleSubscriptionDeleted(event)
	default:
		log.Printf("Unhandled event type: %s", event.Type)
		return nil
	}
}

// handleCheckoutSessionCompleted handles successful checkout sessions
func (ws *WebhookServer) handleCheckoutSessionCompleted(event *stripe.Event) error {
	var session stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &session); err != nil {
		return fmt.Errorf("error parsing checkout session: %w", err)
	}

	log.Printf("Checkout session completed: %s", session.ID)

	// Extract metadata
	userIDStr, exists := session.Metadata["user_id"]
	if !exists {
		return fmt.Errorf("user_id not found in session metadata")
	}

	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid user_id: %w", err)
	}

	paymentType := session.Metadata["payment_type"]
	amountStr := session.Metadata["amount"]

	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		return fmt.Errorf("invalid amount: %w", err)
	}

	// Process based on payment type
	switch paymentType {
	case "reset_usage":
		return ws.processResetUsagePayment(userID, amount, session.ID)
	default:
		log.Printf("Unknown payment type: %s", paymentType)
		return nil
	}
}

// processResetUsagePayment processes reset usage payments
func (ws *WebhookServer) processResetUsagePayment(userID int64, amount float64, sessionID string) error {
	log.Printf("Processing reset usage payment - User: %d, Amount: $%.2f, Session: %s", userID, amount, sessionID)

	// TODO: Update user's usage in database
	// This would typically involve:
	// 1. Connect to your database
// 2. Reset user's daily usage count
	// 3. Update last reset timestamp
	// 4. Log the transaction

	// TODO: Send success notification to user via Telegram
	// This would require:
	// 1. Get user's chat ID from database
	// 2. Send success message via Telegram Bot API

	// For now, just log the successful payment
	log.Printf("✅ Usage reset successful for user %d", userID)
	return nil
}

// handlePaymentIntentSucceeded handles successful payment intents
func (ws *WebhookServer) handlePaymentIntentSucceeded(event *stripe.Event) error {
	var paymentIntent stripe.PaymentIntent
	if err := json.Unmarshal(event.Data.Raw, &paymentIntent); err != nil {
		return fmt.Errorf("error parsing payment intent: %w", err)
	}

	log.Printf("Payment intent succeeded: %s", paymentIntent.ID)
	return nil
}

// handlePaymentIntentFailed handles failed payment intents
func (ws *WebhookServer) handlePaymentIntentFailed(event *stripe.Event) error {
	var paymentIntent stripe.PaymentIntent
	if err := json.Unmarshal(event.Data.Raw, &paymentIntent); err != nil {
		return fmt.Errorf("error parsing payment intent: %w", err)
	}

	log.Printf("❌ Payment intent failed: %s", paymentIntent.ID)

	// TODO: Notify user of payment failure
	// TODO: Log failure for analytics

	return nil
}

// handleInvoicePaymentSucceeded handles successful invoice payments
func (ws *WebhookServer) handleInvoicePaymentSucceeded(event *stripe.Event) error {
	var invoice stripe.Invoice
	if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
		return fmt.Errorf("error parsing invoice: %w", err)
	}

	log.Printf("Invoice payment succeeded: %s", invoice.ID)
	return nil
}

// handleSubscriptionCreated handles new subscription creation
func (ws *WebhookServer) handleSubscriptionCreated(event *stripe.Event) error {
	var subscription stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
		return fmt.Errorf("error parsing subscription: %w", err)
	}

	log.Printf("Subscription created: %s", subscription.ID)
	return nil
}

// handleSubscriptionDeleted handles subscription cancellation
func (ws *WebhookServer) handleSubscriptionDeleted(event *stripe.Event) error {
	var subscription stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
		return fmt.Errorf("error parsing subscription: %w", err)
	}

	log.Printf("Subscription deleted: %s", subscription.ID)
	return nil
}

// handleHealth provides health check endpoint
func (ws *WebhookServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// handleRoot provides basic info about the webhook server
func (ws *WebhookServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"service": "msg2git-stripe-webhook",
		"status":  "running",
		"version": "1.0.0",
		"endpoints": map[string]string{
			"/stripe/webhook": "POST - Stripe webhook handler",
			"/health":         "GET - Health check",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
