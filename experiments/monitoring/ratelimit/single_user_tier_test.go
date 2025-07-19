//go:build experiments

package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// Test to find breaking points with SINGLE USER per tier 
// This properly tests the rate limiting since each tier gets one user ID

func TestRateLimiter_SingleUser_TierBreakingPoints(t *testing.T) {
	// Configuration with clear limits
	config := Config{
		CommandLimit: RateLimit{Requests: 10, Window: time.Second},
		PremiumMultipliers: map[int]float64{
			0: 1.0,  // Free: 10 req/sec
			1: 2.0,  // Coffee: 20 req/sec
			2: 4.0,  // Cake: 40 req/sec
			3: 8.0,  // Sponsor: 80 req/sec
		},
	}

	limiter := NewMemoryRateLimiter(config, getTestMetricsCollector())
	defer limiter.Close()

	ctx := context.Background()

	tiers := []struct {
		name         string
		level        int
		expectedLimit int
		testCounts   []int
	}{
		{"Free", 0, 10, []int{10, 15, 20, 30, 50}},
		{"Coffee", 1, 20, []int{20, 30, 40, 60, 100}},
		{"Cake", 2, 40, []int{40, 60, 80, 120, 200}},
		{"Sponsor", 3, 80, []int{80, 120, 160, 240, 400}},
	}

	for _, tier := range tiers {
		t.Run(tier.name+"_SingleUser_Breakdown", func(t *testing.T) {
			t.Logf("\n=== %s TIER SINGLE USER TEST ===", tier.name)
			t.Logf("Expected limit: %d requests/second", tier.expectedLimit)

			for _, requestCount := range tier.testCounts {
				t.Run(fmt.Sprintf("Requests_%d", requestCount), func(t *testing.T) {
					userID := int64(tier.level + 1000) // One user per tier
					
					var mu sync.Mutex
					allowed := 0
					denied := 0
					
					var wg sync.WaitGroup
					start := time.Now()

					// Single user makes many concurrent requests
					for i := 0; i < requestCount; i++ {
						wg.Add(1)
						go func() {
							defer wg.Done()
							
							err := limiter.ConsumeLimit(ctx, userID, LimitTypeCommand, tier.level)
							
							mu.Lock()
							if err == nil {
								allowed++
							} else {
								denied++
							}
							mu.Unlock()
						}()
					}

					wg.Wait()
					duration := time.Since(start)

					successRate := float64(allowed) / float64(requestCount) * 100
					
					t.Logf("Requests: %d, Allowed: %d, Denied: %d, Success: %.1f%%, Duration: %v",
						requestCount, allowed, denied, successRate, duration)

					// Highlight 50% breaking point
					if successRate >= 45 && successRate <= 55 {
						t.Logf("*** %s TIER ~50%% BREAKING POINT: %d requests = %.1f%% success ***",
							tier.name, requestCount, successRate)
					}

					// Wait for window reset
					time.Sleep(200 * time.Millisecond)
				})
			}
		})
	}
}

func TestRateLimiter_MultiUser_ScaleToBreakingPoint(t *testing.T) {
	t.Skip("Skipping multi-user scale test - too slow for CI")
	// Test: How many users of each tier until we hit 50% success rate?
	// Each user makes 2 requests, so at 50% success rate:
	// Free (10 req/sec): Need 10 users (20 requests, 10 allowed)
	// Coffee (20 req/sec): Need 20 users (40 requests, 20 allowed)  
	// Cake (40 req/sec): Need 40 users (80 requests, 40 allowed)
	// Sponsor (80 req/sec): Need 80 users (160 requests, 80 allowed)
	
	config := Config{
		CommandLimit: RateLimit{Requests: 10, Window: time.Second},
		PremiumMultipliers: map[int]float64{
			0: 1.0, 1: 2.0, 2: 4.0, 3: 8.0,
		},
	}

	limiter := NewMemoryRateLimiter(config, getTestMetricsCollector())
	defer limiter.Close()

	ctx := context.Background()

	scenarios := []struct {
		tierName      string
		tierLevel     int
		expectedLimit int
		userCounts    []int
	}{
		{"Free", 0, 10, []int{5, 10, 15, 20}},      // Expected 50% at 10 users
		{"Coffee", 1, 20, []int{10, 20, 30, 40}},   // Expected 50% at 20 users
		{"Cake", 2, 40, []int{20, 40, 60, 80}},     // Expected 50% at 40 users
		{"Sponsor", 3, 80, []int{40, 80, 120, 160}}, // Expected 50% at 80 users
	}

	for _, scenario := range scenarios {
		t.Run(scenario.tierName+"_UserScale_BreakingPoint", func(t *testing.T) {
			t.Logf("\n=== %s TIER USER SCALING TEST ===", scenario.tierName)
			t.Logf("Expected limit: %d req/sec, Looking for 50%% at %d users", 
				scenario.expectedLimit, scenario.expectedLimit)

			for _, userCount := range scenario.userCounts {
				t.Run(fmt.Sprintf("Users_%d", userCount), func(t *testing.T) {
					var mu sync.Mutex
					totalAllowed := 0
					totalRequests := userCount * 2 // Each user makes 2 requests
					
					var wg sync.WaitGroup
					start := time.Now()

					// Create multiple users, each making 2 requests
					for i := 0; i < userCount; i++ {
						wg.Add(1)
						go func(userId int64) {
							defer wg.Done()
							
							localAllowed := 0
							for j := 0; j < 2; j++ {
								err := limiter.ConsumeLimit(ctx, userId, LimitTypeCommand, scenario.tierLevel)
								if err == nil {
									localAllowed++
								}
							}
							
							mu.Lock()
							totalAllowed += localAllowed
							mu.Unlock()
						}(int64(scenario.tierLevel*10000 + i))
					}

					wg.Wait()
					duration := time.Since(start)

					successRate := float64(totalAllowed) / float64(totalRequests) * 100
					
					t.Logf("%s: %d users, %d total requests, %d allowed, %.1f%% success, %v",
						scenario.tierName, userCount, totalRequests, totalAllowed, successRate, duration)

					// Highlight expected 50% point
					expectedBreakingPoint := scenario.expectedLimit
					if userCount == expectedBreakingPoint {
						t.Logf("*** %s EXPECTED 50%% POINT: %d users = %.1f%% success ***",
							scenario.tierName, userCount, successRate)
					}

					// Wait for window reset
					time.Sleep(200 * time.Millisecond)
				})
			}
		})
	}
}

