package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/msg2git/msg2git/internal/config"
)

type Client struct {
	cfg           *config.Config
	geminiClient  *GeminiSDKClient
}

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatResponse struct {
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

type Choice struct {
	Message Message `json:"message"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func NewClient(cfg *config.Config) *Client {
	client := &Client{cfg: cfg}
	
	// Initialize Gemini client if provider is gemini
	if cfg != nil && cfg.HasLLMConfig() && strings.ToLower(cfg.LLMProvider) == "gemini" {
		if geminiClient, err := NewGeminiSDKClient(cfg); err == nil {
			client.geminiClient = geminiClient
		}
		// Note: If Gemini client initialization fails (e.g., invalid API key),
		// we silently continue without it. The client will fall back to HTTP-based API calls.
	}
	
	return client
}

func (c *Client) ProcessMessage(message string) (string, *Usage, error) {
	if c.cfg == nil || !c.cfg.HasLLMConfig() {
		return "", nil, nil
	}

	// Use Gemini client if available
	if c.geminiClient != nil {
		ctx := context.Background()
		content, usage, err := c.geminiClient.ProcessMessage(ctx, message)
		return content, usage, err
	}

	// Fallback to OpenAI-compatible API (for Deepseek, etc.)
	prompt := fmt.Sprintf("Generate a short title (2-4 words) and exactly 2 hashtags for this message. Return ONLY in this exact format: title|#tag1 #tag2\n\nDo not include any explanations, comments, or additional text.\n\nMessage: %s", message)
	
	reqBody := ChatRequest{
		Model: c.cfg.LLMModel,
		Messages: []Message{
			{
				Role:    "user",
				Content: prompt,
			},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return message, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", c.cfg.LLMEndpoint+"/chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return message, nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.LLMToken)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return message, nil, fmt.Errorf("failed to send request to %s: %w", req.URL.String(), err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return message, nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return message, nil, fmt.Errorf("LLM API returned status %d: %s", resp.StatusCode, string(body))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return message, nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return message, nil, fmt.Errorf("no choices in LLM response")
	}

	return chatResp.Choices[0].Message.Content, chatResp.Usage, nil
}

// Close cleans up the client resources
func (c *Client) Close() error {
	if c.geminiClient != nil {
		return c.geminiClient.Close()
	}
	return nil
}

// GenerateTitle generates a title using the appropriate client
func (c *Client) GenerateTitle(message string) (string, *Usage, error) {
	if c.cfg == nil || !c.cfg.HasLLMConfig() {
		return "", nil, nil
	}

	// Use Gemini client if available
	if c.geminiClient != nil {
		ctx := context.Background()
		return c.geminiClient.GenerateTitle(ctx, message)
	}

	// For non-Gemini providers, extract title from ProcessMessage result
	result, usage, err := c.ProcessMessage(message)
	if err != nil || result == "" {
		return "", usage, err
	}

	// Parse title from "title|#tag1 #tag2" format
	parts := strings.Split(result, "|")
	if len(parts) > 0 {
		return strings.TrimSpace(parts[0]), usage, nil
	}

	return "", usage, nil
}

// GenerateHashtags generates hashtags using the appropriate client
func (c *Client) GenerateHashtags(message string) ([]string, *Usage, error) {
	if c.cfg == nil || !c.cfg.HasLLMConfig() {
		return nil, nil, nil
	}

	// Use Gemini client if available
	if c.geminiClient != nil {
		ctx := context.Background()
		return c.geminiClient.GenerateHashtags(ctx, message)
	}

	// For non-Gemini providers, extract hashtags from ProcessMessage result
	result, usage, err := c.ProcessMessage(message)
	if err != nil || result == "" {
		return nil, usage, err
	}

	// Parse hashtags from "title|#tag1 #tag2" format
	parts := strings.Split(result, "|")
	if len(parts) < 2 {
		return nil, usage, nil
	}

	hashtagStr := strings.TrimSpace(parts[1])
	words := strings.Fields(hashtagStr)
	
	var hashtags []string
	for _, word := range words {
		if strings.HasPrefix(word, "#") {
			hashtags = append(hashtags, word)
		}
	}

	return hashtags, usage, nil
}

// ProcessImageWithMessage processes an image with optional message using multimodal capabilities
// Currently only supported for Gemini clients
func (c *Client) ProcessImageWithMessage(imageData []byte, message string) (string, *Usage, error) {
	if c.cfg == nil || !c.cfg.HasLLMConfig() {
		return "", nil, nil
	}

	// Use Gemini client if available (multimodal support)
	if c.geminiClient != nil {
		ctx := context.Background()
		return c.geminiClient.ProcessImageWithMessage(ctx, imageData, message)
	}

	// For non-Gemini providers, multimodal is not supported
	// Return nil to indicate no processing was done
	return "", nil, nil
}

// SupportsMultimodal returns true if the current client supports multimodal processing
func (c *Client) SupportsMultimodal() bool {
	return c.geminiClient != nil
}