package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/msg2git/msg2git/internal/config"
	"github.com/msg2git/msg2git/internal/logger"
	"google.golang.org/genai"
)

// GeminiSDKClient wraps the official Google Gemini Go SDK
type GeminiSDKClient struct {
	client    *genai.Client
	modelName string
	apiKey    string
}

// NewGeminiSDKClient creates a new Gemini client using the official Google SDK
func NewGeminiSDKClient(cfg *config.Config) (*GeminiSDKClient, error) {
	if cfg == nil || cfg.LLMToken == "" {
		return nil, fmt.Errorf("gemini API key is required")
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: cfg.LLMToken,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	return &GeminiSDKClient{
		client:    client,
		modelName: cfg.LLMModel,
		apiKey:    cfg.LLMToken,
	}, nil
}

// ProcessMessage processes a message using Gemini SDK and returns formatted output
func (gc *GeminiSDKClient) ProcessMessage(ctx context.Context, message string) (string, *Usage, error) {
	if gc.client == nil {
		return "", nil, fmt.Errorf("gemini SDK client not initialized")
	}

	prompt := fmt.Sprintf("Generate a short title (2-4 words) and exactly 2 hashtags for this message. Return ONLY in this exact format: title|#tag1 #tag2\n\nDo not include any explanations, comments, or additional text.\n\nMessage: %s", message)

	// Create content for the request
	contents := genai.Text(prompt)

	// Create generation config with thinking disabled
	config := &genai.GenerateContentConfig{
		Temperature:     genai.Ptr(float32(0.1)),
		TopK:            genai.Ptr(float32(1)),
		TopP:            genai.Ptr(float32(0.8)),
		MaxOutputTokens: 100,
		ThinkingConfig: &genai.ThinkingConfig{
			ThinkingBudget:   genai.Ptr(int32(0)), // Disable thinking mode
			IncludeThoughts:  false,
		},
	}

	resp, err := gc.client.Models.GenerateContent(ctx, gc.modelName, contents, config)
	if err != nil {
		return message, nil, fmt.Errorf("failed to generate content: %w", err)
	}

	// Debug: Log the complete response structure
	logger.Debug("Gemini SDK Response", map[string]interface{}{
		"candidates_count": len(resp.Candidates),
		"usage_metadata":   resp.UsageMetadata,
	})

	if len(resp.Candidates) == 0 {
		return message, nil, fmt.Errorf("no candidates in Gemini response")
	}

	// Extract content from the first candidate
	candidate := resp.Candidates[0]
	if candidate.Content == nil || len(candidate.Content.Parts) == 0 {
		return message, nil, fmt.Errorf("no content parts in Gemini response")
	}

	// Get the text content
	var content string
	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			content += part.Text
		}
	}

	content = strings.TrimSpace(content)

	// Extract usage information from SDK response
	var usage *Usage
	if resp.UsageMetadata != nil {
		usage = &Usage{
			PromptTokens:     int(resp.UsageMetadata.PromptTokenCount),
			CompletionTokens: int(resp.UsageMetadata.CandidatesTokenCount),
			TotalTokens:      int(resp.UsageMetadata.TotalTokenCount),
		}

		// Debug: Log extracted usage information
		logger.Debug("Gemini SDK Token Usage Extracted", map[string]interface{}{
			"prompt_tokens":     usage.PromptTokens,
			"completion_tokens": usage.CompletionTokens,
			"total_tokens":      usage.TotalTokens,
		})
	}

	return content, usage, nil
}

