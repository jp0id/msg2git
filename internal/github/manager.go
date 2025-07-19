package github

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitconfig "github.com/msg2git/msg2git/internal/config"
	"github.com/msg2git/msg2git/internal/consts"
	"github.com/msg2git/msg2git/internal/logger"
)

type Manager struct {
	cfg          *gitconfig.Config
	repoPath     string
	repo         *git.Repository
	premiumLevel int // Add premiumLevel to the Manager struct
	userID       string // For file locking support
}

func NewManager(cfg *gitconfig.Config, premiumLevel int) (*Manager, error) {
	// Generate a unique repository path based on the repository URL
	repoPath := generateRepoPath(cfg.GitHubRepo)

	logger.Info("GitHub Manager configured", map[string]interface{}{
		"repo_path":     repoPath,
		"repo_url":      cfg.GitHubRepo,
		"premium_level": premiumLevel,
	})

	m := &Manager{
		cfg:          cfg,
		repoPath:     repoPath,
		repo:         nil,          // Will be initialized lazily
		premiumLevel: premiumLevel, // Initialize premiumLevel
	}

	return m, nil
}

// generateRepoPath creates a unique local path for the repository based on its URL
func generateRepoPath(repoURL string) string {
	// Ensure data directory exists
	dataDir := "./data"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		logger.Warn("Failed to create data directory, using current directory", map[string]interface{}{
			"error": err.Error(),
		})
		dataDir = "."
	}

	if repoURL == "" {
		return filepath.Join(dataDir, "notes-repo-default")
	}

	// Create a hash of the repository URL to ensure uniqueness
	hash := md5.Sum([]byte(repoURL))
	hashStr := hex.EncodeToString(hash[:])

	// Extract repo name for readability
	repoName := "repo"
	if strings.Contains(repoURL, "/") {
		parts := strings.Split(strings.TrimSuffix(repoURL, ".git"), "/")
		if len(parts) > 0 {
			repoName = parts[len(parts)-1]
		}
	}

	return filepath.Join(dataDir, fmt.Sprintf("notes-repo-%s-%s", repoName, hashStr[:8]))
}

// getDirectorySize calculates the total size of a directory
func getDirectorySize(dirPath string) (int64, error) {
	var size int64
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

const maxRepoSize = 1 * 1024 * 1024

// checkRepositorySize checks if repository size is within limits (0.2MB)
func (m *Manager) checkRepositorySize() error {
	return m.checkRepositorySizeWithPremium(m.premiumLevel)
}

// checkRepositorySizeWithPremium checks if repository size is within limits based on premium level
func (m *Manager) checkRepositorySizeWithPremium(premiumLevel int) error {
	if m.repoPath == "" {
		return nil
	}

	// Check if repository directory exists
	if _, err := os.Stat(m.repoPath); os.IsNotExist(err) {
		return nil // Repository doesn't exist yet
	}

	size, err := getDirectorySize(m.repoPath)
	if err != nil {
		logger.Warn("Failed to check repository size", map[string]interface{}{
			"error": err.Error(),
			"path":  m.repoPath,
		})
		return nil // Don't fail on size check errors
	}

	// Get the appropriate size limit based on premium level
	var maxSize float64
	if premiumLevel > 0 {
		maxSize = m.GetRepositoryMaxSizeWithPremium(premiumLevel)
	} else {
		maxSize = m.GetRepositoryMaxSize()
	}
	maxSizeBytes := int64(maxSize * 1024 * 1024)

	if size > maxSizeBytes {
		return fmt.Errorf("repository size (%.1fMB) exceeds maximum allowed size (%.1fMB)", float64(size)/1024/1024, maxSize)
	}

	logger.Debug("Repository size check passed", map[string]interface{}{
		"size_mb": float64(size) / 1024 / 1024,
		"path":    m.repoPath,
	})

	return nil
}

// RepoInfo holds information about a repository directory for garbage collection
type RepoInfo struct {
	Path         string
	Size         int64
	LastAccessed time.Time
}

// cleanupDataDirectory performs garbage collection on the data directory
func cleanupDataDirectory() error {
	dataDir := "./data"

	// Check if data directory exists
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		return nil // No data directory to clean
	}

	// Get total size of data directory
	totalSize, err := getDirectorySize(dataDir)
	if err != nil {
		return fmt.Errorf("failed to calculate data directory size: %w", err)
	}

	const maxDataSize = 1024 * 1024 * 1024 // 1GB
	if totalSize <= maxDataSize {
		logger.Debug("Data directory size within limits", map[string]interface{}{
			"size_mb":  float64(totalSize) / 1024 / 1024,
			"limit_mb": 1024,
		})
		return nil // No cleanup needed
	}

	logger.Info("Data directory exceeds size limit, starting cleanup", map[string]interface{}{
		"size_mb":  float64(totalSize) / 1024 / 1024,
		"limit_mb": 1024,
	})

	// Collect information about all repository directories
	var repos []RepoInfo
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return fmt.Errorf("failed to read data directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "notes-repo-") {
			continue
		}

		repoPath := filepath.Join(dataDir, entry.Name())
		info, err := os.Stat(repoPath)
		if err != nil {
			continue
		}

		size, err := getDirectorySize(repoPath)
		if err != nil {
			logger.Warn("Failed to get repository size", map[string]interface{}{
				"path":  repoPath,
				"error": err.Error(),
			})
			continue
		}

		repos = append(repos, RepoInfo{
			Path:         repoPath,
			Size:         size,
			LastAccessed: info.ModTime(),
		})
	}

	// Sort by last accessed time (oldest first)
	for i := 0; i < len(repos)-1; i++ {
		for j := i + 1; j < len(repos); j++ {
			if repos[i].LastAccessed.After(repos[j].LastAccessed) {
				repos[i], repos[j] = repos[j], repos[i]
			}
		}
	}

	// Remove repositories starting from oldest until we're under the limit
	currentSize := totalSize
	for _, repo := range repos {
		if currentSize <= maxDataSize {
			break
		}

		logger.Info("Removing old repository to free space", map[string]interface{}{
			"path":        repo.Path,
			"size_mb":     float64(repo.Size) / 1024 / 1024,
			"last_access": repo.LastAccessed.Format("2006-01-02 15:04:05"),
		})

		if err := os.RemoveAll(repo.Path); err != nil {
			logger.Error("Failed to remove repository", map[string]interface{}{
				"path":  repo.Path,
				"error": err.Error(),
			})
			continue
		}

		currentSize -= repo.Size
	}

	logger.Info("Data directory cleanup completed", map[string]interface{}{
		"final_size_mb": float64(currentSize) / 1024 / 1024,
		"limit_mb":      1024,
	})

	return nil
}

// CleanupOldRepositories removes old repository directories that don't match the current config
func CleanupOldRepositories(currentRepoURL string) error {
	currentPath := generateRepoPath(currentRepoURL)

	// Find all notes-repo directories
	entries, err := os.ReadDir(".")
	if err != nil {
		return fmt.Errorf("failed to read current directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "notes-repo") && entry.Name() != strings.TrimPrefix(currentPath, "./") {
			logger.Debug("Cleaning up old repository directory", map[string]interface{}{
				"directory": entry.Name(),
			})
			if err := os.RemoveAll(entry.Name()); err != nil {
				logger.Warn("Failed to remove old repository directory", map[string]interface{}{
					"directory": entry.Name(),
					"error":     err.Error(),
				})
				// Continue with other cleanups even if one fails
			}
		}
	}

	return nil
}

func (m *Manager) ensureRepository() error {
	return m.ensureRepositoryWithPremium(m.premiumLevel)
}

// EnsureRepositoryWithPremium ensures repository exists with premium-aware size checking
func (m *Manager) EnsureRepositoryWithPremium(premiumLevel int) error {
	return m.ensureRepositoryWithPremium(premiumLevel)
}

// NeedsClone checks if the repository needs to be cloned (doesn't exist locally)
func (m *Manager) NeedsClone() bool {
	// Check if repository is already initialized in memory
	if m.repo != nil {
		return false
	}

	// Check if repository directory exists on disk
	if _, err := os.Stat(m.repoPath); os.IsNotExist(err) {
		return true // Directory doesn't exist, needs cloning
	}

	return false // Directory exists, just needs to be opened
}

