package github

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/msg2git/msg2git/internal/logger"
)

// GitHub Issues API structures
type apiIssueRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type apiIssueResponse struct {
	ID      int    `json:"id"`
	Number  int    `json:"number"`
	Title   string `json:"title"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
}

type apiCommentRequest struct {
	Body string `json:"body"`
}

type apiCommentResponse struct {
	ID      int    `json:"id"`
	HTMLURL string `json:"html_url"`
}

// IssueManager implementation for API provider
func (p *APIBasedProvider) CreateIssue(title, body string) (string, int, error) {
	endpoint := fmt.Sprintf("/repos/%s/%s/issues", p.repoOwner, p.repoName)
	
	issueRequest := apiIssueRequest{
		Title: title,
		Body:  body,
	}

	resp, err := p.makeAPIRequest("POST", endpoint, issueRequest)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create issue: %w", err)
	}
	defer resp.Body.Close()

	var issueResponse apiIssueResponse
	if err := json.NewDecoder(resp.Body).Decode(&issueResponse); err != nil {
		return "", 0, fmt.Errorf("failed to decode issue response: %w", err)
	}

	logger.Info("Issue created via API", map[string]interface{}{
		"issue_number": issueResponse.Number,
		"issue_title":  issueResponse.Title,
		"issue_url":    issueResponse.HTMLURL,
		"user_id":      p.config.UserID,
	})

	return issueResponse.HTMLURL, issueResponse.Number, nil
}

func (p *APIBasedProvider) GetIssueStatus(issueNumber int) (*IssueStatus, error) {
	endpoint := fmt.Sprintf("/repos/%s/%s/issues/%d", p.repoOwner, p.repoName, issueNumber)
	
	resp, err := p.makeAPIRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get issue status: %w", err)
	}
	defer resp.Body.Close()

	var issueResponse apiIssueResponse
	if err := json.NewDecoder(resp.Body).Decode(&issueResponse); err != nil {
		return nil, fmt.Errorf("failed to decode issue response: %w", err)
	}

	status := &IssueStatus{
		Number:  issueResponse.Number,
		Title:   issueResponse.Title,
		State:   issueResponse.State,
		HTMLURL: issueResponse.HTMLURL,
	}

	return status, nil
}

func (p *APIBasedProvider) SyncIssueStatuses(issueNumbers []int) (map[int]*IssueStatus, error) {
	if len(issueNumbers) == 0 {
		return make(map[int]*IssueStatus), nil
	}

	logger.Debug("Syncing issue statuses via API", map[string]interface{}{
		"issue_count": len(issueNumbers),
		"user_id":     p.config.UserID,
	})

	// For API provider, we can use GraphQL for efficient batch queries
	// For now, we'll implement individual queries (can be optimized later)
	statuses := make(map[int]*IssueStatus)

	for _, number := range issueNumbers {
		status, err := p.GetIssueStatus(number)
		if err != nil {
			logger.Error("Failed to get issue status", map[string]interface{}{
				"issue_number": number,
				"error":        err.Error(),
				"user_id":      p.config.UserID,
			})
			continue // Skip failed issues, don't fail the entire operation
		}
		statuses[number] = status
	}

	logger.Info("Issue statuses synced via API", map[string]interface{}{
		"synced_count": len(statuses),
		"total_count":  len(issueNumbers),
		"user_id":      p.config.UserID,
	})

	return statuses, nil
}

func (p *APIBasedProvider) AddIssueComment(issueNumber int, commentText string) (string, error) {
	endpoint := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", p.repoOwner, p.repoName, issueNumber)
	
	commentRequest := apiCommentRequest{
		Body: commentText,
	}

	resp, err := p.makeAPIRequest("POST", endpoint, commentRequest)
	if err != nil {
		return "", fmt.Errorf("failed to add issue comment: %w", err)
	}
	defer resp.Body.Close()

	var commentResponse apiCommentResponse
	if err := json.NewDecoder(resp.Body).Decode(&commentResponse); err != nil {
		return "", fmt.Errorf("failed to decode comment response: %w", err)
	}

	logger.Info("Issue comment added via API", map[string]interface{}{
		"issue_number": issueNumber,
		"comment_id":   commentResponse.ID,
		"comment_url":  commentResponse.HTMLURL,
		"user_id":      p.config.UserID,
	})

	return commentResponse.HTMLURL, nil
}

func (p *APIBasedProvider) CloseIssue(issueNumber int) error {
	endpoint := fmt.Sprintf("/repos/%s/%s/issues/%d", p.repoOwner, p.repoName, issueNumber)
	
	updateRequest := map[string]string{
		"state": "closed",
	}

	resp, err := p.makeAPIRequest("PATCH", endpoint, updateRequest)
	if err != nil {
		return fmt.Errorf("failed to close issue: %w", err)
	}
	defer resp.Body.Close()

	// Read response to ensure it worked
	var issueResponse apiIssueResponse
	if err := json.NewDecoder(resp.Body).Decode(&issueResponse); err != nil {
		return fmt.Errorf("failed to decode close issue response: %w", err)
	}

	if issueResponse.State != "closed" {
		return fmt.Errorf("issue was not closed successfully, current state: %s", issueResponse.State)
	}

	logger.Info("Issue closed via API", map[string]interface{}{
		"issue_number": issueNumber,
		"issue_url":    issueResponse.HTMLURL,
		"user_id":      p.config.UserID,
	})

	return nil
}

// Enhanced SyncIssueStatuses using GraphQL for better performance
func (p *APIBasedProvider) SyncIssueStatusesGraphQL(issueNumbers []int) (map[int]*IssueStatus, error) {
	if len(issueNumbers) == 0 {
		return make(map[int]*IssueStatus), nil
	}

	// Build GraphQL query for batch issue fetching
	var queryParts []string
	for i, number := range issueNumbers {
		queryPart := fmt.Sprintf(`
			issue%d: issue(number: %d) {
				number
				title
				state
				url
			}`, i, number)
		queryParts = append(queryParts, queryPart)
	}

	graphqlQuery := fmt.Sprintf(`{
		repository(owner: "%s", name: "%s") {
			%s
		}
	}`, p.repoOwner, p.repoName, strings.Join(queryParts, ""))

	// Make GraphQL request
	requestBody := map[string]string{
		"query": graphqlQuery,
	}

	resp, err := p.makeAPIRequest("POST", "/graphql", requestBody)
	if err != nil {
		return nil, fmt.Errorf("GraphQL query failed: %w", err)
	}
	defer resp.Body.Close()

	// Parse GraphQL response
	var graphqlResponse struct {
		Data struct {
			Repository map[string]struct {
				Number int    `json:"number"`
				Title  string `json:"title"`
				State  string `json:"state"`
				URL    string `json:"url"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&graphqlResponse); err != nil {
		return nil, fmt.Errorf("failed to decode GraphQL response: %w", err)
	}

	if len(graphqlResponse.Errors) > 0 {
		return nil, fmt.Errorf("GraphQL errors: %v", graphqlResponse.Errors)
	}

	// Convert to IssueStatus format
	statuses := make(map[int]*IssueStatus)
	for _, issue := range graphqlResponse.Data.Repository {
		if issue.Number > 0 { // Valid issue
			status := &IssueStatus{
				Number:  issue.Number,
				Title:   issue.Title,
				State:   strings.ToLower(issue.State),
				HTMLURL: issue.URL,
			}
			statuses[issue.Number] = status
		}
	}

	logger.Info("Issue statuses synced via GraphQL", map[string]interface{}{
		"synced_count": len(statuses),
		"total_count":  len(issueNumbers),
		"user_id":      p.config.UserID,
	})

	return statuses, nil
}