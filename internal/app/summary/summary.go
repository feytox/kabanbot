// Package summary summarizes chat history with an LLM.
package summary

import (
	"context"
	"errors"
	"fmt"
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

// Service summarizes chat history.
type Service struct {
	history History
	models  Models
	prompt  string
}

// New creates a Service.
func New(history History, models Models, prompt string) *Service {
	return &Service{history: history, models: models, prompt: prompt}
}

// Summarize returns a Markdown summary of the chat starting at fromMessageID.
func (s *Service) Summarize(ctx context.Context, chatID int64, fromMessageID int) (string, error) {
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
	req := target.Request(s.prompt, llm.Message{
		Role:    llm.RoleUser,
		Content: "Messages:\n```\n" + Transcript(msgs) + "\n```",
	})
	resp, err := target.Client.Complete(ctx, req)
	if err != nil {
		return "", fmt.Errorf("summarize: %w", err)
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
