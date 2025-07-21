package database

import (
	"encoding/json"
	"fmt"
	"time"
)

// User represents a Telegram user with their configuration
type User struct {
	ID                  int       `db:"id" json:"id"`
	ChatId              int64     `db:"chat_id" json:"chat_id"`
	Username            string    `db:"username" json:"username"`
	GitHubToken         string    `db:"github_token" json:"github_token"`
	GitHubRepo          string    `db:"github_repo" json:"github_repo"`
	LLMToken            string    `db:"llm_token" json:"llm_token"`
	LLMSwitch           bool      `db:"llm_switch" json:"llm_switch"`
	LLMMultimodalSwitch bool      `db:"llm_multimodal_switch" json:"llm_multimodal_switch"`
	CustomFiles         string    `db:"custom_files" json:"custom_files"` // JSON array of custom file paths
	Committer           string    `db:"committer" json:"committer"`       // Custom commit author
	CreatedAt           time.Time `db:"created_at" json:"created_at"`
	UpdatedAt           time.Time `db:"updated_at" json:"updated_at"`
}

// UserConfig represents the configuration that can be updated by users
type UserConfig struct {
	GitHubToken string `json:"github_token"`
	GitHubRepo  string `json:"github_repo"`
	LLMToken    string `json:"llm_token"`
}

// HasGitHubConfig checks if user has complete GitHub configuration
func (u *User) HasGitHubConfig() bool {
	return u.GitHubToken != "" && u.GitHubRepo != ""
}

// HasLLMConfig checks if user has complete LLM configuration
func (u *User) HasLLMConfig() bool {
	return u.LLMToken != ""
}

// GetCustomFiles returns the list of custom files as a slice
func (u *User) GetCustomFiles() []string {
	var files []string
	if u.CustomFiles == "" {
		return files
	}

	// Parse JSON array
	if err := json.Unmarshal([]byte(u.CustomFiles), &files); err != nil {
		return []string{} // Return empty slice on parse error
	}

	return files
}

// SetCustomFiles sets the custom files from a slice
func (u *User) SetCustomFiles(files []string) error {
	if files == nil {
		files = []string{} // Ensure we have an empty slice instead of nil
	}

	data, err := json.Marshal(files)
	if err != nil {
		return err
	}

	u.CustomFiles = string(data)
	return nil
}

// AddCustomFile adds a file to the custom files list if not already present
func (u *User) AddCustomFile(filePath string) error {
	files := u.GetCustomFiles()

	// Check if file already exists
	for _, file := range files {
		if file == filePath {
			return nil // Already exists, no need to add
		}
	}

	// Add the new file
	files = append(files, filePath)
	return u.SetCustomFiles(files)
}

// RemoveCustomFile removes a file from the custom files list
func (u *User) RemoveCustomFile(filePath string) error {
	files := u.GetCustomFiles()

	// Find and remove the file
	for i, file := range files {
		if file == filePath {
			// Remove element at index i
			files = append(files[:i], files[i+1:]...)
			break
		}
	}

	return u.SetCustomFiles(files)
}

