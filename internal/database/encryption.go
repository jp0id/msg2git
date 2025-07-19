package database

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

// EncryptionManager handles encryption and decryption of sensitive data
type EncryptionManager struct {
	key []byte
}

// NewEncryptionManager creates a new encryption manager with the given password
func NewEncryptionManager(password string) *EncryptionManager {
	if password == "" {
		return nil // No encryption if no password provided
	}
	
	// Create a 32-byte key from password using SHA256
	hash := sha256.Sum256([]byte(password))
	return &EncryptionManager{
		key: hash[:],
	}
}

// Encrypt encrypts plaintext using AES-GCM
func (em *EncryptionManager) Encrypt(plaintext string) (string, error) {
	if em == nil || plaintext == "" {
		return plaintext, nil // Return as-is if no encryption manager or empty text
	}

	block, err := aes.NewCipher(em.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// GCM mode provides authenticated encryption
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Create a random nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt the data
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// Encode to base64 for storage
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts ciphertext using AES-GCM
func (em *EncryptionManager) Decrypt(ciphertext string) (string, error) {
	if em == nil || ciphertext == "" {
		return ciphertext, nil // Return as-is if no encryption manager or empty text
	}

	// Decode from base64
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	block, err := aes.NewCipher(em.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	// Extract nonce and ciphertext
	nonce, ciphertext_bytes := data[:nonceSize], data[nonceSize:]

	// Decrypt the data
	plaintext, err := gcm.Open(nil, nonce, ciphertext_bytes, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

// IsEncrypted checks if the encryption manager is available
func (em *EncryptionManager) IsEncrypted() bool {
	return em != nil
}