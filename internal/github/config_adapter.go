package github

import (
	gitconfig "github.com/msg2git/msg2git/internal/config"
)

// ConfigAdapter adapts the existing config.Config to implement GitHubConfig interface
type ConfigAdapter struct {
	cfg *gitconfig.Config
}

// NewConfigAdapter creates a new config adapter
func NewConfigAdapter(cfg *gitconfig.Config) GitHubConfig {
	return &ConfigAdapter{cfg: cfg}
}

// Implement GitHubConfig interface
func (c *ConfigAdapter) GetGitHubUsername() string {
	return c.cfg.GitHubUsername
}

func (c *ConfigAdapter) GetGitHubToken() string {
	return c.cfg.GitHubToken
}

func (c *ConfigAdapter) GetGitHubRepo() string {
	return c.cfg.GitHubRepo
}

func (c *ConfigAdapter) GetCommitAuthor() string {
	return c.cfg.CommitAuthor
}

// Helper function to create ProviderConfig from existing config
func NewProviderConfig(cfg *gitconfig.Config, premiumLevel int, userID string) *ProviderConfig {
	return &ProviderConfig{
		Config:       NewConfigAdapter(cfg),
		PremiumLevel: premiumLevel,
		UserID:       userID,
	}
}