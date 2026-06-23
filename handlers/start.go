package handlers

import (
	"context"
	"log/slog"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// GreetingGenerator defines the interface for generating dynamic greeting messages.
type GreetingGenerator interface {
	GenerateGreeting(ctx context.Context, firstName string) (string, error)
}

// Handler wraps all message and command handlers.
type Handler struct {
	llmService GreetingGenerator
}

// NewHandler initializes a new Handler with services.
func NewHandler(llmService GreetingGenerator) *Handler {
	return &Handler{
		llmService: llmService,
	}
}

// Start handles the /start command.
func (h *Handler) Start(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}

	chatID := update.Message.Chat.ID
	firstName := update.Message.From.FirstName

	// Send an initial status message to indicate processing
	sentMsg, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   "Generating a greeting for you...",
	})
	if err != nil {
		slog.Error("Failed to send processing message", "error", err, "chat_id", chatID)
	}

	greeting, err := h.llmService.GenerateGreeting(ctx, firstName)
	if err != nil {
		slog.Warn("Failed to generate greeting via LLM", "error", err, "first_name", firstName)
		greeting = "Hello " + firstName + "! Welcome to Kabanbot. (Unable to generate a custom greeting at this time)."
	}

	if sentMsg != nil {
		// Edit the initial message with the LLM output
		_, err = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: sentMsg.ID,
			Text:      greeting,
		})
		if err != nil {
			slog.Error("Failed to edit greeting message", "error", err, "chat_id", chatID, "message_id", sentMsg.ID)
		}
	} else {
		// If initial send failed, try sending a fresh message
		_, err = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   greeting,
		})
		if err != nil {
			slog.Error("Failed to send fallback greeting message", "error", err, "chat_id", chatID)
		}
	}
}
