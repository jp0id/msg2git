//go:build experiments

package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestMemoryLimiter() *MemoryRateLimiter {
	config := Config{
		CommandLimit: RateLimit{
			Requests: 10,
			Window:   time.Minute,
		},
		GitHubRESTLimit: RateLimit{
			Requests: 100,
			Window:   time.Hour,
		},
		GitHubQLLimit: RateLimit{
			Requests: 200,
			Window:   time.Hour,
		},
		PremiumMultipliers: map[int]float64{
			0: 1.0,
			1: 2.0,
			2: 4.0,
			3: 10.0,
		},
	}
	
	return NewMemoryRateLimiter(config, getTestMetricsCollector())
}

func TestNewMemoryRateLimiter(t *testing.T) {
	limiter := createTestMemoryLimiter()
	defer limiter.Close()
	
	assert.NotNil(t, limiter)
	assert.NotNil(t, limiter.metrics)
	assert.NotNil(t, limiter.limits)
	assert.NotNil(t, limiter.windows)
	assert.Equal(t, 1.0, limiter.premiumMultipliers[0])
	assert.Equal(t, 10.0, limiter.premiumMultipliers[3])
}

func TestMemoryLimiter_CheckLimit_UnknownType(t *testing.T) {
	limiter := createTestMemoryLimiter()
	defer limiter.Close()
	
	ctx := context.Background()
	allowed, err := limiter.CheckLimit(ctx, 12345, "unknown_limit", 0)
	
	assert.Error(t, err)
	assert.False(t, allowed)
	assert.Contains(t, err.Error(), "unknown limit type")
}

