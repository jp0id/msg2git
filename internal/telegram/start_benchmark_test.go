//go:build benchmark

package telegram

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/msg2git/msg2git/internal/database"
	"golang.org/x/time/rate"
)

// MockSender tracks message sending without making actual API calls
type MockSender struct {
	messageCount int64
	totalLatency int64
	mu           sync.Mutex
	responses    []string
}

func (m *MockSender) SendMessage(chatID int64, text string) error {
	start := time.Now()

	// Simulate minimal processing delay
	time.Sleep(time.Microsecond * 100)

	atomic.AddInt64(&m.messageCount, 1)
	atomic.AddInt64(&m.totalLatency, int64(time.Since(start).Microseconds()))

	m.mu.Lock()
	m.responses = append(m.responses, text)
	m.mu.Unlock()

	return nil
}

func (m *MockSender) GetMessageCount() int64 {
	return atomic.LoadInt64(&m.messageCount)
}

func (m *MockSender) GetAverageLatency() time.Duration {
	count := atomic.LoadInt64(&m.messageCount)
	if count == 0 {
		return 0
	}
	avgMicros := atomic.LoadInt64(&m.totalLatency) / count
	return time.Duration(avgMicros) * time.Microsecond
}

func (m *MockSender) GetResponses() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.responses...)
}

// MockDatabase provides fast in-memory operations
type MockDatabase struct {
	users       sync.Map
	operations  int64
	createDelay time.Duration
}

func NewMockDatabase() *MockDatabase {
	return &MockDatabase{
		createDelay: time.Microsecond * 50, // Simulate 50µs DB operation
	}
}

func (m *MockDatabase) GetOrCreateUser(chatID int64, username string) (*database.User, error) {
	atomic.AddInt64(&m.operations, 1)

	// Simulate database operation delay
	time.Sleep(m.createDelay)

	// Check if user exists
	if user, exists := m.users.Load(chatID); exists {
		return user.(*database.User), nil
	}

	// Create new user
	user := &database.User{
		ID:          int(chatID),
		ChatId:      chatID,
		Username:    username,
		GitHubToken: "",
		GitHubRepo:  "",
		LLMToken:    "",
		CustomFiles: "[]",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	m.users.Store(chatID, user)
	return user, nil
}

func (m *MockDatabase) GetOperationCount() int64 {
	return atomic.LoadInt64(&m.operations)
}

// TestableBot is a minimal bot structure for benchmarking /start command
type TestableBot struct {
	sender        *MockSender
	database      *MockDatabase
	globalLimiter *rate.Limiter
	userLimiters  map[int64]*rate.Limiter
	limiterMu     sync.RWMutex
}

func NewTestableBot() *TestableBot {
	return &TestableBot{
		sender:        &MockSender{},
		database:      NewMockDatabase(),
		globalLimiter: rate.NewLimiter(rate.Limit(5000), 5000), // High limit for testing
		userLimiters:  make(map[int64]*rate.Limiter),
	}
}

// Simulate the core logic of handleStartCommand without external dependencies
func (bot *TestableBot) HandleStartCommand(chatID int64, username string) error {
	// Rate limiting check
	if !bot.globalLimiter.Allow() {
		return fmt.Errorf("rate limit exceeded")
	}

	// Per-user rate limiting
	bot.limiterMu.RLock()
	userLimiter, exists := bot.userLimiters[chatID]
	bot.limiterMu.RUnlock()

	if !exists {
		bot.limiterMu.Lock()
		userLimiter = rate.NewLimiter(rate.Limit(30), 30)
		bot.userLimiters[chatID] = userLimiter
		bot.limiterMu.Unlock()
	}

	if !userLimiter.Allow() {
		return fmt.Errorf("user rate limit exceeded")
	}

	// Database operation (user creation/retrieval)
	user, err := bot.database.GetOrCreateUser(chatID, username)
	if err != nil {
		return fmt.Errorf("database error: %w", err)
	}

	// Generate welcome message (simulate the actual /start response)
	welcomeMsg := fmt.Sprintf(`🎉 <b>Welcome to NoteBook!</b>

👋 Hello %s! NoteBook is a minimalist Telegram bot that turns your messages into GitHub commits.

<b>📝 How it works:</b>
• Send any message to this bot
• Choose where to save it (NOTE, TODO, ISSUE, etc.)
• Your message gets automatically committed to your GitHub repository

<b>🚀 Quick Setup:</b>
1️⃣ <code>/setrepo https://github.com/yourusername/your-repo</code>
2️⃣ <code>/repotoken your_github_personal_access_token</code>
3️⃣ Start sending messages!

<b>📚 Available Commands:</b>
/start - Show this welcome message
/sync - Synchronize issue statuses
/todo - Show latest TODO items  
/issue - Show latest open issues
/customfile - Manage custom files
/insight - View usage statistics and insights
/resetusage - Reset usage counters (paid service)
/coffee - Support the project and unlock premium features
/committer - Set custom commit author (premium)

<b>💡 Pro Tips:</b>
• Use TODO for task items with checkboxes
• Use ISSUE to create GitHub issues automatically  
• Use CUSTOM to organize messages into specific folders
• Premium users get higher limits and more features

Ready to start? Send me any message! 🚀`, user.Username)

	// Send response
	return bot.sender.SendMessage(chatID, welcomeMsg)
}

// HandleStartCommandWithWait simulates the real production behavior using Wait() instead of Allow()
func (bot *TestableBot) HandleStartCommandWithWait(chatID int64, username string) error {
	// Rate limiting check - Wait instead of Allow (production behavior)
	ctx := context.Background()

	// Wait for global rate limiter
	if err := bot.globalLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("global rate limiter error: %w", err)
	}

	// Per-user rate limiting with Wait
	bot.limiterMu.RLock()
	userLimiter, exists := bot.userLimiters[chatID]
	bot.limiterMu.RUnlock()

	if !exists {
		bot.limiterMu.Lock()
		userLimiter = rate.NewLimiter(rate.Limit(30), 30)
		bot.userLimiters[chatID] = userLimiter
		bot.limiterMu.Unlock()
	}

	// Wait for user rate limiter
	if err := userLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("user rate limiter error: %w", err)
	}

	// Database operation (user creation/retrieval)
	user, err := bot.database.GetOrCreateUser(chatID, username)
	if err != nil {
		return fmt.Errorf("database error: %w", err)
	}

	// Generate welcome message (simulate the actual /start response)
	welcomeMsg := fmt.Sprintf(`🎉 <b>Welcome to NoteBook!</b>

👋 Hello %s! NoteBook is a minimalist Telegram bot that turns your messages into GitHub commits.

<b>📝 How it works:</b>
• Send any message to this bot
• Choose where to save it (NOTE, TODO, ISSUE, etc.)
• Your message gets automatically committed to your GitHub repository

<b>🚀 Quick Setup:</b>
1️⃣ <code>/setrepo https://github.com/yourusername/your-repo</code>
2️⃣ <code>/repotoken your_github_personal_access_token</code>
3️⃣ Start sending messages!

<b>📚 Available Commands:</b>
/start - Show this welcome message
/sync - Synchronize issue statuses
/todo - Show latest TODO items  
/issue - Show latest open issues
/customfile - Manage custom files
/insight - Check repository status and usage
/insight - View usage statistics and insights
/resetusage - Reset usage counters (paid service)
/coffee - Support the project and unlock premium features
/committer - Set custom commit author (premium)

<b>💡 Pro Tips:</b>
• Use TODO for task items with checkboxes
• Use ISSUE to create GitHub issues automatically  
• Use CUSTOM to organize messages into specific folders
• Premium users get higher limits and more features

Ready to start? Send me any message! 🚀`, user.Username)

	// Send response
	return bot.sender.SendMessage(chatID, welcomeMsg)
}

