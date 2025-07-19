package telegram

import (
	"strings"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/msg2git/msg2git/internal/config"
	"github.com/msg2git/msg2git/internal/github"
)

func TestGenerateTitleFromContent(t *testing.T) {
	bot := &Bot{}
	
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "short content",
			content:  "Short message",
			expected: "Short message",
		},
		{
			name:     "long content with good word boundary",
			content:  "This is a very long message that should be truncated at a word boundary for better readability",
			expected: "This is a very long message that should be...",
		},
		{
			name:     "long content without good word boundary",
			content:  "Verylongwordwithoutspacesorbreaksthatcannotbetruncatedatwordboundary",
			expected: "Verylongwordwithoutspacesorbreaksthatcannotbetr...",
		},
		{
			name:     "multiline content",
			content:  "First line\nSecond line\nThird line",
			expected: "First line",
		},
		{
			name:     "content starting with whitespace",
			content:  "   \n\n  Actual content",
			expected: "Actual content",
		},
		{
			name:     "empty content",
			content:  "",
			expected: "untitled",
		},
		{
			name:     "whitespace only",
			content:  "   \n\n   ",
			expected: "untitled",
		},
		{
			name:     "single character",
			content:  "A",
			expected: "A",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := bot.generateTitleFromContent(tt.content)
			if result != tt.expected {
				t.Errorf("generateTitleFromContent(%q) = %q, want %q", tt.content, result, tt.expected)
			}
		})
	}
}

func TestAddMarkdownLineBreaks(t *testing.T) {
	bot := &Bot{}
	
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "single line",
			content:  "Single line",
			expected: "Single line  ",
		},
		{
			name:     "multiple lines",
			content:  "Line 1\nLine 2\nLine 3",
			expected: "Line 1  \nLine 2  \nLine 3  ",
		},
		{
			name:     "empty lines",
			content:  "Line 1\n\nLine 3",
			expected: "Line 1  \n  \nLine 3  ",
		},
		{
			name:     "trailing spaces",
			content:  "Line with trailing spaces   \nAnother line",
			expected: "Line with trailing spaces  \nAnother line  ",
		},
		{
			name:     "empty content",
			content:  "",
			expected: "",
		},
		{
			name:     "only newlines",
			content:  "\n\n",
			expected: "  \n  \n",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := bot.addMarkdownLineBreaks(tt.content)
			if result != tt.expected {
				t.Errorf("addMarkdownLineBreaks(%q) = %q, want %q", tt.content, result, tt.expected)
			}
		})
	}
}

func TestParseTitleAndTags(t *testing.T) {
	bot := &Bot{}
	
	tests := []struct {
		name            string
		llmResponse     string
		content         string
		expectedTitle   string
		expectedTags    string
	}{
		{
			name:            "valid format",
			llmResponse:     "Meeting Notes|#work #important",
			content:         "Original content",
			expectedTitle:   "Meeting Notes",
			expectedTags:    "#work #important",
		},
		{
			name:            "no tags",
			llmResponse:     "Simple Title|",
			content:         "Original content",
			expectedTitle:   "Simple Title",
			expectedTags:    "",
		},
		{
			name:            "invalid format - missing pipe",
			llmResponse:     "Invalid Response",
			content:         "This is the original content for fallback",
			expectedTitle:   "This is the original content for fallback",
			expectedTags:    "",
		},
		{
			name:            "empty title",
			llmResponse:     "|#tag1 #tag2",
			content:         "Fallback content",
			expectedTitle:   "Fallback content",
			expectedTags:    "#tag1 #tag2",
		},
		{
			name:            "whitespace handling",
			llmResponse:     "  Padded Title  | #tag1 #tag2  ",
			content:         "Original content",
			expectedTitle:   "Padded Title",
			expectedTags:    "#tag1 #tag2",
		},
		{
			name:            "multiple pipes",
			llmResponse:     "Title|#tag1|extra",
			content:         "Original content",
			expectedTitle:   "Title",
			expectedTags:    "#tag1|extra",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, tags := bot.parseTitleAndTags(tt.llmResponse, tt.content)
			if title != tt.expectedTitle {
				t.Errorf("parseTitleAndTags() title = %q, want %q", title, tt.expectedTitle)
			}
			if tags != tt.expectedTags {
				t.Errorf("parseTitleAndTags() tags = %q, want %q", tags, tt.expectedTags)
			}
		})
	}
}

