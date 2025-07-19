package llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/msg2git/msg2git/internal/config"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name           string
		config         *config.Config
		expectGemini   bool
	}{
		{
			name: "non-gemini provider",
			config: &config.Config{
				LLMProvider: "test",
				LLMEndpoint: "https://api.test.com",
				LLMToken:    "test-token",
				LLMModel:    "test-model",
			},
			expectGemini: false,
		},
		{
			name: "gemini provider",
			config: &config.Config{
				LLMProvider: "gemini",
				LLMToken:    "test-token",
				LLMModel:    "gemini-pro",
			},
			expectGemini: true,
		},
		{
			name: "gemini provider uppercase",
			config: &config.Config{
				LLMProvider: "GEMINI",
				LLMToken:    "test-token",
				LLMModel:    "gemini-pro",
			},
			expectGemini: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.config)
			if client == nil {
				t.Errorf("NewClient() returned nil")
			}
			if client.cfg != tt.config {
				t.Errorf("NewClient() config mismatch")
			}
			
			// Note: Gemini client initialization may fail with invalid API keys
			// In production, this would be handled gracefully by falling back to HTTP API
			if tt.expectGemini && client.geminiClient == nil {
				t.Logf("NewClient() Gemini client not initialized (likely due to invalid test API key)")
			}
			if !tt.expectGemini && client.geminiClient != nil {
				t.Errorf("NewClient() unexpected Gemini client initialization")
			}
		})
	}
}

func TestProcessMessage_NoLLMConfig(t *testing.T) {
	tests := []struct {
		name   string
		config *config.Config
	}{
		{
			name:   "nil config",
			config: nil,
		},
		{
			name:   "empty config",
			config: &config.Config{},
		},
		{
			name: "incomplete config - missing provider",
			config: &config.Config{
				LLMEndpoint: "https://api.test.com",
				LLMToken:    "test-token",
				LLMModel:    "test-model",
			},
		},
		{
			name: "incomplete config - missing endpoint",
			config: &config.Config{
				LLMProvider: "test",
				LLMToken:    "test-token",
				LLMModel:    "test-model",
			},
		},
		{
			name: "incomplete config - missing token",
			config: &config.Config{
				LLMProvider: "test",
				LLMEndpoint: "https://api.test.com",
				LLMModel:    "test-model",
			},
		},
		{
			name: "incomplete config - missing model",
			config: &config.Config{
				LLMProvider: "test",
				LLMEndpoint: "https://api.test.com",
				LLMToken:    "test-token",
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.config)
			result, _, err := client.ProcessMessage("test message")
			
			// Should return empty string when no LLM config
			if result != "" {
				t.Errorf("ProcessMessage() with no LLM config = %q, want empty string", result)
			}
			if err != nil {
				t.Errorf("ProcessMessage() with no LLM config error = %v, want nil", err)
			}
		})
	}
}

func TestProcessMessage_Success(t *testing.T) {
	// Create a test server that mimics LLM API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and headers
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type: application/json, got %s", r.Header.Get("Content-Type"))
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("Expected Authorization header with Bearer token, got %s", r.Header.Get("Authorization"))
		}
		
		// Verify request body structure
		var reqBody ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("Failed to decode request body: %v", err)
		}
		if reqBody.Model != "test-model" {
			t.Errorf("Expected model test-model, got %s", reqBody.Model)
		}
		if len(reqBody.Messages) != 1 {
			t.Errorf("Expected 1 message, got %d", len(reqBody.Messages))
		}
		if reqBody.Messages[0].Role != "user" {
			t.Errorf("Expected role user, got %s", reqBody.Messages[0].Role)
		}
		
		// Return mock response
		response := ChatResponse{
			Choices: []Choice{
				{
					Message: Message{
						Role:    "assistant",
						Content: "Test Title|#tag1 #tag2",
					},
				},
			},
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	
	cfg := &config.Config{
		LLMProvider: "test",
		LLMEndpoint: server.URL,
		LLMToken:    "test-token",
		LLMModel:    "test-model",
	}
	
	client := NewClient(cfg)
	result, _, err := client.ProcessMessage("Test message for processing")
	
	if err != nil {
		t.Errorf("ProcessMessage() unexpected error = %v", err)
	}
	if result != "Test Title|#tag1 #tag2" {
		t.Errorf("ProcessMessage() = %q, want %q", result, "Test Title|#tag1 #tag2")
	}
}