// BenchmarkStartCommandConcurrency tests 10000 concurrent /start requests
func BenchmarkStartCommandConcurrency(b *testing.B) {
	const targetRequests = 10000

	if b.N < targetRequests {
		b.N = targetRequests
	}

	bot := NewTestableBot()

	b.ResetTimer()
	startTime := time.Now()

	var wg sync.WaitGroup
	var successCount int64
	var errorCount int64

	// Launch concurrent requests
	for i := 0; i < b.N; i++ {
		wg.Add(1)

		go func(requestID int) {
			defer wg.Done()

			chatID := int64(10000 + requestID)
			username := fmt.Sprintf("user%d", requestID)

			err := bot.HandleStartCommand(chatID, username)
			if err != nil {
				atomic.AddInt64(&errorCount, 1)
				if strings.Contains(err.Error(), "rate limit") {
					// Rate limiting is expected behavior under high load
				} else {
					b.Logf("Request %d failed: %v", requestID, err)
				}
			} else {
				atomic.AddInt64(&successCount, 1)
			}
		}(i)
	}

	// Wait for all requests to complete
	wg.Wait()

	totalTime := time.Since(startTime)
	b.StopTimer()

	// Collect metrics
	successful := atomic.LoadInt64(&successCount)
	failed := atomic.LoadInt64(&errorCount)
	totalMessages := bot.sender.GetMessageCount()
	avgLatency := bot.sender.GetAverageLatency()
	dbOps := bot.database.GetOperationCount()

	// Calculate performance metrics
	requestsPerSecond := float64(successful) / totalTime.Seconds()
	avgResponseTime := float64(totalTime.Nanoseconds()) / float64(successful) / 1e6 // milliseconds

	// Report metrics
	b.ReportMetric(float64(successful), "successful_requests")
	b.ReportMetric(float64(failed), "failed_requests")
	b.ReportMetric(float64(totalMessages), "messages_sent")
	b.ReportMetric(float64(dbOps), "database_operations")
	b.ReportMetric(float64(totalTime.Milliseconds()), "total_time_ms")
	b.ReportMetric(avgResponseTime, "avg_response_time_ms")
	b.ReportMetric(requestsPerSecond, "requests_per_second")
	b.ReportMetric(float64(avgLatency.Microseconds()), "avg_send_latency_us")

	// Log detailed results
	b.Logf("\n" + strings.Repeat("=", 60))
	b.Logf("📊 CONCURRENCY BENCHMARK RESULTS - /start command")
	b.Logf(strings.Repeat("=", 60))
	b.Logf("🎯 Target requests: %d", targetRequests)
	b.Logf("✅ Successful requests: %d", successful)
	b.Logf("❌ Failed requests: %d", failed)
	b.Logf("📈 Success rate: %.2f%%", float64(successful)/float64(targetRequests)*100)
	b.Logf("⏱️  Total time: %v", totalTime)
	b.Logf("🚀 Requests per second: %.2f", requestsPerSecond)
	b.Logf("⚡ Average response time: %.2f ms", avgResponseTime)
	b.Logf("📨 Messages sent: %d", totalMessages)
	b.Logf("🗄️  Database operations: %d", dbOps)
	b.Logf("🔄 DB ops per request: %.2f", float64(dbOps)/float64(successful))
	b.Logf("📡 Average send latency: %v", avgLatency)

	// Performance analysis
	b.Logf("\n📋 PERFORMANCE ANALYSIS:")
	if requestsPerSecond > 1000 {
		b.Logf("🟢 Excellent: >1000 req/s")
	} else if requestsPerSecond > 500 {
		b.Logf("🟡 Good: >500 req/s")
	} else if requestsPerSecond > 100 {
		b.Logf("🟠 Moderate: >100 req/s")
	} else {
		b.Logf("🔴 Needs optimization: <100 req/s")
	}

	if avgResponseTime < 10 {
		b.Logf("🟢 Response time: Excellent (<10ms)")
	} else if avgResponseTime < 50 {
		b.Logf("🟡 Response time: Good (<50ms)")
	} else if avgResponseTime < 100 {
		b.Logf("🟠 Response time: Moderate (<100ms)")
	} else {
		b.Logf("🔴 Response time: Needs optimization (>100ms)")
	}

	b.Logf(strings.Repeat("=", 60))
}

