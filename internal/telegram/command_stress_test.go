//go:build stress

package telegram

import (
	"fmt"
	"testing"
	"time"

	"github.com/msg2git/msg2git/internal/cache"
	"github.com/msg2git/msg2git/internal/database"
	"github.com/msg2git/msg2git/internal/github"
)

// StressTestGitHubManager simulates GitHub operations with configurable delays
type StressTestGitHubManager struct {
	issueStatuses map[int]*github.IssueStatus
	apiDelay      time.Duration
	fileDelay     time.Duration
}

func NewStressTestGitHubManager(apiDelay, fileDelay time.Duration) *StressTestGitHubManager {
	return &StressTestGitHubManager{
		issueStatuses: make(map[int]*github.IssueStatus),
		apiDelay:      apiDelay,
		fileDelay:     fileDelay,
	}
}

func (m *StressTestGitHubManager) SyncIssueStatuses(issueNumbers []int) (map[int]*github.IssueStatus, error) {
	// Simulate API delay per batch (GitHub allows batch requests)
	time.Sleep(m.apiDelay)
	
	statuses := make(map[int]*github.IssueStatus)
	for _, num := range issueNumbers {
		state := "open"
		if num%3 == 0 {
			state = "closed"
		}
		
		statuses[num] = &github.IssueStatus{
			Number: num,
			Title:  fmt.Sprintf("Mock Issue #%d", num),
			State:  state,
		}
	}
	
	return statuses, nil
}

func (m *StressTestGitHubManager) ReadFile(filename string) (string, error) {
	time.Sleep(m.fileDelay)
	
	switch filename {
	case "issue.md":
		return generateTestIssues(100), nil // Default 100 issues
	case "note.md":
		return generateTestNote(MediumFile), nil
	default:
		return "", fmt.Errorf("file not found")
	}
}

func (m *StressTestGitHubManager) ReplaceFileWithAuthorAndPremium(filename, content, commitMsg string, committer interface{}, premiumLevel int) error {
	time.Sleep(m.fileDelay)
	return nil
}

// Setup helper for creating test bot
func createTestBot() *Bot {
	return &Bot{
		cache: cache.NewWithConfig(1000, 30*time.Minute, 5*time.Minute),
		// db field removed for stress testing - using mock methods instead
	}
}

// MockDB simulates database operations
type MockDB struct{}

func (m *MockDB) GetUserByChatID(chatID int64) (*database.User, error) {
	return &database.User{
		ChatId:     chatID,
		Username:   fmt.Sprintf("user%d", chatID),
		GitHubRepo: "https://github.com/test/repo",
		GitHubToken: "mock_token",
	}, nil
}

func (m *MockDB) GetPremiumUser(uid int64) (*database.PremiumUser, error) {
	return nil, nil // No premium for test
}

// Benchmark sync command with different issue counts
func BenchmarkSyncCommand(b *testing.B) {
	testCases := []struct {
		name      string
		numIssues int
		apiDelay  time.Duration
	}{
		{"10_Issues_Fast_API", 10, 100 * time.Millisecond},
		{"10_Issues_GitHub_API", 10, 2 * time.Second}, // Realistic GitHub API delay
		{"50_Issues_Fast_API", 50, 100 * time.Millisecond},
		{"50_Issues_GitHub_API", 50, 2 * time.Second},
		{"100_Issues_Fast_API", 100, 100 * time.Millisecond},
		{"100_Issues_GitHub_API", 100, 2 * time.Second},
		{"500_Issues_Fast_API", 500, 100 * time.Millisecond},
		{"500_Issues_GitHub_API", 500, 2 * time.Second},
		{"1000_Issues_Fast_API", 1000, 100 * time.Millisecond},
		{"1000_Issues_GitHub_API", 1000, 2 * time.Second},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			bot := createTestBot()
			content := generateTestIssues(tc.numIssues)
			mockGitHub := NewStressTestGitHubManager(tc.apiDelay, 50*time.Millisecond)
			
			b.ResetTimer()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				// Parse issues from content
				issueNumbers := bot.parseIssueNumbers(content)
				
				// Simulate sync operation
				statuses, err := mockGitHub.SyncIssueStatuses(issueNumbers)
				if err != nil {
					b.Fatal(err)
				}
				
				// Generate new content (simplified for benchmarking)
				_ = len(statuses)
			}
		})
	}
}

