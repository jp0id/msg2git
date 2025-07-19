//go:build experiments

package github_monitor

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/msg2git/msg2git/experiments/monitoring/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestMonitor() (*GitHubAPIMonitor, *metrics.MetricsCollector) {
	metricsCollector := metrics.NewMetricsCollector()
	config := Config{
		WarningThreshold:  0.8,
		CriticalThreshold: 0.9,
		MaxHistorySize:    50,
	}
	
	monitor := NewGitHubAPIMonitor(config, metricsCollector)
	return monitor, metricsCollector
}

func TestNewGitHubAPIMonitor(t *testing.T) {
	metricsCollector := metrics.NewMetricsCollector()
	
	// Test with empty config (should use defaults)
	monitor := NewGitHubAPIMonitor(Config{}, metricsCollector)
	
	assert.NotNil(t, monitor)
	assert.Equal(t, 0.8, monitor.warningThreshold)
	assert.Equal(t, 0.9, monitor.criticalThreshold)
	assert.Equal(t, 100, monitor.maxHistorySize)
	assert.NotNil(t, monitor.rateLimits)
	assert.NotNil(t, monitor.requestHistory)
}

func TestNewGitHubAPIMonitor_WithConfig(t *testing.T) {
	metricsCollector := metrics.NewMetricsCollector()
	config := Config{
		WarningThreshold:  0.7,
		CriticalThreshold: 0.85,
		MaxHistorySize:    25,
	}
	
	monitor := NewGitHubAPIMonitor(config, metricsCollector)
	
	assert.Equal(t, 0.7, monitor.warningThreshold)
	assert.Equal(t, 0.85, monitor.criticalThreshold)
	assert.Equal(t, 25, monitor.maxHistorySize)
}

func TestAPIType_Constants(t *testing.T) {
	assert.Equal(t, APIType("REST"), APITypeREST)
	assert.Equal(t, APIType("GraphQL"), APITypeGraphQL)
}

func TestRateLimitInfo_Structure(t *testing.T) {
	resetTime := time.Now()
	info := &RateLimitInfo{
		Limit:     5000,
		Remaining: 4500,
		ResetTime: resetTime,
		Used:      500,
	}
	
	assert.Equal(t, 5000, info.Limit)
	assert.Equal(t, 4500, info.Remaining)
	assert.Equal(t, resetTime, info.ResetTime)
	assert.Equal(t, 500, info.Used)
}

func TestTrackRequest_Success(t *testing.T) {
	monitor, metricsCollector := createTestMonitor()
	
	userID := int64(12345)
	startTime := time.Now().Add(-100 * time.Millisecond)
	
	// Create a mock response with rate limit headers
	resp := &http.Response{
		StatusCode: 200,
		Header: http.Header{
			"X-RateLimit-Limit":     []string{"5000"},
			"X-RateLimit-Remaining": []string{"4500"},
			"X-RateLimit-Reset":     []string{fmt.Sprintf("%d", time.Now().Add(time.Hour).Unix())},
			"X-RateLimit-Used":      []string{"500"},
		},
	}
	
	// Track the request
	monitor.TrackRequest(userID, APITypeREST, "/repos/owner/repo", startTime, resp, nil)
	
	// Verify rate limit info was stored
	info := monitor.GetRateLimitInfo(userID, APITypeREST)
	require.NotNil(t, info)
	assert.Equal(t, 5000, info.Limit)
	assert.Equal(t, 4500, info.Remaining)
	assert.Equal(t, 500, info.Used)
	
	// Verify request was added to history
	monitor.mu.RLock()
	history := monitor.requestHistory[userID][APITypeREST]
	monitor.mu.RUnlock()
	
	assert.Len(t, history, 1)
	assert.True(t, history[0].Equal(startTime) || history[0].After(startTime.Add(-time.Millisecond)))
}

func TestTrackRequest_Error(t *testing.T) {
	monitor, _ := createTestMonitor()
	
	userID := int64(12345)
	startTime := time.Now()
	
	// Track request with error
	monitor.TrackRequest(userID, APITypeREST, "/repos/owner/repo", startTime, nil, fmt.Errorf("network error"))
	
	// Verify request was added to history even with error
	monitor.mu.RLock()
	history := monitor.requestHistory[userID][APITypeREST]
	monitor.mu.RUnlock()
	
	assert.Len(t, history, 1)
}

