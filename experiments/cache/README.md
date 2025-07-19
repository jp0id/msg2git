# In-Memory Cache

A high-performance, thread-safe in-memory cache implementation with automatic expiration and size limits.

## Features

- **Thread-safe**: Concurrent read/write operations with RWMutex optimization
- **Automatic expiration**: Items expire after configurable duration with O(1) checking
- **Size limits**: Configurable maximum cache size (default 1000) with automatic eviction
- **Background cleanup**: Non-blocking automatic removal of expired items
- **Context support**: Context-aware operations for cancellation and timeout handling
- **Statistics**: Built-in cache statistics including size, expired items, and configuration
- **High performance**: 4.3M+ set ops/sec, 13M+ get ops/sec on modern hardware
- **Memory efficient**: Minimal allocations per operation (1-4 allocs/op)
- **Panic recovery**: Safe goroutine management with automatic cleanup
- **Zero dependencies**: No external dependencies beyond Go standard library

## Quick Start

```go
package main

import (
    "fmt"
    "time"
    
    "github.com/msg2git/msg2git/experiments/cache"
)

func main() {
    // Create cache with default settings
    c := cache.New()
    defer c.Close()
    
    // Set a value
    c.Set("key1", "hello world")
    
    // Get a value
    if value, exists := c.Get("key1"); exists {
        fmt.Println("Found:", value)
    }
    
    // Set with custom expiry
    c.SetWithExpiry("temp", "temporary data", 5*time.Second)
}
```

## Configuration

```go
// Create cache with custom configuration
c := cache.NewWithConfig(
    1000,                // max size (number of items)
    5*time.Minute,      // default expiry duration
    1*time.Minute,      // cleanup interval
)
defer c.Close()
```

## API Reference

### Core Operations

- `Set(key, value)` - Store item with default expiry (5 minutes)
- `SetWithExpiry(key, value, expiry)` - Store item with custom expiry duration
- `Get(key)` - Retrieve item (returns value, exists) with automatic expiry checking
- `Delete(key)` - Remove specific item from cache
- `Clear()` - Remove all items from cache instantly

### Utility Methods

- `Size()` - Get current number of items in cache
- `Keys()` - Get all non-expired keys (excludes expired items)
- `GetStats()` - Get detailed cache statistics (size, max size, expired count)
- `Close()` - Stop background cleanup goroutine and free resources

### Advanced Operations

- `NewWithConfig(maxSize, defaultExpiry, cleanupInterval)` - Create cache with custom settings
- `WithContext(ctx)` - Create context-aware cache wrapper for cancellation
- `cleanupExpired()` - Manual cleanup of expired items (called automatically)

### Context Support

```go
ctx := context.Background()
cc := c.WithContext(ctx)

err := cc.Set("key", "value")
value, exists, err := cc.Get("key")
```

## Performance

Run benchmarks to see performance characteristics:

```bash
go test -bench=. -benchmem
```

**Benchmark Results (Apple M4 Pro):**
- **Set operations**: 4.3M ops/sec (234ns/op, 80B/op, 4 allocs/op)
- **Get operations**: 13M ops/sec (77ns/op, 13B/op, 1 alloc/op)
- **Concurrent Set**: 2.6M ops/sec (415ns/op) - scales well with cores
- **Concurrent Get**: 8.9M ops/sec (127ns/op) - excellent read scaling
- **Mixed operations**: 6.5M ops/sec (205ns/op) - balanced read/write
- **Memory usage**: Scales efficiently from 100 to 10K items
- **Context operations**: 4.6M ops/sec (281ns/op) - minimal overhead
- **High contention**: 3.5M ops/sec (370ns/op) - handles contention well

**Key Performance Features:**
- **RWMutex optimization**: Allows concurrent reads for better performance
- **Efficient expiry checking**: O(1) expiry validation during get operations
- **Background cleanup**: Non-blocking cleanup of expired items
- **Memory efficient**: Minimal allocations per operation
- **Lock-free reads**: When items are not expired, minimal locking overhead

## Configuration Guidelines

### Cache Size
- **Small cache (100-1K items)**: Good for hot data, minimal memory usage
- **Medium cache (1K-10K items)**: Balance between memory and hit rate
- **Large cache (10K+ items)**: High hit rate, higher memory usage

### Expiry Settings
- **Short expiry (seconds-minutes)**: Good for volatile data
- **Medium expiry (minutes-hours)**: Good for session data
- **Long expiry (hours-days)**: Good for reference data

