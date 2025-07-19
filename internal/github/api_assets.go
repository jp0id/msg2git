package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/msg2git/msg2git/internal/consts"
	"github.com/msg2git/msg2git/internal/logger"
)

var (
	// Per-repository mutex map for release creation to prevent race conditions
	// when multiple concurrent uploads try to create the same release
	releaseMutexes = make(map[string]*repositoryMutex)
	releaseMutexesMu sync.RWMutex
	cdnCleanupStarted bool
)

// repositoryMutex wraps a mutex with usage tracking for cleanup
type repositoryMutex struct {
	mu       sync.Mutex
	lastUsed time.Time
	refCount int
}

// GitHub Releases API structures
type apiReleaseRequest struct {
	TagName         string `json:"tag_name"`
	TargetCommitish string `json:"target_commitish"`
	Name            string `json:"name"`
	Body            string `json:"body"`
	Draft           bool   `json:"draft"`
	Prerelease      bool   `json:"prerelease"`
}

type apiReleaseResponse struct {
	ID          int    `json:"id"`
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	HTMLURL     string `json:"html_url"`
	UploadURL   string `json:"upload_url"`
	AssetsURL   string `json:"assets_url"`
	CreatedAt   string `json:"created_at"`
	PublishedAt string `json:"published_at"`
}