// BenchmarkStartCommandScaling tests performance at different concurrency levels
func BenchmarkStartCommandScaling(b *testing.B) {
	concurrencyLevels := []int{1, 10, 50, 100, 500, 1000, 2500, 5000, 10000}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("Users-%d", concurrency), func(b *testing.B) {
			bot := NewTestableBot()

			b.ResetTimer()
			startTime := time.Now()

			var wg sync.WaitGroup
			var successCount int64
			var errorCount int64

			for i := 0; i < concurrency; i++ {
				wg.Add(1)

				go func(requestID int) {
					defer wg.Done()

					chatID := int64(20000 + requestID)
					username := fmt.Sprintf("user%d", requestID)

					err := bot.HandleStartCommand(chatID, username)
					if err != nil {
						atomic.AddInt64(&errorCount, 1)
					} else {
						atomic.AddInt64(&successCount, 1)
					}
				}(i)
			}

			wg.Wait()
			totalTime := time.Since(startTime)
			b.StopTimer()

			successful := atomic.LoadInt64(&successCount)
			failed := atomic.LoadInt64(&errorCount)
			requestsPerSecond := float64(successful) / totalTime.Seconds()
			avgResponseTime := float64(totalTime.Milliseconds()) / float64(successful)

			b.ReportMetric(float64(successful), "successful")
			b.ReportMetric(float64(failed), "failed")
			b.ReportMetric(requestsPerSecond, "req_per_sec")
			b.ReportMetric(avgResponseTime, "avg_ms")

			b.Logf("👥 %d users: %d✅ %d❌ | %.1f req/s | %.2f ms avg",
				concurrency, successful, failed, requestsPerSecond, avgResponseTime)
		})
	}
}

// TestStartCommandCorrectness verifies the benchmark logic is correct
func TestStartCommandCorrectness(t *testing.T) {
	bot := NewTestableBot()

	// Test single request
	err := bot.HandleStartCommand(12345, "testuser")
	if err != nil {
		t.Fatalf("Failed to handle start command: %v", err)
	}

	// Verify message was sent
	if bot.sender.GetMessageCount() != 1 {
		t.Errorf("Expected 1 message sent, got %d", bot.sender.GetMessageCount())
	}

	// Verify database operation occurred
	if bot.database.GetOperationCount() != 1 {
		t.Errorf("Expected 1 database operation, got %d", bot.database.GetOperationCount())
	}

	// Verify response content
	responses := bot.sender.GetResponses()
	if len(responses) != 1 {
		t.Fatalf("Expected 1 response, got %d", len(responses))
	}

	if !strings.Contains(responses[0], "Welcome to NoteBook") {
		t.Error("Response doesn't contain welcome message")
	}

	if !strings.Contains(responses[0], "testuser") {
		t.Error("Response doesn't contain username")
	}

	t.Logf("✅ Correctness test passed - response length: %d chars", len(responses[0]))
}

// BenchmarkStartCommandSingle measures single-threaded performance
func BenchmarkStartCommandSingle(b *testing.B) {
	bot := NewTestableBot()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		chatID := int64(30000 + i)
		username := fmt.Sprintf("user%d", i)

		err := bot.HandleStartCommand(chatID, username)
		if err != nil {
			b.Fatalf("Request %d failed: %v", i, err)
		}
	}

	b.StopTimer()

	// Report single-threaded performance
	totalMessages := bot.sender.GetMessageCount()
	dbOps := bot.database.GetOperationCount()

	b.ReportMetric(float64(totalMessages), "messages_sent")
	b.ReportMetric(float64(dbOps), "database_operations")
}

