package metrics

import (
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// MetricsCollector manages all Prometheus metrics for the msg2git system
type MetricsCollector struct {
	// Telegram command metrics
	telegramCommandsTotal *prometheus.CounterVec
	userRateLimitViolations *prometheus.CounterVec
	commandQueueDepth *prometheus.GaugeVec
	commandProcessingDuration *prometheus.HistogramVec

	// GitHub API metrics
	githubAPIRequestsTotal *prometheus.CounterVec
	githubAPIRateLimitRemaining *prometheus.GaugeVec
	githubAPIRateLimitResetTime *prometheus.GaugeVec
	githubAPIRequestDuration *prometheus.HistogramVec

	// System health metrics
	activeUsersGauge prometheus.Gauge
	systemLoadFactor prometheus.Gauge
	cacheHitRatio *prometheus.GaugeVec

	// Queue metrics
	queuedRequestsTotal *prometheus.CounterVec
	queueProcessingTime *prometheus.HistogramVec

	// Rate limiting metrics
	rateLimitChecksTotal *prometheus.CounterVec
	rateLimitAllowedTotal *prometheus.CounterVec

	// Internal state
	mu sync.RWMutex
	activeUsers map[int64]time.Time // Track active users for cleanup
}

// NewMetricsCollector creates a new metrics collector with all required Prometheus metrics
func NewMetricsCollector() *MetricsCollector {
	return NewMetricsCollectorWithRegistry(nil)
}

// NewMetricsCollectorWithRegistry creates a new metrics collector with a custom registry
// If registry is nil, uses the default global registry
func NewMetricsCollectorWithRegistry(registry *prometheus.Registry) *MetricsCollector {
	// Create a factory for the given registry
	var factory promauto.Factory
	if registry == nil {
		factory = promauto.With(prometheus.DefaultRegisterer)
	} else {
		factory = promauto.With(registry)
	}
	return &MetricsCollector{
		telegramCommandsTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "telegram_commands_total",
				Help: "Total number of Telegram commands processed",
			},
			[]string{"user_id", "command", "status"},
		),

		userRateLimitViolations: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "user_rate_limit_violations_total",
				Help: "Total number of rate limit violations per user",
			},
			[]string{"user_id", "limit_type"},
		),

		commandQueueDepth: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "command_queue_depth",
				Help: "Current depth of command queue per user",
			},
			[]string{"user_id"},
		),

		commandProcessingDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "command_processing_duration_seconds",
				Help: "Time spent processing commands",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"command", "status"},
		),

		githubAPIRequestsTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "github_api_requests_total",
				Help: "Total number of GitHub API requests",
			},
			[]string{"user_id", "api_type", "endpoint", "status"},
		),

		githubAPIRateLimitRemaining: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "github_api_rate_limit_remaining",
				Help: "Remaining GitHub API rate limit",
			},
			[]string{"user_id", "api_type"},
		),

		githubAPIRateLimitResetTime: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "github_api_rate_limit_reset_time",
				Help: "GitHub API rate limit reset time (Unix timestamp)",
			},
			[]string{"user_id", "api_type"},
		),

		githubAPIRequestDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "github_api_request_duration_seconds",
				Help: "Time spent on GitHub API requests",
				Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"api_type", "endpoint", "status"},
		),

		activeUsersGauge: factory.NewGauge(
			prometheus.GaugeOpts{
				Name: "active_users_gauge",
				Help: "Number of currently active users",
			},
		),

		systemLoadFactor: factory.NewGauge(
			prometheus.GaugeOpts{
				Name: "system_load_factor",
				Help: "Current system load factor (0-1)",
			},
		),

		cacheHitRatio: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "cache_hit_ratio",
				Help: "Cache hit ratio by cache type",
			},
			[]string{"cache_type"},
		),

		queuedRequestsTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "queued_requests_total",
				Help: "Total number of queued requests",
			},
			[]string{"user_id", "request_type", "status"},
		),

		queueProcessingTime: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "queue_processing_time_seconds",
				Help: "Time spent processing queued requests",
				Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
			},
			[]string{"request_type"},
		),

		rateLimitChecksTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "rate_limit_checks_total",
				Help: "Total number of rate limit checks performed",
			},
			[]string{"user_id", "limit_type"},
		),

		rateLimitAllowedTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "rate_limit_allowed_total",
				Help: "Total number of requests allowed by rate limiter",
			},
			[]string{"user_id", "limit_type"},
		),

		activeUsers: make(map[int64]time.Time),
	}
}

