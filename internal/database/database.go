package database

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/msg2git/msg2git/internal/logger"
)

type DB struct {
	conn              *sql.DB
	encryptionManager *EncryptionManager
}

// NewDB creates a new database connection
func NewDB(dsn, tokenPassword string) (*DB, error) {
	if dsn == "" {
		return nil, nil // No database configured
	}

	conn, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	// Test the connection
	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Initialize encryption manager
	encryptionManager := NewEncryptionManager(tokenPassword)
	if encryptionManager != nil {
		logger.InfoMsg("Token encryption enabled")
	} else {
		logger.WarnMsg("No TOKEN_PASSWORD provided, tokens will be stored unencrypted")
	}

	db := &DB{
		conn:              conn,
		encryptionManager: encryptionManager,
	}

	// Initialize tables if they don't exist
	if err := db.initTables(); err != nil {
		return nil, fmt.Errorf("failed to initialize tables: %w", err)
	}

	logger.InfoMsg("Database connection established successfully")
	return db, nil
}

// Close closes the database connection
func (db *DB) Close() error {
	if db.conn != nil {
		return db.conn.Close()
	}
	return nil
}

// initTables creates the users table and premium tables if they don't exist
func (db *DB) initTables() error {
	query := `
	CREATE TABLE IF NOT EXISTS users (
		id SERIAL PRIMARY KEY,
		chat_id BIGINT UNIQUE NOT NULL,
		username VARCHAR(255) NOT NULL DEFAULT '',
		github_token VARCHAR(255) NOT NULL DEFAULT '',
		github_repo VARCHAR(255) NOT NULL DEFAULT '',
		llm_token VARCHAR(255) NOT NULL DEFAULT '',
		llm_switch BOOLEAN NOT NULL DEFAULT FALSE,
		llm_multimodal_switch BOOLEAN NOT NULL DEFAULT TRUE,
		committer VARCHAR(255) NOT NULL DEFAULT '',
		custom_files TEXT NOT NULL DEFAULT '[]',
		created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
		updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_users_chat_id ON users(chat_id);

	CREATE TABLE IF NOT EXISTS premium_user (
		id SERIAL PRIMARY KEY,
		uid BIGINT UNIQUE NOT NULL,
		username VARCHAR(255) NOT NULL DEFAULT '',
		level INTEGER NOT NULL DEFAULT 0,
		expire_at BIGINT NOT NULL DEFAULT -1,
		subscription_id VARCHAR(255) NOT NULL DEFAULT '',
		customer_id VARCHAR(255) NOT NULL DEFAULT '',
		billing_period VARCHAR(50) NOT NULL DEFAULT '',
		is_subscription BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_premium_user_uid ON premium_user(uid);

	CREATE TABLE IF NOT EXISTS user_topup_log (
		id SERIAL PRIMARY KEY,
		uid BIGINT NOT NULL,
		username VARCHAR(255) NOT NULL DEFAULT '',
		amount DECIMAL(10,2) NOT NULL DEFAULT 0.00,
		service VARCHAR(50) NOT NULL DEFAULT 'COFFEE',
		transaction_id VARCHAR(255) NOT NULL DEFAULT '',
		created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_user_topup_log_uid ON user_topup_log(uid);

	CREATE TABLE IF NOT EXISTS user_insights (
		id SERIAL PRIMARY KEY,
		uid BIGINT UNIQUE NOT NULL,
		commit_cnt BIGINT NOT NULL DEFAULT 0,
		issue_cnt BIGINT NOT NULL DEFAULT 0,
		image_cnt BIGINT NOT NULL DEFAULT 0,
		repo_size DECIMAL(10,2) NOT NULL DEFAULT 0.00,
		reset_cnt BIGINT NOT NULL DEFAULT 0,
		update_time TIMESTAMP WITH TIME ZONE DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_user_insights_uid ON user_insights(uid);

	CREATE TABLE IF NOT EXISTS user_usage (
		id SERIAL PRIMARY KEY,
		uid BIGINT UNIQUE NOT NULL,
		issue_cnt BIGINT NOT NULL DEFAULT 0,
		image_cnt BIGINT NOT NULL DEFAULT 0,
		update_time TIMESTAMP WITH TIME ZONE DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_user_usage_uid ON user_usage(uid);

	CREATE TABLE IF NOT EXISTS reset_log (
		id SERIAL PRIMARY KEY,
		uid BIGINT NOT NULL,
		issues BIGINT NOT NULL DEFAULT 0,
		images BIGINT NOT NULL DEFAULT 0,
		topup_log_id INTEGER NOT NULL DEFAULT 0,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_reset_log_uid ON reset_log(uid);
	CREATE INDEX IF NOT EXISTS idx_reset_log_topup_log_id ON reset_log(topup_log_id);

	CREATE TABLE IF NOT EXISTS subscription_change_log (
		id SERIAL PRIMARY KEY,
		uid BIGINT NOT NULL,
		subscription_id VARCHAR(255) NOT NULL,
		operation VARCHAR(50) NOT NULL,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_subscription_change_log_uid ON subscription_change_log(uid);
	CREATE INDEX IF NOT EXISTS idx_subscription_change_log_subscription_id ON subscription_change_log(subscription_id);
	CREATE INDEX IF NOT EXISTS idx_subscription_change_log_created_at ON subscription_change_log(created_at);
	`

	if _, err := db.conn.Exec(query); err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	// Add custom_files column to existing users table if it doesn't exist
	alterQuery := `
	ALTER TABLE users ADD COLUMN IF NOT EXISTS custom_files TEXT NOT NULL DEFAULT '[]';
	ALTER TABLE users ADD COLUMN IF NOT EXISTS llm_switch BOOLEAN NOT NULL DEFAULT FALSE;
	ALTER TABLE users ADD COLUMN IF NOT EXISTS llm_multimodal_switch BOOLEAN NOT NULL DEFAULT TRUE;
	ALTER TABLE users ADD COLUMN IF NOT EXISTS committer VARCHAR(255) NOT NULL DEFAULT '';
	ALTER TABLE user_insights ADD COLUMN IF NOT EXISTS reset_cnt BIGINT NOT NULL DEFAULT 0;
	ALTER TABLE user_insights ADD COLUMN IF NOT EXISTS issue_cmt_cnt BIGINT NOT NULL DEFAULT 0;
	ALTER TABLE user_insights ADD COLUMN IF NOT EXISTS issue_close_cnt BIGINT NOT NULL DEFAULT 0;
	ALTER TABLE user_insights ADD COLUMN IF NOT EXISTS sync_cmd_cnt BIGINT NOT NULL DEFAULT 0;
	ALTER TABLE user_insights ADD COLUMN IF NOT EXISTS insight_cmd_cnt BIGINT NOT NULL DEFAULT 0;
	ALTER TABLE user_insights ADD COLUMN IF NOT EXISTS token_input BIGINT NOT NULL DEFAULT 0;
	ALTER TABLE user_insights ADD COLUMN IF NOT EXISTS token_output BIGINT NOT NULL DEFAULT 0;
	ALTER TABLE user_usage ADD COLUMN IF NOT EXISTS token_input BIGINT NOT NULL DEFAULT 0;
	ALTER TABLE user_usage ADD COLUMN IF NOT EXISTS token_output BIGINT NOT NULL DEFAULT 0;
	ALTER TABLE premium_user ADD COLUMN IF NOT EXISTS subscription_id VARCHAR(255) NOT NULL DEFAULT '';
	ALTER TABLE premium_user ADD COLUMN IF NOT EXISTS customer_id VARCHAR(255) NOT NULL DEFAULT '';
	ALTER TABLE premium_user ADD COLUMN IF NOT EXISTS billing_period VARCHAR(50) NOT NULL DEFAULT '';
	ALTER TABLE premium_user ADD COLUMN IF NOT EXISTS is_subscription BOOLEAN NOT NULL DEFAULT FALSE;
	ALTER TABLE user_topup_log ADD COLUMN IF NOT EXISTS service VARCHAR(50) NOT NULL DEFAULT 'COFFEE';
	ALTER TABLE user_topup_log ADD COLUMN IF NOT EXISTS transaction_id VARCHAR(255) NOT NULL DEFAULT '';
	ALTER TABLE user_topup_log ADD COLUMN IF NOT EXISTS invoice_id VARCHAR(255) NOT NULL DEFAULT '';
	ALTER TABLE reset_log ADD COLUMN IF NOT EXISTS token_input BIGINT NOT NULL DEFAULT 0;
	ALTER TABLE reset_log ADD COLUMN IF NOT EXISTS token_output BIGINT NOT NULL DEFAULT 0;
	`

	if _, err := db.conn.Exec(alterQuery); err != nil {
		return fmt.Errorf("failed to add new columns: %w", err)
	}

	return nil
}

