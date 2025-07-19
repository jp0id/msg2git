# Stripe Integration Experiment

## Overview

This experiment implements Stripe payment integration for the msg2git Telegram bot, specifically for the `/resetusage pay <amount>` command that allows users to reset their daily usage limits via payment.

## Files Structure

```
experiments/
├── stripe_integration.go      # Core Stripe manager and payment logic
├── resetusage_command.go      # Telegram command handler for /resetusage
├── stripe_webhook_server.go   # Standalone webhook server for Stripe events
├── test_stripe_integration.go # Test utilities and integration tests
├── go.mod                     # Go module dependencies
└── README.md                  # This file

done/                          # Archived experiment files
├── comment_link_improvement.md
├── test_issue_commands.md
├── issue_duplicate_content_fix.md
├── commit_graph.go
└── private_repo_graph.go
```

## Features Implemented

### 1. `/resetusage pay <amount>` Command
- Parse payment amount from user input (e.g., `/resetusage pay 2.5`)
- Validate amount range ($0.50 - $50.00)
- Create Stripe checkout session
- Send secure payment link to user
- Handle payment cancellation

### 2. Stripe Integration
- Checkout session creation for one-time payments
- Webhook event processing for payment confirmation
- Secure webhook signature verification
- Support for multiple payment types (extensible)

### 3. Webhook Server
- Standalone HTTP server for Stripe webhooks
- Handles multiple event types:
  - `checkout.session.completed`
  - `payment_intent.succeeded`
  - `payment_intent.payment_failed`
  - `invoice.payment_succeeded` (for future subscriptions)
  - `customer.subscription.created/deleted` (for future subscriptions)
- Graceful shutdown with signal handling
- Health check endpoint

### 4. Testing Framework
- Configuration validation
- Checkout session creation testing
- Command flow simulation
- Integration test suite

## Environment Variables Required

Add these to your `.env` file:

```bash
# Stripe Configuration
STRIPE_PUBLISHABLE_KEY=pk_test_...
STRIPE_SECRET_KEY=sk_test_...
STRIPE_WEBHOOK_SECRET=whsec_...

# Webhook Server (optional)
WEBHOOK_PORT=8080
```

## Usage Flow

### User Experience:
1. User types: `/resetusage pay 2.5`
2. Bot validates amount and creates Stripe session
3. Bot sends message with secure payment button
4. User clicks button → redirected to Stripe checkout
5. User completes payment securely on Stripe
6. Stripe sends webhook to your server
7. Server processes payment and resets user usage
8. User receives success confirmation

### Technical Flow:
```
Telegram Bot → Stripe API → Checkout Session → User Payment → Webhook → Database Update
```

## Testing Instructions

### 1. Environment Setup
```bash
cd experiments
go mod tidy
```

### 2. Test Configuration
```bash
go run test_stripe_integration.go
```

### 3. Test Webhook Server
```bash
# Terminal 1: Start webhook server
go run stripe_webhook_server.go

# Terminal 2: Test health endpoint
curl http://localhost:8080/health
```

### 4. Integration with Main Bot
To integrate with the main msg2git application:

1. Copy Stripe manager to `internal/stripe/`
2. Add command handler to `internal/telegram/commands.go`
3. Add callback handler to `internal/telegram/callback_router.go`
4. Add webhook endpoint to main server or run separately

## Joint Testing Setup

### 1. Environment Setup
```bash
cd experiments
cp .env.example .env
# Edit .env with your actual Stripe keys from dashboard
go mod tidy
```

### 2. Basic Configuration Test
```bash
# Test Stripe configuration and create test checkout session
go run test_stripe_integration.go stripe_integration.go stripe_webhook_server.go
```

### 3. Webhook Server Testing
```bash
# Terminal 1: Start webhook server with logging
go run webhook_test_server.go stripe_integration.go stripe_webhook_server.go

# Terminal 2: Test webhook endpoints
curl http://localhost:8080/health  # Should return "OK"
curl http://localhost:8080/        # Should return JSON service info

# Test webhook security (should return 400 - signature verification working)
go run simple_webhook_check.go
```