func TestFormatMessageContentWithTitleAndTags(t *testing.T) {
	bot := &Bot{}
	
	// Mock time for consistent testing
	content := "Test message content"
	filename := "note.md"
	messageID := 123
	chatID := int64(456)
	title := "Test Title"
	tags := "#test #example"
	
	result := bot.formatMessageContentWithTitleAndTags(content, filename, messageID, chatID, title, tags)
	
	// Check that all components are present
	if !strings.Contains(result, "[123]") {
		t.Errorf("formatMessageContentWithTitleAndTags() should contain message ID [123]")
	}
	if !strings.Contains(result, "[456]") {
		t.Errorf("formatMessageContentWithTitleAndTags() should contain chat ID [456]")
	}
	if !strings.Contains(result, "## Test Title") {
		t.Errorf("formatMessageContentWithTitleAndTags() should contain title '## Test Title'")
	}
	if !strings.Contains(result, "#test #example") {
		t.Errorf("formatMessageContentWithTitleAndTags() should contain tags '#test #example'")
	}
	if !strings.Contains(result, "Test message content") {
		t.Errorf("formatMessageContentWithTitleAndTags() should contain original content")
	}
	if !strings.Contains(result, "---") {
		t.Errorf("formatMessageContentWithTitleAndTags() should contain separator '---'")
	}
	
	// Check timestamp format (should contain current date)
	now := time.Now()
	expectedDate := now.Format("2006-01-02")
	if !strings.Contains(result, expectedDate) {
		t.Errorf("formatMessageContentWithTitleAndTags() should contain current date %s", expectedDate)
	}
}

func TestFormatTodoContent(t *testing.T) {
	bot := &Bot{}
	
	tests := []struct {
		name     string
		content  string
		messageID int
		chatID   int64
		expected string
	}{
		{
			name:     "valid todo content",
			content:  "Buy groceries",
			messageID: 123,
			chatID:   456,
			expected: "- [ ] <!--[123] [456]--> Buy groceries",
		},
		{
			name:     "content with line breaks",
			content:  "Line 1\nLine 2",
			messageID: 123,
			chatID:   456,
			expected: "", // Should return empty for content with line breaks
		},
		{
			name:     "empty content",
			content:  "",
			messageID: 123,
			chatID:   456,
			expected: "- [ ] <!--[123] [456]--> ",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := bot.formatTodoContent(tt.content, tt.messageID, tt.chatID)
			
			if tt.expected == "" {
				// For content with line breaks, should return empty
				if result != "" {
					t.Errorf("formatTodoContent() with line breaks = %q, want empty string", result)
				}
			} else {
				if !strings.Contains(result, tt.expected) {
					t.Errorf("formatTodoContent() = %q, should contain %q", result, tt.expected)
				}
				// Check date format
				now := time.Now()
				expectedDate := now.Format("2006-01-02")
				if !strings.Contains(result, expectedDate) {
					t.Errorf("formatTodoContent() should contain current date %s", expectedDate)
				}
			}
		})
	}
}

func TestParseTodoItems(t *testing.T) {
	bot := &Bot{}
	
	content := `- [ ] <!--[123] [456]--> Undone task 1 (2024-01-01)
- [x] <!--[124] [456]--> Done task (2024-01-01)
- [ ] [125] [456] Old bracket format task (2024-01-01)
- [x] [126] [456] Old bracket format done (2024-01-01)
- [ ] [127] Old format task (2024-01-01)
Random line that should be ignored
- [ ] <!--[128] [789]--> Another user's task (2024-01-01)`
	
	todos := bot.parseTodoItems(content)
	
	if len(todos) != 6 {
		t.Errorf("parseTodoItems() found %d todos, want 6", len(todos))
	}
	
	// Check first todo (new format, undone)
	if todos[0].MessageID != 123 {
		t.Errorf("todos[0].MessageID = %d, want 123", todos[0].MessageID)
	}
	if todos[0].ChatID != 456 {
		t.Errorf("todos[0].ChatID = %d, want 456", todos[0].ChatID)
	}
	if todos[0].Content != "Undone task 1" {
		t.Errorf("todos[0].Content = %q, want %q", todos[0].Content, "Undone task 1")
	}
	if todos[0].Done {
		t.Errorf("todos[0].Done = %v, want false", todos[0].Done)
	}
	
	// Check second todo (new format, done)
	if !todos[1].Done {
		t.Errorf("todos[1].Done = %v, want true", todos[1].Done)
	}
	
	// Check third todo (old bracket format)
	if todos[2].MessageID != 125 {
		t.Errorf("todos[2].MessageID = %d, want 125", todos[2].MessageID)
	}
	if todos[2].ChatID != 456 {
		t.Errorf("todos[2].ChatID = %d, want 456", todos[2].ChatID)
	}
	
	// Check fifth todo (oldest format)
	if todos[4].ChatID != 0 {
		t.Errorf("todos[4].ChatID = %d, want 0 for old format", todos[4].ChatID)
	}
}

