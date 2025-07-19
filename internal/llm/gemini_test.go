package llm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/msg2git/msg2git/internal/config"
)

func TestNewGeminiSDKClient(t *testing.T) {
	tests := []struct {
		name        string
		config      *config.Config
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil config",
			config:      nil,
			expectError: true,
			errorMsg:    "gemini API key is required",
		},
		{
			name: "empty API key",
			config: &config.Config{
				LLMToken: "",
				LLMModel: "gemini-2.5-flash",
			},
			expectError: true,
			errorMsg:    "gemini API key is required",
		},
		{
			name: "valid config",
			config: &config.Config{
				LLMToken: "test-api-key",
				LLMModel: "gemini-2.5-flash",
			},
			expectError: false, // SDK client can be created with any key, errors happen during API calls
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewGeminiSDKClient(tt.config)

			if tt.expectError {
				if err == nil {
					t.Errorf("NewGeminiSDKClient() expected error but got nil")
				}
				if err != nil && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("NewGeminiSDKClient() error = %v, want to contain %q", err, tt.errorMsg)
				}
				if client != nil {
					t.Errorf("NewGeminiSDKClient() expected nil client on error, got %v", client)
				}
			} else {
				if err != nil {
					t.Errorf("NewGeminiSDKClient() unexpected error = %v", err)
				}
				if client == nil {
					t.Errorf("NewGeminiSDKClient() returned nil client")
				}
			}
		})
	}
}

func TestGeminiSDKClient_ProcessMessage_NilClient(t *testing.T) {
	client := &GeminiSDKClient{
		client:    nil,
		modelName: "",
		apiKey:    "",
	}

	ctx := context.Background()
	result, _, err := client.ProcessMessage(ctx, "test message")

	if err == nil {
		t.Errorf("ProcessMessage() with nil llm expected error but got nil")
	}
	if !strings.Contains(err.Error(), "gemini SDK client not initialized") {
		t.Errorf("ProcessMessage() error = %v, want to contain 'gemini SDK client not initialized'", err)
	}
	if result != "" {
		t.Errorf("ProcessMessage() with nil llm = %q, want empty string", result)
	}
}

func TestGeminiSDKClient_GenerateTitle_NilClient(t *testing.T) {
	client := &GeminiSDKClient{
		client:    nil,
		modelName: "",
		apiKey:    "",
	}

	ctx := context.Background()
	result, _, err := client.GenerateTitle(ctx, "test message")

	if err == nil {
		t.Errorf("GenerateTitle() with nil llm expected error but got nil")
	}
	if !strings.Contains(err.Error(), "gemini SDK client not initialized") {
		t.Errorf("GenerateTitle() error = %v, want to contain 'gemini SDK client not initialized'", err)
	}
	if result != "" {
		t.Errorf("GenerateTitle() with nil llm = %q, want empty string", result)
	}
}

func TestGeminiSDKClient_GenerateHashtags_NilClient(t *testing.T) {
	client := &GeminiSDKClient{
		client:    nil,
		modelName: "",
		apiKey:    "",
	}

	ctx := context.Background()
	result, _, err := client.GenerateHashtags(ctx, "test message")

	if err == nil {
		t.Errorf("GenerateHashtags() with nil llm expected error but got nil")
	}
	if !strings.Contains(err.Error(), "gemini SDK client not initialized") {
		t.Errorf("GenerateHashtags() error = %v, want to contain 'gemini SDK client not initialized'", err)
	}
	if result != nil {
		t.Errorf("GenerateHashtags() with nil llm = %v, want nil", result)
	}
}

func TestGeminiSDKClient_Close(t *testing.T) {
	client := &GeminiSDKClient{
		client:    nil,
		modelName: "",
		apiKey:    "",
	}

	err := client.Close()
	if err != nil {
		t.Errorf("Close() unexpected error = %v", err)
	}
}