func (m *Manager) ensureRepositoryWithPremium(premiumLevel int) error {
	// If repository is already initialized, no need to do anything
	if m.repo != nil {
		return nil
	}

	// Run garbage collection before doing any repository operations
	if err := cleanupDataDirectory(); err != nil {
		logger.Warn("Failed to cleanup data directory", map[string]interface{}{
			"error": err.Error(),
		})
		// Don't fail the operation if cleanup fails
	}

	if _, err := os.Stat(m.repoPath); os.IsNotExist(err) {
		logger.Info("Repository directory doesn't exist, cloning", map[string]interface{}{
			"repo_path": m.repoPath,
		})
		if err := m.cloneRepositoryWithPremium(premiumLevel); err != nil {
			return fmt.Errorf("failed to clone repository: %w", err)
		}
	} else {
		logger.Debug("Repository directory exists, opening", map[string]interface{}{
			"repo_path": m.repoPath,
		})
		repo, err := git.PlainOpen(m.repoPath)
		if err != nil {
			return fmt.Errorf("failed to open repository: %w", err)
		}
		m.repo = repo
	}

	// Check repository size limit with premium awareness
	if err := m.checkRepositorySizeWithPremium(premiumLevel); err != nil {
		return err
	}

	return nil
}

// ensureRepositoryReadOnly initializes repository for read-only operations without size checks
func (m *Manager) ensureRepositoryReadOnly() error {
	// If repository is already initialized, no need to do anything
	if m.repo != nil {
		return nil
	}

	// Run garbage collection before doing any repository operations
	if err := cleanupDataDirectory(); err != nil {
		logger.Warn("Failed to cleanup data directory", map[string]interface{}{
			"error": err.Error(),
		})
		// Don't fail the operation if cleanup fails
	}

	if _, err := os.Stat(m.repoPath); os.IsNotExist(err) {
		logger.Info("Repository directory doesn't exist, cloning for read-only access", map[string]interface{}{
			"repo_path": m.repoPath,
		})
		if err := m.cloneRepositoryWithPremium(0); err != nil { // Use basic clone without premium features
			return fmt.Errorf("failed to clone repository: %w", err)
		}
	} else {
		logger.Debug("Repository directory exists, opening for read-only access", map[string]interface{}{
			"repo_path": m.repoPath,
		})
		repo, err := git.PlainOpen(m.repoPath)
		if err != nil {
			return fmt.Errorf("failed to open repository: %w", err)
		}
		m.repo = repo
	}

	// Skip size check for read-only operations
	return nil
}

// GitHubRepo represents the repository information from GitHub API
type GitHubRepo struct {
	Size int `json:"size"` // Size in KB
}

func (m *Manager) cloneRepository() error {
	return m.cloneRepositoryWithPremium(m.premiumLevel)
}

func (m *Manager) cloneRepositoryWithPremium(premiumLevel int) error {
	// Step 1: Check repository size via GitHub API before cloning to prevent network waste
	remoteSizeBytes, err := m.getRemoteRepositorySize()
	if err != nil {
		return fmt.Errorf("failed to get remote repository size: %w", err)
	}

	// Get size limits for comparison
	var maxSizeMB float64
	if premiumLevel > 0 {
		maxSizeMB = m.GetRepositoryMaxSizeWithPremium(premiumLevel)
	} else {
		maxSizeMB = m.GetRepositoryMaxSize()
	}
	maxSizeBytes := int64(maxSizeMB * 1024 * 1024)

	// Pre-clone size check
	if remoteSizeBytes > maxSizeBytes {
		remoteSizeMB := float64(remoteSizeBytes) / 1024 / 1024
		return fmt.Errorf("remote repository size (%.1fMB) exceeds your tier limit (%.1fMB). Upgrade with /coffee to access larger repositories", remoteSizeMB, maxSizeMB)
	}

	logger.Info("Pre-clone size check passed, proceeding with clone", map[string]interface{}{
		"remote_size_mb": float64(remoteSizeBytes) / 1024 / 1024,
		"max_size_mb":    maxSizeMB,
		"premium_level":  premiumLevel,
	})

	// Step 2: Clone the repository
	auth := &githttp.BasicAuth{
		Username: m.cfg.GitHubUsername,
		Password: m.cfg.GitHubToken,
	}

	repo, err := git.PlainClone(m.repoPath, false, &git.CloneOptions{
		URL:  m.cfg.GitHubRepo,
		Auth: auth,
	})
	if err != nil {
		if strings.Contains(err.Error(), "remote repository is empty") {
			return m.initRepository()
		}
		return fmt.Errorf("failed to clone repository: %w", err)
	}

	m.repo = repo

	// Step 3: Double confirmation - check actual cloned size
	actualSize, err := getDirectorySize(m.repoPath)
	if err != nil {
		logger.Warn("Failed to check actual cloned size, proceeding anyway", map[string]interface{}{
			"error": err.Error(),
			"path":  m.repoPath,
		})
		return nil // Don't fail the clone for size check errors
	}

	actualSizeMB := float64(actualSize) / 1024 / 1024
	remoteSizeMB := float64(remoteSizeBytes) / 1024 / 1024

	logger.Info("Post-clone size verification", map[string]interface{}{
		"remote_size_mb": remoteSizeMB,
		"actual_size_mb": actualSizeMB,
		"max_size_mb":    maxSizeMB,
		"size_diff_mb":   actualSizeMB - remoteSizeMB,
		"premium_level":  premiumLevel,
	})

	// Double confirmation: check if actual cloned size exceeds limit
	if actualSize > maxSizeBytes {
		// Log warning but keep the repository for /insight inspection
		logger.Warn("Actual cloned repository exceeds size limit, but keeping for inspection", map[string]interface{}{
			"actual_size_mb": actualSizeMB,
			"max_size_mb":    maxSizeMB,
			"path":           m.repoPath,
			"note":           "Repository kept for /insight command",
		})

		return fmt.Errorf("actual repository size after cloning (%.1fMB) exceeds your tier limit (%.1fMB). Use /insight to check. Upgrade with /coffee for larger repositories", actualSizeMB, maxSizeMB)
	}

	// Both checks passed
	if actualSizeMB != remoteSizeMB {
		logger.Info("Repository size verification completed with size difference", map[string]interface{}{
			"remote_api_size": remoteSizeMB,
			"actual_git_size": actualSizeMB,
			"difference_mb":   actualSizeMB - remoteSizeMB,
			"status":          "within_limits",
		})
	} else {
		logger.Info("Repository size verification completed - sizes match exactly", map[string]interface{}{
			"size_mb": actualSizeMB,
			"status":  "exact_match",
		})
	}

	return nil
}

func (m *Manager) initRepository() error {
	repo, err := git.PlainInit(m.repoPath, false)
	if err != nil {
		return fmt.Errorf("failed to initialize repository: %w", err)
	}

	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{m.cfg.GitHubRepo},
	})
	if err != nil {
		return fmt.Errorf("failed to add remote: %w", err)
	}

	m.repo = repo
	return nil
}

func (m *Manager) CommitFile(filename, content, commitMessage string) error {
	// Ensure repository is initialized (lazy initialization)
	if err := m.ensureRepository(); err != nil {
		return fmt.Errorf("failed to ensure repository: %w", err)
	}

	// Pull latest changes before committing to avoid conflicts
	logger.Debug("Pulling latest changes before committing file", map[string]interface{}{
		"filename": filename,
	})
	if err := m.pullLatest(); err != nil {
		// If it's an auth error, return it immediately with helpful message
		if strings.Contains(err.Error(), "GitHub authorization failed") {
			return err
		}
		// For other errors, only fail if it's not an empty repo
		if !strings.Contains(err.Error(), "remote repository is empty") {
			return fmt.Errorf("failed to pull latest changes: %w", err)
		}
	}

	filePath := filepath.Join(m.repoPath, filename)

	if err := m.prependToFile(filePath, content); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	if err := m.commitAndPush(filename, commitMessage); err != nil {
		return fmt.Errorf("failed to commit and push: %w", err)
	}

	return nil
}

func (m *Manager) CommitFileWithAuthor(filename, content, commitMessage, customAuthor string) error {
	return m.CommitFileWithAuthorAndPremium(filename, content, commitMessage, customAuthor, m.premiumLevel)
}

func (m *Manager) CommitFileWithAuthorAndPremium(filename, content, commitMessage, customAuthor string, premiumLevel int) error {
	// Get user ID for file locking
	userID := m.getUserIDForLocking()
	
	// Get repository URL for locking
	repoURL := m.cfg.GitHubRepo
	
	// Use file lock manager to prevent concurrent modifications
	flm := GetFileLockManager()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	
	return flm.WithFileLock(ctx, userID, repoURL, filename, true, func() error {
		return m.commitFileWithAuthorAndPremiumLocked(filename, content, commitMessage, customAuthor, premiumLevel)
	})
}

