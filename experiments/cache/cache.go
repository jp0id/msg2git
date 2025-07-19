package cache

import (
	"context"
	"sync"
	"time"
)

const (
	// DefaultMaxSize is the default maximum number of items in cache
	DefaultMaxSize = 1000
	// DefaultExpiry is the default expiration duration for cache items
	DefaultExpiry = 5 * time.Minute
	// DefaultCleanupInterval is how often to run cleanup of expired items
	DefaultCleanupInterval = 1 * time.Minute
)

// Item represents a cached item with expiration time
type Item struct {
	Value     interface{}
	ExpiresAt time.Time
}

// IsExpired checks if the item has expired
func (i *Item) IsExpired() bool {
	return time.Now().After(i.ExpiresAt)
}

// Cache represents an in-memory cache with size limits and expiration
type Cache struct {
	mu              sync.RWMutex
	items           map[string]*Item
	maxSize         int
	defaultExpiry   time.Duration
	cleanupInterval time.Duration
	stopCleanup     chan struct{}
	cleanupStarted  bool
}

// New creates a new cache with default settings
func New() *Cache {
	return NewWithConfig(DefaultMaxSize, DefaultExpiry, DefaultCleanupInterval)
}

// NewWithConfig creates a new cache with custom configuration
func NewWithConfig(maxSize int, defaultExpiry, cleanupInterval time.Duration) *Cache {
	c := &Cache{
		items:           make(map[string]*Item),
		maxSize:         maxSize,
		defaultExpiry:   defaultExpiry,
		cleanupInterval: cleanupInterval,
		stopCleanup:     make(chan struct{}),
		cleanupStarted:  false,
	}
	
	// Start cleanup goroutine
	c.startCleanup()
	
	return c
}

// Set stores an item in the cache with default expiry
func (c *Cache) Set(key string, value interface{}) {
	c.SetWithExpiry(key, value, c.defaultExpiry)
}

// SetWithExpiry stores an item in the cache with custom expiry
func (c *Cache) SetWithExpiry(key string, value interface{}, expiry time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// Check if we need to evict items to make space
	if len(c.items) >= c.maxSize {
		c.evictLRU()
	}
	
	c.items[key] = &Item{
		Value:     value,
		ExpiresAt: time.Now().Add(expiry),
	}
}

// Get retrieves an item from the cache
func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	item, exists := c.items[key]
	c.mu.RUnlock()
	
	if !exists {
		return nil, false
	}
	
	if item.IsExpired() {
		// Remove expired item
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		return nil, false
	}
	
	return item.Value, true
}

// Delete removes an item from the cache
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

// Clear removes all items from the cache
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*Item)
}

// Size returns the current number of items in the cache
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Keys returns all keys in the cache (excluding expired items)
func (c *Cache) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	var keys []string
	now := time.Now()
	
	for key, item := range c.items {
		if now.Before(item.ExpiresAt) {
			keys = append(keys, key)
		}
	}
	
	return keys
}

// Stats returns cache statistics
type Stats struct {
	Size           int
	MaxSize        int
	DefaultExpiry  time.Duration
	ExpiredItems   int
}

// GetStats returns current cache statistics
func (c *Cache) GetStats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	expiredCount := 0
	now := time.Now()
	
	for _, item := range c.items {
		if now.After(item.ExpiresAt) {
			expiredCount++
		}
	}
	
	return Stats{
		Size:          len(c.items),
		MaxSize:       c.maxSize,
		DefaultExpiry: c.defaultExpiry,
		ExpiredItems:  expiredCount,
	}
}

// Close stops the cleanup goroutine and cleans up resources
func (c *Cache) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if c.cleanupStarted {
		close(c.stopCleanup)
		c.cleanupStarted = false
	}
}

// startCleanup starts the background cleanup goroutine
func (c *Cache) startCleanup() {
	if c.cleanupStarted {
		return
	}
	
	c.cleanupStarted = true
	
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// Log panic but don't crash the application
			}
		}()
		
		ticker := time.NewTicker(c.cleanupInterval)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				c.cleanupExpired()
			case <-c.stopCleanup:
				return
			}
		}
	}()
}

// cleanupExpired removes expired items from the cache
func (c *Cache) cleanupExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	now := time.Now()
	
	for key, item := range c.items {
		if now.After(item.ExpiresAt) {
			delete(c.items, key)
		}
	}
}

// evictLRU removes the least recently used item (simple FIFO for now)
// In a real implementation, you might want to track access times
func (c *Cache) evictLRU() {
	if len(c.items) == 0 {
		return
	}
	
	// Simple eviction: remove first item found
	// In production, you'd want proper LRU tracking
	for key := range c.items {
		delete(c.items, key)
		break
	}
}

// WithContext returns a context-aware cache wrapper
func (c *Cache) WithContext(ctx context.Context) *ContextCache {
	return &ContextCache{
		cache: c,
		ctx:   ctx,
	}
}

// ContextCache is a context-aware wrapper around Cache
type ContextCache struct {
	cache *Cache
	ctx   context.Context
}

// Set stores an item if context is not cancelled
func (cc *ContextCache) Set(key string, value interface{}) error {
	select {
	case <-cc.ctx.Done():
		return cc.ctx.Err()
	default:
		cc.cache.Set(key, value)
		return nil
	}
}

// Get retrieves an item if context is not cancelled
func (cc *ContextCache) Get(key string) (interface{}, bool, error) {
	select {
	case <-cc.ctx.Done():
		return nil, false, cc.ctx.Err()
	default:
		value, exists := cc.cache.Get(key)
		return value, exists, nil
	}
}