package telegram

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/msg2git/msg2git/internal/consts"
	"github.com/msg2git/msg2git/internal/database"
	"github.com/msg2git/msg2git/internal/github"
	"github.com/msg2git/msg2git/internal/logger"
)

// Information command handlers

func (b *Bot) handleInsightCommand(message *tgbotapi.Message) error {
	// Ensure user exists in database
	_, err := b.ensureUser(message)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	if b.db == nil {
		b.sendResponse(message.Chat.ID, "❌ Insights feature requires database configuration")
		return nil
	}

	// Increment insight command count in insights
	if err := b.db.IncrementInsightCmdCount(message.Chat.ID); err != nil {
		logger.Error("Failed to increment insight command count", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": message.Chat.ID,
		})
	}

	// Send initial analyzing message
	analyzingMsg := tgbotapi.NewMessage(message.Chat.ID, "📊 Analyzing your data...\n\n🔍 Fetching insights\n📈 Gathering usage statistics\n📊 Loading commit activity")
	analyzingMsg.ParseMode = "HTML"
	sentMsg, err := b.rateLimitedSend(message.Chat.ID, analyzingMsg)
	if err != nil {
		return fmt.Errorf("failed to send analyzing message: %w", err)
	}
	statusMessageID := sentMsg.MessageID

	// Get user insights and usage
	insights, err := b.db.GetUserInsights(message.Chat.ID)
	if err != nil {
		logger.Error("Failed to get user insights", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": message.Chat.ID,
		})
		b.editMessage(message.Chat.ID, statusMessageID, "❌ Failed to get insights data")
		return nil
	}

	usage, err := b.db.GetUserUsage(message.Chat.ID)
	if err != nil {
		logger.Error("Failed to get user usage", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": message.Chat.ID,
		})
		b.editMessage(message.Chat.ID, statusMessageID, "❌ Failed to get usage data")
		return nil
	}

	// Get premium status
	var premiumLevel int = 0
	var premiumInfo string = "Free"
	var isPremium bool
	premiumUser, err := b.db.GetPremiumUser(message.Chat.ID)
	if err == nil && premiumUser != nil && premiumUser.IsPremiumUser() {
		premiumLevel = premiumUser.Level
		isPremium = true
		tierNames := []string{"Free", "☕ Coffee", "🍰 Cake", "🎁 Sponsor"}
		if premiumLevel < len(tierNames) {
			premiumInfo = tierNames[premiumLevel]
			// Add expiry information if premium user has an expiry date
			if premiumUser.ExpireAt > 0 {
				expireTime := time.Unix(premiumUser.ExpireAt, 0)
				daysLeft := int(time.Until(expireTime).Hours() / 24)
				if daysLeft > 0 {
					premiumInfo += fmt.Sprintf(" (expires in %d days)", daysLeft)
				} else if daysLeft == 0 {
					premiumInfo += " (expires today)"
				} else {
					premiumInfo += " (expired)"
				}
			} else {
				premiumInfo += " (lifetime)"
			}
		}
	}

	// Get repository status information (integrated from /repostatus)
	var repoStatusSection string
	userGitHubProvider, err := b.getUserGitHubProvider(message.Chat.ID)
	if err != nil {
		// If GitHub not configured, show a message about it
		repoStatusSection = `<b>📊 Repository Status:</b>
❌ GitHub not configured
Use /repo to setup`
	} else {
		// Get repository size information with caching (consistent with /repo command)
		var statusEmoji, sizeSource string
		
		sizeMB, percentage, fromCache, cacheExpiry, err := b.getRepositorySizeWithCache(message.Chat.ID, userGitHubProvider, premiumLevel)
		
		// Determine source based on provider type and cache status (consistent with /repo command)
		if fromCache {
			// Calculate time remaining until cache expires
			timeLeft := time.Until(cacheExpiry)
			var timeLeftStr string
			
			if timeLeft < 0 {
				// Cache has already expired (edge case)
				timeLeftStr = "expired"
			} else if timeLeft >= time.Minute {
				// More than 1 minute: show in minutes
				minutes := int(timeLeft.Minutes())
				if minutes == 1 {
					timeLeftStr = "1 min"
				} else {
					timeLeftStr = fmt.Sprintf("%d mins", minutes)
				}
			} else {
				// Less than 1 minute: show in seconds
				seconds := int(timeLeft.Seconds())
				if seconds <= 1 {
					timeLeftStr = "1 sec"
				} else {
					timeLeftStr = fmt.Sprintf("%d secs", seconds)
				}
			}
			
			sizeSource = fmt.Sprintf("(Cached, %s left)", timeLeftStr)
		} else if userGitHubProvider.GetProviderType() == github.ProviderTypeAPI {
			sizeSource = "(GitHub API)"
		} else if userGitHubProvider.NeedsClone() {
			sizeSource = "(Remote API)"
		} else {
			sizeSource = "(Actual cloned size)"
		}
		
		// Get max size for display
		var maxSizeMB float64
		if premiumLevel > 0 {
			maxSizeMB = userGitHubProvider.GetRepositoryMaxSizeWithPremium(premiumLevel)
		} else {
			maxSizeMB = userGitHubProvider.GetRepositoryMaxSize()
		}
		
		if err != nil {
			repoStatusSection = `<b>📊 Repository Status:</b>
❌ Failed to get repository size info`
		} else {
			// Create progress bar
			progressBar := createProgressBarWithLen(percentage, 12)
			
			// Format status emoji based on usage
			if percentage < 50 {
				statusEmoji = "🟢"
			} else if percentage < 80 {
				statusEmoji = "🟡"
			} else {
				statusEmoji = "🔴"
			}
			
			// Add upgrade hint if repository is getting full
			var upgradeHint string
			if percentage >= 80 && !isPremium {
				upgradeHint = "\n💡 <i>Repository almost full - Use /coffee to upgrade!</i>"
			} else if percentage >= 80 && isPremium && premiumLevel < 3 {
				upgradeHint = "\n💡 <i>Repository almost full - Use /coffee for more space!</i>"
			}
			
			repoStatusSection = fmt.Sprintf(`<b>📊 Repository Status:</b>
%s %.2f MB / %.1f MB (%.1f%%)
%s
<i>Size Source: %s</i>%s`, statusEmoji, sizeMB, maxSizeMB, percentage, progressBar, sizeSource, upgradeHint)
		}
	}

	// Initialize counters
	totalCommits := int64(0)
	totalIssues := int64(0)
	totalImages := int64(0)
	resetCount := int64(0)
	issueComments := int64(0)
	issueCloses := int64(0)
	currentIssues := int64(0)
	currentImages := int64(0)

	if insights != nil {
		totalCommits = insights.CommitCnt
		totalIssues = insights.IssueCnt
		totalImages = insights.ImageCnt
		resetCount = insights.ResetCnt
		issueComments = insights.IssueCmtCnt
		issueCloses = insights.IssueCloseCnt
	}

	if usage != nil {
		currentIssues = usage.IssueCnt
		currentImages = usage.ImageCnt
	}

	// Get limits
	issueLimit := database.GetIssueLimit(premiumLevel)
	imageLimit := database.GetImageLimit(premiumLevel)

	// Get token limit and calculate percentage
	tokenLimit := database.GetTokenLimit(premiumLevel)
	var currentTokens int64
	if usage != nil {
		currentTokens = usage.TokenInput + usage.TokenOutput
	}

	// Calculate percentages
	issuePercentage := float64(currentIssues) / float64(issueLimit) * 100
	imagePercentage := float64(currentImages) / float64(imageLimit) * 100
	tokenPercentage := float64(currentTokens) / float64(tokenLimit) * 100

	// Generate commit graph
	commitGraph, err := b.generateCommitGraph(message.Chat.ID)
	if err != nil {
		logger.Warn("Failed to generate commit graph for insights", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": message.Chat.ID,
		})
		// Use fallback if commit graph fails
		commitGraph = "📊 <b>30-Day Commit Activity</b>\n<i>Unable to fetch commit data</i>\n"
	}

	// Format right-aligned usage lines
	issuesLine := b.formatUsageLine("📝 Issues:", currentIssues, issueLimit, issuePercentage)
	imagesLine := b.formatUsageLine("📷 Images:", currentImages, imageLimit, imagePercentage)
	tokensLine := b.formatTokenUsageLine("🧠 Tokens:", currentTokens, tokenLimit, tokenPercentage)

	// Format insight token usage information (all-time stats)
	var insightTokenLine string
	if insights.TokenInput > 0 || insights.TokenOutput > 0 {
		totalTokens := insights.TokenInput + insights.TokenOutput
		insightTokenLine = fmt.Sprintf("🧠 Tokens: %s (↗️%s ↘️%s)",
			formatTokenCount(totalTokens),
			formatTokenCount(insights.TokenInput),
			formatTokenCount(insights.TokenOutput))
	} else {
		insightTokenLine = "🧠 Tokens: 0 (no LLM usage)"
	}

	insightMsg := fmt.Sprintf(`📊 <b>Your Insights</b>

%s

<b>📈 Current Usage:</b>
%s
%s
%s

<b>🎯 All-Time Stats:</b>
💾 Commits: %d | 📝 Issues: %d
💬 Comments: %d | ✅ Closes: %d
📷 Images: %d | 🔄 Resets: %d
%s
✨ Tier: %s

%s

<i>Usage resets with /resetusage</i>`,
		repoStatusSection,
		issuesLine,
		imagesLine,
		tokensLine,
		totalCommits,
		totalIssues,
		issueComments,
		issueCloses,
		totalImages,
		resetCount,
		insightTokenLine,
		premiumInfo,
		commitGraph)

	// Edit the analyzing message with the complete insights
	editMsg := tgbotapi.NewEditMessageText(message.Chat.ID, statusMessageID, insightMsg)
	editMsg.ParseMode = "HTML"

	if _, err := b.rateLimitedSend(message.Chat.ID, editMsg); err != nil {
		return fmt.Errorf("failed to edit insight message: %w", err)
	}

	return nil
}