// BenchmarkStartCommandWithWait tests 10000 concurrent /start requests using Wait() for 100% success rate
func BenchmarkStartCommandWithWait(b *testing.B) {
	testCases := []struct {
		name     string
		requests int
	}{
		{"1K-Requests", 1000},
		{"5K-Requests", 5000},
		{"10K-Requests", 10000},
		{"25K-Requests", 25000},
		{"50K-Requests", 50000},
		{"100K-Requests", 100000},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			bot := NewTestableBot()

			b.ResetTimer()
			startTime := time.Now()

			var wg sync.WaitGroup
			var successCount int64
			var errorCount int64

			// Launch concurrent requests
			for i := 0; i < tc.requests; i++ {
				wg.Add(1)

				go func(requestID int) {
					defer wg.Done()

					chatID := int64(50000 + requestID)
					username := fmt.Sprintf("user%d", requestID)

					err := bot.HandleStartCommandWithWait(chatID, username)
					if err != nil {
						atomic.AddInt64(&errorCount, 1)
						b.Logf("Request %d failed: %v", requestID, err)
					} else {
						atomic.AddInt64(&successCount, 1)
					}
				}(i)
			}

			// Wait for all requests to complete
			wg.Wait()

			totalTime := time.Since(startTime)
			b.StopTimer()

			// Collect metrics
			successful := atomic.LoadInt64(&successCount)
			failed := atomic.LoadInt64(&errorCount)
			totalMessages := bot.sender.GetMessageCount()
			avgLatency := bot.sender.GetAverageLatency()
			dbOps := bot.database.GetOperationCount()

			// Calculate performance metrics
			requestsPerSecond := float64(successful) / totalTime.Seconds()
			avgResponseTime := float64(totalTime.Nanoseconds()) / float64(successful) / 1e6 // milliseconds

			// Report metrics
			b.ReportMetric(float64(successful), "successful_requests")
			b.ReportMetric(float64(failed), "failed_requests")
			b.ReportMetric(float64(totalMessages), "messages_sent")
			b.ReportMetric(float64(dbOps), "database_operations")
			b.ReportMetric(float64(totalTime.Milliseconds()), "total_time_ms")
			b.ReportMetric(avgResponseTime, "avg_response_time_ms")
			b.ReportMetric(requestsPerSecond, "requests_per_second")
			b.ReportMetric(float64(avgLatency.Microseconds()), "avg_send_latency_us")

			// Log detailed results
			b.Logf("\n" + strings.Repeat("=", 70))
			b.Logf("📊 WAIT() BENCHMARK RESULTS - %s", tc.name)
			b.Logf(strings.Repeat("=", 70))
			b.Logf("🎯 Target requests: %d", tc.requests)
			b.Logf("✅ Successful requests: %d", successful)
			b.Logf("❌ Failed requests: %d", failed)
			b.Logf("📈 Success rate: %.2f%%", float64(successful)/float64(tc.requests)*100)
			b.Logf("⏱️  Total processing time: %v", totalTime)
			b.Logf("🚀 Effective throughput: %.2f req/s", requestsPerSecond)
			b.Logf("⚡ Average response time: %.2f ms", avgResponseTime)
			b.Logf("📨 Messages sent: %d", totalMessages)
			b.Logf("🗄️  Database operations: %d", dbOps)
			b.Logf("🔄 DB ops per request: %.2f", float64(dbOps)/float64(successful))
			b.Logf("📡 Average send latency: %v", avgLatency)

			// Performance analysis
			b.Logf("\n📋 WAIT() PERFORMANCE ANALYSIS:")
			if successful == int64(tc.requests) {
				b.Logf("🟢 Perfect Success Rate: 100%% (%d/%d)", successful, tc.requests)
			} else {
				b.Logf("🟡 Partial Success Rate: %.2f%% (%d/%d)", float64(successful)/float64(tc.requests)*100, successful, tc.requests)
			}

			if totalTime.Seconds() < 1 {
				b.Logf("🟢 Excellent Speed: Completed in <1 second")
			} else if totalTime.Seconds() < 5 {
				b.Logf("🟡 Good Speed: Completed in %.2f seconds", totalTime.Seconds())
			} else if totalTime.Seconds() < 30 {
				b.Logf("🟠 Moderate Speed: Completed in %.2f seconds", totalTime.Seconds())
			} else {
				b.Logf("🔴 Slow: Took %.2f seconds (rate limiting effect)", totalTime.Seconds())
			}

			// Rate limiting analysis
			theoreticalMinTime := float64(tc.requests) / 5000.0 // Based on 5000 req/s global limit
			actualTime := totalTime.Seconds()
			efficiency := theoreticalMinTime / actualTime * 100

			b.Logf("📊 Rate Limiting Analysis:")
			b.Logf("   Theoretical min time (5000 req/s): %.3f seconds", theoreticalMinTime)
			b.Logf("   Actual time: %.3f seconds", actualTime)
			b.Logf("   Rate limiting efficiency: %.1f%%", efficiency)

			b.Logf(strings.Repeat("=", 70))
		})
	}
}

// BenchmarkMaxThroughputDiscovery tests extreme scale to find system limits
func BenchmarkMaxThroughputDiscovery(b *testing.B) {
	extremeTestCases := []struct {
		name     string
		requests int
		timeout  time.Duration
	}{
		{"250K-Requests", 250000, 30 * time.Second},
		{"500K-Requests", 500000, 60 * time.Second},
		{"1M-Requests", 1000000, 120 * time.Second},
	}

	for _, tc := range extremeTestCases {
		b.Run(tc.name, func(b *testing.B) {
			bot := NewTestableBot()

			b.ResetTimer()
			startTime := time.Now()

			var wg sync.WaitGroup
			var successCount int64
			var errorCount int64
			var timeoutCount int64

			// Create a context with timeout to prevent infinite hangs
			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()

			// Launch concurrent requests
			for i := 0; i < tc.requests; i++ {
				wg.Add(1)

				go func(requestID int) {
					defer wg.Done()

					chatID := int64(100000 + requestID)
					username := fmt.Sprintf("user%d", requestID)

					// Check if context is already cancelled
					select {
					case <-ctx.Done():
						atomic.AddInt64(&timeoutCount, 1)
						return
					default:
					}

					err := bot.HandleStartCommandWithWait(chatID, username)
					if err != nil {
						if ctx.Err() != nil {
							atomic.AddInt64(&timeoutCount, 1)
						} else {
							atomic.AddInt64(&errorCount, 1)
							if atomic.LoadInt64(&errorCount) <= 10 { // Log first 10 errors only
								b.Logf("Request %d failed: %v", requestID, err)
							}
						}
					} else {
						atomic.AddInt64(&successCount, 1)
					}
				}(i)
			}

			// Wait for completion or timeout
			done := make(chan struct{})
			go func() {
				wg.Wait()
				close(done)
			}()

			select {
			case <-done:
				// All requests completed
			case <-ctx.Done():
				b.Logf("⚠️ Test timed out after %v", tc.timeout)
			}

			totalTime := time.Since(startTime)
			b.StopTimer()

			// Collect metrics
			successful := atomic.LoadInt64(&successCount)
			failed := atomic.LoadInt64(&errorCount)
			timedOut := atomic.LoadInt64(&timeoutCount)
			totalMessages := bot.sender.GetMessageCount()
			avgLatency := bot.sender.GetAverageLatency()
			dbOps := bot.database.GetOperationCount()

			// Calculate performance metrics
			requestsPerSecond := float64(successful) / totalTime.Seconds()
			avgResponseTime := float64(totalTime.Nanoseconds()) / float64(successful) / 1e6 // milliseconds

			// Report metrics
			b.ReportMetric(float64(successful), "successful_requests")
			b.ReportMetric(float64(failed), "failed_requests")
			b.ReportMetric(float64(timedOut), "timed_out_requests")
			b.ReportMetric(float64(totalMessages), "messages_sent")
			b.ReportMetric(float64(dbOps), "database_operations")
			b.ReportMetric(float64(totalTime.Milliseconds()), "total_time_ms")
			b.ReportMetric(avgResponseTime, "avg_response_time_ms")
			b.ReportMetric(requestsPerSecond, "requests_per_second")
			b.ReportMetric(float64(avgLatency.Microseconds()), "avg_send_latency_us")

			// Log detailed results
			b.Logf("\n" + strings.Repeat("=", 80))
			b.Logf("🚀 EXTREME SCALE BENCHMARK RESULTS - %s", tc.name)
			b.Logf(strings.Repeat("=", 80))
			b.Logf("🎯 Target requests: %d", tc.requests)
			b.Logf("✅ Successful requests: %d", successful)
			b.Logf("❌ Failed requests: %d", failed)
			b.Logf("⏰ Timed out requests: %d", timedOut)
			b.Logf("📈 Success rate: %.2f%%", float64(successful)/float64(tc.requests)*100)
			b.Logf("⏱️  Total processing time: %v (limit: %v)", totalTime, tc.timeout)
			b.Logf("🚀 Achieved throughput: %.2f req/s", requestsPerSecond)
			b.Logf("⚡ Average response time: %.2f ms", avgResponseTime)
			b.Logf("📨 Messages sent: %d", totalMessages)
			b.Logf("🗄️  Database operations: %d", dbOps)
			b.Logf("🔄 DB ops per request: %.2f", float64(dbOps)/float64(successful))
			b.Logf("📡 Average send latency: %v", avgLatency)

			// System stress analysis
			b.Logf("\n🔥 SYSTEM STRESS ANALYSIS:")

			completionRate := float64(successful+failed) / float64(tc.requests) * 100
			if completionRate >= 99 {
				b.Logf("🟢 Excellent Completion: %.1f%% within time limit", completionRate)
			} else if completionRate >= 90 {
				b.Logf("🟡 Good Completion: %.1f%% within time limit", completionRate)
			} else if completionRate >= 50 {
				b.Logf("🟠 Partial Completion: %.1f%% within time limit", completionRate)
			} else {
				b.Logf("🔴 Poor Completion: %.1f%% within time limit - System overloaded", completionRate)
			}

			if totalTime >= tc.timeout {
				b.Logf("🔴 TIMEOUT: System could not handle %d requests within %v", tc.requests, tc.timeout)
				b.Logf("🎯 LIMIT REACHED: Maximum sustainable load appears to be below %d requests", tc.requests)
			} else if successful == int64(tc.requests) {
				b.Logf("🟢 EXCELLENT: All %d requests completed successfully", tc.requests)
				b.Logf("🚀 CAPACITY: System can handle at least %d concurrent requests", tc.requests)
			} else {
				b.Logf("🟡 PARTIAL: %d/%d requests completed", successful, tc.requests)
			}

			// Throughput analysis
			theoreticalMax := 5000.0 // Based on rate limit
			actualThroughput := requestsPerSecond
			efficiency := (actualThroughput / theoreticalMax) * 100

			b.Logf("\n📊 THROUGHPUT ANALYSIS:")
			b.Logf("   Rate limit ceiling: %.0f req/s", theoreticalMax)
			b.Logf("   Achieved throughput: %.1f req/s", actualThroughput)
			b.Logf("   System efficiency: %.1f%%", efficiency)

			if efficiency > 100 {
				b.Logf("⚡ BURST PERFORMANCE: Exceeded rate limit due to burst capacity!")
			} else if efficiency > 80 {
				b.Logf("🟢 HIGH EFFICIENCY: Near optimal rate limit utilization")
			} else if efficiency > 50 {
				b.Logf("🟡 MODERATE EFFICIENCY: Rate limiting working as designed")
			} else {
				b.Logf("🔴 LOW EFFICIENCY: System bottlenecks other than rate limiting")
			}

			b.Logf(strings.Repeat("=", 80))
		})
	}
}

