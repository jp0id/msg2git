//go:build ignore
// +build ignore

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
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
		fmt.Println("Usage: go run commit_graph.go <github-repo-url>")
		fmt.Println("Example: go run commit_graph.go https://github.com/owner/repo")
		os.Exit(1)
	}

	repoURL := os.Args[1]
	
	// Parse repository owner and name from URL
	owner, repo, err := parseRepoURL(repoURL)
	if err != nil {
		fmt.Printf("Error parsing repository URL: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("📊 Fetching commit activity for %s/%s (last 30 days)\n\n", owner, repo)

	// Fetch commits from GitHub API
	commits, err := fetchCommits(owner, repo)
	if err != nil {
		fmt.Printf("Error fetching commits: %v\n", err)
		os.Exit(1)
	}

	// Process commits into daily activity
	activities := processCommits(commits)
	
	// Generate and display the activity graph
	displayActivityGraph(activities)
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

// fetchCommits retrieves commits from GitHub API
func fetchCommits(owner, repo string) ([]GitHubCommit, error) {
	// Calculate date 30 days ago
	since := time.Now().AddDate(0, 0, -30).Format(time.RFC3339)
	
	// GitHub API URL for commits
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits?since=%s&per_page=100", owner, repo, since)
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	
	// Add User-Agent to avoid rate limiting
	req.Header.Set("User-Agent", "msg2git-commit-graph-tool")
	
	// Add GitHub token if available in environment
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error (status %d): %s", resp.StatusCode, string(body))
	}
	
	var commits []GitHubCommit
	if err := json.NewDecoder(resp.Body).Decode(&commits); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	
	return commits, nil
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

// displayActivityGraph renders the commit activity graph
func displayActivityGraph(activities []CommitActivity) {
	// Calculate total commits
	totalCommits := 0
	maxCommits := 0
	for _, activity := range activities {
		totalCommits += activity.Count
		if activity.Count > maxCommits {
			maxCommits = activity.Count
		}
	}
	
	fmt.Printf("Total commits: %d\n", totalCommits)
	fmt.Printf("Max commits per day: %d\n\n", maxCommits)
	
	// Display the graph in 3 rows of 10 days each
	rowSize := 10
	
	for row := 0; row < 3; row++ {
		// Print dates
		fmt.Print("    ")
		for i := 0; i < rowSize; i++ {
			dayIndex := row*rowSize + i
			if dayIndex < len(activities) {
				date := activities[dayIndex].Date
				fmt.Printf("%-5s", date.Format("01/02"))
			}
		}
		fmt.Println()
		
		// Print activity blocks
		fmt.Print("    ")
		for i := 0; i < rowSize; i++ {
			dayIndex := row*rowSize + i
			if dayIndex < len(activities) {
				count := activities[dayIndex].Count
				symbol := getActivitySymbol(count, maxCommits)
				fmt.Printf("%-5s", symbol)
			}
		}
		fmt.Println()
		
		// Print commit counts
		fmt.Print("    ")
		for i := 0; i < rowSize; i++ {
			dayIndex := row*rowSize + i
			if dayIndex < len(activities) {
				count := activities[dayIndex].Count
				if count > 0 {
					fmt.Printf("%-5d", count)
				} else {
					fmt.Printf("%-5s", "-")
				}
			}
		}
		fmt.Println("\n")
	}
	
	// Display legend
	fmt.Println("Legend:")
	fmt.Println("  ⬜ 0 commits")
	fmt.Println("  🟩 1-2 commits")
	fmt.Println("  🟨 3-5 commits")
	fmt.Println("  🟧 6-10 commits")
	fmt.Println("  🟥 11+ commits")
	
	// Display recent activity summary
	fmt.Println("\nRecent Activity:")
	recentDays := activities[len(activities)-7:] // Last 7 days
	sort.Slice(recentDays, func(i, j int) bool {
		return recentDays[i].Date.After(recentDays[j].Date)
	})
	
	for _, activity := range recentDays {
		if activity.Count > 0 {
			fmt.Printf("  %s: %d commit(s)\n", 
				activity.Date.Format("Mon Jan 02"), activity.Count)
		}
	}
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