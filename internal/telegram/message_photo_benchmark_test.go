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

	"github.com/msg2git/msg2git/internal/config"
	"github.com/msg2git/msg2git/internal/llm"
	"golang.org/x/time/rate"
)

// MockGitHubManager for message and photo benchmarks
type MockGitHubManager struct {
	commitCount    int64
	commitLatency  time.Duration
	repoExists     bool
	mu             sync.Mutex
	operations     []string
}

func NewMockGitHubManager() *MockGitHubManager {
	return &MockGitHubManager{
		commitLatency: time.Second * 2, // Simulate 2s GitHub API call
		repoExists:    true,
	}
}

func (m *MockGitHubManager) CommitFileWithAuthorAndPremium(filename, content, commitMsg string, committerInfo string, premiumLevel int) error {
	start := time.Now()
	
	// Simulate GitHub API latency
	time.Sleep(m.commitLatency)
	
	atomic.AddInt64(&m.commitCount, 1)
	
	m.mu.Lock()
	m.operations = append(m.operations, fmt.Sprintf("commit:%s:%dµs", filename, time.Since(start).Microseconds()))
	m.mu.Unlock()
	
	return nil
}

func (m *MockGitHubManager) EnsureRepositoryWithPremium(premiumLevel int) error {
	if !m.repoExists {
		time.Sleep(time.Millisecond * 50) // Simulate repo creation
		m.repoExists = true
	}
	return nil
}

func (m *MockGitHubManager) IsRepositoryNearCapacityWithPremium(premiumLevel int) (bool, float64, error) {
	return false, 15.5, nil // Not near capacity, 15.5% used
}

func (m *MockGitHubManager) GetRepositorySizeInfoWithPremium(premiumLevel int) (float64, float64, error) {
	return 125.7, 1000.0, nil // 125.7MB used, 1GB limit
}

func (m *MockGitHubManager) GetGitHubFileURLWithBranch(filename string) (string, error) {
	return fmt.Sprintf("https://github.com/user/repo/blob/main/%s", filename), nil
}

func (m *MockGitHubManager) GetCommitCount() int64 {
	return atomic.LoadInt64(&m.commitCount)
}

func (m *MockGitHubManager) GetOperations() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.operations...)
}

// MockLLMClient for message processing benchmarks
type MockLLMClient struct {
	processCount int64
	processDelay time.Duration
}

func NewMockLLMClient() *MockLLMClient {
	return &MockLLMClient{
		processDelay: time.Second * 5, // Simulate 5s LLM processing
	}
}

func (m *MockLLMClient) ProcessMessage(content string) (string, *llm.Usage, error) {
	time.Sleep(m.processDelay)
	atomic.AddInt64(&m.processCount, 1)
	
	// Return mock LLM response in expected format: "title|#tags"
	title := fmt.Sprintf("Message Title %d", atomic.LoadInt64(&m.processCount))
	tags := "#message #inbox #auto"
	
	// Mock usage data
	usage := &llm.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}
	
	return fmt.Sprintf("%s|%s", title, tags), usage, nil
}

func (m *MockLLMClient) GetProcessCount() int64 {
	return atomic.LoadInt64(&m.processCount)
}

// MessageTestableBot for message and photo benchmarks
type MessageTestableBot struct {
	mockDB      *MockDatabase
	mockSender  *MockSender
	mockGitHub  *MockGitHubManager
	mockLLM     *MockLLMClient
	config      *config.Config
	pendingMsgs map[string]string
	globalLimit *rate.Limiter
	userLimits  map[int64]*rate.Limiter
	limiterMu   sync.RWMutex
}

