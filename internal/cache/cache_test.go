package cache

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestCache_Basic(t *testing.T) {
	c := New()
	defer c.Close()
	
	// Test Set and Get
	c.Set("key1", "value1")
	
	value, exists := c.Get("key1")
	if !exists {
		t.Error("Expected key1 to exist")
	}
	if value != "value1" {
		t.Errorf("Expected 'value1', got %v", value)
	}
	
	// Test non-existent key
	_, exists = c.Get("nonexistent")
	if exists {
		t.Error("Expected nonexistent key to not exist")
	}
}

func TestCache_Expiry(t *testing.T) {
	c := NewWithConfig(100, 50*time.Millisecond, 10*time.Millisecond)
	defer c.Close()
	
	// Set item with short expiry
	c.SetWithExpiry("expiring", "value", 50*time.Millisecond)
	
	// Should exist immediately
	value, exists := c.Get("expiring")
	if !exists || value != "value" {
		t.Error("Expected item to exist immediately after setting")
	}
	
	// Wait for expiration
	time.Sleep(100 * time.Millisecond)
	
	// Should not exist after expiration
	_, exists = c.Get("expiring")
	if exists {
		t.Error("Expected item to be expired")
	}
}

func TestCache_SizeLimit(t *testing.T) {
	c := NewWithConfig(3, time.Hour, time.Minute)
	defer c.Close()
	
	// Fill cache to capacity
	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.Set("key3", "value3")
	
	if c.Size() != 3 {
		t.Errorf("Expected size 3, got %d", c.Size())
	}
	
	// Add one more item (should evict one)
	c.Set("key4", "value4")
	
	if c.Size() != 3 {
		t.Errorf("Expected size to remain 3 after eviction, got %d", c.Size())
	}
	
	// key4 should exist
	_, exists := c.Get("key4")
	if !exists {
		t.Error("Expected newest item to exist after eviction")
	}
}

func TestCache_Delete(t *testing.T) {
	c := New()
	defer c.Close()
	
	c.Set("key1", "value1")
	c.Delete("key1")
	
	_, exists := c.Get("key1")
	if exists {
		t.Error("Expected deleted key to not exist")
	}
}

func TestCache_Clear(t *testing.T) {
	c := New()
	defer c.Close()
	
	c.Set("key1", "value1")
	c.Set("key2", "value2")
	
	c.Clear()
	
	if c.Size() != 0 {
		t.Errorf("Expected size 0 after clear, got %d", c.Size())
	}
}

func TestCache_Keys(t *testing.T) {
	c := NewWithConfig(100, time.Hour, time.Minute)
	defer c.Close()
	
	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.SetWithExpiry("expired", "value", -time.Hour) // Already expired
	
	keys := c.Keys()
	
	if len(keys) != 2 {
		t.Errorf("Expected 2 non-expired keys, got %d", len(keys))
	}
	
	// Check that expired key is not included
	for _, key := range keys {
		if key == "expired" {
			t.Error("Expected expired key to not be in keys list")
		}
	}
}

func TestCache_Stats(t *testing.T) {
	c := NewWithConfig(100, time.Hour, time.Minute)
	defer c.Close()
	
	c.Set("key1", "value1")
	c.SetWithExpiry("expired", "value", -time.Hour) // Already expired
	
	stats := c.GetStats()
	
	if stats.Size != 2 {
		t.Errorf("Expected total size 2, got %d", stats.Size)
	}
	if stats.MaxSize != 100 {
		t.Errorf("Expected max size 100, got %d", stats.MaxSize)
	}
	if stats.ExpiredItems != 1 {
		t.Errorf("Expected 1 expired item, got %d", stats.ExpiredItems)
	}
}

func TestCache_Cleanup(t *testing.T) {
	c := NewWithConfig(100, time.Hour, 25*time.Millisecond) // Long default expiry
	defer c.Close()
	
	// Add items with short expiry
	c.SetWithExpiry("temp1", "value1", 50*time.Millisecond)
	c.SetWithExpiry("temp2", "value2", 50*time.Millisecond)
	c.Set("permanent", "value") // Uses default expiry (1 hour)
	
	// Wait for cleanup to run
	time.Sleep(150 * time.Millisecond)
	
	// Only permanent item should remain
	if c.Size() > 1 {
		t.Errorf("Expected cleanup to remove expired items, size: %d", c.Size())
	}
	
	_, exists := c.Get("permanent")
	if !exists {
		t.Error("Expected permanent item to still exist after cleanup")
	}
}

func TestCache_Concurrent(t *testing.T) {
	c := New()
	defer c.Close()
	
	const numGoroutines = 10
	const numOperations = 100
	
	var wg sync.WaitGroup
	
	// Concurrent writers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				c.Set(key, j)
			}
		}(i)
	}
	
	// Concurrent readers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				c.Get(key)
			}
		}(i)
	}
	
	wg.Wait()
	
	// Test should complete without race conditions
}

func TestContextCache(t *testing.T) {
	c := New()
	defer c.Close()
	
	ctx, cancel := context.WithCancel(context.Background())
	cc := c.WithContext(ctx)
	
	// Should work with active context
	err := cc.Set("key1", "value1")
	if err != nil {
		t.Errorf("Expected no error with active context, got %v", err)
	}
	
	value, exists, err := cc.Get("key1")
	if err != nil {
		t.Errorf("Expected no error with active context, got %v", err)
	}
	if !exists || value != "value1" {
		t.Error("Expected to get value with active context")
	}
	
	// Cancel context
	cancel()
	
	// Should fail with cancelled context
	err = cc.Set("key2", "value2")
	if err == nil {
		t.Error("Expected error with cancelled context")
	}
	
	_, _, err = cc.Get("key1")
	if err == nil {
		t.Error("Expected error with cancelled context")
	}
}

func TestCache_IsExpired(t *testing.T) {
	// Test Item.IsExpired method
	item := &Item{
		Value:     "test",
		ExpiresAt: time.Now().Add(-time.Hour), // Already expired
	}
	
	if !item.IsExpired() {
		t.Error("Expected item to be expired")
	}
	
	item.ExpiresAt = time.Now().Add(time.Hour) // Not expired
	if item.IsExpired() {
		t.Error("Expected item to not be expired")
	}
}

func TestCache_MultipleTypes(t *testing.T) {
	c := New()
	defer c.Close()
	
	// Test different value types
	c.Set("string", "hello")
	c.Set("int", 42)
	c.Set("slice", []int{1, 2, 3})
	c.Set("map", map[string]int{"a": 1, "b": 2})
	
	// Verify all types
	if val, exists := c.Get("string"); !exists || val != "hello" {
		t.Error("String value not stored correctly")
	}
	
	if val, exists := c.Get("int"); !exists || val != 42 {
		t.Error("Int value not stored correctly")
	}
	
	if val, exists := c.Get("slice"); !exists {
		t.Error("Slice value not stored")
	} else if slice, ok := val.([]int); !ok || len(slice) != 3 {
		t.Error("Slice value not stored correctly")
	}
	
	if val, exists := c.Get("map"); !exists {
		t.Error("Map value not stored")
	} else if m, ok := val.(map[string]int); !ok || m["a"] != 1 {
		t.Error("Map value not stored correctly")
	}
}