func TestParseRESTRateLimit(t *testing.T) {
	monitor, _ := createTestMonitor()
	
	tests := []struct {
		name     string
		headers  http.Header
		expected *RateLimitInfo
	}{
		{
			name: "Valid headers",
			headers: http.Header{
				"X-RateLimit-Limit":     []string{"5000"},
				"X-RateLimit-Remaining": []string{"4500"},
				"X-RateLimit-Reset":     []string{"1640995200"}, // 2022-01-01 00:00:00 UTC
				"X-RateLimit-Used":      []string{"500"},
			},
			expected: &RateLimitInfo{
				Limit:     5000,
				Remaining: 4500,
				ResetTime: time.Unix(1640995200, 0),
				Used:      500,
			},
		},
		{
			name: "Missing headers",
			headers: http.Header{
				"X-RateLimit-Limit": []string{"5000"},
				// Missing other headers
			},
			expected: nil,
		},
		{
			name: "Invalid numbers",
			headers: http.Header{
				"X-RateLimit-Limit":     []string{"invalid"},
				"X-RateLimit-Remaining": []string{"4500"},
				"X-RateLimit-Reset":     []string{"1640995200"},
			},
			expected: nil,
		},
		{
			name: "No used header (should default to 0)",
			headers: http.Header{
				"X-RateLimit-Limit":     []string{"5000"},
				"X-RateLimit-Remaining": []string{"4500"},
				"X-RateLimit-Reset":     []string{"1640995200"},
			},
			expected: &RateLimitInfo{
				Limit:     5000,
				Remaining: 4500,
				ResetTime: time.Unix(1640995200, 0),
				Used:      0,
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{Header: tt.headers}
			result := monitor.parseRESTRateLimit(resp)
			
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.expected.Limit, result.Limit)
				assert.Equal(t, tt.expected.Remaining, result.Remaining)
				assert.Equal(t, tt.expected.ResetTime, result.ResetTime)
				assert.Equal(t, tt.expected.Used, result.Used)
			}
		})
	}
}

func TestParseGraphQLRateLimit(t *testing.T) {
	monitor, _ := createTestMonitor()
	
	resp := &http.Response{
		Header: http.Header{
			"X-RateLimit-Limit":     []string{"5000"},
			"X-RateLimit-Remaining": []string{"4800"},
			"X-RateLimit-Reset":     []string{"1640995200"},
			"X-RateLimit-Cost":      []string{"10"},
		},
	}
	
	result := monitor.parseGraphQLRateLimit(resp)
	require.NotNil(t, result)
	
	assert.Equal(t, 5000, result.Limit)
	assert.Equal(t, 4800, result.Remaining)
	assert.Equal(t, time.Unix(1640995200, 0), result.ResetTime)
	assert.Equal(t, 10, result.Used) // For GraphQL, Used represents cost
}

func TestIsApproachingLimit(t *testing.T) {
	monitor, _ := createTestMonitor()
	userID := int64(12345)
	
	// Set up rate limit info
	monitor.mu.Lock()
	monitor.rateLimits[userID] = map[APIType]*RateLimitInfo{
		APITypeREST: {
			Limit:     5000,
			Remaining: 1000, // 80% used (4000/5000)
			ResetTime: time.Now().Add(time.Hour),
			Used:      4000,
		},
	}
	monitor.mu.Unlock()
	
	// Test various thresholds
	assert.True(t, monitor.IsApproachingLimit(userID, APITypeREST, 0.7))  // 70% threshold
	assert.True(t, monitor.IsApproachingLimit(userID, APITypeREST, 0.8))  // 80% threshold (exact)
	assert.False(t, monitor.IsApproachingLimit(userID, APITypeREST, 0.9)) // 90% threshold
	
	// Test with non-existent user
	assert.False(t, monitor.IsApproachingLimit(99999, APITypeREST, 0.5))
}

func TestIsAtWarningAndCriticalThreshold(t *testing.T) {
	monitor, _ := createTestMonitor()
	userID := int64(12345)
	
	// Set up rate limit info at 85% usage (between warning 80% and critical 90%)
	monitor.mu.Lock()
	monitor.rateLimits[userID] = map[APIType]*RateLimitInfo{
		APITypeREST: {
			Limit:     5000,
			Remaining: 750, // 85% used (4250/5000)
			ResetTime: time.Now().Add(time.Hour),
		},
	}
	monitor.mu.Unlock()
	
	assert.True(t, monitor.IsAtWarningThreshold(userID, APITypeREST))   // 85% > 80%
	assert.False(t, monitor.IsAtCriticalThreshold(userID, APITypeREST)) // 85% < 90%
	
	// Update to 95% usage (above critical)
	monitor.mu.Lock()
	monitor.rateLimits[userID][APITypeREST].Remaining = 250 // 95% used (4750/5000)
	monitor.mu.Unlock()
	
	assert.True(t, monitor.IsAtWarningThreshold(userID, APITypeREST))  // 95% > 80%
	assert.True(t, monitor.IsAtCriticalThreshold(userID, APITypeREST)) // 95% > 90%
}

