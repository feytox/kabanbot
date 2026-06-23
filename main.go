package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"kabanbot/config"
	"kabanbot/handlers"
	"kabanbot/services"
)

func main() {
	// Initialize default structured logger
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	slog.Info("Starting Kabanbot initialization...")

	// 1. Load config
	cfg, err := config.LoadConfig()
	if err != nil {
		slog.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	if cfg.BotToken == "" {
		slog.Error("BOT_TOKEN environment variable is not set")
		os.Exit(1)
	}

	// 2. Setup Context with Cancellation on Interrupt Signals
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 3. Initialize Services
	llmService := services.NewLLMService(cfg)

	// 4. Initialize Handlers
	h := handlers.NewHandler(llmService)

	// 5. Setup Bot Options
	opts := []bot.Option{
		bot.WithMessageTextHandler("start", bot.MatchTypeCommand, h.Start),
		bot.WithDefaultHandler(defaultHandler),
	}

	// 6. Create Bot
	b, err := bot.New(cfg.BotToken, opts...)
	if err != nil {
		slog.Error("Failed to create Telegram bot", "error", err)
		os.Exit(1)
	}

	slog.Info("Bot is running. Press CTRL+C to stop.")

	// 7. Start polling
	b.Start(ctx)

	slog.Info("Bot stopped gracefully.")
}

func defaultHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message != nil {
		slog.Info("Received unhandled update",
			"type", update.Message.Chat.Type,
			"chat_id", update.Message.Chat.ID,
			"text", update.Message.Text,
		)
	}
}
