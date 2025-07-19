//go:build experiments

package ratelimit

import (
	"context"
	"testing"
	"time"
)

// Benchmark comparing old vs new cleanup approach
func BenchmarkCleanupOptimization(b *testing.B) {
	limiter := createTestLimiter()
	defer limiter.Close()
	
	ctx := context.Background()
	userID := int64(12345)
	
	// Pre-populate with many requests (mix of old and new)
	now := time.Now()
	window := limiter.getOrCreateWindow("command_rate:12345")
	
	// Add 1000 old requests (will be cleaned up)
	window.mu.Lock()
	for i := 0; i < 1000; i++ {
		window.requests = append(window.requests, now.Add(-time.Hour))
	}
	// Add 100 recent requests (will be kept)
	for i := 0; i < 100; i++ {
		window.requests = append(window.requests, now.Add(-time.Minute))
	}
	window.mu.Unlock()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		// Trigger cleanup through CheckLimit (which calls the optimized cleanup)
		limiter.CheckLimit(ctx, userID, LimitTypeCommand, 0)
	}
}

func BenchmarkCleanupLargeWindow(b *testing.B) {
	limiter := createTestLimiter()
	defer limiter.Close()
	
	ctx := context.Background()
	userID := int64(12345)
	
	// Pre-populate with many requests across different time periods
	now := time.Now()
	window := limiter.getOrCreateWindow("command_rate:12345")
	
	window.mu.Lock()
	// Add 5000 requests spread over time
	for i := 0; i < 5000; i++ {
		// First 4000 are old (will be expired)
		if i < 4000 {
			window.requests = append(window.requests, now.Add(-2*time.Hour))
		} else {
			// Last 1000 are recent (will be kept)
			window.requests = append(window.requests, now.Add(-30*time.Second))
		}
	}
	window.mu.Unlock()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		limiter.CheckLimit(ctx, userID, LimitTypeCommand, 0)
	}
}

// Benchmark the new efficient cleanup vs simulated old approach
func BenchmarkCleanupComparison(b *testing.B) {
	b.Run("OptimizedCleanup", func(b *testing.B) {
		benchmarkCleanupMethod(b, true)
	})
	
	b.Run("SimulatedOldApproach", func(b *testing.B) {
		benchmarkCleanupMethod(b, false)
	})
}

func benchmarkCleanupMethod(b *testing.B, useOptimized bool) {
	requests := make([]time.Time, 10000)
	now := time.Now()
	cutoff := now.Add(-time.Hour)
	
	// Populate with mix of old and new requests
	for i := 0; i < 10000; i++ {
		if i < 7000 {
			// Old requests (will be expired)
			requests[i] = now.Add(-2 * time.Hour)
		} else {
			// New requests (will be kept)
			requests[i] = now.Add(-30 * time.Minute)
		}
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		if useOptimized {
			// Optimized approach: find first valid, slice
			firstValidIndex := -1
			for j, reqTime := range requests {
				if reqTime.After(cutoff) {
					firstValidIndex = j
					break
				}
			}
			if firstValidIndex != -1 {
				_ = requests[firstValidIndex:]
			}
		} else {
			// Old approach: append to new slice
			var validRequests []time.Time
			for _, reqTime := range requests {
				if reqTime.After(cutoff) {
					validRequests = append(validRequests, reqTime)
				}
			}
			_ = validRequests
		}
	}
}

func BenchmarkGetCurrentUsageOptimization(b *testing.B) {
	limiter := createTestLimiter()
	defer limiter.Close()
	
	ctx := context.Background()
	userID := int64(12345)
	
	// Pre-populate with many requests
	window := limiter.getOrCreateWindow("command_rate:12345")
	now := time.Now()
	
	window.mu.Lock()
	// Add 2000 old requests + 500 recent requests
	for i := 0; i < 2000; i++ {
		window.requests = append(window.requests, now.Add(-2*time.Hour))
	}
	for i := 0; i < 500; i++ {
		window.requests = append(window.requests, now.Add(-30*time.Second))
	}
	window.mu.Unlock()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		limiter.GetCurrentUsage(ctx, userID, LimitTypeCommand)
	}
}