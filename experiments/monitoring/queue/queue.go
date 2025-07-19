package queue

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/msg2git/msg2git/experiments/monitoring/metrics"
)

// RequestType represents different types of requests that can be queued
type RequestType string

const (
	RequestTypeSync      RequestType = "sync"
	RequestTypeGitHubAPI RequestType = "github_api"
	RequestTypeCommand   RequestType = "command"
	RequestTypeInsight   RequestType = "insight"
)

// Priority levels for queued requests
type Priority int

const (
	PriorityLow Priority = iota
	PriorityNormal
	PriorityHigh
	PriorityUrgent
)

// QueuedRequest represents a request waiting to be processed
type QueuedRequest struct {
	ID          string
	UserID      int64
	Type        RequestType
	Priority    Priority
	Payload     interface{}
	CreatedAt   time.Time
	RetryCount  int
	MaxRetries  int
	ProcessAt   time.Time // When the request should be processed (for delays)
	
	// Callback function to execute when request is processed
	Handler func(ctx context.Context, request *QueuedRequest) error
}

// RequestQueue manages queued requests with priorities and delays
type RequestQueue struct {
	mu       sync.RWMutex
	requests map[int64][]*QueuedRequest // User ID -> requests
	metrics  *metrics.MetricsCollector
	
	// Processing control
	stopCh   chan struct{}
	workers  int
	
	// Configuration
	maxQueueSize     int
	processingDelay  time.Duration
	retryDelay       time.Duration
	cleanupInterval  time.Duration
}

// Config holds configuration for the request queue
type Config struct {
	Workers         int           // Number of worker goroutines
	MaxQueueSize    int           // Maximum number of requests per user
	ProcessingDelay time.Duration // Base delay between processing requests
	RetryDelay      time.Duration // Delay before retrying failed requests
	CleanupInterval time.Duration // How often to clean up old completed requests
}

// NewRequestQueue creates a new request queue
func NewRequestQueue(config Config, metricsCollector *metrics.MetricsCollector) *RequestQueue {
	// Set defaults if not provided
	if config.Workers == 0 {
		config.Workers = 5
	}
	if config.MaxQueueSize == 0 {
		config.MaxQueueSize = 100
	}
	if config.ProcessingDelay == 0 {
		config.ProcessingDelay = 100 * time.Millisecond
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = 30 * time.Second
	}
	if config.CleanupInterval == 0 {
		config.CleanupInterval = 5 * time.Minute
	}
	
	return &RequestQueue{
		requests:        make(map[int64][]*QueuedRequest),
		metrics:         metricsCollector,
		stopCh:          make(chan struct{}),
		workers:         config.Workers,
		maxQueueSize:    config.MaxQueueSize,
		processingDelay: config.ProcessingDelay,
		retryDelay:      config.RetryDelay,
		cleanupInterval: config.CleanupInterval,
	}
}

// Start begins processing queued requests
func (q *RequestQueue) Start(ctx context.Context) {
	// Start worker goroutines
	for i := 0; i < q.workers; i++ {
		go q.worker(ctx, i)
	}
	
	// Start cleanup goroutine
	go q.cleanupWorker(ctx)
}

// Stop stops processing queued requests
func (q *RequestQueue) Stop() {
	close(q.stopCh)
}

// QueueRequest adds a request to the queue
func (q *RequestQueue) QueueRequest(request *QueuedRequest) error {
	if request == nil {
		return fmt.Errorf("request cannot be nil")
	}
	
	if request.UserID == 0 {
		return fmt.Errorf("user ID is required")
	}
	
	if request.Handler == nil {
		return fmt.Errorf("handler function is required")
	}
	
	// Set defaults
	if request.ID == "" {
		request.ID = fmt.Sprintf("%d_%d_%s", request.UserID, time.Now().UnixNano(), request.Type)
	}
	if request.CreatedAt.IsZero() {
		request.CreatedAt = time.Now()
	}
	if request.ProcessAt.IsZero() {
		request.ProcessAt = time.Now()
	}
	if request.MaxRetries == 0 {
		request.MaxRetries = 3
	}
	
	q.mu.Lock()
	defer q.mu.Unlock()
	
	// Check queue size limit
	userQueue := q.requests[request.UserID]
	if len(userQueue) >= q.maxQueueSize {
		q.metrics.RecordQueuedRequest(request.UserID, string(request.Type), "rejected_full")
		return fmt.Errorf("queue full for user %d", request.UserID)
	}
	
	// Add request to queue
	q.requests[request.UserID] = append(userQueue, request)
	
	// Update metrics
	q.metrics.RecordQueuedRequest(request.UserID, string(request.Type), "queued")
	q.metrics.UpdateCommandQueueDepth(request.UserID, len(q.requests[request.UserID]))
	
	return nil
}

