package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/msg2git/msg2git/experiments/monitoring/github_monitor"
	"github.com/msg2git/msg2git/experiments/monitoring/metrics"
	"github.com/msg2git/msg2git/experiments/monitoring/queue"
	"github.com/msg2git/msg2git/experiments/monitoring/ratelimit"
)

func main() {
	fmt.Println("🚀 Starting Prometheus Monitoring System Demo")
	
	// Create metrics collector
	metricsCollector := metrics.NewMetricsCollector()
	
	// Create GitHub API monitor
	githubConfig := github_monitor.Config{
		WarningThreshold:  0.8,
		CriticalThreshold: 0.9,
		MaxHistorySize:    100,
	}
	githubMonitor := github_monitor.NewGitHubAPIMonitor(githubConfig, metricsCollector)
	
	// Create request queue
	queueConfig := queue.Config{
		Workers:         3,
		MaxQueueSize:    50,
		ProcessingDelay: 100 * time.Millisecond,
		RetryDelay:      time.Second,
		CleanupInterval: 30 * time.Second,
	}
	requestQueue := queue.NewRequestQueue(queueConfig, metricsCollector)
	
	// Create rate limiter (would normally connect to Redis)
	// For demo purposes, we'll show the configuration
	rateLimitConfig := ratelimit.DefaultConfig()
	fmt.Printf("📊 Rate Limiter Config:\n")
	fmt.Printf("  - Command Limit: %d requests per %v\n", rateLimitConfig.CommandLimit.Requests, rateLimitConfig.CommandLimit.Window)
	fmt.Printf("  - GitHub REST Limit: %d requests per %v\n", rateLimitConfig.GitHubRESTLimit.Requests, rateLimitConfig.GitHubRESTLimit.Window)
	fmt.Printf("  - GitHub GraphQL Limit: %d points per %v\n", rateLimitConfig.GitHubQLLimit.Requests, rateLimitConfig.GitHubQLLimit.Window)
	
	// Start the monitoring system
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	requestQueue.Start(ctx)
	defer requestQueue.Stop()
	
	// Start Prometheus metrics endpoint
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		fmt.Println("📈 Prometheus metrics available at http://localhost:8080/metrics")
		log.Fatal(http.ListenAndServe(":8080", nil))
	}()
	
	// Simulate some activity
	demonstrateSystem(metricsCollector, githubMonitor, requestQueue)
	
	// Keep running to allow metrics scraping
	fmt.Println("🔄 System running... Press Ctrl+C to stop")
	fmt.Println("📊 View metrics at: http://localhost:8080/metrics")
	
	// Run for demo period
	select {
	case <-time.After(30 * time.Second):
		fmt.Println("✅ Demo completed")
	case <-ctx.Done():
		fmt.Println("🛑 System stopped")
	}
}