// BenchmarkPhotoProgressBarPerformance tests photo processing with progress bar updates
func BenchmarkPhotoProgressBarPerformance(b *testing.B) {
	testCases := []struct {
		name              string
		progressUpdates   int
		concurrentPhotos  int
		enableProgressBar bool
	}{
		{"No-Progress-1Photo", 0, 1, false},
		{"No-Progress-10Photos", 0, 10, false},
		{"No-Progress-100Photos", 0, 100, false},
		{"Progress-3Updates-1Photo", 3, 1, true},
		{"Progress-5Updates-1Photo", 5, 1, true},
		{"Progress-3Updates-10Photos", 3, 10, true},
		{"Progress-5Updates-10Photos", 5, 10, true},
		{"Progress-3Updates-100Photos", 3, 100, true},
		{"Progress-5Updates-100Photos", 5, 100, true},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			bot := NewTestableBot()

			b.ResetTimer()
			startTime := time.Now()

			var wg sync.WaitGroup
			var successCount int64
			var errorCount int64
			var totalProgressUpdates int64

			// Launch concurrent photo processing
			for i := 0; i < tc.concurrentPhotos; i++ {
				wg.Add(1)

				go func(photoID int) {
					defer wg.Done()

					chatID := int64(200000 + photoID)
					messageID := photoID + 1000

					err := bot.SimulatePhotoProcessingWithProgressBar(chatID, messageID, tc.progressUpdates, tc.enableProgressBar)
					if err != nil {
						atomic.AddInt64(&errorCount, 1)
						if atomic.LoadInt64(&errorCount) <= 5 {
							b.Logf("Photo %d failed: %v", photoID, err)
						}
					} else {
						atomic.AddInt64(&successCount, 1)
					}

					if tc.enableProgressBar {
						atomic.AddInt64(&totalProgressUpdates, int64(tc.progressUpdates))
					}
				}(i)
			}

			// Wait for all photo processing to complete
			wg.Wait()

			totalTime := time.Since(startTime)
			b.StopTimer()

			// Collect metrics
			successful := atomic.LoadInt64(&successCount)
			failed := atomic.LoadInt64(&errorCount)
			progressUpdates := atomic.LoadInt64(&totalProgressUpdates)
			totalMessages := bot.sender.GetMessageCount()
			avgLatency := bot.sender.GetAverageLatency()
			dbOps := bot.database.GetOperationCount()

			// Calculate performance metrics
			photosPerSecond := float64(successful) / totalTime.Seconds()
			avgProcessingTime := float64(totalTime.Nanoseconds()) / float64(successful) / 1e6 // milliseconds
			progressUpdatesPerSecond := float64(progressUpdates) / totalTime.Seconds()

			// Report metrics
			b.ReportMetric(float64(successful), "successful_photos")
			b.ReportMetric(float64(failed), "failed_photos")
			b.ReportMetric(float64(progressUpdates), "progress_updates")
			b.ReportMetric(float64(totalMessages), "total_messages")
			b.ReportMetric(float64(dbOps), "database_operations")
			b.ReportMetric(float64(totalTime.Milliseconds()), "total_time_ms")
			b.ReportMetric(avgProcessingTime, "avg_processing_time_ms")
			b.ReportMetric(photosPerSecond, "photos_per_second")
			b.ReportMetric(progressUpdatesPerSecond, "progress_updates_per_second")
			b.ReportMetric(float64(avgLatency.Microseconds()), "avg_send_latency_us")

			// Log detailed results
			b.Logf("\n" + strings.Repeat("=", 75))
			b.Logf("📸 PHOTO PROGRESS BAR BENCHMARK - %s", tc.name)
			b.Logf(strings.Repeat("=", 75))
			b.Logf("📸 Photos processed: %d", successful)
			b.Logf("❌ Failed photos: %d", failed)
			b.Logf("📊 Progress updates: %d", progressUpdates)
			b.Logf("📈 Success rate: %.2f%%", float64(successful)/float64(tc.concurrentPhotos)*100)
			b.Logf("⏱️  Total processing time: %v", totalTime)
			b.Logf("🚀 Photos per second: %.2f", photosPerSecond)
			b.Logf("⚡ Average processing time: %.2f ms", avgProcessingTime)
			b.Logf("📨 Total messages sent: %d", totalMessages)
			b.Logf("🔄 Progress updates per second: %.2f", progressUpdatesPerSecond)
			b.Logf("🗄️  Database operations: %d", dbOps)
			b.Logf("📡 Average send latency: %v", avgLatency)

			// Progress bar efficiency analysis
			b.Logf("\n📋 PROGRESS BAR EFFICIENCY ANALYSIS:")

			if tc.enableProgressBar {
				messagesPerPhoto := float64(totalMessages) / float64(successful)
				expectedMessages := float64(tc.progressUpdates + 1) // progress updates + final result
				efficiency := expectedMessages / messagesPerPhoto * 100

				b.Logf("   Expected messages per photo: %.1f", expectedMessages)
				b.Logf("   Actual messages per photo: %.1f", messagesPerPhoto)
				b.Logf("   Message efficiency: %.1f%%", efficiency)

				if tc.progressUpdates <= 5 {
					b.Logf("🟢 GOOD: Progress updates (%d) follow best practice (≤5)", tc.progressUpdates)
				} else {
					b.Logf("🟡 WARNING: Too many progress updates (%d), consider reducing to ≤5", tc.progressUpdates)
				}

				progressOverhead := float64(progressUpdates) / float64(successful) * 200 // 200ms delay per update
				b.Logf("   Progress bar overhead: %.0f ms per photo", progressOverhead)

				if progressOverhead < 1000 { // Less than 1 second overhead
					b.Logf("🟢 LOW OVERHEAD: Progress bar adds %.0f ms per photo", progressOverhead)
				} else if progressOverhead < 3000 { // Less than 3 seconds
					b.Logf("🟡 MODERATE OVERHEAD: Progress bar adds %.0f ms per photo", progressOverhead)
				} else {
					b.Logf("🔴 HIGH OVERHEAD: Progress bar adds %.0f ms per photo", progressOverhead)
				}
			} else {
				b.Logf("🚫 NO PROGRESS BAR: Baseline performance measurement")
			}

			// Performance classification
			if photosPerSecond > 100 {
				b.Logf("🟢 EXCELLENT: >100 photos/second")
			} else if photosPerSecond > 50 {
				b.Logf("🟡 GOOD: >50 photos/second")
			} else if photosPerSecond > 10 {
				b.Logf("🟠 MODERATE: >10 photos/second")
			} else {
				b.Logf("🔴 SLOW: <10 photos/second - needs optimization")
			}

			b.Logf(strings.Repeat("=", 75))
		})
	}
}

