package telegram

import (
	"context"
	"fmt"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/msg2git/msg2git/internal/logger"
)

// WorkerPool manages concurrent processing of messages and callbacks
type WorkerPool struct {
	bot                 *Bot
	messageQueue        chan *tgbotapi.Message
	callbackQueue       chan *tgbotapi.CallbackQuery
	messageWorkerCount  int
	callbackWorkerCount int

	// Concurrency control
	maxConcurrentOps int
	opSemaphore      chan struct{}

	// Lifecycle management
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	started bool
	mu      sync.RWMutex
}

// WorkerPoolConfig holds configuration for the worker pool
type WorkerPoolConfig struct {
	MessageWorkers    int // Number of workers processing messages
	CallbackWorkers   int // Number of workers processing callbacks
	MessageQueueSize  int // Size of message queue buffer
	CallbackQueueSize int // Size of callback queue buffer
	MaxConcurrentOps  int // Maximum concurrent operations (GitHub/LLM calls)
}

// DefaultWorkerPoolConfig returns a sensible default configuration
func DefaultWorkerPoolConfig() WorkerPoolConfig {
	return WorkerPoolConfig{
		MessageWorkers:    35,  // 5 concurrent message processors
		CallbackWorkers:   30,  // 3 concurrent callback processors
		MessageQueueSize:  200, // Buffer up to 100 messages
		CallbackQueueSize: 100, // Buffer up to 50 callbacks
		MaxConcurrentOps:  20,  // Max 10 concurrent GitHub/LLM operations
	}
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(bot *Bot, config WorkerPoolConfig) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())

	return &WorkerPool{
		bot:                 bot,
		messageQueue:        make(chan *tgbotapi.Message, config.MessageQueueSize),
		callbackQueue:       make(chan *tgbotapi.CallbackQuery, config.CallbackQueueSize),
		messageWorkerCount:  config.MessageWorkers,
		callbackWorkerCount: config.CallbackWorkers,
		maxConcurrentOps:    config.MaxConcurrentOps,
		opSemaphore:         make(chan struct{}, config.MaxConcurrentOps),
		ctx:                 ctx,
		cancel:              cancel,
		started:             false,
	}
}

// Start initializes and starts all worker goroutines
func (wp *WorkerPool) Start() error {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if wp.started {
		return fmt.Errorf("worker pool already started")
	}

	logger.Info("Starting worker pool", map[string]interface{}{
		"message_workers":     wp.messageWorkerCount,
		"callback_workers":    wp.callbackWorkerCount,
		"max_concurrent_ops":  wp.maxConcurrentOps,
		"message_queue_size":  cap(wp.messageQueue),
		"callback_queue_size": cap(wp.callbackQueue),
	})

	// Start message workers
	for i := 0; i < wp.messageWorkerCount; i++ {
		wp.wg.Add(1)
		go wp.messageWorker(i)
	}

	// Start callback workers
	for i := 0; i < wp.callbackWorkerCount; i++ {
		wp.wg.Add(1)
		go wp.callbackWorker(i)
	}

	wp.started = true
	logger.InfoMsg("Worker pool started successfully")
	return nil
}

// Stop gracefully shuts down the worker pool
func (wp *WorkerPool) Stop() error {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if !wp.started {
		return fmt.Errorf("worker pool not started")
	}

	logger.InfoMsg("Stopping worker pool...")

	// Close queues to signal workers to stop accepting new work
	close(wp.messageQueue)
	close(wp.callbackQueue)

	// Cancel context to signal workers to finish current work
	wp.cancel()

	// Wait for all workers to finish with timeout
	done := make(chan struct{})
	go func() {
		wp.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.InfoMsg("Worker pool stopped gracefully")
	case <-time.After(30 * time.Second):
		logger.Warn("Worker pool shutdown timed out", nil)
		return fmt.Errorf("worker pool shutdown timed out")
	}

	wp.started = false
	return nil
}

// SubmitMessage adds a message to the processing queue
func (wp *WorkerPool) SubmitMessage(message *tgbotapi.Message) error {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	if !wp.started {
		return fmt.Errorf("worker pool not started")
	}

	select {
	case wp.messageQueue <- message:
		logger.Debug("Message queued for processing", map[string]interface{}{
			"chat_id":    message.Chat.ID,
			"username":   message.From.UserName,
			"queue_size": len(wp.messageQueue),
		})
		return nil
	case <-wp.ctx.Done():
		return fmt.Errorf("worker pool is shutting down")
	default:
		// Queue is full
		logger.Warn("Message queue full, dropping message", map[string]interface{}{
			"chat_id":  message.Chat.ID,
			"username": message.From.UserName,
		})
		return fmt.Errorf("message queue full")
	}
}

// SubmitCallback adds a callback query to the processing queue
func (wp *WorkerPool) SubmitCallback(callback *tgbotapi.CallbackQuery) error {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	if !wp.started {
		return fmt.Errorf("worker pool not started")
	}

	select {
	case wp.callbackQueue <- callback:
		logger.Debug("Callback queued for processing", map[string]interface{}{
			"chat_id":       callback.Message.Chat.ID,
			"callback_id":   callback.ID,
			"callback_data": callback.Data,
			"queue_size":    len(wp.callbackQueue),
		})
		return nil
	case <-wp.ctx.Done():
		return fmt.Errorf("worker pool is shutting down")
	default:
		// Queue is full
		logger.Warn("Callback queue full, dropping callback", map[string]interface{}{
			"chat_id":     callback.Message.Chat.ID,
			"callback_id": callback.ID,
		})
		return fmt.Errorf("callback queue full")
	}
}

