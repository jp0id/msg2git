package github

import (
	"fmt"

	"github.com/msg2git/msg2git/internal/logger"
)

// Repository metadata structure from GitHub API
type apiRepositoryInfo struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Size          int    `json:"size"` // Size in KB
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
}

// RepositoryManager implementation for API provider
func (p *APIBasedProvider) EnsureRepositoryWithPremium(premiumLevel int) error {
	// For API provider, we don't need to clone anything
	// Just verify the repository exists and is accessible
	repoInfo, err := p.getRepositoryInfo()
	if err != nil {
		return fmt.Errorf("failed to access repository: %w", err)
	}

	logger.Info("Repository verified via API", map[string]interface{}{
		"repo_id":        repoInfo.ID,
		"full_name":      repoInfo.FullName,
		"size_kb":        repoInfo.Size,
		"default_branch": repoInfo.DefaultBranch,
		"user_id":        p.config.UserID,
	})

	return nil
}

func (p *APIBasedProvider) NeedsClone() bool {
	// API provider never needs cloning
	return false
}

func (p *APIBasedProvider) GetRepoInfo() (owner, repo string, err error) {
	return p.repoOwner, p.repoName, nil
}

func (p *APIBasedProvider) GetRepositorySize() (int64, error) {
	repoInfo, err := p.getRepositoryInfo()
	if err != nil {
		return 0, fmt.Errorf("failed to get repository size: %w", err)
	}

	// GitHub API returns size in KB, convert to bytes
	return int64(repoInfo.Size * 1024), nil
}

func (p *APIBasedProvider) GetRepositoryMaxSize() float64 {
	// Base repository size limit (1MB for free tier) - return in MB to match interface contract
	return 1.0 // 1MB in MB
}

func (p *APIBasedProvider) GetRepositoryMaxSizeWithPremium(premiumLevel int) float64 {
	baseSize := p.GetRepositoryMaxSize()

	// Premium multipliers based on level (same as clone-based provider)
	switch premiumLevel {
	case 0:
		// Free tier - 1MB
		return baseSize
	case 1:
		// Coffee tier ($5) - 2x capacity (2MB)
		return baseSize * 2
	case 2:
		// Cake tier ($15) - 4x capacity (4MB)
		return baseSize * 4
	case 3:
		// Sponsor tier ($50) - 10x capacity (10MB)
		return baseSize * 10
	default:
		// Unknown premium level, return base size
		return baseSize
	}
}

func (p *APIBasedProvider) GetRepositorySizeInfo() (float64, float64, error) {
	size, err := p.GetRepositorySize()
	if err != nil {
		return 0, 0, err
	}

	// Convert bytes to MB and calculate percentage to match interface contract
	sizeMB := float64(size) / 1024 / 1024
	maxSizeMB := p.GetRepositoryMaxSize()
	percentage := (sizeMB / maxSizeMB) * 100
	return sizeMB, percentage, nil
}

func (p *APIBasedProvider) GetRepositorySizeInfoWithPremium(premiumLevel int) (float64, float64, error) {
	size, err := p.GetRepositorySize()
	if err != nil {
		return 0, 0, err
	}

	// Convert bytes to MB and calculate percentage to match interface contract
	sizeMB := float64(size) / 1024 / 1024
	maxSizeMB := p.GetRepositoryMaxSizeWithPremium(premiumLevel)
	percentage := (sizeMB / maxSizeMB) * 100
	return sizeMB, percentage, nil
}

func (p *APIBasedProvider) IsRepositoryNearCapacity() (bool, float64, error) {
	_, percentage, err := p.GetRepositorySizeInfo()
	if err != nil {
		return false, 0, err
	}

	isNear := percentage > 80 // Consider >80% as near capacity

	return isNear, percentage, nil
}

func (p *APIBasedProvider) IsRepositoryNearCapacityWithPremium(premiumLevel int) (bool, float64, error) {
	_, percentage, err := p.GetRepositorySizeInfoWithPremium(premiumLevel)
	if err != nil {
		return false, 0, err
	}

	isNear := percentage > 80 // Consider >80% as near capacity

	return isNear, percentage, nil
}

func (p *APIBasedProvider) GetDefaultBranch() (string, error) {
	repoInfo, err := p.getRepositoryInfo()
	if err != nil {
		return "", fmt.Errorf("failed to get repository info: %w", err)
	}

	return repoInfo.DefaultBranch, nil
}

func (p *APIBasedProvider) GetGitHubFileURL(filename string) (string, error) {
	return p.GetGitHubFileURLWithBranch(filename)
}

func (p *APIBasedProvider) GetGitHubFileURLWithBranch(filename string) (string, error) {
	// Get the actual default branch
	defaultBranch, err := p.GetDefaultBranch()
	if err != nil {
		return "", fmt.Errorf("failed to get default branch: %w", err)
	}

	// For API provider, we can construct the URL directly using the actual default branch
	url := fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s", p.repoOwner, p.repoName, defaultBranch, filename)
	return url, nil
}

