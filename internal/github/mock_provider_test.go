package github

import "fmt"

// MockProvider implements GitHubProvider for testing
type MockProvider struct {
	files        map[string]string
	issues       map[int]*IssueStatus
	repoOwner    string
	repoName     string
	repoSize     int64
	maxSize      float64
	shouldError  bool
	errorMessage string
}

func NewMockProvider() *MockProvider {
	return &MockProvider{
		files:     make(map[string]string),
		issues:    make(map[int]*IssueStatus),
		repoOwner: "testowner",
		repoName:  "testrepo",
		repoSize:  1024,
		maxSize:   1048576, // 1MB
	}
}

// Implement GitHubProvider interface
var _ GitHubProvider = (*MockProvider)(nil)

// RepositoryManager implementation
func (m *MockProvider) EnsureRepositoryWithPremium(premiumLevel int) error {
	if m.shouldError {
		return fmt.Errorf(m.errorMessage)
	}
	return nil
}

func (m *MockProvider) NeedsClone() bool {
	return false
}

func (m *MockProvider) GetRepoInfo() (owner, repo string, err error) {
	if m.shouldError {
		return "", "", fmt.Errorf(m.errorMessage)
	}
	return m.repoOwner, m.repoName, nil
}

func (m *MockProvider) GetRepositorySize() (int64, error) {
	if m.shouldError {
		return 0, fmt.Errorf(m.errorMessage)
	}
	return m.repoSize, nil
}

func (m *MockProvider) GetRepositoryMaxSize() float64 {
	return m.maxSize
}

func (m *MockProvider) GetRepositoryMaxSizeWithPremium(premiumLevel int) float64 {
	return m.maxSize * float64(premiumLevel+1)
}

func (m *MockProvider) GetRepositorySizeInfo() (float64, float64, error) {
	if m.shouldError {
		return 0, 0, fmt.Errorf(m.errorMessage)
	}
	sizeMB := float64(m.repoSize) / 1024 / 1024
	percentage := (sizeMB / m.maxSize) * 100
	return sizeMB, percentage, nil
}

func (m *MockProvider) GetRepositorySizeInfoWithPremium(premiumLevel int) (float64, float64, error) {
	if m.shouldError {
		return 0, 0, fmt.Errorf(m.errorMessage)
	}
	sizeMB := float64(m.repoSize) / 1024 / 1024
	maxSizeMB := m.maxSize * float64(premiumLevel+1)
	percentage := (sizeMB / maxSizeMB) * 100
	return sizeMB, percentage, nil
}

func (m *MockProvider) IsRepositoryNearCapacity() (bool, float64, error) {
	if m.shouldError {
		return false, 0, fmt.Errorf(m.errorMessage)
	}
	percentage := float64(m.repoSize) / m.maxSize * 100
	return percentage > 80, percentage, nil
}

func (m *MockProvider) IsRepositoryNearCapacityWithPremium(premiumLevel int) (bool, float64, error) {
	if m.shouldError {
		return false, 0, fmt.Errorf(m.errorMessage)
	}
	maxSize := m.maxSize * float64(premiumLevel+1)
	percentage := float64(m.repoSize) / maxSize * 100
	return percentage > 80, percentage, nil
}

func (m *MockProvider) GetDefaultBranch() (string, error) {
	if m.shouldError {
		return "", fmt.Errorf(m.errorMessage)
	}
	return "main", nil
}

func (m *MockProvider) GetGitHubFileURL(filename string) (string, error) {
	if m.shouldError {
		return "", fmt.Errorf(m.errorMessage)
	}
	return fmt.Sprintf("https://github.com/%s/%s/blob/main/%s", m.repoOwner, m.repoName, filename), nil
}

func (m *MockProvider) GetGitHubFileURLWithBranch(filename string) (string, error) {
	if m.shouldError {
		return "", fmt.Errorf(m.errorMessage)
	}
	return fmt.Sprintf("https://github.com/%s/%s/blob/main/%s", m.repoOwner, m.repoName, filename), nil
}

// FileManager implementation
func (m *MockProvider) CommitFile(filename, content, commitMessage string) error {
	if m.shouldError {
		return fmt.Errorf(m.errorMessage)
	}
	m.files[filename] = content
	return nil
}

func (m *MockProvider) CommitFileWithAuthor(filename, content, commitMessage, customAuthor string) error {
	if m.shouldError {
		return fmt.Errorf(m.errorMessage)
	}
	m.files[filename] = content
	return nil
}

func (m *MockProvider) CommitFileWithAuthorAndPremium(filename, content, commitMessage, customAuthor string, premiumLevel int) error {
	if m.shouldError {
		return fmt.Errorf(m.errorMessage)
	}
	m.files[filename] = content
	return nil
}

