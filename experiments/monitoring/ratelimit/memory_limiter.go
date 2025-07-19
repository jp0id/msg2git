package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/msg2git/msg2git/experiments/monitoring/metrics"
)

// MemoryRateLimiter provides in-memory rate limiting without Redis dependency
type MemoryRateLimiter struct {
	metrics *metrics.MetricsCollector
	limits  map[LimitType]RateLimit
	
	// Premium multipliers
	premiumMultipliers map[int]float64
	
	// In-memory storage
	mu      sync.RWMutex
	windows map[string]*slidingWindow // key format: "limitType:userID"
	
	// Cleanup
	cleanupTicker *time.Ticker
	stopCleanup   chan struct{}
	cleanupCtx    context.Context
	cleanupCancel context.CancelFunc
}

// slidingWindow represents a sliding window for rate limiting
type slidingWindow struct {
	requests []time.Time
	mu       sync.RWMutex
}

// NewMemoryRateLimiter creates a Redis-free rate limiter
func NewMemoryRateLimiter(config Config, metricsCollector *metrics.MetricsCollector) *MemoryRateLimiter {
	// Set default premium multipliers if not provided
	if config.PremiumMultipliers == nil {
		config.PremiumMultipliers = map[int]float64{
			0: 1.0, // Free tier
			1: 2.0, // Coffee tier
			2: 4.0, // Cake tier
			3: 10.0, // Sponsor tier
		}
	}
	
	limiter := &MemoryRateLimiter{
		metrics: metricsCollector,
		limits: map[LimitType]RateLimit{
			LimitTypeCommand:    config.CommandLimit,
			LimitTypeGitHubREST: config.GitHubRESTLimit,
			LimitTypeGitHubQL:   config.GitHubQLLimit,
			LimitTypeGlobal:     config.GlobalLimit,
		},
		premiumMultipliers: config.PremiumMultipliers,
		windows:           make(map[string]*slidingWindow),
		stopCleanup:       make(chan struct{}),
	}
	
	// Start cleanup goroutine with context
	limiter.cleanupCtx, limiter.cleanupCancel = context.WithCancel(context.Background())
	limiter.cleanupTicker = time.NewTicker(5 * time.Minute)
	go limiter.cleanupWorker()
	
	return limiter
}

// CheckLimit checks if a user is within their rate limit
func (rl *MemoryRateLimiter) CheckLimit(ctx context.Context, userID int64, limitType LimitType, premiumLevel int) (bool, error) {
	limit, exists := rl.limits[limitType]
	if !exists {
		return false, fmt.Errorf("unknown limit type: %s", limitType)
	}
	
	// Apply premium multiplier
	multiplier := rl.premiumMultipliers[premiumLevel]
	if multiplier == 0 {
		multiplier = 1.0 // Default to free tier
	}
	
	adjustedLimit := int(float64(limit.Requests) * multiplier)
	
	// Get or create sliding window
	key := fmt.Sprintf("%s:%d", limitType, userID)
	window := rl.getOrCreateWindow(key)
	
	// Clean old requests and count current ones
	now := time.Now()
	windowStart := now.Add(-limit.Window)
	
	window.mu.Lock()
	defer window.mu.Unlock()
	
	// Remove expired requests efficiently
	// Since requests are added chronologically, expired ones should be at the start
	// Find first request that's still valid (within current window)
	firstValidIndex := -1
	for i, reqTime := range window.requests {
		if reqTime.After(windowStart) {
			firstValidIndex = i
			break
		}
	}
	
	if firstValidIndex == -1 {
		// All requests are expired
		window.requests = window.requests[:0] // Keep capacity, clear length
	} else if firstValidIndex > 0 {
		// Some requests expired, slice from first valid one
		window.requests = window.requests[firstValidIndex:]
	}
	// If firstValidIndex == 0, no requests expired, no action needed
	
	allowed := len(window.requests) < adjustedLimit
	
	// Record metrics
	rl.metrics.RecordRateLimitCheck(userID, string(limitType), allowed)
	
	if !allowed {
		rl.metrics.RecordRateLimitViolation(userID, string(limitType))
	}
	
	return allowed, nil
}

