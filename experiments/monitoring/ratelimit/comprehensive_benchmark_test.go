//go:build experiments

package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// Comprehensive benchmark suite for rate limiter performance and correctness

func BenchmarkRateLimiter_Comprehensive(b *testing.B) {
	benchmarks := []struct {
		name string
		fn   func(*testing.B)
	}{
		{"SingleUser_Sequential", benchmarkSingleUserSequential},
		{"SingleUser_Concurrent", benchmarkSingleUserConcurrent},
		{"MultiUser_Sequential", benchmarkMultiUserSequential},
		{"MultiUser_Concurrent", benchmarkMultiUserConcurrent},
		{"PremiumTiers_Performance", benchmarkPremiumTiersPerformance},
		{"MemoryUsage_Growth", benchmarkMemoryUsageGrowth},
		{"HighLoad_StressTest", benchmarkHighLoadStressTest},
		{"RateLimit_Accuracy", benchmarkRateLimitAccuracy},
		{"Cleanup_Performance", benchmarkCleanupPerformance},
		{"WindowSliding_Accuracy", benchmarkWindowSlidingAccuracy},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, bm.fn)
	}
}

func benchmarkSingleUserSequential(b *testing.B) {
	limiter := createTestLimiterBenchmark()
	ctx := context.Background()
	userID := int64(12345)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = limiter.CheckLimit(ctx, userID, LimitTypeCommand, 0)
	}
}

func benchmarkSingleUserConcurrent(b *testing.B) {
	limiter := createTestLimiterBenchmark()
	ctx := context.Background()
	userID := int64(12345)

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = limiter.CheckLimit(ctx, userID, LimitTypeCommand, 0)
		}
	})
}

func benchmarkMultiUserSequential(b *testing.B) {
	limiter := createTestLimiterBenchmark()
	ctx := context.Background()
	numUsers := 1000

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		userID := int64(i % numUsers)
		_, _ = limiter.CheckLimit(ctx, userID, LimitTypeCommand, 0)
	}
}

func benchmarkMultiUserConcurrent(b *testing.B) {
	limiter := createTestLimiterBenchmark()
	ctx := context.Background()
	numUsers := 1000

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		var counter int
		for pb.Next() {
			userID := int64(counter % numUsers)
			counter++
			_, _ = limiter.CheckLimit(ctx, userID, LimitTypeCommand, 0)
		}
	})
}

func benchmarkPremiumTiersPerformance(b *testing.B) {
	limiter := createTestLimiterBenchmark()
	ctx := context.Background()

	premiumLevels := []int{0, 1, 2, 3} // Free, Coffee, Cake, Sponsor

	for _, level := range premiumLevels {
		b.Run(fmt.Sprintf("PremiumLevel_%d", level), func(b *testing.B) {
			userID := int64(12345 + level)
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, _ = limiter.CheckLimit(ctx, userID, LimitTypeCommand, level)
			}
		})
	}
}

func benchmarkMemoryUsageGrowth(b *testing.B) {
	limiter := createTestLimiterBenchmark()
	ctx := context.Background()

	// Test memory usage with increasing number of users
	userCounts := []int{100, 1000, 10000}

	for _, userCount := range userCounts {
		b.Run(fmt.Sprintf("Users_%d", userCount), func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				userID := int64(i % userCount)
				_, _ = limiter.CheckLimit(ctx, userID, LimitTypeCommand, 0)
			}
		})
	}
}

func benchmarkHighLoadStressTest(b *testing.B) {
	limiter := createTestLimiterBenchmark()
	ctx := context.Background()
	numUsers := 10000

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		var counter int
		for pb.Next() {
			userID := int64(counter % numUsers)
			counter++

			// Simulate mixed workload
			limitTypes := []LimitType{
				LimitTypeCommand,
				LimitTypeGitHubREST,
				LimitTypeGitHubQL,
			}
			limitType := limitTypes[counter%len(limitTypes)]
			premiumLevel := counter % 4

			_, _ = limiter.CheckLimit(ctx, userID, limitType, premiumLevel)
		}
	})
}

func benchmarkRateLimitAccuracy(b *testing.B) {
	// Test rate limit accuracy under load
	config := Config{
		CommandLimit: RateLimit{Requests: 10, Window: time.Second}, // 10 requests per second
		PremiumMultipliers: map[int]float64{
			0: 1.0,
			1: 2.0,
		},
	}

	limiter := NewMemoryRateLimiter(config, getTestMetricsCollector())
	ctx := context.Background()
	userID := int64(12345)

	b.ResetTimer()

	// This benchmark tests accuracy rather than speed
	b.N = 1 // Run once but with controlled timing

	start := time.Now()
	allowed := 0
	denied := 0

	// Try to make 20 requests in 1 second (should allow ~10, deny ~10)
	for i := 0; i < 20; i++ {
		isAllowed, _ := limiter.CheckLimit(ctx, userID, LimitTypeCommand, 0)
		if isAllowed {
			allowed++
		} else {
			denied++
		}
		time.Sleep(50 * time.Millisecond) // 20 requests over 1 second
	}

	elapsed := time.Since(start)
	b.Logf("Accuracy test: %d allowed, %d denied in %v", allowed, denied, elapsed)
}

