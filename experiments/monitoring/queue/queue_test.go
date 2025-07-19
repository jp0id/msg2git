//go:build experiments

package queue

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/msg2git/msg2git/experiments/monitoring/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestQueue() (*RequestQueue, *metrics.MetricsCollector) {
	metricsCollector := metrics.NewMetricsCollector()
	config := Config{
		Workers:         2,
		MaxQueueSize:    10,
		ProcessingDelay: 10 * time.Millisecond,
		RetryDelay:      100 * time.Millisecond,
		CleanupInterval: time.Second,
	}
	
	queue := NewRequestQueue(config, metricsCollector)
	return queue, metricsCollector
}

func TestNewRequestQueue(t *testing.T) {
	metricsCollector := metrics.NewMetricsCollector()
	
	// Test with empty config (should use defaults)
	queue := NewRequestQueue(Config{}, metricsCollector)
	
	assert.NotNil(t, queue)
	assert.Equal(t, 5, queue.workers)
	assert.Equal(t, 100, queue.maxQueueSize)
	assert.Equal(t, 100*time.Millisecond, queue.processingDelay)
	assert.Equal(t, 30*time.Second, queue.retryDelay)
	assert.Equal(t, 5*time.Minute, queue.cleanupInterval)
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	
	assert.Equal(t, 5, config.Workers)
	assert.Equal(t, 50, config.MaxQueueSize)
	assert.Equal(t, 200*time.Millisecond, config.ProcessingDelay)
	assert.Equal(t, 30*time.Second, config.RetryDelay)
	assert.Equal(t, 5*time.Minute, config.CleanupInterval)
}

func TestRequestType_Constants(t *testing.T) {
	assert.Equal(t, RequestType("sync"), RequestTypeSync)
	assert.Equal(t, RequestType("github_api"), RequestTypeGitHubAPI)
	assert.Equal(t, RequestType("command"), RequestTypeCommand)
	assert.Equal(t, RequestType("insight"), RequestTypeInsight)
}

func TestPriority_Constants(t *testing.T) {
	assert.Equal(t, Priority(0), PriorityLow)
	assert.Equal(t, Priority(1), PriorityNormal)
	assert.Equal(t, Priority(2), PriorityHigh)
	assert.Equal(t, Priority(3), PriorityUrgent)
}

func TestQueueRequest_Success(t *testing.T) {
	queue, _ := createTestQueue()
	
	executed := false
	request := &QueuedRequest{
		UserID:   12345,
		Type:     RequestTypeSync,
		Priority: PriorityNormal,
		Payload:  "test payload",
		Handler: func(ctx context.Context, req *QueuedRequest) error {
			executed = true
			return nil
		},
	}
	
	err := queue.QueueRequest(request)
	assert.NoError(t, err)
	
	// Verify request was queued
	assert.Equal(t, 1, queue.GetQueueDepth(12345))
	
	// Verify defaults were set
	assert.NotEmpty(t, request.ID)
	assert.False(t, request.CreatedAt.IsZero())
	assert.False(t, request.ProcessAt.IsZero())
	assert.Equal(t, 3, request.MaxRetries)
}

func TestQueueRequest_Validation(t *testing.T) {
	queue, _ := createTestQueue()
	
	// Test nil request
	err := queue.QueueRequest(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "request cannot be nil")
	
	// Test missing user ID
	request := &QueuedRequest{
		Type: RequestTypeSync,
	}
	err = queue.QueueRequest(request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "user ID is required")
	
	// Test missing handler
	request = &QueuedRequest{
		UserID: 12345,
		Type:   RequestTypeSync,
	}
	err = queue.QueueRequest(request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "handler function is required")
}

func TestQueueRequest_QueueFull(t *testing.T) {
	queue, _ := createTestQueue()
	userID := int64(12345)
	
	// Fill the queue to max capacity
	for i := 0; i < queue.maxQueueSize; i++ {
		request := &QueuedRequest{
			UserID:  userID,
			Type:    RequestTypeSync,
			Handler: func(ctx context.Context, req *QueuedRequest) error { return nil },
		}
		err := queue.QueueRequest(request)
		assert.NoError(t, err)
	}
	
	// Try to add one more request (should fail)
	request := &QueuedRequest{
		UserID:  userID,
		Type:    RequestTypeSync,
		Handler: func(ctx context.Context, req *QueuedRequest) error { return nil },
	}
	err := queue.QueueRequest(request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "queue full")
}

