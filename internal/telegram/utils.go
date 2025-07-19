package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/msg2git/msg2git/internal/consts"
	"github.com/msg2git/msg2git/internal/database"
	"github.com/msg2git/msg2git/internal/github"
	"github.com/msg2git/msg2git/internal/llm"
	"github.com/msg2git/msg2git/internal/logger"
)

// TodoItem represents a parsed TODO item
type TodoItem struct {
	MessageID int
	ChatID    int64
	Content   string
	Date      string
	Done      bool
}

// Message formatting utilities

// formatTokenCount formats large token numbers with M suffix for millions
// Examples: 99999 -> 99999, 150000 -> 0.15M, 1340000 -> 1.34M, 12340000 -> 12.3M, 123400000 -> 123M
func formatTokenCount(count int64) string {
	if count >= 100000 { // 0.1M threshold
		millions := float64(count) / 1000000.0
		if millions >= 100 {
			return fmt.Sprintf("%.0fM", millions)
		} else if millions >= 10 {
			return fmt.Sprintf("%.1fM", millions)
		} else {
			return fmt.Sprintf("%.2fM", millions)
		}
	}
	return fmt.Sprintf("%d", count)
}

func (b *Bot) generateTitleFromContent(content string) string {
	// Get the first line of content
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return "untitled"
	}

	firstLine := strings.TrimSpace(lines[0])
	if firstLine == "" {
		return "untitled"
	}

	// Limit to 50 characters
	if len(firstLine) > 50 {
		// Try to cut at a word boundary near 50 chars
		if cutIndex := strings.LastIndex(firstLine[:50], " "); cutIndex > 20 {
			return firstLine[:cutIndex] + "..."
		}
		// If no good word boundary, just cut at 47 chars and add "..."
		return firstLine[:47] + "..."
	}

	return firstLine
}

func (b *Bot) addMarkdownLineBreaks(content string) string {
	// Split content into lines
	lines := strings.Split(content, "\n")

	// Add two spaces at the end of each line (except the last one if it's empty)
	for i, line := range lines {
		// Don't add spaces to empty lines or the last line if it's empty
		if line != "" || (i < len(lines)-1) {
			lines[i] = strings.TrimRight(line, " ") + "  "
		}
	}

	return strings.Join(lines, "\n")
}

func (b *Bot) parseTitleAndTags(llmResponse, content string) (title, tags string) {
	// Parse format: title|#tag1 #tag2
	parts := strings.SplitN(strings.TrimSpace(llmResponse), "|", 2)
	if len(parts) != 2 {
		logger.Warn("Invalid LLM response format", map[string]interface{}{
			"response": llmResponse,
		})
		return b.generateTitleFromContent(content), ""
	}

	title = strings.TrimSpace(parts[0])
	tags = strings.TrimSpace(parts[1])

	if title == "" {
		title = b.generateTitleFromContent(content)
	}

	return title, tags
}

func (b *Bot) formatMessageContentWithTitleAndTags(content, filename string, messageID int, chatID int64, title, tags string) string {
	timestamp := time.Now().Format("2006-01-02 15:04")

	// Clean up tags and ensure proper format
	cleanTags := ""
	if tags != "" {
		cleanTags = strings.TrimSpace(tags)
	}

	// Improve markdown formatting by adding two spaces at the end of each line
	// This creates proper line breaks in markdown rendering
	formattedContent := b.addMarkdownLineBreaks(content)

	// New format: HTML comment for metadata, title on separate line, tags on line below title
	var result strings.Builder

	// HTML comment with metadata
	result.WriteString("<!--\n")
	result.WriteString(fmt.Sprintf("[%d] [%d] [%s] \n", messageID, chatID, timestamp))
	result.WriteString("-->\n\n")

	// Title
	result.WriteString(fmt.Sprintf("## %s\n", title))

	// Tags on separate line (if any)
	if cleanTags != "" {
		result.WriteString(fmt.Sprintf("%s\n", cleanTags))
	}
	result.WriteString("\n")

	// Content
	result.WriteString(formattedContent)
	result.WriteString("\n\n---\n\n")

	return result.String()
}

func (b *Bot) formatTodoContent(content string, messageID int, chatID int64) string {
	timestamp := time.Now().Format("2006-01-02")

	// New TODO format: - [ ] <!--[msg_id] [chat_id]--> message (timestamp)
	// Check if content contains line breaks
	if strings.Contains(content, "\n") {
		// If content has line breaks, it cannot be saved to TODO.md
		logger.Debug("Content contains line breaks, cannot save to TODO.md", nil)
		return ""
	}

	return fmt.Sprintf("- [ ] <!--[%d] [%d]--> %s (%s)\n", messageID, chatID, content, timestamp)
}

// Parsing utilities

func (b *Bot) parseTodoItems(content string) []TodoItem {
	var todos []TodoItem
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var done bool
		if strings.HasPrefix(line, "- [ ]") {
			done = false
		} else if strings.HasPrefix(line, "- [x]") {
			done = true
		} else {
			continue
		}

		// Try new HTML comment format first: - [ ] <!--[msg_id] [chat_id]--> message (date)
		htmlCommentRe := regexp.MustCompile(`^- \[[ x]\] <!--\[(\d+)\] \[(\d+)\]--> (.+) \(([^)]+)\)$`)
		matches := htmlCommentRe.FindStringSubmatch(line)
		if len(matches) == 5 {
			if msgID, err := strconv.Atoi(matches[1]); err == nil {
				if chatID, err := strconv.ParseInt(matches[2], 10, 64); err == nil {
					todos = append(todos, TodoItem{
						MessageID: msgID,
						ChatID:    chatID,
						Content:   matches[3],
						Date:      matches[4],
						Done:      done,
					})
					continue
				}
			}
		}

		// Try old square bracket format for backward compatibility: - [ ] [msg_id] [chat_id] message (date)
		oldNewFormatRe := regexp.MustCompile(`^- \[[ x]\] \[(\d+)\] \[(\d+)\] (.+) \(([^)]+)\)$`)
		matches = oldNewFormatRe.FindStringSubmatch(line)
		if len(matches) == 5 {
			if msgID, err := strconv.Atoi(matches[1]); err == nil {
				if chatID, err := strconv.ParseInt(matches[2], 10, 64); err == nil {
					todos = append(todos, TodoItem{
						MessageID: msgID,
						ChatID:    chatID,
						Content:   matches[3],
						Date:      matches[4],
						Done:      done,
					})
					continue
				}
			}
		}

		// Fall back to oldest format for backward compatibility: - [ ] [msg_id] message (date)
		oldFormatRe := regexp.MustCompile(`^- \[[ x]\] \[(\d+)\] (.+) \(([^)]+)\)$`)
		matches = oldFormatRe.FindStringSubmatch(line)
		if len(matches) == 4 {
			if msgID, err := strconv.Atoi(matches[1]); err == nil {
				todos = append(todos, TodoItem{
					MessageID: msgID,
					ChatID:    0, // Default to 0 for old format
					Content:   matches[2],
					Date:      matches[3],
					Done:      done,
				})
			}
		}
	}

	return todos
}