func TestGeminiSDKClient_GenerateHashtags_ParseResponse(t *testing.T) {
	tests := []struct {
		name           string
		mockResponse   string
		expectedLength int
		expectedTags   []string
	}{
		{
			name:           "valid two hashtags",
			mockResponse:   "#coding #golang",
			expectedLength: 2,
			expectedTags:   []string{"#coding", "#golang"},
		},
		{
			name:           "extra hashtags - should limit to 2",
			mockResponse:   "#coding #golang #programming #dev",
			expectedLength: 2,
			expectedTags:   []string{"#coding", "#golang"},
		},
		{
			name:           "one hashtag - should pad to 2",
			mockResponse:   "#coding",
			expectedLength: 2,
			expectedTags:   []string{"#coding", "#note"},
		},
		{
			name:           "no hashtags - should add defaults",
			mockResponse:   "coding golang",
			expectedLength: 2,
			expectedTags:   []string{"#note", "#note"},
		},
		{
			name:           "mixed content with hashtags",
			mockResponse:   "Here are some tags: #coding #golang for your project",
			expectedLength: 2,
			expectedTags:   []string{"#coding", "#golang"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: We're testing the parsing logic directly, not the actual API call
			// Simulate hashtag parsing logic
			content := strings.TrimSpace(tt.mockResponse)
			var hashtags []string
			words := strings.Fields(content)
			for _, word := range words {
				if strings.HasPrefix(word, "#") {
					hashtags = append(hashtags, word)
				}
			}

			// Apply the same logic as in GenerateHashtags
			if len(hashtags) < 2 {
				for len(hashtags) < 2 {
					hashtags = append(hashtags, "#note")
				}
			} else if len(hashtags) > 2 {
				hashtags = hashtags[:2]
			}

			if len(hashtags) != tt.expectedLength {
				t.Errorf("Parsed hashtags length = %d, want %d", len(hashtags), tt.expectedLength)
			}

			for i, expected := range tt.expectedTags {
				if i < len(hashtags) && hashtags[i] != expected {
					t.Errorf("Hashtag[%d] = %s, want %s", i, hashtags[i], expected)
				}
			}
		})
	}
}

