// Package chat lets people talk to the bot. Chat admins may also change the bot's personality
// through tool calls.
package chat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/feytox/kabanbot/internal/app/settings"
	"github.com/feytox/kabanbot/internal/domain"
	"github.com/feytox/kabanbot/internal/llm"
)

const (
	// contextSize is how many recent chat messages the model sees.
	contextSize = 50
	// maxRounds caps model calls per answer. The last round offers no tools, so it must answer.
	maxRounds = 5
	// maxQuoteLen limits how much of a replied-to message is repeated.
	maxQuoteLen = 200
)

// ErrDisabled means chatting is turned off in the chat's settings.
var ErrDisabled = errors.New("chatting is disabled in this chat")

// History reads cached chat messages.
type History interface {
	Recent(ctx context.Context, chatID int64, limit int) ([]domain.Message, error)
}

// Models resolves the model a chat talks with.
type Models interface {
	ChatTarget(ctx context.Context, chatID int64) (llm.Target, error)
}

// Chats reads chat settings and personality.
type Chats interface {
	Chat(ctx context.Context, id int64) (domain.Chat, error)
}

// Settings changes chat settings on behalf of a user, checking their rights.
type Settings interface {
	CanManage(ctx context.Context, u settings.User, chatID int64) (bool, error)
	SetPersonality(ctx context.Context, u settings.User, chatID int64, text string, via domain.ChangeVia) error
	SetSummaryStyle(ctx context.Context, u settings.User, chatID int64, style string) error
}

// Usage records how many tokens model calls used.
type Usage interface {
	RecordUsage(ctx context.Context, u domain.Usage) error
}

// Service answers people in chats.
type Service struct {
	history  History
	models   Models
	chats    Chats
	settings Settings
	usage    Usage
	limiter  *Limiter
	prompt   string
	botID    int64
	log      *slog.Logger
}

// Deps are what the Service needs.
type Deps struct {
	History  History
	Models   Models
	Chats    Chats
	Settings Settings
	Usage    Usage
	// Prompt is the base system prompt.
	Prompt string
	// BotID tells the bot's own messages apart in the history.
	BotID int64
	Log   *slog.Logger
}

// New creates a Service.
func New(d Deps) *Service {
	return &Service{
		history: d.History, models: d.Models, chats: d.Chats, settings: d.Settings, usage: d.Usage,
		limiter: NewLimiter(), prompt: d.Prompt, botID: d.BotID, log: d.Log,
	}
}

// Request is a message addressed to the bot. The message itself is already in the history.
type Request struct {
	ChatID int64
	User   settings.User
}

// Progress reports an answer as it is written.
type Progress struct {
	// Text is the answer so far.
	Text string
	// Tool is the tool being run, if any.
	Tool string
}

// Reply answers the latest messages of the chat, calling progress as the answer grows.
// If the model fails midway or ctx is canceled, it returns the text written so far with the error.
func (s *Service) Reply(ctx context.Context, req Request, progress func(Progress)) (string, error) {
	c, err := s.chats.Chat(ctx, req.ChatID)
	if err != nil {
		return "", err
	}
	if st := c.Settings; !st.Enabled || !st.Chat {
		return "", ErrDisabled
	}
	target, err := s.models.ChatTarget(ctx, req.ChatID)
	if err != nil {
		return "", err
	}
	if err := s.limiter.Allow(req.ChatID, req.User.ID, c.Settings.Limits); err != nil {
		return "", err
	}
	history, err := s.history.Recent(ctx, req.ChatID, contextSize)
	if err != nil {
		return "", err
	}
	var tools []llm.Tool
	admin, err := s.settings.CanManage(ctx, req.User, req.ChatID)
	if err != nil {
		// Chatting still works; only the personality tools are withheld.
		s.log.WarnContext(ctx, "check admin for tools", "chat_id", req.ChatID, "err", err)
	}
	if admin {
		tools = adminTools
	}

	msgs := s.conversation(history)
	system := s.system(c, req.User)
	var answer strings.Builder
	for round := 1; ; round++ {
		r := target.Request(system, msgs...)
		if round < maxRounds {
			r.Tools = tools
		}
		resp, err := s.stream(ctx, target, r, &answer, progress)
		s.record(ctx, req, target, resp.Usage)
		if err != nil {
			return answer.String(), err
		}
		// A model may call tools it was not offered; after the last round they are ignored.
		if len(resp.ToolCalls) == 0 || round >= maxRounds {
			return answer.String(), nil
		}

		msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Content: resp.Text, ToolCalls: resp.ToolCalls})
		for _, call := range resp.ToolCalls {
			progress(Progress{Text: answer.String(), Tool: call.Name})
			result := s.runTool(ctx, req.User, req.ChatID, call)
			msgs = append(msgs, llm.Message{Role: llm.RoleTool, Content: result, ToolCall: &call})
		}
		if answer.Len() > 0 {
			answer.WriteString("\n\n")
		}
	}
}

