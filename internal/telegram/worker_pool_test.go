package telegram

import (
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/msg2git/msg2git/internal/config"
)

func TestWorkerPoolCreation(t *testing.T) {
	// Create a mock bot for testing
	cfg := &config.Config{
		TelegramBotToken: "test_token",
	}
	
	// Create a bot struct with minimal fields for testing
	bot := &Bot{
		config: cfg,
	}
	
	// Test worker pool creation
	config := DefaultWorkerPoolConfig()
	wp := NewWorkerPool(bot, config)
	
	if wp == nil {
		t.Fatal("Worker pool should not be nil")
	}
	
	if wp.messageWorkerCount != config.MessageWorkers {
		t.Errorf("Expected %d message workers, got %d", config.MessageWorkers, wp.messageWorkerCount)
	}
	
	if wp.callbackWorkerCount != config.CallbackWorkers {
		t.Errorf("Expected %d callback workers, got %d", config.CallbackWorkers, wp.callbackWorkerCount)
	}
	
	if wp.maxConcurrentOps != config.MaxConcurrentOps {
		t.Errorf("Expected %d max concurrent ops, got %d", config.MaxConcurrentOps, wp.maxConcurrentOps)
	}
}

func TestWorkerPoolStartStop(t *testing.T) {
	// Create a mock bot for testing
	cfg := &config.Config{
		TelegramBotToken: "test_token",
	}
	
	bot := &Bot{
		config: cfg,
	}
	
	// Create worker pool with smaller config for testing
	config := WorkerPoolConfig{
		MessageWorkers:     2,
		CallbackWorkers:    1,
		MessageQueueSize:   10,
		CallbackQueueSize:  5,
		MaxConcurrentOps:   3,
	}
	
	wp := NewWorkerPool(bot, config)
	
	// Test starting worker pool
	err := wp.Start()
	if err != nil {
		t.Fatalf("Failed to start worker pool: %v", err)
	}
	
	// Verify it's started
	stats := wp.GetStats()
	if !stats["started"].(bool) {
		t.Error("Worker pool should be marked as started")
	}
	
	// Test starting again should fail
	err = wp.Start()
	if err == nil {
		t.Error("Starting already started worker pool should return error")
	}
	
	// Give workers a moment to initialize
	time.Sleep(10 * time.Millisecond)
	
	// Test stopping
	err = wp.Stop()
	if err != nil {
		t.Fatalf("Failed to stop worker pool: %v", err)
	}
	
	// Verify it's stopped
	stats = wp.GetStats()
	if stats["started"].(bool) {
		t.Error("Worker pool should be marked as stopped")
	}
}

func TestWorkerPoolSubmission(t *testing.T) {
	// Create a mock bot for testing
	cfg := &config.Config{
		TelegramBotToken: "test_token",
	}
	
	bot := &Bot{
		config: cfg,
	}
	
	// Create worker pool with very small queue for testing
	config := WorkerPoolConfig{
		MessageWorkers:     1,
		CallbackWorkers:    1,
		MessageQueueSize:   1, // Very small queue
		CallbackQueueSize:  1, // Very small queue
		MaxConcurrentOps:   1,
	}
	
	wp := NewWorkerPool(bot, config)
	
	err := wp.Start()
	if err != nil {
		t.Fatalf("Failed to start worker pool: %v", err)
	}
	defer wp.Stop()
	
	// Test message submission
	mockMessage := &tgbotapi.Message{
		MessageID: 1,
		From: &tgbotapi.User{
			ID:       12345,
			UserName: "testuser",
		},
		Chat: &tgbotapi.Chat{
			ID: 12345,
		},
		Text: "test message",
	}
	
	// Submit should succeed
	err = wp.SubmitMessage(mockMessage)
	if err != nil {
		t.Errorf("Failed to submit message: %v", err)
	}
	
	// Test callback submission
	mockCallback := &tgbotapi.CallbackQuery{
		ID:   "test_callback",
		Data: "test_data",
		Message: &tgbotapi.Message{
			Chat: &tgbotapi.Chat{
				ID: 12345,
			},
		},
	}
	
	// Submit should succeed
	err = wp.SubmitCallback(mockCallback)
	if err != nil {
		t.Errorf("Failed to submit callback: %v", err)
	}
	
	// Give some time for processing
	time.Sleep(50 * time.Millisecond)
}

func TestWorkerPoolStats(t *testing.T) {
	cfg := &config.Config{
		TelegramBotToken: "test_token",
	}
	
	bot := &Bot{
		config: cfg,
	}
	
	config := DefaultWorkerPoolConfig()
	wp := NewWorkerPool(bot, config)
	
	// Test stats before starting
	stats := wp.GetStats()
	if stats["started"].(bool) {
		t.Error("Worker pool should not be started initially")
	}
	
	// Start and test stats
	wp.Start()
	defer wp.Stop()
	
	stats = wp.GetStats()
	if !stats["started"].(bool) {
		t.Error("Worker pool should be started")
	}
	
	expectedFields := []string{
		"started", "message_queue_size", "callback_queue_size",
		"message_queue_capacity", "callback_queue_capacity",
		"active_operations", "max_concurrent_ops",
		"message_workers", "callback_workers",
	}
	
	for _, field := range expectedFields {
		if _, exists := stats[field]; !exists {
			t.Errorf("Stats should contain field: %s", field)
		}
	}
}

func TestDefaultWorkerPoolConfig(t *testing.T) {
	config := DefaultWorkerPoolConfig()
	
	if config.MessageWorkers <= 0 {
		t.Error("MessageWorkers should be positive")
	}
	
	if config.CallbackWorkers <= 0 {
		t.Error("CallbackWorkers should be positive")
	}
	
	if config.MessageQueueSize <= 0 {
		t.Error("MessageQueueSize should be positive")
	}
	
	if config.CallbackQueueSize <= 0 {
		t.Error("CallbackQueueSize should be positive")
	}
	
	if config.MaxConcurrentOps <= 0 {
		t.Error("MaxConcurrentOps should be positive")
	}
}