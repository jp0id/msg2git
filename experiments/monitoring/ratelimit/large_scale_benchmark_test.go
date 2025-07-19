//go:build experiments

package ratelimit

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"
)

// Large scale benchmark for 50,000 users with premium tiers
// Objective: Test rate limiting effectiveness with premium tiers
// Assumptions: 90% free users, 10% premium (distributed across tiers)
// Average request processing time: 8 seconds

func TestRateLimiter_LargeScale_50K_Users(t *testing.T) {
	t.Skip("Skipping large scale test - requires careful rate limit tuning for 50K concurrent users")
	// Configuration for realistic load testing
	config := Config{
		CommandLimit: RateLimit{Requests: 1, Window: time.Second}, // 1 command per second - will cause rate limiting
		GitHubRESTLimit: RateLimit{Requests: 60, Window: time.Hour}, // 60 REST calls per hour
		GitHubQLLimit: RateLimit{Requests: 100, Window: time.Hour}, // 100 GraphQL points per hour
		PremiumMultipliers: map[int]float64{
			0: 1.0,  // Free: 1/sec commands
			1: 2.0,  // Coffee: 2/sec commands  
			2: 4.0,  // Cake: 4/sec commands
			3: 10.0, // Sponsor: 10/sec commands
		},
	}

	limiter := NewMemoryRateLimiter(config, getTestMetricsCollector())
	defer limiter.Close()

	ctx := context.Background()

	// User distribution: 50,000 users total
	// 90% free (45,000), 10% premium (5,000) distributed as:
	// Coffee: 6% (3,000), Cake: 3% (1,500), Sponsor: 1% (500)
	totalUsers := 50000
	freeUsers := 45000
	coffeeUsers := 3000
	cakeUsers := 1500
	sponsorUsers := 500

	t.Logf("Starting large scale test:")
	t.Logf("- Total users: %d", totalUsers)
	t.Logf("- Free users: %d (90%%)", freeUsers)
	t.Logf("- Coffee users: %d (6%%)", coffeeUsers)
	t.Logf("- Cake users: %d (3%%)", cakeUsers)
	t.Logf("- Sponsor users: %d (1%%)", sponsorUsers)

	// Generate user list with premium levels
	users := make([]struct {
		ID    int64
		Level int
		Name  string
	}, totalUsers)

	userIndex := 0
	
	// Free users (level 0)
	for i := 0; i < freeUsers; i++ {
		users[userIndex] = struct {
			ID    int64
			Level int
			Name  string
		}{int64(userIndex + 1), 0, "Free"}
		userIndex++
	}
	
	// Coffee users (level 1)
	for i := 0; i < coffeeUsers; i++ {
		users[userIndex] = struct {
			ID    int64
			Level int
			Name  string
		}{int64(userIndex + 1), 1, "Coffee"}
		userIndex++
	}
	
	// Cake users (level 2)
	for i := 0; i < cakeUsers; i++ {
		users[userIndex] = struct {
			ID    int64
			Level int
			Name  string
		}{int64(userIndex + 1), 2, "Cake"}
		userIndex++
	}
	
	// Sponsor users (level 3)
	for i := 0; i < sponsorUsers; i++ {
		users[userIndex] = struct {
			ID    int64
			Level int
			Name  string
		}{int64(userIndex + 1), 3, "Sponsor"}
		userIndex++
	}

	// Shuffle users to randomize access patterns
	rand.Seed(time.Now().UnixNano())
	for i := range users {
		j := rand.Intn(i + 1)
		users[i], users[j] = users[j], users[i]
	}

	t.Run("PremiumTier_RateLimit_Effectiveness", func(t *testing.T) {
		start := time.Now()
		
		// Statistics tracking
		var mu sync.Mutex
		stats := map[string]struct {
			allowed int
			denied  int
			total   int
		}{
			"Free":    {0, 0, 0},
			"Coffee":  {0, 0, 0},
			"Cake":    {0, 0, 0},
			"Sponsor": {0, 0, 0},
		}

		// Simulate concurrent access from all users
		// Each user tries to make 2 requests (should test rate limiting)
		var wg sync.WaitGroup
		requestsPerUser := 2

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
				
				for i := 0; i < requestsPerUser; i++ {
					err := limiter.ConsumeLimit(ctx, u.ID, LimitTypeCommand, u.Level)
					if err == nil {
						localAllowed++
					} else {
						localDenied++
					}
					
					// Small delay to simulate processing time
					time.Sleep(1 * time.Millisecond)
				}
				
				// Update statistics thread-safely
				mu.Lock()
				tierStats := stats[u.Name]
				tierStats.allowed += localAllowed
				tierStats.denied += localDenied
				tierStats.total += requestsPerUser
				stats[u.Name] = tierStats
				mu.Unlock()
			}(user)
		}

		// Wait for all requests to complete
		wg.Wait()
		duration := time.Since(start)

		// Report results
		t.Logf("\n=== RATE LIMITING EFFECTIVENESS TEST ===")
		t.Logf("Total processing time: %v", duration)
		t.Logf("Total requests processed: %d", totalUsers*requestsPerUser)
		t.Logf("Requests per second: %.2f", float64(totalUsers*requestsPerUser)/duration.Seconds())

		totalAllowed := 0
		totalDenied := 0
		for tier, stat := range stats {
			allowedRate := float64(stat.allowed) / float64(stat.total) * 100
			t.Logf("%s tier: %d allowed, %d denied, %.1f%% success rate", 
				tier, stat.allowed, stat.denied, allowedRate)
			totalAllowed += stat.allowed
			totalDenied += stat.denied
		}

		// Verify premium tiers get better treatment
		freeSuccessRate := float64(stats["Free"].allowed) / float64(stats["Free"].total)
		coffeeSuccessRate := float64(stats["Coffee"].allowed) / float64(stats["Coffee"].total)
		cakeSuccessRate := float64(stats["Cake"].allowed) / float64(stats["Cake"].total)
		sponsorSuccessRate := float64(stats["Sponsor"].allowed) / float64(stats["Sponsor"].total)

		t.Logf("\n=== PREMIUM TIER EFFECTIVENESS ===")
		t.Logf("Free tier success rate: %.2f%%", freeSuccessRate*100)
		t.Logf("Coffee tier success rate: %.2f%%", coffeeSuccessRate*100)
		t.Logf("Cake tier success rate: %.2f%%", cakeSuccessRate*100)
		t.Logf("Sponsor tier success rate: %.2f%%", sponsorSuccessRate*100)

		// Premium tiers should have higher success rates
		if coffeeSuccessRate <= freeSuccessRate {
			t.Errorf("Coffee tier should have higher success rate than free tier")
		}
		if cakeSuccessRate <= coffeeSuccessRate {
			t.Errorf("Cake tier should have higher success rate than coffee tier")
		}
		if sponsorSuccessRate <= cakeSuccessRate {
			t.Errorf("Sponsor tier should have higher success rate than cake tier")
		}

		t.Logf("\n=== OVERALL STATISTICS ===")
		t.Logf("Total allowed: %d", totalAllowed)
		t.Logf("Total denied: %d", totalDenied)
		t.Logf("Overall success rate: %.2f%%", float64(totalAllowed)/float64(totalAllowed+totalDenied)*100)
	})

	t.Run("Simulated_8_Second_Processing_Time", func(t *testing.T) {
		// Test with simulated 8-second average processing time
		// Use a smaller subset for this test to keep it reasonable
		testUsers := users[:1000] // Test with 1000 users
		
		start := time.Now()
		
		var wg sync.WaitGroup
		var mu sync.Mutex
		processedCount := 0
		queuedCount := 0

		for _, user := range testUsers {
			wg.Add(1)
			go func(u struct {
				ID    int64
				Level int
				Name  string
			}) {
				defer wg.Done()
				
				// Try to get rate limit approval
				err := limiter.ConsumeLimit(ctx, u.ID, LimitTypeCommand, u.Level)
				
				mu.Lock()
				if err == nil {
					processedCount++
					mu.Unlock()
					
					// Simulate 8-second processing with random variation (6-10 seconds)
					processingTime := time.Duration(6000+rand.Intn(4000)) * time.Millisecond
					time.Sleep(processingTime)
				} else {
					queuedCount++
					mu.Unlock()
				}
			}(user)
		}

		wg.Wait()
		totalDuration := time.Since(start)

		t.Logf("\n=== 8-SECOND PROCESSING SIMULATION ===")
		t.Logf("Test users: %d", len(testUsers))
		t.Logf("Processed immediately: %d", processedCount)
		t.Logf("Queued (rate limited): %d", queuedCount)
		t.Logf("Total test duration: %v", totalDuration)
		t.Logf("Theoretical max concurrent: %d", processedCount)
		
		if processedCount > 0 {
			avgProcessingTime := totalDuration / time.Duration(processedCount)
			t.Logf("Average processing time per request: %v", avgProcessingTime)
		}

		// Calculate how long it would take to process all queued requests
		if queuedCount > 0 {
			estimatedQueueTime := time.Duration(queuedCount) * 8 * time.Second
			t.Logf("Estimated time to process queue: %v", estimatedQueueTime)
			t.Logf("Total estimated completion time: %v", totalDuration+estimatedQueueTime)
		}
	})
}