func NewMessageTestableBot() *MessageTestableBot {
	return &MessageTestableBot{
		mockDB:      NewMockDatabase(),
		mockSender:  &MockSender{},
		mockGitHub:  NewMockGitHubManager(),
		mockLLM:     NewMockLLMClient(),
		config: &config.Config{
			TelegramBotToken: "test-token",
			GitHubUsername:   "testuser",
			CommitAuthor:     "Test User <test@example.com>",
			LLMProvider:      "deepseek",
			LLMEndpoint:      "https://api.deepseek.com",
			LLMModel:         "deepseek-chat",
		},
		pendingMsgs: make(map[string]string),
		globalLimit: rate.NewLimiter(5000, 1000),
		userLimits:  make(map[int64]*rate.Limiter),
	}
}

// Rate limiting helpers using Wait() strategy
func (tb *MessageTestableBot) checkRateLimit(chatID int64) error {
	ctx := context.Background()
	
	// Wait for global rate limit (blocks until allowed)
	err := tb.globalLimit.Wait(ctx)
	if err != nil {
		return fmt.Errorf("global rate limit wait failed: %w", err)
	}
	
	tb.limiterMu.RLock()
	userLimiter, exists := tb.userLimits[chatID]
	tb.limiterMu.RUnlock()
	
	if !exists {
		tb.limiterMu.Lock()
		userLimiter = rate.NewLimiter(30, 30)
		tb.userLimits[chatID] = userLimiter
		tb.limiterMu.Unlock()
	}
	
	// Wait for user rate limit (blocks until allowed)
	err = userLimiter.Wait(ctx)
	if err != nil {
		return fmt.Errorf("user rate limit wait failed: %w", err)
	}
	
	return nil
}

// SimulateMessageWithInboxClick simulates a complete message -> INBOX button click flow
func (tb *MessageTestableBot) SimulateMessageWithInboxClick(chatID int64, messageID int, content string) error {
	// Rate limiting
	if err := tb.checkRateLimit(chatID); err != nil {
		return err
	}
	
	// Ensure user exists
	username := fmt.Sprintf("user_%d", chatID)
	_, err := tb.mockDB.GetOrCreateUser(chatID, username)
	if err != nil {
		return err
	}
	
	// Process with LLM
	llmResponse, _, err := tb.mockLLM.ProcessMessage(content)
	if err != nil {
		return err
	}
	
	// Parse LLM response: "title|#tags"
	parts := strings.SplitN(strings.TrimSpace(llmResponse), "|", 2)
	title := "untitled"
	tags := ""
	if len(parts) >= 1 {
		title = strings.TrimSpace(parts[0])
	}
	if len(parts) >= 2 {
		tags = strings.TrimSpace(parts[1])
	}
	
	// Format content (simplified)
	timestamp := time.Now().Format("2006-01-02 15:04")
	formattedContent := fmt.Sprintf("# %s\n\n%s\n\n%s\n\n---\n*%s* | [%d]", title, content, tags, timestamp, messageID)
	
	// Commit to GitHub
	commitMsg := fmt.Sprintf("Add message: %s to inbox.md via Telegram", title)
	return tb.mockGitHub.CommitFileWithAuthorAndPremium("inbox.md", formattedContent, commitMsg, tb.config.CommitAuthor, 0)
}

// SimulatePhotoUpload simulates a complete photo upload flow
func (tb *MessageTestableBot) SimulatePhotoUpload(chatID int64, messageID int, caption string) error {
	// Rate limiting
	if err := tb.checkRateLimit(chatID); err != nil {
		return err
	}
	
	// Ensure user exists
	username := fmt.Sprintf("user_%d", chatID)
	_, err := tb.mockDB.GetOrCreateUser(chatID, username)
	if err != nil {
		return err
	}
	
	// Process with LLM
	llmResponse, _, err := tb.mockLLM.ProcessMessage(caption)
	if err != nil {
		return err
	}
	
	// Parse LLM response: "title|#tags"
	parts := strings.SplitN(strings.TrimSpace(llmResponse), "|", 2)
	title := "untitled"
	tags := ""
	if len(parts) >= 1 {
		title = strings.TrimSpace(parts[0])
	}
	if len(parts) >= 2 {
		tags = strings.TrimSpace(parts[1])
	}
	
	// Format photo content
	photoURL := fmt.Sprintf("https://api.telegram.org/file/bot123/photos/photo_%d_%d.jpg", chatID, messageID)
	timestamp := time.Now().Format("2006-01-02 15:04")
	formattedContent := fmt.Sprintf("# %s\n\n![Photo](%s)\n\n%s\n\n%s\n\n---\n*%s* | [%d]", title, photoURL, caption, tags, timestamp, messageID)
	
	// Commit to GitHub
	commitMsg := fmt.Sprintf("Add photo: %s to inbox.md via Telegram", title)
	return tb.mockGitHub.CommitFileWithAuthorAndPremium("inbox.md", formattedContent, commitMsg, tb.config.CommitAuthor, 0)
}


