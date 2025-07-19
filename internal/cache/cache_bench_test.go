//go:build benchmark

package cache

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// BenchmarkCache_Set benchmarks setting values in the cache
func BenchmarkCache_Set(b *testing.B) {
	c := New()
	defer c.Close()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i)
		c.Set(key, i)
	}
}

// BenchmarkCache_Get benchmarks getting values from the cache
func BenchmarkCache_Get(b *testing.B) {
	c := New()
	defer c.Close()
	
	// Pre-populate cache
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("key-%d", i)
		c.Set(key, i)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i%1000)
		c.Get(key)
	}
}

// BenchmarkCache_SetGet benchmarks mixed set/get operations
func BenchmarkCache_SetGet(b *testing.B) {
	c := New()
	defer c.Close()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i)
		c.Set(key, i)
		c.Get(key)
	}
}

// BenchmarkCache_Concurrent_Set benchmarks concurrent set operations
func BenchmarkCache_Concurrent_Set(b *testing.B) {
	c := New()
	defer c.Close()
	
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("key-%d", i)
			c.Set(key, i)
			i++
		}
	})
}

// BenchmarkCache_Concurrent_Get benchmarks concurrent get operations
func BenchmarkCache_Concurrent_Get(b *testing.B) {
	c := New()
	defer c.Close()
	
	// Pre-populate cache
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("key-%d", i)
		c.Set(key, i)
	}
	
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("key-%d", i%1000)
			c.Get(key)
			i++
		}
	})
}

// BenchmarkCache_Concurrent_Mixed benchmarks mixed concurrent operations
func BenchmarkCache_Concurrent_Mixed(b *testing.B) {
	c := New()
	defer c.Close()
	
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("key-%d", i)
			if i%2 == 0 {
				c.Set(key, i)
			} else {
				c.Get(key)
			}
			i++
		}
	})
}

// BenchmarkCache_Size_Small benchmarks with small cache size (100 items)
func BenchmarkCache_Size_Small(b *testing.B) {
	c := NewWithConfig(100, time.Hour, time.Minute)
	defer c.Close()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i)
		c.Set(key, i)
	}
}

// BenchmarkCache_Size_Large benchmarks with large cache size (10000 items)
func BenchmarkCache_Size_Large(b *testing.B) {
	c := NewWithConfig(10000, time.Hour, time.Minute)
	defer c.Close()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i)
		c.Set(key, i)
	}
}

// BenchmarkCache_WithExpiry benchmarks operations with expiry checking
func BenchmarkCache_WithExpiry(b *testing.B) {
	c := NewWithConfig(1000, 1*time.Millisecond, time.Minute) // Very short expiry
	defer c.Close()
	
	// Pre-populate cache with items that will expire
	for i := 0; i < 500; i++ {
		key := fmt.Sprintf("key-%d", i)
		c.Set(key, i)
	}
	
	// Wait for items to expire
	time.Sleep(10 * time.Millisecond)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i%500)
		c.Get(key) // This will trigger expiry checks
	}
}

// BenchmarkCache_Keys benchmarks the Keys() method
func BenchmarkCache_Keys(b *testing.B) {
	c := New()
	defer c.Close()
	
	// Pre-populate cache
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("key-%d", i)
		c.Set(key, i)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Keys()
	}
}

// BenchmarkCache_Stats benchmarks the GetStats() method
func BenchmarkCache_Stats(b *testing.B) {
	c := New()
	defer c.Close()
	
	// Pre-populate cache with some expired items
	for i := 0; i < 500; i++ {
		key := fmt.Sprintf("key-%d", i)
		c.Set(key, i)
	}
	for i := 500; i < 1000; i++ {
		key := fmt.Sprintf("key-%d", i)
		c.SetWithExpiry(key, i, -time.Hour) // Expired
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.GetStats()
	}
}

// BenchmarkCache_Cleanup benchmarks the cleanup process
func BenchmarkCache_Cleanup(b *testing.B) {
	c := NewWithConfig(1000, time.Hour, time.Hour) // Disable automatic cleanup
	defer c.Close()
	
	// Pre-populate cache with expired items
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("key-%d", i)
		c.SetWithExpiry(key, i, -time.Hour) // All expired
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.cleanupExpired()
	}
}

// BenchmarkCache_MemoryUsage benchmarks memory usage patterns
func BenchmarkCache_MemoryUsage(b *testing.B) {
	sizes := []int{100, 1000, 10000}
	
	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size_%d", size), func(b *testing.B) {
			c := NewWithConfig(size, time.Hour, time.Minute)
			defer c.Close()
			
			// Create large string values to test memory usage
			largeValue := make([]byte, 1024) // 1KB per value
			for i := range largeValue {
				largeValue[i] = byte(i % 256)
			}
			
			b.ResetTimer()
			for i := 0; i < b.N && i < size; i++ {
				key := fmt.Sprintf("key-%d", i)
				c.Set(key, string(largeValue))
			}
		})
	}
}

// BenchmarkCache_ContextOperations benchmarks context-aware operations
func BenchmarkCache_ContextOperations(b *testing.B) {
	c := New()
	defer c.Close()
	
	// Create context cache
	ctx := context.Background()
	cc := c.WithContext(ctx)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i)
		cc.Set(key, i)
		cc.Get(key)
	}
}

// BenchmarkCache_HighContention benchmarks high contention scenarios
func BenchmarkCache_HighContention(b *testing.B) {
	c := New()
	defer c.Close()
	
	const numGoroutines = 100
	
	b.ResetTimer()
	
	var wg sync.WaitGroup
	start := make(chan struct{})
	
	// Create high contention by having many goroutines access same keys
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start // Wait for all goroutines to be ready
			
			for j := 0; j < b.N/numGoroutines; j++ {
				key := fmt.Sprintf("hot-key-%d", j%10) // Only 10 different keys
				c.Set(key, fmt.Sprintf("value-%d-%d", id, j))
				c.Get(key)
			}
		}(i)
	}
	
	close(start) // Start all goroutines
	wg.Wait()
}

