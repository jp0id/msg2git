package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	TelegramBotToken string
	GitHubToken      string
	GitHubRepo       string
	GitHubUsername   string
	CommitAuthor     string
	LLMProvider      string
	LLMEndpoint      string
	LLMToken         string
	LLMModel         string
	PostgreDSN       string
	TokenPassword    string
	LogLevel         string
	
	// GitHub OAuth configuration
	GitHubOAuthClientID     string
	GitHubOAuthClientSecret string
	GitHubOAuthRedirectURI  string
	
	// Website configuration
	BaseURL string // Base URL for website (e.g., "https://yourdomain.com")
}

func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		return nil, fmt.Errorf("failed to load .env file: %w", err)
	}

	cfg := &Config{
		TelegramBotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		GitHubUsername:   os.Getenv("GITHUB_USERNAME"),
		CommitAuthor:     os.Getenv("COMMIT_AUTHOR"),
		LLMProvider:      os.Getenv("LLM_PROVIDER"),
		LLMEndpoint:      os.Getenv("LLM_ENDPOINT"),
		LLMToken:         os.Getenv("LLM_TOKEN"),
		LLMModel:         os.Getenv("LLM_MODEL"),
		PostgreDSN:       os.Getenv("POSTGRE_DSN"),
		TokenPassword:    os.Getenv("TOKEN_PASSWORD"),
		LogLevel:         getEnvOrDefault("LOG_LEVEL", "info"),
		
		// GitHub OAuth configuration
		GitHubOAuthClientID:     os.Getenv("GITHUB_OAUTH_CLIENT_ID"),
		GitHubOAuthClientSecret: os.Getenv("GITHUB_OAUTH_CLIENT_SECRET"),
		GitHubOAuthRedirectURI:  os.Getenv("GITHUB_OAUTH_REDIRECT_URI"),
		
		// Website configuration
		BaseURL: os.Getenv("BASE_URL"),
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	required := map[string]string{
		"TELEGRAM_BOT_TOKEN": c.TelegramBotToken,
		"GITHUB_USERNAME":    c.GitHubUsername,
		"COMMIT_AUTHOR":      c.CommitAuthor,
	}

	for key, value := range required {
		if value == "" {
			return fmt.Errorf("required environment variable %s is not set", key)
		}
	}

	return nil
}

func (c *Config) HasLLMConfig() bool {
	return c.LLMProvider != "" && c.LLMEndpoint != "" && c.LLMToken != "" && c.LLMModel != ""
}

func (c *Config) HasDatabaseConfig() bool {
	return c.PostgreDSN != ""
}

func (c *Config) HasGitHubOAuthConfig() bool {
	return c.GitHubOAuthClientID != "" && c.GitHubOAuthClientSecret != "" && c.GitHubOAuthRedirectURI != ""
}

// getEnvOrDefault returns the environment variable value or a default value
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}