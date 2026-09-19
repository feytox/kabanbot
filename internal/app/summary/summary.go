// Package summary summarizes chat history with an LLM.
package summary

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/feytox/kabanbot/internal/domain"
	"github.com/feytox/kabanbot/internal/llm"
)

// ErrNoHistory is returned when there are no cached messages to summarize.
var ErrNoHistory = errors.New("no cached messages to summarize")

// maxQuoteLen limits how much of a replied-to message is repeated in the prompt.
const maxQuoteLen = 200

// History reads cached chat messages.
type History interface {
	Since(ctx context.Context, chatID int64, messageID int) ([]domain.Message, error)
}

// Models resolves the model a chat uses for summaries.
type Models interface {
	SummaryTarget(ctx context.Context, chatID int64) (llm.Target, error)
}

// Chats reads a chat's summary style.
type Chats interface {
	Chat(ctx context.Context, id int64) (domain.Chat, error)
}

// Usage records how many tokens model calls used.
type Usage interface {
	RecordUsage(ctx context.Context, u domain.Usage) error
}

// Service summarizes chat history.
type Service struct {
	history History
	models  Models
	chats   Chats
	usage   Usage
	prompt  string
	log     *slog.Logger
}

// New creates a Service.
func New(history History, models Models, chats Chats, usage Usage, prompt string, log *slog.Logger) *Service {
	return &Service{history: history, models: models, chats: chats, usage: usage, prompt: prompt, log: log}
}

// Summarize returns a Markdown summary of the chat starting at fromMessageID, asked for by userID.
func (s *Service) Summarize(ctx context.Context, chatID, userID int64, fromMessageID int) (string, error) {
	msgs, err := s.history.Since(ctx, chatID, fromMessageID)
	if err != nil {
		return "", err
	}
	if len(msgs) == 0 {
		return "", ErrNoHistory
	}

	target, err := s.models.SummaryTarget(ctx, chatID)
	if err != nil {
		return "", err
	}
	system := s.prompt
	if c, err := s.chats.Chat(ctx, chatID); err != nil {
		s.log.WarnContext(ctx, "load summary style", "chat_id", chatID, "err", err)
	} else if c.SummaryStyle != "" {
		system += "\n\n<style>\nАдминистраторы чата попросили писать пересказы так:\n" + c.SummaryStyle + "\n</style>"
	}
	req := target.Request(system, llm.Message{
		Role:    llm.RoleUser,
		Content: "Messages:\n```\n" + Transcript(msgs) + "\n```",
	})
	resp, err := target.Client.Complete(ctx, req)
	if err != nil {
		return "", fmt.Errorf("summarize: %w", err)
	}
	err = s.usage.RecordUsage(ctx, domain.Usage{
		ChatID: chatID, UserID: userID, ModelID: target.Model.ID, Kind: domain.UsageSummary,
		TokensIn: resp.Usage.InputTokens, TokensOut: resp.Usage.OutputTokens,
	})
	if err != nil {
		s.log.WarnContext(ctx, "record usage", "chat_id", chatID, "err", err)
	}
	return strings.TrimSpace(resp.Text), nil
}

// Transcript renders messages in the format described by the summary prompt.
func Transcript(msgs []domain.Message) string {
	var b strings.Builder
	for i, m := range msgs {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(m.Username)
		if m.ReplyTo != nil {
			fmt.Fprintf(&b, " (replying to %s: %q)", m.ReplyTo.Username, truncate(m.ReplyTo.Text, maxQuoteLen))
		}
		b.WriteString(": ")
		b.WriteString(m.Text)
	}
	return b.String()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