func demonstrateSystem(metrics *metrics.MetricsCollector, githubMonitor *github_monitor.GitHubAPIMonitor, requestQueue *queue.RequestQueue) {
	fmt.Println("\n🎭 Demonstrating system capabilities...")
	
	userID := int64(12345)
	
	// Simulate Telegram commands
	fmt.Println("1. 📱 Simulating Telegram commands...")
	commands := []string{"sync", "todo", "issue", "note", "insight"}
	for i, cmd := range commands {
		status := "success"
		if i%4 == 3 {
			status = "error" // Simulate some errors
		}
		
		metrics.RecordTelegramCommand(userID, cmd, status)
		metrics.RecordCommandProcessingTime(cmd, status, time.Duration(100+i*50)*time.Millisecond)
		
		time.Sleep(100 * time.Millisecond)
	}
	
	// Simulate GitHub API usage
	fmt.Println("2. 🐙 Simulating GitHub API requests...")
	endpoints := []string{"/repos/owner/repo", "/repos/owner/repo/contents/note.md", "/graphql"}
	apiTypes := []github_monitor.APIType{github_monitor.APITypeREST, github_monitor.APITypeREST, github_monitor.APITypeGraphQL}
	
	for i, endpoint := range endpoints {
		startTime := time.Now()
		
		// Simulate decreasing rate limits
		remaining := 4500 - i*500
		resp := &http.Response{
			StatusCode: 200,
			Header: http.Header{
				"X-RateLimit-Limit":     []string{"5000"},
				"X-RateLimit-Remaining": []string{fmt.Sprintf("%d", remaining)},
				"X-RateLimit-Reset":     []string{fmt.Sprintf("%d", time.Now().Add(time.Hour).Unix())},
			},
		}
		
		githubMonitor.TrackRequest(userID, apiTypes[i], endpoint, startTime, resp, nil)
		
		// Check if approaching limits
		if githubMonitor.IsAtWarningThreshold(userID, apiTypes[i]) {
			fmt.Printf("   ⚠️  User %d approaching %s API limit (%d remaining)\n", userID, apiTypes[i], remaining)
		}
		
		time.Sleep(200 * time.Millisecond)
	}
	
	// Simulate request queuing
	fmt.Println("3. 📋 Simulating request queuing...")
	priorities := []queue.Priority{queue.PriorityLow, queue.PriorityNormal, queue.PriorityHigh, queue.PriorityUrgent}
	
	for i, priority := range priorities {
		request := &queue.QueuedRequest{
			UserID:   userID,
			Type:     queue.RequestTypeCommand,
			Priority: priority,
			Payload:  fmt.Sprintf("demo-request-%d", i),
			Handler: func(requestID string) func(ctx context.Context, req *queue.QueuedRequest) error {
				return func(ctx context.Context, req *queue.QueuedRequest) error {
					fmt.Printf("   ✅ Processed queued request: %s (priority: %d)\n", requestID, req.Priority)
					metrics.RecordTelegramCommand(req.UserID, "queued_command", "success")
					return nil
				}
			}(fmt.Sprintf("demo-request-%d", i)),
		}
		
		err := requestQueue.QueueRequest(request)
		if err != nil {
			fmt.Printf("   ❌ Failed to queue request: %v\n", err)
		} else {
			fmt.Printf("   📝 Queued request with priority %d\n", priority)
		}
	}
	
	// Simulate rate limit violations
	fmt.Println("4. 🚫 Simulating rate limit checks...")
	for i := 0; i < 5; i++ {
		allowed := i < 3 // Allow first 3, block last 2
		limitType := "command_rate"
		
		metrics.RecordRateLimitCheck(userID, limitType, allowed)
		
		if !allowed {
			metrics.RecordRateLimitViolation(userID, limitType)
			fmt.Printf("   🚫 Rate limit violation for user %d\n", userID)
		}
	}
	
	// Update system metrics
	fmt.Println("5. 📊 Updating system metrics...")
	metrics.UpdateSystemLoadFactor(0.75)
	metrics.UpdateCacheHitRatio("commit_graph", 0.95)
	metrics.UpdateCacheHitRatio("issue_data", 0.82)
	
	// Show queue stats
	fmt.Println("6. 📈 Queue statistics:")
	stats := requestQueue.GetQueueStats()
	fmt.Printf("   - Total requests: %v\n", stats["total_requests"])
	fmt.Printf("   - Active users: %v\n", stats["active_users"])
	fmt.Printf("   - Max queue size: %v\n", stats["max_queue_size"])
	
	// Show GitHub stats
	fmt.Println("7. 🐙 GitHub API statistics:")
	userStats := githubMonitor.GetUserAPIStats(userID)
	for apiType, info := range userStats {
		usagePercent := float64(info.Limit-info.Remaining) / float64(info.Limit) * 100
		fmt.Printf("   - %s API: %.1f%% used (%d/%d)\n", apiType, usagePercent, info.Limit-info.Remaining, info.Limit)
	}
	
	// Show active users
	fmt.Printf("8. 👥 Active users: %d\n", metrics.GetActiveUsersCount())
	
	// Wait for queue processing
	time.Sleep(2 * time.Second)
	
	fmt.Println("✨ Demonstration completed! Check metrics at http://localhost:8080/metrics")
}