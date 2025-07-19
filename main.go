package main

import (
	"log"

	"github.com/msg2git/msg2git/internal/config"
	"github.com/msg2git/msg2git/internal/logger"
	"github.com/msg2git/msg2git/internal/telegram"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize logger
	if err := logger.InitLogger(cfg.LogLevel); err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}

	logger.Info("msg2git is starting", map[string]interface{}{
		"log_level":    cfg.LogLevel,
		"has_database": cfg.HasDatabaseConfig(),
		"has_llm":      cfg.HasLLMConfig(),
	})

	bot, err := telegram.NewBot(cfg)
	if err != nil {
		logger.Error("Failed to create Telegram bot", map[string]interface{}{
			"error": err.Error(),
		})
		log.Fatalf("Failed to create Telegram bot: %v", err)
	}

	logger.InfoMsg("📝 Ready to turn your messages into GitHub commits!")

	defer bot.Stop()
	if err := bot.Start(); err != nil {
		logger.Error("Bot error", map[string]interface{}{
			"error": err.Error(),
		})
	}
}
