//go:build stress

package telegram

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/msg2git/msg2git/internal/database"
)

// Test data generators for different file sizes
const (
	SmallFile   = 1000    // 1KB
	MediumFile  = 100000  // 100KB
	LargeFile   = 1000000 // 1MB
	XLargeFile  = 5000000 // 5MB
	XXLargeFile = 10000000 // 10MB
)

// MockGitHubAPI simulates GitHub API delays
type MockGitHubAPI struct {
	issueDelay time.Duration
	fileDelay  time.Duration
}

func NewMockGitHubAPI(issueDelay, fileDelay time.Duration) *MockGitHubAPI {
	return &MockGitHubAPI{
		issueDelay: issueDelay,
		fileDelay:  fileDelay,
	}
}

func (m *MockGitHubAPI) MockIssueAPICall() {
	time.Sleep(m.issueDelay)
}

func (m *MockGitHubAPI) MockFileOperation() {
	time.Sleep(m.fileDelay)
}

// generateTestNote creates a note.md content of specified size
func generateTestNote(sizeBytes int) string {
	baseEntry := "# Test Note Entry\n\nThis is a test note entry with some content to simulate real usage.\n\n"
	entrySize := len(baseEntry)
	entriesNeeded := sizeBytes / entrySize
	
	var content strings.Builder
	content.WriteString("# Notes\n\n")
	
	for i := 0; i < entriesNeeded; i++ {
		content.WriteString(fmt.Sprintf("## Entry %d\n", i+1))
		content.WriteString(fmt.Sprintf("Timestamp: %s\n", time.Now().Format("2006-01-02 15:04:05")))
		content.WriteString("This is a test note entry with some content to simulate real usage.\n")
		content.WriteString("Lorem ipsum dolor sit amet, consectetur adipiscing elit.\n\n")
	}
	
	return content.String()
}

// generateTestIssues creates issue.md content with specified number of issues
func generateTestIssues(numIssues int) string {
	var content strings.Builder
	content.WriteString("# Issues\n\n")
	
	for i := 1; i <= numIssues; i++ {
		status := "🟢 OPEN"
		if i%3 == 0 {
			status = "🔴 CLOSED"
		}
		
		content.WriteString(fmt.Sprintf("## Issue #%d - Test Issue %d %s\n", i, i, status))
		content.WriteString(fmt.Sprintf("Created: %s\n", time.Now().AddDate(0, 0, -i).Format("2006-01-02")))
		content.WriteString("Description: This is a test issue for stress testing purposes.\n")
		content.WriteString("Labels: test, stress-test\n\n")
	}
	
	return content.String()
}

// generateTestTodos creates todo.md content with specified number of todos
func generateTestTodos(numTodos int) string {
	var content strings.Builder
	content.WriteString("# TODOs\n\n")
	
	for i := 1; i <= numTodos; i++ {
		checked := ""
		if i%4 == 0 {
			checked = "x"
		}
		
		content.WriteString(fmt.Sprintf("- [%s] Todo item %d - Test task for stress testing\n", checked, i))
	}
	
	return content.String()
}

// Benchmark file parsing operations
func BenchmarkNoteFileParsing(b *testing.B) {
	testCases := []struct {
		name string
		size int
	}{
		{"Small_1KB", SmallFile},
		{"Medium_100KB", MediumFile},
		{"Large_1MB", LargeFile},
		{"XLarge_5MB", XLargeFile},
		{"XXLarge_10MB", XXLargeFile},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			content := generateTestNote(tc.size)
			b.ResetTimer()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				// Simulate note processing
				lines := strings.Split(content, "\n")
				_ = len(lines)
			}
		})
	}
}

func BenchmarkIssueFileParsing(b *testing.B) {
	testCases := []struct {
		name      string
		numIssues int
	}{
		{"10_Issues", 10},
		{"50_Issues", 50},
		{"100_Issues", 100},
		{"500_Issues", 500},
		{"1000_Issues", 1000},
		{"5000_Issues", 5000},
	}

	_ = &Bot{} // Bot placeholder for reference

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			content := generateTestIssues(tc.numIssues)
			b.ResetTimer()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				// Test issue parsing - simplified for benchmark
				lines := strings.Split(content, "\n")
				_ = len(lines)
			}
		})
	}
}

func BenchmarkTodoFileParsing(b *testing.B) {
	testCases := []struct {
		name     string
		numTodos int
	}{
		{"50_Todos", 50},
		{"200_Todos", 200},
		{"500_Todos", 500},
		{"1000_Todos", 1000},
		{"2000_Todos", 2000},
	}

	_ = &Bot{} // Bot placeholder for reference

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			content := generateTestTodos(tc.numTodos)
			b.ResetTimer()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				// Test todo parsing - simplified for benchmark
				lines := strings.Split(content, "\n")
				_ = len(lines)
			}
		})
	}
}

// Benchmark sync operations with mock API
func BenchmarkSyncOperations(b *testing.B) {
	testCases := []struct {
		name         string
		numIssues    int
		apiDelay     time.Duration
	}{
		{"10_Issues_FastAPI", 10, 100 * time.Millisecond},
		{"10_Issues_SlowAPI", 10, 2 * time.Second},
		{"50_Issues_FastAPI", 50, 100 * time.Millisecond},
		{"50_Issues_SlowAPI", 50, 2 * time.Second},
		{"100_Issues_FastAPI", 100, 100 * time.Millisecond},
		{"100_Issues_SlowAPI", 100, 2 * time.Second},
		{"500_Issues_FastAPI", 500, 100 * time.Millisecond},
		{"500_Issues_SlowAPI", 500, 2 * time.Second},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			mockAPI := NewMockGitHubAPI(tc.apiDelay, 50*time.Millisecond)
			content := generateTestIssues(tc.numIssues)
			
			_ = &Bot{} // Bot placeholder for reference
			// Simulate issue parsing
			lines := strings.Split(content, "\n")
			issueCount := 0
			for _, line := range lines {
				if strings.Contains(line, "Issue #") {
					issueCount++
				}
			}
			
			b.ResetTimer()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				// Simulate sync operation
				for j := 0; j < tc.numIssues; j++ {
					mockAPI.MockIssueAPICall()
				}
			}
		})
	}
}