func TestRateLimiter_CrossTier_Competition(t *testing.T) {
	t.Skip("Skipping cross-tier competition test - too slow for CI")
	// Test: All tiers competing with realistic user distributions
	// This should show clear tier hierarchy even under load
	
	config := Config{
		CommandLimit: RateLimit{Requests: 20, Window: time.Second}, // 20 req/sec base
		PremiumMultipliers: map[int]float64{
			0: 1.0, 1: 2.0, 2: 4.0, 3: 8.0,
		},
	}

	limiter := NewMemoryRateLimiter(config, getTestMetricsCollector())
	defer limiter.Close()

	ctx := context.Background()

	// Simulate realistic load scenarios
	loadScenarios := []struct {
		name              string
		freeUsers         int
		coffeeUsers       int
		cakeUsers         int
		sponsorUsers      int
		requestsPerUser   int
	}{
		{
			name: "Light_Load_All_Should_Succeed",
			freeUsers: 5, coffeeUsers: 2, cakeUsers: 1, sponsorUsers: 1,
			requestsPerUser: 2,
		},
		{
			name: "Medium_Load_Free_Should_Struggle", 
			freeUsers: 15, coffeeUsers: 5, cakeUsers: 2, sponsorUsers: 1,
			requestsPerUser: 3,
		},
		{
			name: "Heavy_Load_Clear_Tier_Benefits",
			freeUsers: 25, coffeeUsers: 8, cakeUsers: 4, sponsorUsers: 2,
			requestsPerUser: 4,
		},
	}

	for _, scenario := range loadScenarios {
		t.Run(scenario.name, func(t *testing.T) {
			t.Logf("\n=== %s SCENARIO ===", scenario.name)
			t.Logf("Users: Free=%d, Coffee=%d, Cake=%d, Sponsor=%d, Requests per user=%d",
				scenario.freeUsers, scenario.coffeeUsers, scenario.cakeUsers, 
				scenario.sponsorUsers, scenario.requestsPerUser)

			var mu sync.Mutex
			stats := map[string]struct {
				allowed int
				denied  int
				total   int
			}{
				"Free": {0, 0, 0}, "Coffee": {0, 0, 0}, 
				"Cake": {0, 0, 0}, "Sponsor": {0, 0, 0},
			}

			var wg sync.WaitGroup
			start := time.Now()

			// Create users for each tier
			tiers := []struct {
				name      string
				level     int
				userCount int
			}{
				{"Free", 0, scenario.freeUsers},
				{"Coffee", 1, scenario.coffeeUsers},
				{"Cake", 2, scenario.cakeUsers},
				{"Sponsor", 3, scenario.sponsorUsers},
			}

			for _, tier := range tiers {
				for userIndex := 0; userIndex < tier.userCount; userIndex++ {
					wg.Add(1)
					go func(tierName string, tierLevel int, userId int64) {
						defer wg.Done()
						
						localAllowed := 0
						localDenied := 0
						
						for i := 0; i < scenario.requestsPerUser; i++ {
							err := limiter.ConsumeLimit(ctx, userId, LimitTypeCommand, tierLevel)
							if err == nil {
								localAllowed++
							} else {
								localDenied++
							}
						}
						
						mu.Lock()
						tierStats := stats[tierName]
						tierStats.allowed += localAllowed
						tierStats.denied += localDenied
						tierStats.total += scenario.requestsPerUser
						stats[tierName] = tierStats
						mu.Unlock()
					}(tier.name, tier.level, int64(tier.level*1000+userIndex))
				}
			}

			wg.Wait()
			duration := time.Since(start)

			t.Logf("Total test duration: %v", duration)
			t.Logf("\n=== CROSS-TIER COMPETITION RESULTS ===")

			for _, tier := range tiers {
				stat := stats[tier.name]
				if stat.total > 0 {
					successRate := float64(stat.allowed) / float64(stat.total) * 100
					t.Logf("%s: %d/%d allowed (%.1f%% success)",
						tier.name, stat.allowed, stat.total, successRate)
				}
			}

			// Verify tier hierarchy
			if stats["Free"].total > 0 && stats["Coffee"].total > 0 {
				freeSuccess := float64(stats["Free"].allowed) / float64(stats["Free"].total)
				coffeeSuccess := float64(stats["Coffee"].allowed) / float64(stats["Coffee"].total)
				
				t.Logf("\n=== TIER COMPARISON ===")
				t.Logf("Free vs Coffee: %.1f%% vs %.1f%%", freeSuccess*100, coffeeSuccess*100)
				
				if coffeeSuccess >= freeSuccess {
					t.Logf("✅ Coffee tier performing better than or equal to Free tier")
				} else {
					t.Logf("⚠️  Coffee tier (%.1f%%) performing worse than Free tier (%.1f%%)", 
						coffeeSuccess*100, freeSuccess*100)
				}
			}

			// Wait for reset
			time.Sleep(1500 * time.Millisecond)
		})
	}
}