func (m *MockProvider) ReplaceFile(filename, content, commitMessage string) error {
	if m.shouldError {
		return fmt.Errorf(m.errorMessage)
	}
	m.files[filename] = content
	return nil
}

func (m *MockProvider) ReplaceFileWithAuthor(filename, content, commitMessage, customAuthor string) error {
	if m.shouldError {
		return fmt.Errorf(m.errorMessage)
	}
	m.files[filename] = content
	return nil
}

func (m *MockProvider) ReplaceFileWithAuthorAndPremium(filename, content, commitMessage, customAuthor string, premiumLevel int) error {
	if m.shouldError {
		return fmt.Errorf(m.errorMessage)
	}
	m.files[filename] = content
	return nil
}

func (m *MockProvider) ReplaceMultipleFilesWithAuthorAndPremium(files map[string]string, commitMessage, customAuthor string, premiumLevel int) error {
	if m.shouldError {
		return fmt.Errorf(m.errorMessage)
	}
	for filename, content := range files {
		m.files[filename] = content
	}
	return nil
}

func (m *MockProvider) CommitBinaryFile(filename string, data []byte, commitMessage string) error {
	if m.shouldError {
		return fmt.Errorf(m.errorMessage)
	}
	m.files[filename] = string(data)
	return nil
}

func (m *MockProvider) ReadFile(filename string) (string, error) {
	if m.shouldError {
		return "", fmt.Errorf(m.errorMessage)
	}
	content, exists := m.files[filename]
	if !exists {
		return "", fmt.Errorf("file not found")
	}
	return content, nil
}

// IssueManager implementation
func (m *MockProvider) CreateIssue(title, body string) (string, int, error) {
	if m.shouldError {
		return "", 0, fmt.Errorf(m.errorMessage)
	}
	issueNumber := len(m.issues) + 1
	issue := &IssueStatus{
		Number:  issueNumber,
		Title:   title,
		State:   "open",
		HTMLURL: fmt.Sprintf("https://github.com/%s/%s/issues/%d", m.repoOwner, m.repoName, issueNumber),
	}
	m.issues[issueNumber] = issue
	return issue.HTMLURL, issueNumber, nil
}

func (m *MockProvider) GetIssueStatus(issueNumber int) (*IssueStatus, error) {
	if m.shouldError {
		return nil, fmt.Errorf(m.errorMessage)
	}
	issue, exists := m.issues[issueNumber]
	if !exists {
		return nil, fmt.Errorf("issue not found")
	}
	return issue, nil
}

func (m *MockProvider) SyncIssueStatuses(issueNumbers []int) (map[int]*IssueStatus, error) {
	if m.shouldError {
		return nil, fmt.Errorf(m.errorMessage)
	}
	result := make(map[int]*IssueStatus)
	for _, num := range issueNumbers {
		if issue, exists := m.issues[num]; exists {
			result[num] = issue
		}
	}
	return result, nil
}

func (m *MockProvider) AddIssueComment(issueNumber int, commentText string) (string, error) {
	if m.shouldError {
		return "", fmt.Errorf(m.errorMessage)
	}
	if _, exists := m.issues[issueNumber]; !exists {
		return "", fmt.Errorf("issue not found")
	}
	return fmt.Sprintf("https://github.com/%s/%s/issues/%d#comment", m.repoOwner, m.repoName, issueNumber), nil
}

func (m *MockProvider) CloseIssue(issueNumber int) error {
	if m.shouldError {
		return fmt.Errorf(m.errorMessage)
	}
	if issue, exists := m.issues[issueNumber]; exists {
		issue.State = "closed"
	}
	return nil
}

// AssetManager implementation
func (m *MockProvider) UploadImageToCDN(filename string, data []byte) (string, error) {
	if m.shouldError {
		return "", fmt.Errorf(m.errorMessage)
	}
	return fmt.Sprintf("https://github.com/%s/%s/releases/download/assets/%s", m.repoOwner, m.repoName, filename), nil
}

// Helper methods for testing
func (m *MockProvider) SetError(shouldError bool, message string) {
	m.shouldError = shouldError
	m.errorMessage = message
}

func (m *MockProvider) GetFiles() map[string]string {
	return m.files
}

func (m *MockProvider) GetIssues() map[int]*IssueStatus {
	return m.issues
}

// GetProviderType returns the provider type for MockProvider
func (m *MockProvider) GetProviderType() ProviderType {
	return ProviderTypeClone // Mock provider simulates clone behavior by default
}