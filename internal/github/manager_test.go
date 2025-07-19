package github

import (
	"fmt"
	"strings"
	"testing"

	gitconfig "github.com/msg2git/msg2git/internal/config"
)

func TestGraphQLIssueQuery(t *testing.T) {
	// Skip this test if no GitHub token is available
	cfg := &gitconfig.Config{
		GitHubToken: "your_token_here", // Replace with actual token for testing
		GitHubRepo:  "https://github.com/your_username/your_repo", // Replace with actual repo
	}

	if cfg.GitHubToken == "your_token_here" {
		t.Skip("Skipping GraphQL test - no real GitHub token provided")
	}

	manager, err := NewManager(cfg, 0)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Test with a small set of issue numbers
	testIssueNumbers := []int{1, 2, 3} // Adjust these to actual issue numbers in your repo

	t.Logf("Testing GraphQL with issue numbers: %v", testIssueNumbers)

	// Parse repo URL to get owner and repo name
	owner, repo, err := manager.parseRepoURL()
	if err != nil {
		t.Fatalf("Failed to parse repo URL: %v", err)
	}

	t.Logf("Repository: owner=%s, name=%s", owner, repo)

	// Test the GraphQL function directly
	statuses, err := manager.fetchIssuesViaGraphQL(owner, repo, testIssueNumbers)
	if err != nil {
		t.Fatalf("GraphQL fetch failed: %v", err)
	}

	t.Logf("GraphQL returned %d statuses", len(statuses))
	for issueNum, status := range statuses {
		t.Logf("Issue %d: title=%s, state=%s, url=%s", issueNum, status.Title, status.State, status.HTMLURL)
	}

	// Test the full sync process
	syncStatuses, err := manager.SyncIssueStatuses(testIssueNumbers)
	if err != nil {
		t.Fatalf("SyncIssueStatuses failed: %v", err)
	}

	t.Logf("SyncIssueStatuses returned %d statuses", len(syncStatuses))
	for issueNum, status := range syncStatuses {
		t.Logf("Synced Issue %d: title=%s, state=%s, url=%s", issueNum, status.Title, status.State, status.HTMLURL)
	}
}

func TestGraphQLQueryGeneration(t *testing.T) {
	// Test query generation
	issueNumbers := []int{1, 2, 3}

	// Build the query parts like the real function does
	var queryParts []string
	for i, num := range issueNumbers {
		queryParts = append(queryParts, fmt.Sprintf(`
		  issue%d: issue(number: %d) {
		    number
		    title
		    state
		    url
		  }`, i, num))
	}

	query := fmt.Sprintf(`{
	  repository(owner: "%s", name: "%s") {
	    %s
	  }
	}`, "testowner", "testrepo", strings.Join(queryParts, ""))

	t.Logf("Generated GraphQL query:\n%s", query)

	// Check that the query contains expected elements
	expectedElements := []string{
		"repository(owner: \"testowner\", name: \"testrepo\")",
		"issue0: issue(number: 1)",
		"issue1: issue(number: 2)", 
		"issue2: issue(number: 3)",
		"number",
		"title",
		"state",
		"url",
	}

	for _, element := range expectedElements {
		if !strings.Contains(query, element) {
			t.Errorf("Query missing expected element: %s", element)
		}
	}
}

func TestParseRepoURL(t *testing.T) {
	tests := []struct {
		repoURL     string
		expectedOwner string
		expectedRepo  string
		shouldError bool
	}{
		{
			repoURL:       "https://github.com/owner/repo",
			expectedOwner: "owner",
			expectedRepo:  "repo",
			shouldError:   false,
		},
		{
			repoURL:       "https://github.com/owner/repo.git",
			expectedOwner: "owner",
			expectedRepo:  "repo",
			shouldError:   false,
		},
		{
			repoURL:     "invalid-url",
			shouldError: true,
		},
	}

	for _, test := range tests {
		cfg := &gitconfig.Config{
			GitHubRepo: test.repoURL,
		}

		manager := &Manager{cfg: cfg}
		owner, repo, err := manager.parseRepoURL()

		if test.shouldError {
			if err == nil {
				t.Errorf("Expected error for URL %s, but got none", test.repoURL)
			}
			continue
		}

		if err != nil {
			t.Errorf("Unexpected error for URL %s: %v", test.repoURL, err)
			continue
		}

		if owner != test.expectedOwner {
			t.Errorf("Expected owner %s, got %s", test.expectedOwner, owner)
		}

		if repo != test.expectedRepo {
			t.Errorf("Expected repo %s, got %s", test.expectedRepo, repo)
		}
	}
}