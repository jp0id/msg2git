package github_monitor

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/msg2git/msg2git/experiments/monitoring/metrics"
)

// APIType represents the type of GitHub API
type APIType string

const (
	APITypeREST    APIType = "REST"
	APITypeGraphQL APIType = "GraphQL"
)

// RateLimitInfo holds rate limit information from GitHub API headers
type RateLimitInfo struct {
	Limit     int
	Remaining int
	ResetTime time.Time
	Used      int
}

// GitHubAPIMonitor monitors GitHub API usage and rate limits
type GitHubAPIMonitor struct {
	metrics *metrics.MetricsCollector
	
	// Rate limit tracking per user and API type
	mu         sync.RWMutex
	rateLimits map[int64]map[APIType]*RateLimitInfo
	
	// Thresholds for alerts
	warningThreshold  float64 // e.g., 0.8 for 80%
	criticalThreshold float64 // e.g., 0.9 for 90%
	
	// Request history for intelligent queuing
	requestHistory map[int64]map[APIType][]time.Time
	maxHistorySize int
}

// Config holds configuration for the GitHub API monitor
type Config struct {
	WarningThreshold  float64 // Percentage of rate limit that triggers warning (0.0-1.0)
	CriticalThreshold float64 // Percentage of rate limit that triggers critical alert (0.0-1.0)
	MaxHistorySize    int     // Maximum number of recent requests to track per user/API type
}

// NewGitHubAPIMonitor creates a new GitHub API monitor
func NewGitHubAPIMonitor(config Config, metricsCollector *metrics.MetricsCollector) *GitHubAPIMonitor {
	// Set defaults if not provided
	if config.WarningThreshold == 0 {
		config.WarningThreshold = 0.8 // 80%
	}
	if config.CriticalThreshold == 0 {
		config.CriticalThreshold = 0.9 // 90%
	}
	if config.MaxHistorySize == 0 {
		config.MaxHistorySize = 100 // Track last 100 requests per user/type
	}
	
	return &GitHubAPIMonitor{
		metrics:           metricsCollector,
		rateLimits:        make(map[int64]map[APIType]*RateLimitInfo),
		warningThreshold:  config.WarningThreshold,
		criticalThreshold: config.CriticalThreshold,
		requestHistory:    make(map[int64]map[APIType][]time.Time),
		maxHistorySize:    config.MaxHistorySize,
	}
}

// TrackRequest tracks a GitHub API request and updates metrics
func (m *GitHubAPIMonitor) TrackRequest(userID int64, apiType APIType, endpoint string, startTime time.Time, resp *http.Response, err error) {
	duration := time.Since(startTime)
	
	// Determine status
	status := "success"
	if err != nil {
		status = "error"
	} else if resp != nil && resp.StatusCode >= 400 {
		status = "error"
	}
	
	// Record metrics
	m.metrics.RecordGitHubAPIRequest(userID, string(apiType), endpoint, status)
	m.metrics.RecordGitHubAPIRequestDuration(string(apiType), endpoint, status, duration)
	
	// Update rate limit info from response headers
	if resp != nil && status == "success" {
		m.updateRateLimitFromHeaders(userID, apiType, resp)
	}
	
	// Track request in history
	m.addToRequestHistory(userID, apiType, startTime)
}

// updateRateLimitFromHeaders extracts rate limit info from GitHub API response headers
func (m *GitHubAPIMonitor) updateRateLimitFromHeaders(userID int64, apiType APIType, resp *http.Response) {
	var rateLimitInfo *RateLimitInfo
	
	switch apiType {
	case APITypeREST:
		rateLimitInfo = m.parseRESTRateLimit(resp)
	case APITypeGraphQL:
		rateLimitInfo = m.parseGraphQLRateLimit(resp)
	default:
		return
	}
	
	if rateLimitInfo == nil {
		return
	}
	
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.rateLimits[userID] == nil {
		m.rateLimits[userID] = make(map[APIType]*RateLimitInfo)
	}
	
	m.rateLimits[userID][apiType] = rateLimitInfo
	
	// Update Prometheus metrics
	m.metrics.UpdateGitHubRateLimit(userID, string(apiType), rateLimitInfo.Remaining, rateLimitInfo.ResetTime)
}

// parseRESTRateLimit parses REST API rate limit headers
func (m *GitHubAPIMonitor) parseRESTRateLimit(resp *http.Response) *RateLimitInfo {
	limitHeader := resp.Header.Get("X-RateLimit-Limit")
	remainingHeader := resp.Header.Get("X-RateLimit-Remaining")
	resetHeader := resp.Header.Get("X-RateLimit-Reset")
	usedHeader := resp.Header.Get("X-RateLimit-Used")
	
	if limitHeader == "" || remainingHeader == "" || resetHeader == "" {
		return nil
	}
	
	limit, err := strconv.Atoi(limitHeader)
	if err != nil {
		return nil
	}
	
	remaining, err := strconv.Atoi(remainingHeader)
	if err != nil {
		return nil
	}
	
	resetTimestamp, err := strconv.ParseInt(resetHeader, 10, 64)
	if err != nil {
		return nil
	}
	
	used := 0
	if usedHeader != "" {
		used, _ = strconv.Atoi(usedHeader)
	}
	
	return &RateLimitInfo{
		Limit:     limit,
		Remaining: remaining,
		ResetTime: time.Unix(resetTimestamp, 0),
		Used:      used,
	}
}