// messageWorker processes messages from the message queue
func (wp *WorkerPool) messageWorker(workerID int) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Message worker panic recovered", map[string]interface{}{
				"worker_id": workerID,
				"panic":     r,
			})
		}
		wp.wg.Done()
	}()

	logger.Debug("Message worker started", map[string]interface{}{
		"worker_id": workerID,
	})

	for {
		select {
		case message, ok := <-wp.messageQueue:
			if !ok {
				// Queue closed, worker should exit
				logger.Debug("Message worker stopping", map[string]interface{}{
					"worker_id": workerID,
				})
				return
			}

			wp.processMessageWithConcurrencyControl(message, workerID)

		case <-wp.ctx.Done():
			// Context cancelled, worker should exit
			logger.Debug("Message worker cancelled", map[string]interface{}{
				"worker_id": workerID,
			})
			return
		}
	}
}

// callbackWorker processes callbacks from the callback queue
func (wp *WorkerPool) callbackWorker(workerID int) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Callback worker panic recovered", map[string]interface{}{
				"worker_id": workerID,
				"panic":     r,
			})
		}
		wp.wg.Done()
	}()

	logger.Debug("Callback worker started", map[string]interface{}{
		"worker_id": workerID,
	})

	for {
		select {
		case callback, ok := <-wp.callbackQueue:
			if !ok {
				// Queue closed, worker should exit
				logger.Debug("Callback worker stopping", map[string]interface{}{
					"worker_id": workerID,
				})
				return
			}

			wp.processCallbackWithConcurrencyControl(callback, workerID)

		case <-wp.ctx.Done():
			// Context cancelled, worker should exit
			logger.Debug("Callback worker cancelled", map[string]interface{}{
				"worker_id": workerID,
			})
			return
		}
	}
}

// processMessageWithConcurrencyControl processes a message with concurrency limits
func (wp *WorkerPool) processMessageWithConcurrencyControl(message *tgbotapi.Message, workerID int) {
	// Acquire semaphore for concurrent operations limit
	select {
	case wp.opSemaphore <- struct{}{}:
		defer func() { <-wp.opSemaphore }()
	case <-wp.ctx.Done():
		return
	}

	startTime := time.Now()

	logger.Debug("Processing message", map[string]interface{}{
		"worker_id": workerID,
		"chat_id":   message.Chat.ID,
		"username":  message.From.UserName,
	})

	if err := wp.bot.handleMessage(message); err != nil {
		logger.Error("Error processing message", map[string]interface{}{
			"worker_id": workerID,
			"error":     err.Error(),
			"chat_id":   message.Chat.ID,
			"username":  message.From.UserName,
		})
		wp.bot.sendErrorResponse(message.Chat.ID, err)
	}

	duration := time.Since(startTime)
	logger.Debug("Message processed", map[string]interface{}{
		"worker_id": workerID,
		"chat_id":   message.Chat.ID,
		"duration":  duration.String(),
	})
}

// processCallbackWithConcurrencyControl processes a callback with concurrency limits
func (wp *WorkerPool) processCallbackWithConcurrencyControl(callback *tgbotapi.CallbackQuery, workerID int) {
	// Acquire semaphore for concurrent operations limit
	select {
	case wp.opSemaphore <- struct{}{}:
		defer func() { <-wp.opSemaphore }()
	case <-wp.ctx.Done():
		return
	}

	startTime := time.Now()

	logger.Debug("Processing callback", map[string]interface{}{
		"worker_id":     workerID,
		"chat_id":       callback.Message.Chat.ID,
		"callback_id":   callback.ID,
		"callback_data": callback.Data,
	})

	if err := wp.bot.handleCallbackQuery(callback); err != nil {
		logger.Error("Error processing callback", map[string]interface{}{
			"worker_id":   workerID,
			"error":       err.Error(),
			"chat_id":     callback.Message.Chat.ID,
			"callback_id": callback.ID,
		})
		wp.bot.sendErrorResponse(callback.Message.Chat.ID, err)
	}

	duration := time.Since(startTime)
	logger.Debug("Callback processed", map[string]interface{}{
		"worker_id": workerID,
		"chat_id":   callback.Message.Chat.ID,
		"duration":  duration.String(),
	})
}

// GetStats returns current worker pool statistics
func (wp *WorkerPool) GetStats() map[string]interface{} {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	return map[string]interface{}{
		"started":                 wp.started,
		"message_queue_size":      len(wp.messageQueue),
		"callback_queue_size":     len(wp.callbackQueue),
		"message_queue_capacity":  cap(wp.messageQueue),
		"callback_queue_capacity": cap(wp.callbackQueue),
		"active_operations":       len(wp.opSemaphore),
		"max_concurrent_ops":      wp.maxConcurrentOps,
		"message_workers":         wp.messageWorkerCount,
		"callback_workers":        wp.callbackWorkerCount,
	}
}

