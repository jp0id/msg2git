package database

import (
	"strings"
	"testing"
)

// TestEncryptionManager_Basic tests basic encryption and decryption
func TestEncryptionManager_Basic(t *testing.T) {
	password := "test-password-123"
	em := NewEncryptionManager(password)
	
	if em == nil {
		t.Fatal("Expected encryption manager to be created, got nil")
	}
	
	if !em.IsEncrypted() {
		t.Error("Expected IsEncrypted to return true")
	}
	
	plaintext := "hello world"
	
	// Test encryption
	ciphertext, err := em.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Failed to encrypt: %v", err)
	}
	
	if ciphertext == plaintext {
		t.Error("Ciphertext should not equal plaintext")
	}
	
	if ciphertext == "" {
		t.Error("Ciphertext should not be empty")
	}
	
	// Test decryption
	decrypted, err := em.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Failed to decrypt: %v", err)
	}
	
	if decrypted != plaintext {
		t.Errorf("Expected decrypted text '%s', got '%s'", plaintext, decrypted)
	}
}

// TestEncryptionManager_EmptyPassword tests behavior with empty password
func TestEncryptionManager_EmptyPassword(t *testing.T) {
	em := NewEncryptionManager("")
	
	if em != nil {
		t.Error("Expected nil encryption manager with empty password")
	}
	
	plaintext := "hello world"
	
	// Test encryption with nil manager
	ciphertext, err := em.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Unexpected error with nil manager: %v", err)
	}
	
	if ciphertext != plaintext {
		t.Errorf("Expected plaintext to be returned as-is, got %s", ciphertext)
	}
	
	// Test decryption with nil manager
	decrypted, err := em.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Unexpected error with nil manager: %v", err)
	}
	
	if decrypted != plaintext {
		t.Errorf("Expected plaintext to be returned as-is, got %s", decrypted)
	}
	
	// IsEncrypted should return false
	if em.IsEncrypted() {
		t.Error("Expected IsEncrypted to return false for nil manager")
	}
}

// TestEncryptionManager_EmptyText tests encryption of empty strings
func TestEncryptionManager_EmptyText(t *testing.T) {
	password := "test-password-123"
	em := NewEncryptionManager(password)
	
	// Test empty string
	plaintext := ""
	
	ciphertext, err := em.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Failed to encrypt empty string: %v", err)
	}
	
	if ciphertext != plaintext {
		t.Errorf("Expected empty string to be returned as-is, got %s", ciphertext)
	}
	
	decrypted, err := em.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Failed to decrypt empty string: %v", err)
	}
	
	if decrypted != plaintext {
		t.Errorf("Expected empty string to be returned as-is, got %s", decrypted)
	}
}

// TestEncryptionManager_LongText tests encryption of long text
func TestEncryptionManager_LongText(t *testing.T) {
	password := "test-password-123"
	em := NewEncryptionManager(password)
	
	// Create a long text
	plaintext := strings.Repeat("This is a test message with some content. ", 100)
	
	ciphertext, err := em.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Failed to encrypt long text: %v", err)
	}
	
	if ciphertext == plaintext {
		t.Error("Ciphertext should not equal plaintext")
	}
	
	decrypted, err := em.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Failed to decrypt long text: %v", err)
	}
	
	if decrypted != plaintext {
		t.Error("Decrypted text does not match original")
	}
}

// TestEncryptionManager_SpecialCharacters tests encryption of special characters
func TestEncryptionManager_SpecialCharacters(t *testing.T) {
	password := "test-password-123"
	em := NewEncryptionManager(password)
	
	plaintext := "Hello! @#$%^&*()[]{}|\\:;\"'<>?,./`~+=_-😀🔐🚀"
	
	ciphertext, err := em.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Failed to encrypt special characters: %v", err)
	}
	
	decrypted, err := em.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Failed to decrypt special characters: %v", err)
	}
	
	if decrypted != plaintext {
		t.Errorf("Expected '%s', got '%s'", plaintext, decrypted)
	}
}

// TestEncryptionManager_DifferentPasswords tests that different passwords produce different results
func TestEncryptionManager_DifferentPasswords(t *testing.T) {
	plaintext := "secret message"
	
	em1 := NewEncryptionManager("password1")
	em2 := NewEncryptionManager("password2")
	
	ciphertext1, err := em1.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Failed to encrypt with password1: %v", err)
	}
	
	ciphertext2, err := em2.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Failed to encrypt with password2: %v", err)
	}
	
	if ciphertext1 == ciphertext2 {
		t.Error("Different passwords should produce different ciphertexts")
	}
	
	// Try to decrypt with wrong password
	_, err = em2.Decrypt(ciphertext1)
	if err == nil {
		t.Error("Expected error when decrypting with wrong password")
	}
}

