package github

// GitHubProvider defines the complete interface for GitHub operations
// This allows for different implementations (clone-based, API-only, etc.)
type GitHubProvider interface {
	RepositoryManager
	FileManager
	IssueManager
	AssetManager
	
	// Provider metadata
	GetProviderType() ProviderType
}

// RepositoryManager handles repository setup and metadata operations
type RepositoryManager interface {
	// Repository initialization and setup
	EnsureRepositoryWithPremium(premiumLevel int) error
	NeedsClone() bool
	
	// Repository information
	GetRepoInfo() (owner, repo string, err error)
	GetRepositorySize() (int64, error)
	GetRepositoryMaxSize() float64
	GetRepositoryMaxSizeWithPremium(premiumLevel int) float64
	GetRepositorySizeInfo() (float64, float64, error)
	GetRepositorySizeInfoWithPremium(premiumLevel int) (float64, float64, error)
	IsRepositoryNearCapacity() (bool, float64, error)
	IsRepositoryNearCapacityWithPremium(premiumLevel int) (bool, float64, error)
	
	// Branch and URL operations
	GetDefaultBranch() (string, error)
	GetGitHubFileURL(filename string) (string, error)
	GetGitHubFileURLWithBranch(filename string) (string, error)
}

// FileManager handles all file operations (read, write, commit)
type FileManager interface {
	// Single file operations (prepend mode - main use case)
	CommitFile(filename, content, commitMessage string) error
	CommitFileWithAuthor(filename, content, commitMessage, customAuthor string) error
	CommitFileWithAuthorAndPremium(filename, content, commitMessage, customAuthor string, premiumLevel int) error
	
	// Single file operations (replace mode)
	ReplaceFile(filename, content, commitMessage string) error
	ReplaceFileWithAuthor(filename, content, commitMessage, customAuthor string) error
	ReplaceFileWithAuthorAndPremium(filename, content, commitMessage, customAuthor string, premiumLevel int) error
	
	// Batch operations
	ReplaceMultipleFilesWithAuthorAndPremium(files map[string]string, commitMessage, customAuthor string, premiumLevel int) error
	
	// Binary file operations
	CommitBinaryFile(filename string, data []byte, commitMessage string) error
	
	// File reading
	ReadFile(filename string) (string, error)
}

// IssueManager handles GitHub issue operations
type IssueManager interface {
	// Issue creation and management
	CreateIssue(title, body string) (string, int, error)
	GetIssueStatus(issueNumber int) (*IssueStatus, error)
	SyncIssueStatuses(issueNumbers []int) (map[int]*IssueStatus, error)
	AddIssueComment(issueNumber int, commentText string) (string, error)
	CloseIssue(issueNumber int) error
}

// AssetManager handles binary asset uploads (photos, files)
type AssetManager interface {
	// Asset upload operations
	UploadImageToCDN(filename string, data []byte) (string, error)
}

// GitHubConfig defines the configuration interface needed by providers
type GitHubConfig interface {
	GetGitHubUsername() string
	GetGitHubToken() string
	GetGitHubRepo() string
	GetCommitAuthor() string
}

// ProviderConfig contains all configuration needed to create a provider
type ProviderConfig struct {
	Config       GitHubConfig
	PremiumLevel int
	UserID       string // For identifying user-specific operations
}

// ProviderType defines the implementation type
type ProviderType string

const (
	ProviderTypeClone ProviderType = "clone" // Current implementation
	ProviderTypeAPI   ProviderType = "api"   // Future API-only implementation
	ProviderTypeHybrid ProviderType = "hybrid" // Mixed approach
)

// ProviderFactory creates GitHub providers based on type
type ProviderFactory interface {
	CreateProvider(providerType ProviderType, config *ProviderConfig) (GitHubProvider, error)
}

// Note: IssueStatus is already defined in manager.go, so we don't redefine it here

// FileOperation represents a single file operation for batch processing
type FileOperation struct {
	Filename    string
	Content     string
	Mode        FileMode
	BinaryData  []byte // For binary files
}

// FileMode defines how the file should be handled
type FileMode string

const (
	FileModeAppend  FileMode = "append"  // Prepend content (current behavior)
	FileModeReplace FileMode = "replace" // Replace entire file
	FileModeBinary  FileMode = "binary"  // Binary file upload
)

// CommitOptions provides options for commit operations
type CommitOptions struct {
	Message      string
	Author       string
	PremiumLevel int
	Files        []FileOperation
}