func (b *Bot) handleSyncCommand(message *tgbotapi.Message) error {
	logger.Info("Starting issue status sync", map[string]interface{}{
		"chat_id": message.Chat.ID,
	})

	// Get user-specific GitHub manager
	userGitHubProvider, err := b.getUserGitHubProvider(message.Chat.ID)
	if err != nil {
		logger.Error("Failed to get GitHub manager for user", map[string]interface{}{
			"chat_id": message.Chat.ID,
			"error":   err.Error(),
		})
		b.sendResponse(message.Chat.ID, "❌ GitHub not configured. Please use /repo to settle repo first.")
		return nil
	}

	// Increment sync command count in insights
	if b.db != nil {
		if err := b.db.IncrementSyncCmdCount(message.Chat.ID); err != nil {
			logger.Error("Failed to increment sync command count", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": message.Chat.ID,
			})
		}
	}

	// Send status message and get message ID for later editing
	statusMessageID := b.sendResponseAndGetMessageID(message.Chat.ID, "🔄 Syncing issue statuses...")

	// Acquire locks for both issue files before reading anything
	userID, err := b.getUserIDForLocking(message.Chat.ID)
	if err != nil {
		logger.Error("Failed to get user ID for locking", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": message.Chat.ID,
		})
		if statusMessageID > 0 {
			b.editMessage(message.Chat.ID, statusMessageID, "❌ Failed to initialize file locking")
		} else {
			b.sendResponse(message.Chat.ID, "❌ Failed to initialize file locking")
		}
		return err
	}

	repoURL, err := b.getRepositoryURL(message.Chat.ID)
	if err != nil {
		logger.Error("Failed to get repository URL for locking", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": message.Chat.ID,
		})
		if statusMessageID > 0 {
			b.editMessage(message.Chat.ID, statusMessageID, "❌ Failed to get repository information")
		} else {
			b.sendResponse(message.Chat.ID, "❌ Failed to get repository information")
		}
		return err
	}

	// Get file lock manager
	flm := github.GetFileLockManager()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute) // 5 minute timeout for sync
	defer cancel()

	// Acquire lock for issue.md
	issueHandle, err := flm.AcquireFileLock(ctx, userID, repoURL, "issue.md", true)
	if err != nil {
		logger.Error("Failed to acquire lock for issue.md", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": message.Chat.ID,
		})
		if statusMessageID > 0 {
			b.editMessage(message.Chat.ID, statusMessageID, "❌ Failed to acquire lock for issue.md - another sync may be in progress")
		} else {
			b.sendResponse(message.Chat.ID, "❌ Failed to acquire lock for issue.md - another sync may be in progress")
		}
		return err
	}
	defer issueHandle.Release()

	// Acquire lock for issue_archived.md
	archiveHandle, err := flm.AcquireFileLock(ctx, userID, repoURL, consts.IssueArchiveFile, true)
	if err != nil {
		logger.Error("Failed to acquire lock for issue_archived.md", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": message.Chat.ID,
		})
		if statusMessageID > 0 {
			b.editMessage(message.Chat.ID, statusMessageID, "❌ Failed to acquire lock for issue_archived.md - another sync may be in progress")
		} else {
			b.sendResponse(message.Chat.ID, "❌ Failed to acquire lock for issue_archived.md - another sync may be in progress")
		}
		return err
	}
	defer archiveHandle.Release()

	logger.Info("File locks acquired for sync operation", map[string]interface{}{
		"chat_id":    message.Chat.ID,
		"files":      []string{"issue.md", consts.IssueArchiveFile},
		"user_id":    userID,
		"timeout":    "5 minutes",
	})

	// NOW safe to read current issue.md file (with locks held)
	issueContent, err := userGitHubProvider.ReadFile("issue.md")
	if err != nil {
		logger.Error("Failed to read issue.md", map[string]interface{}{
			"error": err.Error(),
		})

		// Check if it's an authorization error and provide helpful message
		var errorMsg string
		if strings.Contains(err.Error(), "GitHub authorization failed") {
			errorMsg = "❌ " + err.Error()
		} else {
			errorMsg = "❌ Failed to read issue.md file"
		}

		if statusMessageID > 0 {
			b.editMessage(message.Chat.ID, statusMessageID, errorMsg)
		} else {
			b.sendResponse(message.Chat.ID, errorMsg)
		}
		return nil
	}

	// Parse issue statuses from content (numbers + current states from file)
	logger.Debug("Parsing issue.md content for statuses", map[string]interface{}{
		"content_length": len(issueContent),
	})
	currentStatuses := b.parseIssueStatusesFromContent(issueContent, userGitHubProvider)
	if len(currentStatuses) == 0 {
		if statusMessageID > 0 {
			b.editMessage(message.Chat.ID, statusMessageID, "ℹ️ No issues found in issue.md")
		} else {
			b.sendResponse(message.Chat.ID, "ℹ️ No issues found in issue.md")
		}
		return nil
	}

	logger.Info("Found issues to sync", map[string]interface{}{
		"count": len(currentStatuses),
	})

	// Prepare archiving - move ALL closed issues to archive for maximum GraphQL optimization
	var archivedCount, archivedOpen, archivedClosed int
	var activeIssueNumbers []int
	var archiveContent string

	// Always check for closed issues to archive (no threshold needed)
	archivedCount, archivedOpen, archivedClosed, activeIssueNumbers, archiveContent, err = b.prepareArchiveOptimized(message.Chat.ID, userGitHubProvider, currentStatuses, statusMessageID)
	if err != nil {
		logger.Error("Failed to prepare archiving", map[string]interface{}{
			"error": err.Error(),
		})
		if statusMessageID > 0 {
			b.editMessage(message.Chat.ID, statusMessageID, "❌ Failed to prepare archiving")
		} else {
			b.sendResponse(message.Chat.ID, "❌ Failed to prepare archiving")
		}
		return err
	}

	logger.Info("Issues prepared for efficient sync", map[string]interface{}{
		"original_count":  len(currentStatuses),
		"active_count":    len(activeIssueNumbers),
		"archived_count":  archivedCount,
		"archived_open":   archivedOpen,
		"archived_closed": archivedClosed,
	})

	// NOW make GraphQL call for ONLY the open issues (much fewer than 121!)
	statuses, err := userGitHubProvider.SyncIssueStatuses(activeIssueNumbers)
	if err != nil {
		logger.Error("Failed to sync issue statuses", map[string]interface{}{
			"error": err.Error(),
		})
		if statusMessageID > 0 {
			b.editMessage(message.Chat.ID, statusMessageID, "❌ Failed to fetch issue statuses from GitHub")
		} else {
			b.sendResponse(message.Chat.ID, "❌ Failed to fetch issue statuses from GitHub")
		}
		return nil
	}

	// Debug: log all statuses that were actually returned
	logger.Debug("Received statuses from GitHub", map[string]interface{}{
		"count": len(statuses),
	})
	for num, status := range statuses {
		logger.Debug("Issue status", map[string]interface{}{
			"number": num,
			"title":  status.Title,
			"state":  status.State,
		})
	}

	// Generate completely new issue.md content with current statuses
	newContent := b.generateIssueContent(statuses, userGitHubProvider)

	// Handle commit - single file or multiple files depending on whether archiving occurred
	commitMsg := "Sync issue statuses via Telegram"
	committerInfo := b.getCommitterInfo(message.Chat.ID)
	premiumLevel := b.getPremiumLevel(message.Chat.ID)

	if archivedCount > 0 {
		// If archiving occurred, commit both files together using prepared archive content
		commitMsg = fmt.Sprintf("Sync issue statuses via Telegram (archived %d issues)", archivedCount)
		// Use locked version since we already hold the file locks
		if apiProvider, ok := userGitHubProvider.(*github.APIBasedProvider); ok {
			err = apiProvider.ReplaceMultipleFilesWithAuthorAndPremiumLocked(map[string]string{
				"issue.md":              newContent,
				consts.IssueArchiveFile: archiveContent,
			}, commitMsg, committerInfo, premiumLevel)
		} else {
			err = userGitHubProvider.ReplaceMultipleFilesWithAuthorAndPremium(map[string]string{
				"issue.md":              newContent,
				consts.IssueArchiveFile: archiveContent,
			}, commitMsg, committerInfo, premiumLevel)
		}
		if err != nil {
			logger.Error("Failed to commit updated files", map[string]interface{}{
				"error": err.Error(),
			})
			if statusMessageID > 0 {
				b.editMessage(message.Chat.ID, statusMessageID, "❌ Failed to update files")
			} else {
				b.sendResponse(message.Chat.ID, "❌ Failed to update files")
			}
			return err
		}
	} else {
		// Normal single file commit
		if err := userGitHubProvider.ReplaceFileWithAuthorAndPremium("issue.md", newContent, commitMsg, committerInfo, premiumLevel); err != nil {
			logger.Error("Failed to commit updated issue.md", map[string]interface{}{
				"error": err.Error(),
			})
			if statusMessageID > 0 {
				b.editMessage(message.Chat.ID, statusMessageID, "❌ Failed to update issue.md")
			} else {
				b.sendResponse(message.Chat.ID, "❌ Failed to update issue.md")
			}
			return err
		}
	}

	// Count issues for success message
	openCount := 0
	closedCount := 0
	for _, status := range statuses {
		logger.Debug("Sync counting issue", map[string]interface{}{
			"number":       status.Number,
			"state":        status.State,
			"state_length": len(status.State),
			"is_open":      strings.ToLower(status.State) == "open",
		})
		if strings.ToLower(strings.TrimSpace(status.State)) == "open" {
			openCount++
		} else {
			closedCount++
		}
	}

	logger.Info("Sync final counts", map[string]interface{}{
		"open_count":   openCount,
		"closed_count": closedCount,
		"total_count":  len(statuses),
	})

	// Generate GitHub links for the files using proper branch detection
	issueFileLink, err := userGitHubProvider.GetGitHubFileURLWithBranch("issue.md")
	if err != nil {
		logger.Warn("Failed to get issue.md GitHub URL", map[string]interface{}{
			"error": err.Error(),
		})
		issueFileLink = "issue.md" // Fallback to filename only
	}

	archiveFileLink, err := userGitHubProvider.GetGitHubFileURLWithBranch(consts.IssueArchiveFile)
	if err != nil {
		logger.Warn("Failed to get archive file GitHub URL", map[string]interface{}{
			"error": err.Error(),
		})
		archiveFileLink = consts.IssueArchiveFile // Fallback to filename only
	}

	// Edit the status message to show success with archiving info and GitHub links
	var successMsg string
	if archivedCount > 0 {
		successMsg = fmt.Sprintf("✅ Synced %d open issues 🟢\n📦 Archived all %d closed issues 🔴 to <a href=\"%s\">%s</a>\n\n🔗 <a href=\"%s\">View issue.md</a> (open issues only)",
			len(statuses), archivedCount, archiveFileLink, consts.IssueArchiveFile, issueFileLink)
	} else {
		successMsg = fmt.Sprintf("✅ Synced %d issues: %d open 🟢, %d closed 🔴\n\n🔗 <a href=\"%s\">View issue.md</a>",
			len(statuses), openCount, closedCount, issueFileLink)
	}

	if statusMessageID > 0 {
		editMsg := tgbotapi.NewEditMessageText(message.Chat.ID, statusMessageID, successMsg)
		editMsg.ParseMode = "HTML"
		if _, err := b.rateLimitedSend(message.Chat.ID, editMsg); err != nil {
			logger.Error("Failed to edit sync message with HTML", map[string]interface{}{
				"error": err.Error(),
			})
			// Fallback to plain text
			b.editMessage(message.Chat.ID, statusMessageID, strings.ReplaceAll(strings.ReplaceAll(successMsg, "<a href=\"", ""), "</a>", ""))
		}
	} else {
		msg := tgbotapi.NewMessage(message.Chat.ID, successMsg)
		msg.ParseMode = "HTML"
		if _, err := b.rateLimitedSend(message.Chat.ID, msg); err != nil {
			logger.Error("Failed to send sync message with HTML", map[string]interface{}{
				"error": err.Error(),
			})
			// Fallback to plain text
			b.sendResponse(message.Chat.ID, strings.ReplaceAll(strings.ReplaceAll(successMsg, "<a href=\"", ""), "</a>", ""))
		}
	}

	return nil
}