// GenerateTitle generates a title for the given message
func (gc *GeminiSDKClient) GenerateTitle(ctx context.Context, message string) (string, *Usage, error) {
	if gc.client == nil {
		return "", nil, fmt.Errorf("gemini SDK client not initialized")
	}

	prompt := fmt.Sprintf("Generate a concise title (2-4 words) for this message. Return ONLY the title without any explanations.\n\nMessage: %s", message)

	// Create content for the request
	contents := genai.Text(prompt)

	// Create generation config with thinking disabled
	config := &genai.GenerateContentConfig{
		Temperature:     genai.Ptr(float32(0.1)),
		TopK:            genai.Ptr(float32(1)),
		TopP:            genai.Ptr(float32(0.8)),
		MaxOutputTokens: 100,
		ThinkingConfig: &genai.ThinkingConfig{
			ThinkingBudget:   genai.Ptr(int32(0)), // Disable thinking mode
			IncludeThoughts:  false,
		},
	}

	resp, err := gc.client.Models.GenerateContent(ctx, gc.modelName, contents, config)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate title: %w", err)
	}

	// Debug: Log the complete response structure for GenerateTitle
	logger.Debug("Gemini SDK GenerateTitle Response", map[string]interface{}{
		"candidates_count": len(resp.Candidates),
		"usage_metadata":   resp.UsageMetadata,
	})

	if len(resp.Candidates) == 0 {
		return "", nil, fmt.Errorf("no candidates in Gemini response")
	}

	// Extract content from the first candidate
	candidate := resp.Candidates[0]
	if candidate.Content == nil || len(candidate.Content.Parts) == 0 {
		return "", nil, fmt.Errorf("no content parts in Gemini response")
	}

	// Get the text content
	var title string
	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			title += part.Text
		}
	}

	title = strings.TrimSpace(title)

	// Extract usage information from SDK response
	var usage *Usage
	if resp.UsageMetadata != nil {
		usage = &Usage{
			PromptTokens:     int(resp.UsageMetadata.PromptTokenCount),
			CompletionTokens: int(resp.UsageMetadata.CandidatesTokenCount),
			TotalTokens:      int(resp.UsageMetadata.TotalTokenCount),
		}

		// Debug: Log extracted usage information
		logger.Debug("Gemini SDK Token Usage Extracted", map[string]interface{}{
			"prompt_tokens":     usage.PromptTokens,
			"completion_tokens": usage.CompletionTokens,
			"total_tokens":      usage.TotalTokens,
		})
	}

	return title, usage, nil
}

// GenerateHashtags generates hashtags for the given message
func (gc *GeminiSDKClient) GenerateHashtags(ctx context.Context, message string) ([]string, *Usage, error) {
	if gc.client == nil {
		return nil, nil, fmt.Errorf("gemini SDK client not initialized")
	}

	prompt := fmt.Sprintf("Generate exactly 2 relevant hashtags for this message. Return ONLY the hashtags separated by spaces, starting with #.\n\nMessage: %s", message)

	// Create content for the request
	contents := genai.Text(prompt)

	// Create generation config with thinking disabled
	config := &genai.GenerateContentConfig{
		Temperature:     genai.Ptr(float32(0.1)),
		TopK:            genai.Ptr(float32(1)),
		TopP:            genai.Ptr(float32(0.8)),
		MaxOutputTokens: 100,
		ThinkingConfig: &genai.ThinkingConfig{
			ThinkingBudget:   genai.Ptr(int32(0)), // Disable thinking mode
			IncludeThoughts:  false,
		},
	}

	resp, err := gc.client.Models.GenerateContent(ctx, gc.modelName, contents, config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate hashtags: %w", err)
	}

	// Debug: Log the complete response structure for GenerateHashtags
	logger.Debug("Gemini SDK GenerateHashtags Response", map[string]interface{}{
		"candidates_count": len(resp.Candidates),
		"usage_metadata":   resp.UsageMetadata,
	})

	if len(resp.Candidates) == 0 {
		return nil, nil, fmt.Errorf("no candidates in Gemini response")
	}

	// Extract content from the first candidate
	candidate := resp.Candidates[0]
	if candidate.Content == nil || len(candidate.Content.Parts) == 0 {
		return nil, nil, fmt.Errorf("no content parts in Gemini response")
	}

	// Get the text content
	var content string
	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			content += part.Text
		}
	}

	content = strings.TrimSpace(content)

	// Parse hashtags from the response
	var hashtags []string
	words := strings.Fields(content)
	for _, word := range words {
		if strings.HasPrefix(word, "#") {
			hashtags = append(hashtags, word)
		}
	}

	// Ensure we have exactly 2 hashtags
	if len(hashtags) < 2 {
		// Add default hashtags if needed
		for len(hashtags) < 2 {
			hashtags = append(hashtags, "#note")
		}
	} else if len(hashtags) > 2 {
		// Limit to first 2 hashtags
		hashtags = hashtags[:2]
	}

	// Extract usage information from SDK response
	var usage *Usage
	if resp.UsageMetadata != nil {
		usage = &Usage{
			PromptTokens:     int(resp.UsageMetadata.PromptTokenCount),
			CompletionTokens: int(resp.UsageMetadata.CandidatesTokenCount),
			TotalTokens:      int(resp.UsageMetadata.TotalTokenCount),
		}

		// Debug: Log extracted usage information
		logger.Debug("Gemini SDK Token Usage Extracted", map[string]interface{}{
			"prompt_tokens":     usage.PromptTokens,
			"completion_tokens": usage.CompletionTokens,
			"total_tokens":      usage.TotalTokens,
		})
	}

	return hashtags, usage, nil
}