// commitFileWithAuthorAndPremiumLocked performs the actual file commit with the assumption that the file is locked
func (m *Manager) commitFileWithAuthorAndPremiumLocked(filename, content, commitMessage, customAuthor string, premiumLevel int) error {
	logger.Debug("Starting locked file commit", map[string]interface{}{
		"filename": filename,
		"author":   customAuthor,
	})

	// Ensure repository is initialized (lazy initialization)
	if err := m.ensureRepositoryWithPremium(premiumLevel); err != nil {
		return fmt.Errorf("failed to ensure repository: %w", err)
	}

	// Pull latest changes before committing to avoid conflicts
	logger.Debug("Pulling latest changes before committing file with custom author", map[string]interface{}{
		"filename": filename,
		"author":   customAuthor,
	})
	if err := m.pullLatest(); err != nil {
		// If it's an auth error, return it immediately with helpful message
		if strings.Contains(err.Error(), "GitHub authorization failed") {
			return err
		}
		// For other errors, only fail if it's not an empty repo
		if !strings.Contains(err.Error(), "remote repository is empty") {
			return fmt.Errorf("failed to pull latest changes: %w", err)
		}
	}

	filePath := filepath.Join(m.repoPath, filename)

	if err := m.prependToFile(filePath, content); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	if err := m.commitAndPushWithAuthor(filename, commitMessage, customAuthor); err != nil {
		return fmt.Errorf("failed to commit and push: %w", err)
	}

	logger.Info("File committed with file lock", map[string]interface{}{
		"filename": filename,
		"author":   customAuthor,
	})

	return nil
}

// getUserIDForLocking extracts user ID for file locking
func (m *Manager) getUserIDForLocking() int64 {
	if m.userID != "" {
		// Handle "user_123456" format by extracting the numeric part
		userIDStr := m.userID
		if strings.HasPrefix(userIDStr, "user_") {
			userIDStr = strings.TrimPrefix(userIDStr, "user_")
		}
		
		if userID, err := strconv.ParseInt(userIDStr, 10, 64); err == nil {
			return userID
		}
		
		// If parsing fails, use a hash of the original string
		hash := int64(0)
		for _, char := range m.userID {
			hash = hash*31 + int64(char)
		}
		if hash < 0 {
			hash = -hash
		}
		return hash
	}
	
	// Fallback: use a hash of the repository URL as a pseudo user ID
	// This ensures different repositories have different locks even without explicit user ID
	if m.cfg != nil && m.cfg.GitHubRepo != "" {
		hash := int64(0)
		for _, char := range m.cfg.GitHubRepo {
			hash = hash*31 + int64(char)
		}
		if hash < 0 {
			hash = -hash
		}
		return hash
	}
	
	return 0 // Default fallback
}

