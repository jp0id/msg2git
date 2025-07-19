package github

import "fmt"

// CloneBasedAdapter adapts the existing Manager to implement GitHubProvider interface
// This allows us to keep the current implementation while providing a clean interface
type CloneBasedAdapter struct {
	manager *Manager
	config  *ProviderConfig
}

// Ensure CloneBasedAdapter implements GitHubProvider
var _ GitHubProvider = (*CloneBasedAdapter)(nil)

// RepositoryManager implementation
func (a *CloneBasedAdapter) EnsureRepositoryWithPremium(premiumLevel int) error {
	return a.manager.EnsureRepositoryWithPremium(premiumLevel)
}

func (a *CloneBasedAdapter) NeedsClone() bool {
	return a.manager.NeedsClone()
}

func (a *CloneBasedAdapter) GetRepoInfo() (owner, repo string, err error) {
	return a.manager.GetRepoInfo()
}

func (a *CloneBasedAdapter) GetRepositorySize() (int64, error) {
	return a.manager.GetRepositorySize()
}

func (a *CloneBasedAdapter) GetRepositoryMaxSize() float64 {
	return a.manager.GetRepositoryMaxSize()
}

func (a *CloneBasedAdapter) GetRepositoryMaxSizeWithPremium(premiumLevel int) float64 {
	return a.manager.GetRepositoryMaxSizeWithPremium(premiumLevel)
}

func (a *CloneBasedAdapter) GetRepositorySizeInfo() (float64, float64, error) {
	return a.manager.GetRepositorySizeInfo()
}

func (a *CloneBasedAdapter) GetRepositorySizeInfoWithPremium(premiumLevel int) (float64, float64, error) {
	return a.manager.GetRepositorySizeInfoWithPremium(premiumLevel)
}

func (a *CloneBasedAdapter) IsRepositoryNearCapacity() (bool, float64, error) {
	return a.manager.IsRepositoryNearCapacity()
}

func (a *CloneBasedAdapter) IsRepositoryNearCapacityWithPremium(premiumLevel int) (bool, float64, error) {
	return a.manager.IsRepositoryNearCapacityWithPremium(premiumLevel)
}

func (a *CloneBasedAdapter) GetDefaultBranch() (string, error) {
	return a.manager.GetDefaultBranch()
}

func (a *CloneBasedAdapter) GetGitHubFileURL(filename string) (string, error) {
	return a.manager.GetGitHubFileURL(filename)
}

func (a *CloneBasedAdapter) GetGitHubFileURLWithBranch(filename string) (string, error) {
	return a.manager.GetGitHubFileURLWithBranch(filename)
}

// FileManager implementation
func (a *CloneBasedAdapter) CommitFile(filename, content, commitMessage string) error {
	return a.manager.CommitFile(filename, content, commitMessage)
}

func (a *CloneBasedAdapter) CommitFileWithAuthor(filename, content, commitMessage, customAuthor string) error {
	return a.manager.CommitFileWithAuthor(filename, content, commitMessage, customAuthor)
}

func (a *CloneBasedAdapter) CommitFileWithAuthorAndPremium(filename, content, commitMessage, customAuthor string, premiumLevel int) error {
	return a.manager.CommitFileWithAuthorAndPremium(filename, content, commitMessage, customAuthor, premiumLevel)
}

func (a *CloneBasedAdapter) ReplaceFile(filename, content, commitMessage string) error {
	return a.manager.ReplaceFile(filename, content, commitMessage)
}

func (a *CloneBasedAdapter) ReplaceFileWithAuthor(filename, content, commitMessage, customAuthor string) error {
	return a.manager.ReplaceFileWithAuthor(filename, content, commitMessage, customAuthor)
}

func (a *CloneBasedAdapter) ReplaceFileWithAuthorAndPremium(filename, content, commitMessage, customAuthor string, premiumLevel int) error {
	return a.manager.ReplaceFileWithAuthorAndPremium(filename, content, commitMessage, customAuthor, premiumLevel)
}

func (a *CloneBasedAdapter) ReplaceMultipleFilesWithAuthorAndPremium(files map[string]string, commitMessage, customAuthor string, premiumLevel int) error {
	return a.manager.ReplaceMultipleFilesWithAuthorAndPremium(files, commitMessage, customAuthor, premiumLevel)
}

func (a *CloneBasedAdapter) CommitBinaryFile(filename string, data []byte, commitMessage string) error {
	return a.manager.CommitBinaryFile(filename, data, commitMessage)
}

func (a *CloneBasedAdapter) ReadFile(filename string) (string, error) {
	return a.manager.ReadFile(filename)
}

// IssueManager implementation
func (a *CloneBasedAdapter) CreateIssue(title, body string) (string, int, error) {
	return a.manager.CreateIssue(title, body)
}

func (a *CloneBasedAdapter) GetIssueStatus(issueNumber int) (*IssueStatus, error) {
	return a.manager.GetIssueStatus(issueNumber)
}

func (a *CloneBasedAdapter) SyncIssueStatuses(issueNumbers []int) (map[int]*IssueStatus, error) {
	return a.manager.SyncIssueStatuses(issueNumbers)
}

func (a *CloneBasedAdapter) AddIssueComment(issueNumber int, commentText string) (string, error) {
	return a.manager.AddIssueComment(issueNumber, commentText)
}

func (a *CloneBasedAdapter) CloseIssue(issueNumber int) error {
	return a.manager.CloseIssue(issueNumber)
}

// AssetManager implementation
func (a *CloneBasedAdapter) UploadImageToCDN(filename string, data []byte) (string, error) {
	return a.manager.UploadImageToCDN(filename, data)
}

// Additional helper methods for the adapter

// GetUnderlyingManager returns the underlying manager for backward compatibility
// This should only be used during migration period
func (a *CloneBasedAdapter) GetUnderlyingManager() *Manager {
	return a.manager
}

// GetProviderType returns the provider type
func (a *CloneBasedAdapter) GetProviderType() ProviderType {
	return ProviderTypeClone
}

// GetConfig returns the provider configuration
func (a *CloneBasedAdapter) GetConfig() *ProviderConfig {
	return a.config
}

// HealthCheck performs a health check on the provider
func (a *CloneBasedAdapter) HealthCheck() error {
	// Basic health checks for clone-based provider
	if a.manager == nil {
		return fmt.Errorf("manager is nil")
	}
	
	// Check if we can get repo info
	_, _, err := a.manager.GetRepoInfo()
	if err != nil {
		return fmt.Errorf("cannot get repo info: %w", err)
	}
	
	return nil
}

// GetMetrics returns current metrics for this provider instance
func (a *CloneBasedAdapter) GetMetrics() *ProviderMetrics {
	metrics := GetProviderMetrics(ProviderTypeClone)
	
	// Add instance-specific metrics if available
	// This could be extended to track actual performance
	
	return metrics
}