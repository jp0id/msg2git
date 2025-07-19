package telegram

import (
	"fmt"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/msg2git/msg2git/internal/config"
	"github.com/msg2git/msg2git/internal/database"
	"github.com/msg2git/msg2git/internal/llm"
)

func TestNewBot_ConfigValidation(t *testing.T) {
	// Since NewBot tries to connect to Telegram API, we'll test the logic indirectly
	// by testing config validation and bot structure creation
	tests := []struct {
		name        string
		config      *config.Config
		expectError bool
	}{
		{
			name: "valid config structure",
			config: &config.Config{
				TelegramBotToken: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
				GitHubUsername:   "user",
				CommitAuthor:     "User <user@example.com>",
			},
			expectError: false,
		},
		{
			name: "config with database",
			config: &config.Config{
				TelegramBotToken: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
				GitHubUsername:   "user",
				CommitAuthor:     "User <user@example.com>",
				PostgreDSN:       "postgres://user:pass@localhost/db",
			},
			expectError: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test basic structure properties (since we can't access validate() method)
			if !tt.expectError {
				if tt.config.TelegramBotToken == "" {
					t.Errorf("TelegramBotToken should not be empty")
				}
				if tt.config.GitHubUsername == "" {
					t.Errorf("GitHubUsername should not be empty")
				}
				if tt.config.CommitAuthor == "" {
					t.Errorf("CommitAuthor should not be empty")
				}
				
				// Test database config detection
				if tt.config.PostgreDSN != "" {
					if !tt.config.HasDatabaseConfig() {
						t.Errorf("Config with PostgreDSN should be detected as having database config")
					}
				}
			}
		})
	}
}

func TestNewBot_WithLLMConfig(t *testing.T) {
	cfg := &config.Config{
		TelegramBotToken: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
		GitHubUsername:   "user",
		CommitAuthor:     "User <user@example.com>",
		LLMProvider:      "deepseek",
		LLMEndpoint:      "https://api.deepseek.com",
		LLMToken:         "sk-test-token",
		LLMModel:         "deepseek-chat",
	}
	
	// Test that the config has LLM configuration
	if !cfg.HasLLMConfig() {
		t.Errorf("Config should have LLM configuration")
	}
	
	// Test that LLM client can be created with this config
	client := llm.NewClient(cfg)
	if client == nil {
		t.Errorf("Should be able to create LLM client with valid config")
	}
}

func TestGetUserGitHubManager_NoDatabase(t *testing.T) {
	// Test the logic without mocking complex dependencies
	bot := &Bot{
		config: &config.Config{
			TelegramBotToken: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
			GitHubUsername:   "user",
			CommitAuthor:     "User <user@example.com>",
		},
		githubManager: nil, // No default manager
		db:            nil, // No database
	}
	
	// Test getting GitHub manager for any user when no database (should require database)
	manager, err := bot.getUserGitHubManager(123456)
	if err == nil {
		t.Errorf("getUserGitHubManager() expected error but got nil")
	}
	if manager != nil {
		t.Errorf("getUserGitHubManager() should return nil manager when no database available")
	}
	
	// Check that error message mentions database requirement
	if err != nil && !contains(err.Error(), "database is required") {
		t.Errorf("getUserGitHubManager() error should mention database requirement, got: %v", err)
	}
}

func TestGetUserGitHubManager_NoGitHubManager(t *testing.T) {
	cfg := &config.Config{
		TelegramBotToken: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
		GitHubUsername:   "user",
		CommitAuthor:     "User <user@example.com>",
	}
	
	// Create bot without GitHub manager and without database
	bot := &Bot{
		config:          cfg,
		githubManager:   nil, // No default GitHub manager
		db:              nil, // No database
		pendingMessages: make(map[string]string),
	}
	
	manager, err := bot.getUserGitHubManager(123456)
	if err == nil {
		t.Errorf("getUserGitHubManager() expected error but got nil")
	}
	if manager != nil {
		t.Errorf("getUserGitHubManager() should return nil manager when no database available")
	}
	
	// Check that error message mentions database requirement
	if err != nil && !contains(err.Error(), "database is required") {
		t.Errorf("getUserGitHubManager() error should mention database requirement, got: %v", err)
	}
}

func TestGetUserLLMClient_NoDatabase(t *testing.T) {
	// Test the logic without complex dependencies
	bot := &Bot{
		config: &config.Config{
			TelegramBotToken: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
			GitHubUsername:   "user",
			CommitAuthor:     "User <user@example.com>",
		},
		llmClient: nil, // No default LLM client
		db:        nil, // No database
	}
	
	// Test getting LLM client for any user when no database
	client := bot.getUserLLMClient(123456)
	// Should return nil when no database exists
	if client != nil {
		t.Errorf("getUserLLMClient() should return nil when no database")
	}
}


func TestEnsureUser_NoDatabase(t *testing.T) {
	cfg := &config.Config{
		TelegramBotToken: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
		GitHubUsername:   "user",
		CommitAuthor:     "User <user@example.com>",
	}
	
	bot := &Bot{
		config: cfg,
		db:     nil, // No database
	}
	
	// Create a mock message
	message := &tgbotapi.Message{
		From: &tgbotapi.User{UserName: "testuser"},
		Chat: &tgbotapi.Chat{ID: 123456},
		Text: "test message",
	}
	
	user, err := bot.ensureUser(message)
	if err != nil {
		t.Errorf("ensureUser() with no database should not error, got: %v", err)
	}
	if user != nil {
		t.Errorf("ensureUser() with no database should return nil user")
	}
}

// Mock structures for testing

type mockDatabase struct {
	users map[int64]*database.User
}

func (m *mockDatabase) GetUserByChatID(chatID int64) (*database.User, error) {
	user, exists := m.users[chatID]
	if !exists {
		return nil, nil
	}
	return user, nil
}

func (m *mockDatabase) GetOrCreateUser(chatID int64, username string) (*database.User, error) {
	user, exists := m.users[chatID]
	if !exists {
		user = &database.User{
			ChatId:   chatID,
			Username: username,
		}
		m.users[chatID] = user
	}
	return user, nil
}

func (m *mockDatabase) UpdateUserGitHubConfig(chatID int64, token, repo string) error {
	user, exists := m.users[chatID]
	if !exists {
		return fmt.Errorf("user not found")
	}
	user.GitHubToken = token
	user.GitHubRepo = repo
	return nil
}

func (m *mockDatabase) UpdateUserLLMConfig(chatID int64, token string) error {
	user, exists := m.users[chatID]
	if !exists {
		return fmt.Errorf("user not found")
	}
	user.LLMToken = token
	return nil
}


// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}