func benchmarkCleanupPerformance(b *testing.B) {
	limiter := createTestLimiterBenchmark()
	ctx := context.Background()

	// Create data for 10000 users across different time periods
	numUsers := 10000
	for i := 0; i < numUsers; i++ {
		userID := int64(i)
		// Create old and new data
		_, _ = limiter.CheckLimit(ctx, userID, LimitTypeCommand, 0)
	}

	b.ResetTimer()
	b.ReportAllocs()

	// Test performance by creating many requests and letting sliding window clean itself
	for i := 0; i < b.N; i++ {
		userID := int64(i % numUsers)
		_, _ = limiter.CheckLimit(ctx, userID, LimitTypeCommand, 0)
	}
}

func benchmarkWindowSlidingAccuracy(b *testing.B) {
	// Test sliding window accuracy
	config := Config{
		CommandLimit: RateLimit{Requests: 5, Window: time.Second}, // 5 requests per second
		PremiumMultipliers: map[int]float64{0: 1.0},
	}

	limiter := NewMemoryRateLimiter(config, getTestMetricsCollector())
	ctx := context.Background()
	userID := int64(12345)

	b.ResetTimer()
	b.N = 1 // Single test for accuracy

	// Test sliding window behavior
	results := make([]bool, 0, 15)

	// Make 5 requests quickly (should all be allowed)
	for i := 0; i < 5; i++ {
		allowed, _ := limiter.CheckLimit(ctx, userID, LimitTypeCommand, 0)
		results = append(results, allowed)
	}

	// 6th request should be denied
	allowed, _ := limiter.CheckLimit(ctx, userID, LimitTypeCommand, 0)
	results = append(results, allowed)

	// Wait for window to slide, then make more requests
	time.Sleep(500 * time.Millisecond)

	for i := 0; i < 3; i++ {
		allowed, _ := limiter.CheckLimit(ctx, userID, LimitTypeCommand, 0)
		results = append(results, allowed)
	}

	// Wait for full window to expire
	time.Sleep(600 * time.Millisecond)

	// Should be able to make 5 more requests
	for i := 0; i < 5; i++ {
		allowed, _ := limiter.CheckLimit(ctx, userID, LimitTypeCommand, 0)
		results = append(results, allowed)
	}

	b.Logf("Sliding window test results: %v", results)
}

// Additional specialized benchmarks

func BenchmarkConcurrentUsers_ScaleTest(b *testing.B) {
	userCounts := []int{10, 100, 1000, 10000}

	for _, userCount := range userCounts {
		b.Run(fmt.Sprintf("Users_%d", userCount), func(b *testing.B) {
			limiter := createTestLimiterBenchmark()
			ctx := context.Background()

			b.ResetTimer()
			b.ReportAllocs()

			var wg sync.WaitGroup
			
			b.RunParallel(func(pb *testing.PB) {
				var counter int
				for pb.Next() {
					wg.Add(1)
					go func(id int) {
						defer wg.Done()
						userID := int64(id % userCount)
						_, _ = limiter.CheckLimit(ctx, userID, LimitTypeCommand, 0)
					}(counter)
					counter++
				}
			})
			
			wg.Wait()
		})
	}
}

func BenchmarkLimitType_Performance(b *testing.B) {
	limiter := createTestLimiterBenchmark()
	ctx := context.Background()
	userID := int64(12345)

	limitTypes := []LimitType{
		LimitTypeCommand,
		LimitTypeGitHubREST,
		LimitTypeGitHubQL,
	}

	for _, limitType := range limitTypes {
		b.Run(string(limitType), func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, _ = limiter.CheckLimit(ctx, userID, limitType, 0)
			}
		})
	}
}

func BenchmarkMemoryEfficiency(b *testing.B) {
	limiter := createTestLimiterBenchmark()
	ctx := context.Background()

	// Test memory efficiency by creating many windows
	b.ResetTimer()
	b.ReportAllocs()

	// Create many windows and test memory usage
	for i := 0; i < b.N; i++ {
		userID := int64(i % 10000)
		_, _ = limiter.CheckLimit(ctx, userID, LimitTypeCommand, 0)
		_, _ = limiter.CheckLimit(ctx, userID, LimitTypeGitHubREST, 0)
		_, _ = limiter.CheckLimit(ctx, userID, LimitTypeGitHubQL, 0)
	}
}

// Helper function to create test limiter
func createTestLimiterBenchmark() RateLimiterInterface {
	config := Config{
		CommandLimit:    RateLimit{Requests: 30, Window: time.Minute},
		GitHubRESTLimit: RateLimit{Requests: 60, Window: time.Hour},
		GitHubQLLimit:   RateLimit{Requests: 100, Window: time.Hour},
		PremiumMultipliers: map[int]float64{
			0: 1.0,  // Free
			1: 2.0,  // Coffee
			2: 4.0,  // Cake
			3: 10.0, // Sponsor
		},
	}

	return NewMemoryRateLimiter(config, getTestMetricsCollector())
}