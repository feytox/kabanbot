package telegram

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/feytox/kabanbot/internal/app/chat"
	"github.com/feytox/kabanbot/internal/app/settings"
	"github.com/feytox/kabanbot/internal/domain"
)

const (
	// editInterval spaces out edits of a streamed answer in groups, which Telegram rate-limits.
	editInterval = 1500 * time.Millisecond
	// draftInterval spaces out draft updates in private chats.
	draftInterval = 400 * time.Millisecond
)

// Chatter answers people who talk to the bot.
type Chatter interface {
	Reply(ctx context.Context, req chat.Request, progress func(chat.Progress)) (string, error)
}

// draftKey identifies a streamed draft, so the user's "stop" can cancel it.
type draftKey struct {
	chatID  int64
	draftID int
}

// isForBot reports whether a group message talks to the bot: it mentions the bot
// or replies to one of its messages.
func (b *Bot) isForBot(msg *telego.Message) bool {
	if r := msg.ReplyToMessage; r != nil && r.From != nil && r.From.ID == b.me.ID {
		return true
	}
	return strings.Contains(strings.ToLower(content(msg)), "@"+strings.ToLower(b.me.Username))
}

// handleChat answers msg. explicit is true for /ask, whose failures are always explained;
// a mere mention in a chat where chatting is off gets no answer.
func (b *Bot) handleChat(ctx context.Context, msg *telego.Message, u settings.User, explicit bool) {
	var out answer
	if msg.Chat.Type == telego.ChatTypePrivate {
		d := &draftAnswer{b: b, msg: msg, id: rand.IntN(1<<31-2) + 1}
		key := draftKey{msg.Chat.ID, d.id}
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		b.drafts.Store(key, cancel)
		defer func() {
			b.drafts.Delete(key)
			cancel()
		}()
		d.think(ctx, "Думаю…")
		out = d
	} else {
		g := &groupAnswer{b: b, msg: msg}
		typingCtx, stopTyping := context.WithCancel(ctx)
		defer stopTyping()
		go b.typing(typingCtx, msg)
		g.stopTyping = stopTyping
		out = g
	}

	text, err := b.deps.Chat.Reply(ctx, chat.Request{ChatID: msg.Chat.ID, User: u}, func(p chat.Progress) {
		out.progress(ctx, p)
	})
	// A stopped or timed-out answer still gets shown as far as it went.
	ctx = context.WithoutCancel(ctx)
	switch {
	case err == nil:
		if sent := out.finish(ctx, text); sent != nil {
			b.remember(ctx, sent, text)
		}
	case out.started():
		out.finish(ctx, text+"\n\n"+b.chatFailure(ctx, msg, err, true))
	default:
		if note := b.chatFailure(ctx, msg, err, explicit); note != "" {
			out.fail(ctx, note)
		}
	}
}

// chatFailure explains why there is no (full) answer. It returns "" when the bot should stay silent.
func (b *Bot) chatFailure(ctx context.Context, msg *telego.Message, err error, explicit bool) string {
	private := msg.Chat.Type == telego.ChatTypePrivate
	switch {
	case errors.Is(err, context.Canceled):
		return "_Остановлено._"
	case errors.Is(err, chat.ErrDisabled):
		if !explicit {
			return ""
		}
		return "Общение с ботом выключено в настройках этого чата."
	case errors.Is(err, domain.ErrNoModel):
		if private {
			return "Выберите модель для разговора: /settings → «Мой чат»."
		}
		return "В этом чате не выбрана модель. Администратор может выбрать её командой /settings."
	}
	if rl, ok := errors.AsType[*chat.RateLimitError](err); ok {
		who := "в этом чате"
		if rl.PerUser {
			who = "от вас"
		}
		return fmt.Sprintf("Слишком много вопросов %s за час. Попробуйте через %d мин.", who, int(rl.RetryIn.Minutes())+1)
	}
	b.log.ErrorContext(ctx, "chat", "chat_id", msg.Chat.ID, "err", err)
	if d := describeLLMError(err); d != "" {
		return "❌ Не получилось ответить.\n\n" + d
	}
	return "❌ Не получилось ответить, попробуйте позже."
}

