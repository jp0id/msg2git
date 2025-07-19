package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
)

// Main function for running the webhook server with graceful shutdown
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

	// Create webhook server
	server := NewWebhookServer(port)
	
	// Setup graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	
	// Start server in goroutine
	go func() {
		log.Printf("Starting webhook server on port %s", port)
		log.Printf("Endpoints available:")
		log.Printf("  - Health: http://localhost:%s/health", port)
		log.Printf("  - Info: http://localhost:%s/", port)
		log.Printf("  - Webhook: http://localhost:%s/stripe/webhook", port)
		log.Printf("Press Ctrl+C to stop the server")
		
		if err := server.Start(); err != nil {
			log.Printf("Webhook server error: %v", err)
		}
	}()
	
	// Wait for interrupt signal
	<-quit
	log.Println("Shutting down webhook server...")
}