func (m *Manager) prependToFile(filePath, content string) error {
	// Ensure parent directories exist
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create parent directories: %w", err)
	}

	// Read existing content if file exists
	var existingContent []byte
	if _, err := os.Stat(filePath); err == nil {
		existingContent, err = ioutil.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read existing file: %w", err)
		}
	}

	// Create new content with prepended content
	newContent := content + string(existingContent)

	// Write the combined content
	if err := ioutil.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func (m *Manager) commitAndPushWithAuthor(filename, commitMessage, customAuthor string) error {
	worktree, err := m.repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	if _, err := worktree.Add(filename); err != nil {
		return fmt.Errorf("failed to add file: %w", err)
	}

	// Use custom author if provided, otherwise use default
	authorString := customAuthor
	if authorString == "" {
		authorString = m.cfg.CommitAuthor
	}

	authorParts := strings.Split(authorString, " <")
	if len(authorParts) != 2 {
		return fmt.Errorf("invalid commit author format, expected 'Name <email>'")
	}

	name := authorParts[0]
	email := strings.TrimSuffix(authorParts[1], ">")

	commit, err := worktree.Commit(commitMessage, &git.CommitOptions{
		Author: &object.Signature{
			Name:  name,
			Email: email,
			When:  time.Now(),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	obj, err := m.repo.CommitObject(commit)
	if err != nil {
		return fmt.Errorf("failed to get commit object: %w", err)
	}

	logger.Info("Commit created with custom author", map[string]interface{}{
		"hash":   obj.Hash.String(),
		"author": authorString,
	})

	auth := &githttp.BasicAuth{
		Username: m.cfg.GitHubUsername,
		Password: m.cfg.GitHubToken,
	}

	if err := m.repo.Push(&git.PushOptions{
		Auth: auth,
	}); err != nil {
		return fmt.Errorf("failed to push: %w", err)
	}

	logger.Info("Changes pushed to repository", map[string]interface{}{
		"filename": filename,
		"author":   authorString,
	})

	return nil
}

func (m *Manager) commitAndPush(filename, commitMessage string) error {
	worktree, err := m.repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	if _, err := worktree.Add(filename); err != nil {
		return fmt.Errorf("failed to add file: %w", err)
	}

	authorParts := strings.Split(m.cfg.CommitAuthor, " <")
	if len(authorParts) != 2 {
		return fmt.Errorf("invalid commit author format, expected 'Name <email>'")
	}

	name := authorParts[0]
	email := strings.TrimSuffix(authorParts[1], ">")

	commit, err := worktree.Commit(commitMessage, &git.CommitOptions{
		Author: &object.Signature{
			Name:  name,
			Email: email,
			When:  time.Now(),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	obj, err := m.repo.CommitObject(commit)
	if err != nil {
		return fmt.Errorf("failed to get commit object: %w", err)
	}

	logger.Info("Commit created", map[string]interface{}{
		"hash": obj.Hash.String(),
	})

	auth := &githttp.BasicAuth{
		Username: m.cfg.GitHubUsername,
		Password: m.cfg.GitHubToken,
	}

	if err := m.repo.Push(&git.PushOptions{
		Auth: auth,
	}); err != nil {
		return fmt.Errorf("failed to push: %w", err)
	}

	return nil
}

// pullLatest fetches and resets to remote HEAD to ensure local repo is in sync
func (m *Manager) pullLatest() error {
	auth := &githttp.BasicAuth{
		Username: m.cfg.GitHubUsername,
		Password: m.cfg.GitHubToken,
	}

	// First, fetch the latest changes
	err := m.repo.Fetch(&git.FetchOptions{
		RemoteName: "origin",
		Auth:       auth,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		// Handle various scenarios where fetch might fail but we can continue
		if strings.Contains(err.Error(), "couldn't find remote ref") ||
			strings.Contains(err.Error(), "reference not found") ||
			strings.Contains(err.Error(), "remote repository is empty") {
			return nil
		}

		// For authorization failures, provide a more helpful error message
		if strings.Contains(err.Error(), "authorization failed") ||
			strings.Contains(err.Error(), "authentication failed") ||
			strings.Contains(err.Error(), "401") ||
			strings.Contains(err.Error(), "403") {
			return fmt.Errorf(consts.GitHubAuthFailed)
		}

		return fmt.Errorf("failed to fetch: %w", err)
	}

	// If fetch was successful, try to rebase
	if err == git.NoErrAlreadyUpToDate {
		return nil // Already up to date, no need to rebase
	}

	worktree, err := m.repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	// Get the current branch
	head, err := m.repo.Head()
	if err != nil {
		return fmt.Errorf("failed to get HEAD: %w", err)
	}

	// Get the remote tracking branch (e.g., origin/main or origin/master)
	remoteBranchName := "origin/" + head.Name().Short()
	remoteRef, err := m.repo.Reference(plumbing.NewRemoteReferenceName("origin", head.Name().Short()), true)
	if err != nil {
		// Try with master if main doesn't exist
		if head.Name().Short() == "main" {
			remoteRef, err = m.repo.Reference(plumbing.NewRemoteReferenceName("origin", "master"), true)
			if err != nil {
				return fmt.Errorf("failed to find remote branch: %w", err)
			}
		} else {
			return fmt.Errorf("failed to find remote branch %s: %w", remoteBranchName, err)
		}
	}

	// Simple approach: if there are remote changes, reset to remote HEAD to avoid conflicts
	// This is safer for automation since we always want remote to win
	currentCommit, err := m.repo.CommitObject(head.Hash())
	if err != nil {
		return fmt.Errorf("failed to get current commit: %w", err)
	}

	remoteCommit, err := m.repo.CommitObject(remoteRef.Hash())
	if err != nil {
		return fmt.Errorf("failed to get remote commit: %w", err)
	}

	// If remote is ahead, reset to remote HEAD
	if currentCommit.Hash != remoteCommit.Hash {
		logger.Info("Remote has changes, resetting local to remote HEAD", map[string]interface{}{
			"local_commit":  currentCommit.Hash.String()[:8],
			"remote_commit": remoteCommit.Hash.String()[:8],
		})

		err = worktree.Reset(&git.ResetOptions{
			Commit: remoteRef.Hash(),
			Mode:   git.HardReset,
		})
		if err != nil {
			return fmt.Errorf("failed to reset to remote HEAD: %w", err)
		}

		logger.Info("Successfully synchronized with remote repository", map[string]interface{}{
			"reset_to": remoteCommit.Hash.String()[:8],
		})
	} else {
		logger.Debug("Local repository is already up to date with remote", nil)
	}

	return nil
}

type IssueRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type IssueResponse struct {
	HTMLURL string `json:"html_url"`
	Number  int    `json:"number"`
}

func (m *Manager) CreateIssue(title, body string) (string, int, error) {
	// Extract owner and repo from GitHub repo URL
	owner, repo, err := m.parseRepoURL()
	if err != nil {
		return "", 0, fmt.Errorf("failed to parse repository URL: %w", err)
	}

	logger.Info("Creating issue", map[string]interface{}{
		"repo":  fmt.Sprintf("%s/%s", owner, repo),
		"title": title,
	})

	// Validate GitHub token format
	if m.cfg.GitHubToken == "" {
		return "", 0, fmt.Errorf("GitHub token is empty")
	}

	// GitHub personal access tokens should start with 'ghp_' for classic tokens or 'github_pat_' for fine-grained
	if !strings.HasPrefix(m.cfg.GitHubToken, "ghp_") && !strings.HasPrefix(m.cfg.GitHubToken, "github_pat_") {
		logger.WarnMsg("Warning: GitHub token format may be incorrect (should start with 'ghp_' or 'github_pat_')")
	}

	// Create issue request
	issueReq := IssueRequest{
		Title: title,
		Body:  body,
	}

	jsonData, err := json.Marshal(issueReq)
	if err != nil {
		return "", 0, fmt.Errorf("failed to marshal issue request: %w", err)
	}

	// Create HTTP request to GitHub API
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues", owner, repo)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+m.cfg.GitHubToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		logger.Error("GitHub API error", map[string]interface{}{
			"status":   resp.StatusCode,
			"response": string(bodyBytes),
		})

		// Common error scenarios
		if resp.StatusCode == 401 {
			return "", 0, fmt.Errorf("unauthorized - check GitHub token permissions")
		}
		if resp.StatusCode == 403 {
			return "", 0, fmt.Errorf("forbidden - token may not have issues permissions or rate limit exceeded")
		}
		if resp.StatusCode == 404 {
			return "", 0, fmt.Errorf("repository not found - check repository URL and token access")
		}

		return "", 0, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse response
	var issueResp IssueResponse
	if err := json.NewDecoder(resp.Body).Decode(&issueResp); err != nil {
		return "", 0, fmt.Errorf("failed to decode response: %w", err)
	}

	return issueResp.HTMLURL, issueResp.Number, nil
}

func (m *Manager) parseRepoURL() (owner, repo string, err error) {
	// Support both HTTPS and SSH URLs
	// HTTPS: https://github.com/owner/repo.git
	// SSH: git@github.com:owner/repo.git

	repoURL := m.cfg.GitHubRepo

	// Remove .git suffix if present
	repoURL = strings.TrimSuffix(repoURL, ".git")

	// Handle HTTPS URLs
	if strings.HasPrefix(repoURL, "https://github.com/") {
		path := strings.TrimPrefix(repoURL, "https://github.com/")
		parts := strings.Split(path, "/")
		if len(parts) != 2 {
			return "", "", fmt.Errorf("invalid GitHub HTTPS URL format")
		}
		return parts[0], parts[1], nil
	}

	// Handle SSH URLs
	if strings.HasPrefix(repoURL, "git@github.com:") {
		path := strings.TrimPrefix(repoURL, "git@github.com:")
		parts := strings.Split(path, "/")
		if len(parts) != 2 {
			return "", "", fmt.Errorf("invalid GitHub SSH URL format")
		}
		return parts[0], parts[1], nil
	}

	// Try to extract from any GitHub URL using regex
	re := regexp.MustCompile(`github\.com[:/]([^/]+)/([^/]+)`)
	matches := re.FindStringSubmatch(repoURL)
	if len(matches) != 3 {
		return "", "", fmt.Errorf("unsupported repository URL format: %s", repoURL)
	}

	return matches[1], matches[2], nil
}

func (m *Manager) GetRepoInfo() (owner, repo string, err error) {
	return m.parseRepoURL()
}

type IssueStatus struct {
	Number      int                    `json:"number"`
	Title       string                 `json:"title"`
	State       string                 `json:"state"` // "open" or "closed"
	HTMLURL     string                 `json:"html_url"`
	PullRequest map[string]interface{} `json:"pull_request,omitempty"` // Present if this is a PR
}

func (m *Manager) GetIssueStatus(issueNumber int) (*IssueStatus, error) {
	owner, repo, err := m.parseRepoURL()
	if err != nil {
		return nil, fmt.Errorf("failed to parse repository URL: %w", err)
	}

	// Create HTTP request to GitHub API
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d", owner, repo, issueNumber)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+m.cfg.GitHubToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == 404 {
			return nil, fmt.Errorf("issue #%d not found", issueNumber)
		}
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse response
	var issue IssueStatus
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	logger.Debug("Individual API call for issue", map[string]interface{}{
		"issue_number": issue.Number,
		"title":        issue.Title,
		"state":        issue.State,
	})
	return &issue, nil
}

func (m *Manager) SyncIssueStatuses(issueNumbers []int) (map[int]*IssueStatus, error) {
	if len(issueNumbers) == 0 {
		return make(map[int]*IssueStatus), nil
	}

	logger.Debug("Syncing issues using efficient batch method", map[string]interface{}{
		"issue_count": len(issueNumbers),
	})

	// Use efficient batch method only - no fallback to wasteful individual calls
	statuses, err := m.getBatchIssueStatuses(issueNumbers)
	if err != nil {
		logger.Debug("Batch method failed", map[string]interface{}{
			"error": err.Error(),
		})
		// Return the error directly - no inefficient fallback
		return nil, err
	}

	return statuses, nil
}

// AddIssueComment adds a comment to an existing GitHub issue and returns the comment URL
func (m *Manager) AddIssueComment(issueNumber int, commentText string) (string, error) {
	logger.Debug("Adding comment to GitHub issue", map[string]interface{}{
		"issue_number":   issueNumber,
		"comment_length": len(commentText),
	})

	owner, repo, err := m.parseRepoURL()
	if err != nil {
		return "", fmt.Errorf("failed to parse repository URL: %w", err)
	}

	// Create the comment request body
	commentBody := map[string]interface{}{
		"body": commentText,
	}

	commentBodyJSON, err := json.Marshal(commentBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal comment body: %w", err)
	}

	// Make API request to add comment
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d/comments", owner, repo, issueNumber)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(commentBodyJSON))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "token "+m.cfg.GitHubToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "msg2git-telegram-bot")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API error: %s (status: %d)", string(body), resp.StatusCode)
	}

	// Parse the response to get the comment URL
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	var commentResponse struct {
		HTMLURL string `json:"html_url"`
		ID      int64  `json:"id"`
	}

	if err := json.Unmarshal(body, &commentResponse); err != nil {
		return "", fmt.Errorf("failed to parse comment response: %w", err)
	}

	logger.Info("Successfully added comment to GitHub issue", map[string]interface{}{
		"issue_number": issueNumber,
		"repo":         fmt.Sprintf("%s/%s", owner, repo),
		"comment_url":  commentResponse.HTMLURL,
		"comment_id":   commentResponse.ID,
	})

	return commentResponse.HTMLURL, nil
}

// CloseIssue closes an existing GitHub issue
func (m *Manager) CloseIssue(issueNumber int) error {
	logger.Debug("Closing GitHub issue", map[string]interface{}{
		"issue_number": issueNumber,
	})

	owner, repo, err := m.parseRepoURL()
	if err != nil {
		return fmt.Errorf("failed to parse repository URL: %w", err)
	}

	// Create the close request body
	closeBody := map[string]interface{}{
		"state": "closed",
	}

	closeBodyJSON, err := json.Marshal(closeBody)
	if err != nil {
		return fmt.Errorf("failed to marshal close body: %w", err)
	}

	// Make API request to close issue
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d", owner, repo, issueNumber)
	req, err := http.NewRequest("PATCH", url, bytes.NewBuffer(closeBodyJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "token "+m.cfg.GitHubToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "msg2git-telegram-bot")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GitHub API error: %s (status: %d)", string(body), resp.StatusCode)
	}

	logger.Info("Successfully closed GitHub issue", map[string]interface{}{
		"issue_number": issueNumber,
		"repo":         fmt.Sprintf("%s/%s", owner, repo),
	})

	return nil
}

// getBatchIssueStatuses fetches multiple issues efficiently using the list issues API
func (m *Manager) getBatchIssueStatuses(issueNumbers []int) (map[int]*IssueStatus, error) {
	if len(issueNumbers) == 0 {
		return make(map[int]*IssueStatus), nil
	}

	owner, repo, err := m.parseRepoURL()
	if err != nil {
		return nil, fmt.Errorf("failed to parse repository URL: %w", err)
	}

	logger.Debug("Attempting efficient batch issue fetch", map[string]interface{}{
		"issue_count": len(issueNumbers),
		"method":      "graphql_batch",
	})

	// Try to use GraphQL for efficient batch fetching
	statuses, err := m.fetchIssuesViaGraphQL(owner, repo, issueNumbers)
	if err != nil {
		logger.Debug("GraphQL batch fetch failed", map[string]interface{}{
			"error": err.Error(),
		})
		// Return a user-friendly error instead of fallback to inefficient individual calls
		return nil, fmt.Errorf("unable to fetch issue statuses efficiently. This may be due to API limitations or network issues. Please try again later")
	}

	logger.Debug("Batch fetch completed successfully", map[string]interface{}{
		"found_count":     len(statuses),
		"requested_count": len(issueNumbers),
	})

	return statuses, nil
}

// fetchIssuesPage fetches a single page of issues from GitHub API
func (m *Manager) fetchIssuesPage(url string) ([]IssueStatus, string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+m.cfg.GitHubToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var issues []IssueStatus
	if err := json.NewDecoder(resp.Body).Decode(&issues); err != nil {
		return nil, "", fmt.Errorf("failed to decode response: %w", err)
	}

	// Parse Link header for pagination
	nextURL := ""
	linkHeader := resp.Header.Get("Link")
	if linkHeader != "" {
		// Simple parsing of Link header to find "next" URL
		// Format: <https://api.github.com/repos/owner/repo/issues?page=2>; rel="next"
		if strings.Contains(linkHeader, `rel="next"`) {
			parts := strings.Split(linkHeader, ",")
			for _, part := range parts {
				if strings.Contains(part, `rel="next"`) {
					if start := strings.Index(part, "<"); start != -1 {
						if end := strings.Index(part, ">"); end != -1 {
							nextURL = part[start+1 : end]
							break
						}
					}
				}
			}
		}
	}

	return issues, nextURL, nil
}

// fetchIssuesViaGraphQL fetches specific issues using GitHub's GraphQL API
func (m *Manager) fetchIssuesViaGraphQL(owner, repo string, issueNumbers []int) (map[int]*IssueStatus, error) {
	// Build GraphQL query to fetch specific issues by number
	// GraphQL allows us to fetch multiple specific issues in a single request
	var queryParts []string
	for i, num := range issueNumbers {
		queryParts = append(queryParts, fmt.Sprintf(`
		  issue%d: issue(number: %d) {
		    number
		    title
		    state
		    url
		  }`, i, num))
	}

	query := fmt.Sprintf(`{
	  repository(owner: "%s", name: "%s") {
	    %s
	  }
	}`, owner, repo, strings.Join(queryParts, ""))

	logger.Debug("GraphQL query details", map[string]interface{}{
		"owner":         owner,
		"repo":          repo,
		"issue_count":   len(issueNumbers),
		"issue_numbers": issueNumbers,
		"query":         query,
	})

	// Prepare GraphQL request
	requestBody := map[string]interface{}{
		"query": query,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GraphQL query: %w", err)
	}

	logger.Debug("GraphQL request body", map[string]interface{}{
		"json_data": string(jsonData),
	})

	// Send GraphQL request
	req, err := http.NewRequest("POST", "https://api.github.com/graphql", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create GraphQL request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+m.cfg.GitHubToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GraphQL request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("GraphQL request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse GraphQL response
	var response struct {
		Data struct {
			Repository map[string]interface{} `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read GraphQL response: %w", err)
	}

	logger.Debug("GraphQL raw response", map[string]interface{}{
		"response": string(body),
	})

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse GraphQL response: %w", err)
	}

	if len(response.Errors) > 0 {
		logger.Error("GraphQL returned errors", map[string]interface{}{
			"errors": response.Errors,
		})
		return nil, fmt.Errorf("GraphQL errors: %v", response.Errors)
	}

	// Convert GraphQL response to our IssueStatus format
	statuses := make(map[int]*IssueStatus)

	logger.Debug("GraphQL response data", map[string]interface{}{
		"repository_data": response.Data.Repository,
		"data_keys":       getKeys(response.Data.Repository),
	})

	// Check if repository data is empty
	if response.Data.Repository == nil || len(response.Data.Repository) == 0 {
		logger.Warn("GraphQL returned empty repository data", map[string]interface{}{
			"owner": owner,
			"repo":  repo,
		})
		return nil, fmt.Errorf("GraphQL returned no repository data - check repository name and token permissions")
	}

	// Parse each issue from the repository response
	for key, value := range response.Data.Repository {
		logger.Debug("Processing GraphQL response key", map[string]interface{}{
			"key":   key,
			"value": value,
			"type":  fmt.Sprintf("%T", value),
		})

		if strings.HasPrefix(key, "issue") && value != nil {
			// Convert interface{} to map
			if issueMap, ok := value.(map[string]interface{}); ok {
				logger.Debug("Found issue data", map[string]interface{}{
					"key":       key,
					"issue_map": issueMap,
				})

				// Extract fields safely
				var number int
				var title, state, url string

				if numVal, ok := issueMap["number"].(float64); ok {
					number = int(numVal)
				}
				if titleVal, ok := issueMap["title"].(string); ok {
					title = titleVal
				}
				if stateVal, ok := issueMap["state"].(string); ok {
					state = stateVal
				}
				if urlVal, ok := issueMap["url"].(string); ok {
					url = urlVal
				}

				if number > 0 {
					statuses[number] = &IssueStatus{
						Number:  number,
						Title:   title,
						State:   state,
						HTMLURL: url,
					}

					logger.Debug("Parsed GraphQL issue", map[string]interface{}{
						"number": number,
						"title":  title,
						"state":  state,
						"url":    url,
					})
				} else {
					logger.Warn("Issue has invalid number", map[string]interface{}{
						"key":        key,
						"number":     number,
						"issue_data": issueMap,
					})
				}
			} else {
				logger.Warn("Issue value is not a map", map[string]interface{}{
					"key":   key,
					"value": value,
					"type":  fmt.Sprintf("%T", value),
				})
			}
		}
	}

	logger.Debug("GraphQL batch fetch completed", map[string]interface{}{
		"found_count":     len(statuses),
		"requested_count": len(issueNumbers),
	})

	return statuses, nil
}

// getKeys is a helper function to extract keys from a map for debugging
func getKeys(m map[string]interface{}) []string {
	if m == nil {
		return []string{}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func (m *Manager) ReadFile(filename string) (string, error) {
	// Ensure repository is initialized for read-only access (no size check)
	if err := m.ensureRepositoryReadOnly(); err != nil {
		return "", fmt.Errorf("failed to ensure repository: %w", err)
	}

	// Pull latest changes before reading to ensure we have the most recent version
	logger.Debug("Pulling latest changes before reading file", map[string]interface{}{
		"filename": filename,
	})
	if err := m.pullLatest(); err != nil {
		// Log warning but don't fail the read operation for pull errors
		logger.Warn("Failed to pull latest changes before reading file", map[string]interface{}{
			"error":    err.Error(),
			"filename": filename,
		})
	}

	filePath := filepath.Join(m.repoPath, filename)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return "", fmt.Errorf("file %s does not exist", filename)
	}

	// Read file contents
	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", filename, err)
	}

	return string(content), nil
}

func (m *Manager) ReplaceFile(filename, content, commitMessage string) error {
	// Ensure repository is initialized (lazy initialization)
	if err := m.ensureRepositoryWithPremium(m.premiumLevel); err != nil {
		return fmt.Errorf("failed to ensure repository: %w", err)
	}

	// Pull latest changes before replacing file to avoid conflicts
	logger.Debug("Pulling latest changes before replacing file", map[string]interface{}{
		"filename": filename,
	})
	if err := m.pullLatest(); err != nil {
		if !strings.Contains(err.Error(), "remote repository is empty") {
			return fmt.Errorf("failed to pull latest changes: %w", err)
		}
	}

	filePath := filepath.Join(m.repoPath, filename)

	// Ensure parent directories exist
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create parent directories: %w", err)
	}

	// Write the new content (completely replace the file)
	if err := ioutil.WriteFile(filePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	if err := m.commitAndPush(filename, commitMessage); err != nil {
		return fmt.Errorf("failed to commit and push: %w", err)
	}

	return nil
}

func (m *Manager) ReplaceFileWithAuthor(filename, content, commitMessage, customAuthor string) error {
	return m.ReplaceFileWithAuthorAndPremium(filename, content, commitMessage, customAuthor, m.premiumLevel)
}

func (m *Manager) ReplaceFileWithAuthorAndPremium(filename, content, commitMessage, customAuthor string, premiumLevel int) error {
	// Get user ID for file locking
	userID := m.getUserIDForLocking()
	
	// Get repository URL for locking
	repoURL := m.cfg.GitHubRepo
	
	// Use file lock manager to prevent concurrent modifications
	flm := GetFileLockManager()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	
	return flm.WithFileLock(ctx, userID, repoURL, filename, true, func() error {
		return m.replaceFileWithAuthorAndPremiumLocked(filename, content, commitMessage, customAuthor, premiumLevel)
	})
}

// replaceFileWithAuthorAndPremiumLocked performs the actual file replacement with the assumption that the file is locked
func (m *Manager) replaceFileWithAuthorAndPremiumLocked(filename, content, commitMessage, customAuthor string, premiumLevel int) error {
	logger.Debug("Starting locked file replacement", map[string]interface{}{
		"filename": filename,
		"author":   customAuthor,
	})

	// Ensure repository is initialized (lazy initialization)
	if err := m.ensureRepositoryWithPremium(premiumLevel); err != nil {
		return fmt.Errorf("failed to ensure repository: %w", err)
	}

	// Pull latest changes before replacing file to avoid conflicts
	logger.Debug("Pulling latest changes before replacing file with custom author", map[string]interface{}{
		"filename": filename,
		"author":   customAuthor,
	})
	if err := m.pullLatest(); err != nil {
		if !strings.Contains(err.Error(), "remote repository is empty") {
			return fmt.Errorf("failed to pull latest changes: %w", err)
		}
	}

	filePath := filepath.Join(m.repoPath, filename)

	// Ensure parent directories exist
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create parent directories: %w", err)
	}

	// Write the new content (completely replace the file)
	if err := ioutil.WriteFile(filePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	if err := m.commitAndPushWithAuthor(filename, commitMessage, customAuthor); err != nil {
		return fmt.Errorf("failed to commit and push: %w", err)
	}

	logger.Info("File replaced with file lock", map[string]interface{}{
		"filename": filename,
		"author":   customAuthor,
	})

	return nil
}

// ReplaceMultipleFilesWithAuthorAndPremium replaces multiple files in a single commit
func (m *Manager) ReplaceMultipleFilesWithAuthorAndPremium(files map[string]string, commitMessage, customAuthor string, premiumLevel int) error {
	// Get user ID for file locking
	userID := m.getUserIDForLocking()
	
	// Get repository URL for locking
	repoURL := m.cfg.GitHubRepo
	
	// Use file lock manager to prevent concurrent modifications
	flm := GetFileLockManager()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second) // Longer timeout for multiple files
	defer cancel()
	
	return m.replaceMultipleFilesWithLocks(ctx, flm, userID, repoURL, files, commitMessage, customAuthor, premiumLevel)
}

// replaceMultipleFilesWithLocks handles the complex locking for multiple files
func (m *Manager) replaceMultipleFilesWithLocks(ctx context.Context, flm *FileLockManager, userID int64, repoURL string, files map[string]string, commitMessage, customAuthor string, premiumLevel int) error {
	// Acquire locks for all files in a deterministic order to prevent deadlocks
	var filenames []string
	for filename := range files {
		filenames = append(filenames, filename)
	}
	
	// Sort filenames to ensure consistent locking order
	sort.Strings(filenames)
	
	// Acquire locks for all files
	var handles []*FileLockHandle
	defer func() {
		// Release all locks in reverse order
		for i := len(handles) - 1; i >= 0; i-- {
			handles[i].Release()
		}
	}()
	
	logger.Debug("Acquiring locks for multiple files", map[string]interface{}{
		"file_count": len(filenames),
		"files":      filenames,
	})
	
	for _, filename := range filenames {
		handle, err := flm.AcquireFileLock(ctx, userID, repoURL, filename, true)
		if err != nil {
			return fmt.Errorf("failed to acquire lock for file %s: %w", filename, err)
		}
		handles = append(handles, handle)
	}
	
	logger.Debug("All file locks acquired, proceeding with multiple file replacement", map[string]interface{}{
		"file_count": len(filenames),
	})
	
	// Now perform the actual file replacement with all locks held
	return m.replaceMultipleFilesWithAuthorAndPremiumLocked(files, commitMessage, customAuthor, premiumLevel)
}

// replaceMultipleFilesWithAuthorAndPremiumLocked performs the actual file replacement with the assumption that all files are locked
func (m *Manager) replaceMultipleFilesWithAuthorAndPremiumLocked(files map[string]string, commitMessage, customAuthor string, premiumLevel int) error {
	logger.Debug("Starting locked multiple file replacement", map[string]interface{}{
		"file_count": len(files),
		"author":     customAuthor,
	})

	// Ensure repository is initialized (lazy initialization)
	if err := m.ensureRepositoryWithPremium(premiumLevel); err != nil {
		return fmt.Errorf("failed to ensure repository: %w", err)
	}

	// Pull latest changes before replacing files to avoid conflicts
	logger.Debug("Pulling latest changes before replacing multiple files", map[string]interface{}{
		"file_count": len(files),
		"author":     customAuthor,
	})
	if err := m.pullLatest(); err != nil {
		if !strings.Contains(err.Error(), "remote repository is empty") {
			return fmt.Errorf("failed to pull latest changes: %w", err)
		}
	}

	// Write all files first
	for filename, content := range files {
		filePath := filepath.Join(m.repoPath, filename)

		// Ensure parent directories exist
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return fmt.Errorf("failed to create parent directories for %s: %w", filename, err)
		}

		// Write the new content (completely replace the file)
		if err := ioutil.WriteFile(filePath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write file %s: %w", filename, err)
		}
	}

	// Now commit all files in a single commit
	if err := m.commitMultipleFilesAndPushWithAuthor(files, commitMessage, customAuthor); err != nil {
		return fmt.Errorf("failed to commit and push multiple files: %w", err)
	}

	logger.Info("Multiple files replaced with file locks", map[string]interface{}{
		"file_count": len(files),
		"author":     customAuthor,
	})

	return nil
}

// commitMultipleFilesAndPushWithAuthor commits multiple files in a single commit
func (m *Manager) commitMultipleFilesAndPushWithAuthor(files map[string]string, commitMessage, customAuthor string) error {
	worktree, err := m.repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	// Add all files to the commit
	for filename := range files {
		if _, err := worktree.Add(filename); err != nil {
			return fmt.Errorf("failed to add file %s: %w", filename, err)
		}
	}

	// Use custom author if provided, otherwise use default
	authorString := customAuthor
	if authorString == "" {
		authorString = m.cfg.CommitAuthor
	}

	authorParts := strings.Split(authorString, " <")
	if len(authorParts) != 2 {
		return fmt.Errorf("invalid commit author format, expected 'Name <email>'")
	}

	name := authorParts[0]
	email := strings.TrimSuffix(authorParts[1], ">")

	commit, err := worktree.Commit(commitMessage, &git.CommitOptions{
		Author: &object.Signature{
			Name:  name,
			Email: email,
			When:  time.Now(),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	obj, err := m.repo.CommitObject(commit)
	if err != nil {
		return fmt.Errorf("failed to get commit object: %w", err)
	}

	logger.Debug("Multiple files committed successfully", map[string]interface{}{
		"commit_hash": obj.Hash.String(),
		"file_count":  len(files),
		"author":      name,
	})

	// Push to remote
	auth := &githttp.BasicAuth{
		Username: m.cfg.GitHubUsername,
		Password: m.cfg.GitHubToken,
	}

	if err := m.repo.Push(&git.PushOptions{
		Auth: auth,
	}); err != nil {
		return fmt.Errorf("failed to push: %w", err)
	}

	logger.Info("Multiple files pushed to repository", map[string]interface{}{
		"file_count": len(files),
		"author":     name,
	})

	return nil
}

func (m *Manager) CommitBinaryFile(filename string, data []byte, commitMessage string) error {
	// Ensure repository is initialized (lazy initialization)
	if err := m.ensureRepositoryWithPremium(m.premiumLevel); err != nil {
		return fmt.Errorf("failed to ensure repository: %w", err)
	}

	// Pull latest changes before committing binary file to avoid conflicts
	logger.Debug("Pulling latest changes before committing binary file", map[string]interface{}{
		"filename": filename,
	})
	if err := m.pullLatest(); err != nil {
		// If it's an auth error, return it immediately with helpful message
		if strings.Contains(err.Error(), "GitHub authorization failed") {
			return err
		}
		// For other errors, only fail if it's not an empty repo
		if !strings.Contains(err.Error(), "remote repository is empty") {
			return fmt.Errorf("failed to pull latest changes: %w", err)
		}
	}

	filePath := filepath.Join(m.repoPath, filename)

	// Create directory if it doesn't exist
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Write binary data directly to file
	if err := ioutil.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write binary file: %w", err)
	}

	logger.Debug("Binary file written to repository", map[string]interface{}{
		"filename": filename,
		"size":     len(data),
		"path":     filePath,
	})

	if err := m.commitAndPush(filename, commitMessage); err != nil {
		return fmt.Errorf("failed to commit and push: %w", err)
	}

	return nil
}

// UploadImageToCDN uploads an image to GitHub releases and returns the CDN URL
func (m *Manager) UploadImageToCDN(filename string, data []byte) (string, error) {
	// Get repository info
	owner, repo, err := m.GetRepoInfo()
	if err != nil {
		return "", fmt.Errorf("failed to get repo info: %w", err)
	}

	// First, check if we have a release to upload to, or create one
	releaseID, err := m.getOrCreateAssetRelease(owner, repo)
	if err != nil {
		return "", fmt.Errorf("failed to get/create release: %w", err)
	}

	// Upload asset to the release
	uploadURL := fmt.Sprintf("https://uploads.github.com/repos/%s/%s/releases/%d/assets?name=%s", owner, repo, releaseID, filename)

	// Create HTTP request
	req, err := http.NewRequest("POST", uploadURL, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Authorization", "token "+m.cfg.GitHubToken)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	// Make the request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to upload image: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response to get the download URL
	var result struct {
		BrowserDownloadURL string `json:"browser_download_url"`
		URL                string `json:"url"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	logger.Debug("Image uploaded to GitHub CDN", map[string]interface{}{
		"filename": filename,
		"url":      result.BrowserDownloadURL,
		"size":     len(data),
	})

	return result.BrowserDownloadURL, nil
}

// getOrCreateAssetRelease gets or creates a release for asset uploads with chunking support
func (m *Manager) getOrCreateAssetRelease(owner, repo string) (int, error) {
	// Find the current active release for assets
	releaseID, releaseName, err := m.findActiveAssetRelease(owner, repo)
	if err != nil {
		return 0, fmt.Errorf("failed to find active release: %w", err)
	}

	// If we found a release, check if it's near capacity
	if releaseID > 0 {
		assetCount, err := m.getAssetCount(owner, repo, releaseID)
		if err != nil {
			logger.Warn("Failed to get asset count, assuming release is not full", map[string]interface{}{
				"error":      err.Error(),
				"release_id": releaseID,
			})
			return releaseID, nil
		}

		// Check if we're at or near the limit
		if assetCount < consts.MaxAssetsPerRelease {
			logger.Debug("Using existing release", map[string]interface{}{
				"release_id":   releaseID,
				"release_name": releaseName,
				"asset_count":  assetCount,
				"max_assets":   consts.MaxAssetsPerRelease,
			})
			return releaseID, nil
		}

		// Release is full, need to create a new one
		logger.Info("Release is full, creating new release", map[string]interface{}{
			"current_release": releaseName,
			"asset_count":     assetCount,
			"max_assets":      consts.MaxAssetsPerRelease,
		})
	}

	// Create a new release (either first time or current is full)
	newReleaseName := m.getNextAssetReleaseName(owner, repo)
	newReleaseID, err := m.createAssetRelease(owner, repo, newReleaseName)
	if err != nil {
		return 0, fmt.Errorf("failed to create new release: %w", err)
	}

	logger.Info("Created new asset release", map[string]interface{}{
		"release_id":   newReleaseID,
		"release_name": newReleaseName,
		"repo":         fmt.Sprintf("%s/%s", owner, repo),
	})

	return newReleaseID, nil
}

// findActiveAssetRelease finds the current active asset release
func (m *Manager) findActiveAssetRelease(owner, repo string) (int, string, error) {
	// Get all releases and find the latest asset release
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases", owner, repo)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "token "+m.cfg.GitHubToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("failed to get releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, "", nil // No releases found
	}

	var releases []struct {
		ID      int    `json:"id"`
		TagName string `json:"tag_name"`
		Name    string `json:"name"`
	}

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, "", fmt.Errorf("failed to read response: %w", err)
	}

	if err := json.Unmarshal(respBody, &releases); err != nil {
		return 0, "", fmt.Errorf("failed to parse releases: %w", err)
	}

	// Find the latest asset release (Assets, Assets1, Assets2, etc.)
	var latestAssetRelease struct {
		ID   int
		Name string
	}

	for _, release := range releases {
		if strings.HasPrefix(release.TagName, "assets") {
			// Found an asset release, check if it's the latest
			if latestAssetRelease.ID == 0 || m.isNewerAssetRelease(release.TagName, latestAssetRelease.Name) {
				latestAssetRelease.ID = release.ID
				latestAssetRelease.Name = release.TagName
			}
		}
	}

	return latestAssetRelease.ID, latestAssetRelease.Name, nil
}

// getAssetCount gets the number of assets in a release
func (m *Manager) getAssetCount(owner, repo string, releaseID int) (int, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/%d/assets", owner, repo, releaseID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "token "+m.cfg.GitHubToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to get assets: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("failed to get assets, status: %d", resp.StatusCode)
	}

	var assets []struct {
		ID int `json:"id"`
	}

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response: %w", err)
	}

	if err := json.Unmarshal(respBody, &assets); err != nil {
		return 0, fmt.Errorf("failed to parse assets: %w", err)
	}

	return len(assets), nil
}

// getNextAssetReleaseName determines the next release name to create
func (m *Manager) getNextAssetReleaseName(owner, repo string) string {
	// Get all releases to find the highest numbered asset release
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases", owner, repo)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "assets" // Default to first release
	}

	req.Header.Set("Authorization", "token "+m.cfg.GitHubToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "assets" // Default to first release
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "assets" // Default to first release
	}

	var releases []struct {
		TagName string `json:"tag_name"`
	}

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "assets" // Default to first release
	}

	if err := json.Unmarshal(respBody, &releases); err != nil {
		return "assets" // Default to first release
	}

	// Find the highest numbered asset release
	maxNumber := 0
	hasAssetsRelease := false

	for _, release := range releases {
		if release.TagName == "assets" {
			hasAssetsRelease = true
		} else if strings.HasPrefix(release.TagName, "assets") {
			// Extract number from "assets1", "assets2", etc.
			numberStr := strings.TrimPrefix(release.TagName, "assets")
			if number, err := strconv.Atoi(numberStr); err == nil {
				if number > maxNumber {
					maxNumber = number
				}
			}
		}
	}

	// If no assets release exists, create "assets"
	if !hasAssetsRelease && maxNumber == 0 {
		return "assets"
	}

	// Create next numbered release
	return fmt.Sprintf("assets%d", maxNumber+1)
}

