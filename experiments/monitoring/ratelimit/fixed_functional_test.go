//go:build experiments

package ratelimit

import (
	"context"
	"testing"
	"time"
)

// Fixed functional tests using ConsumeLimit instead of CheckLimit

func TestRateLimiter_FixedFunctionalCorrectness(t *testing.T) {
	// Use shared metrics collector to avoid registration conflicts
	metricsCollector := getTestMetricsCollector()
	
	config := Config{
		CommandLimit:    RateLimit{Requests: 5, Window: 500 * time.Millisecond}, // Shorter window for faster tests
		GitHubRESTLimit: RateLimit{Requests: 10, Window: 500 * time.Millisecond}, // Add GitHub REST limit for testing
		PremiumMultipliers: map[int]float64{
			0: 1.0, // Free: 5 requests/second
			1: 2.0, // Coffee: 10 requests/second
		},
	}
	
	limiter := NewMemoryRateLimiter(config, metricsCollector)
	defer limiter.Close()
	
	ctx := context.Background()
	userID := int64(12345)

	t.Run("Basic Rate Limiting - FIXED", func(t *testing.T) {
		// Should allow first 5 requests
		for i := 0; i < 5; i++ {
			err := limiter.ConsumeLimit(ctx, userID, LimitTypeCommand, 0)
			if err != nil {
				t.Fatalf("Request %d should be allowed but got error: %v", i+1, err)
			}
		}

		// 6th request should be denied
		err := limiter.ConsumeLimit(ctx, userID, LimitTypeCommand, 0)
		if err == nil {
			t.Fatal("6th request should be denied but was allowed")
		}
		t.Logf("6th request correctly denied: %v", err)
	})

	t.Run("Premium Tier Multipliers - FIXED", func(t *testing.T) {
		premiumUserID := int64(67890)
		
		// Coffee tier should allow 10 requests (2x multiplier)
		for i := 0; i < 10; i++ {
			err := limiter.ConsumeLimit(ctx, premiumUserID, LimitTypeCommand, 1)
			if err != nil {
				t.Fatalf("Premium request %d should be allowed but got error: %v", i+1, err)
			}
		}

		// 11th request should be denied
		err := limiter.ConsumeLimit(ctx, premiumUserID, LimitTypeCommand, 1)
		if err == nil {
			t.Fatal("11th premium request should be denied but was allowed")
		}
		t.Logf("11th premium request correctly denied: %v", err)
	})

	t.Run("Window Sliding - FIXED", func(t *testing.T) {
		slidingUserID := int64(11111)
		
		// Use up all requests
		for i := 0; i < 5; i++ {
			limiter.ConsumeLimit(ctx, slidingUserID, LimitTypeCommand, 0)
		}
		
		// Should be denied now
		err := limiter.ConsumeLimit(ctx, slidingUserID, LimitTypeCommand, 0)
		if err == nil {
			t.Fatal("Request should be denied due to rate limit")
		}
		
		// Wait for window to slide (need > 500ms for 500ms window)
		time.Sleep(600 * time.Millisecond)
		
		// Should be allowed again
		err = limiter.ConsumeLimit(ctx, slidingUserID, LimitTypeCommand, 0)
		if err != nil {
			t.Fatalf("Request should be allowed after window slide but got: %v", err)
		}
		t.Log("Request correctly allowed after window slide")
	})

	t.Run("Multiple Limit Types - FIXED", func(t *testing.T) {
		multiUserID := int64(22222)
		
		// Use up command limits
		for i := 0; i < 5; i++ {
			limiter.ConsumeLimit(ctx, multiUserID, LimitTypeCommand, 0)
		}
		
		// Command should be denied
		err := limiter.ConsumeLimit(ctx, multiUserID, LimitTypeCommand, 0)
		if err == nil {
			t.Fatal("Command should be denied")
		}
		
		// GitHub REST should still be allowed (different limit type)
		err = limiter.ConsumeLimit(ctx, multiUserID, LimitTypeGitHubREST, 0)
		if err != nil {
			t.Fatalf("GitHub REST should be allowed (different limit type) but got: %v", err)
		}
		t.Log("Different limit types work independently")
	})

	t.Run("Concurrent Access Safety - FIXED", func(t *testing.T) {
		concurrentUserID := int64(33333)
		
		// Test concurrent access doesn't break rate limiting
		done := make(chan error, 20)
		
		// Launch 20 concurrent requests
		for i := 0; i < 20; i++ {
			go func() {
				err := limiter.ConsumeLimit(ctx, concurrentUserID, LimitTypeCommand, 0)
				done <- err
			}()
		}
		
		// Wait for all goroutines and count successes
		allowedCount := 0
		deniedCount := 0
		for i := 0; i < 20; i++ {
			err := <-done
			if err == nil {
				allowedCount++
			} else {
				deniedCount++
			}
		}
		
		// Should allow approximately 5 requests (rate limit), allowing for race conditions
		if allowedCount < 4 || allowedCount > 6 {
			t.Fatalf("Expected 4-6 allowed requests (target: 5), got %d (denied: %d)", allowedCount, deniedCount)
		}
		t.Logf("Concurrent safety: %d allowed, %d denied", allowedCount, deniedCount)
	})
}

func TestRateLimiter_PremiumTierAccuracy(t *testing.T) {
	// Use shared metrics collector to avoid registration conflicts
	metricsCollector := getTestMetricsCollector()
	
	config := Config{
		CommandLimit: RateLimit{Requests: 10, Window: time.Second},
		PremiumMultipliers: map[int]float64{
			0: 1.0,  // Free: 10 requests/second
			1: 2.0,  // Coffee: 20 requests/second  
			2: 4.0,  // Cake: 40 requests/second
			3: 10.0, // Sponsor: 100 requests/second
		},
	}
	
	limiter := NewMemoryRateLimiter(config, metricsCollector)
	defer limiter.Close()
	
	ctx := context.Background()

	tiers := []struct {
		name     string
		level    int
		expected int
	}{
		{"Free", 0, 10},
		{"Coffee", 1, 20},
		{"Cake", 2, 40},
		{"Sponsor", 3, 100},
	}

	for _, tier := range tiers {
		t.Run(tier.name, func(t *testing.T) {
			userID := int64(40000 + tier.level)
			
			// Try to consume expected number of requests
			allowedCount := 0
			for i := 0; i < tier.expected+10; i++ { // Try 10 extra
				err := limiter.ConsumeLimit(ctx, userID, LimitTypeCommand, tier.level)
				if err == nil {
					allowedCount++
				} else {
					break // Stop at first denial
				}
			}
			
			if allowedCount != tier.expected {
				t.Fatalf("%s tier: expected %d allowed requests, got %d", 
					tier.name, tier.expected, allowedCount)
			}
			t.Logf("%s tier correctly allowed %d requests", tier.name, allowedCount)
		})
	}
}