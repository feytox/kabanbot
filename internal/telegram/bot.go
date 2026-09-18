// Package telegram is the Telegram Bot API adapter: it turns updates into use-case calls
// and renders their results.
package telegram

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/feytox/kabanbot/internal/app/summary"
	"github.com/feytox/kabanbot/internal/domain"
)

// maxConcurrentHandlers bounds how many slow handlers (LLM calls, API requests) run at once.
const maxConcurrentHandlers = 32

// Ingester stores incoming messages.
type Ingester interface {
	Ingest(ctx context.Context, m domain.Message) error
}

// Summarizer summarizes chat history.
type Summarizer interface {
	Summarize(ctx context.Context, chatID int64, fromMessageID int) (string, error)
}

// Mentioner resolves @all targets.
type Mentioner interface {
	Targets(ctx context.Context, chatID, authorID int64) ([]domain.User, error)
}

// Deps are the use cases the bot dispatches to.
type Deps struct {
	Ingest  Ingester
	Summary Summarizer
	Mention Mentioner
	// Allowed reports whether the bot may operate in the chat.
	Allowed func(chatID int64) bool
}

// Client is a connected Bot API client.
type Client struct {
	api *telego.Bot
	me  *telego.User
}

// Connect creates a Bot API client and verifies the token.
func Connect(ctx context.Context, token string) (*Client, error) {
	api, err := telego.NewBot(token, telego.WithDiscardLogger())
	if err != nil {
		return nil, fmt.Errorf("telegram: new bot: %w", err)
	}
	me, err := api.GetMe(ctx)
	if err != nil {
		return nil, fmt.Errorf("telegram: get me: %w", err)
	}
	return &Client{api: api, me: me}, nil
}

// Bot receives updates and dispatches them.
type Bot struct {
	*Client
	deps Deps
	log  *slog.Logger

	commands map[string]bool
	sem      chan struct{}
	wg       sync.WaitGroup
}

var botCommands = []telego.BotCommand{
	{Command: "summary", Description: "Пересказ сообщений, начиная с того, на которое вы ответили"},
}

// NewBot creates a Bot.
func NewBot(c *Client, deps Deps, log *slog.Logger) *Bot {
	commands := make(map[string]bool, len(botCommands))
	for _, cmd := range botCommands {
		commands[cmd.Command] = true
	}
	return &Bot{
		Client:   c,
		deps:     deps,
		log:      log.With("bot", c.me.Username),
		commands: commands,
		sem:      make(chan struct{}, maxConcurrentHandlers),
	}
}

// Run receives updates until ctx is canceled, then waits for running handlers.
func (b *Bot) Run(ctx context.Context) error {
	err := b.api.SetMyCommands(ctx, &telego.SetMyCommandsParams{
		Commands: botCommands,
		Scope:    tu.ScopeAllGroupChats(),
	})
	if err != nil {
		return fmt.Errorf("telegram: set commands: %w", err)
	}

	updates, err := b.api.UpdatesViaLongPolling(ctx, &telego.GetUpdatesParams{
		Timeout:        30,
		AllowedUpdates: []string{"message"},
	})
	if err != nil {
		return fmt.Errorf("telegram: long polling: %w", err)
	}
	b.log.InfoContext(ctx, "bot started")

	for u := range updates {
		if u.Message != nil {
			b.onMessage(ctx, u.Message)
		}
	}
	b.wg.Wait()
	b.log.Info("bot stopped")
	return nil
}

// onMessage stores the message synchronously, so the cache keeps Telegram's order,
// and hands anything slow off to a goroutine.
func (b *Bot) onMessage(ctx context.Context, msg *telego.Message) {
	if msg.Chat.Type != telego.ChatTypeGroup && msg.Chat.Type != telego.ChatTypeSupergroup {
		return
	}
	if !b.deps.Allowed(msg.Chat.ID) {
		return
	}

	cmd, isCmd := parseCommand(msg.Text)
	mine := isCmd && cmd.isFor(b.me.Username, b.commands)
	if !mine {
		if err := b.deps.Ingest.Ingest(ctx, toDomain(msg)); err != nil {
			b.log.ErrorContext(ctx, "ingest message", "chat_id", msg.Chat.ID, "err", err)
		}
	}

	switch {
	case mine && cmd.Name == "summary":
		b.spawn(ctx, func(ctx context.Context) { b.handleSummary(ctx, msg) })
	case strings.Contains(msg.Text, "@all"):
		b.spawn(ctx, func(ctx context.Context) { b.handleMentionAll(ctx, msg) })
	}
}

