package github

import (
	"fmt"
	gitconfig "github.com/msg2git/msg2git/internal/config"
)

// DefaultProviderFactory implements ProviderFactory
type DefaultProviderFactory struct{}

// NewProviderFactory creates a new provider factory
func NewProviderFactory() ProviderFactory {
	return &DefaultProviderFactory{}
}

// CreateProvider creates a GitHub provider based on the specified type
func (f *DefaultProviderFactory) CreateProvider(providerType ProviderType, config *ProviderConfig) (GitHubProvider, error) {
	switch providerType {
	case ProviderTypeClone:
		return NewCloneBasedProvider(config)
	case ProviderTypeAPI:
		return NewAPIBasedProvider(config)
	case ProviderTypeHybrid:
		return NewHybridProvider(config)
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", providerType)
	}
}

// NewCloneBasedProvider creates the current implementation (clone-based)
func NewCloneBasedProvider(config *ProviderConfig) (GitHubProvider, error) {
	// Convert to existing config format
	gitConfig := &gitconfig.Config{
		GitHubUsername: config.Config.GetGitHubUsername(),
		GitHubToken:    config.Config.GetGitHubToken(),
		GitHubRepo:     config.Config.GetGitHubRepo(),
		CommitAuthor:   config.Config.GetCommitAuthor(),
	}
	
	manager, err := NewManager(gitConfig, config.PremiumLevel)
	if err != nil {
		return nil, fmt.Errorf("failed to create clone-based provider: %w", err)
	}
	
	return &CloneBasedAdapter{
		manager: manager,
		config:  config,
	}, nil
}

// NewAPIBasedProvider creates the API-only implementation
func NewAPIBasedProvider(config *ProviderConfig) (GitHubProvider, error) {
	return newAPIProvider(config)
}

// NewHybridProvider creates a hybrid implementation
func NewHybridProvider(config *ProviderConfig) (GitHubProvider, error) {
	return nil, fmt.Errorf("hybrid provider not implemented yet")
}

// GetRecommendedProvider returns the recommended provider type based on usage patterns
func GetRecommendedProvider(userCount int, avgFilesPerCommit int, avgCommitsPerDay int) ProviderType {
	// Decision logic for provider selection
	totalOperationsPerDay := userCount * avgCommitsPerDay
	
	// If high volume or simple operations, prefer API
	if totalOperationsPerDay > 1000 || avgFilesPerCommit == 1 {
		return ProviderTypeAPI
	}
	
	// If complex multi-file operations, prefer clone
	if avgFilesPerCommit > 3 {
		return ProviderTypeClone
	}
	
	// Default to current stable implementation
	return ProviderTypeClone
}

// ProviderMetrics holds metrics for provider performance comparison
type ProviderMetrics struct {
	Type              ProviderType
	AvgLatencyMS      int64
	SuccessRate       float64
	DiskUsageMB       int64
	MemoryUsageMB     int64
	ConcurrentUsers   int
	ErrorRate         float64
	RateLimitHits     int
}

// GetProviderMetrics returns performance metrics for a provider type
func GetProviderMetrics(providerType ProviderType) *ProviderMetrics {
	switch providerType {
	case ProviderTypeClone:
		return &ProviderMetrics{
			Type:              ProviderTypeClone,
			AvgLatencyMS:      2000, // Higher due to clone operations
			SuccessRate:       0.98,
			DiskUsageMB:       50,   // Per user repository
			MemoryUsageMB:     10,   // Per operation
			ConcurrentUsers:   100,  // Limited by disk I/O
			ErrorRate:         0.02,
			RateLimitHits:     0,    // No API rate limits for git operations
		}
	case ProviderTypeAPI:
		return &ProviderMetrics{
			Type:              ProviderTypeAPI,
			AvgLatencyMS:      500,  // Faster HTTP calls
			SuccessRate:       0.95, // Slightly lower due to rate limits
			DiskUsageMB:       0,    // No local storage
			MemoryUsageMB:     2,    // Just HTTP requests
			ConcurrentUsers:   1000, // Much higher scalability
			ErrorRate:         0.05,
			RateLimitHits:     10,   // GitHub API rate limits
		}
	default:
		return nil
	}
}