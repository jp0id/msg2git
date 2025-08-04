package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/msg2git/msg2git/internal/logger"
)

// GitHub Contents API structures
type apiFileContent struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	SHA         string `json:"sha"`
	Size        int    `json:"size"`
	URL         string `json:"url"`
	HTMLURL     string `json:"html_url"`
	GitURL      string `json:"git_url"`
	DownloadURL string `json:"download_url"`
	Type        string `json:"type"`
	Content     string `json:"content"`
	Encoding    string `json:"encoding"`
}

type apiFileUpdateRequest struct {
	Message   string                 `json:"message"`
	Content   string                 `json:"content"`
	SHA       string                 `json:"sha,omitempty"`
	Branch    string                 `json:"branch,omitempty"`
	Committer *apiCommitterInfo      `json:"committer,omitempty"`
	Author    *apiCommitterInfo      `json:"author,omitempty"`
}

type apiCommitterInfo struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type apiFileUpdateResponse struct {
	Content apiFileContent `json:"content"`
	Commit  struct {
		SHA     string `json:"sha"`
		HTMLURL string `json:"html_url"`
	} `json:"commit"`
}

// FileManager implementation for API provider
func (p *APIBasedProvider) ReadFile(filename string) (string, error) {
	endpoint := fmt.Sprintf("/repos/%s/%s/contents/%s", p.repoOwner, p.repoName, filename)
	
	resp, err := p.makeAPIRequest("GET", endpoint, nil)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			// File doesn't exist, return empty content
			return "", nil
		}
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	defer resp.Body.Close()

	var fileContent apiFileContent
	if err := json.NewDecoder(resp.Body).Decode(&fileContent); err != nil {
		return "", fmt.Errorf("failed to decode file content: %w", err)
	}

	if fileContent.Encoding != "base64" {
		return "", fmt.Errorf("unsupported file encoding: %s", fileContent.Encoding)
	}

	// Decode base64 content
	contentBytes, err := base64.StdEncoding.DecodeString(fileContent.Content)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64 content: %w", err)
	}

	logger.Debug("File read via API", map[string]interface{}{
		"filename": filename,
		"size":     fileContent.Size,
		"sha":      fileContent.SHA,
		"user_id":  p.config.UserID,
	})

	return string(contentBytes), nil
}

// fileExists checks if a file exists in the repository
func (p *APIBasedProvider) fileExists(filename string) bool {
	endpoint := fmt.Sprintf("/repos/%s/%s/contents/%s", p.repoOwner, p.repoName, filename)
	
	// Use a direct HTTP request instead of makeAPIRequest to avoid error conversion
	url := p.baseURL + endpoint
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false
	}
	
	// Set authentication headers
	req.Header.Set("Authorization", "Bearer "+p.config.Config.GetGitHubToken())
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	
	return resp.StatusCode == 200
}

func (p *APIBasedProvider) CommitFile(filename, content, commitMessage string) error {
	return p.CommitFileWithAuthorAndPremium(filename, content, commitMessage, p.config.Config.GetCommitAuthor(), p.config.PremiumLevel)
}

func (p *APIBasedProvider) CommitFileWithAuthor(filename, content, commitMessage, customAuthor string) error {
	return p.CommitFileWithAuthorAndPremium(filename, content, commitMessage, customAuthor, p.config.PremiumLevel)
}

func (p *APIBasedProvider) CommitFileWithAuthorAndPremium(filename, content, commitMessage, customAuthor string, premiumLevel int) error {
	// For msg2git's use case, CommitFile means "prepend" to existing file
	return p.updateFileContent(filename, content, commitMessage, customAuthor, true) // true = prepend mode
}

// CommitFileWithAuthorAndPremiumLocked performs file commit with the assumption that the file is already locked
func (p *APIBasedProvider) CommitFileWithAuthorAndPremiumLocked(filename, content, commitMessage, customAuthor string, premiumLevel int) error {
	// For msg2git's use case, CommitFile means "prepend" to existing file
	return p.updateFileContentLocked(filename, content, commitMessage, customAuthor, true) // true = prepend mode
}

func (p *APIBasedProvider) ReplaceFile(filename, content, commitMessage string) error {
	return p.ReplaceFileWithAuthorAndPremium(filename, content, commitMessage, p.config.Config.GetCommitAuthor(), p.config.PremiumLevel)
}

func (p *APIBasedProvider) ReplaceFileWithAuthor(filename, content, commitMessage, customAuthor string) error {
	return p.ReplaceFileWithAuthorAndPremium(filename, content, commitMessage, customAuthor, p.config.PremiumLevel)
}

func (p *APIBasedProvider) ReplaceFileWithAuthorAndPremium(filename, content, commitMessage, customAuthor string, premiumLevel int) error {
	// Replace mode - completely replace file content
	return p.updateFileContent(filename, content, commitMessage, customAuthor, false) // false = replace mode
}

