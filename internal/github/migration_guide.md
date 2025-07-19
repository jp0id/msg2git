# GitHub Provider Interface Migration Guide

This guide explains how to migrate from the existing direct Manager usage to the new interface-based approach.

## Overview

The new interface structure provides:
- **Clean abstraction** for different GitHub implementations
- **Easy migration path** from clone-based to API-only
- **A/B testing capabilities** between implementations
- **Backward compatibility** during transition period

## Interface Structure

```
GitHubProvider (main interface)
├── RepositoryManager (repo setup & metadata)
├── FileManager (file operations)
├── IssueManager (GitHub issues)
└── AssetManager (photo/binary uploads)
```

## Migration Steps

### 1. Current Usage Pattern
```go
// OLD: Direct manager usage
manager, err := github.NewManager(cfg, premiumLevel)
if err != nil {
    return err
}
err = manager.CommitFile("notes.md", content, "Add note")
```

### 2. New Interface Usage
```go
// NEW: Interface-based usage
factory := github.NewProviderFactory()
config := github.NewProviderConfig(cfg, premiumLevel, userID)

provider, err := factory.CreateProvider(github.ProviderTypeClone, config)
if err != nil {
    return err
}
err = provider.CommitFile("notes.md", content, "Add note")
```

### 3. Gradual Migration Strategy

#### Phase 1: Add Interface Layer (Backward Compatible)
```go
// Existing code continues to work
manager, err := github.NewManager(cfg, premiumLevel)

// New code can use interface
provider := &github.CloneBasedAdapter{
    manager: manager,
    config:  providerConfig,
}
```

#### Phase 2: Update Call Sites
Replace direct manager calls with interface calls:

```go
// Before
func processMessage(manager *github.Manager, content string) error {
    return manager.CommitFile("notes.md", content, "Add note")
}

// After  
func processMessage(provider github.GitHubProvider, content string) error {
    return provider.CommitFile("notes.md", content, "Add note")
}
```

#### Phase 3: Implement API Provider
```go
// New API-only implementation
provider, err := factory.CreateProvider(github.ProviderTypeAPI, config)
```

#### Phase 4: A/B Testing
```go
// Feature flag based provider selection
providerType := github.ProviderTypeClone
if isAPITestUser(userID) {
    providerType = github.ProviderTypeAPI
}
provider, err := factory.CreateProvider(providerType, config)
```

## Key Benefits

### 1. **Zero Downtime Migration**
- Current implementation continues working
- New interface layer is opt-in
- Gradual migration of call sites

### 2. **Easy A/B Testing**
- Switch providers per user
- Compare performance metrics
- Gradual rollout of new implementation

### 3. **Clean Architecture**
- Clear separation of concerns
- Interface-based design
- Easy to mock for testing

### 4. **Future Extensibility**
- Easy to add new provider types
- Plugin-style architecture
- Provider-specific optimizations

## Code Examples

### Basic File Operations
```go
// Initialize provider
factory := github.NewProviderFactory()
config := github.NewProviderConfig(userConfig, premiumLevel, userID)
provider, err := factory.CreateProvider(github.ProviderTypeClone, config)

// File operations
err = provider.CommitFile("notes.md", "New note content", "Add note")
err = provider.ReplaceFile("config.md", "Updated config", "Update config")

// Read files
content, err := provider.ReadFile("notes.md")
```

### Issue Management
```go
// Create issue
issueURL, issueNumber, err := provider.CreateIssue("Bug Report", "Description")

// Add comment
commentURL, err := provider.AddIssueComment(issueNumber, "Additional info")

// Sync statuses
statuses, err := provider.SyncIssueStatuses([]int{1, 2, 3})
```

### Repository Management
```go
// Setup repository
err = provider.EnsureRepository(premiumLevel)

// Check capacity
isNear, percentage, err := provider.IsRepositoryNearCapacityWithPremium(premiumLevel)

// Get size info
currentSize, maxSize, err := provider.GetRepositorySizeInfoWithPremium(premiumLevel)
```

### Provider Selection Logic
```go
func selectProvider(userID string, operationType string) github.ProviderType {
    // High-volume users get API provider
    if isHighVolumeUser(userID) {
        return github.ProviderTypeAPI
    }
    
    // Complex operations stay with clone
    if operationType == "multi-file-commit" {
        return github.ProviderTypeClone
    }
    
    // Default to current implementation
    return github.ProviderTypeClone
}
```

## Testing Strategy

### 1. **Unit Tests**
```go
// Mock provider for testing
type MockProvider struct{}

func (m *MockProvider) CommitFile(filename, content, message string) error {
    return nil // Mock implementation
}

func TestMessageProcessing(t *testing.T) {
    mockProvider := &MockProvider{}
    err := processMessage(mockProvider, "test content")
    assert.NoError(t, err)
}
```

### 2. **Integration Tests**
```go
func TestProviderCompatibility(t *testing.T) {
    providers := []github.ProviderType{
        github.ProviderTypeClone,
        github.ProviderTypeAPI,
    }
    
    for _, providerType := range providers {
        t.Run(string(providerType), func(t *testing.T) {
            provider, err := factory.CreateProvider(providerType, config)
            require.NoError(t, err)
            
            // Test all operations
            testFileOperations(t, provider)
            testIssueOperations(t, provider)
        })
    }
}
```

### 3. **Performance Comparison**
```go
func BenchmarkProviders(b *testing.B) {
    providers := map[string]github.ProviderType{
        "clone": github.ProviderTypeClone,
        "api":   github.ProviderTypeAPI,
    }
    
    for name, providerType := range providers {
        b.Run(name, func(b *testing.B) {
            provider, _ := factory.CreateProvider(providerType, config)
            
            b.ResetTimer()
            for i := 0; i < b.N; i++ {
                provider.CommitFile("test.md", "content", "message")
            }
        })
    }
}
```

## Migration Checklist

- [ ] Create interface files (`interfaces.go`, `factory.go`, `adapter.go`)
- [ ] Add config adapter for backward compatibility
- [ ] Update one service to use interface (pilot test)
- [ ] Add feature flag for provider selection
- [ ] Implement API-only provider
- [ ] Create A/B testing infrastructure
- [ ] Update all call sites gradually
- [ ] Add comprehensive tests
- [ ] Monitor metrics and performance
- [ ] Deprecate direct manager usage
- [ ] Remove old implementation (final phase)

This migration strategy ensures zero downtime while providing a clean path to the new architecture.