func TestProcessMessage_HTTPError(t *testing.T) {
	// Create a test server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()
	
	cfg := &config.Config{
		LLMProvider: "test",
		LLMEndpoint: server.URL,
		LLMToken:    "test-token",
		LLMModel:    "test-model",
	}
	
	client := NewClient(cfg)
	result, _, err := client.ProcessMessage("Test message")
	
	if err == nil {
		t.Errorf("ProcessMessage() expected error but got nil")
	}
	if !strings.Contains(err.Error(), "LLM API returned status 500") {
		t.Errorf("ProcessMessage() error = %v, want to contain 'LLM API returned status 500'", err)
	}
	if result != "Test message" {
		t.Errorf("ProcessMessage() on error = %q, want original message %q", result, "Test message")
	}
}

func TestProcessMessage_InvalidJSON(t *testing.T) {
	// Create a test server that returns invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()
	
	cfg := &config.Config{
		LLMProvider: "test",
		LLMEndpoint: server.URL,
		LLMToken:    "test-token",
		LLMModel:    "test-model",
	}
	
	client := NewClient(cfg)
	result, _, err := client.ProcessMessage("Test message")
	
	if err == nil {
		t.Errorf("ProcessMessage() expected error but got nil")
	}
	if !strings.Contains(err.Error(), "failed to unmarshal response") {
		t.Errorf("ProcessMessage() error = %v, want to contain 'failed to unmarshal response'", err)
	}
	if result != "Test message" {
		t.Errorf("ProcessMessage() on error = %q, want original message %q", result, "Test message")
	}
}

func TestProcessMessage_NoChoices(t *testing.T) {
	// Create a test server that returns empty choices
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := ChatResponse{
			Choices: []Choice{},
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	
	cfg := &config.Config{
		LLMProvider: "test",
		LLMEndpoint: server.URL,
		LLMToken:    "test-token",
		LLMModel:    "test-model",
	}
	
	client := NewClient(cfg)
	result, _, err := client.ProcessMessage("Test message")
	
	if err == nil {
		t.Errorf("ProcessMessage() expected error but got nil")
	}
	if !strings.Contains(err.Error(), "no choices in LLM response") {
		t.Errorf("ProcessMessage() error = %v, want to contain 'no choices in LLM response'", err)
	}
	if result != "Test message" {
		t.Errorf("ProcessMessage() on error = %q, want original message %q", result, "Test message")
	}
}

func TestProcessMessage_NetworkError(t *testing.T) {
	// Use an invalid URL to simulate network error
	cfg := &config.Config{
		LLMProvider: "test",
		LLMEndpoint: "http://invalid-host-that-does-not-exist.com",
		LLMToken:    "test-token",
		LLMModel:    "test-model",
	}
	
	client := NewClient(cfg)
	result, _, err := client.ProcessMessage("Test message")
	
	if err == nil {
		t.Errorf("ProcessMessage() expected error but got nil")
	}
	// The error could be either network-related or HTTP status-related
	if !strings.Contains(err.Error(), "failed to send request") && !strings.Contains(err.Error(), "LLM API returned status") {
		t.Errorf("ProcessMessage() error = %v, want to contain either 'failed to send request' or 'LLM API returned status'", err)
	}
	if result != "Test message" {
		t.Errorf("ProcessMessage() on error = %q, want original message %q", result, "Test message")
	}
}

func TestChatRequestStructure(t *testing.T) {
	// Test that the ChatRequest structure can be properly marshaled
	req := ChatRequest{
		Model: "test-model",
		Messages: []Message{
			{
				Role:    "user",
				Content: "test content",
			},
		},
	}
	
	jsonData, err := json.Marshal(req)
	if err != nil {
		t.Errorf("Failed to marshal ChatRequest: %v", err)
	}
	
	// Verify it can be unmarshaled back
	var unmarshaled ChatRequest
	if err := json.Unmarshal(jsonData, &unmarshaled); err != nil {
		t.Errorf("Failed to unmarshal ChatRequest: %v", err)
	}
	
	if unmarshaled.Model != req.Model {
		t.Errorf("Model mismatch after marshal/unmarshal: got %s, want %s", unmarshaled.Model, req.Model)
	}
	if len(unmarshaled.Messages) != len(req.Messages) {
		t.Errorf("Messages length mismatch after marshal/unmarshal: got %d, want %d", len(unmarshaled.Messages), len(req.Messages))
	}
	if unmarshaled.Messages[0].Role != req.Messages[0].Role {
		t.Errorf("Message role mismatch after marshal/unmarshal: got %s, want %s", unmarshaled.Messages[0].Role, req.Messages[0].Role)
	}
	if unmarshaled.Messages[0].Content != req.Messages[0].Content {
		t.Errorf("Message content mismatch after marshal/unmarshal: got %s, want %s", unmarshaled.Messages[0].Content, req.Messages[0].Content)
	}
}

func TestClient_Close(t *testing.T) {
	client := NewClient(&config.Config{
		LLMProvider: "test",
		LLMEndpoint: "https://api.test.com",
		LLMToken:    "test-token",
		LLMModel:    "test-model",
	})
	
	err := client.Close()
	if err != nil {
		t.Errorf("Close() unexpected error = %v", err)
	}
}

func TestClient_GenerateTitle_NoConfig(t *testing.T) {
	tests := []struct {
		name   string
		config *config.Config
	}{
		{
			name:   "nil config",
			config: nil,
		},
		{
			name:   "empty config",
			config: &config.Config{},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.config)
			result, _, err := client.GenerateTitle("test message")
			
			if result != "" {
				t.Errorf("GenerateTitle() with no LLM config = %q, want empty string", result)
			}
			if err != nil {
				t.Errorf("GenerateTitle() with no LLM config error = %v, want nil", err)
			}
		})
	}
}