// remember stores the bot's answer, so it sees its side of the conversation next time.
func (b *Bot) remember(ctx context.Context, sent *telego.Message, text string) {
	err := b.deps.Ingest.Ingest(ctx, domain.Message{
		ChatID: sent.Chat.ID, MessageID: sent.MessageID, UserID: b.me.ID, Username: b.me.Username,
		Text: text, SentAt: time.Unix(sent.Date, 0), IsBot: true,
	})
	if err != nil {
		b.log.ErrorContext(ctx, "remember answer", "chat_id", sent.Chat.ID, "err", err)
	}
}

// onStopped cancels a draft whose "stop" button the user pressed.
func (b *Bot) onStopped(s *telego.MessageGenerationStopped) {
	if cancel, ok := b.drafts.Load(draftKey{s.Chat.ID, s.DraftID}); ok {
		cancel.(context.CancelFunc)()
	}
}

// answer shows a reply as the model writes it.
type answer interface {
	progress(ctx context.Context, p chat.Progress)
	// started reports whether any of the answer is already shown.
	started() bool
	// finish shows the final text and returns the message holding it, if it was sent.
	finish(ctx context.Context, text string) *telego.Message
	// fail explains that there is no answer.
	fail(ctx context.Context, note string)
}

func toolLabel(name string) string {
	switch name {
	case "set_personality":
		return "Меняю личность…"
	case "reset_personality":
		return "Сбрасываю личность…"
	case "set_summary_style":
		return "Меняю стиль пересказов…"
	}
	return "Смотрю настройки…"
}

// groupAnswer streams by editing one message, sent when the first text arrives.
type groupAnswer struct {
	b          *Bot
	msg        *telego.Message
	sent       *telego.Message
	last       time.Time
	stopTyping func()
}

func (g *groupAnswer) started() bool { return g.sent != nil }

func (g *groupAnswer) progress(ctx context.Context, p chat.Progress) {
	text := p.Text
	if p.Tool != "" {
		text = strings.TrimSpace(text + "\n\n_" + toolLabel(p.Tool) + "_")
	}
	if text == "" || time.Since(g.last) < editInterval {
		return
	}
	g.last = time.Now()
	text = tail(text, richMessageLimit)
	if g.sent == nil {
		// Partial Markdown may be rejected; the next chunk tries again.
		sent, err := g.b.api.SendRichMessage(ctx, &telego.SendRichMessageParams{
			ChatID:          g.msg.Chat.ChatID(),
			MessageThreadID: g.msg.MessageThreadID,
			RichMessage:     telego.InputRichMessage{Markdown: text},
			ReplyParameters: &telego.ReplyParameters{MessageID: g.msg.MessageID, AllowSendingWithoutReply: true},
		})
		if err != nil {
			g.b.log.DebugContext(ctx, "send partial answer", "err", err)
			return
		}
		g.sent = sent
		g.stopTyping()
		return
	}
	if err := g.b.editRich(ctx, g.sent, text); err != nil {
		g.b.log.DebugContext(ctx, "edit partial answer", "err", err)
	}
}

func (g *groupAnswer) finish(ctx context.Context, text string) *telego.Message {
	g.stopTyping()
	if strings.TrimSpace(text) == "" {
		text = "🤷 Модель ничего не ответила."
	}
	if g.sent != nil {
		parts := split(text, richMessageLimit)
		if err := g.b.editRich(ctx, g.sent, parts[0]); err == nil {
			if len(parts) > 1 {
				g.reply(ctx, strings.Join(parts[1:], "\n\n"))
			}
			return g.sent
		}
		// The final Markdown is rejected too; start over, with a plain-text fallback.
		if err := g.b.api.DeleteMessage(ctx, &telego.DeleteMessageParams{ChatID: g.msg.Chat.ChatID(), MessageID: g.sent.MessageID}); err != nil {
			g.b.log.WarnContext(ctx, "delete partial answer", "err", err)
		}
	}
	return g.reply(ctx, text)
}