// RecordTelegramCommand records metrics for a Telegram command
func (m *MetricsCollector) RecordTelegramCommand(userID int64, command, status string) {
	userIDStr := strconv.FormatInt(userID, 10)
	m.telegramCommandsTotal.WithLabelValues(userIDStr, command, status).Inc()
	
	// Update active users
	m.mu.Lock()
	m.activeUsers[userID] = time.Now()
	m.mu.Unlock()
	
	m.updateActiveUsersGauge()
}

// RecordCommandProcessingTime records time spent processing a command
func (m *MetricsCollector) RecordCommandProcessingTime(command, status string, duration time.Duration) {
	m.commandProcessingDuration.WithLabelValues(command, status).Observe(duration.Seconds())
}

// RecordRateLimitViolation records a rate limit violation
func (m *MetricsCollector) RecordRateLimitViolation(userID int64, limitType string) {
	userIDStr := strconv.FormatInt(userID, 10)
	m.userRateLimitViolations.WithLabelValues(userIDStr, limitType).Inc()
}

// UpdateCommandQueueDepth updates the command queue depth for a user
func (m *MetricsCollector) UpdateCommandQueueDepth(userID int64, depth int) {
	userIDStr := strconv.FormatInt(userID, 10)
	m.commandQueueDepth.WithLabelValues(userIDStr).Set(float64(depth))
}

// RecordGitHubAPIRequest records a GitHub API request
func (m *MetricsCollector) RecordGitHubAPIRequest(userID int64, apiType, endpoint, status string) {
	userIDStr := strconv.FormatInt(userID, 10)
	m.githubAPIRequestsTotal.WithLabelValues(userIDStr, apiType, endpoint, status).Inc()
}

// RecordGitHubAPIRequestDuration records time spent on GitHub API request
func (m *MetricsCollector) RecordGitHubAPIRequestDuration(apiType, endpoint, status string, duration time.Duration) {
	m.githubAPIRequestDuration.WithLabelValues(apiType, endpoint, status).Observe(duration.Seconds())
}

// UpdateGitHubRateLimit updates GitHub API rate limit metrics
func (m *MetricsCollector) UpdateGitHubRateLimit(userID int64, apiType string, remaining int, resetTime time.Time) {
	userIDStr := strconv.FormatInt(userID, 10)
	m.githubAPIRateLimitRemaining.WithLabelValues(userIDStr, apiType).Set(float64(remaining))
	m.githubAPIRateLimitResetTime.WithLabelValues(userIDStr, apiType).Set(float64(resetTime.Unix()))
}

// RecordQueuedRequest records a queued request
func (m *MetricsCollector) RecordQueuedRequest(userID int64, requestType, status string) {
	userIDStr := strconv.FormatInt(userID, 10)
	m.queuedRequestsTotal.WithLabelValues(userIDStr, requestType, status).Inc()
}

// RecordQueueProcessingTime records time spent processing queued requests
func (m *MetricsCollector) RecordQueueProcessingTime(requestType string, duration time.Duration) {
	m.queueProcessingTime.WithLabelValues(requestType).Observe(duration.Seconds())
}

// RecordRateLimitCheck records a rate limit check
func (m *MetricsCollector) RecordRateLimitCheck(userID int64, limitType string, allowed bool) {
	userIDStr := strconv.FormatInt(userID, 10)
	m.rateLimitChecksTotal.WithLabelValues(userIDStr, limitType).Inc()
	
	if allowed {
		m.rateLimitAllowedTotal.WithLabelValues(userIDStr, limitType).Inc()
	}
}

// UpdateCacheHitRatio updates cache hit ratio metrics
func (m *MetricsCollector) UpdateCacheHitRatio(cacheType string, ratio float64) {
	m.cacheHitRatio.WithLabelValues(cacheType).Set(ratio)
}

// UpdateSystemLoadFactor updates the system load factor
func (m *MetricsCollector) UpdateSystemLoadFactor(loadFactor float64) {
	m.systemLoadFactor.Set(loadFactor)
}

// updateActiveUsersGauge updates the active users gauge
func (m *MetricsCollector) updateActiveUsersGauge() {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Clean up inactive users (older than 5 minutes)
	cutoff := time.Now().Add(-5 * time.Minute)
	for userID, lastSeen := range m.activeUsers {
		if lastSeen.Before(cutoff) {
			delete(m.activeUsers, userID)
		}
	}
	
	m.activeUsersGauge.Set(float64(len(m.activeUsers)))
}

// GetActiveUsersCount returns the current number of active users
func (m *MetricsCollector) GetActiveUsersCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.activeUsers)
}

// Cleanup performs periodic cleanup of metrics
func (m *MetricsCollector) Cleanup() {
	m.updateActiveUsersGauge()
}