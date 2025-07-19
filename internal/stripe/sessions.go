package stripe

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/msg2git/msg2git/internal/logger"
	"github.com/stripe/stripe-go/v82"
	billingportalsession "github.com/stripe/stripe-go/v82/billingportal/session"
	"github.com/stripe/stripe-go/v82/checkout/session"
	"github.com/stripe/stripe-go/v82/customer"
)

// CreateResetUsageSession creates a Stripe checkout session for reset usage payment
func (sm *Manager) CreateResetUsageSession(userID int64) (*stripe.CheckoutSession, error) {
	if sm.resetPrice == "" {
		return nil, fmt.Errorf("RESET_PRICE not configured - please set the RESET_PRICE environment variable to a valid Stripe Price ID")
	}

	params := &stripe.CheckoutSessionParams{
		PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(sm.resetPrice),
				Quantity: stripe.Int64(1),
			},
		},
		Mode:       stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL: stripe.String(fmt.Sprintf("%s/payment-success?session_id={CHECKOUT_SESSION_ID}&type=reset", sm.baseURL)),
		CancelURL:  stripe.String(fmt.Sprintf("%s/payment-cancel", sm.baseURL)),
		Metadata: map[string]string{
			"user_id":      strconv.FormatInt(userID, 10),
			"payment_type": "reset_usage",
		},
	}

	return session.New(params)
}

// CreateSubscriptionSession creates a Stripe checkout session for subscription-based premium tiers
func (sm *Manager) CreateSubscriptionSession(userID int64, tierName string, premiumLevel int, isAnnual bool) (*stripe.CheckoutSession, error) {
	// Get the appropriate price ID based on tier and billing period
	var priceID string
	switch premiumLevel {
	case 1: // Coffee
		if isAnnual {
			priceID = sm.coffeePriceAnnually
		} else {
			priceID = sm.coffeePriceMonthly
		}
	case 2: // Cake
		if isAnnual {
			priceID = sm.cakePriceAnnually
		} else {
			priceID = sm.cakePriceMonthly
		}
	case 3: // Sponsor
		if isAnnual {
			priceID = sm.sponsorPriceAnnually
		} else {
			priceID = sm.sponsorPriceMonthly
		}
	default:
		return nil, fmt.Errorf("invalid premium level: %d", premiumLevel)
	}

	if priceID == "" {
		return nil, fmt.Errorf("price ID not configured for tier %s (%s)", tierName, func() string {
			if isAnnual {
				return "annual"
			} else {
				return "monthly"
			}
		}())
	}

	billingPeriod := "monthly"
	if isAnnual {
		billingPeriod = "annually"
	}

	// Create or find customer first to ensure proper metadata
	customer, err := sm.FindOrCreateCustomer(userID, fmt.Sprintf("user_%d@telegram.local", userID))
	if err != nil {
		return nil, fmt.Errorf("failed to create/find customer: %w", err)
	}

	params := &stripe.CheckoutSessionParams{
		PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceID),
				Quantity: stripe.Int64(1),
			},
		},
		Mode:       stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		SuccessURL: stripe.String(fmt.Sprintf("%s/payment-success?session_id={CHECKOUT_SESSION_ID}&type=subscription", sm.baseURL)),
		CancelURL:  stripe.String(fmt.Sprintf("%s/payment-cancel", sm.baseURL)),
		Customer:   stripe.String(customer.ID), // Use the customer with proper metadata
		Metadata: map[string]string{
			"user_id":        strconv.FormatInt(userID, 10),
			"payment_type":   "subscription",
			"tier_name":      tierName,
			"premium_level":  strconv.Itoa(premiumLevel),
			"billing_period": billingPeriod,
		},
	}

	return session.New(params)
}

// CreateCustomerPortalSession creates a Stripe billing portal session for subscription management
func (sm *Manager) CreateCustomerPortalSession(customerID string, returnURL string) (*stripe.BillingPortalSession, error) {
	if customerID == "" {
		return nil, fmt.Errorf("customer ID is required")
	}

	if returnURL == "" {
		returnURL = fmt.Sprintf("%s/account", sm.baseURL)
	}

	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(returnURL),
	}

	session, err := billingportalsession.New(params)
	if err != nil {
		// Check for common Customer Portal issues
		if strings.Contains(err.Error(), "billing portal") || strings.Contains(err.Error(), "not enabled") {
			return nil, fmt.Errorf("customer Portal not enabled in Stripe dashboard - please enable it in Stripe Dashboard > Settings > Billing > Customer Portal")
		}
		if strings.Contains(err.Error(), "No such customer") {
			return nil, fmt.Errorf("customer ID '%s' not found in Stripe", customerID)
		}
		return nil, fmt.Errorf("stripe customer portal error: %w", err)
	}

	return session, nil
}

// FindOrCreateCustomer finds existing customer or creates a new one
func (sm *Manager) FindOrCreateCustomer(userID int64, email string) (*stripe.Customer, error) {
	if email == "" {
		email = fmt.Sprintf("user_%d@telegram.local", userID)
	}

	// Try to find existing customer first
	searchParams := &stripe.CustomerSearchParams{}
	searchParams.Query = fmt.Sprintf("email:'%s'", email)

	searchResult := customer.Search(searchParams)
	for searchResult.Next() {
		existingCustomer := searchResult.Customer()
		if existingCustomer != nil {
			// Check if existing customer has telegram_user_id metadata
			if _, exists := existingCustomer.Metadata["telegram_user_id"]; !exists {
				// Update existing customer with telegram_user_id metadata
				logger.Info("Updating existing customer with telegram_user_id metadata", map[string]interface{}{
					"customer_id": existingCustomer.ID,
				})
				updateParams := &stripe.CustomerParams{
					Metadata: map[string]string{
						"telegram_user_id": strconv.FormatInt(userID, 10),
					},
				}
				updatedCustomer, err := customer.Update(existingCustomer.ID, updateParams)
				if err != nil {
					logger.Error("Failed to update customer metadata", map[string]interface{}{
						"error":       err.Error(),
						"customer_id": existingCustomer.ID,
					})
					return existingCustomer, nil // Return original customer if update fails
				}
				return updatedCustomer, nil
			}
			return existingCustomer, nil
		}
	}

	// Create new customer if not found
	customerParams := &stripe.CustomerParams{
		Email: stripe.String(email),
		Metadata: map[string]string{
			"telegram_user_id": strconv.FormatInt(userID, 10),
		},
	}

	return customer.New(customerParams)
}

// UpdateCustomerMetadata manually updates a customer's telegram_user_id metadata
// This is useful for fixing existing customers that were created before the metadata fix
func (sm *Manager) UpdateCustomerMetadata(customerID string, userID int64) error {
	updateParams := &stripe.CustomerParams{
		Metadata: map[string]string{
			"telegram_user_id": strconv.FormatInt(userID, 10),
		},
	}

	_, err := customer.Update(customerID, updateParams)
	if err != nil {
		return fmt.Errorf("failed to update customer %s metadata: %w", customerID, err)
	}

	logger.Info("Successfully updated customer with telegram_user_id", map[string]interface{}{
		"customer_id": customerID,
		"user_id":     userID,
	})
	return nil
}