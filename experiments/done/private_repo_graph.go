//go:build ignore
// +build ignore

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run private_repo_graph.go <github-repo-url>")
		fmt.Println("Example: go run private_repo_graph.go https://github.com/owner/repo")
		fmt.Println("")
		fmt.Println("Note: Loads GITHUB_TOKEN from .env file for experiments")
		fmt.Println("In live version, token will be read from database")
		os.Exit(1)
	}

	// Load GitHub token from .env file (for experiments)
	// TODO: In live version, read token from database for the specific user
	token := loadGitHubTokenFromEnv()
	if token == "" {
		fmt.Println("❌ Error: GITHUB_TOKEN not found in .env file")
		fmt.Println("Please add GITHUB_TOKEN=your_token to .env file")
		os.Exit(1)
	}

	repoURL := os.Args[1]
	
	// Parse repository owner and name from URL
	owner, repo, err := parseRepoURL(repoURL)
	if err != nil {
		fmt.Printf("Error parsing repository URL: %v\n", err)
		os.Exit(1)
	}

	// Fetch commits from GitHub API
	commits, err := fetchCommits(owner, repo, token)
	if err != nil {
		// Check if it's a rate limit error and provide friendly fallback
		if strings.Contains(err.Error(), "rate limit exceeded") {
			fmt.Printf("⚠️  %v\n\n", err)
			fmt.Println("Displaying empty activity graph (rate limited):")
			displayRateLimitedGraph()
			return
		}
		fmt.Printf("Error fetching commits: %v\n", err)
		os.Exit(1)
	}

	// Process commits into daily activity
	activities := processCommits(commits)
	
	// Generate and display the compact activity graph
	displayCompactGraph(activities, len(commits))
}

// loadGitHubTokenFromEnv loads GITHUB_TOKEN from .env file
// TODO: In live version, replace this with database lookup for user's token
func loadGitHubTokenFromEnv() string {
	// Try to read from .env file in parent directory (msg2git root)
	envPaths := []string{
		"../.env",     // From experiments folder
		".env",        // Current folder
		"../../.env",  // In case we're deeper
	}
	
	for _, envPath := range envPaths {
		if token := readTokenFromFile(envPath); token != "" {
			return token
		}
	}
	
	// Fallback to environment variable
	return os.Getenv("GITHUB_TOKEN")
}

// readTokenFromFile reads GITHUB_TOKEN from a specific .env file
func readTokenFromFile(filePath string) string {
	file, err := os.Open(filePath)
	if err != nil {
		return ""
	}
	defer file.Close()
	
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		
		// Skip comments and empty lines
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		
		// Look for GITHUB_TOKEN=value
		if strings.HasPrefix(line, "GITHUB_TOKEN=") {
			token := strings.TrimPrefix(line, "GITHUB_TOKEN=")
			// Remove quotes if present
			token = strings.Trim(token, `"'`)
			return token
		}
	}
	
	return ""
}

// parseRepoURL extracts owner and repo name from GitHub URL
func parseRepoURL(url string) (string, string, error) {
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
	} else if strings.Contains(url, "/") {
		// Assume it's in owner/repo format
		parts := strings.Split(url, "/")
		if len(parts) >= 2 {
			return parts[0], parts[1], nil
		}
	}
	
	return "", "", fmt.Errorf("invalid repository URL format. Use: https://github.com/owner/repo or owner/repo")
}

// fetchCommits retrieves commits from GitHub API with smart pagination
func fetchCommits(owner, repo, token string) ([]GitHubCommit, error) {
	// Calculate date 30 days ago
	since := time.Now().AddDate(0, 0, -30).Format(time.RFC3339)
	
	var allCommits []GitHubCommit
	page := 1
	maxPages := 5 // Limit to 5 pages (500 commits max) to balance completeness vs API usage
	
	for page <= maxPages {
		// GitHub API URL for commits with pagination
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits?since=%s&per_page=100&page=%d", 
			owner, repo, since, page)
		
		commits, hasMore, err := fetchCommitsPage(url, token, since)
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

// fetchCommitsPage fetches a single page of commits
func fetchCommitsPage(url, token, since string) ([]GitHubCommit, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, false, fmt.Errorf("creating request: %w", err)
	}
	
	// Add required headers for GitHub API
	req.Header.Set("User-Agent", "msg2git-private-repo-graph-tool")
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == 401 {
		return nil, false, fmt.Errorf("authentication failed. Please check your GITHUB_TOKEN")
	} else if resp.StatusCode == 403 {
		// Check if it's rate limiting
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)
		if strings.Contains(bodyStr, "rate limit exceeded") {
			return nil, false, fmt.Errorf("GitHub API rate limit exceeded. Please try again later or use a GitHub token for higher limits")
		}
		return nil, false, fmt.Errorf("access forbidden. Check your token permissions: %s", bodyStr)
	} else if resp.StatusCode == 404 {
		return nil, false, fmt.Errorf("repository not found. Check the URL and your access permissions")
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

// processCommits converts commits into daily activity counts
func processCommits(commits []GitHubCommit) []CommitActivity {
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

// displayCompactGraph renders the compact commit activity graph
func displayCompactGraph(activities []CommitActivity, fetchedCommits int) {
	// Calculate total commits and max commits
	totalCommits := 0
	maxCommits := 0
	for _, activity := range activities {
		totalCommits += activity.Count
		if activity.Count > maxCommits {
			maxCommits = activity.Count
		}
	}
	
	fmt.Printf("Total commits: %d", totalCommits)
	if fetchedCommits >= 500 { // maxPages * 100
		fmt.Printf(" (showing 500+ commits)")
	} else if fetchedCommits >= 100 {
		fmt.Printf(" (fetched %d commits)", fetchedCommits)
	}
	fmt.Println()
	fmt.Printf("Max commits per day: %d\n\n", maxCommits)
	
	// Display the compact graph in 3 rows of 10 emojis each
	rowSize := 10
	
	for row := 0; row < 3; row++ {
		for i := 0; i < rowSize; i++ {
			dayIndex := row*rowSize + i
			if dayIndex < len(activities) {
				count := activities[dayIndex].Count
				symbol := getActivitySymbol(count, maxCommits)
				fmt.Print(symbol)
			}
		}
		fmt.Println()
	}
}

// displayRateLimitedGraph shows an empty graph when rate limited
func displayRateLimitedGraph() {
	fmt.Printf("Total commits: N/A (rate limited)\n")
	fmt.Printf("Max commits per day: N/A (rate limited)\n\n")
	
	// Display 3 rows of empty squares
	for row := 0; row < 3; row++ {
		for i := 0; i < 10; i++ {
			fmt.Print("⬜")
		}
		fmt.Println()
	}
	
	fmt.Println("\n💡 Tip: Wait a few minutes and try again, or use a GitHub token for higher rate limits")
}

// getActivitySymbol returns an emoji representing commit activity level
func getActivitySymbol(count, maxCommits int) string {
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