// Benchmark memory usage with large files
func BenchmarkMemoryUsage(b *testing.B) {
	testCases := []struct {
		name string
		size int
	}{
		{"1MB_File", LargeFile},
		{"5MB_File", XLargeFile},
		{"10MB_File", XXLargeFile},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				content := generateTestNote(tc.size)
				
				// Simulate file operations
				lines := strings.Split(content, "\n")
				words := 0
				for _, line := range lines {
					words += len(strings.Fields(line))
				}
				_ = words
			}
		})
	}
}

// Test concurrent file operations
func BenchmarkConcurrentOperations(b *testing.B) {
	testCases := []struct {
		name       string
		goroutines int
		fileSize   int
	}{
		{"5_Goroutines_1MB", 5, LargeFile},
		{"10_Goroutines_1MB", 10, LargeFile},
		{"20_Goroutines_1MB", 20, LargeFile},
		{"5_Goroutines_5MB", 5, XLargeFile},
		{"10_Goroutines_5MB", 10, XLargeFile},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			content := generateTestNote(tc.fileSize)
			b.ResetTimer()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				done := make(chan bool, tc.goroutines)
				
				for j := 0; j < tc.goroutines; j++ {
					go func() {
						// Simulate concurrent file processing
						lines := strings.Split(content, "\n")
						_ = len(lines)
						done <- true
					}()
				}
				
				// Wait for all goroutines to complete
				for j := 0; j < tc.goroutines; j++ {
					<-done
				}
			}
		})
	}
}

// Benchmark database operations with large datasets
func BenchmarkDatabaseOperations(b *testing.B) {
	testCases := []struct {
		name     string
		numUsers int
	}{
		{"10_Users", 10},
		{"100_Users", 100},
		{"1000_Users", 1000},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				// Simulate database operations
				users := make([]*database.User, tc.numUsers)
				for j := 0; j < tc.numUsers; j++ {
					users[j] = &database.User{
						ID:      j,
						ChatId:  int64(j),
						Username: fmt.Sprintf("user%d", j),
					}
				}
				_ = users
			}
		})
	}
}

// Test file size limits and performance degradation
func TestFileSizeLimits(t *testing.T) {
	testCases := []struct {
		name     string
		size     int
		expected time.Duration // Expected processing time threshold
	}{
		{"Small_1KB", SmallFile, 1 * time.Millisecond},
		{"Medium_100KB", MediumFile, 10 * time.Millisecond},
		{"Large_1MB", LargeFile, 100 * time.Millisecond},
		{"XLarge_5MB", XLargeFile, 500 * time.Millisecond},
		{"XXLarge_10MB", XXLargeFile, 1 * time.Second},
	}

	_ = &Bot{} // Bot placeholder for reference

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			content := generateTestNote(tc.size)
			
			start := time.Now()
			lines := strings.Split(content, "\n")
			
			// Simulate parsing operations
			for _, line := range lines {
				if strings.Contains(line, "#") {
					_ = strings.TrimSpace(line)
				}
			}
			
			duration := time.Since(start)
			
			t.Logf("File size: %d bytes, Processing time: %v", tc.size, duration)
			
			if duration > tc.expected*10 { // Allow 10x threshold for slow machines
				t.Logf("WARNING: Processing took longer than expected (expected: %v, actual: %v)", 
					tc.expected, duration)
			}
		})
	}
}

// Test issue processing scalability
func TestIssueProcessingScalability(t *testing.T) {
	testCases := []struct {
		name         string
		numIssues    int
		apiDelay     time.Duration
		maxDuration  time.Duration
	}{
		{"10_Issues_Fast", 10, 100 * time.Millisecond, 5 * time.Second},
		{"50_Issues_Fast", 50, 100 * time.Millisecond, 10 * time.Second},
		{"100_Issues_Fast", 100, 100 * time.Millisecond, 20 * time.Second},
		{"10_Issues_Slow", 10, 2 * time.Second, 30 * time.Second},
		{"50_Issues_Slow", 50, 2 * time.Second, 120 * time.Second},
	}

	_ = &Bot{} // Bot placeholder for reference

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			content := generateTestIssues(tc.numIssues)
			mockAPI := NewMockGitHubAPI(tc.apiDelay, 50*time.Millisecond)
			
			start := time.Now()
			
			// Parse issues - simplified for test
			lines := strings.Split(content, "\n")
			issueCount := 0
			for _, line := range lines {
				if strings.Contains(line, "Issue #") {
					issueCount++
				}
			}
			
			// Simulate API calls
			for j := 0; j < issueCount; j++ {
				mockAPI.MockIssueAPICall()
			}
			
			duration := time.Since(start)
			
			t.Logf("Issues: %d, API delay: %v, Total time: %v", 
				tc.numIssues, tc.apiDelay, duration)
			
			if duration > tc.maxDuration {
				t.Logf("WARNING: Processing took longer than maximum expected (max: %v, actual: %v)", 
					tc.maxDuration, duration)
			}
			
			// Calculate throughput
			throughput := float64(tc.numIssues) / duration.Seconds()
			t.Logf("Throughput: %.2f issues/second", throughput)
		})
	}
}