// parseMessageMetadata extracts metadata from HTML comment block in markdown files
func (b *Bot) parseMessageMetadata(content string) (messageID int, chatID int64, timestamp string, err error) {
	// Look for HTML comment block at the start of content
	lines := strings.Split(content, "\n")

	// Find the comment block
	var commentLines []string
	inComment := false

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "<!--" {
			inComment = true
			continue
		}
		if line == "-->" {
			break
		}
		if inComment {
			commentLines = append(commentLines, line)
		}
		// Stop searching after first few lines if no comment found
		if i > 5 && !inComment {
			break
		}
	}

	// Parse the metadata line: [msg_id] [chat_id] [timestamp]
	for _, commentLine := range commentLines {
		metadataRe := regexp.MustCompile(`^\[(\d+)\] \[(\d+)\] \[([^\]]+)\]`)
		matches := metadataRe.FindStringSubmatch(commentLine)
		if len(matches) == 4 {
			if msgID, parseErr := strconv.Atoi(matches[1]); parseErr == nil {
				if chID, parseErr := strconv.ParseInt(matches[2], 10, 64); parseErr == nil {
					return msgID, chID, matches[3], nil
				}
			}
		}
	}

	return 0, 0, "", fmt.Errorf("no metadata found in HTML comment")
}

func (b *Bot) getUndoneTodos(todos []TodoItem, chatID int64, offset, limit int) []TodoItem {
	var undone []TodoItem
	for _, todo := range todos {
		// Filter by chat ID and undone status
		// Include items with ChatID 0 (old format) for backward compatibility
		if !todo.Done && (todo.ChatID == chatID || todo.ChatID == 0) {
			undone = append(undone, todo)
		}
	}

	// Return slice based on offset and limit
	start := offset
	if start >= len(undone) {
		return []TodoItem{}
	}

	end := start + limit
	if end > len(undone) {
		end = len(undone)
	}

	return undone[start:end]
}

func (b *Bot) parseIssueNumbers(content string) []int {
	var numbers []int

	// Multiple regex patterns to handle various manual formats
	patterns := []string{
		// Standard format: user/repo#123 [title] or user/repo#123[title]
		`[^/\s]+/[^/\s]+#(\d+)`,
		// Simple #123 format (fallback for manual entries)
		`#(\d+)`,
		// GitHub URL format: https://github.com/user/repo/issues/123
		`github\.com/[^/]+/[^/]+/issues/(\d+)`,
	}

	// Use a map to avoid duplicates
	uniqueNumbers := make(map[int]bool)

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindAllStringSubmatch(content, -1)

		for _, match := range matches {
			if len(match) > 1 {
				if num, err := strconv.Atoi(match[1]); err == nil {
					uniqueNumbers[num] = true
				}
			}
		}
	}

	// Convert map to slice
	for num := range uniqueNumbers {
		numbers = append(numbers, num)
	}

	logger.Debug("Parsed issue numbers from content", map[string]interface{}{
		"numbers": numbers,
	})
	return numbers
}

// parseIssueStatusesFromContent parses issue numbers and their states directly from issue.md content
func (b *Bot) parseIssueStatusesFromContent(content string, githubProvider github.GitHubProvider) map[int]*github.IssueStatus {
	statuses := make(map[int]*github.IssueStatus)

	owner, repo, err := githubProvider.GetRepoInfo()
	if err != nil {
		logger.Warn("Failed to get repo info for parsing", map[string]interface{}{
			"error": err.Error(),
		})
		return statuses
	}

	// Parse lines that look like: - 🟢 owner/repo#123 [title] or - 🔴 owner/repo#456 [title]
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "- ") {
			continue
		}

		// Extract emoji, issue number, and title
		// Pattern: - 🟢 owner/repo#123 [title]
		re := regexp.MustCompile(`^- ([🟢🔴]) [^/\s]+/[^/\s]+#(\d+) \[([^\]]*)\]`)
		matches := re.FindStringSubmatch(line)

		if len(matches) == 4 {
			emoji := matches[1]
			numberStr := matches[2]
			title := matches[3]

			if number, err := strconv.Atoi(numberStr); err == nil {
				state := "closed"
				if emoji == "🟢" {
					state = "open"
				}

				// Construct URL
				url := fmt.Sprintf("https://github.com/%s/%s/issues/%d", owner, repo, number)

				statuses[number] = &github.IssueStatus{
					Number:  number,
					Title:   title,
					State:   state,
					HTMLURL: url,
				}
			}
		}
	}

	logger.Debug("Parsed issue statuses from content", map[string]interface{}{
		"count": len(statuses),
	})

	return statuses
}

func (b *Bot) generateIssueContent(statuses map[int]*github.IssueStatus, userGitHubProvider github.GitHubProvider) string {
	// Convert map to slice for sorting
	var issueList []*github.IssueStatus
	for _, status := range statuses {
		issueList = append(issueList, status)
	}

	// Sort issues: open issues first, then closed issues, both by number descending
	b.sortIssuesForArchiving(issueList)

	// Generate the content using the sorted issue list
	return b.generateIssueContentFromStatuses(issueList, userGitHubProvider)
}

// Telegram message formatting utilities

var needEscape = make(map[rune]struct{})

func init() {
	for _, r := range []rune{'_', '*', '[', ']', '(', ')', '~', '`', '>', '#', '+', '-', '=', '|', '{', '}', '.', '!'} {
		needEscape[r] = struct{}{}
	}
}

