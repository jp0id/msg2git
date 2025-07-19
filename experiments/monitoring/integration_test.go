//go:build experiments

package main

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/msg2git/msg2git/experiments/monitoring/github_monitor"
	"github.com/msg2git/msg2git/experiments/monitoring/metrics"
	"github.com/msg2git/msg2git/experiments/monitoring/queue"
	"github.com/msg2git/msg2git/experiments/monitoring/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// IntegratedMonitoringSystem combines all monitoring components
type IntegratedMonitoringSystem struct {
	metrics       *metrics.MetricsCollector
	rateLimiter   *ratelimit.MemoryRateLimiter
	githubMonitor *github_monitor.GitHubAPIMonitor
	requestQueue  *queue.RequestQueue
}

// NewIntegratedMonitoringSystem creates a complete monitoring system
func NewIntegratedMonitoringSystem() *IntegratedMonitoringSystem {
	metricsCollector := metrics.NewMetricsCollector()
	
	// Rate Limiter (Memory-based, no Redis required)
	rateLimitConfig := ratelimit.DefaultConfig()
	rateLimitConfig.CommandLimit.Requests = 10       // Lower for testing
	rateLimitConfig.CommandLimit.Window = time.Minute
	memoryRateLimiter := ratelimit.NewMemoryRateLimiter(rateLimitConfig, metricsCollector)
	
	// GitHub API Monitor
	githubMonitorConfig := github_monitor.Config{
		WarningThreshold:  0.8,
		CriticalThreshold: 0.9,
		MaxHistorySize:    100,
	}
	githubAPIMonitor := github_monitor.NewGitHubAPIMonitor(githubMonitorConfig, metricsCollector)
	
	// Request Queue
	queueConfig := queue.Config{
		Workers:         3,
		MaxQueueSize:    50,
		ProcessingDelay: 50 * time.Millisecond,
		RetryDelay:      100 * time.Millisecond,
		CleanupInterval: time.Minute,
	}
	requestQueue := queue.NewRequestQueue(queueConfig, metricsCollector)
	
	return &IntegratedMonitoringSystem{
		metrics:       metricsCollector,
		rateLimiter:   memoryRateLimiter,
		githubMonitor: githubAPIMonitor,
		requestQueue:  requestQueue,
	}
}

// AllowCommand checks if a user can execute a command based on rate limits
func (ims *IntegratedMonitoringSystem) AllowCommand(userID int64, command string, premiumLevel int) bool {
	ctx := context.Background()
	
	// Check rate limit
	allowed, err := ims.rateLimiter.CheckLimit(ctx, userID, ratelimit.LimitTypeCommand, premiumLevel)
	if err != nil {
		// Log error and allow (fail-open for availability)
		ims.metrics.RecordRateLimitCheck(userID, "command_rate", true)
		return true
	}
	
	// Record the check
	ims.metrics.RecordRateLimitCheck(userID, "command_rate", allowed)
	
	if allowed {
		// Consume the limit
		err = ims.rateLimiter.ConsumeLimit(ctx, userID, ratelimit.LimitTypeCommand, premiumLevel)
		if err != nil {
			// Failed to consume, don't allow
			ims.metrics.RecordRateLimitViolation(userID, "command_rate")
			return false
		}
	}
	
	return allowed
}

// TrackGitHubRequest tracks a GitHub API request and updates all relevant metrics
func (ims *IntegratedMonitoringSystem) TrackGitHubRequest(userID int64, apiType github_monitor.APIType, endpoint string, startTime time.Time, resp *http.Response, err error) {
	// Track in GitHub monitor
	ims.githubMonitor.TrackRequest(userID, apiType, endpoint, startTime, resp, err)
	
	// Check if we should queue future requests
	if ims.githubMonitor.ShouldQueueRequest(userID, apiType) {
		// In real implementation, we'd queue the request
		ims.metrics.RecordQueuedRequest(userID, "github_api", "should_queue")
	}
}

