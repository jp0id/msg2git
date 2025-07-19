package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
)

// TestWebhook creates a test webhook payload and sends it to the server
func TestWebhook() {
	// Load environment
	if err := godotenv.Load(".env"); err != nil {
		fmt.Printf("Error loading .env: %v\n", err)
		return
	}

	webhookSecret := os.Getenv("STRIPE_WEBHOOK_SECRET")
	if webhookSecret == "" {
		fmt.Println("❌ STRIPE_WEBHOOK_SECRET not found")
		return
	}

	// Create test webhook payload (checkout.session.completed)
	payload := `{
  "id": "evt_test_webhook",
  "object": "event",
  "api_version": "2020-08-27",
  "created": ` + fmt.Sprintf("%d", time.Now().Unix()) + `,
  "data": {
    "object": {
      "id": "cs_test_12345",
      "object": "checkout.session",
      "amount_total": 250,
      "currency": "usd",
      "customer": null,
      "metadata": {
        "user_id": "12345",
        "payment_type": "reset_usage",
        "amount": "2.50"
      },
      "payment_status": "paid",
      "status": "complete"
    }
  },
  "livemode": false,
  "pending_webhooks": 1,
  "request": {
    "id": "req_test_12345",
    "idempotency_key": null
  },
  "type": "checkout.session.completed"
}`

	// Create Stripe signature
	timestamp := time.Now().Unix()
	signedPayload := fmt.Sprintf("%d.%s", timestamp, payload)
	
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write([]byte(signedPayload))
	signature := hex.EncodeToString(mac.Sum(nil))
	
	stripeSignature := fmt.Sprintf("t=%d,v1=%s", timestamp, signature)

	// Send webhook request
	fmt.Println("🧪 Testing Webhook with Simulated Stripe Event...")
	fmt.Printf("Payload: checkout.session.completed for user_id=12345, amount=$2.50\n")
	
	req, err := http.NewRequest("POST", "http://localhost:8080/stripe/webhook", bytes.NewBufferString(payload))
	if err != nil {
		fmt.Printf("❌ Error creating request: %v\n", err)
		return
	}
	
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", stripeSignature)
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("❌ Error sending webhook: %v\n", err)
		return
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == 200 {
		fmt.Println("✅ Webhook processed successfully!")
		fmt.Printf("   Status: %d %s\n", resp.StatusCode, resp.Status)
		fmt.Println("   Check server logs for processing details")
	} else {
		fmt.Printf("❌ Webhook failed with status: %d %s\n", resp.StatusCode, resp.Status)
	}
}

func main() {
	fmt.Println("🚀 Stripe Webhook Test")
	fmt.Println("======================")
	
	// Test health endpoint first
	fmt.Println("1. Testing server health...")
	resp, err := http.Get("http://localhost:8080/health")
	if err != nil {
		fmt.Printf("❌ Server not running: %v\n", err)
		fmt.Println("Please start the webhook server first:")
		fmt.Println("go run webhook_main.go stripe_integration.go stripe_webhook_server.go")
		return
	}
	resp.Body.Close()
	
	if resp.StatusCode != 200 {
		fmt.Printf("❌ Server health check failed: %d\n", resp.StatusCode)
		return
	}
	fmt.Println("✅ Server is running")
	
	// Test webhook
	fmt.Println("\n2. Testing webhook endpoint...")
	TestWebhook()
	
	fmt.Println("\n🎉 Test completed!")
	fmt.Println("Next steps:")
	fmt.Println("- Use ngrok to expose the webhook for Stripe")
	fmt.Println("- Configure webhook URL in Stripe dashboard")
	fmt.Println("- Test with real Stripe checkout sessions")
}