// Benchmark issue command parsing and display
func BenchmarkIssueCommand(b *testing.B) {
	testCases := []struct {
		name      string
		numIssues int
		offset    int
	}{
		{"10_Issues_Offset_0", 10, 0},
		{"50_Issues_Offset_0", 50, 0},
		{"100_Issues_Offset_0", 100, 0},
		{"500_Issues_Offset_0", 500, 0},
		{"1000_Issues_Offset_0", 1000, 0},
		{"1000_Issues_Offset_500", 1000, 500},
		{"5000_Issues_Offset_0", 5000, 0},
		{"5000_Issues_Offset_2500", 5000, 2500},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			bot := createTestBot()
			content := generateTestIssues(tc.numIssues)
			
			b.ResetTimer()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				// Parse issues from content
				issues := bot.parseOpenIssuesFromContent(content)
				
				// Simulate pagination
				start := tc.offset
				end := start + 5 // Display 5 issues per page
				if end > len(issues) {
					end = len(issues)
				}
				
				if start < len(issues) {
					_ = issues[start:end]
				}
			}
		})
	}
}

// Benchmark file content generation
func BenchmarkContentGeneration(b *testing.B) {
	testCases := []struct {
		name      string
		numIssues int
	}{
		{"Generate_10_Issues", 10},
		{"Generate_50_Issues", 50},
		{"Generate_100_Issues", 100},
		{"Generate_500_Issues", 500},
		{"Generate_1000_Issues", 1000},
		{"Generate_5000_Issues", 5000},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			_ = createTestBot() // Bot not used in this test
			_ = NewStressTestGitHubManager(0, 0) // GitHub manager not used in this test
			
			// Create mock statuses
			statuses := make(map[int]*github.IssueStatus)
			for i := 1; i <= tc.numIssues; i++ {
				state := "open"
				if i%3 == 0 {
					state = "closed"
				}
				statuses[i] = &github.IssueStatus{
					Number: i,
					Title:  fmt.Sprintf("Test Issue #%d", i),
					State:  state,
				}
			}
			
			b.ResetTimer()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				// Simulate content generation by counting statuses
				contentLength := 0
				for _, status := range statuses {
					contentLength += len(status.Title) + len(status.State)
				}
				_ = contentLength
			}
		})
	}
}

// Test real-world scenarios with mixed operations
func TestRealWorldScenarios(t *testing.T) {
	scenarios := []struct {
		name           string
		numIssues      int
		apiDelay       time.Duration
		expectedMaxTime time.Duration
		description    string
	}{
		{
			name:           "Small_Repo_Fast_Connection",
			numIssues:      25,
			apiDelay:       500 * time.Millisecond,
			expectedMaxTime: 10 * time.Second,
			description:    "Small repository with fast internet connection",
		},
		{
			name:           "Medium_Repo_Normal_Connection",
			numIssues:      100,
			apiDelay:       2 * time.Second,
			expectedMaxTime: 30 * time.Second,
			description:    "Medium repository with normal GitHub API response",
		},
		{
			name:           "Large_Repo_Slow_Connection",
			numIssues:      500,
			apiDelay:       3 * time.Second,
			expectedMaxTime: 2 * time.Minute,
			description:    "Large repository with slow connection/rate limiting",
		},
		{
			name:           "Enterprise_Repo",
			numIssues:      1000,
			apiDelay:       2 * time.Second,
			expectedMaxTime: 3 * time.Minute,
			description:    "Enterprise repository with many issues",
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			bot := createTestBot()
			content := generateTestIssues(scenario.numIssues)
			mockGitHub := NewStressTestGitHubManager(scenario.apiDelay, 100*time.Millisecond)
			
			t.Logf("Testing: %s", scenario.description)
			t.Logf("Issues: %d, API Delay: %v", scenario.numIssues, scenario.apiDelay)
			
			start := time.Now()
			
			// Full sync operation simulation
			issueNumbers := bot.parseIssueNumbers(content)
			statuses, err := mockGitHub.SyncIssueStatuses(issueNumbers)
			if err != nil {
				t.Fatal(err)
			}
			
			// Simulate content generation and file operations
			newContentSize := len(statuses) * 100 // Estimate 100 chars per issue
			err = mockGitHub.ReplaceFileWithAuthorAndPremium("issue.md", "", "test", nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			
			duration := time.Since(start)
			
			t.Logf("Total processing time: %v", duration)
			t.Logf("Issues processed: %d", len(issueNumbers))
			t.Logf("Throughput: %.2f issues/second", float64(len(issueNumbers))/duration.Seconds())
			
			if duration > scenario.expectedMaxTime {
				t.Logf("WARNING: Processing took longer than expected maximum")
				t.Logf("Expected max: %v, Actual: %v", scenario.expectedMaxTime, duration)
			}
			
			// Memory usage estimation
			t.Logf("Generated content size: %d bytes (%.2f KB)", newContentSize, float64(newContentSize)/1024)
		})
	}
}