// https://github.com/go-telegram-bot-api/telegram-bot-api/issues/231#issuecomment-979332620
func (b *Bot) telegramToMarkdown(text string, messageEntities []tgbotapi.MessageEntity) string {
	if len(messageEntities) == 0 {
		return text
	}

	// Create a map to track which positions are inside code/pre entities
	codeRanges := make(map[int]bool)

	// First pass: identify all code and pre entity ranges
	for _, e := range messageEntities {
		if e.Type == "code" || e.Type == "pre" {
			utf16pos := 0
			runeIndex := 0
			input := []rune(text)

			// Find the actual rune positions for this entity
			for runeIndex < len(input) && utf16pos < e.Offset {
				utf16pos += len(utf16.Encode([]rune{input[runeIndex]}))
				runeIndex++
			}
			startRune := runeIndex

			for runeIndex < len(input) && utf16pos < e.Offset+e.Length {
				utf16pos += len(utf16.Encode([]rune{input[runeIndex]}))
				runeIndex++
			}
			endRune := runeIndex

			// Mark all positions in this range as code
			for i := startRune; i < endRune; i++ {
				codeRanges[i] = true
			}
		}
	}

	insertions := make(map[int]string)
	for _, e := range messageEntities {
		var before, after string
		switch e.Type {
		case "bold":
			before = "**"
			after = "**"
		case "italic":
			before = "*"
			after = "*"
		case "underline":
			before = "__"
			after = "__"
		case "strikethrough":
			before = "~~"
			after = "~~"
		case "code":
			before = "`"
			after = "`"
		case "pre":
			if e.Language != "" {
				before = "```" + e.Language + "\n"
			} else {
				before = "```\n"
			}
			after = "\n```"
		case "text_link":
			before = "["
			after = "](" + e.URL + ")"
		case "url":
			// URLs don't need special formatting, they're already markdown-compatible
			continue
		}
		if before != "" {
			insertions[e.Offset] += before
			insertions[e.Offset+e.Length] += after
		}
	}

	input := []rune(text)
	var output []rune
	utf16pos := 0

	for _, c := range input {
		output = append(output, []rune(insertions[utf16pos])...)

		// Only escape characters if we're NOT inside a code/pre block
		// if _, has := needEscape[c]; has && !codeRanges[runeIndex] {
		// 	output = append(output, '\\')
		// }
		output = append(output, c)
		utf16pos += len(utf16.Encode([]rune{c}))
	}
	output = append(output, []rune(insertions[utf16pos])...)
	return string(output)
}

// File selection and message handling

func (b *Bot) showFileSelectionButtons(message *tgbotapi.Message) error {
	logger.Debug("Showing file selection buttons", nil)

	// Convert Telegram message to markdown format
	markdownContent := b.telegramToMarkdown(message.Text, message.Entities)

	// Store the formatted message content AND original message ID for later use
	messageKey := fmt.Sprintf("%d_%d", message.Chat.ID, message.MessageID)
	messageData := fmt.Sprintf("%s|||DELIM|||%d", markdownContent, message.MessageID)
	b.pendingMessages[messageKey] = messageData

	// Get user's pinned custom files (first 2 items in custom_files array)
	var pinnedFiles []string
	if b.db != nil {
		user, err := b.db.GetUserByChatID(message.Chat.ID)
		if err == nil && user != nil {
			customFiles := user.GetCustomFiles()
			// Take up to 2 pinned files (first 2 items in the array)
			pinnedCount := len(customFiles)
			if pinnedCount > 2 {
				pinnedCount = 2
			}
			for i := 0; i < pinnedCount; i++ {
				pinnedFiles = append(pinnedFiles, customFiles[i])
			}
		}
	}

	// Create inline keyboard with file options
	row1 := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📝 NOTE", fmt.Sprintf("file_NOTE_%s", messageKey)),
		tgbotapi.NewInlineKeyboardButtonData("❓ ISSUE", fmt.Sprintf("file_ISSUE_%s", messageKey)),
	)
	if !strings.Contains(messageData, "\n") {
		row1 = append(row1, tgbotapi.NewInlineKeyboardButtonData("✅ TODO", fmt.Sprintf("file_TODO_%s", messageKey)))
	}
	row2 := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("💡 IDEA", fmt.Sprintf("file_IDEA_%s", messageKey)),
		tgbotapi.NewInlineKeyboardButtonData("📥 INBOX", fmt.Sprintf("file_INBOX_%s", messageKey)),
		tgbotapi.NewInlineKeyboardButtonData("🔧 TOOL", fmt.Sprintf("file_TOOL_%s", messageKey)),
	)

	// Add pinned custom files row if any exist
	var rows [][]tgbotapi.InlineKeyboardButton
	if len(pinnedFiles) > 0 {
		pinnedRow := []tgbotapi.InlineKeyboardButton{}
		for i, filePath := range pinnedFiles {
			// Get display name (remove .md extension and truncate if needed)
			displayName := strings.TrimSuffix(filePath, ".md")
			if len(displayName) > 15 {
				displayName = displayName[:12] + "..."
			}
			pinnedRow = append(pinnedRow, tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("📌 %s", displayName),
				fmt.Sprintf("file_PINNED_%d_%s", i, messageKey),
			))
		}
		rows = append(rows, pinnedRow)
	}
	rows = append(rows, row1, row2)

	// Final row with CUSTOM and CANCEL
	row3 := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📁 CUSTOM", fmt.Sprintf("file_CUSTOM_%s", messageKey)),
		tgbotapi.NewInlineKeyboardButtonData("❌ CANCEL", fmt.Sprintf("cancel_%s", messageKey)),
	)
	rows = append(rows, row3)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	msg := tgbotapi.NewMessage(message.Chat.ID, "Please choose a location:")
	msg.ReplyMarkup = keyboard

	if _, err := b.rateLimitedSend(message.Chat.ID, msg); err != nil {
		return fmt.Errorf("failed to send file selection message: %w", err)
	}

	return nil
}

// Configuration update methods

