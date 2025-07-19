package github

import (
	"fmt"
	"testing"
)

func TestMockProvider(t *testing.T) {
	provider := NewMockProvider()

	// Test that provider implements GitHubProvider interface
	var _ GitHubProvider = provider

	t.Run("FileManager operations", func(t *testing.T) {
		// Test CommitFile
		err := provider.CommitFile("test.md", "test content", "test commit")
		if err != nil {
			t.Errorf("CommitFile failed: %v", err)
		}

		// Test ReadFile
		content, err := provider.ReadFile("test.md")
		if err != nil {
			t.Errorf("ReadFile failed: %v", err)
		}
		if content != "test content" {
			t.Errorf("Expected 'test content', got %s", content)
		}

		// Test ReplaceFile
		err = provider.ReplaceFile("test2.md", "replaced content", "replace commit")
		if err != nil {
			t.Errorf("ReplaceFile failed: %v", err)
		}

		// Test CommitBinaryFile
		binaryData := []byte{1, 2, 3, 4}
		err = provider.CommitBinaryFile("binary.dat", binaryData, "binary commit")
		if err != nil {
			t.Errorf("CommitBinaryFile failed: %v", err)
		}

		// Test ReplaceMultipleFiles
		files := map[string]string{
			"file1.md": "content1",
			"file2.md": "content2",
		}
		err = provider.ReplaceMultipleFilesWithAuthorAndPremium(files, "multi commit", "author", 1)
		if err != nil {
			t.Errorf("ReplaceMultipleFiles failed: %v", err)
		}
	})

	t.Run("IssueManager operations", func(t *testing.T) {
		// Test CreateIssue
		url, number, err := provider.CreateIssue("Test Issue", "Test body")
		if err != nil {
			t.Errorf("CreateIssue failed: %v", err)
		}
		if number != 1 {
			t.Errorf("Expected issue number 1, got %d", number)
		}
		if url == "" {
			t.Error("Expected URL but got empty string")
		}

		// Test GetIssueStatus
		status, err := provider.GetIssueStatus(number)
		if err != nil {
			t.Errorf("GetIssueStatus failed: %v", err)
		}
		if status.Number != number {
			t.Errorf("Expected issue number %d, got %d", number, status.Number)
		}
		if status.State != "open" {
			t.Errorf("Expected state 'open', got %s", status.State)
		}

		// Test AddIssueComment
		commentURL, err := provider.AddIssueComment(number, "test comment")
		if err != nil {
			t.Errorf("AddIssueComment failed: %v", err)
		}
		if commentURL == "" {
			t.Error("Expected comment URL but got empty string")
		}

		// Test SyncIssueStatuses
		statuses, err := provider.SyncIssueStatuses([]int{number})
		if err != nil {
			t.Errorf("SyncIssueStatuses failed: %v", err)
		}
		if len(statuses) != 1 {
			t.Errorf("Expected 1 status, got %d", len(statuses))
		}

		// Test CloseIssue
		err = provider.CloseIssue(number)
		if err != nil {
			t.Errorf("CloseIssue failed: %v", err)
		}

		// Verify issue is closed
		status, err = provider.GetIssueStatus(number)
		if err != nil {
			t.Errorf("GetIssueStatus after close failed: %v", err)
		}
		if status.State != "closed" {
			t.Errorf("Expected state 'closed', got %s", status.State)
		}
	})

	t.Run("RepositoryManager operations", func(t *testing.T) {
		// Test EnsureRepository
		err := provider.EnsureRepositoryWithPremium(1)
		if err != nil {
			t.Errorf("EnsureRepository failed: %v", err)
		}

		// Test GetRepoInfo
		owner, repo, err := provider.GetRepoInfo()
		if err != nil {
			t.Errorf("GetRepoInfo failed: %v", err)
		}
		if owner != "testowner" {
			t.Errorf("Expected owner 'testowner', got %s", owner)
		}
		if repo != "testrepo" {
			t.Errorf("Expected repo 'testrepo', got %s", repo)
		}

		// Test GetRepositorySize
		size, err := provider.GetRepositorySize()
		if err != nil {
			t.Errorf("GetRepositorySize failed: %v", err)
		}
		if size != 1024 {
			t.Errorf("Expected size 1024, got %d", size)
		}

		// Test IsRepositoryNearCapacity
		_, percentage, err := provider.IsRepositoryNearCapacityWithPremium(1)
		if err != nil {
			t.Errorf("IsRepositoryNearCapacity failed: %v", err)
		}
		if percentage <= 0 {
			t.Error("Expected positive percentage")
		}

		// Test GetDefaultBranch
		branch, err := provider.GetDefaultBranch()
		if err != nil {
			t.Errorf("GetDefaultBranch failed: %v", err)
		}
		if branch != "main" {
			t.Errorf("Expected branch 'main', got %s", branch)
		}
	})

	t.Run("AssetManager operations", func(t *testing.T) {
		// Test UploadImageToCDN
		imageData := []byte{0xFF, 0xD8, 0xFF} // JPEG header
		url, err := provider.UploadImageToCDN("test.jpg", imageData)
		if err != nil {
			t.Errorf("UploadImageToCDN failed: %v", err)
		}
		if url == "" {
			t.Error("Expected URL but got empty string")
		}
		if !containsString(url, "test.jpg") {
			t.Error("URL should contain filename")
		}
	})
}

func TestMockProviderErrorHandling(t *testing.T) {
	provider := NewMockProvider()
	provider.SetError(true, "test error")

	// Test that errors are properly propagated
	err := provider.CommitFile("test.md", "content", "message")
	if err == nil {
		t.Error("Expected error but got none")
	}
	if err.Error() != "test error" {
		t.Errorf("Expected 'test error', got %s", err.Error())
	}

	// Test issue operations with error
	_, _, err = provider.CreateIssue("title", "body")
	if err == nil {
		t.Error("Expected error but got none")
	}

	// Test repository operations with error
	_, err = provider.GetRepositorySize()
	if err == nil {
		t.Error("Expected error but got none")
	}
}

func TestProviderInterfaceCompatibility(t *testing.T) {
	// Test that multiple provider types can be used interchangeably
	providers := []GitHubProvider{
		NewMockProvider(),
		// NewCloneBasedAdapter would go here if we had a real implementation
	}

	for i, provider := range providers {
		t.Run(fmt.Sprintf("Provider %d", i), func(t *testing.T) {
			// Test basic operations
			err := provider.CommitFile("test.md", "content", "message")
			if err != nil {
				t.Errorf("Provider %d CommitFile failed: %v", i, err)
			}

			_, _, err = provider.GetRepoInfo()
			if err != nil {
				t.Errorf("Provider %d GetRepoInfo failed: %v", i, err)
			}
		})
	}
}

// Use helper functions from factory_test.go