//go:build experiments

package metrics

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMetricsCollector(t *testing.T) {
	collector := NewMetricsCollector()
	
	assert.NotNil(t, collector)
	assert.NotNil(t, collector.telegramCommandsTotal)
	assert.NotNil(t, collector.githubAPIRequestsTotal)
	assert.NotNil(t, collector.activeUsersGauge)
	assert.NotNil(t, collector.activeUsers)
	assert.Equal(t, 0, len(collector.activeUsers))
}

func TestRecordTelegramCommand(t *testing.T) {
	collector := NewMetricsCollector()
	
	// Record some commands
	collector.RecordTelegramCommand(12345, "sync", "success")
	collector.RecordTelegramCommand(12345, "sync", "error")
	collector.RecordTelegramCommand(67890, "todo", "success")
	
	// Check metrics values
	expected := `
		# HELP telegram_commands_total Total number of Telegram commands processed
		# TYPE telegram_commands_total counter
		telegram_commands_total{command="sync",status="error",user_id="12345"} 1
		telegram_commands_total{command="sync",status="success",user_id="12345"} 1
		telegram_commands_total{command="todo",status="success",user_id="67890"} 1
	`
	
	err := testutil.GatherAndCompare(collector.telegramCommandsTotal, strings.NewReader(expected))
	assert.NoError(t, err)
	
	// Check that active users were updated
	assert.Equal(t, 2, collector.GetActiveUsersCount())
}

func TestRecordCommandProcessingTime(t *testing.T) {
	collector := NewMetricsCollector()
	
	// Record processing times
	collector.RecordCommandProcessingTime("sync", "success", 2*time.Second)
	collector.RecordCommandProcessingTime("sync", "success", 1*time.Second)
	collector.RecordCommandProcessingTime("todo", "error", 500*time.Millisecond)
	
	// Verify histogram was updated (we can't easily check exact values due to buckets)
	metricFamily, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	
	found := false
	for _, mf := range metricFamily {
		if mf.GetName() == "command_processing_duration_seconds" {
			found = true
			assert.Equal(t, 3, len(mf.GetMetric())) // 2 metrics for sync, 1 for todo
		}
	}
	assert.True(t, found, "command_processing_duration_seconds metric not found")
}

func TestRecordRateLimitViolation(t *testing.T) {
	collector := NewMetricsCollector()
	
	// Record violations
	collector.RecordRateLimitViolation(12345, "command_rate")
	collector.RecordRateLimitViolation(12345, "command_rate")
	collector.RecordRateLimitViolation(67890, "github_api")
	
	expected := `
		# HELP user_rate_limit_violations_total Total number of rate limit violations per user
		# TYPE user_rate_limit_violations_total counter
		user_rate_limit_violations_total{limit_type="command_rate",user_id="12345"} 2
		user_rate_limit_violations_total{limit_type="github_api",user_id="67890"} 1
	`
	
	err := testutil.GatherAndCompare(collector.userRateLimitViolations, strings.NewReader(expected))
	assert.NoError(t, err)
}

func TestUpdateCommandQueueDepth(t *testing.T) {
	collector := NewMetricsCollector()
	
	// Update queue depths
	collector.UpdateCommandQueueDepth(12345, 5)
	collector.UpdateCommandQueueDepth(67890, 0)
	collector.UpdateCommandQueueDepth(12345, 3) // Update same user
	
	expected := `
		# HELP command_queue_depth Current depth of command queue per user
		# TYPE command_queue_depth gauge
		command_queue_depth{user_id="12345"} 3
		command_queue_depth{user_id="67890"} 0
	`
	
	err := testutil.GatherAndCompare(collector.commandQueueDepth, strings.NewReader(expected))
	assert.NoError(t, err)
}

func TestRecordGitHubAPIRequest(t *testing.T) {
	collector := NewMetricsCollector()
	
	// Record API requests
	collector.RecordGitHubAPIRequest(12345, "REST", "/repos/owner/repo", "success")
	collector.RecordGitHubAPIRequest(12345, "GraphQL", "/graphql", "success")
	collector.RecordGitHubAPIRequest(67890, "REST", "/repos/owner/repo", "error")
	
	expected := `
		# HELP github_api_requests_total Total number of GitHub API requests
		# TYPE github_api_requests_total counter
		github_api_requests_total{api_type="GraphQL",endpoint="/graphql",status="success",user_id="12345"} 1
		github_api_requests_total{api_type="REST",endpoint="/repos/owner/repo",status="error",user_id="67890"} 1
		github_api_requests_total{api_type="REST",endpoint="/repos/owner/repo",status="success",user_id="12345"} 1
	`
	
	err := testutil.GatherAndCompare(collector.githubAPIRequestsTotal, strings.NewReader(expected))
	assert.NoError(t, err)
}