// ProcessImageWithMessage analyzes an image and processes an optional message to generate title and hashtags
func (gc *GeminiSDKClient) ProcessImageWithMessage(ctx context.Context, imageData []byte, message string) (string, *Usage, error) {
	if gc.client == nil {
		return "", nil, fmt.Errorf("gemini SDK client not initialized")
	}

	var prompt string
	if message != "" && !strings.HasPrefix(message, "Photo: ") {
		// If there's a caption/message, include it in the analysis
		prompt = fmt.Sprintf("Analyze this image and the accompanying text. Generate a short title (2-4 words) and exactly 2 hashtags. Return ONLY in this exact format: title|#tag1 #tag2\n\nDo not include any explanations, comments, or additional text.\n\nAccompanying text: %s", message)
	} else {
		// If no caption, analyze only the image
		prompt = "Analyze this image. Generate a short title (2-4 words) and exactly 2 hashtags based on what you see. Return ONLY in this exact format: title|#tag1 #tag2\n\nDo not include any explanations, comments, or additional text."
	}

	// Create multimodal content with text and image
	// Create a single content with both text and image parts
	multimodalContent := &genai.Content{
		Parts: []*genai.Part{
			{Text: prompt},
			{InlineData: &genai.Blob{MIMEType: "image/jpeg", Data: imageData}},
		},
	}
	
	parts := []*genai.Content{multimodalContent}

	// Create generation config with thinking disabled
	config := &genai.GenerateContentConfig{
		Temperature:     genai.Ptr(float32(0.1)),
		TopK:            genai.Ptr(float32(1)),
		TopP:            genai.Ptr(float32(0.8)),
		MaxOutputTokens: 100,
		ThinkingConfig: &genai.ThinkingConfig{
			ThinkingBudget:   genai.Ptr(int32(0)), // Disable thinking mode
			IncludeThoughts:  false,
		},
	}

	resp, err := gc.client.Models.GenerateContent(ctx, gc.modelName, parts, config)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate content from image: %w", err)
	}

	// Debug: Log the complete response structure
	logger.Debug("Gemini SDK Image Analysis Response", map[string]interface{}{
		"candidates_count": len(resp.Candidates),
		"usage_metadata":   resp.UsageMetadata,
		"message_provided": message != "",
		"image_size":       len(imageData),
	})

	if len(resp.Candidates) == 0 {
		return "", nil, fmt.Errorf("no candidates in Gemini image response")
	}

	// Extract content from the first candidate
	candidate := resp.Candidates[0]
	if candidate.Content == nil || len(candidate.Content.Parts) == 0 {
		return "", nil, fmt.Errorf("no content parts in Gemini image response")
	}

	// Get the text content
	var content string
	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			content += part.Text
		}
	}

	content = strings.TrimSpace(content)

	// Extract usage information from SDK response
	var usage *Usage
	if resp.UsageMetadata != nil {
		usage = &Usage{
			PromptTokens:     int(resp.UsageMetadata.PromptTokenCount),
			CompletionTokens: int(resp.UsageMetadata.CandidatesTokenCount),
			TotalTokens:      int(resp.UsageMetadata.TotalTokenCount),
		}

		// Debug: Log extracted usage information
		logger.Debug("Gemini SDK Image Token Usage Extracted", map[string]interface{}{
			"prompt_tokens":     usage.PromptTokens,
			"completion_tokens": usage.CompletionTokens,
			"total_tokens":      usage.TotalTokens,
		})
	}

	return content, usage, nil
}

// Close cleans up the Gemini SDK client resources
func (gc *GeminiSDKClient) Close() error {
	// The new SDK client doesn't require explicit cleanup
	return nil
}

