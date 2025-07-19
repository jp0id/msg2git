# Rate Limiting Systems Analysis

## Overview

This document explains the relationship between the existing rate limiting system and the new Prometheus monitoring system in the msg2git project.

## **Analysis: New Monitoring System vs Existing Rate Limiting**

### **1. Did the new system replace the old rate limiting?**

**No**, the new Prometheus monitoring system did **NOT** replace the existing rate limiting system. The new system is **completely separate** and **complementary** to the existing rate limiting.

### **2. Core Differences Between the Systems**

| **Aspect** | **Existing Rate Limiting** | **New Prometheus Monitoring** |
|------------|---------------------------|------------------------------|
| **Purpose** | **Telegram API Protection** | **GitHub API + System Monitoring** |
| **Scope** | Telegram message sending only | All operations (GitHub API, commands, queues) |
| **Technology** | `golang.org/x/time/rate` | Prometheus metrics + sliding window |
| **Location** | `internal/telegram/bot.go` | `internal/monitoring/` |
| **Rate Limits** | Fixed: 5000/sec global, 30/user/sec | Dynamic: Premium tier multipliers |
| **Integration** | Built into `rateLimitedSend()` | Standalone monitoring system |
| **Persistence** | Memory-only (lost on restart) | Memory-based with optional Redis |
| **Metrics** | None | Comprehensive Prometheus metrics |

## Existing System Details

### Location and Implementation
- **File**: `internal/telegram/bot.go:98-114`
- **Function**: `rateLimitedSend()` and `getUserRateLimiter()`

### Rate Limits
```go
// Global: 5000 messages/second, burst 5000
globalLimiter:  rate.NewLimiter(rate.Limit(5000), 5000)

// Per-user: 30 messages/second, burst 30  
limiter = rate.NewLimiter(rate.Limit(30), 30)
```

### Current Function
The `rateLimitedSend()` function protects **only Telegram API calls** from being rate-limited by Telegram servers.

```go
func (b *Bot) rateLimitedSend(chatID int64, msg tgbotapi.Chattable) (tgbotapi.Message, error) {
    // Wait for global rate limiter
    if err := b.globalLimiter.Wait(context.Background()); err != nil {
        return tgbotapi.Message{}, fmt.Errorf("global rate limiter error: %w", err)
    }

    // Wait for user-specific rate limiter
    userLimiter := b.getUserRateLimiter(chatID)
    if err := userLimiter.Wait(context.Background()); err != nil {
        return tgbotapi.Message{}, fmt.Errorf("user rate limiter error: %w", err)
    }

    return b.api.Send(msg)
}
```

## New Prometheus Monitoring System

### Purpose
The new monitoring system addresses **completely different concerns:**
- **GitHub API rate limiting** (5000 REST requests/hour, 5000 GraphQL points/hour)
- **User command rate limiting** (with premium tier benefits)
- **Comprehensive monitoring** (Prometheus metrics for all operations)
- **Request queuing** (when limits are approached)
- **System health tracking** (queue depths, processing times, etc.)

### Components
1. **Metrics Collector** (`internal/monitoring/metrics/`)
2. **Memory-based Rate Limiter** (`internal/monitoring/ratelimit/`)
3. **GitHub API Monitor** (`internal/monitoring/github_monitor/`)
4. **Request Queue System** (`internal/monitoring/queue/`)

### Rate Limits
```go
Free User Limits (per user):
├── Commands: 30/minute
├── GitHub REST: 60/hour  
└── GitHub GraphQL: 100 points/hour

Premium Users get MULTIPLIERS:
├── Coffee (2x): 60 commands/min, 120 REST/hour
├── Cake (4x): 120 commands/min, 240 REST/hour  
└── Sponsor (10x): 300 commands/min, 600 REST/hour
```

## Integration Strategy

The systems work **together** without conflicts:

```go
// Existing: Telegram rate limiting (keeps working as-is)
func (b *Bot) rateLimitedSend(chatID int64, msg tgbotapi.Chattable) {
    b.globalLimiter.Wait(context.Background())      // Existing: 5000/sec global
    b.getUserRateLimiter(chatID).Wait(...)          // Existing: 30/user/sec
    return b.api.Send(msg)
}

// New: GitHub API + command monitoring (additional layer)
func (b *Bot) handleSyncCommand(message *tgbotapi.Message) {
    // NEW: Check GitHub API limits before making calls
    if !rateLimiter.CheckLimit(userID, "github_graphql", premiumLevel) {
        queue.QueueRequest(userID, "sync", requestData)
        return "⏳ Command queued due to GitHub API limits"
    }
    
    // Execute sync with GitHub API monitoring
    githubMonitor.TrackRequest(userID, "GraphQL", "/graphql", startTime, response, err)
    
    // Existing: Send response via Telegram (still rate limited)
    b.rateLimitedSend(message.Chat.ID, responseMsg)
}
```

## Key Insights

### Why Two Systems Are Needed

1. **Different APIs, Different Limits**:
   - **Telegram API**: High frequency (5000/sec), protects against bot spam
   - **GitHub API**: Low frequency (5000/hour), protects against quota exhaustion

2. **Different Purposes**:
   - **Existing**: Prevents Telegram from blocking the bot
   - **New**: Prevents GitHub from blocking user tokens + provides monitoring

3. **Different Granularity**:
   - **Existing**: Per-second rate limiting for message sending
   - **New**: Per-hour rate limiting for API operations + comprehensive metrics

### Benefits of Separation

✅ **Existing system stays intact** - continues protecting Telegram API  
✅ **New system adds value** - monitors GitHub API and provides metrics  
✅ **No conflicts** - they address different rate limiting concerns  
✅ **Better user experience** - premium users get GitHub API benefits while Telegram limits remain consistent

## Conclusion

The new monitoring system fills the **missing gap** of GitHub API management while the existing system continues its job of protecting against Telegram rate limits. Both systems are necessary and complementary for a robust, well-monitored application.

### Current Status
- ✅ **Existing Telegram rate limiting**: Active and unchanged
- ✅ **New Prometheus monitoring**: Implemented and ready for integration
- 🔧 **Integration pending**: Need to add monitoring calls to bot commands
- 📊 **Metrics collection**: Ready for Grafana dashboards and alerting