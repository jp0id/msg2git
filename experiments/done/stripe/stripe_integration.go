package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/checkout/session"
	"github.com/stripe/stripe-go/v76/webhook"
)

// StripeManager handles Stripe payment integration
type StripeManager struct {
	publishableKey string
	secretKey      string
	webhookSecret  string
}

// NewStripeManager creates a new Stripe manager
func NewStripeManager() *StripeManager {
	return &StripeManager{
		publishableKey: os.Getenv("STRIPE_PUBLISHABLE_KEY"),
		secretKey:      os.Getenv("STRIPE_SECRET_KEY"),
		webhookSecret:  os.Getenv("STRIPE_WEBHOOK_SECRET"),
	}
}

// Initialize sets up Stripe configuration
func (sm *StripeManager) Initialize() error {
	if sm.secretKey == "" {
		return fmt.Errorf("STRIPE_SECRET_KEY not found in environment")
	}
	
	stripe.Key = sm.secretKey
	log.Printf("Stripe initialized with secret key: %s...", sm.secretKey[:10])
	return nil
}

// CreateResetUsageSession creates a Stripe checkout session for reset usage payment
func (sm *StripeManager) CreateResetUsageSession(userID int64, amount float64) (*stripe.CheckoutSession, error) {
	// Convert amount to cents (Stripe uses cents)
	amountCents := int64(amount * 100)
	
	params := &stripe.CheckoutSessionParams{
		PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency: stripe.String("usd"),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name:        stripe.String("Reset Usage Limit"),
						Description: stripe.String("Reset your daily usage limit to continue using premium features"),
					},
					UnitAmount: stripe.Int64(amountCents),
				},
				Quantity: stripe.Int64(1),
			},
		},
		Mode:       stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL: stripe.String("https://your-domain.com/success?session_id={CHECKOUT_SESSION_ID}"),
		CancelURL:  stripe.String("https://your-domain.com/cancel"),
		Metadata: map[string]string{
			"user_id":     strconv.FormatInt(userID, 10),
			"payment_type": "reset_usage",
			"amount":      fmt.Sprintf("%.2f", amount),
		},
	}

	return session.New(params)
}

// HandleWebhook processes Stripe webhook events
func (sm *StripeManager) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	const MaxBodyBytes = int64(65536)
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		http.Error(w, "Error reading request body", http.StatusServiceUnavailable)
		return
	}

	// Verify webhook signature
	event, err := webhook.ConstructEvent(body, r.Header.Get("Stripe-Signature"), sm.webhookSecret)
	if err != nil {
		log.Printf("Error verifying webhook signature: %v", err)
		http.Error(w, "Error verifying webhook signature", http.StatusBadRequest)
		return
	}

	// Handle the event
	switch event.Type {
	case "checkout.session.completed":
		var checkoutSession stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &checkoutSession); err != nil {
			log.Printf("Error parsing webhook JSON: %v", err)
			http.Error(w, "Error parsing webhook JSON", http.StatusBadRequest)
			return
		}
		
		if err := sm.handlePaymentSuccess(&checkoutSession); err != nil {
			log.Printf("Error handling payment success: %v", err)
			http.Error(w, "Error processing payment", http.StatusInternalServerError)
			return
		}
		
	case "payment_intent.payment_failed":
		var paymentIntent stripe.PaymentIntent
		if err := json.Unmarshal(event.Data.Raw, &paymentIntent); err != nil {
			log.Printf("Error parsing webhook JSON: %v", err)
			http.Error(w, "Error parsing webhook JSON", http.StatusBadRequest)
			return
		}
		
		log.Printf("Payment failed for PaymentIntent: %s", paymentIntent.ID)
		
	default:
		log.Printf("Unhandled event type: %s", event.Type)
	}

	w.WriteHeader(http.StatusOK)
}

// handlePaymentSuccess processes successful payment
func (sm *StripeManager) handlePaymentSuccess(session *stripe.CheckoutSession) error {
	userIDStr, exists := session.Metadata["user_id"]
	if !exists {
		return fmt.Errorf("user_id not found in session metadata")
	}
	
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid user_id: %w", err)
	}
	
	paymentType, exists := session.Metadata["payment_type"]
	if !exists {
		return fmt.Errorf("payment_type not found in session metadata")
	}
	
	amount := session.Metadata["amount"]
	
	log.Printf("Payment successful - UserID: %d, Type: %s, Amount: $%s", userID, paymentType, amount)
	
	// TODO: Integrate with your user database to reset usage
	// For now, just log the successful payment
	switch paymentType {
	case "reset_usage":
		return sm.resetUserUsage(userID)
	default:
		return fmt.Errorf("unknown payment type: %s", paymentType)
	}
}

// resetUserUsage resets the user's daily usage limit
func (sm *StripeManager) resetUserUsage(userID int64) error {
	// TODO: Implement actual database update
	// This would typically update the user's usage count in your database
	log.Printf("Resetting usage for user %d", userID)
	
	// Placeholder implementation
	// In real implementation, you would:
	// 1. Update user's daily usage count to 0
	// 2. Update user's last reset timestamp
	// 3. Possibly send confirmation message via Telegram
	
	return nil
}

// StartWebhookServer starts the webhook server
func (sm *StripeManager) StartWebhookServer(port string) {
	http.HandleFunc("/stripe/webhook", sm.HandleWebhook)
	
	log.Printf("Starting Stripe webhook server on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start webhook server: %v", err)
	}
}