func TestEstimateTimeToLimit(t *testing.T) {
	monitor, _ := createTestMonitor()
	userID := int64(12345)
	resetTime := time.Now().Add(time.Hour)
	
	// Set up rate limit info
	monitor.mu.Lock()
	monitor.rateLimits[userID] = map[APIType]*RateLimitInfo{
		APITypeREST: {
			Limit:     5000,
			Remaining: 1000,
			ResetTime: resetTime,
		},
	}
	
	// Add some request history to simulate usage rate
	now := time.Now()
	monitor.requestHistory[userID] = map[APIType][]time.Time{
		APITypeREST: {
			now.Add(-30 * time.Minute), // 2 requests in the last hour
			now.Add(-15 * time.Minute),
		},
	}
	monitor.mu.Unlock()
	
	estimate := monitor.EstimateTimeToLimit(userID, APITypeREST)
	
	// Should return a reasonable estimate
	assert.Greater(t, estimate, time.Duration(0))
	assert.LessOrEqual(t, estimate, time.Until(resetTime))
}

func TestEstimateTimeToLimit_NoRemaining(t *testing.T) {
	monitor, _ := createTestMonitor()
	userID := int64(12345)
	
	// Set up rate limit info with no remaining requests
	monitor.mu.Lock()
	monitor.rateLimits[userID] = map[APIType]*RateLimitInfo{
		APITypeREST: {
			Limit:     5000,
			Remaining: 0,
			ResetTime: time.Now().Add(time.Hour),
		},
	}
	monitor.mu.Unlock()
	
	estimate := monitor.EstimateTimeToLimit(userID, APITypeREST)
	assert.Equal(t, time.Duration(0), estimate)
}

func TestGetRecentRequestRate(t *testing.T) {
	monitor, _ := createTestMonitor()
	userID := int64(12345)
	
	// Add request history
	now := time.Now()
	monitor.mu.Lock()
	monitor.requestHistory[userID] = map[APIType][]time.Time{
		APITypeREST: {
			now.Add(-45 * time.Minute), // 3 requests in last hour
			now.Add(-30 * time.Minute),
			now.Add(-15 * time.Minute),
			now.Add(-2 * time.Hour),    // This one is outside the hour window
		},
	}
	monitor.mu.Unlock()
	
	rate := monitor.getRecentRequestRate(userID, APITypeREST, time.Hour)
	
	// Should be 3 requests per hour = 3/3600 requests per second
	expectedRate := 3.0 / 3600.0
	assert.InDelta(t, expectedRate, rate, 0.001)
}

func TestShouldQueueRequest(t *testing.T) {
	monitor, _ := createTestMonitor()
	userID := int64(12345)
	
	// Test case 1: At critical threshold - should queue
	monitor.mu.Lock()
	monitor.rateLimits[userID] = map[APIType]*RateLimitInfo{
		APITypeREST: {
			Limit:     5000,
			Remaining: 250, // 95% used - above critical threshold
			ResetTime: time.Now().Add(time.Hour),
		},
	}
	monitor.mu.Unlock()
	
	assert.True(t, monitor.ShouldQueueRequest(userID, APITypeREST))
	
	// Test case 2: Below critical threshold - should not queue
	monitor.mu.Lock()
	monitor.rateLimits[userID][APITypeREST].Remaining = 1000 // 80% used - at warning but below critical
	monitor.mu.Unlock()
	
	assert.False(t, monitor.ShouldQueueRequest(userID, APITypeREST))
}