// GetUserByChatID retrieves a user by their chat ID
func (db *DB) GetUserByChatID(chatID int64) (*User, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	query := `
	SELECT id, chat_id, username, github_token, github_repo, llm_token, llm_switch, llm_multimodal_switch, custom_files, committer, created_at, updated_at
	FROM users 
	WHERE chat_id = $1
	`

	user := &User{}
	var encryptedGitHubToken, encryptedLLMToken sql.NullString

	err := db.conn.QueryRow(query, chatID).Scan(
		&user.ID, &user.ChatId, &user.Username,
		&encryptedGitHubToken, &user.GitHubRepo, &encryptedLLMToken, &user.LLMSwitch, &user.LLMMultimodalSwitch, &user.CustomFiles, &user.Committer,
		&user.CreatedAt, &user.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // User not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Decrypt tokens if they exist
	if encryptedGitHubToken.Valid {
		decrypted, err := db.encryptionManager.Decrypt(encryptedGitHubToken.String)
		if err != nil {
			logger.Warn("Failed to decrypt GitHub token for user", map[string]interface{}{
				"chat_id": chatID,
				"error":   err.Error(),
			})
			user.GitHubToken = encryptedGitHubToken.String // Fallback to original if decryption fails
		} else {
			user.GitHubToken = decrypted
		}
	}

	if encryptedLLMToken.Valid {
		decrypted, err := db.encryptionManager.Decrypt(encryptedLLMToken.String)
		if err != nil {
			logger.Warn("Failed to decrypt LLM token for user", map[string]interface{}{
				"chat_id": chatID,
				"error":   err.Error(),
			})
			user.LLMToken = encryptedLLMToken.String // Fallback to original if decryption fails
		} else {
			user.LLMToken = decrypted
		}
	}

	return user, nil
}

// GetUserByTelegramID retrieves a user by their Telegram user ID (alias for backward compatibility)
func (db *DB) GetUserByTelegramID(telegramUserID int64) (*User, error) {
	return db.GetUserByChatID(telegramUserID)
}

// CreateUser creates a new user in the database
func (db *DB) CreateUser(chatID int64, username string) (*User, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	now := time.Now()
	query := `
	INSERT INTO users (chat_id, username, created_at, updated_at)
	VALUES ($1, $2, $3, $4)
	RETURNING id, chat_id, username, github_token, github_repo, llm_token, llm_switch, llm_multimodal_switch, custom_files, committer, created_at, updated_at
	`

	user := &User{}
	var encryptedGitHubToken, encryptedLLMToken sql.NullString

	err := db.conn.QueryRow(query, chatID, username, now, now).Scan(
		&user.ID, &user.ChatId, &user.Username,
		&encryptedGitHubToken, &user.GitHubRepo, &encryptedLLMToken, &user.LLMSwitch, &user.LLMMultimodalSwitch, &user.CustomFiles, &user.Committer,
		&user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	logger.Info("Created new user", map[string]interface{}{
		"chat_id":  chatID,
		"username": username,
	})
	return user, nil
}

// UpdateUserGitHubConfig updates a user's GitHub configuration
func (db *DB) UpdateUserGitHubConfig(chatID int64, githubToken, githubRepo string) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	// Encrypt the GitHub token
	encryptedToken, err := db.encryptionManager.Encrypt(githubToken)
	if err != nil {
		return fmt.Errorf("failed to encrypt GitHub token: %w", err)
	}

	query := `
	UPDATE users 
	SET github_token = $2, github_repo = $3, updated_at = $4
	WHERE chat_id = $1
	`

	result, err := db.conn.Exec(query, chatID, encryptedToken, githubRepo, time.Now())
	if err != nil {
		return fmt.Errorf("failed to update GitHub config: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("user not found")
	}

	return nil
}

// UpdateUserLLMConfig updates a user's LLM configuration
func (db *DB) UpdateUserLLMConfig(chatID int64, llmToken string) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	// Encrypt the LLM token
	encryptedToken, err := db.encryptionManager.Encrypt(llmToken)
	if err != nil {
		return fmt.Errorf("failed to encrypt LLM token: %w", err)
	}

	query := `
	UPDATE users 
	SET llm_token = $2, updated_at = $3
	WHERE chat_id = $1
	`

	result, err := db.conn.Exec(query, chatID, encryptedToken, time.Now())
	if err != nil {
		return fmt.Errorf("failed to update LLM config: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("user not found")
	}

	return nil
}

// GetOrCreateUser gets an existing user or creates a new one
func (db *DB) GetOrCreateUser(chatID int64, username string) (*User, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	// Try to get existing user first
	user, err := db.GetUserByChatID(chatID)
	if err != nil {
		return nil, err
	}

	// If user exists, return it
	if user != nil {
		return user, nil
	}

	// Create new user if not found
	return db.CreateUser(chatID, username)
}

// DeleteUser deletes a user from the database
func (db *DB) DeleteUser(chatID int64) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	query := `DELETE FROM users WHERE chat_id = $1`

	result, err := db.conn.Exec(query, chatID)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("user not found")
	}

	return nil
}