func (b *Bot) validateGitHubToken(token string) error {
	// Make a test API call to validate the token
	req, err := http.NewRequest("GET", "https://api.github.com/user", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make API call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	return nil
}

func (b *Bot) updateGitHubRepo(repoURL, username string, chatID int64) error {
	// Clean up old repository directories before updating
	if err := github.CleanupOldRepositories(repoURL); err != nil {
		logger.Warn("Failed to cleanup old repositories", map[string]interface{}{
			"error": err.Error(),
		})
		// Don't fail the entire operation if cleanup fails
	}

	// Update the config
	b.config.GitHubRepo = repoURL
	b.config.GitHubUsername = username

	// Get premium level for the user
	premiumLevel := b.getPremiumLevel(chatID)

	// Create new GitHub manager with updated config
	githubManager, err := github.NewManager(b.config, premiumLevel)
	if err != nil {
		return fmt.Errorf("failed to create new GitHub manager: %w", err)
	}

	b.githubManager = githubManager
	return nil
}

func (b *Bot) updateGitHubToken(token string, chatID int64) error {
	// Update the config
	b.config.GitHubToken = token

	// Clean up old repository directories (in case repo URL was also changed)
	if err := github.CleanupOldRepositories(b.config.GitHubRepo); err != nil {
		logger.Warn("Failed to cleanup old repositories", map[string]interface{}{
			"error": err.Error(),
		})
		// Don't fail the entire operation if cleanup fails
	}

	// Get premium level for the user
	premiumLevel := b.getPremiumLevel(chatID)

	// Create new GitHub manager with updated config
	githubManager, err := github.NewManager(b.config, premiumLevel)
	if err != nil {
		return fmt.Errorf("failed to create new GitHub manager: %w", err)
	}

	b.githubManager = githubManager
	return nil
}

func (b *Bot) validateLLMToken(provider, endpoint, token, model string) error {
	providerLower := strings.ToLower(provider)

	switch providerLower {
	case "deepseek":
		return b.validateDeepseekToken(endpoint, token, model)
	case "gemini":
		return b.validateGeminiToken(token, model)
	default:
		return fmt.Errorf("unsupported provider: %s", provider)
	}
}

func (b *Bot) validateDeepseekToken(endpoint, token, model string) error {
	// Deepseek uses OpenAI-compatible format
	reqBody := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": "test",
			},
		},
		"max_tokens": 10,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", endpoint+"/chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make deepseek API call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return fmt.Errorf("invalid deepseek API token")
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("deepseek API returned status %d", resp.StatusCode)
	}

	return nil
}

func (b *Bot) validateGeminiToken(token, model string) error {
	// Gemini uses Google AI format with API key in URL
	endpoint := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, token)

	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]string{
					{
						"text": "test",
					},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"maxOutputTokens": 10,
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal gemini request: %w", err)
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create gemini request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make gemini API call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 400 {
		return fmt.Errorf("invalid gemini API key or model")
	}
	if resp.StatusCode == 403 {
		return fmt.Errorf("gemini API access forbidden - check API key permissions")
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("gemini API returned status %d", resp.StatusCode)
	}

	return nil
}

func (b *Bot) updateLLMConfig(provider, endpoint, token, model string) error {
	// Update the config
	b.config.LLMProvider = provider
	b.config.LLMEndpoint = endpoint
	b.config.LLMToken = token
	b.config.LLMModel = model

	// Create new LLM client with updated config
	b.llmClient = llm.NewClient(b.config)
	return nil
}

// Helper method to check if GitHub configuration is valid
func (b *Bot) validateGitHubConfig() error {
	if b.config.GitHubToken == "" {
		return fmt.Errorf("GitHub token is not set. Use /repo to configure it")
	}
	if b.config.GitHubRepo == "" {
		return fmt.Errorf("GitHub repository is not set. Use /repo to configure it")
	}
	if b.config.GitHubUsername == "" {
		return fmt.Errorf("GitHub username is not set. Use /repo to configure it")
	}
	// Note: We don't validate repository existence here since we use lazy initialization
	// The repository will be cloned when first needed
	return nil
}

// Progress bar utilities

// createProgressBarWithText creates a visual progress bar with percentage
func createProgressBarWithText(percentage int, message string) string {
	const barLength = 10
	filled := (percentage * barLength) / 100
	if filled > barLength {
		filled = barLength
	}

	bar := ""
	for i := 0; i < barLength; i++ {
		if i < filled {
			bar += "▓"
		} else {
			bar += "░"
		}
	}

	return fmt.Sprintf("%s\n[%s] %d%%", message, bar, percentage)
}

// ProgressTracker manages concurrent progress updates without blocking main thread
type ProgressTracker struct {
	bot        *Bot
	chatID     int64
	messageID  int
	ctx        context.Context
	cancel     context.CancelFunc
	progressCh chan ProgressUpdate
	doneCh     chan struct{}
}

// ProgressUpdate represents a single progress update
type ProgressUpdate struct {
	Percentage int
	Message    string
}

// NewProgressTracker creates a new concurrent progress tracker
func (b *Bot) NewProgressTracker(ctx context.Context, chatID int64, messageID int) *ProgressTracker {
	if messageID <= 0 {
		return nil
	}

	childCtx, cancel := context.WithCancel(ctx)

	tracker := &ProgressTracker{
		bot:        b,
		chatID:     chatID,
		messageID:  messageID,
		ctx:        childCtx,
		cancel:     cancel,
		progressCh: make(chan ProgressUpdate, 10), // Buffered channel for progress updates
		doneCh:     make(chan struct{}),
	}

	// Start the progress update goroutine
	go tracker.progressUpdateWorker()

	return tracker
}