// ReplaceFileWithAuthorAndPremiumLocked performs file replacement with the assumption that the file is already locked
func (p *APIBasedProvider) ReplaceFileWithAuthorAndPremiumLocked(filename, content, commitMessage, customAuthor string, premiumLevel int) error {
	// Replace mode - completely replace file content
	return p.updateFileContentLocked(filename, content, commitMessage, customAuthor, false) // false = replace mode
}

func (p *APIBasedProvider) CommitBinaryFile(filename string, data []byte, commitMessage string) error {
	// Encode binary data as base64
	content := base64.StdEncoding.EncodeToString(data)
	
	// Binary files are always replaced, not prepended
	return p.updateFileContent(filename, content, commitMessage, p.config.Config.GetCommitAuthor(), false)
}

func (p *APIBasedProvider) ReplaceMultipleFilesWithAuthorAndPremium(files map[string]string, commitMessage, customAuthor string, premiumLevel int) error {
	// Get user ID for file locking
	userID, err := p.getUserIDForLocking()
	if err != nil {
		return fmt.Errorf("failed to get user ID for locking: %w", err)
	}
	
	// Get repository URL for locking
	repoURL := fmt.Sprintf("%s/%s", p.repoOwner, p.repoName)
	
	// Use file lock manager to prevent concurrent modifications
	flm := GetFileLockManager()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second) // Longer timeout for multiple files
	defer cancel()
	
	return p.replaceMultipleFilesWithLocks(ctx, flm, userID, repoURL, files, commitMessage, customAuthor, premiumLevel)
}

// replaceMultipleFilesWithLocks handles the complex locking for multiple files in API provider
func (p *APIBasedProvider) replaceMultipleFilesWithLocks(ctx context.Context, flm *FileLockManager, userID int64, repoURL string, files map[string]string, commitMessage, customAuthor string, premiumLevel int) error {
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
	
	logger.Debug("Acquiring locks for multiple files via API", map[string]interface{}{
		"file_count": len(filenames),
		"files":      filenames,
		"user_id":    p.config.UserID,
	})
	
	for _, filename := range filenames {
		handle, err := flm.AcquireFileLock(ctx, userID, repoURL, filename, true)
		if err != nil {
			return fmt.Errorf("failed to acquire lock for file %s: %w", filename, err)
		}
		handles = append(handles, handle)
	}
	
	logger.Debug("All file locks acquired, proceeding with multiple file replacement via API", map[string]interface{}{
		"file_count": len(filenames),
		"user_id":    p.config.UserID,
	})
	
	// Now perform the actual file replacement with all locks held
	return p.ReplaceMultipleFilesWithAuthorAndPremiumLocked(files, commitMessage, customAuthor, premiumLevel)
}

// ReplaceMultipleFilesWithAuthorAndPremiumLocked performs the actual file replacement with the assumption that all files are locked
func (p *APIBasedProvider) ReplaceMultipleFilesWithAuthorAndPremiumLocked(files map[string]string, commitMessage, customAuthor string, premiumLevel int) error {
	logger.Debug("Starting locked multiple file replacement via API", map[string]interface{}{
		"file_count": len(files),
		"user_id":    p.config.UserID,
	})
	
	// For multiple files, we need to use Git Data API for atomic commits
	// For now, we'll implement it as sequential operations with all locks held
	// TODO: Implement true atomic multi-file commits using Git Data API
	
	logger.Info("Committing multiple files via API with file locks", map[string]interface{}{
		"file_count": len(files),
		"user_id":    p.config.UserID,
	})

	for filename, content := range files {
		// Use the locked version of the file operation
		if err := p.updateFileContentLocked(filename, content, commitMessage, customAuthor, false); err != nil {
			return fmt.Errorf("failed to commit file %s: %w", filename, err)
		}
	}

	logger.Info("Multiple files replaced via API with file locks", map[string]interface{}{
		"file_count": len(files),
		"user_id":    p.config.UserID,
	})

	return nil
}

// updateFileContent is the core method that handles both prepend and replace operations
func (p *APIBasedProvider) updateFileContent(filename, newContent, commitMessage, customAuthor string, prependMode bool) error {
	// Get user ID for file locking
	userID, err := p.getUserIDForLocking()
	if err != nil {
		return fmt.Errorf("failed to get user ID for locking: %w", err)
	}
	
	// Get repository URL for locking
	repoURL := fmt.Sprintf("%s/%s", p.repoOwner, p.repoName)
	
	// Use file lock manager to prevent concurrent modifications
	flm := GetFileLockManager()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	
	return flm.WithFileLock(ctx, userID, repoURL, filename, true, func() error {
		return p.updateFileContentLocked(filename, newContent, commitMessage, customAuthor, prependMode)
	})
}