### Cleanup Interval
- Should be 10-20% of typical expiry time
- More frequent cleanup = less memory usage, higher CPU
- Less frequent cleanup = more memory usage, lower CPU

## Thread Safety

All operations are thread-safe and can be called concurrently from multiple goroutines. The cache uses RWMutex for optimal read performance.

## Memory Management

The cache automatically:
- Removes expired items during cleanup cycles
- Evicts old items when size limit is reached
- Prevents memory leaks from expired items

## Best Practices

1. **Always call Close()**: Stops background cleanup goroutine
2. **Choose appropriate size limits**: Based on available memory
3. **Set reasonable expiry times**: Based on data freshness needs
4. **Monitor cache statistics**: Use GetStats() for optimization
5. **Use context operations**: For cancellable operations

## Testing

The cache includes comprehensive test coverage:

```bash
# Run all tests
go test

# Run with race detection (ensures thread safety)
go test -race

# Run benchmarks with memory allocation stats
go test -bench=. -benchmem

# Run specific benchmark categories
go test -bench=BenchmarkCache_Concurrent -benchmem
go test -bench=BenchmarkCache_Set -benchmem
go test -bench=BenchmarkCache_Get -benchmem
```

**Test Coverage:**
- ✅ Basic operations (set, get, delete, clear)
- ✅ Expiry functionality and automatic cleanup
- ✅ Size limits and eviction policies
- ✅ Concurrent access and thread safety
- ✅ Context cancellation and timeout handling
- ✅ Memory management and leak prevention
- ✅ Statistics and monitoring functions
- ✅ Edge cases and error conditions

**Benchmark Categories:**
- Single-threaded operations (set, get, mixed)
- Concurrent operations (parallel set/get)
- Memory usage patterns (different cache sizes)
- Cleanup and maintenance operations
- High contention scenarios
- Context-aware operations

## Integration

### Using in the main msg2git project:

```go
import "github.com/msg2git/msg2git/experiments/cache"

// In your service
type Service struct {
    cache *cache.Cache
}

func NewService() *Service {
    return &Service{
        cache: cache.NewWithConfig(1000, 5*time.Minute, 1*time.Minute),
    }
}

func (s *Service) Close() {
    s.cache.Close()
}
```

### Common Use Cases:

**1. GitHub API Response Caching:**
```go
// Cache GitHub API responses to reduce rate limiting
func (s *Service) GetRepository(owner, repo string) (*Repository, error) {
    key := fmt.Sprintf("repo:%s/%s", owner, repo)
    
    if cached, exists := s.cache.Get(key); exists {
        return cached.(*Repository), nil
    }
    
    // Fetch from GitHub API
    repository, err := s.github.GetRepository(owner, repo)
    if err != nil {
        return nil, err
    }
    
    // Cache for 10 minutes
    s.cache.SetWithExpiry(key, repository, 10*time.Minute)
    return repository, nil
}
```

**2. User Session Caching:**
```go
// Cache user sessions to reduce database queries
func (s *Service) GetUserSession(sessionID string) (*UserSession, error) {
    key := fmt.Sprintf("session:%s", sessionID)
    
    if cached, exists := s.cache.Get(key); exists {
        return cached.(*UserSession), nil
    }
    
    // Fetch from database
    session, err := s.db.GetSession(sessionID)
    if err != nil {
        return nil, err
    }
    
    // Cache for 30 minutes
    s.cache.SetWithExpiry(key, session, 30*time.Minute)
    return session, nil
}
```

**3. LLM Response Caching:**
```go
// Cache LLM responses for identical prompts
func (s *Service) GenerateTitle(content string) (string, error) {
    key := fmt.Sprintf("title:%x", md5.Sum([]byte(content)))
    
    if cached, exists := s.cache.Get(key); exists {
        return cached.(string), nil
    }
    
    // Generate with LLM
    title, err := s.llm.GenerateTitle(content)
    if err != nil {
        return "", err
    }
    
    // Cache for 1 hour
    s.cache.SetWithExpiry(key, title, time.Hour)
    return title, nil
}
```

### Production Configuration:

```go
// For high-traffic applications
cache := cache.NewWithConfig(
    10000,              // 10K items max
    15*time.Minute,     // 15min default expiry
    2*time.Minute,      // 2min cleanup interval
)

// For memory-constrained environments
cache := cache.NewWithConfig(
    500,                // 500 items max
    5*time.Minute,      // 5min default expiry
    1*time.Minute,      // 1min cleanup interval
)
```