func TestGetOptimalDelayForRequest(t *testing.T) {
	monitor, _ := createTestMonitor()
	userID := int64(12345)
	
	// Test case 1: No rate limit info - no delay
	delay := monitor.GetOptimalDelayForRequest(userID, APITypeREST)
	assert.Equal(t, time.Duration(0), delay)
	
	// Test case 2: Below warning threshold - no delay
	monitor.mu.Lock()
	monitor.rateLimits[userID] = map[APIType]*RateLimitInfo{
		APITypeREST: {
			Limit:     5000,
			Remaining: 1500, // 70% used - below warning
			ResetTime: time.Now().Add(time.Hour),
		},
	}
	monitor.mu.Unlock()
	
	delay = monitor.GetOptimalDelayForRequest(userID, APITypeREST)
	assert.Equal(t, time.Duration(0), delay)
	
	// Test case 3: At warning threshold - small delay
	monitor.mu.Lock()
	monitor.rateLimits[userID][APITypeREST].Remaining = 1000 // 80% used - at warning
	monitor.mu.Unlock()
	
	delay = monitor.GetOptimalDelayForRequest(userID, APITypeREST)
	assert.Equal(t, 10*time.Second, delay)
	
	// Test case 4: At critical threshold - moderate delay
	monitor.mu.Lock()
	monitor.rateLimits[userID][APITypeREST].Remaining = 250 // 95% used - at critical
	monitor.mu.Unlock()
	
	delay = monitor.GetOptimalDelayForRequest(userID, APITypeREST)
	assert.Equal(t, 30*time.Second, delay)
}

func TestGetUserAPIStats(t *testing.T) {
	monitor, _ := createTestMonitor()
	userID := int64(12345)
	
	// Set up rate limit info for both API types
	resetTime := time.Now().Add(time.Hour)
	monitor.mu.Lock()
	monitor.rateLimits[userID] = map[APIType]*RateLimitInfo{
		APITypeREST: {
			Limit:     5000,
			Remaining: 4000,
			ResetTime: resetTime,
			Used:      1000,
		},
		APITypeGraphQL: {
			Limit:     5000,
			Remaining: 4500,
			ResetTime: resetTime,
			Used:      500,
		},
	}
	monitor.mu.Unlock()
	
	stats := monitor.GetUserAPIStats(userID)
	
	require.Len(t, stats, 2)
	
	// Verify REST stats
	restStats := stats[APITypeREST]
	require.NotNil(t, restStats)
	assert.Equal(t, 5000, restStats.Limit)
	assert.Equal(t, 4000, restStats.Remaining)
	assert.Equal(t, 1000, restStats.Used)
	
	// Verify GraphQL stats
	graphqlStats := stats[APITypeGraphQL]
	require.NotNil(t, graphqlStats)
	assert.Equal(t, 5000, graphqlStats.Limit)
	assert.Equal(t, 4500, graphqlStats.Remaining)
	assert.Equal(t, 500, graphqlStats.Used)
	
	// Verify it's a copy (modifying returned stats shouldn't affect internal state)
	restStats.Remaining = 0
	originalInfo := monitor.GetRateLimitInfo(userID, APITypeREST)
	assert.Equal(t, 4000, originalInfo.Remaining) // Should be unchanged
}

func TestAddToRequestHistory_MaxSize(t *testing.T) {
	monitor, _ := createTestMonitor()
	monitor.maxHistorySize = 3 // Set small max size for testing
	
	userID := int64(12345)
	
	// Add more requests than max size
	for i := 0; i < 5; i++ {
		monitor.addToRequestHistory(userID, APITypeREST, time.Now().Add(time.Duration(i)*time.Minute))
	}
	
	// Should only keep the most recent 3 requests
	monitor.mu.RLock()
	history := monitor.requestHistory[userID][APITypeREST]
	monitor.mu.RUnlock()
	
	assert.Len(t, history, 3)
}

func TestCleanup(t *testing.T) {
	monitor, _ := createTestMonitor()
	userID := int64(12345)
	
	// Add old request history
	oldTime := time.Now().Add(-25 * time.Hour) // Older than 24 hours
	recentTime := time.Now().Add(-1 * time.Hour)
	
	monitor.mu.Lock()
	monitor.requestHistory[userID] = map[APIType][]time.Time{
		APITypeREST: {oldTime, recentTime},
	}
	
	// Add old rate limit info
	monitor.rateLimits[userID] = map[APIType]*RateLimitInfo{
		APITypeREST: {
			Limit:     5000,
			Remaining: 4000,
			ResetTime: time.Now().Add(-2 * time.Hour), // Reset time in the past
		},
	}
	monitor.mu.Unlock()
	
	// Run cleanup
	monitor.Cleanup()
	
	monitor.mu.RLock()
	defer monitor.mu.RUnlock()
	
	// Old request should be removed, recent one should remain
	history := monitor.requestHistory[userID][APITypeREST]
	assert.Len(t, history, 1)
	assert.True(t, history[0].Equal(recentTime) || history[0].After(recentTime.Add(-time.Second)))
	
	// Old rate limit info should be removed
	_, exists := monitor.rateLimits[userID]
	assert.False(t, exists)
}