### 4. Create Test Payment Session
```bash
# Generate a real Stripe checkout session for testing
go run create_test_session.go stripe_integration.go

# This will output:
# - Session ID and payment URL
# - Test card info (4242 4242 4242 4242)
# - Instructions for completing test payment
```

### 5. Ngrok Setup (for real webhook testing)
```bash
# Terminal 1: Start webhook server
go run webhook_test_server.go stripe_integration.go stripe_webhook_server.go

# Terminal 2: Expose webhook with ngrok
ngrok http 8080
# Copy the ngrok URL (e.g., https://abc123.ngrok.io)
```

### 6. Stripe Dashboard Configuration
1. Go to [Stripe Dashboard → Developers → Webhooks](https://dashboard.stripe.com/test/webhooks)
2. Click "Add endpoint"
3. Add endpoint URL: `https://your-ngrok-url.ngrok.io/stripe/webhook`
4. Select events to send:
   - `checkout.session.completed` ✅ (required)
   - `payment_intent.succeeded` (optional)
   - `payment_intent.payment_failed` (optional)
5. Copy webhook signing secret and update `.env` file

### 7. End-to-End Payment Test
```bash
# 1. Ensure webhook server is running and ngrok is active
# 2. Create test checkout session
go run create_test_session.go stripe_integration.go

# 3. Open the generated payment URL in browser
# 4. Use Stripe test card: 4242 4242 4242 4242
#    - Any future expiry date (e.g., 12/25)
#    - Any 3-digit CVC (e.g., 123)
#    - Any name and email
# 5. Complete payment
# 6. Check webhook server logs for successful processing

# Expected webhook server output:
# "Checkout session completed: cs_test_..."
# "Processing reset usage payment - User: 67890, Amount: $3.75"
# "✅ Usage reset successful for user 67890"
```

### 8. Webhook Verification
Monitor your webhook server terminal for output like:
```
2025/07/02 06:51:25 Stripe webhook server starting on port 8080
2025/07/02 06:52:10 Checkout session completed: cs_test_a1JkOay...
2025/07/02 06:52:10 Processing reset usage payment - User: 67890, Amount: $3.75, Session: cs_test_a1JkOay...
2025/07/02 06:52:10 ✅ Usage reset successful for user 67890
```

### 9. Troubleshooting
```bash
# Check if webhook server is running
curl http://localhost:8080/health

# View recent webhook server logs
tail -f webhook.log

# Test webhook without signature (should fail with 400)
go run simple_webhook_check.go

# Kill any stuck processes on port 8080
lsof -ti:8080 | xargs kill -9
```

## Security Features

- ✅ Webhook signature verification
- ✅ HTTPS-only payment URLs
- ✅ Amount validation and limits
- ✅ Secure token handling
- ✅ No sensitive data in logs
- ✅ Request body size limits
- ✅ Graceful error handling

## Error Handling

- Invalid amounts → User-friendly error message
- Stripe API errors → Fallback error handling
- Webhook verification failures → Logged and rejected
- Network timeouts → Retry mechanisms (in production)
- Database failures → Transaction rollback (when integrated)

## Future Enhancements

- [ ] Subscription-based payments for premium features
- [ ] Multiple currency support
- [ ] Payment history tracking
- [ ] Automatic refund handling
- [ ] Usage analytics and reporting
- [ ] Promotional codes and discounts

## Integration Notes

When integrating with the main bot:

1. **Database Integration**: Update user usage limits in your PostgreSQL database
2. **Chat ID Resolution**: Map Stripe user_id to Telegram chat_id for notifications
3. **Premium Level Checks**: Validate user's premium level before allowing payments
4. **Rate Limiting**: Apply rate limits to prevent payment spam
5. **Logging**: Integrate with your existing logger for payment events

## Production Considerations

- Use production Stripe keys
- Set up proper webhook endpoint with SSL
- Implement database transactions for usage updates
- Add monitoring and alerting for payment failures
- Set up backup webhook endpoints
- Implement proper error recovery mechanisms