// progressUpdateWorker runs in a separate goroutine to handle progress updates
func (pt *ProgressTracker) progressUpdateWorker() {
	defer func() {
		// Panic recovery to prevent crashing the entire system
		if r := recover(); r != nil {
			logger.Error("Progress tracker goroutine panic recovered", map[string]interface{}{
				"error":      r,
				"chat_id":    pt.chatID,
				"message_id": pt.messageID,
			})
		}
		close(pt.doneCh)
	}()

	logger.Debug("Progress tracker worker started", map[string]interface{}{
		"chat_id":    pt.chatID,
		"message_id": pt.messageID,
	})

	for {
		select {
		case <-pt.ctx.Done():
			// Context cancelled, stop the worker
			logger.Debug("Progress tracker context cancelled", map[string]interface{}{
				"chat_id":    pt.chatID,
				"message_id": pt.messageID,
			})
			return

		case update, ok := <-pt.progressCh:
			if !ok {
				// Channel closed, stop the worker
				logger.Debug("Progress tracker channel closed", map[string]interface{}{
					"chat_id":    pt.chatID,
					"message_id": pt.messageID,
				})
				return
			}

			// Perform the actual progress update (this may take time)
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Error("Progress update panic recovered", map[string]interface{}{
							"error":      r,
							"chat_id":    pt.chatID,
							"message_id": pt.messageID,
							"percentage": update.Percentage,
						})
					}
				}()

				pt.updateProgressSync(update.Percentage, update.Message)
			}()
		}
	}
}

// updateProgressSync performs the actual progress update (blocking)
func (pt *ProgressTracker) updateProgressSync(percentage int, message string) {
	progressText := createProgressBarWithText(percentage, message)

	// Use a timeout context for the edit operation
	editCtx, cancel := context.WithTimeout(pt.ctx, 5*time.Second)
	defer cancel()

	// Perform the edit with timeout protection
	done := make(chan struct{})
	go func() {
		defer close(done)
		pt.bot.editMessage(pt.chatID, pt.messageID, progressText)
	}()

	select {
	case <-done:
		// Edit completed successfully
		logger.Debug("Progress update sent", map[string]interface{}{
			"chat_id":    pt.chatID,
			"message_id": pt.messageID,
			"percentage": percentage,
		})
	case <-editCtx.Done():
		// Edit operation timed out
		logger.Warn("Progress update timed out", map[string]interface{}{
			"chat_id":    pt.chatID,
			"message_id": pt.messageID,
			"percentage": percentage,
		})
	}

	// Add delay to make progress visible, but respect context cancellation
	select {
	case <-time.After(100 * time.Millisecond): // Reduced from 200ms to 100ms
		// Delay completed
	case <-pt.ctx.Done():
		// Context cancelled during delay
		return
	}
}

// UpdateProgress sends a progress update (non-blocking)
func (pt *ProgressTracker) UpdateProgress(percentage int, message string) {
	if pt == nil {
		return
	}

	select {
	case pt.progressCh <- ProgressUpdate{Percentage: percentage, Message: message}:
		// Update queued successfully
	case <-pt.ctx.Done():
		// Context cancelled, ignore update
	default:
		// Channel full, drop the update to avoid blocking
		logger.Warn("Progress update dropped (channel full)", map[string]interface{}{
			"chat_id":    pt.chatID,
			"message_id": pt.messageID,
			"percentage": percentage,
		})
	}
}

// Finish completes the progress and cleans up resources
func (pt *ProgressTracker) Finish() {
	if pt == nil {
		return
	}

	// Cancel the context to signal completion
	pt.cancel()

	// Close the progress channel
	close(pt.progressCh)

	// Wait for the worker to finish (with timeout)
	select {
	case <-pt.doneCh:
		// Worker finished cleanly
		logger.Debug("Progress tracker finished cleanly", map[string]interface{}{
			"chat_id":    pt.chatID,
			"message_id": pt.messageID,
		})
	case <-time.After(2 * time.Second):
		// Worker didn't finish in time
		logger.Warn("Progress tracker finish timeout", map[string]interface{}{
			"chat_id":    pt.chatID,
			"message_id": pt.messageID,
		})
	}
}

// updateProgressMessage updates a message with progress bar (DEPRECATED - use ProgressTracker)
func (b *Bot) updateProgressMessage(chatID int64, messageID int, percentage int, message string) {
	if messageID <= 0 {
		return
	}

	progressText := createProgressBarWithText(percentage, message)
	b.editMessage(chatID, messageID, progressText)

	// Add a small delay to make progress visible
	time.Sleep(200 * time.Millisecond)
}