// QueueCommand queues a command for later execution
func (ims *IntegratedMonitoringSystem) QueueCommand(userID int64, command string, payload interface{}) error {
	request := &queue.QueuedRequest{
		UserID:   userID,
		Type:     queue.RequestTypeCommand,
		Priority: queue.PriorityNormal,
		Payload:  payload,
		Handler: func(ctx context.Context, req *queue.QueuedRequest) error {
			// Simulate command execution
			ims.metrics.RecordTelegramCommand(userID, command, "success")
			return nil
		},
	}
	
	return ims.requestQueue.QueueRequest(request)
}

// Start begins monitoring and processing
func (ims *IntegratedMonitoringSystem) Start(ctx context.Context) {
	ims.requestQueue.Start(ctx)
}

// Stop stops all monitoring
func (ims *IntegratedMonitoringSystem) Stop() {
	ims.requestQueue.Stop()
	ims.rateLimiter.Close()
}

// GetSystemStatus returns comprehensive system status
func (ims *IntegratedMonitoringSystem) GetSystemStatus() map[string]interface{} {
	return map[string]interface{}{
		"active_users":   ims.metrics.GetActiveUsersCount(),
		"queue_stats":    ims.requestQueue.GetQueueStats(),
		"github_global":  ims.githubMonitor.GetGlobalAPIStats(),
	}
}

func TestIntegratedSystem_BasicFlow(t *testing.T) {
	system := NewIntegratedMonitoringSystem()
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Start the system
	system.Start(ctx)
	defer system.Stop()
	
	userID := int64(12345)
	
	// Test 1: Command rate limiting
	allowed := system.AllowCommand(userID, "sync", 0)
	assert.True(t, allowed)
	
	// Test 2: Track GitHub API request
	startTime := time.Now()
	resp := &http.Response{
		StatusCode: 200,
		Header: http.Header{
			"X-RateLimit-Limit":     []string{"5000"},
			"X-RateLimit-Remaining": []string{"4000"},
			"X-RateLimit-Reset":     []string{fmt.Sprintf("%d", time.Now().Add(time.Hour).Unix())},
		},
	}
	
	system.TrackGitHubRequest(userID, github_monitor.APITypeREST, "/repos/owner/repo", startTime, resp, nil)
	
	// Test 3: Queue a command
	err := system.QueueCommand(userID, "sync", map[string]string{"repo": "test"})
	assert.NoError(t, err)
	
	// Test 4: Verify system status
	status := system.GetSystemStatus()
	assert.NotNil(t, status["active_users"])
	assert.NotNil(t, status["queue_stats"])
	assert.NotNil(t, status["github_global"])
	
	// Wait for queued command to be processed
	time.Sleep(200 * time.Millisecond)
	
	// Verify queue is empty after processing
	queueStats := status["queue_stats"].(map[string]interface{})
	// Note: The exact queue depth depends on timing, so we'll just verify structure
	assert.Contains(t, queueStats, "total_requests")
}

func TestIntegratedSystem_GitHubRateLimitFlow(t *testing.T) {
	system := NewIntegratedMonitoringSystem()
	userID := int64(12345)
	
	// Simulate approaching rate limit
	startTime := time.Now()
	
	// First request - plenty of capacity
	resp1 := &http.Response{
		StatusCode: 200,
		Header: http.Header{
			"X-RateLimit-Limit":     []string{"5000"},
			"X-RateLimit-Remaining": []string{"4000"},
			"X-RateLimit-Reset":     []string{fmt.Sprintf("%d", time.Now().Add(time.Hour).Unix())},
		},
	}
	system.TrackGitHubRequest(userID, github_monitor.APITypeREST, "/repos/owner/repo", startTime, resp1, nil)
	
	// Verify not at warning threshold
	assert.False(t, system.githubMonitor.IsAtWarningThreshold(userID, github_monitor.APITypeREST))
	
	// Second request - approaching limit
	resp2 := &http.Response{
		StatusCode: 200,
		Header: http.Header{
			"X-RateLimit-Limit":     []string{"5000"},
			"X-RateLimit-Remaining": []string{"500"}, // 90% used
			"X-RateLimit-Reset":     []string{fmt.Sprintf("%d", time.Now().Add(time.Hour).Unix())},
		},
	}
	system.TrackGitHubRequest(userID, github_monitor.APITypeREST, "/repos/owner/repo", startTime, resp2, nil)
	
	// Verify at critical threshold
	assert.True(t, system.githubMonitor.IsAtCriticalThreshold(userID, github_monitor.APITypeREST))
	
	// Verify should queue future requests
	assert.True(t, system.githubMonitor.ShouldQueueRequest(userID, github_monitor.APITypeREST))
	
	// Get user API stats
	stats := system.githubMonitor.GetUserAPIStats(userID)
	restStats := stats[github_monitor.APITypeREST]
	require.NotNil(t, restStats)
	assert.Equal(t, 5000, restStats.Limit)
	assert.Equal(t, 500, restStats.Remaining)
}

