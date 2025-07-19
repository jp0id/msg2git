package file

import (
	"strings"
	"testing"
	"time"
)

func TestNewManager(t *testing.T) {
	manager := NewManager()
	if manager == nil {
		t.Errorf("NewManager() returned nil")
	}
}

func TestNormalizeFilename(t *testing.T) {
	manager := NewManager()
	
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "lowercase with .md extension",
			input:    "test.md",
			expected: "test.md",
		},
		{
			name:     "uppercase with .md extension",
			input:    "TEST.md",
			expected: "test.md",
		},
		{
			name:     "mixed case with .md extension",
			input:    "TeSt.md",
			expected: "test.md",
		},
		{
			name:     "without .md extension",
			input:    "test",
			expected: "test.md",
		},
		{
			name:     "uppercase without .md extension",
			input:    "TEST",
			expected: "test.md",
		},
		{
			name:     "mixed case without .md extension",
			input:    "TeSt",
			expected: "test.md",
		},
		{
			name:     "empty string",
			input:    "",
			expected: ".md",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.normalizeFilename(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeFilename(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatContent(t *testing.T) {
	manager := NewManager()
	
	// Mock time for consistent testing
	now := time.Now()
	expectedTimestamp := now.Format("2006-01-02 15:04:05")
	
	tests := []struct {
		name     string
		message  string
		filename string
		expected string
	}{
		{
			name:     "todo.md file",
			message:  "Test todo item",
			filename: "todo.md",
			expected: "- [ ] Test todo item\n",
		},
		{
			name:     "TODO.md file (uppercase)",
			message:  "Test todo item",
			filename: "TODO.md",
			expected: "- [ ] Test todo item\n",
		},
		{
			name:     "regular file",
			message:  "Test message",
			filename: "note.md",
			expected: expectedTimestamp + " - Test message\n",
		},
		{
			name:     "empty message",
			message:  "",
			filename: "note.md",
			expected: expectedTimestamp + " - \n",
		},
		{
			name:     "message with newlines",
			message:  "Line 1\nLine 2",
			filename: "note.md",
			expected: expectedTimestamp + " - Line 1\nLine 2\n",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.formatContent(tt.message, tt.filename)
			
			if tt.filename == "todo.md" || tt.filename == "TODO.md" {
				// For TODO files, check exact match
				if result != tt.expected {
					t.Errorf("formatContent(%q, %q) = %q, want %q", tt.message, tt.filename, result, tt.expected)
				}
			} else {
				// For regular files, check that it contains the message and has timestamp format
				if !strings.Contains(result, tt.message) {
					t.Errorf("formatContent(%q, %q) = %q, should contain message %q", tt.message, tt.filename, result, tt.message)
				}
				if !strings.Contains(result, " - ") {
					t.Errorf("formatContent(%q, %q) = %q, should contain timestamp separator ' - '", tt.message, tt.filename, result)
				}
				if !strings.HasSuffix(result, "\n") {
					t.Errorf("formatContent(%q, %q) = %q, should end with newline", tt.message, tt.filename, result)
				}
			}
		})
	}
}

func TestProcessMessage(t *testing.T) {
	manager := NewManager()
	
	tests := []struct {
		name          string
		message       string
		filename      string
		expectError   bool
		errorContains string
		checkContent  func(string) bool
	}{
		{
			name:        "valid todo message",
			message:     "Buy groceries",
			filename:    "todo",
			expectError: false,
			checkContent: func(content string) bool {
				return strings.Contains(content, "- [ ] Buy groceries")
			},
		},
		{
			name:        "valid regular message",
			message:     "Meeting notes",
			filename:    "notes",
			expectError: false,
			checkContent: func(content string) bool {
				return strings.Contains(content, "Meeting notes") && strings.Contains(content, " - ")
			},
		},
		{
			name:          "empty filename",
			message:       "Test message",
			filename:      "",
			expectError:   true,
			errorContains: "filename cannot be empty",
		},
		{
			name:        "filename with extension",
			message:     "Test message",
			filename:    "test.md",
			expectError: false,
			checkContent: func(content string) bool {
				return strings.Contains(content, "Test message")
			},
		},
		{
			name:        "uppercase filename",
			message:     "Test message",
			filename:    "TEST",
			expectError: false,
			checkContent: func(content string) bool {
				return strings.Contains(content, "Test message")
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := manager.ProcessMessage(tt.message, tt.filename)
			
			if tt.expectError {
				if err == nil {
					t.Errorf("ProcessMessage(%q, %q) expected error but got nil", tt.message, tt.filename)
					return
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("ProcessMessage(%q, %q) error = %v, want to contain %q", tt.message, tt.filename, err, tt.errorContains)
				}
			} else {
				if err != nil {
					t.Errorf("ProcessMessage(%q, %q) unexpected error = %v", tt.message, tt.filename, err)
					return
				}
				if tt.checkContent != nil && !tt.checkContent(content) {
					t.Errorf("ProcessMessage(%q, %q) content = %q, failed content check", tt.message, tt.filename, content)
				}
			}
		})
	}
}

func TestParseMessage(t *testing.T) {
	manager := NewManager()
	
	tests := []struct {
		name             string
		text             string
		expectError      bool
		errorContains    string
		expectedFilename string
		checkContent     func(string) bool
	}{
		{
			name:             "valid todo message",
			text:             "todo Buy groceries",
			expectError:      false,
			expectedFilename: "todo.md",
			checkContent: func(content string) bool {
				return strings.Contains(content, "- [ ] Buy groceries")
			},
		},
		{
			name:             "valid note message",
			text:             "notes Meeting with team",
			expectError:      false,
			expectedFilename: "notes.md",
			checkContent: func(content string) bool {
				return strings.Contains(content, "Meeting with team") && strings.Contains(content, " - ")
			},
		},
		{
			name:          "message without space",
			text:          "notes",
			expectError:   true,
			errorContains: "message must contain filename and content separated by space",
		},
		{
			name:          "empty message",
			text:          "",
			expectError:   true,
			errorContains: "message must contain filename and content separated by space",
		},
		{
			name:             "filename with extension",
			text:             "test.md Content here",
			expectError:      false,
			expectedFilename: "test.md",
			checkContent: func(content string) bool {
				return strings.Contains(content, "Content here")
			},
		},
		{
			name:             "multi-word content",
			text:             "ideas New project idea for mobile app",
			expectError:      false,
			expectedFilename: "ideas.md",
			checkContent: func(content string) bool {
				return strings.Contains(content, "New project idea for mobile app")
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filename, content, err := manager.ParseMessage(tt.text)
			
			if tt.expectError {
				if err == nil {
					t.Errorf("ParseMessage(%q) expected error but got nil", tt.text)
					return
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("ParseMessage(%q) error = %v, want to contain %q", tt.text, err, tt.errorContains)
				}
			} else {
				if err != nil {
					t.Errorf("ParseMessage(%q) unexpected error = %v", tt.text, err)
					return
				}
				if filename != tt.expectedFilename {
					t.Errorf("ParseMessage(%q) filename = %q, want %q", tt.text, filename, tt.expectedFilename)
				}
				if tt.checkContent != nil && !tt.checkContent(content) {
					t.Errorf("ParseMessage(%q) content = %q, failed content check", tt.text, content)
				}
			}
		})
	}
}