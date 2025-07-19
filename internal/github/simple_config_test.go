package github

import (
	"testing"
)

func TestConfigAdapterInterface(t *testing.T) {
	// Test that our mock config implements the interface
	config := &MockGitHubConfig{
		Username: "testuser",
		Token:    "test-token",
		Repo:     "https://github.com/testuser/testrepo.git",
		Author:   "Test User <test@example.com>",
	}

	// Test interface compliance
	var _ GitHubConfig = config

	// Test values
	if config.GetGitHubUsername() != "testuser" {
		t.Errorf("Expected 'testuser', got %s", config.GetGitHubUsername())
	}

	if config.GetGitHubToken() != "test-token" {
		t.Errorf("Expected 'test-token', got %s", config.GetGitHubToken())
	}

	if config.GetGitHubRepo() != "https://github.com/testuser/testrepo.git" {
		t.Errorf("Expected repo URL, got %s", config.GetGitHubRepo())
	}

	if config.GetCommitAuthor() != "Test User <test@example.com>" {
		t.Errorf("Expected author, got %s", config.GetCommitAuthor())
	}
}

func TestProviderConfigCreation(t *testing.T) {
	config := &MockGitHubConfig{
		Username: "configuser",
		Token:    "config-token",
		Repo:     "https://github.com/configuser/configrepo.git",
		Author:   "Config User <config@example.com>",
	}

	// Create provider config manually (simulating NewProviderConfig)
	providerConfig := &ProviderConfig{
		Config:       config,
		PremiumLevel: 2,
		UserID:       "test-user-123",
	}

	if providerConfig.PremiumLevel != 2 {
		t.Errorf("Expected premium level 2, got %d", providerConfig.PremiumLevel)
	}

	if providerConfig.UserID != "test-user-123" {
		t.Errorf("Expected user ID 'test-user-123', got %s", providerConfig.UserID)
	}

	if providerConfig.Config.GetGitHubUsername() != "configuser" {
		t.Errorf("Expected 'configuser', got %s", providerConfig.Config.GetGitHubUsername())
	}
}

func TestGitHubConfigDefaults(t *testing.T) {
	// Test with empty mock config (should use defaults)
	config := &MockGitHubConfig{}

	if config.GetGitHubUsername() != "testuser" {
		t.Errorf("Expected default username 'testuser', got %s", config.GetGitHubUsername())
	}

	if config.GetGitHubToken() != "test-token" {
		t.Errorf("Expected default token 'test-token', got %s", config.GetGitHubToken())
	}
}

func TestProviderConfigValidation(t *testing.T) {
	tests := []struct {
		name         string
		config       GitHubConfig
		premiumLevel int
		userID       string
		valid        bool
	}{
		{
			name: "Valid config",
			config: &MockGitHubConfig{
				Username: "user",
				Token:    "token",
				Repo:     "https://github.com/user/repo.git",
				Author:   "User <user@example.com>",
			},
			premiumLevel: 1,
			userID:       "user123",
			valid:        true,
		},
		{
			name: "Empty user ID",
			config: &MockGitHubConfig{
				Username: "user",
				Token:    "token",
				Repo:     "https://github.com/user/repo.git",
				Author:   "User <user@example.com>",
			},
			premiumLevel: 1,
			userID:       "",
			valid:        false,
		},
		{
			name: "Invalid premium level",
			config: &MockGitHubConfig{
				Username: "user",
				Token:    "token",
				Repo:     "https://github.com/user/repo.git",
				Author:   "User <user@example.com>",
			},
			premiumLevel: -1,
			userID:       "user123",
			valid:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providerConfig := &ProviderConfig{
				Config:       tt.config,
				PremiumLevel: tt.premiumLevel,
				UserID:       tt.userID,
			}

			// Basic validation
			isValid := providerConfig.Config != nil &&
				providerConfig.UserID != "" &&
				providerConfig.PremiumLevel >= 0

			if isValid != tt.valid {
				t.Errorf("Expected valid=%v, got valid=%v", tt.valid, isValid)
			}
		})
	}
}