// Integration test that requires a real API key
// This test is skipped by default and only runs when GEMINI_API_KEY is set
func TestGeminiSDKClient_Integration(t *testing.T) {
	apiKey := getTestAPIKey()
	if apiKey == "" {
		t.Skip("Skipping integration test: GEMINI_API_KEY not set")
	}

	cfg := &config.Config{
		LLMToken: apiKey,
		LLMModel: "gemini-2.5-flash",
	}

	client, err := NewGeminiSDKClient(cfg)
	if err != nil {
		t.Fatalf("NewGeminiSDKClient() error = %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test ProcessMessage
	t.Run("ProcessMessage", func(t *testing.T) {
		result, _, err := client.ProcessMessage(ctx, "I love programming in Go language")
		if err != nil {
			t.Errorf("ProcessMessage() error = %v", err)
		}
		if result == "" {
			t.Errorf("ProcessMessage() returned empty result")
		}
		if !strings.Contains(result, "|") {
			t.Errorf("ProcessMessage() result should contain '|' separator, got %s", result)
		}
		t.Logf("ProcessMessage result: %s", result)
	})

	// Test GenerateTitle
	t.Run("GenerateTitle", func(t *testing.T) {
		title, _, err := client.GenerateTitle(ctx, "I love programming in Go language")
		if err != nil {
			t.Errorf("GenerateTitle() error = %v", err)
		}
		if title == "" {
			t.Errorf("GenerateTitle() returned empty title")
		}
		t.Logf("GenerateTitle result: %s", title)
	})

	// Test GenerateHashtags
	t.Run("GenerateHashtags", func(t *testing.T) {
		hashtags, _, err := client.GenerateHashtags(ctx, "I love programming in Go language")
		if err != nil {
			t.Errorf("GenerateHashtags() error = %v", err)
		}
		if len(hashtags) != 2 {
			t.Errorf("GenerateHashtags() returned %d hashtags, want 2", len(hashtags))
		}
		for i, tag := range hashtags {
			if !strings.HasPrefix(tag, "#") {
				t.Errorf("Hashtag[%d] = %s, should start with #", i, tag)
			}
		}
		t.Logf("GenerateHashtags result: %v", hashtags)
	})
}

// getTestAPIKey gets the API key from environment variable for integration tests
func getTestAPIKey() string {
	// In a real test environment, you would get this from an environment variable
	// For now, return empty to skip integration tests by default
	return os.Getenv("GEMINI_API_KEY")
	// return ""
}

func TestGeminiSDKClient_ContextCancellation(t *testing.T) {
	apiKey := getTestAPIKey()
	if apiKey == "" {
		t.Skip("Skipping context cancellation test: GEMINI_API_KEY not set")
	}

	cfg := &config.Config{
		LLMToken: apiKey,
		LLMModel: "gemini-2.5-flash",
	}

	client, err := NewGeminiSDKClient(cfg)
	if err != nil {
		t.Fatalf("NewGeminiSDKClient() error = %v", err)
	}
	defer client.Close()

	// Create a context that's immediately cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// These should handle context cancellation gracefully
	// Note: Actual behavior depends on langchain-go implementation
	_, _, err = client.ProcessMessage(ctx, "test message")
	if err != nil && !strings.Contains(err.Error(), "context") {
		// Context cancellation error is expected, but we don't strictly require it
		// depending on langchain-go's implementation
		t.Logf("ProcessMessage with cancelled context: %v", err)
	}
}

func TestGeminiSDKClient_EmptyMessage(t *testing.T) {
	apiKey := getTestAPIKey()
	if apiKey == "" {
		t.Skip("Skipping empty message test: GEMINI_API_KEY not set")
	}

	cfg := &config.Config{
		LLMToken: apiKey,
		LLMModel: "gemini-2.5-flash",
	}

	client, err := NewGeminiSDKClient(cfg)
	if err != nil {
		t.Fatalf("NewGeminiSDKClient() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Test with empty message
	result, _, err := client.ProcessMessage(ctx, "")
	// The behavior with empty message depends on the actual API
	// We just ensure it doesn't panic
	t.Logf("ProcessMessage with empty message - result: %s, error: %v", result, err)
}

func TestGeminiSDKClient_ProcessImageWithMessage_NilClient(t *testing.T) {
	client := &GeminiSDKClient{
		client:    nil,
		modelName: "",
		apiKey:    "",
	}

	ctx := context.Background()
	imageData := []byte("fake image data")
	result, _, err := client.ProcessImageWithMessage(ctx, imageData, "test message")

	if err == nil {
		t.Errorf("ProcessImageWithMessage() with nil client expected error but got nil")
	}
	if !strings.Contains(err.Error(), "gemini SDK client not initialized") {
		t.Errorf("ProcessImageWithMessage() error = %v, want to contain 'gemini SDK client not initialized'", err)
	}
	if result != "" {
		t.Errorf("ProcessImageWithMessage() with nil client = %q, want empty string", result)
	}
}

func TestGeminiSDKClient_ProcessImageWithMessage_Integration(t *testing.T) {
	apiKey := getTestAPIKey()
	if apiKey == "" {
		t.Skip("Skipping multimodal integration test: GEMINI_API_KEY not set")
	}

	cfg := &config.Config{
		LLMToken: apiKey,
		LLMModel: "gemini-2.5-flash",
	}

	client, err := NewGeminiSDKClient(cfg)
	if err != nil {
		t.Fatalf("NewGeminiSDKClient() error = %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a simple test image (1x1 pixel red PNG)
	testImageData := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xDE, 0x00, 0x00, 0x00,
		0x0C, 0x49, 0x44, 0x41, 0x54, 0x08, 0x99, 0x01, 0x01, 0x00, 0x00, 0x00,
		0xFF, 0xFF, 0x00, 0x00, 0x00, 0x00, 0x00, 0x05, 0x00, 0x01, 0x0D, 0x0A,
		0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42,
		0x60, 0x82,
	}

	// Test ProcessImageWithMessage with no message
	t.Run("ProcessImageWithMessage_NoMessage", func(t *testing.T) {
		result, usage, err := client.ProcessImageWithMessage(ctx, testImageData, "")
		if err != nil {
			t.Errorf("ProcessImageWithMessage() error = %v", err)
			return
		}
		if result == "" {
			t.Errorf("ProcessImageWithMessage() returned empty result")
			return
		}
		if !strings.Contains(result, "|") {
			t.Errorf("ProcessImageWithMessage() result should contain '|' separator, got %s", result)
		}
		if usage == nil {
			t.Errorf("ProcessImageWithMessage() should return usage information")
		}
		t.Logf("ProcessImageWithMessage (no message) result: %s", result)
	})

	// Test ProcessImageWithMessage with message
	t.Run("ProcessImageWithMessage_WithMessage", func(t *testing.T) {
		result, usage, err := client.ProcessImageWithMessage(ctx, testImageData, "This is a test image")
		if err != nil {
			t.Errorf("ProcessImageWithMessage() error = %v", err)
			return
		}
		if result == "" {
			t.Errorf("ProcessImageWithMessage() returned empty result")
			return
		}
		if !strings.Contains(result, "|") {
			t.Errorf("ProcessImageWithMessage() result should contain '|' separator, got %s", result)
		}
		if usage == nil {
			t.Errorf("ProcessImageWithMessage() should return usage information")
		}
		t.Logf("ProcessImageWithMessage (with message) result: %s", result)
	})
}