// PinCustomFile moves a file to the top of the custom files list (pinned position)
func (u *User) PinCustomFile(filePath string) error {
	files := u.GetCustomFiles()

	// Find the file and remove it from current position
	var found bool
	for i, file := range files {
		if file == filePath {
			// Remove from current position
			files = append(files[:i], files[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		// File doesn't exist, can't pin it
		return fmt.Errorf("file not found in custom files list")
	}

	// Add to the beginning (pinned position)
	files = append([]string{filePath}, files...)

	return u.SetCustomFiles(files)
}

// UnpinCustomFile moves a file from pinned position to the end of the list
func (u *User) UnpinCustomFile(filePath string) error {
	files := u.GetCustomFiles()

	// Check if file is in first 2 positions (pinned)
	pinnedIndex := -1
	for i := 0; i < len(files) && i < 2; i++ {
		if files[i] == filePath {
			pinnedIndex = i
			break
		}
	}

	if pinnedIndex == -1 {
		// File is not pinned
		return fmt.Errorf("file is not pinned")
	}

	// Remove from pinned position
	files = append(files[:pinnedIndex], files[pinnedIndex+1:]...)

	// Add to the end
	files = append(files, filePath)

	return u.SetCustomFiles(files)
}

// GetPinnedFiles returns the first 2 custom files (pinned files)
func (u *User) GetPinnedFiles() []string {
	files := u.GetCustomFiles()
	pinnedCount := len(files)
	if pinnedCount > 2 {
		pinnedCount = 2
	}

	pinned := make([]string, pinnedCount)
	copy(pinned, files[:pinnedCount])
	return pinned
}

// GetCustomFileMultiplier returns the correct custom file multiplier for a premium level
func GetCustomFileMultiplier(premiumLevel int) int {
	switch premiumLevel {
	case 1:
		return 2 // Coffee: 2x
	case 2:
		return 4 // Cake: 4x
	case 3:
		return 100 // Sponsor: 10x
	default:
		return 1 // Free: 1x
	}
}

// GetCustomFileLimit returns the maximum number of custom files based on premium level using multiplier
func GetCustomFileLimit(premiumLevel int) int {
	const baseCustomFileLimit = 1 // Free tier base: 2 files
	return baseCustomFileLimit * GetCustomFileMultiplier(premiumLevel)
}

// GetIssueMultiplier returns the correct issue multiplier for a premium level
func GetIssueMultiplier(premiumLevel int) int {
	switch premiumLevel {
	case 1:
		return 2 // Coffee: 2x
	case 2:
		return 4 // Cake: 4x
	case 3:
		return 100 // Sponsor: 10x
	default:
		return 1 // Free: 1x
	}
}

// GetIssueLimit returns the maximum number of issues based on premium level using multiplier
func GetIssueLimit(premiumLevel int) int64 {
	const baseIssueLimit = 90 // Free tier base: 100 issues
	return int64(baseIssueLimit * GetIssueMultiplier(premiumLevel))
}

// GetImageMultiplier returns the correct image multiplier for a premium level
func GetImageMultiplier(premiumLevel int) int {
	switch premiumLevel {
	case 1:
		return 2 // Coffee: 2x
	case 2:
		return 4 // Cake: 4x
	case 3:
		return 100 // Sponsor: 10x
	default:
		return 1 // Free: 1x
	}
}

// GetImageLimit returns the maximum number of images based on premium level using multiplier
func GetImageLimit(premiumLevel int) int64 {
	const baseImageLimit = 90 // Free tier base: 10 images
	return int64(baseImageLimit * GetImageMultiplier(premiumLevel))
}

// GetTokenMultiplier returns the correct token multiplier for a premium level
func GetTokenMultiplier(premiumLevel int) int {
	switch premiumLevel {
	case 1:
		return 2 // Coffee: 2x
	case 2:
		return 4 // Cake: 4x
	case 3:
		return 100 // Sponsor: 10x
	default:
		return 1 // Free: 1x
	}
}

// GetTokenLimit returns the maximum number of tokens based on premium level using multiplier
func GetTokenLimit(premiumLevel int) int64 {
	const baseTokenLimit = 100000 // Free tier base: 100k tokens
	return int64(baseTokenLimit * GetTokenMultiplier(premiumLevel))
}

// PremiumUser represents a premium user
type PremiumUser struct {
	ID             int       `db:"id" json:"id"`
	UID            int64     `db:"uid" json:"uid"` // User chat ID
	Username       string    `db:"username" json:"username"`
	Level          int       `db:"level" json:"level"`                     // Premium level (1 = lowest)
	ExpireAt       int64     `db:"expire_at" json:"expire_at"`             // -1 for no expiry
	SubscriptionID string    `db:"subscription_id" json:"subscription_id"` // Stripe subscription ID
	CustomerID     string    `db:"customer_id" json:"customer_id"`         // Stripe customer ID
	BillingPeriod  string    `db:"billing_period" json:"billing_period"`   // monthly/annually
	IsSubscription bool      `db:"is_subscription" json:"is_subscription"` // true for subscriptions, false for one-time
	CreatedAt      time.Time `db:"created_at" json:"created_at"`
}

// UserTopupLog represents a user's payment/topup record
type UserTopupLog struct {
	ID            int       `db:"id" json:"id"`
	UID           int64     `db:"uid" json:"uid"` // Chat ID
	Username      string    `db:"username" json:"username"`
	Amount        float64   `db:"amount" json:"amount"`                 // Paid amount
	Service       string    `db:"service" json:"service"`               // COFFEE|CAKE|SPONSOR|RESET
	TransactionID string    `db:"transaction_id" json:"transaction_id"` // Stripe session/transaction ID
	InvoiceID     string    `db:"invoice_id" json:"invoice_id"`         // Stripe invoice ID
	CreatedAt     time.Time `db:"created_at" json:"created_at"`
}

// IsExpired checks if premium user is expired
func (pu *PremiumUser) IsExpired() bool {
	if pu.ExpireAt == -1 {
		return false // Never expires
	}
	return time.Now().Unix() > pu.ExpireAt
}

// IsPremiumUser checks if user is a premium user
func (pu *PremiumUser) IsPremiumUser() bool {
	return pu.ID > 0 && pu.Level > 0 && !pu.IsExpired()
}

// UserInsights represents usage analytics for a user
type UserInsights struct {
	ID            int       `db:"id" json:"id"`
	UID           int64     `db:"uid" json:"uid"`                         // User chat ID
	CommitCnt     int64     `db:"commit_cnt" json:"commit_cnt"`           // Count of total messages stored to GitHub
	IssueCnt      int64     `db:"issue_cnt" json:"issue_cnt"`             // Count of created issues
	ImageCnt      int64     `db:"image_cnt" json:"image_cnt"`             // Count of uploaded images
	RepoSize      float64   `db:"repo_size" json:"repo_size"`             // Current repo actual size in MB
	ResetCnt      int64     `db:"reset_cnt" json:"reset_cnt"`             // Count of usage resets
	IssueCmtCnt   int64     `db:"issue_cmt_cnt" json:"issue_cmt_cnt"`     // Count of issue comments
	IssueCloseCnt int64     `db:"issue_close_cnt" json:"issue_close_cnt"` // Count of issue closes
	SyncCmdCnt    int64     `db:"sync_cmd_cnt" json:"sync_cmd_cnt"`       // Count of /sync command executions
	InsightCmdCnt int64     `db:"insight_cmd_cnt" json:"insight_cmd_cnt"` // Count of /insight command executions
	TokenInput    int64     `db:"token_input" json:"token_input"`         // Count of LLM input tokens consumed
	TokenOutput   int64     `db:"token_output" json:"token_output"`       // Count of LLM output tokens consumed
	UpdateTime    time.Time `db:"update_time" json:"update_time"`
}

// UserUsage represents current usage for a user (resettable)
type UserUsage struct {
	ID          int       `db:"id" json:"id"`
	UID         int64     `db:"uid" json:"uid"`                   // User chat ID
	IssueCnt    int64     `db:"issue_cnt" json:"issue_cnt"`       // Count of created issues
	ImageCnt    int64     `db:"image_cnt" json:"image_cnt"`       // Count of uploaded images
	TokenInput  int64     `db:"token_input" json:"token_input"`   // Count of LLM input tokens consumed
	TokenOutput int64     `db:"token_output" json:"token_output"` // Count of LLM output tokens consumed
	UpdateTime  time.Time `db:"update_time" json:"update_time"`
}

// ResetLog represents a usage reset operation record
type ResetLog struct {
	ID          int       `db:"id" json:"id"`
	UID         int64     `db:"uid" json:"uid"`                   // User chat ID
	Issues      int64     `db:"issues" json:"issues"`             // Issue count before reset
	Images      int64     `db:"images" json:"images"`             // Image count before reset
	TokenInput  int64     `db:"token_input" json:"token_input"`   // Token input count before reset
	TokenOutput int64     `db:"token_output" json:"token_output"` // Token output count before reset
	TopupLogID  int       `db:"topup_log_id" json:"topup_log_id"` // Reference to payment record
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
}

// SubscriptionChangeLog represents a subscription change record
type SubscriptionChangeLog struct {
	ID             int       `db:"id" json:"id"`
	UID            int64     `db:"uid" json:"uid"`                         // User chat ID
	SubscriptionID string    `db:"subscription_id" json:"subscription_id"` // Stripe subscription ID
	Operation      string    `db:"operation" json:"operation"`             // TERMINATE or REPLACE
	CreatedAt      time.Time `db:"created_at" json:"created_at"`
}

// SubscriptionResult represents the result of subscription creation including replacement info
type SubscriptionResult struct {
	PremiumUser            *PremiumUser `json:"premium_user"`
	ReplacedSubscriptionID string       `json:"replaced_subscription_id,omitempty"` // Set if a subscription was replaced
}