// TestEncryptionManager_Consistency tests that multiple encryptions of the same text are different
func TestEncryptionManager_Consistency(t *testing.T) {
	password := "test-password-123"
	em := NewEncryptionManager(password)
	
	plaintext := "same message"
	
	// Encrypt the same message multiple times
	ciphertext1, err := em.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Failed to encrypt (1): %v", err)
	}
	
	ciphertext2, err := em.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Failed to encrypt (2): %v", err)
	}
	
	// Should be different due to random nonce
	if ciphertext1 == ciphertext2 {
		t.Error("Multiple encryptions of same text should produce different ciphertexts due to random nonce")
	}
	
	// But both should decrypt to the same plaintext
	decrypted1, err := em.Decrypt(ciphertext1)
	if err != nil {
		t.Fatalf("Failed to decrypt (1): %v", err)
	}
	
	decrypted2, err := em.Decrypt(ciphertext2)
	if err != nil {
		t.Fatalf("Failed to decrypt (2): %v", err)
	}
	
	if decrypted1 != plaintext || decrypted2 != plaintext {
		t.Error("Both decryptions should produce original plaintext")
	}
}

// TestEncryptionManager_InvalidCiphertext tests error handling for invalid ciphertext
func TestEncryptionManager_InvalidCiphertext(t *testing.T) {
	password := "test-password-123"
	em := NewEncryptionManager(password)
	
	// Test invalid base64
	_, err := em.Decrypt("invalid-base64!")
	if err == nil {
		t.Error("Expected error for invalid base64")
	}
	
	// Test valid base64 but invalid ciphertext
	_, err = em.Decrypt("SGVsbG8gV29ybGQ=") // "Hello World" in base64
	if err == nil {
		t.Error("Expected error for invalid ciphertext")
	}
	
	// Test ciphertext that's too short
	_, err = em.Decrypt("YWJj") // "abc" in base64 (too short)
	if err == nil {
		t.Error("Expected error for ciphertext that's too short")
	}
}

// TestEncryptionManager_GitHubToken tests encryption of GitHub tokens
func TestEncryptionManager_GitHubToken(t *testing.T) {
	password := "my-secure-password"
	em := NewEncryptionManager(password)
	
	// Test typical GitHub token format
	token := "ghp_1234567890abcdef1234567890abcdef12345678"
	
	encrypted, err := em.Encrypt(token)
	if err != nil {
		t.Fatalf("Failed to encrypt GitHub token: %v", err)
	}
	
	if encrypted == token {
		t.Error("Encrypted token should not equal original token")
	}
	
	decrypted, err := em.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Failed to decrypt GitHub token: %v", err)
	}
	
	if decrypted != token {
		t.Errorf("Expected '%s', got '%s'", token, decrypted)
	}
}

// TestEncryptionManager_LLMToken tests encryption of LLM tokens
func TestEncryptionManager_LLMToken(t *testing.T) {
	password := "my-secure-password"
	em := NewEncryptionManager(password)
	
	// Test typical LLM token format
	token := "sk-1234567890abcdef1234567890abcdef1234567890abcdef"
	
	encrypted, err := em.Encrypt(token)
	if err != nil {
		t.Fatalf("Failed to encrypt LLM token: %v", err)
	}
	
	if encrypted == token {
		t.Error("Encrypted token should not equal original token")
	}
	
	decrypted, err := em.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Failed to decrypt LLM token: %v", err)
	}
	
	if decrypted != token {
		t.Errorf("Expected '%s', got '%s'", token, decrypted)
	}
}

// BenchmarkEncryptionManager_Encrypt benchmarks encryption performance
func BenchmarkEncryptionManager_Encrypt(b *testing.B) {
	password := "benchmark-password"
	em := NewEncryptionManager(password)
	plaintext := "This is a test message for benchmarking encryption performance."
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := em.Encrypt(plaintext)
		if err != nil {
			b.Fatalf("Encryption failed: %v", err)
		}
	}
}

// BenchmarkEncryptionManager_Decrypt benchmarks decryption performance
func BenchmarkEncryptionManager_Decrypt(b *testing.B) {
	password := "benchmark-password"
	em := NewEncryptionManager(password)
	plaintext := "This is a test message for benchmarking decryption performance."
	
	// Pre-encrypt the text
	ciphertext, err := em.Encrypt(plaintext)
	if err != nil {
		b.Fatalf("Failed to encrypt for benchmark: %v", err)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := em.Decrypt(ciphertext)
		if err != nil {
			b.Fatalf("Decryption failed: %v", err)
		}
	}
}