func TestParseMessageMetadata(t *testing.T) {
	bot := &Bot{}
	
	tests := []struct {
		name      string
		content   string
		wantMsgID int
		wantChatID int64
		wantTimestamp string
		wantErr   bool
	}{
		{
			name: "valid HTML comment metadata",
			content: `<!--
[123] [456789] [2025-06-25 14:30] 
-->

## Test Title
#test #tag

Some content here...

---`,
			wantMsgID: 123,
			wantChatID: 456789,
			wantTimestamp: "2025-06-25 14:30",
			wantErr: false,
		},
		{
			name: "no metadata",
			content: `## Test Title

Some content without metadata...`,
			wantErr: true,
		},
		{
			name: "invalid format",
			content: `<!--
Invalid metadata format
-->

## Test Title`,
			wantErr: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgID, chatID, timestamp, err := bot.parseMessageMetadata(tt.content)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseMessageMetadata() expected error but got none")
				}
				return
			}
			
			if err != nil {
				t.Errorf("parseMessageMetadata() unexpected error: %v", err)
				return
			}
			
			if msgID != tt.wantMsgID {
				t.Errorf("parseMessageMetadata() msgID = %d, want %d", msgID, tt.wantMsgID)
			}
			if chatID != tt.wantChatID {
				t.Errorf("parseMessageMetadata() chatID = %d, want %d", chatID, tt.wantChatID)
			}
			if timestamp != tt.wantTimestamp {
				t.Errorf("parseMessageMetadata() timestamp = %q, want %q", timestamp, tt.wantTimestamp)
			}
		})
	}
}

func TestGetUndoneTodos(t *testing.T) {
	bot := &Bot{}
	
	todos := []TodoItem{
		{MessageID: 1, ChatID: 456, Content: "Task 1", Done: false},
		{MessageID: 2, ChatID: 456, Content: "Task 2", Done: true},
		{MessageID: 3, ChatID: 456, Content: "Task 3", Done: false},
		{MessageID: 4, ChatID: 789, Content: "Other user task", Done: false},
		{MessageID: 5, ChatID: 0, Content: "Old format task", Done: false}, // Old format
		{MessageID: 6, ChatID: 456, Content: "Task 6", Done: false},
	}
	
	tests := []struct {
		name     string
		chatID   int64
		offset   int
		limit    int
		expected int
	}{
		{
			name:     "all undone for user 456",
			chatID:   456,
			offset:   0,
			limit:    10,
			expected: 4, // Tasks 1, 3, 5 (old format), 6
		},
		{
			name:     "with pagination",
			chatID:   456,
			offset:   1,
			limit:    2,
			expected: 2,
		},
		{
			name:     "offset beyond available",
			chatID:   456,
			offset:   10,
			limit:    5,
			expected: 0,
		},
		{
			name:     "different user",
			chatID:   789,
			offset:   0,
			limit:    10,
			expected: 2, // Task 4 and old format task 5 (ChatID 0 included for backward compatibility)
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := bot.getUndoneTodos(todos, tt.chatID, tt.offset, tt.limit)
			if len(result) != tt.expected {
				t.Errorf("getUndoneTodos() returned %d items, want %d", len(result), tt.expected)
			}
		})
	}
}

