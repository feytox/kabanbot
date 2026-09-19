// Package telegram is the Telegram Bot API adapter: it turns updates into use-case calls
// and renders their results.
package telegram

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/feytox/kabanbot/internal/app/settings"
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
	Summarize(ctx context.Context, chatID, userID int64, fromMessageID int) (string, error)
}

// Mentioner resolves @all targets.
type Mentioner interface {
	Targets(ctx context.Context, chatID, authorID int64) ([]domain.User, error)
}

// Chats tracks the groups the bot is in and their settings.
type Chats interface {
	TouchChat(ctx context.Context, id int64, title string, member bool) error
	ChatSettings(ctx context.Context, id int64) (domain.ChatSettings, error)
}

// Deps are the use cases the bot dispatches to.
type Deps struct {
	Ingest  Ingester
	Summary Summarizer
	Mention Mentioner
	Chats   Chats
	// Settings backs the settings menus and makes all their authorization decisions.
	Settings *settings.Service
	// Allowed reports whether the bot may operate in the chat.
	Allowed func(chatID int64) bool
}

// Client is a connected Bot API client.
type Client struct {
	api    *telego.Bot
	me     *telego.User
	admins adminCache
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
	menu     *menu
	dialogs  dialogs
	sem      chan struct{}
	wg       sync.WaitGroup

	// titles caches known chat titles so chats are written to the store only when they change.
	titles sync.Map // chat ID -> title
}

var (
	groupCommands = []telego.BotCommand{
		{Command: "summary", Description: "Пересказ сообщений, начиная с того, на которое вы ответили"},
		{Command: "settings", Description: "Настройки бота в этом чате", IsEphemeral: true},
	}
	privateCommands = []telego.BotCommand{
		{Command: "settings", Description: "Мои модели и группы"},
		{Command: "cancel", Description: "Отменить ввод"},
	}
)

// NewBot creates a Bot.
func NewBot(c *Client, deps Deps, log *slog.Logger) *Bot {
	commands := make(map[string]bool, len(groupCommands))
	for _, cmd := range groupCommands {
		commands[cmd.Command] = true
	}
	return &Bot{
		Client:   c,
		deps:     deps,
		log:      log.With("bot", c.me.Username),
		commands: commands,
		menu:     &menu{svc: deps.Settings, botUsername: c.me.Username},
		sem:      make(chan struct{}, maxConcurrentHandlers),
	}
}

// Run receives updates until ctx is canceled, then waits for running handlers.
func (b *Bot) Run(ctx context.Context) error {
	if err := b.setup(ctx); err != nil {
		return err
	}
	updates, err := b.api.UpdatesViaLongPolling(ctx, &telego.GetUpdatesParams{
		Timeout:        30,
		AllowedUpdates: []string{"message", "callback_query", "my_chat_member"},
	})
	if err != nil {
		return fmt.Errorf("telegram: long polling: %w", err)
	}
	b.log.InfoContext(ctx, "bot started")

	for u := range updates {
		switch {
		case u.Message != nil:
			b.onMessage(ctx, u.Message)
		case u.CallbackQuery != nil:
			b.spawn(ctx, func(ctx context.Context) { b.handleCallback(ctx, u.CallbackQuery) })
		case u.MyChatMember != nil:
			b.onMembership(ctx, u.MyChatMember)
		}
	}
	b.wg.Wait()
	b.log.Info("bot stopped")
	return nil
}

// setup registers commands and resets the menu button.
func (b *Bot) setup(ctx context.Context) error {
	for _, c := range []*telego.SetMyCommandsParams{
		{Commands: groupCommands, Scope: tu.ScopeAllGroupChats()},
		{Commands: privateCommands, Scope: tu.ScopeAllPrivateChats()},
	} {
		if err := b.api.SetMyCommands(ctx, c); err != nil {
			return fmt.Errorf("telegram: set commands: %w", err)
		}
	}
	// Earlier versions pointed the menu button at a Mini App.
	err := b.api.SetChatMenuButton(ctx, &telego.SetChatMenuButtonParams{
		MenuButton: &telego.MenuButtonCommands{Type: telego.ButtonTypeCommands},
	})
	if err != nil {
		return fmt.Errorf("telegram: set menu button: %w", err)
	}
	return nil
}

func isGroup(c telego.Chat) bool {
	return c.Type == telego.ChatTypeGroup || c.Type == telego.ChatTypeSupergroup
}

// onMessage stores the message synchronously, so the cache keeps Telegram's order,
// and hands anything slow off to a goroutine.
func (b *Bot) onMessage(ctx context.Context, msg *telego.Message) {
	if msg.Chat.Type == telego.ChatTypePrivate {
		b.onPrivateMessage(ctx, msg)
		return
	}
	if !isGroup(msg.Chat) || !b.deps.Allowed(msg.Chat.ID) {
		return
	}
	b.touchChat(ctx, msg.Chat, true)

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
	case mine && cmd.Name == "settings":
		b.spawn(ctx, func(ctx context.Context) { b.handleGroupSettings(ctx, msg) })
	case strings.Contains(msg.Text, "@all"):
		b.spawn(ctx, func(ctx context.Context) { b.handleMentionAll(ctx, msg) })
	}
}

// onMembership tracks the groups the bot is added to or removed from.
func (b *Bot) onMembership(ctx context.Context, u *telego.ChatMemberUpdated) {
	if !isGroup(u.Chat) {
		return
	}
	b.touchChat(ctx, u.Chat, u.NewChatMember.MemberIsMember())
}

// touchChat records the chat in the store when it is new, renamed, or the bot joined or left.
func (b *Bot) touchChat(ctx context.Context, c telego.Chat, member bool) {
	if member {
		if title, ok := b.titles.Load(c.ID); ok && title == c.Title {
			return
		}
	}
	if err := b.deps.Chats.TouchChat(ctx, c.ID, c.Title, member); err != nil {
		b.log.ErrorContext(ctx, "track chat", "chat_id", c.ID, "err", err)
		return
	}
	if member {
		b.titles.Store(c.ID, c.Title)
	} else {
		b.titles.Delete(c.ID)
	}
}

// features returns the chat's settings, falling back to defaults if they cannot be read.
func (b *Bot) features(ctx context.Context, chatID int64) domain.ChatSettings {
	s, err := b.deps.Chats.ChatSettings(ctx, chatID)
	if err != nil {
		b.log.ErrorContext(ctx, "load chat settings", "chat_id", chatID, "err", err)
		return domain.DefaultChatSettings()
	}
	return s
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
		// A summary may wait out a provider's rate limit, hence the generous timeout.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Minute)
		defer cancel()
		h(ctx)
	})
}

func (b *Bot) handleMentionAll(ctx context.Context, msg *telego.Message) {
	if s := b.features(ctx, msg.Chat.ID); !s.Enabled || !s.MentionAll {
		return
	}
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