// stream runs one model call, appending its text to answer.
func (s *Service) stream(ctx context.Context, target llm.Target, r llm.Request, answer *strings.Builder,
	progress func(Progress),
) (llm.Response, error) {
	var resp llm.Response
	for c, err := range target.Client.Stream(ctx, r) {
		if err != nil {
			return resp, fmt.Errorf("chat: %w", err)
		}
		if c.Text != "" {
			resp.Text += c.Text
			answer.WriteString(c.Text)
			progress(Progress{Text: answer.String()})
		}
		resp.ToolCalls = append(resp.ToolCalls, c.ToolCalls...)
		if c.Usage != nil {
			resp.Usage = *c.Usage
		}
	}
	return resp, nil
}

func (s *Service) record(ctx context.Context, req Request, target llm.Target, u llm.Usage) {
	if u == (llm.Usage{}) {
		return
	}
	err := s.usage.RecordUsage(ctx, domain.Usage{
		ChatID: req.ChatID, UserID: req.User.ID, ModelID: target.Model.ID, Kind: domain.UsageChat,
		TokensIn: u.InputTokens, TokensOut: u.OutputTokens,
	})
	if err != nil {
		s.log.WarnContext(ctx, "record usage", "chat_id", req.ChatID, "err", err)
	}
}

// system builds the system prompt: the base prompt, the chat's personality and who is asking.
func (s *Service) system(c domain.Chat, u settings.User) string {
	var b strings.Builder
	b.WriteString(s.prompt)
	if c.Personality != "" {
		b.WriteString("\n\n<personality>\nВ этом чате администраторы задали вам личность. Следуйте ей:\n")
		b.WriteString(c.Personality)
		b.WriteString("\n</personality>")
	}
	if t := c.Settings.Trigger; t.Enabled() && !t.Regex {
		fmt.Fprintf(&b, "\n\nВ этом чате к вам обращаются по имени «%s».", t.Text)
	}
	who := u.FirstName
	if u.Username != "" {
		who += " (@" + u.Username + ")"
	}
	fmt.Fprintf(&b, "\n\nСейчас к вам обращается %s. Ответьте на последнее сообщение.", strings.TrimSpace(who))
	return b.String()
}

// conversation turns chat history into model messages: the bot's messages are its own turns,
// and everyone else's run together into user turns.
func (s *Service) conversation(history []domain.Message) []llm.Message {
	var out []llm.Message
	for _, m := range history {
		role, line := llm.RoleUser, m.Username
		if m.UserID == s.botID && s.botID != 0 {
			role, line = llm.RoleAssistant, m.Text
		} else {
			if m.ReplyTo != nil {
				line += fmt.Sprintf(" (replying to %s: %q)", m.ReplyTo.Username, truncate(m.ReplyTo.Text, maxQuoteLen))
			}
			line += ": " + m.Text
		}
		switch n := len(out); {
		case n == 0 && role == llm.RoleAssistant:
			// A conversation must start with a user turn.
		case n > 0 && out[n-1].Role == role:
			out[n-1].Content += "\n" + line
		default:
			out = append(out, llm.Message{Role: role, Content: line})
		}
	}
	return out
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
