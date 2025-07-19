# Prometheus-Based Rate Limiting & API Monitoring Experiment

## Overview

This experiment implements a comprehensive rate limiting and GitHub API monitoring system using Prometheus metrics. The system provides:

1. **QPS Rate Limiting**: Per-user and global command rate limiting
2. **GitHub API Monitoring**: Real-time tracking of REST and GraphQL usage
3. **Intelligent Queuing**: Request queuing when approaching limits
4. **Prometheus Integration**: Full metrics collection and alerting

## Components

### Core Modules
- `metrics/` - Prometheus metrics collection
- `ratelimit/` - Rate limiting with Prometheus backend
- `github_monitor/` - GitHub API rate limit monitoring
- `queue/` - Request queuing system

### Testing
- `*_test.go` - Comprehensive unit tests
- `integration_test.go` - Full system integration tests
- `benchmark_test.go` - Performance benchmarks

## Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Telegram Bot  │───▶│  Rate Limiter    │───▶│ GitHub Monitor  │
└─────────────────┘    └──────────────────┘    └─────────────────┘
         │                       │                       │
         │              ┌────────▼────────┐             │
         │              │  Prometheus     │             │
         │              │  Metrics        │◀────────────┘
         │              └─────────────────┘
         │
         ▼
┌─────────────────┐
│  Request Queue  │
└─────────────────┘
```

## Usage

```go
// Initialize the monitoring system
monitor := NewMonitoringSystem()

// Check if user can execute command
if !monitor.AllowCommand(userID, "sync") {
    // Queue or reject request
    monitor.QueueCommand(userID, "sync", requestData)
    return
}

// Track GitHub API usage
monitor.TrackGitHubAPI(userID, "REST", "/repos/owner/repo", 1)

// Check GitHub API limits
if monitor.ApproachingGitHubLimit(userID, "GraphQL") {
    // Delay or queue GraphQL requests
    monitor.QueueGitHubRequest(userID, "GraphQL", requestData)
    return
}
```

## Metrics Collected

### Command Rate Limiting
- `telegram_commands_total{user_id, command, status}`
- `user_rate_limit_violations_total{user_id, limit_type}`
- `command_queue_depth{user_id}`

### GitHub API Monitoring
- `github_api_requests_total{user_id, api_type, endpoint}`
- `github_api_rate_limit_remaining{user_id, api_type}`
- `github_api_rate_limit_reset_time{user_id, api_type}`

### System Health
- `active_users_gauge`
- `system_load_factor`
- `cache_hit_ratio{cache_type}`

## Alert Rules

The system includes pre-configured Prometheus alert rules for:
- User command rate violations
- GitHub API rate limit warnings (80%, 90%, 95%)
- System overload conditions
- Queue depth alerts

## Testing Strategy

1. **Unit Tests**: Each component tested in isolation
2. **Integration Tests**: Full system behavior validation
3. **Load Tests**: Performance under high traffic
4. **Chaos Tests**: Behavior during failures

## Development Notes

This is an experimental implementation designed to be:
- **Non-intrusive**: Can be enabled/disabled easily
- **Performance-focused**: Minimal overhead
- **Observable**: Rich metrics and logging
- **Testable**: Comprehensive test coverage

Once validated, this system will be integrated into the main msg2git application.