// ConsumeLimit consumes one request from the rate limit
func (rl *MemoryRateLimiter) ConsumeLimit(ctx context.Context, userID int64, limitType LimitType, premiumLevel int) error {
	// First check if the request is allowed
	allowed, err := rl.CheckLimit(ctx, userID, limitType, premiumLevel)
	if err != nil {
		return err
	}
	
	if !allowed {
		return fmt.Errorf("rate limit exceeded for user %d, limit type %s", userID, limitType)
	}
	
	// Add current request to the sliding window
	key := fmt.Sprintf("%s:%d", limitType, userID)
	window := rl.getOrCreateWindow(key)
	
	window.mu.Lock()
	window.requests = append(window.requests, time.Now())
	window.mu.Unlock()
	
	return nil
}

// GetCurrentUsage returns the current usage for a user and limit type
func (rl *MemoryRateLimiter) GetCurrentUsage(ctx context.Context, userID int64, limitType LimitType) (int, error) {
	limit, exists := rl.limits[limitType]
	if !exists {
		return 0, fmt.Errorf("unknown limit type: %s", limitType)
	}
	
	key := fmt.Sprintf("%s:%d", limitType, userID)
	window := rl.getOrCreateWindow(key)
	
	window.mu.RLock()
	defer window.mu.RUnlock()
	
	// Count requests within the window
	now := time.Now()
	windowStart := now.Add(-limit.Window)
	
	// Find first valid request, then count from there
	count := 0
	for i, reqTime := range window.requests {
		if reqTime.After(windowStart) {
			// Found first valid request, count remaining from this index
			count = len(window.requests) - i
			break
		}
	}
	
	return count, nil
}

// GetRemainingRequests returns the number of remaining requests for a user
func (rl *MemoryRateLimiter) GetRemainingRequests(ctx context.Context, userID int64, limitType LimitType, premiumLevel int) (int, error) {
	limit, exists := rl.limits[limitType]
	if !exists {
		return 0, fmt.Errorf("unknown limit type: %s", limitType)
	}
	
	// Apply premium multiplier
	multiplier := rl.premiumMultipliers[premiumLevel]
	if multiplier == 0 {
		multiplier = 1.0
	}
	
	adjustedLimit := int(float64(limit.Requests) * multiplier)
	
	currentUsage, err := rl.GetCurrentUsage(ctx, userID, limitType)
	if err != nil {
		return 0, err
	}
	
	remaining := adjustedLimit - currentUsage
	if remaining < 0 {
		remaining = 0
	}
	
	return remaining, nil
}

// GetResetTime returns when the rate limit will reset for a user
func (rl *MemoryRateLimiter) GetResetTime(ctx context.Context, userID int64, limitType LimitType) (time.Time, error) {
	limit, exists := rl.limits[limitType]
	if !exists {
		return time.Time{}, fmt.Errorf("unknown limit type: %s", limitType)
	}
	
	key := fmt.Sprintf("%s:%d", limitType, userID)
	window := rl.getOrCreateWindow(key)
	
	window.mu.RLock()
	defer window.mu.RUnlock()
	
	if len(window.requests) == 0 {
		// No requests in window, reset time is now
		return time.Now(), nil
	}
	
	// Reset time is when the oldest request expires
	oldestRequest := window.requests[0]
	for _, reqTime := range window.requests {
		if reqTime.Before(oldestRequest) {
			oldestRequest = reqTime
		}
	}
	
	resetTime := oldestRequest.Add(limit.Window)
	return resetTime, nil
}

// ResetUserLimits resets all rate limits for a user
func (rl *MemoryRateLimiter) ResetUserLimits(ctx context.Context, userID int64) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	// Remove all windows for this user
	for key := range rl.windows {
		if fmt.Sprintf(":%d", userID) == key[len(key)-len(fmt.Sprintf(":%d", userID)):] {
			delete(rl.windows, key)
		}
	}
	
	return nil
}