// prepareArchiveOptimized handles archiving prep using current file statuses and returns active issue numbers + archive content
func (b *Bot) prepareArchiveOptimized(chatID int64, githubProvider github.GitHubProvider, currentStatuses map[int]*github.IssueStatus, statusMessageID int) (archivedCount, archivedOpen, archivedClosed int, activeIssueNumbers []int, archiveContent string, err error) {
	logger.Info("Starting optimized issue truncation and archiving", map[string]interface{}{
		"chat_id":      chatID,
		"total_issues": len(currentStatuses),
	})

	// Update status message to show archiving in progress
	if statusMessageID > 0 {
		b.editMessage(chatID, statusMessageID, "🔄 Archiving old issues to optimize sync...")
	}

	// Convert to slice for sorting (using current state info from file)
	var issueList []*github.IssueStatus
	for _, status := range currentStatuses {
		issueList = append(issueList, status)
	}

	// Step 1: Sort issues: open issues first, then closed issues, both by number (descending)
	b.sortIssuesForArchiving(issueList)

	// Step 2: Split issues - ALL open issues stay active, ALL closed issues get archived
	var activeIssues []*github.IssueStatus
	var archivedIssues []*github.IssueStatus

	for _, issue := range issueList {
		if strings.ToLower(issue.State) == "open" {
			activeIssues = append(activeIssues, issue)
		} else {
			archivedIssues = append(archivedIssues, issue)
		}
	}

	if len(archivedIssues) == 0 {
		logger.Info("No closed issues to archive", map[string]interface{}{
			"total_issues": len(issueList),
			"open_issues":  len(activeIssues),
		})
		// Return all issue numbers for GraphQL (all are open)
		activeIssueNumbers = make([]int, 0, len(activeIssues))
		for _, issue := range activeIssues {
			activeIssueNumbers = append(activeIssueNumbers, issue.Number)
		}
		return 0, 0, 0, activeIssueNumbers, "", nil
	}

	// Extract active issue numbers for GraphQL call
	activeIssueNumbers = make([]int, len(activeIssues))
	for i, issue := range activeIssues {
		activeIssueNumbers[i] = issue.Number
	}

	// Count archived issue states (all archived issues are closed by design)
	archivedCount = len(archivedIssues)
	archivedClosed = len(archivedIssues)
	archivedOpen = 0 // No open issues should be archived with this strategy

	logger.Info("Issue archiving breakdown", map[string]interface{}{
		"total_archived":   archivedCount,
		"archived_open":    archivedOpen,
		"archived_closed":  archivedClosed,
		"remaining_active": len(activeIssues),
	})

	// Step 3: Prepare archive content (in bullet format) using current status info
	newArchiveContent, err := b.prepareArchiveContent(githubProvider, archivedIssues)
	if err != nil {
		return 0, 0, 0, nil, "", fmt.Errorf("failed to prepare archive content: %w", err)
	}

	logger.Info("Issue archiving prepared successfully", map[string]interface{}{
		"archived_count":  archivedCount,
		"archived_open":   archivedOpen,
		"archived_closed": archivedClosed,
		"active_numbers":  len(activeIssueNumbers),
	})

	return archivedCount, archivedOpen, archivedClosed, activeIssueNumbers, newArchiveContent, nil
}

