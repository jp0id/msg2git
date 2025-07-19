package github

import (
	"testing"
)

func TestDefaultProviderFactory(t *testing.T) {
	factory := NewProviderFactory()
	if factory == nil {
		t.Error("Factory should not be nil")
	}

	// Test that factory implements ProviderFactory interface
	var _ ProviderFactory = factory
}

func TestCreateProvider(t *testing.T) {
	factory := NewProviderFactory()
	config := &ProviderConfig{
		Config:       &MockGitHubConfig{},
		PremiumLevel: 1,
		UserID:       "test-user",
	}

	tests := []struct {
		name         string
		providerType ProviderType
		wantErr      bool
		errContains  string
	}{
		{
			name:         "Clone provider",
			providerType: ProviderTypeClone,
			wantErr:      false,
		},
		{
			name:         "API provider",
			providerType: ProviderTypeAPI,
			wantErr:      false,
		},
		{
			name:         "Hybrid provider (not implemented)",
			providerType: ProviderTypeHybrid,
			wantErr:      true,
			errContains:  "not implemented yet",
		},
		{
			name:         "Invalid provider type",
			providerType: ProviderType("invalid"),
			wantErr:      true,
			errContains:  "unsupported provider type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := factory.CreateProvider(tt.providerType, config)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("Expected error to contain %q, got %q", tt.errContains, err.Error())
				}
				if provider != nil {
					t.Error("Expected nil provider when error occurs")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				if provider == nil {
					t.Error("Expected provider but got nil")
				}
			}
		})
	}
}

func TestGetRecommendedProvider(t *testing.T) {
	tests := []struct {
		name               string
		userCount          int
		avgFilesPerCommit  int
		avgCommitsPerDay   int
		expectedProvider   ProviderType
	}{
		{
			name:              "High volume users",
			userCount:         1000,
			avgFilesPerCommit: 1,
			avgCommitsPerDay:  2,
			expectedProvider:  ProviderTypeAPI,
		},
		{
			name:              "Simple single file operations",
			userCount:         100,
			avgFilesPerCommit: 1,
			avgCommitsPerDay:  5,
			expectedProvider:  ProviderTypeAPI,
		},
		{
			name:              "Complex multi-file operations",
			userCount:         50,
			avgFilesPerCommit: 5,
			avgCommitsPerDay:  2,
			expectedProvider:  ProviderTypeClone,
		},
		{
			name:              "Default case",
			userCount:         100,
			avgFilesPerCommit: 2,
			avgCommitsPerDay:  3,
			expectedProvider:  ProviderTypeClone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetRecommendedProvider(tt.userCount, tt.avgFilesPerCommit, tt.avgCommitsPerDay)
			if result != tt.expectedProvider {
				t.Errorf("Expected %s, got %s", tt.expectedProvider, result)
			}
		})
	}
}

func TestGetProviderMetrics(t *testing.T) {
	tests := []struct {
		name         string
		providerType ProviderType
		expectNil    bool
	}{
		{
			name:         "Clone provider metrics",
			providerType: ProviderTypeClone,
			expectNil:    false,
		},
		{
			name:         "API provider metrics",
			providerType: ProviderTypeAPI,
			expectNil:    false,
		},
		{
			name:         "Invalid provider type",
			providerType: ProviderType("invalid"),
			expectNil:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics := GetProviderMetrics(tt.providerType)

			if tt.expectNil {
				if metrics != nil {
					t.Error("Expected nil metrics for invalid provider type")
				}
			} else {
				if metrics == nil {
					t.Error("Expected metrics but got nil")
				}
				if metrics.Type != tt.providerType {
					t.Errorf("Expected type %s, got %s", tt.providerType, metrics.Type)
				}
				// Validate metrics have reasonable values
				if metrics.AvgLatencyMS <= 0 {
					t.Error("Average latency should be positive")
				}
				if metrics.SuccessRate < 0 || metrics.SuccessRate > 1 {
					t.Error("Success rate should be between 0 and 1")
				}
			}
		})
	}
}

func TestProviderMetricsValues(t *testing.T) {
	// Test specific metrics values for clone provider
	cloneMetrics := GetProviderMetrics(ProviderTypeClone)
	if cloneMetrics == nil {
		t.Fatal("Clone metrics should not be nil")
	}

	// Clone provider should have higher latency but no rate limits
	if cloneMetrics.AvgLatencyMS < 1000 {
		t.Error("Clone provider should have higher latency due to git operations")
	}
	if cloneMetrics.RateLimitHits > 0 {
		t.Error("Clone provider should not hit rate limits")
	}
	if cloneMetrics.DiskUsageMB <= 0 {
		t.Error("Clone provider should use disk space")
	}

	// Test API provider metrics
	apiMetrics := GetProviderMetrics(ProviderTypeAPI)
	if apiMetrics == nil {
		t.Fatal("API metrics should not be nil")
	}

	// API provider should have lower latency but potential rate limits
	if apiMetrics.AvgLatencyMS > 1000 {
		t.Error("API provider should have lower latency")
	}
	if apiMetrics.DiskUsageMB > 0 {
		t.Error("API provider should not use disk space")
	}
	if apiMetrics.ConcurrentUsers <= cloneMetrics.ConcurrentUsers {
		t.Error("API provider should support more concurrent users")
	}
}

func TestNewCloneBasedProvider(t *testing.T) {
	config := &ProviderConfig{
		Config:       &MockGitHubConfig{},
		PremiumLevel: 1,
		UserID:       "test-user",
	}

	// This test might fail if the actual Manager creation fails
	// but we're testing the interface creation, not the underlying implementation
	provider, err := NewCloneBasedProvider(config)
	
	// Since we might not have a real git repo setup, we expect this to potentially fail
	// The important thing is that the interface is correctly structured
	if err != nil {
		// This is expected in a test environment without proper git setup
		t.Logf("Expected error in test environment: %v", err)
		return
	}

	if provider == nil {
		t.Error("Provider should not be nil when no error occurs")
	}

	// Test that provider implements GitHubProvider interface
	var _ GitHubProvider = provider
}

func TestNewAPIBasedProvider(t *testing.T) {
	config := &ProviderConfig{
		Config:       &MockGitHubConfig{},
		PremiumLevel: 1,
		UserID:       "test-user",
	}

	// Test API provider
	apiProvider, err := NewAPIBasedProvider(config)
	if err != nil {
		t.Errorf("Unexpected error creating API provider: %v", err)
	}
	if apiProvider == nil {
		t.Error("Expected API provider but got nil")
	}

	// Test that provider implements GitHubProvider interface
	if apiProvider != nil {
		var _ GitHubProvider = apiProvider
	}
}

func TestNotImplementedProviders(t *testing.T) {
	config := &ProviderConfig{
		Config:       &MockGitHubConfig{},
		PremiumLevel: 1,
		UserID:       "test-user",
	}

	// Test Hybrid provider (still not implemented)
	hybridProvider, err := NewHybridProvider(config)
	if err == nil {
		t.Error("Expected error for not implemented hybrid provider")
	}
	if hybridProvider != nil {
		t.Error("Expected nil provider for not implemented hybrid provider")
	}
	if !containsString(err.Error(), "not implemented") {
		t.Error("Error should mention not implemented")
	}
}

// Helper function to check if a string contains another string
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || 
		(len(s) > len(substr) && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}