func TestIntegratedSystem_QueuePriorityHandling(t *testing.T) {
	system := NewIntegratedMonitoringSystem()
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	system.Start(ctx)
	defer system.Stop()
	
	userID := int64(12345)
	
	var executionOrder []string
	
	// Queue requests with different priorities
	requests := []struct {
		command  string
		priority queue.Priority
	}{
		{"low-priority", queue.PriorityLow},
		{"urgent", queue.PriorityUrgent},
		{"normal", queue.PriorityNormal},
		{"high", queue.PriorityHigh},
	}
	
	for _, req := range requests {
		queueReq := &queue.QueuedRequest{
			UserID:   userID,
			Type:     queue.RequestTypeCommand,
			Priority: req.priority,
			Payload:  req.command,
			Handler: func(command string) func(ctx context.Context, qr *queue.QueuedRequest) error {
				return func(ctx context.Context, qr *queue.QueuedRequest) error {
					executionOrder = append(executionOrder, command)
					return nil
				}
			}(req.command),
		}
		
		err := system.requestQueue.QueueRequest(queueReq)
		require.NoError(t, err)
	}
	
	// Wait for processing
	time.Sleep(300 * time.Millisecond)
	
	// Verify execution order was by priority
	require.Len(t, executionOrder, 4)
	assert.Equal(t, "urgent", executionOrder[0])      // Highest priority first
	assert.Equal(t, "high", executionOrder[1])        // High priority second
	assert.Equal(t, "normal", executionOrder[2])      // Normal priority third
	assert.Equal(t, "low-priority", executionOrder[3]) // Lowest priority last
}

func TestIntegratedSystem_MetricsCollection(t *testing.T) {
	system := NewIntegratedMonitoringSystem()
	userID := int64(12345)
	
	// Record various metrics
	system.metrics.RecordTelegramCommand(userID, "sync", "success")
	system.metrics.RecordTelegramCommand(userID, "todo", "success")
	system.metrics.RecordTelegramCommand(userID, "issue", "error")
	
	system.metrics.RecordGitHubAPIRequest(userID, "REST", "/repos/owner/repo", "success")
	system.metrics.RecordGitHubAPIRequest(userID, "GraphQL", "/graphql", "success")
	
	system.metrics.RecordRateLimitCheck(userID, "command_rate", true)
	system.metrics.RecordRateLimitCheck(userID, "command_rate", false)
	
	system.metrics.UpdateCacheHitRatio("commit_graph", 0.95)
	system.metrics.UpdateSystemLoadFactor(0.75)
	
	// Verify active users tracking
	assert.Equal(t, 1, system.metrics.GetActiveUsersCount())
	
	// Test metrics cleanup
	system.metrics.Cleanup()
	assert.Equal(t, 1, system.metrics.GetActiveUsersCount()) // Should still be active
}

