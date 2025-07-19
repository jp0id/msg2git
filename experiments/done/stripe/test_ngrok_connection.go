package main

import (
	"fmt"
	"net/http"
)

func main() {
	fmt.Println("🌐 Testing Ngrok Connection")
	fmt.Println("===========================")
	
	// Test local connection first
	fmt.Println("1. Testing local webhook server...")
	resp, err := http.Get("http://localhost:8080/health")
	if err != nil {
		fmt.Printf("❌ Local server not accessible: %v\n", err)
		return
	}
	resp.Body.Close()
	fmt.Printf("✅ Local server responding: %d\n", resp.StatusCode)
	
	// Instructions for ngrok setup
	fmt.Println("\n2. Next steps:")
	fmt.Println("   a) Run: ngrok http 8080")
	fmt.Println("   b) Copy the https://xxxxx.ngrok.io URL")
	fmt.Println("   c) Test ngrok URL: curl https://your-ngrok-url.ngrok.io/health")
	fmt.Println("   d) Configure in Stripe dashboard")
	
	fmt.Println("\n3. After ngrok setup, test with:")
	fmt.Printf("   curl https://your-ngrok-url.ngrok.io/health\n")
	fmt.Printf("   Should return: OK\n")
	
	fmt.Println("\n4. Stripe webhook configuration:")
	fmt.Println("   URL: https://your-ngrok-url.ngrok.io/stripe/webhook")
	fmt.Println("   Events: checkout.session.completed")
}