func TestUpdateGitHubRateLimit(t *testing.T) {
	collector := NewMetricsCollector()
	resetTime := time.Unix(1640995200, 0) // 2022-01-01 00:00:00 UTC
	
	// Update rate limits
	collector.UpdateGitHubRateLimit(12345, "REST", 4500, resetTime)
	collector.UpdateGitHubRateLimit(12345, "GraphQL", 4800, resetTime)
	collector.UpdateGitHubRateLimit(67890, "REST", 3000, resetTime)
	
	expectedRemaining := `
		# HELP github_api_rate_limit_remaining Remaining GitHub API rate limit
		# TYPE github_api_rate_limit_remaining gauge
		github_api_rate_limit_remaining{api_type="GraphQL",user_id="12345"} 4800
		github_api_rate_limit_remaining{api_type="REST",user_id="12345"} 4500
		github_api_rate_limit_remaining{api_type="REST",user_id="67890"} 3000
	`
	
	expectedResetTime := `
		# HELP github_api_rate_limit_reset_time GitHub API rate limit reset time (Unix timestamp)
		# TYPE github_api_rate_limit_reset_time gauge
		github_api_rate_limit_reset_time{api_type="GraphQL",user_id="12345"} 1.6409952e+09
		github_api_rate_limit_reset_time{api_type="REST",user_id="12345"} 1.6409952e+09
		github_api_rate_limit_reset_time{api_type="REST",user_id="67890"} 1.6409952e+09
	`
	
	err := testutil.GatherAndCompare(collector.githubAPIRateLimitRemaining, strings.NewReader(expectedRemaining))
	assert.NoError(t, err)
	
	err = testutil.GatherAndCompare(collector.githubAPIRateLimitResetTime, strings.NewReader(expectedResetTime))
	assert.NoError(t, err)
}

func TestRecordQueuedRequest(t *testing.T) {
	collector := NewMetricsCollector()
	
	// Record queued requests
	collector.RecordQueuedRequest(12345, "sync", "queued")
	collector.RecordQueuedRequest(12345, "sync", "processed")
	collector.RecordQueuedRequest(67890, "github_api", "queued")
	
	expected := `
		# HELP queued_requests_total Total number of queued requests
		# TYPE queued_requests_total counter
		queued_requests_total{request_type="github_api",status="queued",user_id="67890"} 1
		queued_requests_total{request_type="sync",status="processed",user_id="12345"} 1
		queued_requests_total{request_type="sync",status="queued",user_id="12345"} 1
	`
	
	err := testutil.GatherAndCompare(collector.queuedRequestsTotal, strings.NewReader(expected))
	assert.NoError(t, err)
}

func TestRecordRateLimitCheck(t *testing.T) {
	collector := NewMetricsCollector()
	
	// Record rate limit checks
	collector.RecordRateLimitCheck(12345, "command_rate", true)
	collector.RecordRateLimitCheck(12345, "command_rate", true)
	collector.RecordRateLimitCheck(12345, "command_rate", false)
	collector.RecordRateLimitCheck(67890, "github_api", true)
	
	expectedChecks := `
		# HELP rate_limit_checks_total Total number of rate limit checks performed
		# TYPE rate_limit_checks_total counter
		rate_limit_checks_total{limit_type="command_rate",user_id="12345"} 3
		rate_limit_checks_total{limit_type="github_api",user_id="67890"} 1
	`
	
	expectedAllowed := `
		# HELP rate_limit_allowed_total Total number of requests allowed by rate limiter
		# TYPE rate_limit_allowed_total counter
		rate_limit_allowed_total{limit_type="command_rate",user_id="12345"} 2
		rate_limit_allowed_total{limit_type="github_api",user_id="67890"} 1
	`
	
	err := testutil.GatherAndCompare(collector.rateLimitChecksTotal, strings.NewReader(expectedChecks))
	assert.NoError(t, err)
	
	err = testutil.GatherAndCompare(collector.rateLimitAllowedTotal, strings.NewReader(expectedAllowed))
	assert.NoError(t, err)
}

func TestUpdateCacheHitRatio(t *testing.T) {
	collector := NewMetricsCollector()
	
	// Update cache hit ratios
	collector.UpdateCacheHitRatio("commit_graph", 0.95)
	collector.UpdateCacheHitRatio("issue_data", 0.80)
	collector.UpdateCacheHitRatio("commit_graph", 0.92) // Update same cache type
	
	expected := `
		# HELP cache_hit_ratio Cache hit ratio by cache type
		# TYPE cache_hit_ratio gauge
		cache_hit_ratio{cache_type="commit_graph"} 0.92
		cache_hit_ratio{cache_type="issue_data"} 0.8
	`
	
	err := testutil.GatherAndCompare(collector.cacheHitRatio, strings.NewReader(expected))
	assert.NoError(t, err)
}