type apiAssetResponse struct {
	ID                int    `json:"id"`
	Name              string `json:"name"`
	Label             string `json:"label"`
	State             string `json:"state"`
	ContentType       string `json:"content_type"`
	Size              int    `json:"size"`
	DownloadCount     int    `json:"download_count"`
	BrowserDownloadURL string `json:"browser_download_url"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

// AssetManager implementation for API provider
func (p *APIBasedProvider) UploadImageToCDN(filename string, data []byte) (string, error) {
	// Step 1: Get or create a release for assets
	release, err := p.getOrCreateAssetsRelease()
	if err != nil {
		return "", fmt.Errorf("failed to get/create release: %w", err)
	}

	// Step 2: Upload the asset to the release
	assetURL, err := p.uploadAssetToRelease(release.ID, filename, data)
	if err != nil {
		// Check if this is a race condition error and retry once
		if strings.Contains(err.Error(), "release not found") {
			logger.Warn("Release not found during asset upload, retrying once", map[string]interface{}{
				"release_id": release.ID,
				"filename":   filename,
				"user_id":    p.config.UserID,
			})
			// Retry with a fresh release (may have been created by another goroutine)
			retryRelease, retryErr := p.getOrCreateAssetsRelease()
			if retryErr == nil {
				assetURL, err = p.uploadAssetToRelease(retryRelease.ID, filename, data)
				if err == nil {
					return assetURL, nil
				}
			}
		}
		return "", fmt.Errorf("failed to upload asset: %w", err)
	}

	logger.Info("Image uploaded to GitHub CDN via API", map[string]interface{}{
		"filename":    filename,
		"size":        len(data),
		"asset_url":   assetURL,
		"release_id":  release.ID,
		"user_id":     p.config.UserID,
	})

	return assetURL, nil
}

// getOrCreateAssetsRelease gets an existing assets release or creates a new one
// Uses a per-repository mutex to prevent concurrent release creation race conditions
func (p *APIBasedProvider) getOrCreateAssetsRelease() (*apiReleaseResponse, error) {
	// Get repository-specific mutex to prevent concurrent access to release creation
	// This prevents multiple goroutines from creating duplicate releases for the same repo
	repoKey := fmt.Sprintf("%s/%s", p.repoOwner, p.repoName)
	repoMutex := p.getRepositoryMutex(repoKey)
	
	repoMutex.mu.Lock()
	defer func() {
		repoMutex.mu.Unlock()
		// Decrement reference count after use
		releaseMutexesMu.Lock()
		repoMutex.refCount--
		repoMutex.lastUsed = time.Now()
		releaseMutexesMu.Unlock()
	}()

	logger.Debug("Acquiring release creation lock for CDN upload", map[string]interface{}{
		"user_id":  p.config.UserID,
		"repo_key": repoKey,
	})

	// Try to get existing release
	releasesEndpoint := fmt.Sprintf("/repos/%s/%s/releases", p.repoOwner, p.repoName)
	
	resp, err := p.makeAPIRequest("GET", releasesEndpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get releases: %w", err)
	}
	defer resp.Body.Close()

	var releases []apiReleaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("failed to decode releases: %w", err)
	}

	// Look for existing assets release
	const assetsReleasePrefix = "assets-"
	for _, release := range releases {
		if strings.HasPrefix(release.TagName, assetsReleasePrefix) {
			// Check if this release has space for more assets
			assetCount, err := p.getAssetCount(release.ID)
			if err == nil && assetCount < consts.MaxAssetsPerRelease {
				logger.Debug("Using existing assets release", map[string]interface{}{
					"release_id":   release.ID,
					"tag_name":     release.TagName,
					"asset_count":  assetCount,
					"max_assets":   consts.MaxAssetsPerRelease,
					"user_id":      p.config.UserID,
				})
				return &release, nil
			}
		}
	}

	// No suitable release found, create a new one
	logger.Debug("Creating new assets release with mutex protection", map[string]interface{}{
		"user_id":  p.config.UserID,
		"repo_key": repoKey,
	})
	return p.createNewAssetsRelease()
}

// createNewAssetsRelease creates a new release for storing assets
func (p *APIBasedProvider) createNewAssetsRelease() (*apiReleaseResponse, error) {
	timestamp := time.Now().Format("20060102-150405")
	tagName := fmt.Sprintf("assets-%s", timestamp)
	
	// Get the actual default branch
	defaultBranch, err := p.GetDefaultBranch()
	if err != nil {
		return nil, fmt.Errorf("failed to get default branch: %w", err)
	}
	
	releaseRequest := apiReleaseRequest{
		TagName:         tagName,
		TargetCommitish: defaultBranch,
		Name:            fmt.Sprintf("Assets Release %s", timestamp),
		Body:            "This release contains uploaded assets (photos, files) from Msg2Git bot.",
		Draft:           false,
		Prerelease:      true, // Mark as prerelease so it doesn't show as latest
	}

	endpoint := fmt.Sprintf("/repos/%s/%s/releases", p.repoOwner, p.repoName)
	resp, err := p.makeAPIRequest("POST", endpoint, releaseRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to create release: %w", err)
	}
	defer resp.Body.Close()

	var release apiReleaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to decode create release response: %w", err)
	}

	logger.Info("Created new assets release", map[string]interface{}{
		"release_id": release.ID,
		"tag_name":   release.TagName,
		"html_url":   release.HTMLURL,
		"user_id":    p.config.UserID,
	})

	return &release, nil
}

// getAssetCount gets the number of assets in a release
func (p *APIBasedProvider) getAssetCount(releaseID int) (int, error) {
	endpoint := fmt.Sprintf("/repos/%s/%s/releases/%d/assets", p.repoOwner, p.repoName, releaseID)
	
	resp, err := p.makeAPIRequest("GET", endpoint, nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var assets []apiAssetResponse
	if err := json.NewDecoder(resp.Body).Decode(&assets); err != nil {
		return 0, err
	}

	return len(assets), nil
}

// uploadAssetToRelease uploads a file as an asset to a GitHub release
func (p *APIBasedProvider) uploadAssetToRelease(releaseID int, filename string, data []byte) (string, error) {
	// GitHub upload URL needs to be modified for asset uploads
	uploadURL := fmt.Sprintf("https://uploads.github.com/repos/%s/%s/releases/%d/assets?name=%s", 
		p.repoOwner, p.repoName, releaseID, filename)

	// Create the request
	req, err := http.NewRequest("POST", uploadURL, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("failed to create upload request: %w", err)
	}

	// Set headers
	req.Header.Set("Authorization", "Bearer "+p.config.Config.GetGitHubToken())
	req.Header.Set("Content-Type", p.getContentType(filename))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	// Make the request
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		
		// Handle specific error cases with user-friendly messages
		switch resp.StatusCode {
		case 401:
			if strings.Contains(string(bodyBytes), "Bad credentials") {
				return "", fmt.Errorf(consts.GitHubAuthFailed)
			}
			return "", fmt.Errorf("unauthorized - check GitHub token permissions for releases")
		case 403:
			if strings.Contains(string(bodyBytes), "rate limit") {
				return "", fmt.Errorf("GitHub API rate limit exceeded - please try again later")
			}
			return "", fmt.Errorf("forbidden - token may not have releases permissions")
		case 422:
			if strings.Contains(string(bodyBytes), "already_exists") {
				return "", fmt.Errorf("asset with this name already exists - filename collision during concurrent uploads")
			}
			return "", fmt.Errorf("validation failed: %s", string(bodyBytes))
		case 404:
			return "", fmt.Errorf("release not found - unable to upload asset (possible race condition during concurrent uploads)")
		default:
			return "", fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, string(bodyBytes))
		}
	}

	// Parse response
	var asset apiAssetResponse
	if err := json.NewDecoder(resp.Body).Decode(&asset); err != nil {
		return "", fmt.Errorf("failed to decode upload response: %w", err)
	}

	return asset.BrowserDownloadURL, nil
}

// getRepositoryMutex gets or creates a mutex for a specific repository
func (p *APIBasedProvider) getRepositoryMutex(repoKey string) *repositoryMutex {
	releaseMutexesMu.RLock()
	mutex, exists := releaseMutexes[repoKey]
	releaseMutexesMu.RUnlock()

	if !exists {
		releaseMutexesMu.Lock()
		// Double-check pattern
		if mutex, exists = releaseMutexes[repoKey]; !exists {
			mutex = &repositoryMutex{
				lastUsed: time.Now(),
				refCount: 0,
			}
			releaseMutexes[repoKey] = mutex
			logger.Debug("Created new repository mutex for release creation", map[string]interface{}{
				"repo_key": repoKey,
			})
			
			// Start cleanup worker if not already started
			if !cdnCleanupStarted {
				cdnCleanupStarted = true
				go cleanupCDNMutexes()
			}
		}
		releaseMutexesMu.Unlock()
	}

	// Increment reference count
	releaseMutexesMu.Lock()
	mutex.refCount++
	mutex.lastUsed = time.Now()
	releaseMutexesMu.Unlock()

	return mutex
}

// getContentType returns the appropriate content type for a file
func (p *APIBasedProvider) getContentType(filename string) string {
	// Simple content type detection based on file extension
	filename = strings.ToLower(filename)
	
	if strings.HasSuffix(filename, ".jpg") || strings.HasSuffix(filename, ".jpeg") {
		return "image/jpeg"
	} else if strings.HasSuffix(filename, ".png") {
		return "image/png"
	} else if strings.HasSuffix(filename, ".gif") {
		return "image/gif"
	} else if strings.HasSuffix(filename, ".webp") {
		return "image/webp"
	} else if strings.HasSuffix(filename, ".pdf") {
		return "application/pdf"
	} else if strings.HasSuffix(filename, ".txt") {
		return "text/plain"
	} else {
		return "application/octet-stream"
	}
}

// cleanupCDNMutexes periodically cleans up unused CDN mutexes to prevent memory leaks
func cleanupCDNMutexes() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("CDN mutex cleanup worker panic recovered", map[string]interface{}{
				"panic": r,
			})
			// Restart the cleanup worker
			go cleanupCDNMutexes()
		}
	}()
	
	ticker := time.NewTicker(5 * time.Minute) // Clean up every 5 minutes
	defer ticker.Stop()
	
	for range ticker.C {
		releaseMutexesMu.Lock()
		
		cutoff := time.Now().Add(-10 * time.Minute) // Remove mutexes unused for 10 minutes
		var toDelete []string
		
		for repoKey, mutex := range releaseMutexes {
			if mutex.refCount <= 0 && mutex.lastUsed.Before(cutoff) {
				toDelete = append(toDelete, repoKey)
			}
		}
		
		for _, repoKey := range toDelete {
			delete(releaseMutexes, repoKey)
			logger.Debug("Cleaned up unused CDN mutex", map[string]interface{}{
				"repo_key": repoKey,
			})
		}
		
		if len(toDelete) > 0 {
			logger.Debug("CDN mutex cleanup completed", map[string]interface{}{
				"cleaned_mutexes":   len(toDelete),
				"remaining_mutexes": len(releaseMutexes),
			})
		}
		
		releaseMutexesMu.Unlock()
	}
}