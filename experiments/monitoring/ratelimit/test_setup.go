package ratelimit

import (
	"sync"
	"time"

	"github.com/msg2git/msg2git/experiments/monitoring/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	testMetricsCollector *metrics.MetricsCollector
	testMetricsOnce      sync.Once
	testRegistry         *prometheus.Registry
)

// getTestMetricsCollector returns a singleton test metrics collector
// This prevents "duplicate metrics collector registration" errors
func getTestMetricsCollector() *metrics.MetricsCollector {
	testMetricsOnce.Do(func() {
		// Create a new registry for tests to avoid conflicts
		testRegistry = prometheus.NewRegistry()
		
		// Create metrics collector with test registry
		testMetricsCollector = metrics.NewMetricsCollectorWithRegistry(testRegistry)
	})
	return testMetricsCollector
}

// createTestLimiter creates a test rate limiter with shared metrics collector
func createTestLimiter() *MemoryRateLimiter {
	config := Config{
		CommandLimit: RateLimit{Requests: 10, Window: time.Second},
		PremiumMultipliers: map[int]float64{
			0: 1.0, // Free
			1: 2.0, // Coffee
			2: 4.0, // Cake
			3: 8.0, // Sponsor
		},
	}
	
	return NewMemoryRateLimiter(config, getTestMetricsCollector())
}