// showCustomFileSelectionInterface shows the custom file selection interface
func (b *Bot) showCustomFileSelectionInterface(callback *tgbotapi.CallbackQuery, messageKey string, user *database.User, isPhoto bool) error {
	logger.Info("Showing custom file selection interface", map[string]interface{}{
		"message_key": messageKey,
		"is_photo":    isPhoto,
		"chat_id":     callback.Message.Chat.ID,
	})

	// Get premium level to determine file limit
	premiumLevel := b.getPremiumLevel(callback.Message.Chat.ID)
	maxFiles := database.GetCustomFileLimit(premiumLevel)

	// Get current custom files
	customFiles := user.GetCustomFiles()

	var promptText string
	var buttons [][]tgbotapi.InlineKeyboardButton

	if len(customFiles) == 0 {
		// No custom files - different handling for photo vs text messages
		if isPhoto {
			promptText = fmt.Sprintf("📁 <b>Custom Files</b> (0/%d)\n\n❌ No custom files available.\n\nUse /customfile command to manage your custom files, then try again.", maxFiles)
		} else {
			promptText = fmt.Sprintf("📁 <b>Custom Files</b> (0/%d)\n\nYou haven't added any custom files yet.\n\nClick 'Add New File' to create your first custom file.", maxFiles)

			// Add "Add New File" button only for non-photo messages
			callbackData := fmt.Sprintf("add_custom_%s", messageKey)
			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("➕ Add New File", callbackData),
			))
		}

		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Back", fmt.Sprintf("back_to_files_%s", messageKey)),
		))
	} else {
		// Show existing files with full list in message
		var msgText strings.Builder
		msgText.WriteString(fmt.Sprintf("📁 <b>Custom Files</b> (%d/%d)\n\n", len(customFiles), maxFiles))

		// List all custom files in the message
		for i, filePath := range customFiles {
			msgText.WriteString(fmt.Sprintf("%d. <code>%s</code>\n", i+1, filePath))
		}

		msgText.WriteString("\n<i>Choose a file to save your message:</i>")
		promptText = msgText.String()

		// Add buttons for each custom file in 2-column layout (max 20 buttons)
		maxButtons := len(customFiles)
		if maxButtons > 20 {
			maxButtons = 20
		}

		// Add files in rows of 2
		for i := 0; i < maxButtons; i += 2 {
			var row []tgbotapi.InlineKeyboardButton

			// First file in row
			displayName1 := b.getSmartDisplayName(customFiles[i], customFiles)
			if len(displayName1) > 15 { // Shorter for 2-column layout
				displayName1 = displayName1[:12] + "..."
			}

			var callbackData1 string
			if isPhoto {
				callbackData1 = fmt.Sprintf("photo_custom_file_%s_%d", messageKey, i)
			} else {
				callbackData1 = fmt.Sprintf("custom_file_%s_%d", messageKey, i)
			}
			row = append(row, tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d. %s", i+1, displayName1), callbackData1))

			// Second file in row (if exists)
			if i+1 < maxButtons {
				displayName2 := b.getSmartDisplayName(customFiles[i+1], customFiles)
				if len(displayName2) > 15 { // Shorter for 2-column layout
					displayName2 = displayName2[:12] + "..."
				}

				var callbackData2 string
				if isPhoto {
					callbackData2 = fmt.Sprintf("photo_custom_file_%s_%d", messageKey, i+1)
				} else {
					callbackData2 = fmt.Sprintf("custom_file_%s_%d", messageKey, i+1)
				}
				row = append(row, tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d. %s", i+2, displayName2), callbackData2))
			}

			buttons = append(buttons, row)
		}

		// Add "Show More" button if we have more than 20 files
		if len(customFiles) > 20 {
			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("📋 Show All %d Files", len(customFiles)), fmt.Sprintf("show_more_custom_%s", messageKey)),
			))
		}

		// Add action buttons row - Add New File and Remove File (only for non-photo messages)
		if !isPhoto {
			var actionButtons []tgbotapi.InlineKeyboardButton

			// Add "Add New File" button if under limit
			if len(customFiles) < maxFiles {
				addCallbackData := fmt.Sprintf("add_custom_%s", messageKey)
				actionButtons = append(actionButtons, tgbotapi.NewInlineKeyboardButtonData("➕ Add New File", addCallbackData))
			}

			// Always add "Remove File" button if there are files to remove
			removeCallbackData := fmt.Sprintf("remove_custom_%s", messageKey)
			actionButtons = append(actionButtons, tgbotapi.NewInlineKeyboardButtonData("🗑️ Remove File", removeCallbackData))

			// Add action buttons row if we have any
			if len(actionButtons) > 0 {
				buttons = append(buttons, actionButtons)
			}
		}

		// Show upgrade hint if at limit
		if len(customFiles) >= maxFiles {
			if premiumLevel < 3 {
				nextLimit := database.GetCustomFileLimit(premiumLevel + 1)
				promptText += FormatTierUpgradeHint(premiumLevel, maxFiles, nextLimit, "files")
			}
		}

		// Add back button
		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Back", fmt.Sprintf("back_to_files_%s", messageKey)),
		))
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, promptText)
	editMsg.ParseMode = "HTML"
	editMsg.ReplyMarkup = &keyboard

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		logger.Error("Failed to send custom file selection interface", map[string]interface{}{
			"error":       err.Error(),
			"message_key": messageKey,
			"is_photo":    isPhoto,
			"chat_id":     callback.Message.Chat.ID,
		})
		return fmt.Errorf("failed to show custom file selection: %w", err)
	}

	logger.Info("Successfully showed custom file selection interface", map[string]interface{}{
		"message_key":  messageKey,
		"is_photo":     isPhoto,
		"custom_files": len(user.GetCustomFiles()),
		"chat_id":      callback.Message.Chat.ID,
	})

	return nil
}

// processCustomFileSelection processes the selection of an existing custom file
func (b *Bot) processCustomFileSelection(callback *tgbotapi.CallbackQuery, messageKey string, fileIndex int, isPhoto bool) error {
	logger.Info("Processing custom file selection", map[string]interface{}{
		"message_key": messageKey,
		"file_index":  fileIndex,
		"is_photo":    isPhoto,
		"chat_id":     callback.Message.Chat.ID,
	})

	// Get user and their custom files
	user, err := b.ensureUser(callback.Message)
	if err != nil {
		errorMsg := fmt.Sprintf("❌ Failed to get user: %v", err)
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
			logger.Error("Failed to edit message", map[string]interface{}{
				"error": sendErr.Error(),
			})
			b.sendResponse(callback.Message.Chat.ID, errorMsg)
		}
		return nil
	}

	customFiles := user.GetCustomFiles()
	if fileIndex >= len(customFiles) {
		errorMsg := "❌ Invalid file selection"
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
			logger.Error("Failed to edit message", map[string]interface{}{
				"error": sendErr.Error(),
			})
			b.sendResponse(callback.Message.Chat.ID, errorMsg)
		}
		return nil
	}

	filename := customFiles[fileIndex]

	// Retrieve the original message content
	messageData, exists := b.pendingMessages[messageKey]
	if !exists {
		logger.Error("Original message not found in pending messages", map[string]interface{}{
			"message_key":  messageKey,
			"chat_id":      callback.Message.Chat.ID,
			"pending_keys": len(b.pendingMessages),
		})
		return fmt.Errorf("original message not found")
	}

	logger.Debug("Found pending message data", map[string]interface{}{
		"message_key":  messageKey,
		"message_data": messageData,
		"chat_id":      callback.Message.Chat.ID,
	})

	var content string
	var originalMessageID int
	var photoURL string

	if isPhoto {
		// Parse photo data (content|||DELIM|||messageID|||DELIM|||photoURL)
		dataParts := strings.SplitN(messageData, "|||DELIM|||", 3)
		if len(dataParts) != 3 {
			return fmt.Errorf("invalid photo message data format")
		}
		content = dataParts[0]
		originalMessageIDStr := dataParts[1]
		photoURL = dataParts[2]

		var err error
		originalMessageID, err = strconv.Atoi(originalMessageIDStr)
		if err != nil {
			logger.Warn("Failed to parse message ID, using 0", map[string]interface{}{
				"error": err.Error(),
			})
			originalMessageID = 0
		}
	} else {
		// Parse regular message data (content|||DELIM|||messageID)
		dataParts := strings.SplitN(messageData, "|||DELIM|||", 2)
		if len(dataParts) != 2 {
			return fmt.Errorf("invalid message data format")
		}
		content = dataParts[0]
		originalMessageIDStr := dataParts[1]

		var err error
		originalMessageID, err = strconv.Atoi(originalMessageIDStr)
		if err != nil {
			logger.Warn("Failed to parse message ID, using 0", map[string]interface{}{
				"error": err.Error(),
			})
			originalMessageID = 0
		}
	}

	// Clean up pending message
	delete(b.pendingMessages, messageKey)

	logger.Info("About to save message to custom file", map[string]interface{}{
		"filename":            filename,
		"content_length":      len(content),
		"original_message_id": originalMessageID,
		"photo_url":           photoURL,
		"is_photo":            isPhoto,
		"chat_id":             callback.Message.Chat.ID,
	})

	// Process the file save similar to regular file handling
	err = b.saveMessageToCustomFile(callback, filename, content, originalMessageID, photoURL, isPhoto)
	if err != nil {
		logger.Error("Failed to save message to custom file", map[string]interface{}{
			"error":               err.Error(),
			"filename":            filename,
			"content_length":      len(content),
			"original_message_id": originalMessageID,
			"photo_url":           photoURL,
			"is_photo":            isPhoto,
			"chat_id":             callback.Message.Chat.ID,
		})
	}
	return err
}