// Premium user methods

// GetPremiumUser gets premium user by chat ID
func (db *DB) GetPremiumUser(uid int64) (*PremiumUser, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	query := `
	SELECT id, uid, username, level, expire_at, subscription_id, customer_id, billing_period, is_subscription, created_at
	FROM premium_user 
	WHERE uid = $1
	`

	premiumUser := &PremiumUser{}
	err := db.conn.QueryRow(query, uid).Scan(
		&premiumUser.ID, &premiumUser.UID, &premiumUser.Username,
		&premiumUser.Level, &premiumUser.ExpireAt,
		&premiumUser.SubscriptionID, &premiumUser.CustomerID, &premiumUser.BillingPeriod, &premiumUser.IsSubscription,
		&premiumUser.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No premium user found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get premium user: %w", err)
	}

	return premiumUser, nil
}

// CreatePremiumUser creates a premium user
func (db *DB) CreatePremiumUser(uid int64, username string, level int, expireAt int64) (*PremiumUser, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	now := time.Now()
	query := `
	INSERT INTO premium_user (uid, username, level, expire_at, created_at, subscription_id, customer_id, billing_period, is_subscription)
	VALUES ($1, $2, $3, $4, $5, '', '', '', false)
	ON CONFLICT (uid) DO UPDATE SET username = $2, level = $3, expire_at = $4, is_subscription = false
	RETURNING id, uid, username, level, expire_at, subscription_id, customer_id, billing_period, is_subscription, created_at
	`

	premiumUser := &PremiumUser{}
	err := db.conn.QueryRow(query, uid, username, level, expireAt, now).Scan(
		&premiumUser.ID, &premiumUser.UID, &premiumUser.Username,
		&premiumUser.Level, &premiumUser.ExpireAt,
		&premiumUser.SubscriptionID, &premiumUser.CustomerID, &premiumUser.BillingPeriod, &premiumUser.IsSubscription,
		&premiumUser.CreatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create premium user: %w", err)
	}

	logger.Info("Created premium user", map[string]interface{}{
		"uid":       uid,
		"username":  username,
		"level":     level,
		"expire_at": expireAt,
	})
	return premiumUser, nil
}

// CreateSubscriptionPremiumUser creates or updates a premium user with subscription data (without setting expiry)
func (db *DB) CreateSubscriptionPremiumUser(uid int64, username string, level int, subscriptionID, customerID, billingPeriod string) (*SubscriptionResult, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	now := time.Now()
	var replacedSubscriptionID string

	// Check if user already has an active subscription
	activePremiumUser, err := db.GetActivePremiumUserWithSubscription(uid)
	if err != nil {
		return nil, fmt.Errorf("failed to check for active subscription: %w", err)
	}

	// If user has an active subscription, log the replacement using the OLD subscription_id
	if activePremiumUser != nil && activePremiumUser.SubscriptionID != subscriptionID {
		replacedSubscriptionID = activePremiumUser.SubscriptionID
		logger.Info("Detected subscription replacement", map[string]interface{}{
			"uid":                  uid,
			"old_subscription_id":  activePremiumUser.SubscriptionID,
			"new_subscription_id":  subscriptionID,
			"will_set_replaced_id": true,
		})
		_, err := db.CreateSubscriptionChangeLog(uid, activePremiumUser.SubscriptionID, "REPLACE")
		if err != nil {
			logger.Warn("Failed to create subscription change log for replacement", map[string]interface{}{
				"uid":                      uid,
				"replaced_subscription_id": activePremiumUser.SubscriptionID,
				"new_subscription_id":      subscriptionID,
				"error":                    err.Error(),
			})
		} else {
			logger.Info("Logged subscription replacement", map[string]interface{}{
				"uid":                      uid,
				"replaced_subscription_id": activePremiumUser.SubscriptionID,
				"new_subscription_id":      subscriptionID,
			})
		}
	} else {
		logger.Debug("No subscription replacement detected", map[string]interface{}{
			"uid":                        uid,
			"new_subscription_id":        subscriptionID,
			"active_premium_user_exists": activePremiumUser != nil,
			"active_subscription_id": func() string {
				if activePremiumUser != nil {
					return activePremiumUser.SubscriptionID
				} else {
					return "none"
				}
			}(),
			"subscription_ids_different": activePremiumUser != nil && activePremiumUser.SubscriptionID != subscriptionID,
		})
	}

	// For subscription creation, use default expiry (will be updated when first invoice is paid)
	// Use a temporary default of 30 days to ensure subscription is active initially
	defaultExpireAt := now.AddDate(0, 0, 30).Unix() // 30 days from now as default

	query := `
	INSERT INTO premium_user (uid, username, level, expire_at, subscription_id, customer_id, billing_period, is_subscription, created_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, true, $8)
	ON CONFLICT (uid) DO UPDATE SET 
		username = $2, 
		level = $3, 
		subscription_id = $5, 
		customer_id = $6, 
		billing_period = $7, 
		is_subscription = true,
		expire_at = CASE 
			WHEN premium_user.expire_at > $4 THEN premium_user.expire_at 
			ELSE $4 
		END
	RETURNING id, uid, username, level, expire_at, subscription_id, customer_id, billing_period, is_subscription, created_at
	`

	premiumUser := &PremiumUser{}
	err = db.conn.QueryRow(query, uid, username, level, defaultExpireAt, subscriptionID, customerID, billingPeriod, now).Scan(
		&premiumUser.ID, &premiumUser.UID, &premiumUser.Username,
		&premiumUser.Level, &premiumUser.ExpireAt,
		&premiumUser.SubscriptionID, &premiumUser.CustomerID, &premiumUser.BillingPeriod, &premiumUser.IsSubscription,
		&premiumUser.CreatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create subscription premium user: %w", err)
	}

	logger.Info("Created subscription premium user", map[string]interface{}{
		"uid":             uid,
		"username":        username,
		"level":           level,
		"subscription_id": subscriptionID,
		"billing_period":  billingPeriod,
	})

	result := &SubscriptionResult{
		PremiumUser:            premiumUser,
		ReplacedSubscriptionID: replacedSubscriptionID,
	}

	logger.Info("Returning subscription result", map[string]interface{}{
		"uid":                      uid,
		"replaced_subscription_id": result.ReplacedSubscriptionID,
		"has_replacement":          result.ReplacedSubscriptionID != "",
	})

	return result, nil
}