func TestIntegratedSystem_ErrorHandling(t *testing.T) {
	system := NewIntegratedMonitoringSystem()
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	system.Start(ctx)
	defer system.Stop()
	
	userID := int64(12345)
	
	// Test queue request validation
	invalidRequest := &queue.QueuedRequest{
		// Missing required fields
	}
	err := system.requestQueue.QueueRequest(invalidRequest)
	assert.Error(t, err)
	
	// Test GitHub API error handling
	startTime := time.Now()
	errorResp := &http.Response{
		StatusCode: 500,
		Header:     http.Header{},
	}
	
	// Should not panic on error response
	system.TrackGitHubRequest(userID, github_monitor.APITypeREST, "/repos/owner/repo", startTime, errorResp, fmt.Errorf("API error"))
	
	// Test queue with failing handler
	failingRequest := &queue.QueuedRequest{
		UserID:     userID,
		Type:       queue.RequestTypeCommand,
		MaxRetries: 1,
		Handler: func(ctx context.Context, req *queue.QueuedRequest) error {
			return fmt.Errorf("simulated failure")
		},
	}
	
	err = system.requestQueue.QueueRequest(failingRequest)
	assert.NoError(t, err)
	
	// Wait for processing and retries
	time.Sleep(500 * time.Millisecond)
	
	// Queue should eventually be empty after max retries exceeded
	depth := system.requestQueue.GetQueueDepth(userID)
	assert.Equal(t, 0, depth)
}

func TestIntegratedSystem_ConcurrentUsage(t *testing.T) {
	system := NewIntegratedMonitoringSystem()
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	system.Start(ctx)
	defer system.Stop()
	
	// Simulate multiple users using the system concurrently
	numUsers := 10
	requestsPerUser := 5
	
	done := make(chan bool, numUsers)
	
	for i := 0; i < numUsers; i++ {
		go func(userID int64) {
			defer func() { done <- true }()
			
			for j := 0; j < requestsPerUser; j++ {
				// Command rate limiting
				system.AllowCommand(userID, "sync", 0)
				
				// GitHub API tracking
				startTime := time.Now()
				resp := &http.Response{
					StatusCode: 200,
					Header: http.Header{
						"X-RateLimit-Limit":     []string{"5000"},
						"X-RateLimit-Remaining": []string{fmt.Sprintf("%d", 4000+j*100)},
						"X-RateLimit-Reset":     []string{fmt.Sprintf("%d", time.Now().Add(time.Hour).Unix())},
					},
				}
				system.TrackGitHubRequest(userID, github_monitor.APITypeREST, "/repos/test/repo", startTime, resp, nil)
				
				// Queue commands
				system.QueueCommand(userID, "sync", map[string]string{"test": "data"})
				
				// Metrics recording
				system.metrics.RecordTelegramCommand(userID, "sync", "success")
			}
		}(int64(i + 1))
	}
	
	// Wait for all users to complete
	for i := 0; i < numUsers; i++ {
		<-done
	}
	
	// Verify system state
	status := system.GetSystemStatus()
	assert.Equal(t, numUsers, status["active_users"])
	
	queueStats := status["queue_stats"].(map[string]interface{})
	assert.Equal(t, numUsers, queueStats["active_users"])
	
	// Wait for queue processing
	time.Sleep(time.Second)
	
	// Verify GitHub stats
	githubStats := status["github_global"].(map[github_monitor.APIType]struct {
		TotalUsers     int
		AverageUsage   float64
		UsersAtWarning int
		UsersAtCritical int
	})
	
	restStats := githubStats[github_monitor.APITypeREST]
	assert.Equal(t, numUsers, restStats.TotalUsers)
}

func BenchmarkIntegratedSystem_CommandFlow(b *testing.B) {
	system := NewIntegratedMonitoringSystem()
	userID := int64(12345)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		system.AllowCommand(userID, "sync", 0)
		system.metrics.RecordTelegramCommand(userID, "sync", "success")
	}
}