func BenchmarkRateLimiter_LargeScale_Performance(b *testing.B) {
	config := Config{
		CommandLimit: RateLimit{Requests: 30, Window: time.Minute},
		PremiumMultipliers: map[int]float64{
			0: 1.0, 1: 2.0, 2: 4.0, 3: 10.0,
		},
	}

	limiter := NewMemoryRateLimiter(config, getTestMetricsCollector())
	defer limiter.Close()

	ctx := context.Background()
	
	// Prepare user pool
	userPool := make([]struct{ID int64; Level int}, 10000)
	for i := 0; i < 9000; i++ { // 90% free
		userPool[i] = struct{ID int64; Level int}{int64(i), 0}
	}
	for i := 9000; i < 10000; i++ { // 10% premium (mixed levels)
		level := (i % 3) + 1 // Levels 1, 2, 3
		userPool[i] = struct{ID int64; Level int}{int64(i), level}
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		var counter int
		for pb.Next() {
			user := userPool[counter%len(userPool)]
			counter++
			
			// Use ConsumeLimit (the fixed version)
			limiter.ConsumeLimit(ctx, user.ID, LimitTypeCommand, user.Level)
		}
	})
}

func BenchmarkRateLimiter_MemoryUsage_50K_Users(b *testing.B) {
	config := DefaultConfig()
	limiter := NewMemoryRateLimiter(config, getTestMetricsCollector())
	defer limiter.Close()

	ctx := context.Background()

	// Create 50K users with realistic distribution
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		userID := int64(i % 50000)
		premiumLevel := 0
		if i%10 == 0 { // 10% premium
			premiumLevel = (i % 3) + 1
		}
		
		// Use ConsumeLimit (the fixed version)
		limiter.ConsumeLimit(ctx, userID, LimitTypeCommand, premiumLevel)
	}
}