func TestGetGlobalAPIStats(t *testing.T) {
	monitor, _ := createTestMonitor()
	
	// Set up multiple users with different usage patterns
	users := []struct {
		userID   int64
		apiType  APIType
		usage    float64 // percentage
	}{
		{12345, APITypeREST, 0.7},    // Below warning
		{12346, APITypeREST, 0.85},   // At warning
		{12347, APITypeREST, 0.95},   // At critical
		{12348, APITypeGraphQL, 0.6}, // Below warning
		{12349, APITypeGraphQL, 0.92}, // At critical
	}
	
	resetTime := time.Now().Add(time.Hour)
	monitor.mu.Lock()
	for _, user := range users {
		if monitor.rateLimits[user.userID] == nil {
			monitor.rateLimits[user.userID] = make(map[APIType]*RateLimitInfo)
		}
		
		remaining := int(5000 * (1.0 - user.usage))
		monitor.rateLimits[user.userID][user.apiType] = &RateLimitInfo{
			Limit:     5000,
			Remaining: remaining,
			ResetTime: resetTime,
		}
	}
	monitor.mu.Unlock()
	
	stats := monitor.GetGlobalAPIStats()
	
	// Verify REST stats
	restStats := stats[APITypeREST]
	assert.Equal(t, 3, restStats.TotalUsers)
	assert.Equal(t, 1, restStats.UsersAtWarning)  // 0.85 is between 0.8 and 0.9
	assert.Equal(t, 1, restStats.UsersAtCritical) // 0.95 is above 0.9
	
	// Verify GraphQL stats
	graphqlStats := stats[APITypeGraphQL]
	assert.Equal(t, 2, graphqlStats.TotalUsers)
	assert.Equal(t, 0, graphqlStats.UsersAtWarning)  // 0.6 is below 0.8, 0.92 is above 0.9
	assert.Equal(t, 1, graphqlStats.UsersAtCritical) // 0.92 is above 0.9
}

func BenchmarkTrackRequest(b *testing.B) {
	monitor, _ := createTestMonitor()
	userID := int64(12345)
	startTime := time.Now()
	
	resp := &http.Response{
		StatusCode: 200,
		Header: http.Header{
			"X-RateLimit-Limit":     []string{"5000"},
			"X-RateLimit-Remaining": []string{"4500"},
			"X-RateLimit-Reset":     []string{fmt.Sprintf("%d", time.Now().Add(time.Hour).Unix())},
		},
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		monitor.TrackRequest(userID, APITypeREST, "/repos/owner/repo", startTime, resp, nil)
	}
}

func BenchmarkIsApproachingLimit(b *testing.B) {
	monitor, _ := createTestMonitor()
	userID := int64(12345)
	
	// Set up rate limit info
	monitor.mu.Lock()
	monitor.rateLimits[userID] = map[APIType]*RateLimitInfo{
		APITypeREST: {
			Limit:     5000,
			Remaining: 1000,
			ResetTime: time.Now().Add(time.Hour),
		},
	}
	monitor.mu.Unlock()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		monitor.IsApproachingLimit(userID, APITypeREST, 0.8)
	}
}

// Test concurrent access to monitor
func TestGitHubAPIMonitor_ConcurrentAccess(t *testing.T) {
	monitor, _ := createTestMonitor()
	
	done := make(chan bool, 10)
	
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()
			
			userID := int64(id)
			startTime := time.Now()
			
			resp := &http.Response{
				StatusCode: 200,
				Header: http.Header{
					"X-RateLimit-Limit":     []string{"5000"},
					"X-RateLimit-Remaining": []string{fmt.Sprintf("%d", 4000+id*10)},
					"X-RateLimit-Reset":     []string{fmt.Sprintf("%d", time.Now().Add(time.Hour).Unix())},
				},
			}
			
			for j := 0; j < 10; j++ {
				monitor.TrackRequest(userID, APITypeREST, "/repos/test/repo", startTime, resp, nil)
				monitor.IsApproachingLimit(userID, APITypeREST, 0.8)
				monitor.GetUserAPIStats(userID)
			}
		}(i)
	}
	
	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
	
	// Verify we have data for all users
	assert.Equal(t, 10, len(monitor.GetGlobalAPIStats()[APITypeREST].TotalUsers))
}