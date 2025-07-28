package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/msg2git/msg2git/internal/consts"
	"github.com/msg2git/msg2git/internal/logger"
)

// APIBasedProvider implements GitHubProvider using direct GitHub API calls
type APIBasedProvider struct {
	config     *ProviderConfig
	httpClient *http.Client
	baseURL    string
	
	// Repository information
	repoOwner string
	repoName  string
	
	// Rate limiting
	requestCount int
	lastReset    time.Time
	
	// Caching for repository info
	cachedRepoInfo *apiRepositoryInfo
	cacheExpiry    time.Time
}

// Ensure APIBasedProvider implements GitHubProvider interface
var _ GitHubProvider = (*APIBasedProvider)(nil)

// newAPIProvider creates a new API-only GitHub provider
func newAPIProvider(config *ProviderConfig) (*APIBasedProvider, error) {
	if config == nil || config.Config == nil {
		return nil, fmt.Errorf("provider config is required")
	}

	// Parse repository URL to get owner and name
	owner, repo, err := parseRepositoryURL(config.Config.GetGitHubRepo())
	if err != nil {
		return nil, fmt.Errorf("invalid repository URL: %w", err)
	}

	provider := &APIBasedProvider{
		config: config,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL:   "https://api.github.com",
		repoOwner: owner,
		repoName:  repo,
		lastReset: time.Now(),
	}

	logger.Info("API-based GitHub provider initialized", map[string]interface{}{
		"owner":         owner,
		"repo":          repo,
		"user_id":       config.UserID,
		"premium_level": config.PremiumLevel,
	})

	return provider, nil
}

// parseRepositoryURL extracts owner and repo name from GitHub URL
func parseRepositoryURL(repoURL string) (owner, repo string, err error) {
	// Remove .git suffix if present
	repoURL = strings.TrimSuffix(repoURL, ".git")
	
	// Handle different GitHub URL formats
	if strings.Contains(repoURL, "github.com/") {
		parts := strings.Split(repoURL, "github.com/")
		if len(parts) != 2 {
			return "", "", fmt.Errorf("invalid GitHub URL format")
		}
		
		pathParts := strings.Split(parts[1], "/")
		if len(pathParts) < 2 {
			return "", "", fmt.Errorf("invalid GitHub repository path")
		}
		
		owner = pathParts[0]
		repo = pathParts[1]
		return owner, repo, nil
	}
	
	// Handle simple "owner/repo" format
	pathParts := strings.Split(repoURL, "/")
	if len(pathParts) == 2 && pathParts[0] != "" && pathParts[1] != "" {
		owner = pathParts[0]
		repo = pathParts[1]
		return owner, repo, nil
	}
	
	return "", "", fmt.Errorf("not a valid GitHub URL or owner/repo format")
}

// makeAPIRequest makes an authenticated request to GitHub API with rate limiting
func (p *APIBasedProvider) makeAPIRequest(method, endpoint string, body interface{}) (*http.Response, error) {
	// Basic rate limiting (5000 requests per hour = ~1.4 per second)
	if err := p.checkRateLimit(); err != nil {
		return nil, err
	}

	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	url := p.baseURL + endpoint
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set authentication header
	req.Header.Set("Authorization", "Bearer "+p.config.Config.GetGitHubToken())
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	logger.Debug("Making GitHub API request", map[string]interface{}{
		"method":   method,
		"endpoint": endpoint,
		"user_id":  p.config.UserID,
	})

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}

	p.requestCount++

	// Check for API errors
	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		
		logger.Error("GitHub API error", map[string]interface{}{
			"status_code": resp.StatusCode,
			"response":    string(bodyBytes),
			"endpoint":    endpoint,
			"user_id":     p.config.UserID,
		})
		
		// Handle specific error cases with user-friendly messages
		switch resp.StatusCode {
		case 401:
			// Parse response to check for specific auth issues
			if strings.Contains(string(bodyBytes), "Bad credentials") {
				return nil, fmt.Errorf(consts.GitHubAuthFailed)
			}
			// Other 401 scenarios
			return nil, fmt.Errorf("unauthorized - check GitHub token permissions")
		case 403:
			if strings.Contains(string(bodyBytes), "rate limit") {
				return nil, fmt.Errorf("GitHub API rate limit exceeded - please try again later")
			}
			return nil, fmt.Errorf("forbidden - token may not have required permissions")
		case 404:
			if strings.Contains(endpoint, "/repos/") {
				return nil, fmt.Errorf(consts.GitHubRepoNotFound + " - check repository URL and permissions")
			}
			return nil, fmt.Errorf("resource not found")
		default:
			return nil, fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, string(bodyBytes))
		}
	}

	return resp, nil
}