// CancelSubscriptionPremiumUser removes subscription data but keeps the user as free tier
func (db *DB) CancelSubscriptionPremiumUser(uid int64) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	query := `
	UPDATE premium_user 
	SET level = 0, subscription_id = '', billing_period = '', is_subscription = false
	WHERE uid = $1 AND is_subscription = true
	`

	result, err := db.conn.Exec(query, uid)
	if err != nil {
		return fmt.Errorf("failed to cancel subscription: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no active subscription found for user")
	}

	logger.Info("Cancelled subscription for user", map[string]interface{}{
		"uid": uid,
	})
	return nil
}

// RenewSubscriptionPremiumUser extends the expiry date for a subscription user
func (db *DB) RenewSubscriptionPremiumUser(uid int64, billingPeriod string) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	// Calculate new expiry date based on billing period
	now := time.Now()
	var newExpireAt int64
	switch billingPeriod {
	case "monthly":
		newExpireAt = now.AddDate(0, 1, 0).Unix() // 1 month from now
	case "annually":
		newExpireAt = now.AddDate(1, 0, 0).Unix() // 1 year from now
	default:
		// Default to monthly if billing period is unknown
		newExpireAt = now.AddDate(0, 1, 0).Unix()
	}

	query := `
	UPDATE premium_user 
	SET expire_at = $2
	WHERE uid = $1 AND is_subscription = true
	`

	result, err := db.conn.Exec(query, uid, newExpireAt)
	if err != nil {
		return fmt.Errorf("failed to renew subscription: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no active subscription found for user")
	}

	logger.Info("Renewed subscription for user", map[string]interface{}{
		"uid":            uid,
		"billing_period": billingPeriod,
		"new_expire_at":  newExpireAt,
	})
	return nil
}

// SetSubscriptionExpiry sets the expiry date for a subscription user to a specific timestamp
func (db *DB) SetSubscriptionExpiry(uid int64, expireAt int64) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	query := `
	UPDATE premium_user 
	SET expire_at = $2
	WHERE uid = $1 AND is_subscription = true
	`

	result, err := db.conn.Exec(query, uid, expireAt)
	if err != nil {
		return fmt.Errorf("failed to set subscription expiry: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no active subscription found for user")
	}

	logger.Info("Set subscription expiry for user", map[string]interface{}{
		"uid":         uid,
		"expire_at":   expireAt,
		"expire_time": time.Unix(expireAt, 0).Format("2006-01-02 15:04:05"),
	})
	return nil
}

// UpdateUserCommitter updates the committer field for any user
func (db *DB) UpdateUserCommitter(chatID int64, committer string) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	query := `
	UPDATE users 
	SET committer = $2, updated_at = $3
	WHERE chat_id = $1
	`

	result, err := db.conn.Exec(query, chatID, committer, time.Now())
	if err != nil {
		return fmt.Errorf("failed to update committer: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("user not found")
	}

	logger.Info("Updated user committer", map[string]interface{}{
		"chat_id":   chatID,
		"committer": committer,
	})

	return nil
}

// Topup log methods

// CreateTopupLog creates a user topup record
func (db *DB) CreateTopupLog(uid int64, username string, amount float64, service string, transactionID string, invoiceID string) (*UserTopupLog, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	now := time.Now()
	query := `
	INSERT INTO user_topup_log (uid, username, amount, service, transaction_id, invoice_id, created_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	RETURNING id, uid, username, amount, service, transaction_id, invoice_id, created_at
	`

	topupLog := &UserTopupLog{}
	err := db.conn.QueryRow(query, uid, username, amount, service, transactionID, invoiceID, now).Scan(
		&topupLog.ID, &topupLog.UID, &topupLog.Username,
		&topupLog.Amount, &topupLog.Service, &topupLog.TransactionID, &topupLog.InvoiceID, &topupLog.CreatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create topup log: %w", err)
	}

	logger.Info("Created topup log", map[string]interface{}{
		"uid":            uid,
		"username":       username,
		"amount":         amount,
		"service":        service,
		"transaction_id": transactionID,
	})
	return topupLog, nil
}

// GetUserTopupLogs gets all topup logs for a user
func (db *DB) GetUserTopupLogs(uid int64) ([]*UserTopupLog, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	query := `
	SELECT id, uid, username, amount, service, transaction_id, invoice_id, created_at
	FROM user_topup_log 
	WHERE uid = $1
	ORDER BY created_at DESC
	`

	rows, err := db.conn.Query(query, uid)
	if err != nil {
		return nil, fmt.Errorf("failed to get topup logs: %w", err)
	}
	defer rows.Close()

	var logs []*UserTopupLog
	for rows.Next() {
		log := &UserTopupLog{}
		err := rows.Scan(
			&log.ID, &log.UID, &log.Username,
			&log.Amount, &log.Service, &log.TransactionID, &log.InvoiceID, &log.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan topup log: %w", err)
		}
		logs = append(logs, log)
	}

	return logs, nil
}

// User Insights methods

// GetUserInsights gets user insights by uid
func (db *DB) GetUserInsights(uid int64) (*UserInsights, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	query := `
	SELECT id, uid, commit_cnt, issue_cnt, image_cnt, repo_size, reset_cnt, issue_cmt_cnt, issue_close_cnt, sync_cmd_cnt, insight_cmd_cnt, token_input, token_output, update_time
	FROM user_insights 
	WHERE uid = $1
	`

	insights := &UserInsights{}
	err := db.conn.QueryRow(query, uid).Scan(
		&insights.ID, &insights.UID, &insights.CommitCnt,
		&insights.IssueCnt, &insights.ImageCnt, &insights.RepoSize, &insights.ResetCnt,
		&insights.IssueCmtCnt, &insights.IssueCloseCnt, &insights.SyncCmdCnt, &insights.InsightCmdCnt, &insights.TokenInput, &insights.TokenOutput, &insights.UpdateTime,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No insights found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user insights: %w", err)
	}

	return insights, nil
}

// CreateOrUpdateUserInsights creates or updates user insights
func (db *DB) CreateOrUpdateUserInsights(uid int64, commitCnt, issueCnt, imageCnt int64, repoSize float64) (*UserInsights, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	now := time.Now()
	query := `
	INSERT INTO user_insights (uid, commit_cnt, issue_cnt, image_cnt, repo_size, update_time)
	VALUES ($1, $2, $3, $4, $5, $6)
	ON CONFLICT (uid) DO UPDATE SET 
		commit_cnt = $2, 
		issue_cnt = $3, 
		image_cnt = $4, 
		repo_size = $5, 
		update_time = $6
	RETURNING id, uid, commit_cnt, issue_cnt, image_cnt, repo_size, reset_cnt, issue_cmt_cnt, issue_close_cnt, sync_cmd_cnt, insight_cmd_cnt, token_input, token_output, update_time
	`

	insights := &UserInsights{}
	err := db.conn.QueryRow(query, uid, commitCnt, issueCnt, imageCnt, repoSize, now).Scan(
		&insights.ID, &insights.UID, &insights.CommitCnt,
		&insights.IssueCnt, &insights.ImageCnt, &insights.RepoSize, &insights.ResetCnt,
		&insights.IssueCmtCnt, &insights.IssueCloseCnt, &insights.SyncCmdCnt, &insights.InsightCmdCnt, &insights.TokenInput, &insights.TokenOutput, &insights.UpdateTime,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create/update user insights: %w", err)
	}

	return insights, nil
}