func TestMemoryLimiter_BasicRateLimit(t *testing.T) {
	limiter := createTestMemoryLimiter()
	defer limiter.Close()
	
	ctx := context.Background()
	userID := int64(12345)
	
	// Should allow up to 10 requests per minute for free tier
	for i := 0; i < 10; i++ {
		err := limiter.ConsumeLimit(ctx, userID, LimitTypeCommand, 0)
		assert.NoError(t, err, "Request %d should be allowed", i+1)
	}
	
	// 11th request should be blocked
	err := limiter.ConsumeLimit(ctx, userID, LimitTypeCommand, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit exceeded")
	
	// Check current usage
	usage, err := limiter.GetCurrentUsage(ctx, userID, LimitTypeCommand)
	require.NoError(t, err)
	assert.Equal(t, 10, usage)
	
	// Check remaining requests
	remaining, err := limiter.GetRemainingRequests(ctx, userID, LimitTypeCommand, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, remaining)
}

func TestMemoryLimiter_PremiumMultiplier(t *testing.T) {
	limiter := createTestMemoryLimiter()
	defer limiter.Close()
	
	ctx := context.Background()
	userID := int64(12345)
	
	// Coffee tier (2x multiplier) should allow 20 requests
	for i := 0; i < 20; i++ {
		err := limiter.ConsumeLimit(ctx, userID, LimitTypeCommand, 1) // Coffee tier
		assert.NoError(t, err, "Coffee tier request %d should be allowed", i+1)
	}
	
	// 21st request should be blocked
	err := limiter.ConsumeLimit(ctx, userID, LimitTypeCommand, 1)
	assert.Error(t, err)
	
	// Check remaining with different premium levels
	remaining, err := limiter.GetRemainingRequests(ctx, userID, LimitTypeCommand, 0) // Free tier
	require.NoError(t, err)
	assert.Equal(t, 0, remaining) // Free tier would be -10, capped at 0
	
	remaining, err = limiter.GetRemainingRequests(ctx, userID, LimitTypeCommand, 1) // Coffee tier
	require.NoError(t, err)
	assert.Equal(t, 0, remaining) // At limit
	
	remaining, err = limiter.GetRemainingRequests(ctx, userID, LimitTypeCommand, 2) // Cake tier (4x)
	require.NoError(t, err)
	assert.Equal(t, 20, remaining) // 40 - 20 = 20
}

func TestMemoryLimiter_SlidingWindow(t *testing.T) {
	limiter := createTestMemoryLimiter()
	defer limiter.Close()
	
	ctx := context.Background()
	userID := int64(12345)
	
	// Use up limit
	for i := 0; i < 10; i++ {
		err := limiter.ConsumeLimit(ctx, userID, LimitTypeCommand, 0)
		require.NoError(t, err)
	}
	
	// Should be at limit
	err := limiter.ConsumeLimit(ctx, userID, LimitTypeCommand, 0)
	assert.Error(t, err)
	
	// Manually adjust the oldest request to simulate time passing
	key := "command_rate:12345"
	window := limiter.getOrCreateWindow(key)
	window.mu.Lock()
	if len(window.requests) > 0 {
		window.requests[0] = time.Now().Add(-2 * time.Minute) // Make oldest request expire
	}
	window.mu.Unlock()
	
	// Should now allow one more request
	err = limiter.ConsumeLimit(ctx, userID, LimitTypeCommand, 0)
	assert.NoError(t, err)
}

func TestMemoryLimiter_GetResetTime(t *testing.T) {
	limiter := createTestMemoryLimiter()
	defer limiter.Close()
	
	ctx := context.Background()
	userID := int64(12345)
	
	// No requests yet - reset time should be now
	resetTime, err := limiter.GetResetTime(ctx, userID, LimitTypeCommand)
	require.NoError(t, err)
	assert.True(t, resetTime.Before(time.Now().Add(time.Second)))
	
	// Make a request
	err = limiter.ConsumeLimit(ctx, userID, LimitTypeCommand, 0)
	require.NoError(t, err)
	
	// Reset time should be approximately 1 minute from now
	resetTime, err = limiter.GetResetTime(ctx, userID, LimitTypeCommand)
	require.NoError(t, err)
	
	expectedReset := time.Now().Add(time.Minute)
	assert.True(t, resetTime.After(expectedReset.Add(-5*time.Second)))
	assert.True(t, resetTime.Before(expectedReset.Add(5*time.Second)))
}

func TestMemoryLimiter_ResetUserLimits(t *testing.T) {
	limiter := createTestMemoryLimiter()
	defer limiter.Close()
	
	ctx := context.Background()
	userID := int64(12345)
	otherUserID := int64(67890)
	
	// Add requests for both users
	err := limiter.ConsumeLimit(ctx, userID, LimitTypeCommand, 0)
	require.NoError(t, err)
	err = limiter.ConsumeLimit(ctx, otherUserID, LimitTypeCommand, 0)
	require.NoError(t, err)
	
	// Verify both users have usage
	usage, err := limiter.GetCurrentUsage(ctx, userID, LimitTypeCommand)
	require.NoError(t, err)
	assert.Equal(t, 1, usage)
	
	usage, err = limiter.GetCurrentUsage(ctx, otherUserID, LimitTypeCommand)
	require.NoError(t, err)
	assert.Equal(t, 1, usage)
	
	// Reset first user
	err = limiter.ResetUserLimits(ctx, userID)
	require.NoError(t, err)
	
	// First user should have no usage, other user should still have usage
	usage, err = limiter.GetCurrentUsage(ctx, userID, LimitTypeCommand)
	require.NoError(t, err)
	assert.Equal(t, 0, usage)
	
	usage, err = limiter.GetCurrentUsage(ctx, otherUserID, LimitTypeCommand)
	require.NoError(t, err)
	assert.Equal(t, 1, usage)
}

func TestMemoryLimiter_GetGlobalSystemLoad(t *testing.T) {
	limiter := createTestMemoryLimiter()
	defer limiter.Close()
	
	ctx := context.Background()
	
	// No requests - should have very low load
	load, err := limiter.GetGlobalSystemLoad(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0.0, load)
	
	// Add some requests
	for i := 0; i < 100; i++ {
		userID := int64(i % 10) // 10 different users
		limiter.ConsumeLimit(ctx, userID, LimitTypeCommand, 0)
	}
	
	// Should have some load now
	load, err = limiter.GetGlobalSystemLoad(ctx)
	require.NoError(t, err)
	assert.Greater(t, load, 0.0)
	assert.LessOrEqual(t, load, 1.0)
}

func TestMemoryLimiter_GetMemoryStats(t *testing.T) {
	limiter := createTestMemoryLimiter()
	defer limiter.Close()
	
	ctx := context.Background()
	
	// Initially empty
	stats := limiter.GetMemoryStats()
	assert.Equal(t, 0, stats["total_windows"])
	assert.Equal(t, 0, stats["total_requests"])
	
	// Add some requests
	for i := 0; i < 5; i++ {
		userID := int64(i)
		limiter.ConsumeLimit(ctx, userID, LimitTypeCommand, 0)
		limiter.ConsumeLimit(ctx, userID, LimitTypeGitHubREST, 0)
	}
	
	// Should have windows and requests
	stats = limiter.GetMemoryStats()
	assert.Equal(t, 10, stats["total_windows"])  // 5 users × 2 limit types
	assert.Equal(t, 10, stats["total_requests"]) // 5 users × 2 requests
	assert.Contains(t, stats, "memory_usage")
}

func TestMemoryLimiter_Cleanup(t *testing.T) {
	limiter := createTestMemoryLimiter()
	defer limiter.Close()
	
	ctx := context.Background()
	userID := int64(12345)
	
	// Add a request
	err := limiter.ConsumeLimit(ctx, userID, LimitTypeCommand, 0)
	require.NoError(t, err)
	
	// Verify it exists
	usage, err := limiter.GetCurrentUsage(ctx, userID, LimitTypeCommand)
	require.NoError(t, err)
	assert.Equal(t, 1, usage)
	
	// Manually trigger cleanup (normally this would be periodic)
	limiter.cleanup()
	
	// Recent request should still exist
	usage, err = limiter.GetCurrentUsage(ctx, userID, LimitTypeCommand)
	require.NoError(t, err)
	assert.Equal(t, 1, usage)
	
	// Simulate old request by modifying the timestamp
	key := "command_rate:12345"
	window := limiter.getOrCreateWindow(key)
	window.mu.Lock()
	window.requests[0] = time.Now().Add(-25 * time.Hour) // Very old
	window.mu.Unlock()
	
	// Now cleanup should remove it
	limiter.cleanup()
	
	usage, err = limiter.GetCurrentUsage(ctx, userID, LimitTypeCommand)
	require.NoError(t, err)
	assert.Equal(t, 0, usage)
}

func BenchmarkMemoryLimiter_ConsumeLimit(b *testing.B) {
	limiter := createTestMemoryLimiter()
	defer limiter.Close()
	
	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		userID := int64(i % 1000) // Distribute across 1000 users
		limiter.ConsumeLimit(ctx, userID, LimitTypeCommand, 0)
	}
}

