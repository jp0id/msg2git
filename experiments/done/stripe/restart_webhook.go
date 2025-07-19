package main

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
)

func main() {
	fmt.Println("🔄 Restarting Webhook Server with API Version Fix")
	fmt.Println("==================================================")
	
	// Load environment variables
	if err := godotenv.Load(".env"); err != nil {
		log.Printf("Warning: error loading .env file: %v", err)
	}

	// Get port from environment or use default
	port := os.Getenv("WEBHOOK_PORT")
	if port == "" {
		port = "8080"
	}

	// Create and start webhook server
	server := NewWebhookServer(port)
	
	fmt.Printf("✅ API version mismatch fix applied\n")
	fmt.Printf("✅ Webhook server restarting on port %s\n", port)
	fmt.Printf("✅ Ready to receive Stripe events\n\n")
	
	fmt.Println("📋 Test again:")
	fmt.Println("1. Create new payment session: go run create_test_session.go stripe_integration.go")
	fmt.Println("2. Complete payment with test card: 4242 4242 4242 4242")
	fmt.Println("3. Watch for successful webhook processing logs")
	fmt.Println("\nPress Ctrl+C to stop the server")
	
	if err := server.Start(); err != nil {
		log.Fatalf("Webhook server error: %v", err)
	}
}