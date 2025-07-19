package ratelimit

import (
	"context"
	"time"
)

// RateLimiterInterface defines the common interface for all rate limiters
type RateLimiterInterface interface {
	// CheckLimit checks if a user is within their rate limit
	CheckLimit(ctx context.Context, userID int64, limitType LimitType, premiumLevel int) (bool, error)
	
	// ConsumeLimit consumes one request from the rate limit
	ConsumeLimit(ctx context.Context, userID int64, limitType LimitType, premiumLevel int) error
	
	// GetCurrentUsage returns the current usage for a user and limit type
	GetCurrentUsage(ctx context.Context, userID int64, limitType LimitType) (int, error)
	
	// GetRemainingRequests returns the number of remaining requests for a user
	GetRemainingRequests(ctx context.Context, userID int64, limitType LimitType, premiumLevel int) (int, error)
	
	// GetResetTime returns when the rate limit will reset for a user
	GetResetTime(ctx context.Context, userID int64, limitType LimitType) (time.Time, error)
	
	// ResetUserLimits resets all rate limits for a user
	ResetUserLimits(ctx context.Context, userID int64) error
	
	// GetGlobalSystemLoad returns the current global system load factor (0-1)
	GetGlobalSystemLoad(ctx context.Context) (float64, error)
	
	// Close closes the rate limiter and cleans up resources
	Close() error
}

// LimitType represents different types of rate limits
type LimitType string

const (
	LimitTypeCommand    LimitType = "command_rate"
	LimitTypeGitHubREST LimitType = "github_rest"
	LimitTypeGitHubQL   LimitType = "github_graphql"
	LimitTypeGlobal     LimitType = "global_system"
)

// RateLimit defines a rate limit configuration
type RateLimit struct {
	Requests int           // Number of requests allowed
	Window   time.Duration // Time window for the limit
}

// Config holds rate limiter configuration
type Config struct {
	// For Redis-based limiter (deprecated in this experiment)
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	
	// Rate limit configurations
	CommandLimit    RateLimit
	GitHubRESTLimit RateLimit
	GitHubQLLimit   RateLimit
	GlobalLimit     RateLimit
	
	// Premium tier multipliers
	PremiumMultipliers map[int]float64
}

// DefaultConfig returns a default configuration for rate limiters
func DefaultConfig() Config {
	return Config{
		CommandLimit: RateLimit{
			Requests: 30,               // 30 commands per minute
			Window:   time.Minute,
		},
		GitHubRESTLimit: RateLimit{
			Requests: 60,               // 60 REST requests per hour (conservative)
			Window:   time.Hour,
		},
		GitHubQLLimit: RateLimit{
			Requests: 100,              // 100 GraphQL points per hour (conservative)
			Window:   time.Hour,
		},
		GlobalLimit: RateLimit{
			Requests: 1000,             // 1000 total requests per hour per user
			Window:   time.Hour,
		},
		
		PremiumMultipliers: map[int]float64{
			0: 1.0,  // Free tier
			1: 2.0,  // Coffee tier - 2x limits
			2: 4.0,  // Cake tier - 4x limits
			3: 10.0, // Sponsor tier - 10x limits
		},
	}
}