// checkRateLimit implements basic rate limiting
func (p *APIBasedProvider) checkRateLimit() error {
	now := time.Now()
	
	// Reset counter every hour
	if now.Sub(p.lastReset) > time.Hour {
		p.requestCount = 0
		p.lastReset = now
	}
	
	// GitHub allows 5000 requests per hour for authenticated requests
	if p.requestCount >= 4900 { // Leave some buffer
		return fmt.Errorf("rate limit exceeded, please try again later")
	}
	
	return nil
}

// GetProviderType returns the provider type
func (p *APIBasedProvider) GetProviderType() ProviderType {
	return ProviderTypeAPI
}

// GetConfig returns the provider configuration
func (p *APIBasedProvider) GetConfig() *ProviderConfig {
	return p.config
}

// HealthCheck performs a health check on the provider
func (p *APIBasedProvider) HealthCheck() error {
	// Test API connectivity with a simple repository info call
	endpoint := fmt.Sprintf("/repos/%s/%s", p.repoOwner, p.repoName)
	resp, err := p.makeAPIRequest("GET", endpoint, nil)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()
	
	return nil
}

// getRepositoryInfo gets repository info from GitHub API
func (p *APIBasedProvider) getRepositoryInfo() (*apiRepositoryInfo, error) {
	// Check cache first
	if p.cachedRepoInfo != nil && time.Now().Before(p.cacheExpiry) {
		logger.Debug("Using cached repository info", map[string]interface{}{
			"user_id":      p.config.UserID,
			"cache_expiry": p.cacheExpiry,
			"size_kb":      p.cachedRepoInfo.Size,
		})
		return p.cachedRepoInfo, nil
	}

	endpoint := fmt.Sprintf("/repos/%s/%s", p.repoOwner, p.repoName)
	
	resp, err := p.makeAPIRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository info: %w", err)
	}
	defer resp.Body.Close()

	var repoInfo apiRepositoryInfo
	if err := json.NewDecoder(resp.Body).Decode(&repoInfo); err != nil {
		return nil, fmt.Errorf("failed to decode repository info: %w", err)
	}

	// Cache the result for 5 minutes (same as bot-level caching)
	p.cachedRepoInfo = &repoInfo
	p.cacheExpiry = time.Now().Add(5 * time.Minute)
	
	logger.Debug("Cached repository info", map[string]interface{}{
		"user_id":      p.config.UserID,
		"cache_expiry": p.cacheExpiry,
		"size_kb":      repoInfo.Size,
	})

	return &repoInfo, nil
}

// invalidateRepositoryCache invalidates the cached repository info
func (p *APIBasedProvider) invalidateRepositoryCache() {
	p.cachedRepoInfo = nil
	p.cacheExpiry = time.Time{}
	logger.Debug("Repository info cache invalidated", map[string]interface{}{
		"user_id": p.config.UserID,
	})
}

// refreshRepositoryCache forces a refresh of the repository info cache
func (p *APIBasedProvider) refreshRepositoryCache() (*apiRepositoryInfo, error) {
	p.invalidateRepositoryCache()
	return p.getRepositoryInfo()
}

// GetMetrics returns current metrics for this provider instance
func (p *APIBasedProvider) GetMetrics() *ProviderMetrics {
	metrics := GetProviderMetrics(ProviderTypeAPI)
	
	// Add instance-specific metrics
	if metrics != nil {
		// Update with actual request count
		metrics.RateLimitHits = p.requestCount
	}
	
	return metrics
}