func TestUpdateSystemLoadFactor(t *testing.T) {
	collector := NewMetricsCollector()
	
	// Update system load
	collector.UpdateSystemLoadFactor(0.75)
	
	expected := `
		# HELP system_load_factor Current system load factor (0-1)
		# TYPE system_load_factor gauge
		system_load_factor 0.75
	`
	
	err := testutil.GatherAndCompare(collector.systemLoadFactor, strings.NewReader(expected))
	assert.NoError(t, err)
}

func TestActiveUsersTracking(t *testing.T) {
	collector := NewMetricsCollector()
	
	// Initially no active users
	assert.Equal(t, 0, collector.GetActiveUsersCount())
	
	// Add some users
	collector.RecordTelegramCommand(12345, "sync", "success")
	collector.RecordTelegramCommand(67890, "todo", "success")
	collector.RecordTelegramCommand(99999, "issue", "success")
	
	assert.Equal(t, 3, collector.GetActiveUsersCount())
	
	// Adding same user again shouldn't increase count
	collector.RecordTelegramCommand(12345, "note", "success")
	assert.Equal(t, 3, collector.GetActiveUsersCount())
}

func TestActiveUsersCleanup(t *testing.T) {
	collector := NewMetricsCollector()
	
	// Add users at different times
	collector.RecordTelegramCommand(12345, "sync", "success")
	
	// Manually set old timestamp for testing
	collector.mu.Lock()
	collector.activeUsers[67890] = time.Now().Add(-10 * time.Minute) // Old user
	collector.activeUsers[99999] = time.Now().Add(-1 * time.Minute)  // Recent user
	collector.mu.Unlock()
	
	// Trigger cleanup
	collector.Cleanup()
	
	// Only recent users should remain (12345 and 99999)
	assert.Equal(t, 2, collector.GetActiveUsersCount())
	
	// Verify the old user was removed
	collector.mu.RLock()
	_, exists := collector.activeUsers[67890]
	collector.mu.RUnlock()
	assert.False(t, exists)
}

func TestRecordGitHubAPIRequestDuration(t *testing.T) {
	collector := NewMetricsCollector()
	
	// Record API request durations
	collector.RecordGitHubAPIRequestDuration("REST", "/repos/owner/repo", "success", 2*time.Second)
	collector.RecordGitHubAPIRequestDuration("GraphQL", "/graphql", "success", 500*time.Millisecond)
	collector.RecordGitHubAPIRequestDuration("REST", "/repos/owner/repo", "error", 5*time.Second)
	
	// Verify histogram was updated
	metricFamily, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	
	found := false
	for _, mf := range metricFamily {
		if mf.GetName() == "github_api_request_duration_seconds" {
			found = true
			// We should have metrics for different combinations of labels
			assert.GreaterOrEqual(t, len(mf.GetMetric()), 2)
		}
	}
	assert.True(t, found, "github_api_request_duration_seconds metric not found")
}

func TestRecordQueueProcessingTime(t *testing.T) {
	collector := NewMetricsCollector()
	
	// Record queue processing times
	collector.RecordQueueProcessingTime("sync", 3*time.Second)
	collector.RecordQueueProcessingTime("github_api", 1*time.Second)
	
	// Verify histogram was updated
	metricFamily, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	
	found := false
	for _, mf := range metricFamily {
		if mf.GetName() == "queue_processing_time_seconds" {
			found = true
			assert.GreaterOrEqual(t, len(mf.GetMetric()), 2)
		}
	}
	assert.True(t, found, "queue_processing_time_seconds metric not found")
}

func BenchmarkRecordTelegramCommand(b *testing.B) {
	collector := NewMetricsCollector()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.RecordTelegramCommand(int64(i%1000), "sync", "success")
	}
}

func BenchmarkRecordGitHubAPIRequest(b *testing.B) {
	collector := NewMetricsCollector()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.RecordGitHubAPIRequest(int64(i%1000), "REST", "/repos/owner/repo", "success")
	}
}

func BenchmarkUpdateGitHubRateLimit(b *testing.B) {
	collector := NewMetricsCollector()
	resetTime := time.Now()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.UpdateGitHubRateLimit(int64(i%1000), "REST", 4500-i, resetTime)
	}
}

// TestConcurrentAccess tests that the collector handles concurrent access safely
func TestConcurrentAccess(t *testing.T) {
	collector := NewMetricsCollector()
	
	// Run multiple goroutines concurrently
	done := make(chan bool, 10)
	
	for i := 0; i < 10; i++ {
		go func(id int) {
			userID := int64(id)
			for j := 0; j < 100; j++ {
				collector.RecordTelegramCommand(userID, "sync", "success")
				collector.RecordGitHubAPIRequest(userID, "REST", "/repos/test/repo", "success")
				collector.UpdateCommandQueueDepth(userID, j%10)
			}
			done <- true
		}(i)
	}
	
	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
	
	// Verify we have the expected number of active users
	assert.Equal(t, 10, collector.GetActiveUsersCount())
}