// IncrementCommitCount increments the commit count for a user
func (db *DB) IncrementCommitCount(uid int64) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	query := `
	INSERT INTO user_insights (uid, commit_cnt, update_time)
	VALUES ($1, 1, $2)
	ON CONFLICT (uid) DO UPDATE SET 
		commit_cnt = user_insights.commit_cnt + 1,
		update_time = $2
	`

	_, err := db.conn.Exec(query, uid, time.Now())
	if err != nil {
		return fmt.Errorf("failed to increment commit count: %w", err)
	}

	return nil
}

// IncrementIssueCount increments the issue count for a user
func (db *DB) IncrementIssueCount(uid int64) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	query := `
	INSERT INTO user_insights (uid, issue_cnt, update_time)
	VALUES ($1, 1, $2)
	ON CONFLICT (uid) DO UPDATE SET 
		issue_cnt = user_insights.issue_cnt + 1,
		update_time = $2
	`

	_, err := db.conn.Exec(query, uid, time.Now())
	if err != nil {
		return fmt.Errorf("failed to increment issue count: %w", err)
	}

	return nil
}

// IncrementImageCount increments the image count for a user
func (db *DB) IncrementImageCount(uid int64) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	query := `
	INSERT INTO user_insights (uid, image_cnt, update_time)
	VALUES ($1, 1, $2)
	ON CONFLICT (uid) DO UPDATE SET 
		image_cnt = user_insights.image_cnt + 1,
		update_time = $2
	`

	_, err := db.conn.Exec(query, uid, time.Now())
	if err != nil {
		return fmt.Errorf("failed to increment image count: %w", err)
	}

	return nil
}

// UpdateRepoSize updates the repository size for a user
func (db *DB) UpdateRepoSize(uid int64, repoSize float64) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	query := `
	INSERT INTO user_insights (uid, repo_size, update_time)
	VALUES ($1, $2, $3)
	ON CONFLICT (uid) DO UPDATE SET 
		repo_size = $2,
		update_time = $3
	`

	_, err := db.conn.Exec(query, uid, repoSize, time.Now())
	if err != nil {
		return fmt.Errorf("failed to update repo size: %w", err)
	}

	return nil
}

// UpdateUserCustomFiles updates the custom files for a user
func (db *DB) UpdateUserCustomFiles(chatID int64, customFiles string) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	query := `
	UPDATE users 
	SET custom_files = $2, updated_at = $3
	WHERE chat_id = $1
	`

	result, err := db.conn.Exec(query, chatID, customFiles, time.Now())
	if err != nil {
		return fmt.Errorf("failed to update custom files: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("user not found")
	}

	return nil
}

// CheckIssueLimit checks if user can create more issues based on their premium level
func (db *DB) CheckIssueLimit(uid int64, premiumLevel int) (bool, int64, int64, error) {
	if db == nil {
		return false, 0, 0, fmt.Errorf("database not configured")
	}

	// Get current issue count
	insights, err := db.GetUserInsights(uid)
	if err != nil {
		return false, 0, 0, fmt.Errorf("failed to get user insights: %w", err)
	}

	currentCount := int64(0)
	if insights != nil {
		currentCount = insights.IssueCnt
	}

	// Get limit for premium level
	limit := GetIssueLimit(premiumLevel)

	// Check if user can create more issues
	canCreate := currentCount < limit

	return canCreate, currentCount, limit, nil
}

// CheckImageLimit checks if user can upload more images based on their premium level
func (db *DB) CheckImageLimit(uid int64, premiumLevel int) (bool, int64, int64, error) {
	if db == nil {
		return false, 0, 0, fmt.Errorf("database not configured")
	}

	// Get current image count
	insights, err := db.GetUserInsights(uid)
	if err != nil {
		return false, 0, 0, fmt.Errorf("failed to get user insights: %w", err)
	}

	currentCount := int64(0)
	if insights != nil {
		currentCount = insights.ImageCnt
	}

	// Get limit for premium level
	limit := GetImageLimit(premiumLevel)

	// Check if user can upload more images
	canUpload := currentCount < limit

	return canUpload, currentCount, limit, nil
}

// CreateResetLog creates a new reset log entry
func (db *DB) CreateResetLog(uid int64, previousIssues, previousImages, previousTokenInput, previousTokenOutput int64, topupLogID int) (*ResetLog, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	now := time.Now()
	query := `
	INSERT INTO reset_log (uid, issues, images, token_input, token_output, topup_log_id, created_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	RETURNING id, uid, issues, images, token_input, token_output, topup_log_id, created_at
	`

	resetLog := &ResetLog{}
	err := db.conn.QueryRow(query, uid, previousIssues, previousImages, previousTokenInput, previousTokenOutput, topupLogID, now).Scan(
		&resetLog.ID, &resetLog.UID, &resetLog.Issues, &resetLog.Images,
		&resetLog.TokenInput, &resetLog.TokenOutput, &resetLog.TopupLogID, &resetLog.CreatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create reset log: %w", err)
	}

	logger.Info("Created reset log", map[string]interface{}{
		"uid":           uid,
		"issues":        previousIssues,
		"images":        previousImages,
		"token_input":   previousTokenInput,
		"token_output":  previousTokenOutput,
		"topup_log_id":  topupLogID,
	})
	return resetLog, nil
}

// GetUserResetLogs retrieves all reset logs for a user
func (db *DB) GetUserResetLogs(uid int64) ([]*ResetLog, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	query := `
	SELECT id, uid, issues, images, token_input, token_output, topup_log_id, created_at
	FROM reset_log
	WHERE uid = $1
	ORDER BY created_at DESC
	`

	rows, err := db.conn.Query(query, uid)
	if err != nil {
		return nil, fmt.Errorf("failed to query reset logs: %w", err)
	}
	defer rows.Close()

	var resetLogs []*ResetLog
	for rows.Next() {
		resetLog := &ResetLog{}
		err := rows.Scan(
			&resetLog.ID, &resetLog.UID, &resetLog.Issues, &resetLog.Images,
			&resetLog.TokenInput, &resetLog.TokenOutput, &resetLog.TopupLogID, &resetLog.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan reset log: %w", err)
		}
		resetLogs = append(resetLogs, resetLog)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating reset logs: %w", err)
	}

	return resetLogs, nil
}