func TestGetQueuePosition(t *testing.T) {
	queue, _ := createTestQueue()
	userID := int64(12345)
	
	// Add multiple requests
	var requestIDs []string
	for i := 0; i < 3; i++ {
		request := &QueuedRequest{
			ID:      fmt.Sprintf("req-%d", i),
			UserID:  userID,
			Type:    RequestTypeSync,
			Handler: func(ctx context.Context, req *QueuedRequest) error { return nil },
		}
		err := queue.QueueRequest(request)
		require.NoError(t, err)
		requestIDs = append(requestIDs, request.ID)
	}
	
	// Check positions
	for i, requestID := range requestIDs {
		position := queue.GetQueuePosition(userID, requestID)
		assert.Equal(t, i, position)
	}
	
	// Check non-existent request
	position := queue.GetQueuePosition(userID, "non-existent")
	assert.Equal(t, -1, position)
}

func TestCancelRequest(t *testing.T) {
	queue, _ := createTestQueue()
	userID := int64(12345)
	
	// Add requests
	request1 := &QueuedRequest{
		ID:      "req-1",
		UserID:  userID,
		Type:    RequestTypeSync,
		Handler: func(ctx context.Context, req *QueuedRequest) error { return nil },
	}
	request2 := &QueuedRequest{
		ID:      "req-2",
		UserID:  userID,
		Type:    RequestTypeSync,
		Handler: func(ctx context.Context, req *QueuedRequest) error { return nil },
	}
	
	err := queue.QueueRequest(request1)
	require.NoError(t, err)
	err = queue.QueueRequest(request2)
	require.NoError(t, err)
	
	assert.Equal(t, 2, queue.GetQueueDepth(userID))
	
	// Cancel first request
	cancelled := queue.CancelRequest(userID, "req-1")
	assert.True(t, cancelled)
	assert.Equal(t, 1, queue.GetQueueDepth(userID))
	
	// Verify remaining request moved up
	position := queue.GetQueuePosition(userID, "req-2")
	assert.Equal(t, 0, position)
	
	// Try to cancel non-existent request
	cancelled = queue.CancelRequest(userID, "non-existent")
	assert.False(t, cancelled)
}

func TestProcessNextRequest_Priority(t *testing.T) {
	queue, _ := createTestQueue()
	userID := int64(12345)
	
	var executionOrder []string
	var mu sync.Mutex
	
	// Add requests with different priorities
	requests := []*QueuedRequest{
		{
			ID:       "low",
			UserID:   userID,
			Type:     RequestTypeSync,
			Priority: PriorityLow,
			Handler: func(ctx context.Context, req *QueuedRequest) error {
				mu.Lock()
				executionOrder = append(executionOrder, req.ID)
				mu.Unlock()
				return nil
			},
		},
		{
			ID:       "urgent",
			UserID:   userID,
			Type:     RequestTypeSync,
			Priority: PriorityUrgent,
			Handler: func(ctx context.Context, req *QueuedRequest) error {
				mu.Lock()
				executionOrder = append(executionOrder, req.ID)
				mu.Unlock()
				return nil
			},
		},
		{
			ID:       "normal",
			UserID:   userID,
			Type:     RequestTypeSync,
			Priority: PriorityNormal,
			Handler: func(ctx context.Context, req *QueuedRequest) error {
				mu.Lock()
				executionOrder = append(executionOrder, req.ID)
				mu.Unlock()
				return nil
			},
		},
	}
	
	// Queue requests in non-priority order
	for _, req := range requests {
		err := queue.QueueRequest(req)
		require.NoError(t, err)
	}
	
	// Process all requests manually
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		queue.processNextRequest(ctx, 0)
	}
	
	// Verify execution order was by priority
	mu.Lock()
	defer mu.Unlock()
	
	assert.Len(t, executionOrder, 3)
	assert.Equal(t, "urgent", executionOrder[0])   // Highest priority first
	assert.Equal(t, "normal", executionOrder[1])   // Normal priority second
	assert.Equal(t, "low", executionOrder[2])      // Lowest priority last
}

func TestProcessNextRequest_DelayedExecution(t *testing.T) {
	queue, _ := createTestQueue()
	userID := int64(12345)
	
	executed := false
	request := &QueuedRequest{
		UserID:    userID,
		Type:      RequestTypeSync,
		ProcessAt: time.Now().Add(100 * time.Millisecond), // Delay execution
		Handler: func(ctx context.Context, req *QueuedRequest) error {
			executed = true
			return nil
		},
	}
	
	err := queue.QueueRequest(request)
	require.NoError(t, err)
	
	// Try to process immediately (should not execute due to delay)
	ctx := context.Background()
	queue.processNextRequest(ctx, 0)
	assert.False(t, executed)
	
	// Wait for delay and try again
	time.Sleep(150 * time.Millisecond)
	queue.processNextRequest(ctx, 0)
	assert.True(t, executed)
}

