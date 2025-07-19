package database

import (
	"os"
	"testing"
)

// TestDB_EncryptionIntegration tests the full integration of encryption with database operations
func TestDB_EncryptionIntegration(t *testing.T) {
	dsn := getTestDSN()
	if dsn == "" {
		t.Skip("Skipping database tests - no TEST_POSTGRES_DSN environment variable set")
	}

	testPassword := "integration-test-password-456"
	db, err := NewDB(dsn, testPassword)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	defer db.Close()

	chatID := int64(987654321)
	username := "encryptiontest"
	githubToken := "ghp_testtoken12345678901234567890123456"
	githubRepo := "https://github.com/test/repo.git"
	llmToken := "sk-testllmtoken1234567890abcdef"

	// Clean up first
	db.DeleteUser(chatID)

	t.Run("CreateUserAndEncryptTokens", func(t *testing.T) {
		// Create user
		user, err := db.CreateUser(chatID, username)
		if err != nil {
			t.Fatalf("Failed to create user: %v", err)
		}

		if user.ChatId != chatID {
			t.Errorf("Expected ChatId %d, got %d", chatID, user.ChatId)
		}

		// Update GitHub config with encryption
		err = db.UpdateUserGitHubConfig(chatID, githubToken, githubRepo)
		if err != nil {
			t.Fatalf("Failed to update GitHub config: %v", err)
		}

		// Update LLM config with encryption
		err = db.UpdateUserLLMConfig(chatID, llmToken)
		if err != nil {
			t.Fatalf("Failed to update LLM config: %v", err)
		}

		// Retrieve user and verify decryption
		retrievedUser, err := db.GetUserByChatID(chatID)
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}

		if retrievedUser.GitHubToken != githubToken {
			t.Errorf("Expected GitHub token %s, got %s", githubToken, retrievedUser.GitHubToken)
		}

		if retrievedUser.GitHubRepo != githubRepo {
			t.Errorf("Expected GitHub repo %s, got %s", githubRepo, retrievedUser.GitHubRepo)
		}

		if retrievedUser.LLMToken != llmToken {
			t.Errorf("Expected LLM token %s, got %s", llmToken, retrievedUser.LLMToken)
		}

		// Test configuration check methods
		if !retrievedUser.HasGitHubConfig() {
			t.Error("Expected HasGitHubConfig to return true")
		}

		if !retrievedUser.HasLLMConfig() {
			t.Error("Expected HasLLMConfig to return true")
		}
	})

	t.Run("EncryptionConsistency", func(t *testing.T) {
		// Update tokens multiple times to ensure consistency
		newGitHubToken := "ghp_newtoken987654321098765432109876543"
		newLLMToken := "sk-newllmtoken9876543210fedcba"

		err := db.UpdateUserGitHubConfig(chatID, newGitHubToken, githubRepo)
		if err != nil {
			t.Fatalf("Failed to update GitHub config: %v", err)
		}

		err = db.UpdateUserLLMConfig(chatID, newLLMToken)
		if err != nil {
			t.Fatalf("Failed to update LLM config: %v", err)
		}

		// Retrieve and verify
		user, err := db.GetUserByChatID(chatID)
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}

		if user.GitHubToken != newGitHubToken {
			t.Errorf("Expected new GitHub token %s, got %s", newGitHubToken, user.GitHubToken)
		}

		if user.LLMToken != newLLMToken {
			t.Errorf("Expected new LLM token %s, got %s", newLLMToken, user.LLMToken)
		}
	})

	// Clean up
	db.DeleteUser(chatID)
}