// GetResetLogByTopupID retrieves a reset log by topup log ID
func (db *DB) GetResetLogByTopupID(topupLogID int) (*ResetLog, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	query := `
	SELECT id, uid, issues, images, token_input, token_output, topup_log_id, created_at
	FROM reset_log
	WHERE topup_log_id = $1
	`

	resetLog := &ResetLog{}
	err := db.conn.QueryRow(query, topupLogID).Scan(
		&resetLog.ID, &resetLog.UID, &resetLog.Issues, &resetLog.Images,
		&resetLog.TokenInput, &resetLog.TokenOutput, &resetLog.TopupLogID, &resetLog.CreatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No reset log found
		}
		return nil, fmt.Errorf("failed to get reset log: %w", err)
	}

	return resetLog, nil
}

// User Usage methods (for resettable counters)

// GetUserUsage gets user usage by uid
func (db *DB) GetUserUsage(uid int64) (*UserUsage, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	query := `
	SELECT id, uid, issue_cnt, image_cnt, token_input, token_output, update_time
	FROM user_usage 
	WHERE uid = $1
	`

	usage := &UserUsage{}
	err := db.conn.QueryRow(query, uid).Scan(
		&usage.ID, &usage.UID, &usage.IssueCnt,
		&usage.ImageCnt, &usage.TokenInput, &usage.TokenOutput, &usage.UpdateTime,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No usage found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user usage: %w", err)
	}

	return usage, nil
}

// CreateOrUpdateUserUsage creates or updates user usage
func (db *DB) CreateOrUpdateUserUsage(uid int64, issueCnt, imageCnt int64) (*UserUsage, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	now := time.Now()
	query := `
	INSERT INTO user_usage (uid, issue_cnt, image_cnt, update_time)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (uid) DO UPDATE SET 
		issue_cnt = $2, 
		image_cnt = $3, 
		update_time = $4
	RETURNING id, uid, issue_cnt, image_cnt, token_input, token_output, update_time
	`

	usage := &UserUsage{}
	err := db.conn.QueryRow(query, uid, issueCnt, imageCnt, now).Scan(
		&usage.ID, &usage.UID, &usage.IssueCnt,
		&usage.ImageCnt, &usage.TokenInput, &usage.TokenOutput, &usage.UpdateTime,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create/update user usage: %w", err)
	}

	return usage, nil
}

// IncrementUsageIssueCount increments the issue count in user_usage table
func (db *DB) IncrementUsageIssueCount(uid int64) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	query := `
	INSERT INTO user_usage (uid, issue_cnt, update_time)
	VALUES ($1, 1, $2)
	ON CONFLICT (uid) DO UPDATE SET 
		issue_cnt = user_usage.issue_cnt + 1,
		update_time = $2
	`

	_, err := db.conn.Exec(query, uid, time.Now())
	if err != nil {
		return fmt.Errorf("failed to increment usage issue count: %w", err)
	}

	return nil
}

// IncrementUsageImageCount increments the image count in user_usage table
func (db *DB) IncrementUsageImageCount(uid int64) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	query := `
	INSERT INTO user_usage (uid, image_cnt, update_time)
	VALUES ($1, 1, $2)
	ON CONFLICT (uid) DO UPDATE SET 
		image_cnt = user_usage.image_cnt + 1,
		update_time = $2
	`

	_, err := db.conn.Exec(query, uid, time.Now())
	if err != nil {
		return fmt.Errorf("failed to increment usage image count: %w", err)
	}

	return nil
}

// ResetUserUsage resets user usage counters to 0
func (db *DB) ResetUserUsage(uid int64) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	query := `
	INSERT INTO user_usage (uid, issue_cnt, image_cnt, token_input, token_output, update_time)
	VALUES ($1, 0, 0, 0, 0, $2)
	ON CONFLICT (uid) DO UPDATE SET 
		issue_cnt = 0,
		image_cnt = 0,
		token_input = 0,
		token_output = 0,
		update_time = $2
	`

	_, err := db.conn.Exec(query, uid, time.Now())
	if err != nil {
		return fmt.Errorf("failed to reset user usage: %w", err)
	}

	return nil
}

// IncrementResetCount increments the reset count in user_insights table
func (db *DB) IncrementResetCount(uid int64) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	query := `
	INSERT INTO user_insights (uid, reset_cnt, update_time)
	VALUES ($1, 1, $2)
	ON CONFLICT (uid) DO UPDATE SET 
		reset_cnt = user_insights.reset_cnt + 1,
		update_time = $2
	`

	_, err := db.conn.Exec(query, uid, time.Now())
	if err != nil {
		return fmt.Errorf("failed to increment reset count: %w", err)
	}

	return nil
}

// IncrementIssueCommentCount increments the issue comment count in user_insights table
func (db *DB) IncrementIssueCommentCount(uid int64) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	query := `
	INSERT INTO user_insights (uid, issue_cmt_cnt, update_time)
	VALUES ($1, 1, $2)
	ON CONFLICT (uid) DO UPDATE SET 
		issue_cmt_cnt = user_insights.issue_cmt_cnt + 1,
		update_time = $2
	`

	_, err := db.conn.Exec(query, uid, time.Now())
	if err != nil {
		return fmt.Errorf("failed to increment issue comment count: %w", err)
	}

	return nil
}

// IncrementIssueCloseCount increments the issue close count in user_insights table
func (db *DB) IncrementIssueCloseCount(uid int64) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	query := `
	INSERT INTO user_insights (uid, issue_close_cnt, update_time)
	VALUES ($1, 1, $2)
	ON CONFLICT (uid) DO UPDATE SET 
		issue_close_cnt = user_insights.issue_close_cnt + 1,
		update_time = $2
	`

	_, err := db.conn.Exec(query, uid, time.Now())
	if err != nil {
		return fmt.Errorf("failed to increment issue close count: %w", err)
	}

	return nil
}

// IncrementSyncCmdCount increments the sync command count in user_insights table
func (db *DB) IncrementSyncCmdCount(uid int64) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	query := `
	INSERT INTO user_insights (uid, sync_cmd_cnt, update_time)
	VALUES ($1, 1, $2)
	ON CONFLICT (uid) DO UPDATE SET 
		sync_cmd_cnt = user_insights.sync_cmd_cnt + 1,
		update_time = $2
	`

	_, err := db.conn.Exec(query, uid, time.Now())
	if err != nil {
		return fmt.Errorf("failed to increment sync command count: %w", err)
	}

	return nil
}

// IncrementInsightCmdCount increments the insight command count in user_insights table
func (db *DB) IncrementInsightCmdCount(uid int64) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	query := `
	INSERT INTO user_insights (uid, insight_cmd_cnt, update_time)
	VALUES ($1, 1, $2)
	ON CONFLICT (uid) DO UPDATE SET 
		insight_cmd_cnt = user_insights.insight_cmd_cnt + 1,
		update_time = $2
	`

	_, err := db.conn.Exec(query, uid, time.Now())
	if err != nil {
		return fmt.Errorf("failed to increment insight command count: %w", err)
	}

	return nil
}