// BenchmarkMessageInboxFlow tests 10K users sending messages and clicking INBOX
func BenchmarkMessageInboxFlow(b *testing.B) {
	testCases := []struct {
		name      string
		userCount int
	}{
		{"1K-Users-Message-Inbox", 1000},
		{"5K-Users-Message-Inbox", 5000},
		{"10K-Users-Message-Inbox", 10000},
	}
	
	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			bot := NewMessageTestableBot()
			
			var (
				successCount int64
				failCount    int64
				totalTime    time.Duration
			)
			
			b.ResetTimer()
			start := time.Now()
			
			// Create wait group for all users
			var wg sync.WaitGroup
			wg.Add(tc.userCount)
			
			// Launch all users simultaneously
			for i := 0; i < tc.userCount; i++ {
				go func(userID int) {
					defer wg.Done()
					
					chatID := int64(1000000 + userID)
					messageID := userID + 1
					content := fmt.Sprintf("Test message from user %d: This is a sample message for inbox processing", userID)
					
					err := bot.SimulateMessageWithInboxClick(chatID, messageID, content)
					if err != nil {
						atomic.AddInt64(&failCount, 1)
					} else {
						atomic.AddInt64(&successCount, 1)
					}
				}(i)
			}
			
			// Wait for all users to complete
			wg.Wait()
			totalTime = time.Since(start)
			
			b.StopTimer()
			
			// Collect metrics
			successTotal := atomic.LoadInt64(&successCount)
			failTotal := atomic.LoadInt64(&failCount)
			commitCount := bot.mockGitHub.GetCommitCount()
			llmProcessCount := bot.mockLLM.GetProcessCount()
			
			throughputPerSecond := float64(successTotal) / totalTime.Seconds()
			
			// Custom metrics for benchmark output
			b.ReportMetric(float64(totalTime.Milliseconds()), "total_time_ms")
			b.ReportMetric(float64(successTotal), "successful_messages")
			b.ReportMetric(float64(failTotal), "failed_messages")
			b.ReportMetric(float64(commitCount), "github_commits")
			b.ReportMetric(float64(llmProcessCount), "llm_processed")
			b.ReportMetric(throughputPerSecond, "messages_per_second")
			b.ReportMetric(float64(successTotal*100)/float64(tc.userCount), "success_rate_percent")
			b.ReportMetric(float64(totalTime.Microseconds())/float64(successTotal), "avg_processing_time_us")
			
			// Detailed results
			b.Logf(`
================================================================================
📨 MESSAGE + INBOX BENCHMARK RESULTS - %s
================================================================================
👥 Total users: %d
✅ Successful messages: %d
❌ Failed messages: %d
📊 Success rate: %.2f%%
⏱️  Total processing time: %v
🚀 Messages per second: %.2f
📝 GitHub commits: %d
🧠 LLM processed: %d
⚡ Average processing time: %.2f ms
================================================================================`,
				tc.name,
				tc.userCount,
				successTotal,
				failTotal,
				float64(successTotal*100)/float64(tc.userCount),
				totalTime,
				throughputPerSecond,
				commitCount,
				llmProcessCount,
				float64(totalTime.Microseconds())/float64(successTotal)/1000,
			)
		})
	}
}

