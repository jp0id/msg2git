//go:build experiments

package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// Test to find the breaking point for each premium tier
// This will confirm that ALL tiers are actually rate limited, not just free tier

func TestRateLimiter_TierBreakingPointAnalysis(t *testing.T) {
	// Configuration: 5 commands/second base limit with premium multipliers
	config := Config{
		CommandLimit: RateLimit{Requests: 5, Window: 100 * time.Millisecond}, // Shorter window for faster tests
		PremiumMultipliers: map[int]float64{
			0: 1.0,  // Free: 5 req/sec
			1: 2.0,  // Coffee: 10 req/sec
			2: 4.0,  // Cake: 20 req/sec
			3: 8.0,  // Sponsor: 40 req/sec
		},
	}

	limiter := NewMemoryRateLimiter(config, getTestMetricsCollector())
	defer limiter.Close()

	ctx := context.Background()

	// Test each tier individually to find their breaking points
	tiers := []struct {
		name         string
		level        int
		expectedLimit int
		testScales   []int // Number of requests to test
	}{
		{"Free", 0, 5, []int{5, 10, 20, 50, 100}},
		{"Coffee", 1, 10, []int{10, 20, 40, 80, 150}},
		{"Cake", 2, 20, []int{20, 40, 80, 150, 300}},
		{"Sponsor", 3, 40, []int{40, 80, 150, 300, 600}},
	}

	for _, tier := range tiers {
		t.Run(tier.name+"_Breaking_Point", func(t *testing.T) {
			t.Logf("\n=== %s TIER BREAKING POINT ANALYSIS ===", tier.name)
			t.Logf("Expected limit: %d requests/second", tier.expectedLimit)

			for _, requestCount := range tier.testScales {
				t.Run(fmt.Sprintf("Requests_%d", requestCount), func(t *testing.T) {
					userID := int64(1000 + tier.level) // Unique user per tier
					
					var mu sync.Mutex
					allowed := 0
					denied := 0
					
					var wg sync.WaitGroup
					start := time.Now()

					// Send all requests simultaneously
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

					// Verify the allowed count matches expected limit (with reasonable tolerance for race conditions)
					if requestCount <= tier.expectedLimit {
						if allowed != requestCount {
							t.Errorf("Expected all %d requests to be allowed for %s tier, but only %d were allowed",
								requestCount, tier.name, allowed)
						}
					} else {
						// For requests exceeding limit, should allow approximately the limit (±15% tolerance)
						tolerance := 3  // Minimum tolerance of 3 for race conditions
						if tier.expectedLimit/7 > 3 {  // About 15% tolerance
							tolerance = tier.expectedLimit / 7
						}
						if allowed < tier.expectedLimit-tolerance || allowed > tier.expectedLimit+tolerance {
							t.Errorf("Expected ~%d requests to be allowed for %s tier (tolerance: ±%d), but %d were allowed",
								tier.expectedLimit, tier.name, tolerance, allowed)
						}
					}

					// Reset for next test (wait for window to expire)
					time.Sleep(200 * time.Millisecond)
				})
			}
		})
	}
}

