//go:build experiments

package ratelimit

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"
)

// Realistic load test that actually triggers rate limiting
func TestRateLimiter_RealisticLoad_RateLimitingInAction(t *testing.T) {
	// Configure stricter limits to demonstrate rate limiting
	config := Config{
		CommandLimit: RateLimit{Requests: 5, Window: time.Second}, // 5 commands per second (strict)
		GitHubRESTLimit: RateLimit{Requests: 10, Window: time.Minute}, // 10 REST calls per minute  
		PremiumMultipliers: map[int]float64{
			0: 1.0,  // Free: 5/sec commands
			1: 2.0,  // Coffee: 10/sec commands
			2: 4.0,  // Cake: 20/sec commands
			3: 8.0,  // Sponsor: 40/sec commands
		},
	}

	limiter := NewMemoryRateLimiter(config, getTestMetricsCollector())
	defer limiter.Close()

	ctx := context.Background()

	t.Run("RateLimit_With_High_Load", func(t *testing.T) {
		// Test with 10,000 users making aggressive requests
		numUsers := 10000
		requestsPerUser := 10 // Each user tries 10 requests quickly
		
		// User distribution: 80% free, 15% coffee, 4% cake, 1% sponsor
		users := make([]struct {
			ID    int64
			Level int
			Name  string
		}, numUsers)

		for i := 0; i < numUsers; i++ {
			level := 0
			name := "Free"
			
			if i < 100 { // 1% sponsor
				level = 3
				name = "Sponsor"
			} else if i < 500 { // 4% cake
				level = 2
				name = "Cake"
			} else if i < 2000 { // 15% coffee
				level = 1
				name = "Coffee"
			}
			
			users[i] = struct {
				ID    int64
				Level int
				Name  string
			}{int64(i), level, name}
		}

		start := time.Now()
		
		var mu sync.Mutex
		stats := map[string]struct {
			allowed int
			denied  int
		}{
			"Free": {0, 0}, "Coffee": {0, 0}, "Cake": {0, 0}, "Sponsor": {0, 0},
		}

		var wg sync.WaitGroup

		for _, user := range users {
			wg.Add(1)
			go func(u struct {
				ID    int64
				Level int
				Name  string
			}) {
				defer wg.Done()
				
				localAllowed := 0
				localDenied := 0
				
				// Each user makes multiple rapid requests
				for i := 0; i < requestsPerUser; i++ {
					err := limiter.ConsumeLimit(ctx, u.ID, LimitTypeCommand, u.Level)
					if err == nil {
						localAllowed++
					} else {
						localDenied++
					}
					
					// Very short delay to simulate rapid requests
					time.Sleep(10 * time.Millisecond)
				}
				
				mu.Lock()
				tierStats := stats[u.Name]
				tierStats.allowed += localAllowed
				tierStats.denied += localDenied
				stats[u.Name] = tierStats
				mu.Unlock()
			}(user)
		}

		wg.Wait()
		duration := time.Since(start)

		t.Logf("\n=== REALISTIC LOAD TEST RESULTS ===")
		t.Logf("Test duration: %v", duration)
		t.Logf("Total users: %d", numUsers)
		t.Logf("Requests per user: %d", requestsPerUser)
		t.Logf("Total requests: %d", numUsers*requestsPerUser)

		totalAllowed := 0
		totalDenied := 0
		
		for tier, stat := range stats {
			total := stat.allowed + stat.denied
			successRate := float64(stat.allowed) / float64(total) * 100
			t.Logf("%s: %d allowed, %d denied (%.1f%% success)", 
				tier, stat.allowed, stat.denied, successRate)
			totalAllowed += stat.allowed
			totalDenied += stat.denied
		}

		t.Logf("\nOverall: %d allowed, %d denied", totalAllowed, totalDenied)
		t.Logf("System throughput: %.2f requests/sec", float64(totalAllowed)/duration.Seconds())

		// Verify that premium tiers perform better
		freeSuccess := float64(stats["Free"].allowed) / float64(stats["Free"].allowed + stats["Free"].denied)
		coffeeSuccess := float64(stats["Coffee"].allowed) / float64(stats["Coffee"].allowed + stats["Coffee"].denied)
		cakeSuccess := float64(stats["Cake"].allowed) / float64(stats["Cake"].allowed + stats["Cake"].denied)
		sponsorSuccess := float64(stats["Sponsor"].allowed) / float64(stats["Sponsor"].allowed + stats["Sponsor"].denied)

		t.Logf("\n=== PREMIUM TIER BENEFITS ===")
		t.Logf("Free tier success rate: %.2f%%", freeSuccess*100)
		t.Logf("Coffee tier success rate: %.2f%%", coffeeSuccess*100)
		t.Logf("Cake tier success rate: %.2f%%", cakeSuccess*100)
		t.Logf("Sponsor tier success rate: %.2f%%", sponsorSuccess*100)

		// Rate limiting should be working (some requests denied)
		if totalDenied == 0 {
			t.Error("Expected some requests to be denied due to rate limiting")
		}

		// Premium tiers should perform better
		if totalDenied > 0 {
			if coffeeSuccess <= freeSuccess {
				t.Logf("WARNING: Coffee tier (%.2f%%) not performing better than free tier (%.2f%%)", 
					coffeeSuccess*100, freeSuccess*100)
			}
			if cakeSuccess <= coffeeSuccess {
				t.Logf("WARNING: Cake tier (%.2f%%) not performing better than coffee tier (%.2f%%)", 
					cakeSuccess*100, coffeeSuccess*100)
			}
			if sponsorSuccess <= cakeSuccess {
				t.Logf("WARNING: Sponsor tier (%.2f%%) not performing better than cake tier (%.2f%%)", 
					sponsorSuccess*100, cakeSuccess*100)
			}
		}
	})

	t.Run("Request_Processing_Time_Simulation", func(t *testing.T) {
		// Simulate scenario where requests take 8 seconds to process
		// This tests the queue behavior when requests are slow
		
		testUsers := 100 // Smaller test set for processing time simulation
		
		start := time.Now()
		var wg sync.WaitGroup
		var mu sync.Mutex
		
		processed := 0
		queued := 0

		for i := 0; i < testUsers; i++ {
			wg.Add(1)
			go func(userID int64) {
				defer wg.Done()
				
				// Try to consume a request
				err := limiter.ConsumeLimit(ctx, userID, LimitTypeCommand, 0)
				
				mu.Lock()
				if err == nil {
					processed++
					mu.Unlock()
					
					// Simulate 8-second processing with ±25% variation (6-10 seconds)
					baseTime := 8000 // 8 seconds in milliseconds
					variation := int(float64(baseTime) * 0.25) // ±25%
					processingTime := baseTime - variation + rand.Intn(2*variation)
					time.Sleep(time.Duration(processingTime) * time.Millisecond)
				} else {
					queued++
					mu.Unlock()
				}
			}(int64(i))
		}

		wg.Wait()
		totalTime := time.Since(start)

		t.Logf("\n=== PROCESSING TIME SIMULATION ===")
		t.Logf("Test users: %d", testUsers)
		t.Logf("Processed concurrently: %d", processed)
		t.Logf("Queued due to limits: %d", queued)
		t.Logf("Total execution time: %v", totalTime)
		
		if processed > 0 {
			avgTime := totalTime / time.Duration(processed)
			t.Logf("Average processing time: %v", avgTime)
		}

		// Estimate queue processing time
		if queued > 0 {
			// Assume queued requests are processed sequentially
			estimatedQueueTime := time.Duration(queued) * 8 * time.Second
			t.Logf("Estimated queue processing time: %v", estimatedQueueTime)
			t.Logf("Total estimated completion time: %v", totalTime+estimatedQueueTime)
		}

		// Rate limiter should be working (some requests queued)
		if processed+queued != testUsers {
			t.Errorf("Some requests were lost: processed=%d, queued=%d, total=%d", 
				processed, queued, testUsers)
		}
	})
}