// BenchmarkPhotoUploadFlow tests 10K users uploading photos simultaneously
func BenchmarkPhotoUploadFlow(b *testing.B) {
	testCases := []struct {
		name      string
		userCount int
	}{
		{"1K-Users-Photo-Upload", 1000},
		{"5K-Users-Photo-Upload", 5000},
		{"10K-Users-Photo-Upload", 10000},
	}
	
	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			bot := NewMessageTestableBot()
			
			var (
				successCount int64
				failCount    int64
				totalTime    time.Duration
			)
			
			b.ResetTimer()
			start := time.Now()
			
			// Create wait group for all users
			var wg sync.WaitGroup
			wg.Add(tc.userCount)
			
			// Launch all users simultaneously
			for i := 0; i < tc.userCount; i++ {
				go func(userID int) {
					defer wg.Done()
					
					chatID := int64(2000000 + userID)
					messageID := userID + 1
					caption := fmt.Sprintf("Photo caption from user %d: Beautiful sunset photo taken today", userID)
					
					err := bot.SimulatePhotoUpload(chatID, messageID, caption)
					if err != nil {
						atomic.AddInt64(&failCount, 1)
					} else {
						atomic.AddInt64(&successCount, 1)
					}
				}(i)
			}
			
			// Wait for all users to complete
			wg.Wait()
			totalTime = time.Since(start)
			
			b.StopTimer()
			
			// Collect metrics
			successTotal := atomic.LoadInt64(&successCount)
			failTotal := atomic.LoadInt64(&failCount)
			commitCount := bot.mockGitHub.GetCommitCount()
			llmProcessCount := bot.mockLLM.GetProcessCount()
			
			throughputPerSecond := float64(successTotal) / totalTime.Seconds()
			
			// Custom metrics for benchmark output
			b.ReportMetric(float64(totalTime.Milliseconds()), "total_time_ms")
			b.ReportMetric(float64(successTotal), "successful_photos")
			b.ReportMetric(float64(failTotal), "failed_photos")
			b.ReportMetric(float64(commitCount), "github_commits")
			b.ReportMetric(float64(llmProcessCount), "llm_processed")
			b.ReportMetric(throughputPerSecond, "photos_per_second")
			b.ReportMetric(float64(successTotal*100)/float64(tc.userCount), "success_rate_percent")
			b.ReportMetric(float64(totalTime.Microseconds())/float64(successTotal), "avg_processing_time_us")
			
			// Detailed results
			b.Logf(`
================================================================================
📸 PHOTO UPLOAD BENCHMARK RESULTS - %s
================================================================================
👥 Total users: %d
✅ Successful photos: %d
❌ Failed photos: %d
📊 Success rate: %.2f%%
⏱️  Total processing time: %v
🚀 Photos per second: %.2f
📝 GitHub commits: %d
🧠 LLM processed: %d
⚡ Average processing time: %.2f ms
================================================================================`,
				tc.name,
				tc.userCount,
				successTotal,
				failTotal,
				float64(successTotal*100)/float64(tc.userCount),
				totalTime,
				throughputPerSecond,
				commitCount,
				llmProcessCount,
				float64(totalTime.Microseconds())/float64(successTotal)/1000,
			)
		})
	}
}