// updateFileContentLocked performs the actual file update with the assumption that the file is locked
func (p *APIBasedProvider) updateFileContentLocked(filename, newContent, commitMessage, customAuthor string, prependMode bool) error {
	var finalContent string
	var currentSHA string

	logger.Debug("Starting locked file update", map[string]interface{}{
		"filename":     filename,
		"prepend_mode": prependMode,
		"user_id":      p.config.UserID,
	})

	// Check if file exists
	fileExists := p.fileExists(filename)

	if prependMode {
		var existingContent string
		
		// Only read existing content if file exists
		if fileExists {
			content, err := p.ReadFile(filename)
			if err != nil {
				return fmt.Errorf("failed to read existing file: %w", err)
			}
			existingContent = content
			
			// Get current file SHA for update
			sha, err := p.getFileSHA(filename)
			if err != nil {
				return fmt.Errorf("failed to get file SHA: %w", err)
			}
			currentSHA = sha
		}

		// Prepend new content to existing content
		if existingContent == "" {
			finalContent = newContent
		} else {
			finalContent = newContent + "\n" + existingContent
		}
	} else {
		// Replace mode - use new content as-is
		finalContent = newContent

		// Still need SHA if file exists
		if fileExists {
			if sha, err := p.getFileSHA(filename); err == nil {
				currentSHA = sha
			}
		}
	}

	// Parse author information
	author := parseCommitAuthor(customAuthor)

	// Get the actual default branch
	defaultBranch, err := p.GetDefaultBranch()
	if err != nil {
		return fmt.Errorf("failed to get default branch: %w", err)
	}

	// Prepare the update request
	updateRequest := apiFileUpdateRequest{
		Message: commitMessage,
		Content: base64.StdEncoding.EncodeToString([]byte(finalContent)),
		Branch:  defaultBranch,
		Author:  author,
		Committer: author,
	}

	// Include SHA if file exists (for updates)
	if fileExists && currentSHA != "" {
		updateRequest.SHA = currentSHA
	}

	logger.Debug("Making GitHub API request with file lock", map[string]interface{}{
		"filename":     filename,
		"has_sha":      currentSHA != "",
		"current_sha":  currentSHA,
		"user_id":      p.config.UserID,
	})

	// Make the API call
	endpoint := fmt.Sprintf("/repos/%s/%s/contents/%s", p.repoOwner, p.repoName, filename)
	resp, err := p.makeAPIRequest("PUT", endpoint, updateRequest)
	if err != nil {
		return fmt.Errorf("failed to update file: %w", err)
	}
	defer resp.Body.Close()

	var updateResponse apiFileUpdateResponse
	if err := json.NewDecoder(resp.Body).Decode(&updateResponse); err != nil {
		return fmt.Errorf("failed to decode update response: %w", err)
	}

	logger.Info("File updated via API with file lock", map[string]interface{}{
		"filename":     filename,
		"commit_sha":   updateResponse.Commit.SHA,
		"commit_url":   updateResponse.Commit.HTMLURL,
		"prepend_mode": prependMode,
		"user_id":      p.config.UserID,
	})

	return nil
}

// getUserIDForLocking extracts user ID for file locking
func (p *APIBasedProvider) getUserIDForLocking() (int64, error) {
	if p.config.UserID == "" {
		// Fallback to a default value if UserID is not set
		return 0, nil
	}
	
	// Handle "user_123456" format by extracting the numeric part
	userIDStr := p.config.UserID
	if strings.HasPrefix(userIDStr, "user_") {
		userIDStr = strings.TrimPrefix(userIDStr, "user_")
	}
	
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		// If parsing still fails, use a hash of the original string as fallback
		hash := int64(0)
		for _, char := range p.config.UserID {
			hash = hash*31 + int64(char)
		}
		if hash < 0 {
			hash = -hash
		}
		
		logger.Debug("Using hash as user ID for locking", map[string]interface{}{
			"original_user_id": p.config.UserID,
			"hash_user_id":     hash,
		})
		
		return hash, nil
	}
	
	return userID, nil
}

// getFileSHA retrieves the current SHA of a file (needed for updates)
func (p *APIBasedProvider) getFileSHA(filename string) (string, error) {
	endpoint := fmt.Sprintf("/repos/%s/%s/contents/%s", p.repoOwner, p.repoName, filename)
	
	resp, err := p.makeAPIRequest("GET", endpoint, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var fileContent apiFileContent
	if err := json.NewDecoder(resp.Body).Decode(&fileContent); err != nil {
		return "", err
	}

	return fileContent.SHA, nil
}

// parseCommitAuthor parses "Name <email>" format into separate components
func parseCommitAuthor(authorString string) *apiCommitterInfo {
	// Default values
	author := &apiCommitterInfo{
		Name:  "Msg2Git Bot",
		Email: "bot@msg2git.com",
	}

	if authorString == "" {
		return author
	}

	// Parse "Name <email>" format
	if strings.Contains(authorString, "<") && strings.Contains(authorString, ">") {
		parts := strings.Split(authorString, "<")
		if len(parts) == 2 {
			author.Name = strings.TrimSpace(parts[0])
			email := strings.TrimSpace(parts[1])
			email = strings.TrimSuffix(email, ">")
			author.Email = email
		}
	} else {
		// If no email format, treat the whole string as name
		author.Name = strings.TrimSpace(authorString)
	}

	return author
}