// isNewerAssetRelease checks if release1 is newer than release2
func (m *Manager) isNewerAssetRelease(release1, release2 string) bool {
	// Extract numbers from release names
	getNumber := func(name string) int {
		if name == "assets" {
			return 0
		}
		if strings.HasPrefix(name, "assets") {
			numberStr := strings.TrimPrefix(name, "assets")
			if number, err := strconv.Atoi(numberStr); err == nil {
				return number
			}
		}
		return -1
	}

	return getNumber(release1) > getNumber(release2)
}

// createAssetRelease creates a new asset release
func (m *Manager) createAssetRelease(owner, repo, releaseName string) (int, error) {
	createURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases", owner, repo)

	releaseData := map[string]interface{}{
		"tag_name":   releaseName,
		"name":       strings.Title(releaseName),
		"body":       "Automatically generated release for asset storage",
		"draft":      false,
		"prerelease": false,
	}

	jsonData, err := json.Marshal(releaseData)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal release data: %w", err)
	}

	createReq, err := http.NewRequest("POST", createURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return 0, fmt.Errorf("failed to create release request: %w", err)
	}

	createReq.Header.Set("Authorization", "token "+m.cfg.GitHubToken)
	createReq.Header.Set("Accept", "application/vnd.github.v3+json")
	createReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	createResp, err := client.Do(createReq)
	if err != nil {
		return 0, fmt.Errorf("failed to create release: %w", err)
	}
	defer createResp.Body.Close()

	if createResp.StatusCode != http.StatusCreated {
		createRespBody, _ := ioutil.ReadAll(createResp.Body)
		return 0, fmt.Errorf("failed to create release, status %d: %s", createResp.StatusCode, string(createRespBody))
	}

	var newRelease struct {
		ID int `json:"id"`
	}

	createRespBody, err := ioutil.ReadAll(createResp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read create response: %w", err)
	}

	if err := json.Unmarshal(createRespBody, &newRelease); err != nil {
		return 0, fmt.Errorf("failed to parse new release: %w", err)
	}

	return newRelease.ID, nil
}