// IncrementTokenUsage increments token usage in both user_insights and user_usage tables
func (db *DB) IncrementTokenUsage(uid int64, inputTokens, outputTokens int64) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	now := time.Now()

	// Update user_insights (permanent record)
	insightsQuery := `
	INSERT INTO user_insights (uid, token_input, token_output, update_time)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (uid) DO UPDATE SET 
		token_input = user_insights.token_input + $2,
		token_output = user_insights.token_output + $3,
		update_time = $4
	`

	_, err := db.conn.Exec(insightsQuery, uid, inputTokens, outputTokens, now)
	if err != nil {
		return fmt.Errorf("failed to increment token usage in insights: %w", err)
	}

	// Update user_usage (resettable)
	usageQuery := `
	INSERT INTO user_usage (uid, token_input, token_output, update_time)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (uid) DO UPDATE SET 
		token_input = user_usage.token_input + $2,
		token_output = user_usage.token_output + $3,
		update_time = $4
	`

	_, err = db.conn.Exec(usageQuery, uid, inputTokens, outputTokens, now)
	if err != nil {
		return fmt.Errorf("failed to increment token usage in usage: %w", err)
	}

	return nil
}

// IncrementTokenUsageInsights increments token usage only in user_insights table (for personal LLM usage)
func (db *DB) IncrementTokenUsageInsights(uid int64, inputTokens, outputTokens int64) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	now := time.Now()

	// Update user_insights (permanent record)
	insightsQuery := `
	INSERT INTO user_insights (uid, token_input, token_output, update_time)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (uid) DO UPDATE SET 
		token_input = user_insights.token_input + $2,
		token_output = user_insights.token_output + $3,
		update_time = $4
	`

	_, err := db.conn.Exec(insightsQuery, uid, inputTokens, outputTokens, now)
	if err != nil {
		return fmt.Errorf("failed to increment token usage in insights: %w", err)
	}

	logger.Info("Incremented token usage (insights only)", map[string]interface{}{
		"uid":           uid,
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
	})
	return nil
}

// IncrementTokenUsageAll increments token usage in both user_insights and user_usage tables (for default LLM usage)
func (db *DB) IncrementTokenUsageAll(uid int64, inputTokens, outputTokens int64) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	now := time.Now()

	// Update user_insights (permanent record)
	insightsQuery := `
	INSERT INTO user_insights (uid, token_input, token_output, update_time)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (uid) DO UPDATE SET 
		token_input = user_insights.token_input + $2,
		token_output = user_insights.token_output + $3,
		update_time = $4
	`

	_, err := db.conn.Exec(insightsQuery, uid, inputTokens, outputTokens, now)
	if err != nil {
		return fmt.Errorf("failed to increment token usage in insights: %w", err)
	}

	// Update user_usage (resettable)
	usageQuery := `
	INSERT INTO user_usage (uid, token_input, token_output, update_time)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (uid) DO UPDATE SET 
		token_input = user_usage.token_input + $2,
		token_output = user_usage.token_output + $3,
		update_time = $4
	`

	_, err = db.conn.Exec(usageQuery, uid, inputTokens, outputTokens, now)
	if err != nil {
		return fmt.Errorf("failed to increment token usage in usage: %w", err)
	}

	logger.Info("Incremented token usage (both insights and usage)", map[string]interface{}{
		"uid":           uid,
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
	})
	return nil
}

// CheckUsageIssueLimit checks if user can create more issues based on current usage
func (db *DB) CheckUsageIssueLimit(uid int64, premiumLevel int) (bool, int64, int64, error) {
	if db == nil {
		return false, 0, 0, fmt.Errorf("database not configured")
	}

	// Get current usage count
	usage, err := db.GetUserUsage(uid)
	if err != nil {
		return false, 0, 0, fmt.Errorf("failed to get user usage: %w", err)
	}

	currentCount := int64(0)
	if usage != nil {
		currentCount = usage.IssueCnt
	}

	// Get limit for premium level
	limit := GetIssueLimit(premiumLevel)

	// Check if user can create more issues
	canCreate := currentCount < limit

	return canCreate, currentCount, limit, nil
}

// CheckUsageImageLimit checks if user can upload more images based on current usage
func (db *DB) CheckUsageImageLimit(uid int64, premiumLevel int) (bool, int64, int64, error) {
	if db == nil {
		return false, 0, 0, fmt.Errorf("database not configured")
	}

	// Get current usage count
	usage, err := db.GetUserUsage(uid)
	if err != nil {
		return false, 0, 0, fmt.Errorf("failed to get user usage: %w", err)
	}

	currentCount := int64(0)
	if usage != nil {
		currentCount = usage.ImageCnt
	}

	// Get limit for premium level
	limit := GetImageLimit(premiumLevel)

	// Check if user can upload more images
	canUpload := currentCount < limit

	return canUpload, currentCount, limit, nil
}

// GlobalStats represents global bot statistics
type GlobalStats struct {
	TotalCommits       int64   `json:"total_commits"`
	TotalUsers         int64   `json:"total_users"`
	TotalIssues        int64   `json:"total_issues"`
	TotalImages        int64   `json:"total_images"`
	TotalIssueCloses   int64   `json:"total_issue_closes"`
	TotalIssueComments int64   `json:"total_issue_comments"`
	TotalSyncCmds      int64   `json:"total_sync_cmds"`
	TotalInsightCmds   int64   `json:"total_insight_cmds"`
	TotalTokenInput    int64   `json:"total_token_input"`
	TotalTokenOutput   int64   `json:"total_token_output"`
	TotalRepoSizeMB    float64 `json:"total_repo_size_mb"`
}

// GetGlobalStats gets global bot statistics from user_insights table
func (db *DB) GetGlobalStats() (*GlobalStats, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	query := `
	SELECT 
		COALESCE(SUM(commit_cnt), 0) as total_commits,
		COUNT(DISTINCT uid) as total_users,
		COALESCE(SUM(issue_cnt), 0) as total_issues,
		COALESCE(SUM(image_cnt), 0) as total_images,
		COALESCE(SUM(issue_close_cnt), 0) as total_issue_closes,
		COALESCE(SUM(issue_cmt_cnt), 0) as total_issue_comments,
		COALESCE(SUM(sync_cmd_cnt), 0) as total_sync_cmds,
		COALESCE(SUM(insight_cmd_cnt), 0) as total_insight_cmds,
		COALESCE(SUM(token_input), 0) as total_token_input,
		COALESCE(SUM(token_output), 0) as total_token_output,
		COALESCE(SUM(repo_size), 0) as total_repo_size_mb
	FROM user_insights
	`

	stats := &GlobalStats{}
	err := db.conn.QueryRow(query).Scan(
		&stats.TotalCommits,
		&stats.TotalUsers,
		&stats.TotalIssues,
		&stats.TotalImages,
		&stats.TotalIssueCloses,
		&stats.TotalIssueComments,
		&stats.TotalSyncCmds,
		&stats.TotalInsightCmds,
		&stats.TotalTokenInput,
		&stats.TotalTokenOutput,
		&stats.TotalRepoSizeMB,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get global stats: %w", err)
	}

	return stats, nil
}

