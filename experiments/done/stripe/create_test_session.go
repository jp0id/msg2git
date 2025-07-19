package main

import (
	"fmt"
	"log"

	"github.com/joho/godotenv"
)

func main() {
	// Load environment
	if err := godotenv.Load(".env"); err != nil {
		log.Printf("Warning: error loading .env file: %v", err)
	}

	// Create Stripe manager
	sm := NewStripeManager()
	if err := sm.Initialize(); err != nil {
		log.Fatalf("Failed to initialize Stripe: %v", err)
	}

	fmt.Println("🚀 Creating Test Checkout Session")
	fmt.Println("==================================")

	// Create test session
	userID := int64(67890) // Different user ID for testing
	amount := 3.75

	session, err := sm.CreateResetUsageSession(userID, amount)
	if err != nil {
		log.Fatalf("Failed to create checkout session: %v", err)
	}

	fmt.Printf("✅ Test checkout session created!\n\n")
	fmt.Printf("📋 Session Details:\n")
	fmt.Printf("   Session ID: %s\n", session.ID)
	fmt.Printf("   User ID: %d\n", userID)
	fmt.Printf("   Amount: $%.2f\n", amount)
	fmt.Printf("   Payment Type: reset_usage\n\n")

	fmt.Printf("🔗 Payment URL:\n")
	fmt.Printf("   %s\n\n", session.URL)

	fmt.Printf("🧪 For Testing:\n")
	fmt.Printf("1. Open the payment URL in your browser\n")
	fmt.Printf("2. Use Stripe test card: 4242 4242 4242 4242\n")
	fmt.Printf("3. Any future expiry date and any 3-digit CVC\n")
	fmt.Printf("4. Complete the payment\n")
	fmt.Printf("5. Check webhook server logs for the event\n\n")

	fmt.Printf("💡 Expected webhook data:\n")
	fmt.Printf("   Event Type: checkout.session.completed\n")
	fmt.Printf("   User ID: %d\n", userID)
	fmt.Printf("   Amount: $%.2f\n", amount)
	fmt.Printf("   Payment Type: reset_usage\n")
}

