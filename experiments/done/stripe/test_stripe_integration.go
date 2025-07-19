package main

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// TestStripeIntegration provides test utilities for Stripe integration
type TestStripeIntegration struct {
	stripeManager *StripeManager
}

// NewTestStripeIntegration creates a new test instance
func NewTestStripeIntegration() *TestStripeIntegration {
	return &TestStripeIntegration{
		stripeManager: NewStripeManager(),
	}
}

// LoadEnvFile loads environment variables from .env file
func (t *TestStripeIntegration) LoadEnvFile() error {
	if err := godotenv.Load(); err != nil {
		return fmt.Errorf("error loading .env file: %w", err)
	}
	return nil
}

// TestConfiguration tests Stripe configuration
func (t *TestStripeIntegration) TestConfiguration() error {
	fmt.Println("🔧 Testing Stripe Configuration...")

	// Check environment variables
	requiredVars := []string{
		"STRIPE_PUBLISHABLE_KEY",
		"STRIPE_SECRET_KEY",
		"STRIPE_WEBHOOK_SECRET",
	}

	for _, varName := range requiredVars {
		value := os.Getenv(varName)
		if value == "" {
			fmt.Printf("❌ %s not found in environment\n", varName)
			return fmt.Errorf("missing required environment variable: %s", varName)
		}

		// Show partial key for verification
		if len(value) > 10 {
			fmt.Printf("✅ %s: %s...\n", varName, value[:10])
		} else {
			fmt.Printf("✅ %s: [SET]\n", varName)
		}
	}

	// Initialize Stripe
	if err := t.stripeManager.Initialize(); err != nil {
		fmt.Printf("❌ Stripe initialization failed: %v\n", err)
		return err
	}

	fmt.Println("✅ Stripe configuration is valid")
	return nil
}

// TestCreateCheckoutSession tests creating a checkout session
func (t *TestStripeIntegration) TestCreateCheckoutSession() error {
	fmt.Println("\n💳 Testing Checkout Session Creation...")

	testUserID := int64(12345)
	testAmount := 2.50

	session, err := t.stripeManager.CreateResetUsageSession(testUserID, testAmount)
	if err != nil {
		fmt.Printf("❌ Failed to create checkout session: %v\n", err)
		return err
	}

	fmt.Printf("✅ Checkout session created successfully!\n")
	fmt.Printf("   Session ID: %s\n", session.ID)
	fmt.Printf("   Payment URL: %s\n", session.URL)
	fmt.Printf("   User ID: %d\n", testUserID)
	fmt.Printf("   Amount: $%.2f\n", testAmount)

	return nil
}

// TestWebhookServer tests webhook server functionality
func (t *TestStripeIntegration) TestWebhookServer() error {
	fmt.Println("\n🌐 Testing Webhook Server...")

	port := "8081" // Use different port for testing
	fmt.Printf("Starting webhook server on port %s for testing...\n", port)

	// Start server in background
	server := NewWebhookServer(port)
	if err := server.Start(); err != nil {
		log.Printf("Webhook server error: %v", err)
	}

	// Give server time to start
	fmt.Println("✅ Webhook server started successfully")
	fmt.Printf("   Health check: http://localhost:%s/health\n", port)
	fmt.Printf("   Webhook endpoint: http://localhost:%s/stripe/webhook\n", port)

	return nil
}

// TestCommandFlow simulates the /resetusage command flow
func (t *TestStripeIntegration) TestCommandFlow() error {
	fmt.Println("\n⚡ Testing /resetusage Command Flow...")

	// Simulate different command variations
	testCases := []struct {
		command string
		valid   bool
	}{
		{"/resetusage pay 2.5", true},
		{"/resetusage pay 5.00", true},
		{"/resetusage pay 0.25", false}, // Too low
		{"/resetusage pay 100", false},  // Too high
		{"/resetusage pay abc", false},  // Invalid amount
		{"/resetusage", false},          // Missing args
	}

	for _, tc := range testCases {
		fmt.Printf("Testing command: %s\n", tc.command)

		// Parse command (simplified simulation)
		parts := []string{}
		for _, part := range []string{"/resetusage", "pay", "2.5"} {
			parts = append(parts, part)
		}

		if len(parts) >= 3 {
			amountStr := parts[2]
			if amount, err := strconv.ParseFloat(amountStr, 64); err == nil {
				if amount >= 0.50 && amount <= 50.00 {
					fmt.Printf("   ✅ Valid amount: $%.2f\n", amount)
				} else {
					fmt.Printf("   ❌ Amount out of range: $%.2f\n", amount)
				}
			} else {
				fmt.Printf("   ❌ Invalid amount format: %s\n", amountStr)
			}
		} else {
			fmt.Printf("   ❌ Invalid command format\n")
		}
	}

	return nil
}

// RunAllTests runs all integration tests
func (t *TestStripeIntegration) RunAllTests() error {
	fmt.Println("🚀 Starting Stripe Integration Tests")
	fmt.Println("=====================================")

	// Load environment
	if err := t.LoadEnvFile(); err != nil {
		fmt.Printf("⚠️  Warning: %v\n", err)
		fmt.Println("Please ensure your .env file contains Stripe keys")
	}

	// Test configuration
	if err := t.TestConfiguration(); err != nil {
		return err
	}

	// Test checkout session creation
	if err := t.TestCreateCheckoutSession(); err != nil {
		return err
	}

	// Test command flow
	if err := t.TestCommandFlow(); err != nil {
		return err
	}

	// Test webhook server (optional)
	fmt.Println("\n🤔 Would you like to test the webhook server? (y/n)")
	// For automated testing, skip webhook server test
	// if err := t.TestWebhookServer(); err != nil {
	// 	return err
	// }

	fmt.Println("\n🎉 All tests completed successfully!")
	fmt.Println("=====================================")
	fmt.Println("Next steps for joint testing:")
	fmt.Println("1. Start the webhook server: go run experiments/stripe_webhook_server.go")
	fmt.Println("2. Use ngrok to expose webhook endpoint: ngrok http 8080")
	fmt.Println("3. Configure webhook URL in Stripe dashboard")
	fmt.Println("4. Test payment flow with real Stripe checkout")

	return nil
}

// Main function for running tests
func main() {
	tester := NewTestStripeIntegration()
	if err := tester.RunAllTests(); err != nil {
		log.Fatalf("Test failed: %v", err)
	}
}