// parseGraphQLRateLimit parses GraphQL API rate limit headers
func (m *GitHubAPIMonitor) parseGraphQLRateLimit(resp *http.Response) *RateLimitInfo {
	// GraphQL uses different headers
	limitHeader := resp.Header.Get("X-RateLimit-Limit")
	remainingHeader := resp.Header.Get("X-RateLimit-Remaining")
	resetHeader := resp.Header.Get("X-RateLimit-Reset")
	
	// GraphQL also has cost information
	costHeader := resp.Header.Get("X-RateLimit-Cost")
	
	if limitHeader == "" || remainingHeader == "" || resetHeader == "" {
		return nil
	}
	
	limit, err := strconv.Atoi(limitHeader)
	if err != nil {
		return nil
	}
	
	remaining, err := strconv.Atoi(remainingHeader)
	if err != nil {
		return nil
	}
	
	resetTimestamp, err := strconv.ParseInt(resetHeader, 10, 64)
	if err != nil {
		return nil
	}
	
	cost := 1 // Default cost
	if costHeader != "" {
		cost, _ = strconv.Atoi(costHeader)
	}
	
	return &RateLimitInfo{
		Limit:     limit,
		Remaining: remaining,
		ResetTime: time.Unix(resetTimestamp, 0),
		Used:      cost, // For GraphQL, this represents the cost of the request
	}
}

// addToRequestHistory adds a request to the user's request history
func (m *GitHubAPIMonitor) addToRequestHistory(userID int64, apiType APIType, requestTime time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.requestHistory[userID] == nil {
		m.requestHistory[userID] = make(map[APIType][]time.Time)
	}
	
	if m.requestHistory[userID][apiType] == nil {
		m.requestHistory[userID][apiType] = make([]time.Time, 0)
	}
	
	// Add new request
	m.requestHistory[userID][apiType] = append(m.requestHistory[userID][apiType], requestTime)
	
	// Trim history if it exceeds max size
	if len(m.requestHistory[userID][apiType]) > m.maxHistorySize {
		// Keep only the most recent requests
		start := len(m.requestHistory[userID][apiType]) - m.maxHistorySize
		m.requestHistory[userID][apiType] = m.requestHistory[userID][apiType][start:]
	}
}

// GetRateLimitInfo returns the current rate limit info for a user and API type
func (m *GitHubAPIMonitor) GetRateLimitInfo(userID int64, apiType APIType) *RateLimitInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if m.rateLimits[userID] == nil {
		return nil
	}
	
	return m.rateLimits[userID][apiType]
}

// IsApproachingLimit checks if a user is approaching their rate limit
func (m *GitHubAPIMonitor) IsApproachingLimit(userID int64, apiType APIType, threshold float64) bool {
	info := m.GetRateLimitInfo(userID, apiType)
	if info == nil {
		return false
	}
	
	usagePercentage := float64(info.Limit-info.Remaining) / float64(info.Limit)
	return usagePercentage >= threshold
}

// IsAtWarningThreshold checks if a user is at the warning threshold
func (m *GitHubAPIMonitor) IsAtWarningThreshold(userID int64, apiType APIType) bool {
	return m.IsApproachingLimit(userID, apiType, m.warningThreshold)
}

// IsAtCriticalThreshold checks if a user is at the critical threshold
func (m *GitHubAPIMonitor) IsAtCriticalThreshold(userID int64, apiType APIType) bool {
	return m.IsApproachingLimit(userID, apiType, m.criticalThreshold)
}

// EstimateTimeToLimit estimates how long until the user hits their rate limit
func (m *GitHubAPIMonitor) EstimateTimeToLimit(userID int64, apiType APIType) time.Duration {
	info := m.GetRateLimitInfo(userID, apiType)
	if info == nil {
		return time.Hour // Conservative estimate
	}
	
	// If already at limit, return 0
	if info.Remaining <= 0 {
		return 0
	}
	
	// Get recent request rate
	requestRate := m.getRecentRequestRate(userID, apiType, time.Hour)
	if requestRate <= 0 {
		return time.Until(info.ResetTime) // No recent activity, safe until reset
	}
	
	// Estimate time until limit based on current rate
	timeToLimit := time.Duration(float64(info.Remaining)/requestRate) * time.Second
	
	// Cap at reset time
	timeUntilReset := time.Until(info.ResetTime)
	if timeToLimit > timeUntilReset {
		timeToLimit = timeUntilReset
	}
	
	return timeToLimit
}

// getRecentRequestRate calculates the request rate over the specified duration
func (m *GitHubAPIMonitor) getRecentRequestRate(userID int64, apiType APIType, duration time.Duration) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if m.requestHistory[userID] == nil || m.requestHistory[userID][apiType] == nil {
		return 0
	}
	
	history := m.requestHistory[userID][apiType]
	cutoff := time.Now().Add(-duration)
	
	// Count requests after cutoff
	recentRequests := 0
	for _, requestTime := range history {
		if requestTime.After(cutoff) {
			recentRequests++
		}
	}
	
	return float64(recentRequests) / duration.Seconds()
}