// GetRepositorySize returns the current repository size in bytes
// If repository doesn't exist locally, fetches size from GitHub API
func (m *Manager) GetRepositorySize() (int64, error) {
	if m.repoPath == "" {
		return 0, fmt.Errorf("repository path not set")
	}

	// Check if repository directory exists
	if _, err := os.Stat(m.repoPath); os.IsNotExist(err) {
		// Repository doesn't exist locally, try to get size from GitHub API
		return m.getRemoteRepositorySize()
	}

	// Repository exists locally, calculate actual size
	size, err := getDirectorySize(m.repoPath)
	if err != nil {
		return 0, fmt.Errorf("failed to calculate repository size: %w", err)
	}

	return size, nil
}

// getRemoteRepositorySize fetches repository size from GitHub API (returns size in bytes)
func (m *Manager) getRemoteRepositorySize() (int64, error) {
	// Extract owner and repo name from URL
	owner, repo, err := m.GetRepoInfo()
	if err != nil {
		return 0, fmt.Errorf("failed to parse repository URL: %w", err)
	}

	// GitHub API endpoint for repository information
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)

	// Create HTTP request
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create API request: %w", err)
	}

	// Add authorization header
	req.Header.Set("Authorization", fmt.Sprintf("token %s", m.cfg.GitHubToken))
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	// Make the request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to call GitHub API: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return 0, fmt.Errorf("repository not found or access denied")
		}
		return 0, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	// Parse response
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read API response: %w", err)
	}

	var repoInfo GitHubRepo
	if err := json.Unmarshal(body, &repoInfo); err != nil {
		return 0, fmt.Errorf("failed to parse API response: %w", err)
	}

	// GitHub API returns size in KB, convert to bytes
	sizeBytes := int64(repoInfo.Size * 1024)

	logger.Debug("Retrieved remote repository size", map[string]interface{}{
		"repo":       fmt.Sprintf("%s/%s", owner, repo),
		"size_kb":    repoInfo.Size,
		"size_bytes": sizeBytes,
		"size_mb":    float64(sizeBytes) / 1024 / 1024,
	})

	return sizeBytes, nil
}