func TestRateLimiter_MultiTier_CompetitiveLoad(t *testing.T) {
	t.Skip("Skipping multi-tier competitive load test - too slow for CI")
	// Test all tiers competing for resources simultaneously
	config := Config{
		CommandLimit: RateLimit{Requests: 10, Window: time.Second}, // 10 req/sec base
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

	// Test scenarios with increasing load where each tier reaches its breaking point
	scenarios := []struct {
		name              string
		usersPerTier      int
		requestsPerUser   int
		expectedOutcome   map[string]string
	}{
		{
			name:            "Light_Load",
			usersPerTier:    5,
			requestsPerUser: 2,
			expectedOutcome: map[string]string{
				"Free": "100%", "Coffee": "100%", "Cake": "100%", "Sponsor": "100%",
			},
		},
		{
			name:            "Medium_Load",
			usersPerTier:    10,
			requestsPerUser: 5,
			expectedOutcome: map[string]string{
				"Free": "20%", "Coffee": "40%", "Cake": "80%", "Sponsor": "100%",
			},
		},
		{
			name:            "Heavy_Load",
			usersPerTier:    20,
			requestsPerUser: 10,
			expectedOutcome: map[string]string{
				"Free": "5%", "Coffee": "10%", "Cake": "20%", "Sponsor": "40%",
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			t.Logf("\n=== %s SCENARIO ===", scenario.name)
			t.Logf("Users per tier: %d, Requests per user: %d", scenario.usersPerTier, scenario.requestsPerUser)

			var mu sync.Mutex
			stats := map[string]struct {
				allowed int
				denied  int
				total   int
			}{
				"Free": {0, 0, 0}, "Coffee": {0, 0, 0}, "Cake": {0, 0, 0}, "Sponsor": {0, 0, 0},
			}

			var wg sync.WaitGroup
			start := time.Now()

			// Create users for each tier
			tiers := []struct {
				name  string
				level int
			}{
				{"Free", 0}, {"Coffee", 1}, {"Cake", 2}, {"Sponsor", 3},
			}

			for _, tier := range tiers {
				for userIndex := 0; userIndex < scenario.usersPerTier; userIndex++ {
					wg.Add(1)
					go func(tierName string, tierLevel int, userId int64) {
						defer wg.Done()
						
						localAllowed := 0
						localDenied := 0
						
						// Each user makes multiple requests
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
			t.Logf("\n=== COMPETITIVE LOAD RESULTS ===")

			for _, tier := range tiers {
				stat := stats[tier.name]
				successRate := float64(stat.allowed) / float64(stat.total) * 100
				expectedSuccessRate := scenario.expectedOutcome[tier.name]
				
				t.Logf("%s: %d/%d requests allowed (%.1f%% success) - Expected: %s",
					tier.name, stat.allowed, stat.total, successRate, expectedSuccessRate)
			}

			// Verify tier hierarchy is maintained
			freeSuccess := float64(stats["Free"].allowed) / float64(stats["Free"].total)
			coffeeSuccess := float64(stats["Coffee"].allowed) / float64(stats["Coffee"].total)
			cakeSuccess := float64(stats["Cake"].allowed) / float64(stats["Cake"].total)
			sponsorSuccess := float64(stats["Sponsor"].allowed) / float64(stats["Sponsor"].total)

			t.Logf("\n=== TIER HIERARCHY VERIFICATION ===")
			t.Logf("Free: %.2f%%, Coffee: %.2f%%, Cake: %.2f%%, Sponsor: %.2f%%",
				freeSuccess*100, coffeeSuccess*100, cakeSuccess*100, sponsorSuccess*100)

			// Each higher tier should perform better than or equal to lower tiers
			if coffeeSuccess < freeSuccess {
				t.Errorf("Coffee tier (%.2f%%) performing worse than Free tier (%.2f%%)",
					coffeeSuccess*100, freeSuccess*100)
			}
			if cakeSuccess < coffeeSuccess {
				t.Errorf("Cake tier (%.2f%%) performing worse than Coffee tier (%.2f%%)",
					cakeSuccess*100, coffeeSuccess*100)
			}
			if sponsorSuccess < cakeSuccess {
				t.Errorf("Sponsor tier (%.2f%%) performing worse than Cake tier (%.2f%%)",
					sponsorSuccess*100, cakeSuccess*100)
			}

			// Wait for window reset before next scenario
			time.Sleep(1200 * time.Millisecond)
		})
	}
}

func TestRateLimiter_FindExact50PercentBreakingPoint(t *testing.T) {
	t.Skip("Skipping exact breaking point test - too slow for CI")
	// Find the exact user scale where each tier drops to ~50% success rate
	config := Config{
		CommandLimit: RateLimit{Requests: 10, Window: time.Second}, // 10 req/sec base
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
	}{
		{"Free", 0, 10},
		{"Coffee", 1, 20},
		{"Cake", 2, 40},
		{"Sponsor", 3, 80},
	}

	t.Logf("\n=== FINDING 50%% SUCCESS RATE BREAKING POINTS ===")
	t.Logf("Target: Find user count where each tier achieves ~50%% success rate")
	t.Logf("Method: Each user makes 2 requests, success rate = allowed/(users*2)")

	for _, tier := range tiers {
		t.Run(tier.name+"_50_Percent_Point", func(t *testing.T) {
			// For 50% success rate with 2 requests per user:
			// If limit is L req/sec, and each user makes 2 requests
			// We need users = L / (2 * 0.5) = L users for 50% success
			// So for 50% success: users should be L * 2
			expectedUsersFor50Percent := tier.expectedLimit * 2

			userCounts := []int{
				tier.expectedLimit,           // Should be ~100% success
				tier.expectedLimit * 2,       // Should be ~50% success
				tier.expectedLimit * 4,       // Should be ~25% success
			}

			for _, userCount := range userCounts {
				t.Run(fmt.Sprintf("Users_%d", userCount), func(t *testing.T) {
					var mu sync.Mutex
					totalAllowed := 0
					totalRequests := userCount * 2
					
					var wg sync.WaitGroup
					start := time.Now()

					for i := 0; i < userCount; i++ {
						wg.Add(1)
						go func(userId int64) {
							defer wg.Done()
							
							localAllowed := 0
							// Each user makes 2 requests
							for j := 0; j < 2; j++ {
								err := limiter.ConsumeLimit(ctx, userId, LimitTypeCommand, tier.level)
								if err == nil {
									localAllowed++
								}
							}
							
							mu.Lock()
							totalAllowed += localAllowed
							mu.Unlock()
						}(int64(tier.level*10000 + i))
					}

					wg.Wait()
					duration := time.Since(start)

					successRate := float64(totalAllowed) / float64(totalRequests) * 100
					
					t.Logf("%s tier - Users: %d, Total requests: %d, Allowed: %d, Success: %.1f%%, Duration: %v",
						tier.name, userCount, totalRequests, totalAllowed, successRate, duration)

					// Highlight the 50% breaking point
					if userCount == expectedUsersFor50Percent {
						t.Logf("*** %s TIER 50%% BREAKING POINT: %d users = %.1f%% success rate ***",
							tier.name, userCount, successRate)
					}

					// Wait for window reset
					time.Sleep(1200 * time.Millisecond)
				})
			}
		})
	}
}