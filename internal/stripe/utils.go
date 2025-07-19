package stripe

import (
	"github.com/stripe/stripe-go/v82"
)

// getPriceTierInfo extracts tier information from price ID
func (sm *Manager) getPriceTierInfo(priceID string) (tierName string, premiumLevel int, billingPeriod string) {
	switch priceID {
	case sm.coffeePriceMonthly:
		return "☕ Coffee", 1, "monthly"
	case sm.coffeePriceAnnually:
		return "☕ Coffee", 1, "annually"
	case sm.cakePriceMonthly:
		return "🍰 Cake", 2, "monthly"
	case sm.cakePriceAnnually:
		return "🍰 Cake", 2, "annually"
	case sm.sponsorPriceMonthly:
		return "🎁 Sponsor", 3, "monthly"
	case sm.sponsorPriceAnnually:
		return "🎁 Sponsor", 3, "annually"
	default:
		return "Unknown", 0, "unknown"
	}
}

// getSubscriptionTierInfo extracts tier information from subscription
func (sm *Manager) getSubscriptionTierInfo(subscription *stripe.Subscription) (tierName string, premiumLevel int, billingPeriod string) {
	if len(subscription.Items.Data) == 0 || subscription.Items.Data[0].Price == nil {
		return "Unknown", 0, "unknown"
	}

	priceID := subscription.Items.Data[0].Price.ID
	return sm.getPriceTierInfo(priceID)
}