// SimulatePhotoProcessingWithProgressBar simulates photo processing with optional progress bar updates
func (bot *TestableBot) SimulatePhotoProcessingWithProgressBar(chatID int64, messageID int, progressUpdates int, enableProgressBar bool) error {
	// Simulate photo processing steps with progress updates

	if enableProgressBar && progressUpdates > 0 {
		// Step 1: Starting
		bot.updateProgressMessage(chatID, messageID, 10, "🔄 Starting photo processing...")

		// Additional progress steps based on progressUpdates
		for i := 1; i < progressUpdates; i++ {
			percentage := 10 + (60 * i / (progressUpdates - 1)) // 10% to 70%
			var message string
			switch i {
			case 1:
				message = "📊 Checking repository..."
			case 2:
				message = "🧠 LLM processing..."
			case 3:
				message = "📝 Saving to GitHub..."
			case 4:
				message = "🔄 Finalizing..."
			default:
				message = fmt.Sprintf("🔄 Processing step %d...", i)
			}
			bot.updateProgressMessage(chatID, messageID, percentage, message)
		}

		// Final step: Completion
		bot.updateProgressMessage(chatID, messageID, 100, "✅ Photo saved!")
	}

	// Simulate database operation (user creation/retrieval)
	user, err := bot.database.GetOrCreateUser(chatID, fmt.Sprintf("user%d", chatID))
	if err != nil {
		return fmt.Errorf("database error: %w", err)
	}

	// Simulate final success message
	successMsg := fmt.Sprintf("✅ Photo processed successfully for %s", user.Username)
	return bot.sender.SendMessage(chatID, successMsg)
}

