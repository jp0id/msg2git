package stripe

import (
	"fmt"
	"os"

	"github.com/msg2git/msg2git/internal/logger"
	"github.com/stripe/stripe-go/v82"
)

// Manager handles Stripe payment integration
type Manager struct {
	publishableKey string
	secretKey      string
	webhookSecret  string
	baseURL        string

	// Subscription price IDs
	coffeePriceMonthly   string
	coffeePriceAnnually  string
	cakePriceMonthly     string
	cakePriceAnnually    string
	sponsorPriceMonthly  string
	sponsorPriceAnnually string
	
	// One-time payment price IDs
	resetPrice string
}

// NewManager creates a new Stripe manager
func NewManager(baseURL string) *Manager {
	return &Manager{
		publishableKey: os.Getenv("STRIPE_PUBLISHABLE_KEY"),
		secretKey:      os.Getenv("STRIPE_SECRET_KEY"),
		webhookSecret:  os.Getenv("STRIPE_WEBHOOK_SECRET"),
		baseURL:        baseURL,

		// Load subscription price IDs
		coffeePriceMonthly:   os.Getenv("COFFEE_PRICE_MONTHLY"),
		coffeePriceAnnually:  os.Getenv("COFFEE_PRICE_ANNUALLY"),
		cakePriceMonthly:     os.Getenv("CAKE_PRICE_MONTHLY"),
		cakePriceAnnually:    os.Getenv("CAKE_PRICE_ANNUALLY"),
		sponsorPriceMonthly:  os.Getenv("SPONSOR_PRICE_MONTHLY"),
		sponsorPriceAnnually: os.Getenv("SPONSOR_PRICE_ANNUALLY"),
		
		// Load one-time payment price IDs
		resetPrice: os.Getenv("RESET_PRICE"),
	}
}

// Initialize sets up Stripe configuration
func (sm *Manager) Initialize() error {
	if sm.secretKey == "" {
		return fmt.Errorf("STRIPE_SECRET_KEY not found in environment")
	}

	stripe.Key = sm.secretKey
	logger.InfoMsg("Stripe initialized successfully")
	return nil
}