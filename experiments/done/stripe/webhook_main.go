package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

// Main function for running the webhook server
func main() {
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
	log.Printf("Starting webhook server on port %s", port)

	if err := server.Start(); err != nil {
		log.Fatalf("Webhook server error: %v", err)
	}
}