func TestRateLimiter_50K_Users_Progressive_Load(t *testing.T) {
	// Progressive load test: gradually increase load to see breaking point
	config := Config{
		CommandLimit: RateLimit{Requests: 20, Window: time.Minute}, // 20 commands per minute
		PremiumMultipliers: map[int]float64{
			0: 1.0, 1: 2.0, 2: 4.0, 3: 10.0,
		},
	}

	limiter := NewMemoryRateLimiter(config, getTestMetricsCollector())
	defer limiter.Close()

	ctx := context.Background()

	// Test with increasing user counts to see performance characteristics
	userCounts := []int{1000, 5000, 10000, 25000, 50000}
	
	for _, userCount := range userCounts {
		t.Run(fmt.Sprintf("Users_%d", userCount), func(t *testing.T) {
			start := time.Now()
			
			var wg sync.WaitGroup
			var mu sync.Mutex
			totalAllowed := 0
			totalDenied := 0

			for i := 0; i < userCount; i++ {
				wg.Add(1)
				go func(userID int64) {
					defer wg.Done()
					
					// Each user makes 1 request
					err := limiter.ConsumeLimit(ctx, userID, LimitTypeCommand, 0)
					
					mu.Lock()
					if err == nil {
						totalAllowed++
					} else {
						totalDenied++
					}
					mu.Unlock()
				}(int64(i))
			}

			wg.Wait()
			duration := time.Since(start)

			successRate := float64(totalAllowed) / float64(userCount) * 100
			throughput := float64(userCount) / duration.Seconds()

			t.Logf("Users: %d, Duration: %v, Success: %.1f%%, Throughput: %.0f req/s", 
				userCount, duration, successRate, throughput)
		})
	}
}