// BenchmarkCombinedMessagePhotoFlow tests mixed workload
func BenchmarkCombinedMessagePhotoFlow(b *testing.B) {
	testCases := []struct {
		name         string
		messageUsers int
		photoUsers   int
	}{
		{"5K-Messages-5K-Photos", 5000, 5000},
		{"7K-Messages-3K-Photos", 7000, 3000},
		{"3K-Messages-7K-Photos", 3000, 7000},
	}
	
	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			bot := NewMessageTestableBot()
			
			var (
				messageSuccess int64
				photoSuccess   int64
				messageFail    int64
				photoFail      int64
				totalTime      time.Duration
			)
			
			totalUsers := tc.messageUsers + tc.photoUsers
			
			b.ResetTimer()
			start := time.Now()
			
			// Create wait group for all users
			var wg sync.WaitGroup
			wg.Add(totalUsers)
			
			// Launch message users
			for i := 0; i < tc.messageUsers; i++ {
				go func(userID int) {
					defer wg.Done()
					
					chatID := int64(3000000 + userID)
					messageID := userID + 1
					content := fmt.Sprintf("Combined test message from user %d", userID)
					
					err := bot.SimulateMessageWithInboxClick(chatID, messageID, content)
					if err != nil {
						atomic.AddInt64(&messageFail, 1)
					} else {
						atomic.AddInt64(&messageSuccess, 1)
					}
				}(i)
			}
			
			// Launch photo users
			for i := 0; i < tc.photoUsers; i++ {
				go func(userID int) {
					defer wg.Done()
					
					chatID := int64(4000000 + userID)
					messageID := userID + 1
					caption := fmt.Sprintf("Combined test photo from user %d", userID)
					
					err := bot.SimulatePhotoUpload(chatID, messageID, caption)
					if err != nil {
						atomic.AddInt64(&photoFail, 1)
					} else {
						atomic.AddInt64(&photoSuccess, 1)
					}
				}(i)
			}
			
			// Wait for all users to complete
			wg.Wait()
			totalTime = time.Since(start)
			
			b.StopTimer()
			
			// Collect metrics
			msgSuccess := atomic.LoadInt64(&messageSuccess)
			photoSucc := atomic.LoadInt64(&photoSuccess)
			totalSuccess := msgSuccess + photoSucc
			totalFail := atomic.LoadInt64(&messageFail) + atomic.LoadInt64(&photoFail)
			
			commitCount := bot.mockGitHub.GetCommitCount()
			llmProcessCount := bot.mockLLM.GetProcessCount()
			
			throughputPerSecond := float64(totalSuccess) / totalTime.Seconds()
			
			// Custom metrics for benchmark output
			b.ReportMetric(float64(totalTime.Milliseconds()), "total_time_ms")
			b.ReportMetric(float64(totalSuccess), "successful_operations")
			b.ReportMetric(float64(totalFail), "failed_operations")
			b.ReportMetric(float64(msgSuccess), "successful_messages")
			b.ReportMetric(float64(photoSucc), "successful_photos")
			b.ReportMetric(float64(commitCount), "github_commits")
			b.ReportMetric(float64(llmProcessCount), "llm_processed")
			b.ReportMetric(throughputPerSecond, "operations_per_second")
			b.ReportMetric(float64(totalSuccess*100)/float64(totalUsers), "success_rate_percent")
			
			// Detailed results
			b.Logf(`
================================================================================
🔄 COMBINED MESSAGE + PHOTO BENCHMARK RESULTS - %s
================================================================================
👥 Total users: %d (%d messages + %d photos)
✅ Successful operations: %d
   📨 Messages: %d
   📸 Photos: %d
❌ Failed operations: %d
📊 Success rate: %.2f%%
⏱️  Total processing time: %v
🚀 Operations per second: %.2f
📝 GitHub commits: %d
🧠 LLM processed: %d
================================================================================`,
				tc.name,
				totalUsers,
				tc.messageUsers,
				tc.photoUsers,
				totalSuccess,
				msgSuccess,
				photoSucc,
				totalFail,
				float64(totalSuccess*100)/float64(totalUsers),
				totalTime,
				throughputPerSecond,
				commitCount,
				llmProcessCount,
			)
		})
	}
}