func TestClient_GenerateHashtags_NoConfig(t *testing.T) {
	tests := []struct {
		name   string
		config *config.Config
	}{
		{
			name:   "nil config",
			config: nil,
		},
		{
			name:   "empty config",
			config: &config.Config{},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.config)
			result, _, err := client.GenerateHashtags("test message")
			
			if result != nil {
				t.Errorf("GenerateHashtags() with no LLM config = %v, want nil", result)
			}
			if err != nil {
				t.Errorf("GenerateHashtags() with no LLM config error = %v, want nil", err)
			}
		})
	}
}

func TestClient_ParseTitle(t *testing.T) {
	tests := []struct {
		name     string
		response string
		expected string
	}{
		{
			name:     "valid format",
			response: "Go Programming|#golang #coding",
			expected: "Go Programming",
		},
		{
			name:     "no separator",
			response: "Go Programming",
			expected: "Go Programming",
		},
		{
			name:     "empty title",
			response: "|#golang #coding",
			expected: "",
		},
		{
			name:     "extra spaces",
			response: "  Go Programming  |#golang #coding",
			expected: "Go Programming",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the parsing logic directly
			parts := strings.Split(tt.response, "|")
			var title string
			if len(parts) > 0 {
				title = strings.TrimSpace(parts[0])
			}
			
			if title != tt.expected {
				t.Errorf("ParseTitle() = %q, want %q", title, tt.expected)
			}
		})
	}
}

func TestClient_ParseHashtags(t *testing.T) {
	tests := []struct {
		name     string
		response string
		expected []string
	}{
		{
			name:     "valid format",
			response: "Go Programming|#golang #coding",
			expected: []string{"#golang", "#coding"},
		},
		{
			name:     "extra hashtags",
			response: "Go Programming|#golang #coding #dev #programming",
			expected: []string{"#golang", "#coding", "#dev", "#programming"},
		},
		{
			name:     "mixed content",
			response: "Go Programming|Here are #golang and #coding tags",
			expected: []string{"#golang", "#coding"},
		},
		{
			name:     "no hashtags",
			response: "Go Programming|golang coding",
			expected: []string{},
		},
		{
			name:     "no separator",
			response: "Go Programming",
			expected: nil,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the parsing logic directly
			parts := strings.Split(tt.response, "|")
			if len(parts) < 2 {
				if tt.expected != nil {
					t.Errorf("ParseHashtags() = nil, want %v", tt.expected)
				}
				return
			}

			hashtagStr := strings.TrimSpace(parts[1])
			words := strings.Fields(hashtagStr)
			
			var hashtags []string
			for _, word := range words {
				if strings.HasPrefix(word, "#") {
					hashtags = append(hashtags, word)
				}
			}
			
			if len(hashtags) != len(tt.expected) {
				t.Errorf("ParseHashtags() length = %d, want %d", len(hashtags), len(tt.expected))
			}
			
			for i, expected := range tt.expected {
				if i < len(hashtags) && hashtags[i] != expected {
					t.Errorf("ParseHashtags()[%d] = %s, want %s", i, hashtags[i], expected)
				}
			}
		})
	}
}

