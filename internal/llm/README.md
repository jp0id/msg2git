# LLM Package

This package provides LLM (Large Language Model) integration for the msg2git application, supporting multiple providers including Gemini via langchain-go.

## Features

- **Multi-provider support**: OpenAI-compatible APIs and Google Gemini
- **Langchain-go integration**: Uses langchain-go for Gemini API access
- **Graceful fallback**: Falls back to HTTP-based API calls if Gemini initialization fails
- **Comprehensive testing**: Unit tests with mock responses and integration test support

## Components

### Client (`client.go`)
The main LLM client that supports multiple providers:
- Automatically detects provider type based on configuration
- Uses Gemini client when provider is set to "gemini"
- Falls back to OpenAI-compatible HTTP API for other providers
- Provides methods for message processing, title generation, and hashtag generation

### Gemini Client (`gemini.go`)
Dedicated Gemini client using langchain-go:
- Direct integration with Google AI's Gemini API
- Context-aware API calls with timeout support
- Separate methods for different content generation tasks
- Proper error handling and content validation

## Configuration

Set the LLM provider in your configuration:

```go
config := &config.Config{
    LLMProvider: "gemini",        // or "openai", "deepseek", etc.
    LLMToken:    "your-api-key",
    LLMModel:    "gemini-pro",    // or your preferred model
}
```

## Usage

### Basic Usage

```go
// Create a new client
client := llm.NewClient(cfg)
defer client.Close()

// Process a message (generates title and hashtags)
result, err := client.ProcessMessage("I love programming in Go")
// Returns: "Go Programming|#golang #coding"

// Generate title only
title, err := client.GenerateTitle("I love programming in Go")
// Returns: "Go Programming"

// Generate hashtags only
hashtags, err := client.GenerateHashtags("I love programming in Go")
// Returns: ["#golang", "#coding"]
```

### Gemini-specific Usage

```go
// Create Gemini client directly
geminiClient, err := llm.NewGeminiClient(cfg)
if err != nil {
    log.Fatal(err)
}
defer geminiClient.Close()

ctx := context.Background()
result, err := geminiClient.ProcessMessage(ctx, "Your message here")
```

## Testing

Run the test suite:

```bash
# Run all tests
go test ./internal/llm/... -v

# Run with integration tests (requires GEMINI_API_KEY environment variable)
export GEMINI_API_KEY="your-actual-api-key"
go test ./internal/llm/... -v
```

### Test Coverage

- **Unit tests**: Mock responses, error handling, configuration validation
- **Integration tests**: Real API calls (skipped unless API key is provided)
- **Parsing tests**: Content parsing and format validation
- **Error handling**: Network errors, invalid responses, context cancellation

## Error Handling

The package handles various error scenarios gracefully:

1. **Invalid API keys**: Logs warning and continues without LLM features
2. **Network errors**: Returns original message with error details
3. **Invalid responses**: Provides fallback content or error messages
4. **Context cancellation**: Respects context timeouts and cancellation

## Dependencies

- `github.com/tmc/langchaingo`: Langchain-go library for Gemini integration
- `github.com/msg2git/msg2git/internal/config`: Configuration management

## API Response Format

The LLM APIs return content in a specific format:

```
Title|#hashtag1 #hashtag2
```

Example:
```
Go Programming|#golang #coding
```

This format is parsed by the client to extract titles and hashtags separately when needed.