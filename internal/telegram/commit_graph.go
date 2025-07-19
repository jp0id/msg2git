package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// GitHubCommit represents a commit from GitHub API
type GitHubCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Author struct {
			Date string `json:"date"`
		} `json:"author"`
		Message string `json:"message"`
	} `json:"commit"`
}

// CommitActivity represents daily commit count
type CommitActivity struct {
	Date  time.Time
	Count int
}

// generateCommitGraph fetches commit data and generates a compact activity graph
func (b *Bot) generateCommitGraph(chatID int64) (string, error) {
	if b.db == nil {
		return "", fmt.Errorf("database not configured")
	}

	// Get user's GitHub token from database
	user, err := b.db.GetUserByChatID(chatID)
	if err != nil {
		return "", fmt.Errorf("failed to get user: %w", err)
	}

	if user.GitHubToken == "" {
		return "", fmt.Errorf("GitHub token not configured")
	}

	// Parse repository URL to get owner/repo
	owner, repo, err := b.parseGitHubRepoURL(user.GitHubRepo)
	if err != nil {
		return "", fmt.Errorf("invalid repository URL: %w", err)
	}

	// Create cache key based on user repository to avoid conflicts between users
	cacheKey := fmt.Sprintf("commit_graph_%d_%s_%s", chatID, owner, repo)

	// Try to get commit graph from cache first
	if cachedGraph, found := b.cache.Get(cacheKey); found {
		return cachedGraph.(string), nil
	}

	// Fetch commits from GitHub API
	commits, err := b.fetchCommitsForGraph(owner, repo, user.GitHubToken)
	if err != nil {
		// Handle rate limiting gracefully
		if strings.Contains(err.Error(), "rate limit exceeded") {
			// Cache rate-limited response for shorter time (5 minutes)
			rateLimitedGraph := b.generateRateLimitedCommitGraph()
			b.cache.SetWithExpiry(cacheKey, rateLimitedGraph, 5*time.Minute)
			return rateLimitedGraph, nil
		}
		return "", err
	}

	// Process commits into daily activity
	activities := b.processCommitsForGraph(commits)

	// Generate the compact activity graph
	commitGraph := b.formatCommitGraph(activities, len(commits))

	// Cache the commit graph for 30 minutes
	b.cache.SetWithExpiry(cacheKey, commitGraph, 30*time.Minute)

	return commitGraph, nil
}

// parseGitHubRepoURL extracts owner and repo name from GitHub URL
func (b *Bot) parseGitHubRepoURL(url string) (string, string, error) {
	// Remove trailing .git if present
	url = strings.TrimSuffix(url, ".git")

	// Handle different URL formats
	if strings.HasPrefix(url, "https://github.com/") {
		parts := strings.Split(strings.TrimPrefix(url, "https://github.com/"), "/")
		if len(parts) >= 2 {
			return parts[0], parts[1], nil
		}
	} else if strings.HasPrefix(url, "git@github.com:") {
		parts := strings.Split(strings.TrimPrefix(url, "git@github.com:"), "/")
		if len(parts) >= 2 {
			return parts[0], parts[1], nil
		}
	}

	return "", "", fmt.Errorf("invalid GitHub URL format")
}

// fetchCommitsForGraph retrieves commits from GitHub API with smart pagination
func (b *Bot) fetchCommitsForGraph(owner, repo, token string) ([]GitHubCommit, error) {
	// Calculate date 30 days ago
	since := time.Now().AddDate(0, 0, -30).Format(time.RFC3339)

	var allCommits []GitHubCommit
	page := 1
	maxPages := 5 // Limit to 5 pages (500 commits max) to balance completeness vs API usage

	for page <= maxPages {
		// GitHub API URL for commits with pagination
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits?since=%s&per_page=100&page=%d",
			owner, repo, since, page)

		commits, hasMore, err := b.fetchCommitsPageForGraph(url, token, since)
		if err != nil {
			return nil, err
		}

		allCommits = append(allCommits, commits...)

		// Stop if we got less than 100 commits (last page) or no commits before our date range
		if !hasMore {
			break
		}

		page++
	}

	return allCommits, nil
}