func TestParseIssueNumbers(t *testing.T) {
	bot := &Bot{}
	
	content := `- 🟢 user/repo#123 [Issue title]
- 🔴 user/repo#456[Another issue]
- Manual entry: #789
- GitHub URL: https://github.com/owner/project/issues/101
- Duplicate: user/repo#123 [Same issue again]
Some random text without issue numbers
- Another format: issue #999`
	
	numbers := bot.parseIssueNumbers(content)
	
	// Should find unique issue numbers: 123, 456, 789, 101, 999
	expectedCount := 5
	if len(numbers) != expectedCount {
		t.Errorf("parseIssueNumbers() found %d numbers, want %d. Found: %v", len(numbers), expectedCount, numbers)
	}
	
	// Check that all expected numbers are present
	expectedNumbers := []int{123, 456, 789, 101, 999}
	foundNumbers := make(map[int]bool)
	for _, num := range numbers {
		foundNumbers[num] = true
	}
	
	for _, expected := range expectedNumbers {
		if !foundNumbers[expected] {
			t.Errorf("parseIssueNumbers() should find issue number %d", expected)
		}
	}
}

func TestGenerateIssueContent(t *testing.T) {
	bot := &Bot{}
	
	// Create mock GitHub manager
	cfg := &config.Config{
		GitHubRepo: "https://github.com/owner/repo",
	}
	githubManager, _ := github.NewManager(cfg, 0)
	
	statuses := map[int]*github.IssueStatus{
		123: {Number: 123, Title: "Open issue", State: "OPEN", HTMLURL: "https://github.com/owner/repo/issues/123"},
		456: {Number: 456, Title: "Closed issue", State: "CLOSED", HTMLURL: "https://github.com/owner/repo/issues/456"},
		789: {Number: 789, Title: "Another open", State: "OPEN", HTMLURL: "https://github.com/owner/repo/issues/789"},
	}
	
	result := bot.generateIssueContent(statuses, githubManager)
	
	// Check that all issues are included
	if !strings.Contains(result, "🟢 owner/repo#123 [Open issue]") {
		t.Errorf("generateIssueContent() should contain open issue 123")
	}
	if !strings.Contains(result, "🔴 owner/repo#456 [Closed issue]") {
		t.Errorf("generateIssueContent() should contain closed issue 456")
	}
	if !strings.Contains(result, "🟢 owner/repo#789 [Another open]") {
		t.Errorf("generateIssueContent() should contain open issue 789")
	}
	
	// Check sorting (should be open issues first, then by number descending)
	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) != 3 {
		t.Errorf("generateIssueContent() should generate 3 lines, got %d", len(lines))
	}
	
	// First line should be issue 789 (highest open issue number)
	if !strings.Contains(lines[0], "#789") {
		t.Errorf("First line should contain #789, got: %s", lines[0])
	}
}

func TestParseIssueStatusesFromContent(t *testing.T) {
	bot := &Bot{}
	
	// Create mock GitHub manager
	cfg := &config.Config{
		GitHubRepo: "https://github.com/owner/repo",
	}
	githubManager, _ := github.NewManager(cfg, 0)
	
	// Test content with various issue formats
	content := `- 🟢 owner/repo#123 [Open issue]
- 🔴 owner/repo#456 [Closed issue]
- 🟢 owner/repo#789 [Another open issue]
Some random text
- Not an issue line
- 🟢 owner/repo#999 [Final issue]`
	
	statuses := bot.parseIssueStatusesFromContent(content, githubManager)
	
	// Should find 4 issues
	expectedCount := 4
	if len(statuses) != expectedCount {
		t.Errorf("parseIssueStatusesFromContent() found %d issues, want %d", len(statuses), expectedCount)
	}
	
	// Check issue 123 (open)
	if status, exists := statuses[123]; exists {
		if status.Number != 123 {
			t.Errorf("Issue 123 number mismatch: got %d, want 123", status.Number)
		}
		if status.Title != "Open issue" {
			t.Errorf("Issue 123 title mismatch: got %s, want 'Open issue'", status.Title)
		}
		if status.State != "open" {
			t.Errorf("Issue 123 state mismatch: got %s, want 'open'", status.State)
		}
		expectedURL := "https://github.com/owner/repo/issues/123"
		if status.HTMLURL != expectedURL {
			t.Errorf("Issue 123 URL mismatch: got %s, want %s", status.HTMLURL, expectedURL)
		}
	} else {
		t.Errorf("parseIssueStatusesFromContent() should find issue 123")
	}
	
	// Check issue 456 (closed)
	if status, exists := statuses[456]; exists {
		if status.State != "closed" {
			t.Errorf("Issue 456 state mismatch: got %s, want 'closed'", status.State)
		}
		if status.Title != "Closed issue" {
			t.Errorf("Issue 456 title mismatch: got %s, want 'Closed issue'", status.Title)
		}
	} else {
		t.Errorf("parseIssueStatusesFromContent() should find issue 456")
	}
	
	// Check issue 789 (open)
	if status, exists := statuses[789]; exists {
		if status.State != "open" {
			t.Errorf("Issue 789 state mismatch: got %s, want 'open'", status.State)
		}
	} else {
		t.Errorf("parseIssueStatusesFromContent() should find issue 789")
	}
	
	// Check issue 999 (open)
	if status, exists := statuses[999]; exists {
		if status.State != "open" {
			t.Errorf("Issue 999 state mismatch: got %s, want 'open'", status.State)
		}
	} else {
		t.Errorf("parseIssueStatusesFromContent() should find issue 999")
	}
}