// spawn runs h in a goroutine, blocking while too many handlers are already running.
func (b *Bot) spawn(ctx context.Context, h func(ctx context.Context)) {
	select {
	case b.sem <- struct{}{}:
	case <-ctx.Done():
		return
	}
	b.wg.Go(func() {
		defer func() { <-b.sem }()
		// Let handlers finish their reply after shutdown starts, but not forever.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		h(ctx)
	})
}

func (b *Bot) handleSummary(ctx context.Context, msg *telego.Message) {
	if msg.ReplyToMessage == nil {
		b.notify(ctx, msg, "Ответьте командой /summary на сообщение, с которого начать пересказ.")
		return
	}

	typingCtx, stopTyping := context.WithCancel(ctx)
	go b.typing(typingCtx, msg)
	text, err := b.deps.Summary.Summarize(ctx, msg.Chat.ID, msg.ReplyToMessage.MessageID)
	stopTyping()

	switch {
	case errors.Is(err, summary.ErrNoHistory):
		b.notify(ctx, msg, "Не нашёл сохранённых сообщений начиная с этого. Я вижу только сообщения, отправленные после моего добавления в чат.")
		return
	case err != nil:
		b.log.ErrorContext(ctx, "summarize", "chat_id", msg.Chat.ID, "err", err)
		b.notify(ctx, msg, "Не получилось сделать пересказ, попробуйте позже.")
		return
	}
	if err := b.replyMarkdown(ctx, msg, text); err != nil {
		b.log.ErrorContext(ctx, "send summary", "chat_id", msg.Chat.ID, "err", err)
	}
}

func (b *Bot) handleMentionAll(ctx context.Context, msg *telego.Message) {
	authorID, _, _ := author(msg)
	users, err := b.deps.Mention.Targets(ctx, msg.Chat.ID, authorID)
	if err != nil {
		b.log.ErrorContext(ctx, "mention targets", "chat_id", msg.Chat.ID, "err", err)
		return
	}
	if len(users) == 0 {
		b.notify(ctx, msg, "Не удалось найти пользователей для упоминания.")
		return
	}

	mentions := make([]string, len(users))
	for i, u := range users {
		mentions[i] = `<a href="tg://user?id=` + strconv.FormatInt(u.ID, 10) + `">@` + html.EscapeString(u.Name) + `</a>`
	}
	for _, part := range chunk(mentions, " ", plainMessageLimit) {
		params := tu.Message(msg.Chat.ChatID(), part).
			WithParseMode(telego.ModeHTML).
			WithMessageThreadID(msg.MessageThreadID).
			WithReplyParameters(&telego.ReplyParameters{MessageID: msg.MessageID, AllowSendingWithoutReply: true})
		if _, err := b.api.SendMessage(ctx, params); err != nil {
			b.log.ErrorContext(ctx, "send mentions", "chat_id", msg.Chat.ID, "err", err)
			return
		}
	}
}

// Admins lists the chat's human administrators. It implements mention.Admins.
func (c *Client) Admins(ctx context.Context, chatID int64) ([]domain.User, error) {
	members, err := c.api.GetChatAdministrators(ctx, &telego.GetChatAdministratorsParams{ChatID: tu.ID(chatID)})
	if err != nil {
		return nil, fmt.Errorf("get chat administrators: %w", err)
	}
	var out []domain.User
	for _, m := range members {
		if u := m.MemberUser(); !u.IsBot {
			out = append(out, domain.User{ID: u.ID, Name: fullName(u)})
		}
	}
	return out, nil
}
