# Stripe Callbacks and Database Effects Analysis

This document provides a comprehensive overview of all Stripe webhook events and their corresponding database effects in the msg2git system.

## 📋 Table of Contents

1. [Database Tables Overview](#database-tables-overview)
2. [Stripe Event Types](#stripe-event-types)
3. [One-Time Payment Events](#one-time-payment-events)
4. [Subscription Events](#subscription-events)
5. [Schedule Events](#schedule-events)
6. [Database Update Summary](#database-update-summary)
7. [Event Flow Diagrams](#event-flow-diagrams)

---

## 🗃️ Database Tables Overview

### `premium_user` Table
- **Purpose**: Stores premium subscription information
- **Key Fields**:
  - `uid` - Telegram user ID
  - `level` - Premium level (0=Free, 1=Coffee, 2=Cake, 3=Sponsor)
  - `expire_at` - Expiration timestamp (-1 for never expires)
  - `subscription_id` - Stripe subscription ID
  - `customer_id` - Stripe customer ID
  - `billing_period` - "monthly" or "annually"
  - `is_subscription` - TRUE for subscriptions, FALSE for one-time payments

### `user_topup_log` Table
- **Purpose**: Records all payment transactions
- **Key Fields**:
  - `uid` - Telegram user ID
  - `amount` - Payment amount in USD
  - `service` - Service type (COFFEE, CAKE, SPONSOR, RESET)
  - `transaction_id` - Stripe transaction/subscription ID

---

## 🎯 Stripe Event Types

### Event Processing Flow
```
Stripe Webhook → HandleWebhook() → ProcessWebhookEvent() → Specific Handler → Database Updates + Notifications
```

---

## 💰 One-Time Payment Events

### 1. `checkout.session.completed`

**When**: User completes one-time payment checkout (Coffee/Cake/Sponsor or Usage Reset)

**Database Effects**:

#### For Premium Payments:
- **`premium_user`** table:
  - `level` = Premium level (1/2/3)
  - `expire_at` = Current time + 1 year
  - `is_subscription` = FALSE
  - `subscription_id` = Empty
  - `customer_id` = Stripe customer ID

- **`user_topup_log`** table:
  - `amount` = Payment amount ($5/$15/$50)
  - `service` = COFFEE/CAKE/SPONSOR
  - `transaction_id` = Checkout session ID

#### For Usage Reset Payments:
- **`user_topup_log`** table:
  - `amount` = $2.99
  - `service` = RESET
  - `transaction_id` = Checkout session ID
- **`user_usage`** table:
  - All usage counters reset to 0
- **`user_insights`** table:
  - `reset_count` incremented

**Notification**: Premium activation or usage reset success message

---

## 🔄 Subscription Events

### 1. `customer.subscription.created`

**When**: New subscription is created

**Processing**: ⚠️ **SKIPPED** - All subscription creation logic moved to `invoice.payment_succeeded` with `billing_reason: "subscription_create"`

**Database Effects**: None (processing skipped)

**Notification**: None (handled by invoice webhook)

**Reason for Change**: Moving processing to invoice webhook ensures proper expiry date setting from actual Stripe renewal data and prevents duplicate subscription messages.

### 2. `customer.subscription.updated` (Selective Processing)

**When**: Subscription status or details change

**Processing Logic**:
- ✅ **Process if**: `status = "canceled"` (immediate cancellation)
- ✅ **Process if**: `status = "active"` + previous status was "canceled" (reactivation)
- ✅ **Process if**: Cancellation fields changed (schedule cancellation/reactivation)
- ❌ **Skip if**: Plan changes (handled by schedule events)

#### For Immediate Cancellation (`status = "canceled"`):
**Database Effects**:
- **`premium_user`** table:
  - Record marked as cancelled (premium access revoked)

**Notification**: "❌ Subscription Cancelled"

#### For Reactivation (status: canceled → active):
**Database Effects**:
- **`premium_user`** table:
  - Premium access restored
  - `subscription_id` updated
  - `level` updated to current tier

**Notification**: "🎉 Subscription Reactivated!"

#### For Cancellation Scheduling (`cancel_at_period_end = true`):
**Database Effects**:
- **No immediate database changes** (access continues until period end)

**Notification**: "⚠️ Subscription Cancellation Scheduled"

### 3. `customer.subscription.deleted`

**When**: Subscription is permanently deleted

**Database Effects**:
- **`premium_user`** table:
  - Premium access revoked

**Notification**: "😢 Subscription Cancelled"

### 4. `invoice.payment_succeeded`

**When**: Subscription payment succeeds (creation, renewal, or upgrade)

**Processing Logic by Billing Reason**:
- ✅ `billing_reason: "subscription_create"` → **New subscription creation**
- ✅ `billing_reason: "subscription_cycle"` → **Subscription renewal** 
- ✅ `billing_reason: "subscription_update"` → **Plan upgrade with prorated charge**

**Database Effects**:

#### For Subscription Creation (`billing_reason: "subscription_create"`):
- **`premium_user`** table:
  - `level` = Premium level based on price
  - `expire_at` = Actual renewal date from Stripe invoice
  - `is_subscription` = TRUE
  - `subscription_id` = Stripe subscription ID
  - `customer_id` = Stripe customer ID
  - `billing_period` = "monthly" or "annually"

- **`user_topup_log`** table:
  - `amount` = First payment amount
  - `service` = COFFEE/CAKE/SPONSOR
  - `transaction_id` = Subscription ID
  - `invoice_id` = Stripe invoice ID

#### For Subscription Renewal (`billing_reason: "subscription_cycle"`):
- **`premium_user`** table:
  - `expire_at` = Actual renewal date from Stripe invoice (preferred) or calculated date (fallback)

- **`user_topup_log`** table:
  - `amount` = Renewal payment amount
  - `service` = COFFEE/CAKE/SPONSOR
  - `transaction_id` = Subscription ID
  - `invoice_id` = Stripe invoice ID

#### For Plan Upgrade (`billing_reason: "subscription_update"`):
- **`premium_user`** table:
  - `level` = New premium level
  - `billing_period` = Updated billing period

- **`user_topup_log`** table:
  - `amount` = Prorated upgrade amount
  - `service` = New tier service
  - `transaction_id` = Subscription ID
  - `invoice_id` = Stripe invoice ID

**Enhanced Processing**: 
- ✅ **Primary Processing Path**: All subscription logic now handled via invoice webhooks
- ✅ **Accurate Expiry Dates**: Uses actual renewal dates from Stripe invoice data
- ✅ **Smart Line Item Selection**: For upgrades, selects positive amount line items (new tier charges) instead of refund items
- ✅ **Proper Event Classification**: Detects subscription creation vs renewal vs upgrade based on `billing_reason`
- **Race Condition Handling**: If premium_user record not found, creates subscription user first (handles invoice arriving before subscription.created)

**Notification**: 
- "🎉 Subscription Activated!" (for `billing_reason: "subscription_create"`)
- "🔄 Subscription Renewed" (for `billing_reason: "subscription_cycle"`)
- "🎉 Plan Upgraded!" (for `billing_reason: "subscription_update"`)

---

## 📅 Schedule Events

### 1. `subscription_schedule.updated`

**When**: Subscription schedule is created, modified, or cancelled

**Processing Logic**:
- ✅ **If `status = "released/canceled/cancelled"`**: Schedule was cancelled
- ✅ **If future phases exist**: Plan change scheduled

#### For Schedule Cancellation (`status = "released"`):
**Database Effects**:
- **No database changes** (current subscription continues unchanged)

**Notification**: "✅ Scheduled Plan Change Cancelled"

#### For Downgrade Scheduled (future level < current level):
**Database Effects**:
- **No immediate database changes** (change happens at scheduled date)

**Notification**: "🔄 Subscription Plan Downgrade Scheduled"

#### For Upgrade Scheduled (future level > current level):
**Database Effects**:
- **No immediate database changes** (change happens at scheduled date)

**Notification**: "🎉 Subscription Plan Upgrade Scheduled"

#### For Immediate Upgrade with Prorated Charge:
**Database Effects**:
- **`premium_user`** table:
  - `level` updated immediately
  - `billing_period` updated

- **`user_topup_log`** table:
  - `amount` = Prorated charge amount
  - `service` = New tier service
  - `transaction_id` = Subscription ID

**Notification**: "🎉 Plan Upgraded Successfully!" (with prorated charge info)

---

## 📊 Database Update Summary

### `premium_user` Table Updates

| Event Type               | Level     | Expire At   | Is Subscription   | Subscription ID   | Notes                  |
| ------------             | -------   | ----------- | ----------------- | ----------------- | -------                |
| One-time Payment         | Updated   | +1 year     | FALSE             | Empty             | Traditional premium    |
| Subscription Created     | Updated   | 30-day default | TRUE           | Set               | Initial creation       |
| Subscription Renewed     | No change | Stripe date/Extended | TRUE       | No change         | Payment processed      |
| Subscription Cancelled   | No change | No change   | TRUE              | No change         | Access revoked         |
| Subscription Reactivated | Updated   | -1          | TRUE              | Updated           | Access restored        |
| Plan Upgrade (Immediate) | Updated   | No change   | TRUE              | No change         | New tier active        |
| Plan Change (Scheduled)  | No change | No change   | TRUE              | No change         | Changes later          |

### `user_topup_log` Table Updates

| Event Type | Amount | Service | Transaction ID | When |
|------------|--------|---------|----------------|------|
| One-time Premium | $5/$15/$50 | COFFEE/CAKE/SPONSOR | Session ID | Checkout completed |
| Usage Reset | $2.99 | RESET | Session ID | Checkout completed |
| Subscription Created | First payment | COFFEE/CAKE/SPONSOR | Subscription ID | Subscription created |
| Subscription Renewed | Renewal amount | COFFEE/CAKE/SPONSOR | Subscription ID | Invoice paid |
| Plan Upgrade (Prorated) | Prorated amount | New tier service | Subscription ID | Upgrade processed |

### Special Cases

#### Usage Reset Payments
- **Additional Effects**:
  - `user_usage` table: All counters reset to 0
  - `user_insights` table: `reset_count` incremented
  - `user_reset_log` table: New reset record created

#### Subscription Schedule Cancellations
- **No Database Changes**: Current subscription continues unchanged
- **Only Notification**: User informed that scheduled change was cancelled

#### Race Condition Handling (Invoice Before Subscription)
- **Problem**: `invoice.payment_succeeded` webhook may arrive before `customer.subscription.created`
- **Solution**: Invoice handler checks if premium_user record exists
- **If Missing**: Creates subscription user record first using invoice data
- **Then**: Proceeds with normal expiry date setting
- **Notification**: Sends "Subscription Activated" for new subscriptions, "Subscription Renewed" for existing ones
- **Logging**: Warns about race condition and logs successful handling

---

## 🔄 Event Flow Diagrams

### One-Time Payment Flow
```
User clicks premium tier → Stripe checkout → checkout.session.completed → 
premium_user updated + user_topup_log created → Success notification sent
```

### Subscription Creation Flow
```
User selects subscription → Stripe checkout → invoice.payment_succeeded (billing_reason: "subscription_create") → 
premium_user updated + user_topup_log created → Activation notification sent
```

### Subscription Renewal Flow
```
Stripe automatic billing → invoice.payment_succeeded → 
user_topup_log created → Optional renewal notification sent
```

### Plan Upgrade Flow
```
User upgrades plan → subscription.updated + invoice.payment_succeeded → 
premium_user updated + user_topup_log created → Upgrade notification sent
```

### Schedule Cancellation Flow
```
User cancels scheduled change → subscription_schedule.updated (status=released) → 
No database changes → Cancellation notification sent
```

### Subscription Cancellation/Reactivation Flow
```
User cancels subscription → customer.subscription.updated (status=canceled) → 
premium_user access revoked → Cancellation notification sent

User reactivates → customer.subscription.updated (cancel fields cleared) → 
premium_user access restored → Reactivation notification sent
```

---

## 🎯 Key Design Principles

1. **Event Prioritization**: Schedule events take precedence over subscription events for plan changes
2. **Duplicate Prevention**: Caching mechanisms prevent duplicate processing
3. **Graceful Degradation**: Missing data doesn't break the flow
4. **Comprehensive Logging**: All events are logged for debugging
5. **User Communication**: Every significant change triggers appropriate notifications

---

## 🔧 Configuration Notes

- Set `STRIPE_DEBUG=true` for detailed webhook debugging
- Webhook signature verification ensures security
- All payments are logged for audit purposes
- Premium access is immediately activated/revoked based on subscription status

This document provides a complete reference for understanding how Stripe webhooks affect the database and user experience in the msg2git system.