func TestGenerateIssueContentCaseInsensitive(t *testing.T) {
	bot := &Bot{}
	
	// Create mock GitHub manager
	cfg := &config.Config{
		GitHubRepo: "https://github.com/owner/repo",
	}
	githubManager, _ := github.NewManager(cfg, 0)
	
	// Test with various case combinations to ensure robustness
	statuses := map[int]*github.IssueStatus{
		123: {Number: 123, Title: "Uppercase open", State: "OPEN", HTMLURL: "https://github.com/owner/repo/issues/123"},
		456: {Number: 456, Title: "Uppercase closed", State: "CLOSED", HTMLURL: "https://github.com/owner/repo/issues/456"},
		789: {Number: 789, Title: "Lowercase open", State: "open", HTMLURL: "https://github.com/owner/repo/issues/789"},
		999: {Number: 999, Title: "Lowercase closed", State: "closed", HTMLURL: "https://github.com/owner/repo/issues/999"},
	}
	
	result := bot.generateIssueContent(statuses, githubManager)
	
	// Check that open issues get green emoji regardless of case
	if !strings.Contains(result, "🟢 owner/repo#123 [Uppercase open]") {
		t.Errorf("generateIssueContent() should show green emoji for OPEN state")
	}
	if !strings.Contains(result, "🟢 owner/repo#789 [Lowercase open]") {
		t.Errorf("generateIssueContent() should show green emoji for open state")
	}
	
	// Check that closed issues get red emoji regardless of case
	if !strings.Contains(result, "🔴 owner/repo#456 [Uppercase closed]") {
		t.Errorf("generateIssueContent() should show red emoji for CLOSED state")
	}
	if !strings.Contains(result, "🔴 owner/repo#999 [Lowercase closed]") {
		t.Errorf("generateIssueContent() should show red emoji for closed state")
	}
}

func TestTelegramToMarkdown(t *testing.T) {
	bot := &Bot{}
	
	tests := []struct {
		name     string
		text     string
		entities []tgbotapi.MessageEntity
		expected string
	}{
		{
			name:     "no entities",
			text:     "Plain text",
			entities: []tgbotapi.MessageEntity{},
			expected: "Plain text",
		},
		{
			name: "bold text",
			text: "Hello world",
			entities: []tgbotapi.MessageEntity{
				{Type: "bold", Offset: 0, Length: 5},
			},
			expected: "**Hello** world",
		},
		{
			name: "italic text",
			text: "Hello world",
			entities: []tgbotapi.MessageEntity{
				{Type: "italic", Offset: 6, Length: 5},
			},
			expected: "Hello *world*",
		},
		{
			name: "code text",
			text: "Hello code world",
			entities: []tgbotapi.MessageEntity{
				{Type: "code", Offset: 6, Length: 4},
			},
			expected: "Hello `code` world",
		},
		{
			name: "text link",
			text: "Click here",
			entities: []tgbotapi.MessageEntity{
				{Type: "text_link", Offset: 6, Length: 4, URL: "https://example.com"},
			},
			expected: "Click [here](https://example.com)",
		},
		{
			name: "multiple entities",
			text: "Bold and italic text",
			entities: []tgbotapi.MessageEntity{
				{Type: "bold", Offset: 0, Length: 4},
				{Type: "italic", Offset: 9, Length: 6},
			},
			expected: "**Bold** and *italic* text",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := bot.telegramToMarkdown(tt.text, tt.entities)
			if result != tt.expected {
				t.Errorf("telegramToMarkdown() = %q, want %q", result, tt.expected)
			}
		})
	}
}