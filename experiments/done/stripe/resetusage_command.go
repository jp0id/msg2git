package main

import (
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// ResetUsageCommand handles the /resetusage command with Stripe integration
func (b *Bot) handleResetUsageCommand(message *tgbotapi.Message) error {
	userID := message.From.ID
	chatID := message.Chat.ID
	
	// Parse command arguments
	args := strings.Fields(message.Text)
	if len(args) < 3 || args[1] != "pay" {
		return b.sendResetUsageHelp(chatID)
	}
	
	// Parse amount (e.g., "2.5$" or "2.50")
	amountStr := strings.TrimSuffix(args[2], "$")
	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil || amount <= 0 {
		return b.sendResetUsageHelp(chatID)
	}
	
	// Validate amount range (e.g., $0.50 - $50.00)
	if amount < 0.50 || amount > 50.00 {
		msg := tgbotapi.NewMessage(chatID, "❌ <b>Invalid amount</b>\n\nAmount must be between $0.50 and $50.00")
		msg.ParseMode = "html"
		_, err := b.rateLimitedSend(chatID, msg)
		return err
	}
	
	// Check if user has Stripe integration available (premium feature check)
	// TODO: Add actual premium level check from database
	
	// Create Stripe checkout session
	stripeManager := NewStripeManager()
	if err := stripeManager.Initialize(); err != nil {
		msg := tgbotapi.NewMessage(chatID, "❌ <b>Payment system unavailable</b>\n\nPlease try again later.")
		msg.ParseMode = "html"
		_, err := b.rateLimitedSend(chatID, msg)
		return err
	}
	
	session, err := stripeManager.CreateResetUsageSession(userID, amount)
	if err != nil {
		msg := tgbotapi.NewMessage(chatID, "❌ <b>Failed to create payment session</b>\n\nPlease try again later.")
		msg.ParseMode = "html"
		_, err := b.rateLimitedSend(chatID, msg)
		return err
	}
	
	// Send payment link to user
	return b.sendPaymentLink(chatID, session.URL, amount)
}

// sendResetUsageHelp sends help message for resetusage command
func (b *Bot) sendResetUsageHelp(chatID int64) error {
	helpText := `💳 <b>Reset Usage Command</b>

<b>Usage:</b> <code>/resetusage pay &lt;amount&gt;</code>

<b>Examples:</b>
• <code>/resetusage pay 2.5</code> - Pay $2.50 to reset usage
• <code>/resetusage pay 5.00</code> - Pay $5.00 to reset usage

<b>Amount Range:</b> $0.50 - $50.00

<b>What it does:</b>
✅ Resets your daily usage limit
✅ Allows continued access to premium features
✅ Secure payment via Stripe
✅ Instant activation after payment`

	msg := tgbotapi.NewMessage(chatID, helpText)
	msg.ParseMode = "html"
	_, err := b.rateLimitedSend(chatID, msg)
	return err
}

// sendPaymentLink sends the Stripe payment link to user
func (b *Bot) sendPaymentLink(chatID int64, paymentURL string, amount float64) error {
	messageText := fmt.Sprintf(`💳 <b>Payment Required</b>

<b>Amount:</b> $%.2f
<b>Purpose:</b> Reset daily usage limit

Click the button below to complete your payment securely via Stripe.

⚡ <i>Your usage will be reset immediately after successful payment.</i>`, amount)

	// Create inline keyboard with payment button
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL(
				fmt.Sprintf("💳 Pay $%.2f", amount), 
				paymentURL,
			),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "cancel_payment"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ParseMode = "html"
	msg.ReplyMarkup = keyboard
	
	_, err := b.rateLimitedSend(chatID, msg)
	return err
}

// handleCancelPayment handles the cancel payment callback
func (b *Bot) handleCancelPayment(callback *tgbotapi.CallbackQuery) error {
	// Answer callback query
	callbackConfig := tgbotapi.NewCallback(callback.ID, "Payment cancelled")
	if _, err := b.rateLimitedRequest(callback.Message.Chat.ID, callbackConfig); err != nil {
		return err
	}
	
	// Delete the payment message
	deleteMsg := tgbotapi.NewDeleteMessage(callback.Message.Chat.ID, callback.Message.MessageID)
	_, err := b.rateLimitedRequest(callback.Message.Chat.ID, deleteMsg)
	
	if err == nil {
		// Send cancellation confirmation
		confirmMsg := tgbotapi.NewMessage(callback.Message.Chat.ID, "❌ Payment cancelled")
		_, err = b.rateLimitedSend(callback.Message.Chat.ID, confirmMsg)
	}
	
	return err
}

// handlePaymentSuccess handles successful payment notification
// This would be called from your Stripe webhook handler
func (b *Bot) handlePaymentSuccess(userID int64, amount float64, sessionID string) error {
	// TODO: Get user's chat ID from database using userID
	// For now, this is a placeholder implementation
	
	// Reset user usage in database
	// TODO: Implement actual database update
	
	// Send success message to user
	successText := fmt.Sprintf(`✅ <b>Payment Successful!</b>

<b>Amount Paid:</b> $%.2f
<b>Transaction ID:</b> <code>%s</code>

🚀 <b>Your usage limit has been reset!</b>

You can now continue using premium features. Thank you for your payment!`, amount, sessionID)

	// TODO: Send message to user's chat
	// This requires getting the user's chat ID from database
	// msg := tgbotapi.NewMessage(chatID, successText)
	// msg.ParseMode = "html"
	// _, err := b.rateLimitedSend(chatID, msg)
	
	return nil
}

// Integration with existing bot command router
func (b *Bot) setupResetUsageCommand() {
	// Add to your existing command handler in commands.go
	// This would typically be added to your switch statement in handleMessage
	
	/*
	case "/resetusage":
		return b.handleResetUsageCommand(message)
	*/
}

// Integration with callback router
func (b *Bot) setupPaymentCallbacks() {
	// Add to your existing callback router in callback_router.go
	// This would typically be added to your switch statement in handleCallbackQuery
	
	/*
	case "cancel_payment":
		return b.handleCancelPayment(callback)
	*/
}