// TestDB_NoEncryption tests database operations without encryption
func TestDB_NoEncryption(t *testing.T) {
	dsn := getTestDSN()
	if dsn == "" {
		t.Skip("Skipping database tests - no TEST_POSTGRES_DSN environment variable set")
	}

	// Create database without encryption password
	db, err := NewDB(dsn, "")
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	defer db.Close()

	chatID := int64(111222333)
	username := "noencryption"
	githubToken := "ghp_plaintext12345678901234567890123456"
	githubRepo := "https://github.com/test/plain.git"
	llmToken := "sk-plaintexttoken1234567890abcdef"

	// Clean up first
	db.DeleteUser(chatID)

	t.Run("PlaintextOperations", func(t *testing.T) {
		// Create user
		_, err := db.CreateUser(chatID, username)
		if err != nil {
			t.Fatalf("Failed to create user: %v", err)
		}

		// Update configs
		err = db.UpdateUserGitHubConfig(chatID, githubToken, githubRepo)
		if err != nil {
			t.Fatalf("Failed to update GitHub config: %v", err)
		}

		err = db.UpdateUserLLMConfig(chatID, llmToken)
		if err != nil {
			t.Fatalf("Failed to update LLM config: %v", err)
		}

		// Retrieve and verify
		retrievedUser, err := db.GetUserByChatID(chatID)
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}

		if retrievedUser.GitHubToken != githubToken {
			t.Errorf("Expected GitHub token %s, got %s", githubToken, retrievedUser.GitHubToken)
		}

		if retrievedUser.LLMToken != llmToken {
			t.Errorf("Expected LLM token %s, got %s", llmToken, retrievedUser.LLMToken)
		}
	})

	// Clean up
	db.DeleteUser(chatID)
}

// TestDB_MultipleUsers tests operations with multiple users
func TestDB_MultipleUsers(t *testing.T) {
	dsn := getTestDSN()
	if dsn == "" {
		t.Skip("Skipping database tests - no TEST_POSTGRES_DSN environment variable set")
	}

	testPassword := "multiuser-test-password"
	db, err := NewDB(dsn, testPassword)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	defer db.Close()

	// Test data for multiple users
	users := []struct {
		chatID      int64
		username    string
		githubToken string
		githubRepo  string
		llmToken    string
	}{
		{444555666, "user1", "ghp_user1token123", "https://github.com/user1/repo1.git", "sk-user1llm123"},
		{777888999, "user2", "ghp_user2token456", "https://github.com/user2/repo2.git", "sk-user2llm456"},
		{111333555, "user3", "ghp_user3token789", "https://github.com/user3/repo3.git", "sk-user3llm789"},
	}

	// Clean up first
	for _, userData := range users {
		db.DeleteUser(userData.chatID)
	}

	t.Run("CreateMultipleUsers", func(t *testing.T) {
		for _, userData := range users {
			// Create user
			_, err := db.CreateUser(userData.chatID, userData.username)
			if err != nil {
				t.Fatalf("Failed to create user %s: %v", userData.username, err)
			}

			// Update configs
			err = db.UpdateUserGitHubConfig(userData.chatID, userData.githubToken, userData.githubRepo)
			if err != nil {
				t.Fatalf("Failed to update GitHub config for %s: %v", userData.username, err)
			}

			err = db.UpdateUserLLMConfig(userData.chatID, userData.llmToken)
			if err != nil {
				t.Fatalf("Failed to update LLM config for %s: %v", userData.username, err)
			}
		}
	})

	t.Run("VerifyUserIsolation", func(t *testing.T) {
		for _, expectedUser := range users {
			// Get user and verify data isolation
			user, err := db.GetUserByChatID(expectedUser.chatID)
			if err != nil {
				t.Fatalf("Failed to get user %s: %v", expectedUser.username, err)
			}

			if user.Username != expectedUser.username {
				t.Errorf("Expected username %s, got %s", expectedUser.username, user.Username)
			}

			if user.GitHubToken != expectedUser.githubToken {
				t.Errorf("User %s: Expected GitHub token %s, got %s", expectedUser.username, expectedUser.githubToken, user.GitHubToken)
			}

			if user.GitHubRepo != expectedUser.githubRepo {
				t.Errorf("User %s: Expected GitHub repo %s, got %s", expectedUser.username, expectedUser.githubRepo, user.GitHubRepo)
			}

			if user.LLMToken != expectedUser.llmToken {
				t.Errorf("User %s: Expected LLM token %s, got %s", expectedUser.username, expectedUser.llmToken, user.LLMToken)
			}

			if !user.HasGitHubConfig() {
				t.Errorf("User %s should have GitHub config", expectedUser.username)
			}

			if !user.HasLLMConfig() {
				t.Errorf("User %s should have LLM config", expectedUser.username)
			}
		}
	})

	t.Run("UpdateOneUserDoesNotAffectOthers", func(t *testing.T) {
		// Update first user's tokens
		newGitHubToken := "ghp_updated_token_for_user1"
		newLLMToken := "sk-updated_llm_token_for_user1"

		err := db.UpdateUserGitHubConfig(users[0].chatID, newGitHubToken, users[0].githubRepo)
		if err != nil {
			t.Fatalf("Failed to update user1 GitHub config: %v", err)
		}

		err = db.UpdateUserLLMConfig(users[0].chatID, newLLMToken)
		if err != nil {
			t.Fatalf("Failed to update user1 LLM config: %v", err)
		}

		// Verify first user has new tokens
		user1, err := db.GetUserByChatID(users[0].chatID)
		if err != nil {
			t.Fatalf("Failed to get user1: %v", err)
		}

		if user1.GitHubToken != newGitHubToken {
			t.Errorf("User1 should have updated GitHub token")
		}

		if user1.LLMToken != newLLMToken {
			t.Errorf("User1 should have updated LLM token")
		}

		// Verify other users still have original tokens
		for i := 1; i < len(users); i++ {
			user, err := db.GetUserByChatID(users[i].chatID)
			if err != nil {
				t.Fatalf("Failed to get user%d: %v", i+1, err)
			}

			if user.GitHubToken != users[i].githubToken {
				t.Errorf("User%d should still have original GitHub token", i+1)
			}

			if user.LLMToken != users[i].llmToken {
				t.Errorf("User%d should still have original LLM token", i+1)
			}
		}
	})

	// Clean up
	for _, userData := range users {
		db.DeleteUser(userData.chatID)
	}
}