// showAddCustomFilePrompt shows a prompt for adding a new custom file
func (b *Bot) showAddCustomFilePrompt(callback *tgbotapi.CallbackQuery, messageKey string, isPhoto bool) error {
	// Delete the old message first to make a clean transition
	deleteMsg := tgbotapi.NewDeleteMessage(callback.Message.Chat.ID, callback.Message.MessageID)
	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, deleteMsg); err != nil {
		logger.Error("Failed to delete old message before custom file setup", map[string]interface{}{
			"error": err.Error(),
		})
		// Don't return error - deletion failure is not critical, continue with new message
	}

	// Send a new message with ForceReply for better UX
	promptText := `📁 <b>Add New Custom File</b>

Reply to this message with a file path for your new custom file.

<b>Examples:</b>
• <code>projects/my-project.md</code>
• <code>work/meeting-notes.md</code>
• <code>personal/diary.md</code>
• <code>ideas/startup-ideas.md</code>

<i>The file will be created if it doesn't exist, or appended to if it does.</i>`

	msg := tgbotapi.NewMessage(callback.Message.Chat.ID, promptText)
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = tgbotapi.ForceReply{ForceReply: true, Selective: true}

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, msg); err != nil {
		return fmt.Errorf("failed to send custom file prompt: %w", err)
	}

	// Store state for reply handling
	stateKey := fmt.Sprintf("add_custom_%d", callback.Message.Chat.ID)
	stateData := fmt.Sprintf("%s|||DELIM|||%t", messageKey, isPhoto)
	b.pendingMessages[stateKey] = stateData

	return nil
}