func (g *groupAnswer) reply(ctx context.Context, text string) *telego.Message {
	sent, err := g.b.replyMarkdown(ctx, g.msg, text)
	if err != nil {
		g.b.log.ErrorContext(ctx, "send answer", "chat_id", g.msg.Chat.ID, "err", err)
	}
	return sent
}

func (g *groupAnswer) fail(ctx context.Context, note string) {
	g.stopTyping()
	g.b.notify(ctx, g.msg, strings.Trim(note, "_"))
}

// draftAnswer streams through message drafts, which the user can stop, then sends the answer.
type draftAnswer struct {
	b    *Bot
	msg  *telego.Message
	id   int
	last time.Time
	text bool
}

func (d *draftAnswer) started() bool { return d.text }

// think shows a "thinking" placeholder with a stop button.
func (d *draftAnswer) think(ctx context.Context, label string) {
	d.draft(ctx, telego.InputRichMessage{HTML: "<tg-thinking>" + label + "</tg-thinking>"})
}

func (d *draftAnswer) draft(ctx context.Context, m telego.InputRichMessage) {
	err := d.b.api.SendRichMessageDraft(ctx, &telego.SendRichMessageDraftParams{
		ChatID: d.msg.Chat.ID, DraftID: d.id, RichMessage: m, CanStop: true,
	})
	if err != nil && ctx.Err() == nil {
		d.b.log.DebugContext(ctx, "send draft", "err", err)
	}
}

func (d *draftAnswer) progress(ctx context.Context, p chat.Progress) {
	if p.Tool != "" {
		d.think(ctx, toolLabel(p.Tool))
		return
	}
	if p.Text == "" || time.Since(d.last) < draftInterval {
		return
	}
	d.last, d.text = time.Now(), true
	d.draft(ctx, telego.InputRichMessage{Markdown: tail(p.Text, richMessageLimit)})
}

func (d *draftAnswer) finish(ctx context.Context, text string) *telego.Message {
	if strings.TrimSpace(text) == "" {
		text = "🤷 Модель ничего не ответила."
	}
	var first *telego.Message
	for _, part := range split(text, richMessageLimit) {
		sent, err := d.b.api.SendRichMessage(ctx, &telego.SendRichMessageParams{
			ChatID: d.msg.Chat.ChatID(), RichMessage: telego.InputRichMessage{Markdown: part},
		})
		if err != nil {
			d.b.log.WarnContext(ctx, "send rich answer, falling back to plain text", "err", err)
			if sent, err = d.b.api.SendMessage(ctx, tu.Message(d.msg.Chat.ChatID(), part)); err != nil {
				d.b.log.ErrorContext(ctx, "send answer", "err", err)
				return first
			}
		}
		if first == nil {
			first = sent
		}
	}
	return first
}

func (d *draftAnswer) fail(ctx context.Context, note string) {
	d.b.sendText(ctx, d.msg.Chat.ID, strings.Trim(note, "_"))
}

// editRich replaces a message's content with Markdown.
func (b *Bot) editRich(ctx context.Context, m *telego.Message, markdown string) error {
	_, err := b.api.EditMessageText(ctx, &telego.EditMessageTextParams{
		ChatID: m.Chat.ChatID(), MessageID: m.MessageID, RichMessage: &telego.InputRichMessage{Markdown: markdown},
	})
	if err != nil && strings.Contains(err.Error(), "message is not modified") {
		return nil
	}
	return err
}

// typing shows the "typing…" status in the chat until ctx is canceled.
func (b *Bot) typing(ctx context.Context, msg *telego.Message) {
	params := tu.ChatAction(msg.Chat.ChatID(), telego.ChatActionTyping).WithMessageThreadID(msg.MessageThreadID)
	t := time.NewTicker(4 * time.Second) // the status lasts about 5 seconds
	defer t.Stop()
	for {
		if err := b.api.SendChatAction(ctx, params); err != nil && ctx.Err() == nil {
			b.log.DebugContext(ctx, "send chat action", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// tail keeps the end of a streamed text within limit characters.
func tail(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return "…" + string(r[len(r)-limit+1:])
}