// Subscription Change Log methods

// CreateSubscriptionChangeLog creates a new subscription change log entry
func (db *DB) CreateSubscriptionChangeLog(uid int64, subscriptionID string, operation string) (*SubscriptionChangeLog, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	now := time.Now()
	query := `
	INSERT INTO subscription_change_log (uid, subscription_id, operation, created_at)
	VALUES ($1, $2, $3, $4)
	RETURNING id, uid, subscription_id, operation, created_at
	`

	changeLog := &SubscriptionChangeLog{}
	err := db.conn.QueryRow(query, uid, subscriptionID, operation, now).Scan(
		&changeLog.ID, &changeLog.UID, &changeLog.SubscriptionID,
		&changeLog.Operation, &changeLog.CreatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create subscription change log: %w", err)
	}

	logger.Info("Created subscription change log", map[string]interface{}{
		"uid":             uid,
		"subscription_id": subscriptionID,
		"operation":       operation,
	})
	return changeLog, nil
}

// GetSubscriptionChangeLogs retrieves all subscription change logs for a user
func (db *DB) GetSubscriptionChangeLogs(uid int64) ([]*SubscriptionChangeLog, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	query := `
	SELECT id, uid, subscription_id, operation, created_at
	FROM subscription_change_log
	WHERE uid = $1
	ORDER BY created_at DESC
	`

	rows, err := db.conn.Query(query, uid)
	if err != nil {
		return nil, fmt.Errorf("failed to query subscription change logs: %w", err)
	}
	defer rows.Close()

	var changeLogs []*SubscriptionChangeLog
	for rows.Next() {
		changeLog := &SubscriptionChangeLog{}
		err := rows.Scan(
			&changeLog.ID, &changeLog.UID, &changeLog.SubscriptionID,
			&changeLog.Operation, &changeLog.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan subscription change log: %w", err)
		}
		changeLogs = append(changeLogs, changeLog)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating subscription change logs: %w", err)
	}

	return changeLogs, nil
}

// GetSubscriptionChangeLogBySubscriptionID retrieves a subscription change log by subscription ID
func (db *DB) GetSubscriptionChangeLogBySubscriptionID(subscriptionID string) (*SubscriptionChangeLog, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	query := `
	SELECT id, uid, subscription_id, operation, created_at
	FROM subscription_change_log
	WHERE subscription_id = $1
	ORDER BY created_at DESC
	LIMIT 1
	`

	changeLog := &SubscriptionChangeLog{}
	err := db.conn.QueryRow(query, subscriptionID).Scan(
		&changeLog.ID, &changeLog.UID, &changeLog.SubscriptionID,
		&changeLog.Operation, &changeLog.CreatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No change log found
		}
		return nil, fmt.Errorf("failed to get subscription change log: %w", err)
	}

	return changeLog, nil
}

// GetActivePremiumUserWithSubscription checks if user has an active subscription that expires after now
func (db *DB) GetActivePremiumUserWithSubscription(uid int64) (*PremiumUser, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	query := `
	SELECT id, uid, username, level, expire_at, subscription_id, customer_id, billing_period, is_subscription, created_at
	FROM premium_user 
	WHERE uid = $1 AND is_subscription = true AND subscription_id != '' AND expire_at > $2
	`

	premiumUser := &PremiumUser{}
	err := db.conn.QueryRow(query, uid, time.Now().Unix()).Scan(
		&premiumUser.ID, &premiumUser.UID, &premiumUser.Username,
		&premiumUser.Level, &premiumUser.ExpireAt,
		&premiumUser.SubscriptionID, &premiumUser.CustomerID, &premiumUser.BillingPeriod, &premiumUser.IsSubscription,
		&premiumUser.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No active premium user found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get active premium user: %w", err)
	}

	return premiumUser, nil
}

// UpdateUserLLMSwitch updates a user's LLM switch setting
func (db *DB) UpdateUserLLMSwitch(chatID int64, llmSwitch bool) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	query := `
	UPDATE users 
	SET llm_switch = $1, updated_at = $2
	WHERE chat_id = $3
	`

	_, err := db.conn.Exec(query, llmSwitch, time.Now(), chatID)
	if err != nil {
		return fmt.Errorf("failed to update user LLM switch: %w", err)
	}

	logger.Info("Updated user LLM switch", map[string]interface{}{
		"chat_id":    chatID,
		"llm_switch": llmSwitch,
	})
	return nil
}

// UpdateUserLLMMultimodalSwitch updates the LLM multimodal switch setting for a user
func (db *DB) UpdateUserLLMMultimodalSwitch(chatID int64, multimodalSwitch bool) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	query := `
	UPDATE users 
	SET llm_multimodal_switch = $1, updated_at = $2
	WHERE chat_id = $3
	`

	_, err := db.conn.Exec(query, multimodalSwitch, time.Now(), chatID)
	if err != nil {
		return fmt.Errorf("failed to update user LLM multimodal switch: %w", err)
	}

	logger.Info("Updated user LLM multimodal switch", map[string]interface{}{
		"chat_id":                 chatID,
		"llm_multimodal_switch": multimodalSwitch,
	})
	return nil
}

// CanUseDefaultLLM checks if a user can use default LLM processing based on their token usage and limits
func (db *DB) CanUseDefaultLLM(chatID int64, estimatedTokens int64) (bool, error) {
	if db == nil {
		return false, fmt.Errorf("database not configured")
	}

	// Get user's current token usage
	usage, err := db.GetUserUsage(chatID)
	if err != nil {
		return false, fmt.Errorf("failed to get user usage: %w", err)
	}

	// Get user's premium level
	premiumLevel := 0
	premiumUser, err := db.GetPremiumUser(chatID)
	if err == nil && premiumUser != nil && premiumUser.IsPremiumUser() {
		premiumLevel = premiumUser.Level
	}

	// Calculate token limit based on premium level
	tokenLimit := GetTokenLimit(premiumLevel)

	// Check if current usage + estimated tokens would exceed limit
	if usage != nil {
		totalUsage := usage.TokenInput + usage.TokenOutput
		if totalUsage+estimatedTokens > tokenLimit {
			return false, nil // Would exceed limit
		}
	} else {
		// No usage record yet, check if estimated tokens exceed limit
		if estimatedTokens > tokenLimit {
			return false, nil // Would exceed limit
		}
	}

	return true, nil
}