func TestProcessNextRequest_Retry(t *testing.T) {
	queue, _ := createTestQueue()
	userID := int64(12345)
	
	attemptCount := 0
	request := &QueuedRequest{
		UserID:     userID,
		Type:       RequestTypeSync,
		MaxRetries: 2,
		Handler: func(ctx context.Context, req *QueuedRequest) error {
			attemptCount++
			if attemptCount < 3 {
				return fmt.Errorf("simulated error")
			}
			return nil
		},
	}
	
	err := queue.QueueRequest(request)
	require.NoError(t, err)
	
	ctx := context.Background()
	
	// First attempt (should fail and re-queue)
	queue.processNextRequest(ctx, 0)
	assert.Equal(t, 1, attemptCount)
	assert.Equal(t, 1, queue.GetQueueDepth(userID)) // Should be re-queued
	
	// Wait for retry delay
	time.Sleep(150 * time.Millisecond)
	
	// Second attempt (should fail and re-queue)
	queue.processNextRequest(ctx, 0)
	assert.Equal(t, 2, attemptCount)
	assert.Equal(t, 1, queue.GetQueueDepth(userID)) // Should be re-queued
	
	// Wait for retry delay
	time.Sleep(250 * time.Millisecond) // Exponential backoff
	
	// Third attempt (should succeed)
	queue.processNextRequest(ctx, 0)
	assert.Equal(t, 3, attemptCount)
	assert.Equal(t, 0, queue.GetQueueDepth(userID)) // Should be removed from queue
}

func TestProcessNextRequest_MaxRetriesExceeded(t *testing.T) {
	queue, _ := createTestQueue()
	userID := int64(12345)
	
	attemptCount := 0
	request := &QueuedRequest{
		UserID:     userID,
		Type:       RequestTypeSync,
		MaxRetries: 1, // Only allow 1 retry
		Handler: func(ctx context.Context, req *QueuedRequest) error {
			attemptCount++
			return fmt.Errorf("persistent error")
		},
	}
	
	err := queue.QueueRequest(request)
	require.NoError(t, err)
	
	ctx := context.Background()
	
	// First attempt (should fail and re-queue)
	queue.processNextRequest(ctx, 0)
	assert.Equal(t, 1, attemptCount)
	assert.Equal(t, 1, queue.GetQueueDepth(userID))
	
	// Wait for retry delay
	time.Sleep(150 * time.Millisecond)
	
	// Second attempt (should fail and not re-queue due to max retries)
	queue.processNextRequest(ctx, 0)
	assert.Equal(t, 2, attemptCount)
	assert.Equal(t, 0, queue.GetQueueDepth(userID)) // Should be removed permanently
}

func TestGetQueueStats(t *testing.T) {
	queue, _ := createTestQueue()
	
	// Add requests with different types and priorities
	requests := []*QueuedRequest{
		{UserID: 1, Type: RequestTypeSync, Priority: PriorityHigh, Handler: func(ctx context.Context, req *QueuedRequest) error { return nil }},
		{UserID: 1, Type: RequestTypeGitHubAPI, Priority: PriorityNormal, Handler: func(ctx context.Context, req *QueuedRequest) error { return nil }},
		{UserID: 2, Type: RequestTypeSync, Priority: PriorityLow, Handler: func(ctx context.Context, req *QueuedRequest) error { return nil }},
	}
	
	for _, req := range requests {
		err := queue.QueueRequest(req)
		require.NoError(t, err)
	}
	
	stats := queue.GetQueueStats()
	
	assert.Equal(t, 3, stats["total_requests"])
	assert.Equal(t, 2, stats["active_users"])
	assert.Equal(t, queue.maxQueueSize, stats["max_queue_size"])
	assert.Equal(t, queue.workers, stats["workers"])
	
	requestsByType := stats["requests_by_type"].(map[RequestType]int)
	assert.Equal(t, 2, requestsByType[RequestTypeSync])
	assert.Equal(t, 1, requestsByType[RequestTypeGitHubAPI])
	
	requestsByPriority := stats["requests_by_priority"].(map[Priority]int)
	assert.Equal(t, 1, requestsByPriority[PriorityHigh])
	assert.Equal(t, 1, requestsByPriority[PriorityNormal])
	assert.Equal(t, 1, requestsByPriority[PriorityLow])
}