// GetQueueDepth returns the current queue depth for a user
func (q *RequestQueue) GetQueueDepth(userID int64) int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	
	return len(q.requests[userID])
}

// GetQueuePosition returns the position of a request in the queue (0-based)
func (q *RequestQueue) GetQueuePosition(userID int64, requestID string) int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	
	userQueue := q.requests[userID]
	for i, req := range userQueue {
		if req.ID == requestID {
			return i
		}
	}
	
	return -1 // Not found
}

// CancelRequest removes a request from the queue
func (q *RequestQueue) CancelRequest(userID int64, requestID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	
	userQueue := q.requests[userID]
	for i, req := range userQueue {
		if req.ID == requestID {
			// Remove request from queue
			q.requests[userID] = append(userQueue[:i], userQueue[i+1:]...)
			
			// Update metrics
			q.metrics.RecordQueuedRequest(userID, string(req.Type), "cancelled")
			q.metrics.UpdateCommandQueueDepth(userID, len(q.requests[userID]))
			
			return true
		}
	}
	
	return false
}

// worker processes requests from the queue
func (q *RequestQueue) worker(ctx context.Context, workerID int) {
	ticker := time.NewTicker(q.processingDelay)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-q.stopCh:
			return
		case <-ticker.C:
			q.processNextRequest(ctx, workerID)
		}
	}
}

// processNextRequest finds and processes the next available request
func (q *RequestQueue) processNextRequest(ctx context.Context, workerID int) {
	request := q.getNextRequest()
	if request == nil {
		return
	}
	
	startTime := time.Now()
	
	// Process the request
	err := q.processRequest(ctx, request)
	
	duration := time.Since(startTime)
	q.metrics.RecordQueueProcessingTime(string(request.Type), duration)
	
	if err != nil {
		q.handleRequestError(request, err)
	} else {
		q.handleRequestSuccess(request)
	}
}

// getNextRequest finds the next request to process based on priority and timing
func (q *RequestQueue) getNextRequest() *QueuedRequest {
	q.mu.Lock()
	defer q.mu.Unlock()
	
	var bestRequest *QueuedRequest
	var bestUserID int64
	var bestIndex int
	
	now := time.Now()
	
	// Find the highest priority request that's ready to process
	for userID, userQueue := range q.requests {
		for i, req := range userQueue {
			// Skip requests that aren't ready yet
			if req.ProcessAt.After(now) {
				continue
			}
			
			// Select based on priority (higher priority wins)
			if bestRequest == nil || req.Priority > bestRequest.Priority {
				bestRequest = req
				bestUserID = userID
				bestIndex = i
			} else if req.Priority == bestRequest.Priority {
				// Same priority, prefer older requests
				if req.CreatedAt.Before(bestRequest.CreatedAt) {
					bestRequest = req
					bestUserID = userID
					bestIndex = i
				}
			}
		}
	}
	
	// Remove the selected request from the queue
	if bestRequest != nil {
		userQueue := q.requests[bestUserID]
		q.requests[bestUserID] = append(userQueue[:bestIndex], userQueue[bestIndex+1:]...)
		
		// Update metrics
		q.metrics.UpdateCommandQueueDepth(bestUserID, len(q.requests[bestUserID]))
	}
	
	return bestRequest
}

// processRequest executes a request's handler
func (q *RequestQueue) processRequest(ctx context.Context, request *QueuedRequest) error {
	// Create a timeout context for the request
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	
	return request.Handler(requestCtx, request)
}

