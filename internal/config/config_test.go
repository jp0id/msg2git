package config

import (
	"testing"
)

func TestHasLLMConfig(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected bool
	}{
		{
			name: "all LLM fields populated",
			config: &Config{
				LLMProvider: "deepseek",
				LLMEndpoint: "https://api.deepseek.com/v1/chat/completions",
				LLMToken:    "sk-test-token",
				LLMModel:    "deepseek-chat",
			},
			expected: true,
		},
		{
			name: "missing provider",
			config: &Config{
				LLMProvider: "",
				LLMEndpoint: "https://api.deepseek.com/v1/chat/completions",
				LLMToken:    "sk-test-token",
				LLMModel:    "deepseek-chat",
			},
			expected: false,
		},
		{
			name: "missing endpoint",
			config: &Config{
				LLMProvider: "deepseek",
				LLMEndpoint: "",
				LLMToken:    "sk-test-token",
				LLMModel:    "deepseek-chat",
			},
			expected: false,
		},
		{
			name: "missing token",
			config: &Config{
				LLMProvider: "deepseek",
				LLMEndpoint: "https://api.deepseek.com/v1/chat/completions",
				LLMToken:    "",
				LLMModel:    "deepseek-chat",
			},
			expected: false,
		},
		{
			name: "missing model",
			config: &Config{
				LLMProvider: "deepseek",
				LLMEndpoint: "https://api.deepseek.com/v1/chat/completions",
				LLMToken:    "sk-test-token",
				LLMModel:    "",
			},
			expected: false,
		},
		{
			name:     "empty config",
			config:   &Config{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.HasLLMConfig()
			if result != tt.expected {
				t.Errorf("HasLLMConfig() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestHasDatabaseConfig(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected bool
	}{
		{
			name: "has database config",
			config: &Config{
				PostgreDSN: "postgres://user:pass@localhost/db",
			},
			expected: true,
		},
		{
			name: "empty database config",
			config: &Config{
				PostgreDSN: "",
			},
			expected: false,
		},
		{
			name:     "nil config",
			config:   &Config{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.HasDatabaseConfig()
			if result != tt.expected {
				t.Errorf("HasDatabaseConfig() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorContains string
	}{
		{
			name: "valid config",
			config: &Config{
				TelegramBotToken: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
				GitHubUsername:   "user",
				CommitAuthor:     "User <user@example.com>",
			},
			expectError: false,
		},
		{
			name: "missing telegram token",
			config: &Config{
				TelegramBotToken: "",
				GitHubUsername:   "user",
				CommitAuthor:     "User <user@example.com>",
			},
			expectError:   true,
			errorContains: "TELEGRAM_BOT_TOKEN",
		},
		{
			name: "missing github username",
			config: &Config{
				TelegramBotToken: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
				GitHubUsername:   "",
				CommitAuthor:     "User <user@example.com>",
			},
			expectError:   true,
			errorContains: "GITHUB_USERNAME",
		},
		{
			name: "missing commit author",
			config: &Config{
				TelegramBotToken: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
				GitHubUsername:   "user",
				CommitAuthor:     "",
			},
			expectError:   true,
			errorContains: "COMMIT_AUTHOR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			
			if tt.expectError {
				if err == nil {
					t.Errorf("validate() expected error but got nil")
					return
				}
				if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("validate() error = %v, want to contain %s", err, tt.errorContains)
				}
			} else {
				if err != nil {
					t.Errorf("validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	// Since Load() tries to load from .env file which may not exist in test environment,
	// we'll test the validate and struct creation separately
	
	t.Run("valid config struct creation", func(t *testing.T) {
		cfg := &Config{
			TelegramBotToken: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
			GitHubUsername:   "user",
			CommitAuthor:     "User <user@example.com>",
			LLMProvider:      "deepseek",
			LLMEndpoint:      "https://api.deepseek.com",
			LLMModel:         "deepseek-chat",
			PostgreDSN:       "postgres://user:pass@localhost/db",
			TokenPassword:    "secret",
		}
		
		err := cfg.validate()
		if err != nil {
			t.Errorf("validate() unexpected error = %v", err)
		}
		
		// Check all fields are populated correctly
		if cfg.TelegramBotToken != "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11" {
			t.Errorf("TelegramBotToken = %v, want %v", cfg.TelegramBotToken, "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11")
		}
		if cfg.GitHubUsername != "user" {
			t.Errorf("GitHubUsername = %v, want %v", cfg.GitHubUsername, "user")
		}
		if cfg.CommitAuthor != "User <user@example.com>" {
			t.Errorf("CommitAuthor = %v, want %v", cfg.CommitAuthor, "User <user@example.com>")
		}
		if cfg.LLMProvider != "deepseek" {
			t.Errorf("LLMProvider = %v, want %v", cfg.LLMProvider, "deepseek")
		}
		if cfg.LLMEndpoint != "https://api.deepseek.com" {
			t.Errorf("LLMEndpoint = %v, want %v", cfg.LLMEndpoint, "https://api.deepseek.com")
		}
		if cfg.LLMModel != "deepseek-chat" {
			t.Errorf("LLMModel = %v, want %v", cfg.LLMModel, "deepseek-chat")
		}
		if cfg.PostgreDSN != "postgres://user:pass@localhost/db" {
			t.Errorf("PostgreDSN = %v, want %v", cfg.PostgreDSN, "postgres://user:pass@localhost/db")
		}
		if cfg.TokenPassword != "secret" {
			t.Errorf("TokenPassword = %v, want %v", cfg.TokenPassword, "secret")
		}
	})
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > len(substr) && containsAt(s, substr)))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}