func BenchmarkMemoryLimiter_CheckLimit(b *testing.B) {
	limiter := createTestMemoryLimiter()
	defer limiter.Close()
	
	ctx := context.Background()
	
	// Pre-populate some data
	for i := 0; i < 1000; i++ {
		limiter.ConsumeLimit(ctx, int64(i), LimitTypeCommand, 0)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		userID := int64(i % 1000)
		limiter.CheckLimit(ctx, userID, LimitTypeCommand, 0)
	}
}

// Test concurrent access
func TestMemoryLimiter_ConcurrentAccess(t *testing.T) {
	limiter := createTestMemoryLimiter()
	defer limiter.Close()
	
	ctx := context.Background()
	
	done := make(chan bool, 10)
	
	// Multiple goroutines accessing the limiter concurrently
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()
			
			userID := int64(id)
			for j := 0; j < 5; j++ {
				limiter.ConsumeLimit(ctx, userID, LimitTypeCommand, 0)
				limiter.GetCurrentUsage(ctx, userID, LimitTypeCommand)
				limiter.CheckLimit(ctx, userID, LimitTypeCommand, 0)
			}
		}(i)
	}
	
	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
	
	// Verify final state
	stats := limiter.GetMemoryStats()
	totalWindows := stats["total_windows"].(int)
	assert.Greater(t, totalWindows, 0)
	assert.LessOrEqual(t, totalWindows, 10) // Max 10 users
}