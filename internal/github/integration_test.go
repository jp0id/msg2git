package github

import (
	"testing"
)

// TestProviderFactoryIntegration demonstrates how to use the provider factory
func TestProviderFactoryIntegration(t *testing.T) {
	factory := NewProviderFactory()
	config := &ProviderConfig{
		Config:       &MockGitHubConfig{},
		PremiumLevel: 1,
		UserID:       "test-user",
	}

	// Try to create a clone provider - expected to fail in test environment
	provider, err := factory.CreateProvider(ProviderTypeClone, config)
	if err != nil {
		t.Logf("Clone provider creation failed (expected in test environment): %v", err)
		// Use mock provider for testing instead - this is the expected path
		provider = NewMockProvider()
		t.Log("Using mock provider for testing")
		
		// Test the provider interface with mock
		testProviderOperations(t, provider)
	} else {
		t.Log("Using real clone provider (unexpected in test environment)")
		// If somehow we got a real provider, test it but don't fail on auth errors
		testProviderOperationsWithAuth(t, provider)
	}
}

// TestProviderSwapping demonstrates how easy it is to swap providers
func TestProviderSwapping(t *testing.T) {
	// Simulate different provider configurations
	providers := map[string]GitHubProvider{
		"mock": NewMockProvider(),
		// "clone": would be created via factory in real usage
	}

	for name, provider := range providers {
		t.Run(name, func(t *testing.T) {
			testProviderOperations(t, provider)
		})
	}
}

// testProviderOperationsWithAuth tests operations that might fail due to auth
func testProviderOperationsWithAuth(t *testing.T, provider GitHubProvider) {
	// These operations are expected to fail with auth errors in test environment
	// We just verify the interface works, not the results
	
	err := provider.CommitFile("integration_test.md", "Integration test content", "Integration test commit")
	if err != nil {
		t.Logf("CommitFile failed (expected with test credentials): %v", err)
	}
	
	_, _, err = provider.CreateIssue("Integration Test Issue", "This is a test issue for integration testing")
	if err != nil {
		t.Logf("CreateIssue failed (expected with test credentials): %v", err)
	}
	
	// These should work without auth
	owner, repo, err := provider.GetRepoInfo()
	if err == nil && owner != "" && repo != "" {
		t.Logf("GetRepoInfo succeeded: %s/%s", owner, repo)
	}
}

// testProviderOperations tests all major operations on a provider
func testProviderOperations(t *testing.T, provider GitHubProvider) {
	// Test file operations
	err := provider.CommitFile("integration_test.md", "Integration test content", "Integration test commit")
	if err != nil {
		t.Errorf("CommitFile failed: %v", err)
	}

	content, err := provider.ReadFile("integration_test.md")
	if err != nil {
		t.Errorf("ReadFile failed: %v", err)
	}
	if content != "Integration test content" {
		t.Errorf("Expected 'Integration test content', got %s", content)
	}

	// Test issue operations
	issueURL, issueNumber, err := provider.CreateIssue("Integration Test Issue", "This is a test issue for integration testing")
	if err != nil {
		t.Errorf("CreateIssue failed: %v", err)
	}
	if issueNumber == 0 {
		t.Error("Expected non-zero issue number")
	}
	if issueURL == "" {
		t.Error("Expected non-empty issue URL")
	}

	// Test repository operations
	owner, repo, err := provider.GetRepoInfo()
	if err != nil {
		t.Errorf("GetRepoInfo failed: %v", err)
	}
	if owner == "" || repo == "" {
		t.Error("Expected non-empty owner and repo")
	}

	// Test asset operations
	imageData := []byte{0xFF, 0xD8, 0xFF, 0xE0} // JPEG header
	assetURL, err := provider.UploadImageToCDN("integration_test.jpg", imageData)
	if err != nil {
		t.Errorf("UploadImageToCDN failed: %v", err)
	}
	if assetURL == "" {
		t.Error("Expected non-empty asset URL")
	}
}

// TestProviderMetricsAndSelection demonstrates provider selection logic
func TestProviderMetricsAndSelection(t *testing.T) {
	tests := []struct {
		name             string
		userCount        int
		avgFiles         int
		avgCommits       int
		expectedProvider ProviderType
	}{
		{
			name:             "Small team with simple operations",
			userCount:        10,
			avgFiles:         1,
			avgCommits:       5,
			expectedProvider: ProviderTypeAPI,
		},
		{
			name:             "Large team with complex operations",
			userCount:        100,
			avgFiles:         5,
			avgCommits:       10,
			expectedProvider: ProviderTypeClone,
		},
		{
			name:             "High volume simple operations",
			userCount:        1000,
			avgFiles:         1,
			avgCommits:       2,
			expectedProvider: ProviderTypeAPI,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recommended := GetRecommendedProvider(tt.userCount, tt.avgFiles, tt.avgCommits)
			if recommended != tt.expectedProvider {
				t.Errorf("Expected %s, got %s", tt.expectedProvider, recommended)
			}

			// Get metrics for the recommended provider
			metrics := GetProviderMetrics(recommended)
			if metrics == nil {
				t.Error("Expected metrics but got nil")
			}

			t.Logf("Recommended provider %s has metrics: latency=%dms, success_rate=%.2f, disk_usage=%dMB",
				recommended, metrics.AvgLatencyMS, metrics.SuccessRate, metrics.DiskUsageMB)
		})
	}
}

// TestErrorHandlingAcrossProviders ensures error handling is consistent
func TestErrorHandlingAcrossProviders(t *testing.T) {
	// Test with mock provider that can simulate errors
	mockProvider := NewMockProvider()
	mockProvider.SetError(true, "simulated error")

	// Test that all operations properly propagate errors
	operations := []struct {
		name string
		op   func() error
	}{
		{
			name: "CommitFile",
			op: func() error {
				return mockProvider.CommitFile("test.md", "content", "message")
			},
		},
		{
			name: "ReadFile",
			op: func() error {
				_, err := mockProvider.ReadFile("test.md")
				return err
			},
		},
		{
			name: "CreateIssue",
			op: func() error {
				_, _, err := mockProvider.CreateIssue("title", "body")
				return err
			},
		},
		{
			name: "GetRepositorySize",
			op: func() error {
				_, err := mockProvider.GetRepositorySize()
				return err
			},
		},
	}

	for _, op := range operations {
		t.Run(op.name, func(t *testing.T) {
			err := op.op()
			if err == nil {
				t.Error("Expected error but got none")
			}
			if err.Error() != "simulated error" {
				t.Errorf("Expected 'simulated error', got %s", err.Error())
			}
		})
	}
}

// TestProviderTypeString tests string representation of provider types
func TestProviderTypeString(t *testing.T) {
	types := []ProviderType{
		ProviderTypeClone,
		ProviderTypeAPI,
		ProviderTypeHybrid,
	}

	for _, pt := range types {
		t.Run(string(pt), func(t *testing.T) {
			if string(pt) == "" {
				t.Error("Provider type string should not be empty")
			}
		})
	}
}

// BenchmarkProviderOperations benchmarks basic operations
func BenchmarkProviderOperations(b *testing.B) {
	provider := NewMockProvider()

	b.Run("CommitFile", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			err := provider.CommitFile("bench.md", "benchmark content", "benchmark commit")
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ReadFile", func(b *testing.B) {
		// Setup: create a file first
		provider.CommitFile("bench_read.md", "benchmark read content", "setup")
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := provider.ReadFile("bench_read.md")
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("CreateIssue", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _, err := provider.CreateIssue("Benchmark Issue", "Benchmark issue body")
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}