// TestDB_ErrorHandling tests error conditions
func TestDB_ErrorHandling(t *testing.T) {
	dsn := getTestDSN()
	if dsn == "" {
		t.Skip("Skipping database tests - no TEST_POSTGRES_DSN environment variable set")
	}

	testPassword := "error-test-password"
	db, err := NewDB(dsn, testPassword)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	defer db.Close()

	t.Run("UpdateNonExistentUser", func(t *testing.T) {
		nonExistentChatID := int64(999999999)

		err := db.UpdateUserGitHubConfig(nonExistentChatID, "token", "repo")
		if err == nil {
			t.Error("Expected error when updating non-existent user")
		}

		err = db.UpdateUserLLMConfig(nonExistentChatID, "token")
		if err == nil {
			t.Error("Expected error when updating non-existent user")
		}
	})

	t.Run("DeleteNonExistentUser", func(t *testing.T) {
		nonExistentChatID := int64(888888888)

		err := db.DeleteUser(nonExistentChatID)
		if err == nil {
			t.Error("Expected error when deleting non-existent user")
		}
	})

	t.Run("GetNonExistentUser", func(t *testing.T) {
		nonExistentChatID := int64(777777777)

		user, err := db.GetUserByChatID(nonExistentChatID)
		if err != nil {
			t.Fatalf("Unexpected error for non-existent user: %v", err)
		}
		if user != nil {
			t.Error("Expected nil user for non-existent ID")
		}
	})
}

// getTestDSN gets the test database DSN from environment variable
func getTestDSN() string {
	// You can set this environment variable to run tests with a real database
	// Example: TEST_POSTGRES_DSN=postgresql://user:pass@localhost:5432/testdb go test
	return os.Getenv("TEST_POSTGRES_DSN")
}