// sortIssuesForArchiving sorts issues with open issues first, then closed issues
// Within each group, sort by issue number descending (newest first)
// Uses Go's built-in sort.Slice for O(n log n) performance
func (b *Bot) sortIssuesForArchiving(issues []*github.IssueStatus) {
	sort.Slice(issues, func(i, j int) bool {
		iIsOpen := strings.ToLower(issues[i].State) == "open"
		jIsOpen := strings.ToLower(issues[j].State) == "open"

		// Primary sort: open issues before closed issues
		if iIsOpen != jIsOpen {
			return iIsOpen // true means i comes before j
		}

		// Secondary sort: within same state, higher issue numbers first (newest first)
		return issues[i].Number > issues[j].Number
	})
}

// generateIssueContentFromStatuses generates issue.md content from a list of issue statuses in bullet format
func (b *Bot) generateIssueContentFromStatuses(issues []*github.IssueStatus, githubProvider github.GitHubProvider) string {
	if len(issues) == 0 {
		return ""
	}

	owner, repo, _ := githubProvider.GetRepoInfo()
	var lines []string

	for _, issue := range issues {
		emoji := "🟢"
		if strings.ToLower(issue.State) == "closed" {
			emoji = "🔴"
		}
		line := fmt.Sprintf("- %s %s/%s#%d [%s]", emoji, owner, repo, issue.Number, issue.Title)
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n") + "\n"
}

// prepareArchiveContent prepares archive content by prepending archived issues in bullet format
func (b *Bot) prepareArchiveContent(githubProvider github.GitHubProvider, archivedIssues []*github.IssueStatus) (string, error) {
	// Read existing archive content
	existingContent, err := githubProvider.ReadFile(consts.IssueArchiveFile)
	if err != nil {
		// If file doesn't exist, start with empty content
		existingContent = ""
		logger.Info("Archive file doesn't exist, will create new one", map[string]interface{}{
			"filename": consts.IssueArchiveFile,
		})
	}

	// Generate archived issues in bullet format (same as issue.md)
	var archiveContent strings.Builder
	for _, issue := range archivedIssues {
		emoji := "🟢"
		if strings.ToLower(issue.State) == "closed" {
			emoji = "🔴"
		}

		// Get repo info for consistent formatting
		owner, repo, _ := githubProvider.GetRepoInfo()
		line := fmt.Sprintf("- %s %s/%s#%d [%s]", emoji, owner, repo, issue.Number, issue.Title)
		archiveContent.WriteString(line + "\n")
	}

	// Prepend archived issues to existing content
	var newContent string
	if existingContent == "" {
		newContent = archiveContent.String()
	} else {
		newContent = archiveContent.String() + existingContent
	}

	return newContent, nil
}

// Helper function to generate repository insights
func (b *Bot) generateRepositoryInsights(githubProvider github.GitHubProvider) struct {
	TotalFiles int
	TodoCount  int
	IssueCount int
} {
	insights := struct {
		TotalFiles int
		TodoCount  int
		IssueCount int
	}{}

	// Check common files and count content
	commonFiles := []string{"note.md", "todo.md", "issue.md", "idea.md", "inbox.md", "tool.md"}

	for _, filename := range commonFiles {
		content, err := githubProvider.ReadFile(filename)
		if err == nil && content != "" {
			insights.TotalFiles++

			// Count TODOs
			if filename == "todo.md" {
				todos := b.parseTodoItems(content)
				for _, todo := range todos {
					if !todo.Done {
						insights.TodoCount++
					}
				}
			}

			// Count Issues
			if filename == "issue.md" {
				issues := b.parseOpenIssuesFromContent(content)
				insights.IssueCount = len(issues)
			}
		}
	}

	return insights
}

// Helper function to create a progress bar
func createProgressBar(percentage float64) string {
	return createProgressBarWithLen(percentage, consts.ProgressBarLength)
}

func createProgressBarWithLen(percentage float64, barLength int) string {
	// Round to nearest integer for more accurate progress bar
	filled := int((percentage/100.0)*float64(barLength) + 0.5)
	if filled > barLength {
		filled = barLength
	}
	if filled < 0 {
		filled = 0
	}

	bar := ""
	for i := 0; i < barLength; i++ {
		if i < filled {
			bar += "█"
		} else {
			bar += "░"
		}
	}
	return fmt.Sprintf("%s %.0f%%", bar, percentage)
}

// getUsageStatusText returns status text based on usage percentage
func getUsageStatusText(percentage float64) string {
	if percentage < 50 {
		return "Plenty of space available"
	} else if percentage < 80 {
		return "Moderate usage"
	} else if percentage < 95 {
		return "High usage - consider cleanup"
	} else {
		return "Almost full - cleanup needed soon"
	}
}

// formatUsageLine creates a right-aligned usage line with progress bar
func (b *Bot) formatUsageLine(label string, current, limit int64, percentage float64) string {
	// Create progress bar with shorter length for mobile
	progressBar := createProgressBarWithLen(percentage, 6)

	// Format the line with proper spacing for right alignment
	return fmt.Sprintf("%s %d/%d %s", label, current, limit, progressBar)
}

// formatTokenUsageLine creates a right-aligned token usage line with progress bar and input/output breakdown
func (b *Bot) formatTokenUsageLine(label string, currentTokens, tokenLimit int64, percentage float64) string {
	// Create progress bar with shorter length for mobile
	progressBar := createProgressBarWithLen(percentage, 6)

	// Format the line with shorter token display
	return fmt.Sprintf("%s %s %s", label, formatTokenCount(currentTokens), progressBar)
}

func (b *Bot) handleStatsCommand(message *tgbotapi.Message) error {
	if b.db == nil {
		b.sendResponse(message.Chat.ID, "❌ Stats feature requires database configuration")
		return nil
	}

	// Send initial loading message
	loadingMsg := tgbotapi.NewMessage(message.Chat.ID, "📊 Loading global statistics...\n\n🔍 Gathering data from all users\n📈 Calculating totals")
	loadingMsg.ParseMode = "HTML"
	sentMsg, err := b.rateLimitedSend(message.Chat.ID, loadingMsg)
	if err != nil {
		return fmt.Errorf("failed to send loading message: %w", err)
	}
	statusMessageID := sentMsg.MessageID

	// Try to get stats from cache first
	const cacheKey = "global_stats"
	var stats *database.GlobalStats

	if cachedStats, found := b.cache.Get(cacheKey); found {
		logger.Info("Retrieved stats from cache", map[string]interface{}{
			"chat_id": message.Chat.ID,
		})
		stats = cachedStats.(*database.GlobalStats)
	} else {
		// Get global statistics from database
		var err error
		stats, err = b.db.GetGlobalStats()
		if err != nil {
			logger.Error("Failed to get global stats", map[string]interface{}{
				"error":   err.Error(),
				"chat_id": message.Chat.ID,
			})
			b.editMessage(message.Chat.ID, statusMessageID, "❌ Failed to get global statistics")
			return nil
		}

		// Cache the stats for 1 hour
		b.cache.SetWithExpiry(cacheKey, stats, 1*time.Hour)
		logger.Info("Cached global stats", map[string]interface{}{
			"chat_id": message.Chat.ID,
		})
	}

	// Calculate total token usage
	totalTokens := stats.TotalTokenInput + stats.TotalTokenOutput

	// Format the statistics message
	statsMsg := fmt.Sprintf(`📊 <b>Global Bot Statistics</b>

🌍 <b>Total Users:</b> %d
💾 <b>Total Commits:</b> %d
📝 <b>Total Issues:</b> %d
📷 <b>Total Images:</b> %d
🧠 <b>Tokens:</b> %s (↗️%s ↘️%s)
💬 <b>Issue Comments:</b> %d
✅ <b>Issue Closes:</b> %d
🔄 <b>Sync Commands:</b> %d
📊 <b>Insight Commands:</b> %d
📁 <b>Total Repo Size:</b> %.2f MB

<i>Statistics are updated in real-time</i>`,
		stats.TotalUsers,
		stats.TotalCommits,
		stats.TotalIssues,
		stats.TotalImages,
		formatTokenCount(totalTokens),
		formatTokenCount(stats.TotalTokenInput),
		formatTokenCount(stats.TotalTokenOutput),
		stats.TotalIssueComments,
		stats.TotalIssueCloses,
		stats.TotalSyncCmds,
		stats.TotalInsightCmds,
		stats.TotalRepoSizeMB)

	// Edit the loading message with the complete statistics
	editMsg := tgbotapi.NewEditMessageText(message.Chat.ID, statusMessageID, statsMsg)
	editMsg.ParseMode = "HTML"

	if _, err := b.rateLimitedSend(message.Chat.ID, editMsg); err != nil {
		return fmt.Errorf("failed to edit stats message: %w", err)
	}

	return nil
}
