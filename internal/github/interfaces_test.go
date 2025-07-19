package github

import (
	"testing"
)

// Test that all interface types are properly defined
func TestInterfaceDefinitions(t *testing.T) {
	// Test ProviderType constants
	tests := []struct {
		name     string
		provider ProviderType
		expected string
	}{
		{"Clone provider", ProviderTypeClone, "clone"},
		{"API provider", ProviderTypeAPI, "api"},
		{"Hybrid provider", ProviderTypeHybrid, "hybrid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.provider) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, string(tt.provider))
			}
		})
	}
}

// Test FileMode constants
func TestFileMode(t *testing.T) {
	tests := []struct {
		name     string
		mode     FileMode
		expected string
	}{
		{"Append mode", FileModeAppend, "append"},
		{"Replace mode", FileModeReplace, "replace"},
		{"Binary mode", FileModeBinary, "binary"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.mode) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, string(tt.mode))
			}
		})
	}
}

// Test ProviderConfig structure
func TestProviderConfig(t *testing.T) {
	config := &ProviderConfig{
		Config:       &MockGitHubConfig{},
		PremiumLevel: 1,
		UserID:       "test-user",
	}

	if config.PremiumLevel != 1 {
		t.Errorf("Expected premium level 1, got %d", config.PremiumLevel)
	}

	if config.UserID != "test-user" {
		t.Errorf("Expected user ID 'test-user', got %s", config.UserID)
	}
}

// Test IssueStatus structure
func TestIssueStatus(t *testing.T) {
	status := &IssueStatus{
		Number:  123,
		Title:   "Test Issue",
		State:   "open",
		HTMLURL: "https://github.com/user/repo/issues/123",
	}

	if status.Number != 123 {
		t.Errorf("Expected issue number 123, got %d", status.Number)
	}

	if status.State != "open" {
		t.Errorf("Expected state 'open', got %s", status.State)
	}
}

// Test FileOperation structure
func TestFileOperation(t *testing.T) {
	op := FileOperation{
		Filename:   "test.md",
		Content:    "test content",
		Mode:       FileModeAppend,
		BinaryData: []byte{1, 2, 3},
	}

	if op.Filename != "test.md" {
		t.Errorf("Expected filename 'test.md', got %s", op.Filename)
	}

	if op.Mode != FileModeAppend {
		t.Errorf("Expected mode append, got %s", op.Mode)
	}

	if len(op.BinaryData) != 3 {
		t.Errorf("Expected binary data length 3, got %d", len(op.BinaryData))
	}
}

// Test CommitOptions structure
func TestCommitOptions(t *testing.T) {
	files := []FileOperation{
		{Filename: "file1.md", Content: "content1", Mode: FileModeAppend},
		{Filename: "file2.md", Content: "content2", Mode: FileModeReplace},
	}

	options := CommitOptions{
		Message:      "Test commit",
		Author:       "Test Author <test@example.com>",
		PremiumLevel: 2,
		Files:        files,
	}

	if options.Message != "Test commit" {
		t.Errorf("Expected message 'Test commit', got %s", options.Message)
	}

	if len(options.Files) != 2 {
		t.Errorf("Expected 2 files, got %d", len(options.Files))
	}

	if options.PremiumLevel != 2 {
		t.Errorf("Expected premium level 2, got %d", options.PremiumLevel)
	}
}

// MockGitHubConfig for testing
type MockGitHubConfig struct {
	Username string
	Token    string
	Repo     string
	Author   string
}

func (m *MockGitHubConfig) GetGitHubUsername() string {
	if m.Username == "" {
		return "testuser"
	}
	return m.Username
}

func (m *MockGitHubConfig) GetGitHubToken() string {
	if m.Token == "" {
		return "test-token"
	}
	return m.Token
}

func (m *MockGitHubConfig) GetGitHubRepo() string {
	if m.Repo == "" {
		return "https://github.com/testuser/testrepo.git"
	}
	return m.Repo
}

func (m *MockGitHubConfig) GetCommitAuthor() string {
	if m.Author == "" {
		return "Test Author <test@example.com>"
	}
	return m.Author
}