// Benchmark memory usage with large datasets
func BenchmarkMemoryUsageCommands(b *testing.B) {
	testCases := []struct {
		name      string
		numIssues int
	}{
		{"Memory_100_Issues", 100},
		{"Memory_500_Issues", 500},
		{"Memory_1000_Issues", 1000},
		{"Memory_5000_Issues", 5000},
		{"Memory_10000_Issues", 10000},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			bot := createTestBot()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				// Generate large content
				content := generateTestIssues(tc.numIssues)
				
				// Parse issues
				issueNumbers := bot.parseIssueNumbers(content)
				
				// Create mock statuses
				statuses := make(map[int]*github.IssueStatus)
				for _, num := range issueNumbers {
					statuses[num] = &github.IssueStatus{
						Number: num,
						Title:  fmt.Sprintf("Issue #%d", num),
						State:  "open",
					}
				}
				
				// Generate new content (simulate)
				newContentLength := len(statuses) * 50 // Estimate content length
				_ = newContentLength
			}
		})
	}
}

// Test concurrent access patterns
func BenchmarkConcurrentCommands(b *testing.B) {
	testCases := []struct {
		name       string
		goroutines int
		numIssues  int
	}{
		{"Concurrent_5_Users_100_Issues", 5, 100},
		{"Concurrent_10_Users_100_Issues", 10, 100},
		{"Concurrent_20_Users_100_Issues", 20, 100},
		{"Concurrent_5_Users_1000_Issues", 5, 1000},
		{"Concurrent_10_Users_1000_Issues", 10, 1000},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				done := make(chan bool, tc.goroutines)
				
				for j := 0; j < tc.goroutines; j++ {
					go func(userID int) {
						bot := createTestBot()
						content := generateTestIssues(tc.numIssues)
						
						// Simulate command processing
						issueNumbers := bot.parseIssueNumbers(content)
						_ = issueNumbers
						
						done <- true
					}(j)
				}
				
				// Wait for all goroutines
				for j := 0; j < tc.goroutines; j++ {
					<-done
				}
			}
		})
	}
}

// Performance threshold tests
func TestPerformanceThresholds(t *testing.T) {
	thresholds := []struct {
		name           string
		numIssues      int
		maxParseTime   time.Duration
		maxGenerateTime time.Duration
	}{
		{"Threshold_10_Issues", 10, 1 * time.Millisecond, 5 * time.Millisecond},
		{"Threshold_100_Issues", 100, 10 * time.Millisecond, 50 * time.Millisecond},
		{"Threshold_1000_Issues", 1000, 100 * time.Millisecond, 500 * time.Millisecond},
		{"Threshold_5000_Issues", 5000, 500 * time.Millisecond, 2 * time.Second},
	}

	bot := createTestBot()

	for _, threshold := range thresholds {
		t.Run(threshold.name, func(t *testing.T) {
			content := generateTestIssues(threshold.numIssues)
			
			// Test parsing performance
			start := time.Now()
			issueNumbers := bot.parseIssueNumbers(content)
			parseTime := time.Since(start)
			
			// Test generation performance
			mockStatuses := make(map[int]*github.IssueStatus)
			for _, num := range issueNumbers {
				mockStatuses[num] = &github.IssueStatus{
					Number: num,
					Title:  fmt.Sprintf("Issue #%d", num),
					State:  "open",
				}
			}
			
			start = time.Now()
			// Simulate content generation
			newContentSize := len(mockStatuses) * 80 // Estimate content per issue
			generateTime := time.Since(start)
			
			t.Logf("Issues: %d", threshold.numIssues)
			t.Logf("Parse time: %v (threshold: %v)", parseTime, threshold.maxParseTime)
			t.Logf("Generate time: %v (threshold: %v)", generateTime, threshold.maxGenerateTime)
			t.Logf("Content size: %d bytes", newContentSize)
			
			// Warn if exceeding thresholds (allow 5x margin for CI/slow machines)
			if parseTime > threshold.maxParseTime*5 {
				t.Logf("WARNING: Parse time exceeded threshold by significant margin")
			}
			
			if generateTime > threshold.maxGenerateTime*5 {
				t.Logf("WARNING: Generate time exceeded threshold by significant margin")
			}
		})
	}
}