func TestGetUserQueueInfo(t *testing.T) {
	queue, _ := createTestQueue()
	userID := int64(12345)
	
	// Test empty queue
	info := queue.GetUserQueueInfo(userID)
	assert.Equal(t, 0, info["queue_depth"])
	assert.Len(t, info["requests"], 0)
	
	// Add some requests
	for i := 0; i < 2; i++ {
		request := &QueuedRequest{
			ID:       fmt.Sprintf("req-%d", i),
			UserID:   userID,
			Type:     RequestTypeSync,
			Priority: PriorityNormal,
			Handler:  func(ctx context.Context, req *QueuedRequest) error { return nil },
		}
		err := queue.QueueRequest(request)
		require.NoError(t, err)
	}
	
	info = queue.GetUserQueueInfo(userID)
	assert.Equal(t, 2, info["queue_depth"])
	
	requests := info["requests"].([]map[string]interface{})
	assert.Len(t, requests, 2)
	
	// Verify first request
	firstReq := requests[0]
	assert.Equal(t, "req-0", firstReq["id"])
	assert.Equal(t, RequestTypeSync, firstReq["type"])
	assert.Equal(t, PriorityNormal, firstReq["priority"])
	assert.Equal(t, 0, firstReq["position"])
	assert.Equal(t, 0, firstReq["retry_count"])
}

func TestCleanup(t *testing.T) {
	queue, _ := createTestQueue()
	userID := int64(12345)
	
	// Add an old request
	oldRequest := &QueuedRequest{
		UserID:    userID,
		Type:      RequestTypeSync,
		CreatedAt: time.Now().Add(-2 * time.Hour), // Old request
		Handler:   func(ctx context.Context, req *QueuedRequest) error { return nil },
	}
	
	// Add a recent request
	recentRequest := &QueuedRequest{
		UserID:    userID,
		Type:      RequestTypeSync,
		CreatedAt: time.Now(), // Recent request
		Handler:   func(ctx context.Context, req *QueuedRequest) error { return nil },
	}
	
	err := queue.QueueRequest(oldRequest)
	require.NoError(t, err)
	err = queue.QueueRequest(recentRequest)
	require.NoError(t, err)
	
	assert.Equal(t, 2, queue.GetQueueDepth(userID))
	
	// Run cleanup
	queue.cleanup()
	
	// Only recent request should remain
	assert.Equal(t, 1, queue.GetQueueDepth(userID))
}

func TestStartStop(t *testing.T) {
	queue, _ := createTestQueue()
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Start the queue
	queue.Start(ctx)
	
	executed := make(chan bool, 1)
	request := &QueuedRequest{
		UserID: 12345,
		Type:   RequestTypeSync,
		Handler: func(ctx context.Context, req *QueuedRequest) error {
			executed <- true
			return nil
		},
	}
	
	err := queue.QueueRequest(request)
	require.NoError(t, err)
	
	// Wait for execution
	select {
	case <-executed:
		// Request was processed
	case <-time.After(time.Second):
		t.Fatal("Request was not processed within timeout")
	}
	
	// Stop the queue
	queue.Stop()
	
	// Verify queue is empty
	assert.Equal(t, 0, queue.GetQueueDepth(12345))
}

func BenchmarkQueueRequest(b *testing.B) {
	queue, _ := createTestQueue()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		request := &QueuedRequest{
			UserID:  int64(i % 1000),
			Type:    RequestTypeSync,
			Handler: func(ctx context.Context, req *QueuedRequest) error { return nil },
		}
		queue.QueueRequest(request)
	}
}

func BenchmarkGetQueueStats(b *testing.B) {
	queue, _ := createTestQueue()
	
	// Add some requests for realistic testing
	for i := 0; i < 100; i++ {
		request := &QueuedRequest{
			UserID:  int64(i % 10),
			Type:    RequestType([]RequestType{RequestTypeSync, RequestTypeGitHubAPI, RequestTypeCommand}[i%3]),
			Handler: func(ctx context.Context, req *QueuedRequest) error { return nil },
		}
		queue.QueueRequest(request)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		queue.GetQueueStats()
	}
}

// Test concurrent access to queue
func TestRequestQueue_ConcurrentAccess(t *testing.T) {
	queue, _ := createTestQueue()
	
	done := make(chan bool, 10)
	
	// Multiple goroutines adding requests concurrently
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()
			
			userID := int64(id)
			for j := 0; j < 10; j++ {
				request := &QueuedRequest{
					UserID:  userID,
					Type:    RequestTypeSync,
					Handler: func(ctx context.Context, req *QueuedRequest) error { return nil },
				}
				queue.QueueRequest(request)
				
				// Also test other operations
				queue.GetQueueDepth(userID)
				queue.GetQueueStats()
			}
		}(i)
	}
	
	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
	
	// Verify final state
	stats := queue.GetQueueStats()
	assert.Equal(t, 100, stats["total_requests"]) // 10 users * 10 requests each
}