// GetGlobalSystemLoad returns the current global system load factor (0-1)
func (rl *MemoryRateLimiter) GetGlobalSystemLoad(ctx context.Context) (float64, error) {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	
	// Count total requests across all users in the last minute
	totalRequests := 0
	cutoff := time.Now().Add(-time.Minute)
	
	for _, window := range rl.windows {
		window.mu.RLock()
		for _, reqTime := range window.requests {
			if reqTime.After(cutoff) {
				totalRequests++
			}
		}
		window.mu.RUnlock()
	}
	
	// Calculate load factor based on some reasonable maximum
	maxRequestsPerMinute := 10000 // Adjust based on your system
	loadFactor := float64(totalRequests) / float64(maxRequestsPerMinute)
	
	if loadFactor > 1.0 {
		loadFactor = 1.0
	}
	
	// Update Prometheus metric
	rl.metrics.UpdateSystemLoadFactor(loadFactor)
	
	return loadFactor, nil
}

// getOrCreateWindow gets or creates a sliding window for a key
func (rl *MemoryRateLimiter) getOrCreateWindow(key string) *slidingWindow {
	rl.mu.RLock()
	if window, exists := rl.windows[key]; exists {
		rl.mu.RUnlock()
		return window
	}
	rl.mu.RUnlock()
	
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	// Double-check after acquiring write lock
	if window, exists := rl.windows[key]; exists {
		return window
	}
	
	// Create new window
	window := &slidingWindow{
		requests: make([]time.Time, 0),
	}
	rl.windows[key] = window
	
	return window
}

// cleanupWorker periodically cleans up old data
func (rl *MemoryRateLimiter) cleanupWorker() {
	defer func() {
		if r := recover(); r != nil {
			// Log panic and restart worker
			go rl.cleanupWorker()
		}
	}()

	for {
		select {
		case <-rl.cleanupTicker.C:
			rl.cleanup()
		case <-rl.stopCleanup:
			return
		case <-rl.cleanupCtx.Done():
			return
		}
	}
}

// cleanup removes old data from memory
func (rl *MemoryRateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	// Calculate optimal cleanup cutoff based on maximum window duration
	maxWindow := time.Hour // Default to 1 hour for GitHub API limits
	for _, limit := range rl.limits {
		if limit.Window > maxWindow {
			maxWindow = limit.Window
		}
	}
	// Keep data for 2x the maximum window duration as safety buffer
	cutoff := time.Now().Add(-2 * maxWindow)
	
	for key, window := range rl.windows {
		window.mu.Lock()
		
		// Find first request that's still valid (after cutoff)
		// Since requests are chronologically ordered, we can slice from that point
		firstValidIndex := -1
		for i, reqTime := range window.requests {
			if reqTime.After(cutoff) {
				firstValidIndex = i
				break
			}
		}
		
		if firstValidIndex == -1 {
			// No valid requests found - remove empty window
			window.requests = nil // Clear slice to free memory
			window.mu.Unlock()
			delete(rl.windows, key)
		} else {
			// Efficiently slice from first valid request to end
			window.requests = window.requests[firstValidIndex:]
			window.mu.Unlock()
		}
	}
}

// Close stops the cleanup worker gracefully
func (rl *MemoryRateLimiter) Close() error {
	// Cancel context first
	if rl.cleanupCancel != nil {
		rl.cleanupCancel()
	}
	
	// Stop ticker
	if rl.cleanupTicker != nil {
		rl.cleanupTicker.Stop()
	}
	
	// Close channel as backup
	select {
	case <-rl.stopCleanup:
		// Already closed
	default:
		close(rl.stopCleanup)
	}
	
	return nil
}

// GetMemoryStats returns memory usage statistics
func (rl *MemoryRateLimiter) GetMemoryStats() map[string]interface{} {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	
	totalWindows := len(rl.windows)
	totalRequests := 0
	
	for _, window := range rl.windows {
		window.mu.RLock()
		totalRequests += len(window.requests)
		window.mu.RUnlock()
	}
	
	return map[string]interface{}{
		"total_windows":  totalWindows,
		"total_requests": totalRequests,
		"memory_usage":   fmt.Sprintf("~%d KB", (totalWindows*50 + totalRequests*20)/1024), // Rough estimate
	}
}