// handleRequestError handles a failed request
func (q *RequestQueue) handleRequestError(request *QueuedRequest, err error) {
	request.RetryCount++
	
	if request.RetryCount <= request.MaxRetries {
		// Retry the request with exponential backoff
		delay := q.retryDelay * time.Duration(1<<uint(request.RetryCount-1))
		request.ProcessAt = time.Now().Add(delay)
		
		// Re-queue the request
		q.mu.Lock()
		q.requests[request.UserID] = append(q.requests[request.UserID], request)
		q.metrics.UpdateCommandQueueDepth(request.UserID, len(q.requests[request.UserID]))
		q.mu.Unlock()
		
		q.metrics.RecordQueuedRequest(request.UserID, string(request.Type), "retried")
	} else {
		// Max retries exceeded
		q.metrics.RecordQueuedRequest(request.UserID, string(request.Type), "failed")
	}
}

// handleRequestSuccess handles a successful request
func (q *RequestQueue) handleRequestSuccess(request *QueuedRequest) {
	q.metrics.RecordQueuedRequest(request.UserID, string(request.Type), "completed")
}

// cleanupWorker periodically cleans up old data
func (q *RequestQueue) cleanupWorker(ctx context.Context) {
	ticker := time.NewTicker(q.cleanupInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-q.stopCh:
			return
		case <-ticker.C:
			q.cleanup()
		}
	}
}

// cleanup removes old completed requests and updates metrics
func (q *RequestQueue) cleanup() {
	q.mu.Lock()
	defer q.mu.Unlock()
	
	cutoff := time.Now().Add(-time.Hour) // Remove requests older than 1 hour
	
	for userID, userQueue := range q.requests {
		var newQueue []*QueuedRequest
		
		for _, req := range userQueue {
			// Keep requests that are recent or still have retries left
			if req.CreatedAt.After(cutoff) || req.RetryCount <= req.MaxRetries {
				newQueue = append(newQueue, req)
			}
		}
		
		if len(newQueue) == 0 {
			delete(q.requests, userID)
		} else {
			q.requests[userID] = newQueue
		}
		
		// Update metrics
		if len(newQueue) != len(userQueue) {
			q.metrics.UpdateCommandQueueDepth(userID, len(newQueue))
		}
	}
}

// GetQueueStats returns statistics about the queue
func (q *RequestQueue) GetQueueStats() map[string]interface{} {
	q.mu.RLock()
	defer q.mu.RUnlock()
	
	totalRequests := 0
	userCount := len(q.requests)
	requestsByType := make(map[RequestType]int)
	requestsByPriority := make(map[Priority]int)
	
	for _, userQueue := range q.requests {
		totalRequests += len(userQueue)
		
		for _, req := range userQueue {
			requestsByType[req.Type]++
			requestsByPriority[req.Priority]++
		}
	}
	
	return map[string]interface{}{
		"total_requests":       totalRequests,
		"active_users":         userCount,
		"requests_by_type":     requestsByType,
		"requests_by_priority": requestsByPriority,
		"max_queue_size":       q.maxQueueSize,
		"workers":              q.workers,
	}
}

// GetUserQueueInfo returns detailed queue information for a specific user
func (q *RequestQueue) GetUserQueueInfo(userID int64) map[string]interface{} {
	q.mu.RLock()
	defer q.mu.RUnlock()
	
	userQueue := q.requests[userID]
	if len(userQueue) == 0 {
		return map[string]interface{}{
			"queue_depth": 0,
			"requests":    []map[string]interface{}{},
		}
	}
	
	requests := make([]map[string]interface{}, len(userQueue))
	for i, req := range userQueue {
		requests[i] = map[string]interface{}{
			"id":          req.ID,
			"type":        req.Type,
			"priority":    req.Priority,
			"created_at":  req.CreatedAt,
			"process_at":  req.ProcessAt,
			"retry_count": req.RetryCount,
			"position":    i,
		}
	}
	
	return map[string]interface{}{
		"queue_depth": len(userQueue),
		"requests":    requests,
	}
}

// DefaultConfig returns a default configuration for the request queue
func DefaultConfig() Config {
	return Config{
		Workers:         5,
		MaxQueueSize:    50,
		ProcessingDelay: 200 * time.Millisecond,
		RetryDelay:      30 * time.Second,
		CleanupInterval: 5 * time.Minute,
	}
}