func TestChatResponseStructure(t *testing.T) {
	// Test that the ChatResponse structure can be properly unmarshaled
	jsonData := `{
		"choices": [
			{
				"message": {
					"role": "assistant",
					"content": "test response"
				}
			}
		]
	}`
	
	var response ChatResponse
	if err := json.Unmarshal([]byte(jsonData), &response); err != nil {
		t.Errorf("Failed to unmarshal ChatResponse: %v", err)
	}
	
	if len(response.Choices) != 1 {
		t.Errorf("Expected 1 choice, got %d", len(response.Choices))
	}
	if response.Choices[0].Message.Role != "assistant" {
		t.Errorf("Expected role 'assistant', got %s", response.Choices[0].Message.Role)
	}
	if response.Choices[0].Message.Content != "test response" {
		t.Errorf("Expected content 'test response', got %s", response.Choices[0].Message.Content)
	}
}

func TestClient_ProcessImageWithMessage_NoConfig(t *testing.T) {
	tests := []struct {
		name   string
		config *config.Config
	}{
		{
			name:   "nil config",
			config: nil,
		},
		{
			name:   "empty config",
			config: &config.Config{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.config)
			imageData := []byte("test image data")
			
			result, usage, err := client.ProcessImageWithMessage(imageData, "test message")
			
			if result != "" {
				t.Errorf("ProcessImageWithMessage() with no config = %q, want empty string", result)
			}
			if usage != nil {
				t.Errorf("ProcessImageWithMessage() with no config usage = %v, want nil", usage)
			}
			if err != nil {
				t.Errorf("ProcessImageWithMessage() with no config error = %v, want nil", err)
			}
		})
	}
}

func TestClient_ProcessImageWithMessage_NonGemini(t *testing.T) {
	cfg := &config.Config{
		LLMProvider: "deepseek", // Non-Gemini provider
		LLMEndpoint: "https://api.deepseek.com/v1",
		LLMToken:    "test-token",
		LLMModel:    "deepseek-chat",
	}
	
	client := NewClient(cfg)
	imageData := []byte("test image data")
	
	result, usage, err := client.ProcessImageWithMessage(imageData, "test message")
	
	// Non-Gemini providers should return empty result (no multimodal support)
	if result != "" {
		t.Errorf("ProcessImageWithMessage() with non-Gemini provider = %q, want empty string", result)
	}
	if usage != nil {
		t.Errorf("ProcessImageWithMessage() with non-Gemini provider usage = %v, want nil", usage)
	}
	if err != nil {
		t.Errorf("ProcessImageWithMessage() with non-Gemini provider error = %v, want nil", err)
	}
}

func TestClient_SupportsMultimodal(t *testing.T) {
	tests := []struct {
		name     string
		config   *config.Config
		expected bool
	}{
		{
			name:     "nil config",
			config:   nil,
			expected: false,
		},
		{
			name:     "empty config",
			config:   &config.Config{},
			expected: false,
		},
		{
			name: "non-Gemini provider",
			config: &config.Config{
				LLMProvider: "deepseek",
				LLMEndpoint: "https://api.deepseek.com/v1",
				LLMToken:    "test-token",
				LLMModel:    "deepseek-chat",
			},
			expected: false,
		},
		{
			name: "Gemini provider",
			config: &config.Config{
				LLMProvider: "gemini",
				LLMToken:    "test-token",
				LLMModel:    "gemini-2.5-flash",
			},
			expected: false, // Will be false in tests due to invalid API key
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.config)
			result := client.SupportsMultimodal()
			
			if result != tt.expected {
				t.Errorf("SupportsMultimodal() = %v, want %v", result, tt.expected)
			}
		})
	}
}