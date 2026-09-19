package telegram

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/mymmrac/telego"

	"github.com/feytox/kabanbot/internal/app/chat"
	"github.com/feytox/kabanbot/internal/domain"
	"github.com/feytox/kabanbot/internal/llm"
)

func testBot() *Bot {
	return &Bot{Client: &Client{me: &telego.User{ID: 100, Username: "KabanBot", IsBot: true}}, log: slog.New(slog.DiscardHandler)}
}

func TestIsForBot(t *testing.T) {
	b := testBot()
	group := telego.Chat{ID: -1, Type: telego.ChatTypeSupergroup}
	for _, tt := range []struct {
		msg  telego.Message
		want bool
	}{
		{telego.Message{Chat: group, Text: "эй @kabanbot, как дела?"}, true},
		{telego.Message{Chat: group, Text: "просто болтаем"}, false},
		{telego.Message{Chat: group, Text: "ответ", ReplyToMessage: &telego.Message{From: &telego.User{ID: 100}}}, true},
		{telego.Message{Chat: group, Text: "ответ", ReplyToMessage: &telego.Message{From: &telego.User{ID: 5}}}, false},
		{telego.Message{Chat: group, Caption: "фото для @KabanBot", Photo: []telego.PhotoSize{{}}}, true},
	} {
		if got := b.isForBot(&tt.msg); got != tt.want {
			t.Errorf("isForBot(%q) = %v, want %v", content(&tt.msg), got, tt.want)
		}
	}
}

func TestCommandArgs(t *testing.T) {
	for in, want := range map[string]string{
		"/ask как дела?":       "как дела?",
		"/ask@kabanbot   тут ": "тут",
		"/ask":                 "",
		"/ask\nтекст":          "",
	} {
		if got := commandArgs(in); got != want {
			t.Errorf("commandArgs(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestChatFailure(t *testing.T) {
	b := testBot()
	ctx := context.Background()
	group := &telego.Message{Chat: telego.Chat{ID: -1, Type: telego.ChatTypeSupergroup}}
	private := &telego.Message{Chat: telego.Chat{ID: 5, Type: telego.ChatTypePrivate}}

	if got := b.chatFailure(ctx, group, chat.ErrDisabled, false); got != "" {
		t.Errorf("a mention in a chat with chatting off must stay unanswered, got %q", got)
	}
	if got := b.chatFailure(ctx, group, chat.ErrDisabled, true); got == "" {
		t.Error("/ask in a chat with chatting off must be explained")
	}
	if got := b.chatFailure(ctx, private, domain.ErrNoModel, true); !strings.Contains(got, "Мой чат") {
		t.Errorf("no model in private chat: %q", got)
	}
	rl := &chat.RateLimitError{PerUser: true, RetryIn: 90 * time.Second}
	if got := b.chatFailure(ctx, group, rl, false); !strings.Contains(got, "от вас") || !strings.Contains(got, "2 мин") {
		t.Errorf("rate limit: %q", got)
	}
	if got := b.chatFailure(ctx, group, &llm.Error{Provider: "Gemini", Status: 503}, false); !strings.Contains(got, "перегружена") {
		t.Errorf("provider error: %q", got)
	}
	if got := b.chatFailure(ctx, private, errors.Join(context.Canceled), true); !strings.Contains(got, "Остановлено") {
		t.Errorf("stopped: %q", got)
	}
}

func TestTail(t *testing.T) {
	if got := tail("абвгд", 3); got != "…гд" {
		t.Errorf("tail = %q", got)
	}
	if got := tail("аб", 3); got != "аб" {
		t.Errorf("short tail = %q", got)
	}
}