// GetRepositoryMaxSize returns default max size
func (m *Manager) GetRepositoryMaxSize() float64 {
	return float64(maxRepoSize) / 1024 / 1024
}

// GetRepositoryMaxSizeWithPremium returns max size based on premium level
func (m *Manager) GetRepositoryMaxSizeWithPremium(premiumLevel int) float64 {
	baseSize := float64(maxRepoSize)

	// Premium multipliers based on level
	switch premiumLevel {
	case 0:
		// Free tier
		return baseSize / 1024 / 1024
	case 1:
		// Coffee tier ($5) - 2x capacity (10MB)
		return (baseSize * 2) / 1024 / 1024
	case 2:
		// Cake tier ($15) - 4x capacity (20MB)
		return (baseSize * 4) / 1024 / 1024
	case 3:
		// Sponsor tier ($50) - 10x capacity (50MB)
		return (baseSize * 10) / 1024 / 1024
	default:
		// Unknown premium level, return base size
		return baseSize / 1024 / 1024
	}
}

// GetRepositorySizeInfo returns formatted size information
func (m *Manager) GetRepositorySizeInfo() (float64, float64, error) {
	size, err := m.GetRepositorySize()
	if err != nil {
		return 0, 0, err
	}

	sizeMB := float64(size) / 1024 / 1024
	percentage := (float64(size) / float64(maxRepoSize)) * 100

	return sizeMB, percentage, nil
}