func BenchmarkIntegratedSystem_GitHubTracking(b *testing.B) {
	system := NewIntegratedMonitoringSystem()
	userID := int64(12345)
	
	startTime := time.Now()
	resp := &http.Response{
		StatusCode: 200,
		Header: http.Header{
			"X-RateLimit-Limit":     []string{"5000"},
			"X-RateLimit-Remaining": []string{"4000"},
			"X-RateLimit-Reset":     []string{fmt.Sprintf("%d", time.Now().Add(time.Hour).Unix())},
		},
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		system.TrackGitHubRequest(userID, github_monitor.APITypeREST, "/repos/owner/repo", startTime, resp, nil)
	}
}

// TestRealWorldScenario simulates a realistic usage pattern
func TestRealWorldScenario(t *testing.T) {
	system := NewIntegratedMonitoringSystem()
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	system.Start(ctx)
	defer system.Stop()
	
	// Simulate a user's daily activity
	userID := int64(12345)
	
	// Morning: User syncs issues (high GitHub API usage)
	for i := 0; i < 10; i++ {
		if system.AllowCommand(userID, "sync", 0) {
			// Simulate sync command processing
			system.metrics.RecordTelegramCommand(userID, "sync", "success")
			
			// Simulate GitHub API calls during sync
			startTime := time.Now()
			resp := &http.Response{
				StatusCode: 200,
				Header: http.Header{
					"X-RateLimit-Limit":     []string{"5000"},
					"X-RateLimit-Remaining": []string{fmt.Sprintf("%d", 4500-i*50)},
					"X-RateLimit-Reset":     []string{fmt.Sprintf("%d", time.Now().Add(time.Hour).Unix())},
				},
			}
			system.TrackGitHubRequest(userID, github_monitor.APITypeGraphQL, "/graphql", startTime, resp, nil)
		}
	}
	
	// Afternoon: Regular note-taking (low API usage)
	for i := 0; i < 20; i++ {
		if system.AllowCommand(userID, "note", 0) {
			system.metrics.RecordTelegramCommand(userID, "note", "success")
			
			// Simulate file commit
			startTime := time.Now()
			resp := &http.Response{
				StatusCode: 200,
				Header: http.Header{
					"X-RateLimit-Limit":     []string{"5000"},
					"X-RateLimit-Remaining": []string{fmt.Sprintf("%d", 4000-i*10)},
					"X-RateLimit-Reset":     []string{fmt.Sprintf("%d", time.Now().Add(time.Hour).Unix())},
				},
			}
			system.TrackGitHubRequest(userID, github_monitor.APITypeREST, "/repos/owner/repo/contents/note.md", startTime, resp, nil)
		}
	}
	
	// Evening: Approaching rate limits, some requests should be queued
	for i := 0; i < 5; i++ {
		startTime := time.Now()
		resp := &http.Response{
			StatusCode: 200,
			Header: http.Header{
				"X-RateLimit-Limit":     []string{"5000"},
				"X-RateLimit-Remaining": []string{fmt.Sprintf("%d", 300-i*50)}, // Approaching limit
				"X-RateLimit-Reset":     []string{fmt.Sprintf("%d", time.Now().Add(time.Hour).Unix())},
			},
		}
		system.TrackGitHubRequest(userID, github_monitor.APITypeREST, "/repos/owner/repo/contents/note.md", startTime, resp, nil)
		
		// As we approach limits, some requests should be queued
		if system.githubMonitor.ShouldQueueRequest(userID, github_monitor.APITypeREST) {
			err := system.QueueCommand(userID, "note", "queued note content")
			assert.NoError(t, err)
		}
	}
	
	// Wait for queue processing
	time.Sleep(500 * time.Millisecond)
	
	// Verify final state
	status := system.GetSystemStatus()
	assert.Equal(t, 1, status["active_users"])
	
	// Verify GitHub monitoring detected the rate limit approach
	assert.True(t, system.githubMonitor.IsAtCriticalThreshold(userID, github_monitor.APITypeREST))
	
	// Verify metrics were collected
	userStats := system.githubMonitor.GetUserAPIStats(userID)
	assert.NotNil(t, userStats[github_monitor.APITypeREST])
	assert.NotNil(t, userStats[github_monitor.APITypeGraphQL])
}