// ShouldQueueRequest determines if a request should be queued based on rate limit status
func (m *GitHubAPIMonitor) ShouldQueueRequest(userID int64, apiType APIType) bool {
	// Queue if at critical threshold
	if m.IsAtCriticalThreshold(userID, apiType) {
		return true
	}
	
	// Queue if estimated time to limit is very short
	timeToLimit := m.EstimateTimeToLimit(userID, apiType)
	if timeToLimit < 5*time.Minute {
		return true
	}
	
	return false
}

// GetOptimalDelayForRequest calculates an optimal delay before making a request
func (m *GitHubAPIMonitor) GetOptimalDelayForRequest(userID int64, apiType APIType) time.Duration {
	info := m.GetRateLimitInfo(userID, apiType)
	if info == nil {
		return 0 // No info, proceed immediately
	}
	
	// If not approaching any limits, no delay needed
	if !m.IsAtWarningThreshold(userID, apiType) {
		return 0
	}
	
	// If at critical threshold, suggest waiting
	if m.IsAtCriticalThreshold(userID, apiType) {
		timeToLimit := m.EstimateTimeToLimit(userID, apiType)
		
		// If very close to limit, suggest waiting until reset
		if timeToLimit < 5*time.Minute {
			return time.Until(info.ResetTime)
		}
		
		// Otherwise, suggest a moderate delay
		return 30 * time.Second
	}
	
	// At warning threshold, suggest a small delay
	return 10 * time.Second
}

// GetUserAPIStats returns comprehensive API usage statistics for a user
func (m *GitHubAPIMonitor) GetUserAPIStats(userID int64) map[APIType]*RateLimitInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if m.rateLimits[userID] == nil {
		return make(map[APIType]*RateLimitInfo)
	}
	
	// Return a copy to avoid concurrent modification
	stats := make(map[APIType]*RateLimitInfo)
	for apiType, info := range m.rateLimits[userID] {
		statsCopy := *info
		stats[apiType] = &statsCopy
	}
	
	return stats
}

// Cleanup removes old data and performs maintenance
func (m *GitHubAPIMonitor) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	cutoff := time.Now().Add(-24 * time.Hour)
	
	// Clean up request history
	for userID, userHistory := range m.requestHistory {
		for apiType, history := range userHistory {
			// Remove old requests
			var newHistory []time.Time
			for _, requestTime := range history {
				if requestTime.After(cutoff) {
					newHistory = append(newHistory, requestTime)
				}
			}
			
			if len(newHistory) == 0 {
				delete(m.requestHistory[userID], apiType)
			} else {
				m.requestHistory[userID][apiType] = newHistory
			}
		}
		
		// Remove empty user histories
		if len(m.requestHistory[userID]) == 0 {
			delete(m.requestHistory, userID)
		}
	}
	
	// Clean up old rate limit info (reset times in the past)
	now := time.Now()
	for userID, userLimits := range m.rateLimits {
		for apiType, info := range userLimits {
			// If reset time is far in the past, remove the info
			if info.ResetTime.Add(time.Hour).Before(now) {
				delete(m.rateLimits[userID], apiType)
			}
		}
		
		// Remove empty user limits
		if len(m.rateLimits[userID]) == 0 {
			delete(m.rateLimits, userID)
		}
	}
}

// GetGlobalAPIStats returns global API usage statistics across all users
func (m *GitHubAPIMonitor) GetGlobalAPIStats() map[APIType]struct {
	TotalUsers     int
	AverageUsage   float64
	UsersAtWarning int
	UsersAtCritical int
} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	stats := make(map[APIType]struct {
		TotalUsers     int
		AverageUsage   float64
		UsersAtWarning int
		UsersAtCritical int
	})
	
	// Initialize for both API types
	for _, apiType := range []APIType{APITypeREST, APITypeGraphQL} {
		totalUsers := 0
		totalUsage := 0.0
		usersAtWarning := 0
		usersAtCritical := 0
		
		for userID := range m.rateLimits {
			if info := m.rateLimits[userID][apiType]; info != nil {
				totalUsers++
				
				usagePercentage := float64(info.Limit-info.Remaining) / float64(info.Limit)
				totalUsage += usagePercentage
				
				if usagePercentage >= m.criticalThreshold {
					usersAtCritical++
				} else if usagePercentage >= m.warningThreshold {
					usersAtWarning++
				}
			}
		}
		
		averageUsage := 0.0
		if totalUsers > 0 {
			averageUsage = totalUsage / float64(totalUsers)
		}
		
		stats[apiType] = struct {
			TotalUsers     int
			AverageUsage   float64
			UsersAtWarning int
			UsersAtCritical int
		}{
			TotalUsers:     totalUsers,
			AverageUsage:   averageUsage,
			UsersAtWarning: usersAtWarning,
			UsersAtCritical: usersAtCritical,
		}
	}
	
	return stats
}