// fetchCommitsPageForGraph fetches a single page of commits
func (b *Bot) fetchCommitsPageForGraph(url, token, since string) ([]GitHubCommit, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, false, fmt.Errorf("creating request: %w", err)
	}

	// Add required headers for GitHub API
	req.Header.Set("User-Agent", "msg2git-commit-graph")
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return nil, false, fmt.Errorf("authentication failed. Please check your GitHub token with /repo")
	} else if resp.StatusCode == 403 {
		// Check if it's rate limiting
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)
		if strings.Contains(bodyStr, "rate limit exceeded") {
			return nil, false, fmt.Errorf("GitHub API rate limit exceeded. Please try again later")
		}
		return nil, false, fmt.Errorf("access forbidden. Check your token permissions")
	} else if resp.StatusCode == 404 {
		return nil, false, fmt.Errorf("repository not found. Check your repository URL with /repo")
	} else if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, false, fmt.Errorf("GitHub API error (status %d): %s", resp.StatusCode, string(body))
	}

	var commits []GitHubCommit
	if err := json.NewDecoder(resp.Body).Decode(&commits); err != nil {
		return nil, false, fmt.Errorf("decoding response: %w", err)
	}

	// Check if we should continue paginating
	hasMore := len(commits) == 100 // Full page suggests more commits available

	// Also check if the last commit is still within our date range
	if len(commits) > 0 {
		lastCommitTime, err := time.Parse(time.RFC3339, commits[len(commits)-1].Commit.Author.Date)
		if err == nil {
			sinceTime, _ := time.Parse(time.RFC3339, since)
			// If the last commit is before our since date, no need to fetch more
			if lastCommitTime.Before(sinceTime) {
				hasMore = false
				// Filter out commits before our date range
				var filteredCommits []GitHubCommit
				for _, commit := range commits {
					commitTime, err := time.Parse(time.RFC3339, commit.Commit.Author.Date)
					if err != nil || !commitTime.Before(sinceTime) {
						filteredCommits = append(filteredCommits, commit)
					}
				}
				commits = filteredCommits
			}
		}
	}

	return commits, hasMore, nil
}

// processCommitsForGraph converts commits into daily activity counts
func (b *Bot) processCommitsForGraph(commits []GitHubCommit) []CommitActivity {
	// Create a map to count commits per day
	dailyCounts := make(map[string]int)

	// Process each commit
	for _, commit := range commits {
		// Parse commit date
		commitTime, err := time.Parse(time.RFC3339, commit.Commit.Author.Date)
		if err != nil {
			continue
		}

		// Use date only (no time) as key
		dateKey := commitTime.Format("2006-01-02")
		dailyCounts[dateKey]++
	}

	// Create slice of activities for the last 30 days
	var activities []CommitActivity
	now := time.Now()

	for i := 29; i >= 0; i-- {
		date := now.AddDate(0, 0, -i)
		dateKey := date.Format("2006-01-02")
		count := dailyCounts[dateKey]

		activities = append(activities, CommitActivity{
			Date:  date,
			Count: count,
		})
	}

	return activities
}

// formatCommitGraph generates the compact commit activity graph string
func (b *Bot) formatCommitGraph(activities []CommitActivity, fetchedCommits int) string {
	// Calculate total commits and max commits
	totalCommits := 0
	maxCommits := 0
	for _, activity := range activities {
		totalCommits += activity.Count
		if activity.Count > maxCommits {
			maxCommits = activity.Count
		}
	}

	var result strings.Builder

	// Add summary
	result.WriteString(fmt.Sprintf("📊 <b>30-Day Commit Activity</b>\n"))
	if fetchedCommits >= 500 {
		result.WriteString(fmt.Sprintf("Total commits: 500+\n"))
	} else {
		result.WriteString(fmt.Sprintf("Total commits: %d\n", totalCommits))
	}
	result.WriteString(fmt.Sprintf("Max commits per day: %d\n\n", maxCommits))

	// Generate the compact graph in 3 rows of 10 emojis each
	rowSize := 10

	for row := 0; row < 3; row++ {
		for i := 0; i < rowSize; i++ {
			dayIndex := row*rowSize + i
			if dayIndex < len(activities) {
				count := activities[dayIndex].Count
				symbol := b.getActivitySymbol(count)
				result.WriteString(symbol)
			}
		}
		result.WriteString("\n")
	}

	return result.String()
}

// generateRateLimitedCommitGraph returns a rate-limited graph display
func (b *Bot) generateRateLimitedCommitGraph() string {
	var result strings.Builder

	result.WriteString("📊 <b>30-Day Commit Activity</b>\n")
	result.WriteString("Total commits: N/A (rate limited)\n")
	result.WriteString("Max commits per day: N/A (rate limited)\n\n")

	// Display 3 rows of empty squares
	for row := 0; row < 3; row++ {
		for i := 0; i < 10; i++ {
			result.WriteString("⬜")
		}
		result.WriteString("\n")
	}

	result.WriteString("\n💡 <i>GitHub API rate limit reached. Try again later.</i>")

	return result.String()
}

// getActivitySymbol returns an emoji representing commit activity level
func (b *Bot) getActivitySymbol(count int) string {
	if count == 0 {
		return "⬜"
	} else if count <= 2 {
		return "🟩"
	} else if count <= 5 {
		return "🟨"
	} else if count <= 10 {
		return "🟧"
	} else {
		return "🟥"
	}
}