// saveMessageToCustomFile saves a message to a custom file
func (b *Bot) saveMessageToCustomFile(callback *tgbotapi.CallbackQuery, filename, content string, originalMessageID int, photoURL string, isPhoto bool) error {
	// Ensure user exists in database if database is configured
	_, err := b.ensureUser(callback.Message)
	if err != nil {
		errorMsg := fmt.Sprintf("❌ Failed to get user: %v", err)
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
			logger.Error("Failed to edit message", map[string]interface{}{
				"error": sendErr.Error(),
			})
			b.sendResponse(callback.Message.Chat.ID, errorMsg)
		}
		return nil
	}

	// Get user-specific GitHub manager
	userGitHubProvider, err := b.getUserGitHubProvider(callback.Message.Chat.ID)
	if err != nil {
		errorMsg := "❌ " + err.Error()
		if b.db != nil {
			errorMsg += ". " + consts.GitHubSetupPrompt
		}
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
			logger.Error("Failed to edit message", map[string]interface{}{
				"error": sendErr.Error(),
			})
			b.sendResponse(callback.Message.Chat.ID, errorMsg)
		}
		return nil
	}

	// Check repository capacity
	premiumLevel := b.getPremiumLevel(callback.Message.Chat.ID)
	if err := userGitHubProvider.EnsureRepositoryWithPremium(premiumLevel); err != nil {
		logger.Error("Failed to ensure repository for custom file", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": callback.Message.Chat.ID,
		})

		// Get user-friendly formatted error message
		errorMsg := b.formatRepositorySetupError(err, "save to custom files")

		// Send formatted error message
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
		editMsg.ParseMode = "html"
		if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
			logger.Error("Failed to edit message with formatted error", map[string]interface{}{
				"error": sendErr.Error(),
			})
		}
		return nil
	}

	// Update progress
	b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 25, "📝 Processing content...")

	// Get user-specific LLM client for title generation
	userLLMClient, isUsingDefaultLLM := b.getUserLLMClientWithUsageTracking(callback.Message.Chat.ID, content)

	// Process content for title and tags
	var title, tags string
	if userLLMClient != nil {
		b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 50, "🧠 LLM processing...")
		llmResponse, usage, err := userLLMClient.ProcessMessage(content)
		if err != nil {
			logger.Warn("LLM processing failed, using content-based title", map[string]interface{}{
				"error": err.Error(),
			})
			title = b.generateTitleFromContent(content)
			tags = ""
		} else {
			title, tags = b.parseTitleAndTags(llmResponse, content)

			// Record token usage in database based on LLM type
			if usage != nil && b.db != nil {
				if isUsingDefaultLLM {
					// Default LLM: record in both user_insights and user_usage
					if err := b.db.IncrementTokenUsageAll(callback.Message.Chat.ID, int64(usage.PromptTokens), int64(usage.CompletionTokens)); err != nil {
						logger.Warn("Failed to record token usage (default LLM)", map[string]interface{}{
							"error":             err.Error(),
							"chat_id":           callback.Message.Chat.ID,
							"prompt_tokens":     usage.PromptTokens,
							"completion_tokens": usage.CompletionTokens,
						})
					}
				} else {
					// Personal LLM: record only in user_insights
					if err := b.db.IncrementTokenUsageInsights(callback.Message.Chat.ID, int64(usage.PromptTokens), int64(usage.CompletionTokens)); err != nil {
						logger.Warn("Failed to record token usage (personal LLM)", map[string]interface{}{
							"error":             err.Error(),
							"chat_id":           callback.Message.Chat.ID,
							"prompt_tokens":     usage.PromptTokens,
							"completion_tokens": usage.CompletionTokens,
						})
					}
				}
			}
		}
	} else {
		title = b.generateTitleFromContent(content)
		tags = ""
	}

	// Format content based on message type
	var formattedContent string
	if isPhoto && photoURL != "" {
		logger.Info("Formatting photo content for custom file", map[string]interface{}{
			"photo_url": photoURL,
			"filename":  filename,
			"chat_id":   callback.Message.Chat.ID,
		})

		// Include photo in custom file
		if strings.HasPrefix(content, "Photo: ") {
			// No caption case
			formattedContent = b.formatMessageContentWithTitleAndTags(fmt.Sprintf("![Photo](%s)", photoURL), filename, originalMessageID, callback.Message.Chat.ID, title, tags)
		} else {
			// With caption case
			formattedContent = b.formatMessageContentWithTitleAndTags(fmt.Sprintf("![Photo](%s)\n\n%s", photoURL, content), filename, originalMessageID, callback.Message.Chat.ID, title, tags)
		}
	} else if isPhoto && photoURL == "" {
		logger.Error("Photo URL is empty for photo message to custom file", map[string]interface{}{
			"filename": filename,
			"chat_id":  callback.Message.Chat.ID,
		})
		formattedContent = b.formatMessageContentWithTitleAndTags(content, filename, originalMessageID, callback.Message.Chat.ID, title, tags)
	} else {
		formattedContent = b.formatMessageContentWithTitleAndTags(content, filename, originalMessageID, callback.Message.Chat.ID, title, tags)
	}

	// Save to GitHub
	b.updateProgressMessage(callback.Message.Chat.ID, callback.Message.MessageID, 75, "📤 Saving to GitHub...")

	commitMsg := fmt.Sprintf("Add %s to %s via Telegram", title, filename)
	committerInfo := b.getCommitterInfo(callback.Message.Chat.ID)

	logger.Info("Committing content to custom file", map[string]interface{}{
		"filename":    filename,
		"is_photo":    isPhoto,
		"photo_url":   photoURL,
		"content_len": len(formattedContent),
		"chat_id":     callback.Message.Chat.ID,
	})

	if err := userGitHubProvider.CommitFileWithAuthorAndPremium(filename, formattedContent, commitMsg, committerInfo, premiumLevel); err != nil {
		if strings.Contains(err.Error(), "GitHub authorization failed") {
			errorMsg := "❌ " + err.Error()
			editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
			if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
				logger.Error("Failed to edit message", map[string]interface{}{
					"error": sendErr.Error(),
				})
				b.sendResponse(callback.Message.Chat.ID, errorMsg)
			}
		} else {
			errorMsg := fmt.Sprintf("❌ Failed to save to %s: %v", filename, err)
			editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, errorMsg)
			if _, sendErr := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); sendErr != nil {
				logger.Error("Failed to edit message", map[string]interface{}{
					"error": sendErr.Error(),
				})
				b.sendResponse(callback.Message.Chat.ID, errorMsg)
			}
		}
		return nil
	}

	logger.Info("Successfully committed to custom file", map[string]interface{}{
		"filename":  filename,
		"is_photo":  isPhoto,
		"photo_url": photoURL,
		"chat_id":   callback.Message.Chat.ID,
	})

	// Increment commit count
	if b.db != nil {
		if err := b.db.IncrementCommitCount(callback.Message.Chat.ID); err != nil {
			logger.Error("Failed to increment commit count", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
		}

		// Update repo size after commit
		if sizeMB, _, sizeErr := userGitHubProvider.GetRepositorySizeInfoWithPremium(premiumLevel); sizeErr == nil {
			if updateErr := b.db.UpdateRepoSize(callback.Message.Chat.ID, sizeMB); updateErr != nil {
				logger.Error("Failed to update repo size", map[string]interface{}{
					"error":   updateErr.Error(),
					"chat_id": callback.Message.Chat.ID,
					"size_mb": sizeMB,
				})
			}
		} else {
			logger.Warn("Failed to get repo size for update", map[string]interface{}{
				"error":   sizeErr.Error(),
				"chat_id": callback.Message.Chat.ID,
			})
		}
	}

	successMsg := fmt.Sprintf("✅ Saved to %s", filename)

	// Try to get GitHub URL for the file
	githubURL, urlErr := userGitHubProvider.GetGitHubFileURLWithBranch(filename)
	var keyboard *tgbotapi.InlineKeyboardMarkup
	if urlErr == nil {
		row := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("🔗 View on GitHub", githubURL),
		)
		keyboardValue := tgbotapi.NewInlineKeyboardMarkup(row)
		keyboard = &keyboardValue
	}

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, successMsg)
	if keyboard != nil {
		editMsg.ReplyMarkup = keyboard
	}

	if _, err := b.rateLimitedSend(callback.Message.Chat.ID, editMsg); err != nil {
		logger.Error("Failed to edit message", map[string]interface{}{
			"error": err.Error(),
		})
		b.sendResponse(callback.Message.Chat.ID, successMsg)
	}

	return nil
}

// getSmartDisplayName creates a display name that helps distinguish files with same filename
func (b *Bot) getSmartDisplayName(filePath string, allFiles []string) string {
	parts := strings.Split(filePath, "/")
	fileName := parts[len(parts)-1]

	// If only one file, just show filename
	if len(allFiles) <= 1 {
		return fileName
	}

	// Check if any other files have the same filename
	hasConflict := false
	for _, otherPath := range allFiles {
		if otherPath != filePath {
			otherParts := strings.Split(otherPath, "/")
			otherFileName := otherParts[len(otherParts)-1]
			if otherFileName == fileName {
				hasConflict = true
				break
			}
		}
	}

	// If no conflict, just show filename
	if !hasConflict {
		return fileName
	}

	// There's a conflict, show more context
	if len(parts) == 1 {
		// File in root, just return filename
		return fileName
	} else if len(parts) == 2 {
		// Show parent/filename
		return fmt.Sprintf("%s/%s", parts[len(parts)-2], fileName)
	} else {
		// Show ...parent/filename for deeper paths
		return fmt.Sprintf(".../%s/%s", parts[len(parts)-2], fileName)
	}
}