// SimulatePhotoProcessingWithConcurrentProgressBar simulates photo processing with concurrent progress bar updates
func (bot *TestableBot) SimulatePhotoProcessingWithConcurrentProgressBar(chatID int64, messageID int, progressUpdates int, enableProgressBar bool) error {
	var progressTracker *ProgressTracker

	if enableProgressBar && progressUpdates > 0 {
		// Create context for the operation
		ctx := context.Background()

		// Create concurrent progress tracker
		progressTracker = bot.NewProgressTracker(ctx, chatID, messageID)
		if progressTracker != nil {
			defer progressTracker.Finish() // Ensure cleanup

			// Send initial progress update (non-blocking)
			progressTracker.UpdateProgress(10, "🔄 Starting photo processing...")
		}
	}

	// Simulate main processing work (this runs concurrently with progress updates)

	// Step 1: Repository checking (simulate 100ms work)
	time.Sleep(100 * time.Millisecond)
	if progressTracker != nil {
		progressTracker.UpdateProgress(30, "📊 Checking repository...")
	}

	// Step 2: LLM processing (simulate 150ms work)
	time.Sleep(150 * time.Millisecond)
	if progressTracker != nil && progressUpdates >= 3 {
		progressTracker.UpdateProgress(50, "🧠 LLM processing...")
	}

	// Step 3: GitHub operations (simulate 200ms work)
	time.Sleep(200 * time.Millisecond)
	if progressTracker != nil && progressUpdates >= 4 {
		progressTracker.UpdateProgress(70, "📝 Saving to GitHub...")
	}

	// Step 4: Finalization (simulate 100ms work)
	time.Sleep(100 * time.Millisecond)
	if progressTracker != nil && progressUpdates >= 5 {
		progressTracker.UpdateProgress(90, "🔄 Finalizing...")
	}

	// Simulate database operation (user creation/retrieval)
	user, err := bot.database.GetOrCreateUser(chatID, fmt.Sprintf("user%d", chatID))
	if err != nil {
		return fmt.Errorf("database error: %w", err)
	}

	// Final progress update
	if progressTracker != nil {
		progressTracker.UpdateProgress(100, "✅ Photo saved!")
	}

	// Simulate final success message
	successMsg := fmt.Sprintf("✅ Photo processed successfully for %s", user.Username)
	return bot.sender.SendMessage(chatID, successMsg)
}

// NewProgressTracker creates a mock progress tracker for testing
func (bot *TestableBot) NewProgressTracker(ctx context.Context, chatID int64, messageID int) *ProgressTracker {
	if messageID <= 0 {
		return nil
	}

	childCtx, cancel := context.WithCancel(ctx)

	tracker := &ProgressTracker{
		bot:        &Bot{}, // Mock bot - not actually used in test
		chatID:     chatID,
		messageID:  messageID,
		ctx:        childCtx,
		cancel:     cancel,
		progressCh: make(chan ProgressUpdate, 10),
		doneCh:     make(chan struct{}),
	}

	// Start the mock progress update worker
	go bot.mockProgressUpdateWorker(tracker)

	return tracker
}

// mockProgressUpdateWorker simulates the progress update worker for testing
func (bot *TestableBot) mockProgressUpdateWorker(tracker *ProgressTracker) {
	defer func() {
		if r := recover(); r != nil {
			// Panic recovery for test safety
		}
		close(tracker.doneCh)
	}()

	for {
		select {
		case <-tracker.ctx.Done():
			return

		case update, ok := <-tracker.progressCh:
			if !ok {
				return
			}

			// Simulate the progress update (faster for testing)
			func() {
				defer func() {
					if r := recover(); r != nil {
						// Ignore panics in test
					}
				}()

				// Create progress bar text
				progressText := bot.createProgressBarWithText(update.Percentage, update.Message)

				// Send the progress update message (non-blocking for test)
				go bot.sender.SendMessage(tracker.chatID, progressText)

				// Reduced delay for testing (50ms instead of 100ms)
				select {
				case <-time.After(50 * time.Millisecond):
				case <-tracker.ctx.Done():
					return
				}
			}()
		}
	}
}

// updateProgressMessage simulates the progress bar update functionality
func (bot *TestableBot) updateProgressMessage(chatID int64, messageID int, percentage int, message string) {
	if messageID <= 0 {
		return
	}

	// Create progress bar text (simulating the real function)
	progressText := bot.createProgressBarWithText(percentage, message)

	// Simulate editing the message (this would be rateLimitedSend in production)
	bot.sender.SendMessage(chatID, progressText)

	// Add the same delay as production code
	time.Sleep(200 * time.Millisecond)
}

// createProgressBarWithText creates a visual progress bar with percentage (simulating real function)
func (bot *TestableBot) createProgressBarWithText(percentage int, message string) string {
	const barLength = 10
	filled := (percentage * barLength) / 100
	if filled > barLength {
		filled = barLength
	}

	bar := ""
	for i := 0; i < barLength; i++ {
		if i < filled {
			bar += "▓"
		} else {
			bar += "░"
		}
	}

	return fmt.Sprintf("%s\n[%s] %d%%", message, bar, percentage)
}

