# Stripe Subscription Integration

## Overview

Successfully integrated Stripe subscriptions with monthly/annual billing options to replace the previous one-time payment system for the `/coffee` command.

## Key Features Implemented

### 1. Environment Variables Support
- `COFFEE_PRICE_MONTHLY` - Stripe price ID for Coffee monthly subscription
- `COFFEE_PRICE_ANNUALLY` - Stripe price ID for Coffee annual subscription  
- `CAKE_PRICE_MONTHLY` - Stripe price ID for Cake monthly subscription
- `CAKE_PRICE_ANNUALLY` - Stripe price ID for Cake annual subscription
- `SPONSOR_PRICE_MONTHLY` - Stripe price ID for Sponsor monthly subscription
- `SPONSOR_PRICE_ANNUALLY` - Stripe price ID for Sponsor annual subscription

### 2. Updated `/coffee` Command
- Now shows subscription options instead of one-time payments
- Monthly and Annual options for all tiers (Coffee, Cake, Sponsor)
- Clean interface with buttons for each subscription option

### 3. Subscription Management
- Added "⚙️ Manage Subscription" button for existing premium users
- Integrates with Stripe Customer Portal for billing management
- Handles both subscription and legacy one-time payment users
- Direct access to view billing history, update payment methods, cancel subscriptions

### 4. Database Schema Updates
- Added subscription fields to `premium_user` table:
  - `subscription_id` - Stripe subscription ID
  - `customer_id` - Stripe customer ID  
  - `billing_period` - monthly/annually
  - `is_subscription` - true for subscriptions, false for one-time

### 5. Stripe Integration Enhancements
- `CreateSubscriptionSession()` - Creates subscription checkout sessions
- `CreateCustomerPortalSession()` - Generates customer portal URLs
- `FindOrCreateCustomer()` - Manages Stripe customers
- Enhanced webhook handling for subscription events

### 6. Webhook Event Processing
- Handles `customer.subscription.created` events
- Handles `customer.subscription.deleted` events  
- Processes `checkout.session.completed` for subscriptions
- Automatic user notification for subscription changes

### 7. Database Methods
- `CreateSubscriptionPremiumUser()` - Creates subscription-based premium users
- `CancelSubscriptionPremiumUser()` - Handles subscription cancellations
- Updated `GetPremiumUser()` to include subscription fields
- Backward compatibility with existing one-time payment users

## User Experience Flow

### New Subscription Flow
1. User runs `/coffee` command
2. Sees subscription options (Monthly/Annual for each tier)
3. Selects desired subscription option
4. Redirected to Stripe checkout for subscription
5. Upon successful payment, receives notification and premium access activated
6. Can manage subscription via "Manage Subscription" button in `/coffee`

### Legacy User Support
- Existing one-time payment users retain their access
- "Manage Subscription" shows different message for non-subscription users
- Can upgrade to subscription model through `/coffee` command

## Technical Implementation

### Files Modified
- `internal/stripe/manager.go` - Enhanced Stripe integration
- `internal/database/database.go` - Added subscription database methods
- `internal/database/models.go` - Updated PremiumUser struct
- `internal/telegram/commands_premium.go` - Updated `/coffee` command UI
- `internal/telegram/callback_payments.go` - Added subscription callbacks
- `internal/telegram/callback_router.go` - Added subscription routing
- `internal/telegram/bot.go` - Added webhook processing for subscriptions

### Callback Data Format
- Subscription selections: `subscription_{tier}_{period}`
  - Example: `subscription_coffee_monthly`, `subscription_cake_annual`
- Manage subscription: `manage_subscription`

### Stripe Events Handled
- `checkout.session.completed` - For subscription checkouts
- `customer.subscription.created` - New subscription activation
- `customer.subscription.deleted` - Subscription cancellation
- Maintains existing events for reset usage and legacy payments

## Testing and Deployment

### Build Status
✅ Application builds successfully without errors

### Key Testing Points
1. Environment variables properly loaded
2. Subscription checkout sessions created correctly
3. Webhook events processed and database updated
4. Customer portal sessions generated
5. Backward compatibility with existing users
6. Error handling for missing Stripe configuration

### Deployment Checklist
- [ ] Set up 6 Stripe price IDs in environment variables
- [ ] Configure Stripe webhook endpoints for subscription events
- [ ] Test subscription flow in Stripe test mode
- [ ] Verify customer portal functionality
- [ ] Database migration for new subscription fields
- [ ] Monitor webhook event processing

## Benefits

1. **Recurring Revenue** - Predictable monthly/annual revenue stream
2. **Better UX** - Automatic renewal, no manual resets needed
3. **Flexible Billing** - Users choose monthly or annual based on preference
4. **Self-Service** - Users can manage their own subscriptions via Stripe portal
5. **Scalable** - Stripe handles billing, invoicing, and payment retry logic
6. **Backward Compatible** - Existing one-time payment users unaffected

## Next Steps

1. Configure Stripe products and price IDs in production
2. Set up webhook endpoints for subscription events
3. Test end-to-end subscription flow
4. Update documentation for users about subscription model
5. Monitor subscription metrics and user adoption