// GetRepositorySizeInfoWithPremium returns formatted size information with premium consideration
func (m *Manager) GetRepositorySizeInfoWithPremium(premiumLevel int) (float64, float64, error) {
	size, err := m.GetRepositorySize()
	if err != nil {
		return 0, 0, err
	}

	sizeMB := float64(size) / 1024 / 1024
	maxSizeMB := m.GetRepositoryMaxSizeWithPremium(premiumLevel)
	maxSizeBytes := maxSizeMB * 1024 * 1024
	percentage := (float64(size) / maxSizeBytes) * 100

	return sizeMB, percentage, nil
}

// GetGitHubFileURL generates a GitHub file URL for a given filename
func (m *Manager) GetGitHubFileURL(filename string) (string, error) {
	owner, repo, err := m.GetRepoInfo()
	if err != nil {
		return "", fmt.Errorf("failed to get repo info: %w", err)
	}

	// Format: https://github.com/owner/repo/blob/main/filename
	url := fmt.Sprintf("https://github.com/%s/%s/blob/main/%s", owner, repo, filename)
	return url, nil
}

// IsRepositoryNearCapacity checks if repository is close to size limit (free tier)
func (m *Manager) IsRepositoryNearCapacity() (bool, float64, error) {
	return m.IsRepositoryNearCapacityWithPremium(0) // Default to free tier
}

// IsRepositoryNearCapacityWithPremium checks if repository is close to size limit based on premium level
func (m *Manager) IsRepositoryNearCapacityWithPremium(premiumLevel int) (bool, float64, error) {
	_, percentage, err := m.GetRepositorySizeInfoWithPremium(premiumLevel)
	if err != nil {
		return false, 0, err
	}

	// Consider "near capacity" as 97% or more
	return percentage >= 97.0, percentage, nil
}

// GetDefaultBranch attempts to detect the default branch (main or master)
func (m *Manager) GetDefaultBranch() (string, error) {
	if m.repo == nil {
		return "main", nil // Default fallback
	}

	// Try to get the HEAD reference
	head, err := m.repo.Head()
	if err != nil {
		// Fallback to common branch names
		return "main", nil
	}

	// Extract branch name from reference
	branchName := head.Name().Short()
	if branchName == "" {
		return "main", nil
	}

	return branchName, nil
}

// GetGitHubFileURLWithBranch generates a GitHub file URL with correct branch
func (m *Manager) GetGitHubFileURLWithBranch(filename string) (string, error) {
	owner, repo, err := m.GetRepoInfo()
	if err != nil {
		return "", fmt.Errorf("failed to get repo info: %w", err)
	}

	branch, err := m.GetDefaultBranch()
	if err != nil {
		branch = "main" // Fallback
	}

	// Format: https://github.com/owner/repo/blob/branch/filename
	url := fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s", owner, repo, branch, filename)
	return url, nil
}

// GetProviderType returns the provider type for the Manager
func (m *Manager) GetProviderType() ProviderType {
	return ProviderTypeClone
}