// BenchmarkConcurrentVsBlockingProgressBar compares concurrent vs blocking progress bar performance
func BenchmarkConcurrentVsBlockingProgressBar(b *testing.B) {
	testCases := []struct {
		name             string
		progressUpdates  int
		concurrentPhotos int
		useConcurrent    bool
	}{
		// Blocking (original) approach
		{"Blocking-3Updates-1Photo", 3, 1, false},
		{"Blocking-5Updates-1Photo", 5, 1, false},
		{"Blocking-3Updates-10Photos", 3, 10, false},
		{"Blocking-5Updates-10Photos", 5, 10, false},

		// Concurrent (new) approach
		{"Concurrent-3Updates-1Photo", 3, 1, true},
		{"Concurrent-5Updates-1Photo", 5, 1, true},
		{"Concurrent-3Updates-10Photos", 3, 10, true},
		{"Concurrent-5Updates-10Photos", 5, 10, true},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			bot := NewTestableBot()

			b.ResetTimer()
			startTime := time.Now()

			var wg sync.WaitGroup
			var successCount int64
			var errorCount int64
			var totalProgressUpdates int64

			// Launch concurrent photo processing
			for i := 0; i < tc.concurrentPhotos; i++ {
				wg.Add(1)

				go func(photoID int) {
					defer wg.Done()

					chatID := int64(300000 + photoID)
					messageID := photoID + 2000

					var err error
					if tc.useConcurrent {
						err = bot.SimulatePhotoProcessingWithConcurrentProgressBar(chatID, messageID, tc.progressUpdates, true)
					} else {
						err = bot.SimulatePhotoProcessingWithProgressBar(chatID, messageID, tc.progressUpdates, true)
					}

					if err != nil {
						atomic.AddInt64(&errorCount, 1)
						if atomic.LoadInt64(&errorCount) <= 3 {
							b.Logf("Photo %d failed: %v", photoID, err)
						}
					} else {
						atomic.AddInt64(&successCount, 1)
					}

					atomic.AddInt64(&totalProgressUpdates, int64(tc.progressUpdates))
				}(i)
			}

			// Wait for all photo processing to complete
			wg.Wait()

			totalTime := time.Since(startTime)
			b.StopTimer()

			// Collect metrics
			successful := atomic.LoadInt64(&successCount)
			failed := atomic.LoadInt64(&errorCount)
			progressUpdates := atomic.LoadInt64(&totalProgressUpdates)
			totalMessages := bot.sender.GetMessageCount()
			avgLatency := bot.sender.GetAverageLatency()
			dbOps := bot.database.GetOperationCount()

			// Calculate performance metrics
			photosPerSecond := float64(successful) / totalTime.Seconds()
			avgProcessingTime := float64(totalTime.Nanoseconds()) / float64(successful) / 1e6 // milliseconds
			progressUpdatesPerSecond := float64(progressUpdates) / totalTime.Seconds()

			// Report metrics
			b.ReportMetric(float64(successful), "successful_photos")
			b.ReportMetric(float64(failed), "failed_photos")
			b.ReportMetric(float64(progressUpdates), "progress_updates")
			b.ReportMetric(float64(totalMessages), "total_messages")
			b.ReportMetric(float64(dbOps), "database_operations")
			b.ReportMetric(float64(totalTime.Milliseconds()), "total_time_ms")
			b.ReportMetric(avgProcessingTime, "avg_processing_time_ms")
			b.ReportMetric(photosPerSecond, "photos_per_second")
			b.ReportMetric(progressUpdatesPerSecond, "progress_updates_per_second")
			b.ReportMetric(float64(avgLatency.Microseconds()), "avg_send_latency_us")

			// Log detailed results
			b.Logf("\n" + strings.Repeat("=", 80))
			b.Logf("⚡ CONCURRENT vs BLOCKING BENCHMARK - %s", tc.name)
			b.Logf(strings.Repeat("=", 80))
			b.Logf("📸 Photos processed: %d", successful)
			b.Logf("❌ Failed photos: %d", failed)
			b.Logf("📊 Progress updates: %d", progressUpdates)
			b.Logf("📈 Success rate: %.2f%%", float64(successful)/float64(tc.concurrentPhotos)*100)
			b.Logf("⏱️  Total processing time: %v", totalTime)
			b.Logf("🚀 Photos per second: %.2f", photosPerSecond)
			b.Logf("⚡ Average processing time: %.2f ms", avgProcessingTime)
			b.Logf("📨 Total messages sent: %d", totalMessages)
			b.Logf("🔄 Progress updates per second: %.2f", progressUpdatesPerSecond)
			b.Logf("🗄️  Database operations: %d", dbOps)
			b.Logf("📡 Average send latency: %v", avgLatency)

			// Performance comparison analysis
			b.Logf("\n📋 PERFORMANCE ANALYSIS:")

			approach := "BLOCKING"
			if tc.useConcurrent {
				approach = "CONCURRENT"
			}
			b.Logf("   Approach: %s", approach)

			messagesPerPhoto := float64(totalMessages) / float64(successful)
			expectedMessages := float64(tc.progressUpdates + 1) // progress updates + final result

			b.Logf("   Expected messages per photo: %.1f", expectedMessages)
			b.Logf("   Actual messages per photo: %.1f", messagesPerPhoto)

			if tc.useConcurrent {
				// Concurrent approach should have better throughput
				if photosPerSecond > 1.5 {
					b.Logf("🟢 GOOD: Concurrent approach achieving >1.5 photos/second")
				} else {
					b.Logf("🟡 MODERATE: Concurrent approach achieving %.2f photos/second", photosPerSecond)
				}

				// Main processing time should be closer to actual work (550ms simulated work)
				mainWorkTime := 550.0 // 100+150+200+100ms simulated work
				overhead := avgProcessingTime - mainWorkTime
				if overhead < 200 {
					b.Logf("🟢 LOW OVERHEAD: %.0f ms overhead vs %.0f ms main work", overhead, mainWorkTime)
				} else if overhead < 500 {
					b.Logf("🟡 MODERATE OVERHEAD: %.0f ms overhead vs %.0f ms main work", overhead, mainWorkTime)
				} else {
					b.Logf("🔴 HIGH OVERHEAD: %.0f ms overhead vs %.0f ms main work", overhead, mainWorkTime)
				}
			} else {
				// Blocking approach will have progress bar delays
				progressBarDelay := float64(tc.progressUpdates) * 200 // 200ms per update
				expectedTime := 550.0 + progressBarDelay              // Main work + progress delays

				b.Logf("   Expected total time: %.0f ms (%.0f work + %.0f progress)", expectedTime, 550.0, progressBarDelay)
				b.Logf("   Actual total time: %.0f ms", avgProcessingTime)

				efficiency := expectedTime / avgProcessingTime * 100
				b.Logf("   Time efficiency: %.1f%%", efficiency)
			}

			// Overall performance classification
			if photosPerSecond > 5 {
				b.Logf("🟢 EXCELLENT: >5 photos/second")
			} else if photosPerSecond > 2 {
				b.Logf("🟡 GOOD: >2 photos/second")
			} else if photosPerSecond > 1 {
				b.Logf("🟠 MODERATE: >1 photo/second")
			} else {
				b.Logf("🔴 SLOW: <1 photo/second")
			}

			b.Logf(strings.Repeat("=", 80))
		})
	}
}