// BenchmarkRealistic10KRequests tests exactly 10K requests with realistic delays and Wait() strategy
func BenchmarkRealistic10KRequests(b *testing.B) {
	testCases := []struct {
		name       string
		targetReqs int
		testType   string // "messages" or "photos"
	}{
		{"10K-Messages-Realistic-Delays", 10000, "messages"},
		{"10K-Photos-Realistic-Delays", 10000, "photos"},
	}
	
	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			bot := NewMessageTestableBot()
			
			var (
				successCount int64
				failCount    int64
				totalTime    time.Duration
			)
			
			b.ResetTimer()
			start := time.Now()
			
			// Create wait group for all requests
			var wg sync.WaitGroup
			wg.Add(tc.targetReqs)
			
			// Launch all requests simultaneously
			for i := 0; i < tc.targetReqs; i++ {
				go func(userID int) {
					defer wg.Done()
					
					chatID := int64(5000000 + userID) // Unique chat IDs
					messageID := userID + 1
					
					var err error
					if tc.testType == "messages" {
						content := fmt.Sprintf("Realistic test message from user %d: This message will be processed with 5s LLM + 2s GitHub delays", userID)
						err = bot.SimulateMessageWithInboxClick(chatID, messageID, content)
					} else {
						caption := fmt.Sprintf("Realistic test photo from user %d: Photo with 5s LLM + 2s GitHub processing", userID)
						err = bot.SimulatePhotoUpload(chatID, messageID, caption)
					}
					
					if err != nil {
						atomic.AddInt64(&failCount, 1)
					} else {
						atomic.AddInt64(&successCount, 1)
					}
				}(i)
			}
			
			// Wait for all requests to complete
			wg.Wait()
			totalTime = time.Since(start)
			
			b.StopTimer()
			
			// Collect metrics
			successTotal := atomic.LoadInt64(&successCount)
			failTotal := atomic.LoadInt64(&failCount)
			commitCount := bot.mockGitHub.GetCommitCount()
			llmProcessCount := bot.mockLLM.GetProcessCount()
			
			throughputPerSecond := float64(successTotal) / totalTime.Seconds()
			
			// Custom metrics for benchmark output
			b.ReportMetric(float64(totalTime.Milliseconds()), "total_time_ms")
			b.ReportMetric(float64(totalTime.Seconds()), "total_time_seconds")
			b.ReportMetric(float64(successTotal), "successful_operations")
			b.ReportMetric(float64(failTotal), "failed_operations")
			b.ReportMetric(float64(commitCount), "github_commits")
			b.ReportMetric(float64(llmProcessCount), "llm_processed")
			b.ReportMetric(throughputPerSecond, "operations_per_second")
			b.ReportMetric(float64(successTotal*100)/float64(tc.targetReqs), "success_rate_percent")
			b.ReportMetric(float64(totalTime.Milliseconds())/float64(successTotal), "avg_processing_time_ms")
			
			// Detailed results
			b.Logf(`
================================================================================
🔄 REALISTIC 10K BENCHMARK RESULTS - %s
================================================================================
🎯 Target requests: %d
✅ Successful operations: %d
❌ Failed operations: %d
📊 Success rate: %.2f%%
⏱️  Total completion time: %v (%.2f seconds)
🚀 Operations per second: %.2f
📝 GitHub commits: %d
🧠 LLM processed: %d
⚡ Average processing time: %.2f ms
📋 Rate limiting: Global 5000/s, Per-user 30/s with Wait() strategy
🕒 Processing delays: LLM 5s + GitHub 2s = 7s per operation
💡 Theoretical minimum time: %.2f seconds (if no rate limiting)
================================================================================`,
				tc.name,
				tc.targetReqs,
				successTotal,
				failTotal,
				float64(successTotal*100)/float64(tc.targetReqs),
				totalTime,
				totalTime.Seconds(),
				throughputPerSecond,
				commitCount,
				llmProcessCount,
				float64(totalTime.Milliseconds())/float64(successTotal),
				float64(tc.targetReqs)*7.0, // 7s per operation if sequential
			)
		})
	}
}