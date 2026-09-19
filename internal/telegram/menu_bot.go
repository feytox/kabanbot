package telegram

import (
	"context"
	"strings"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/feytox/kabanbot/internal/app/settings"
)

// startParamModels is the /start parameter that opens the user's models, e.g. t.me/<bot>?start=models.
const startParamModels = "models"

// sender returns the user who sent msg. It reports false for messages sent on behalf of a chat
// or an anonymous admin, whose rights cannot be checked.
func sender(msg *telego.Message) (settings.User, bool) {
	u := msg.From
	if u == nil || u.IsBot || u.ID == anonymousAdminID || msg.SenderChat != nil {
		return settings.User{}, false
	}
	return toUser(*u), true
}

func toUser(u telego.User) settings.User {
	return settings.User{ID: u.ID, Username: u.Username, FirstName: u.FirstName}
}

// errorText returns what to tell the user about err, logging unexpected errors.
func (b *Bot) errorText(ctx context.Context, err error) string {
	text, expected := userError(err)
	if !expected {
		b.log.ErrorContext(ctx, "settings", "err", err)
	}
	return text
}

// seen records the user's names so others see whose model a group uses or who shares one.
func (b *Bot) seen(ctx context.Context, u settings.User) {
	if err := b.deps.Settings.Seen(ctx, u); err != nil {
		b.log.WarnContext(ctx, "record user", "user_id", u.ID, "err", err)
	}
}

// handleGroupSettings shows the chat's settings menu to the admin who called /settings.
// The menu is ephemeral, so the rest of the group does not see it.
func (b *Bot) handleGroupSettings(ctx context.Context, msg *telego.Message) {
	u, ok := sender(msg)
	if !ok {
		b.notify(ctx, msg, "Настройки нельзя открыть от имени группы или анонимного администратора. Откройте их в личке с ботом.")
		return
	}
	b.seen(ctx, u)
	out, err := b.menu.handle(ctx, view{user: u}, route{op: opGroup, id: msg.Chat.ID})
	if err != nil {
		b.notify(ctx, msg, b.errorText(ctx, err))
		return
	}
	b.notifyScreen(ctx, msg, *out.screen)
}

// onPrivateMessage handles commands and dialog answers in the user's chat with the bot.
func (b *Bot) onPrivateMessage(ctx context.Context, msg *telego.Message) {
	u, ok := sender(msg)
	if !ok {
		return
	}
	cmd, isCmd := parseCommand(msg.Text)
	if !isCmd {
		// An answer to a dialog may be an API key: it must never reach the history.
		if b.dialogs.has(u.ID) {
			b.spawn(ctx, func(ctx context.Context) { b.handleDialogAnswer(ctx, msg, u) })
			return
		}
		b.touchChat(ctx, msg.Chat.ID, fullName(*msg.From), false)
		b.ingest(ctx, toDomain(msg))
		b.spawn(ctx, func(ctx context.Context) { b.handleChat(ctx, msg, u, true) })
		return
	}
	// Any command ends the dialog, so a forgotten one never swallows a later message.
	hadDialog := b.dialogs.cancel(u.ID)
	switch cmd.Name {
	case "start", "settings":
		b.touchChat(ctx, msg.Chat.ID, fullName(*msg.From), false)
		r := route{op: opHome}
		if _, arg, _ := strings.Cut(msg.Text, " "); strings.TrimSpace(arg) == startParamModels {
			r = route{op: opProviders}
		}
		b.spawn(ctx, func(ctx context.Context) {
			b.seen(ctx, u)
			b.showNew(ctx, msg.Chat.ID, u, r)
		})
	case "cancel":
		text := "Нечего отменять."
		if hadDialog {
			text = "Отменено."
		}
		b.spawn(ctx, func(ctx context.Context) { b.sendText(ctx, msg.Chat.ID, text) })
	}
}

// handleDialogAnswer feeds a private message to the user's dialog, if there is one.
func (b *Bot) handleDialogAnswer(ctx context.Context, msg *telego.Message, u settings.User) {
	reply, ok, err := b.dialogs.answer(ctx, u.ID, msg.Text)
	if !ok {
		return
	}
	if reply.deleteInput {
		err := b.api.DeleteMessage(ctx, &telego.DeleteMessageParams{ChatID: msg.Chat.ChatID(), MessageID: msg.MessageID})
		if err != nil {
			b.log.ErrorContext(ctx, "delete secret message", "err", err)
			b.sendText(ctx, msg.Chat.ID, "Не получилось удалить сообщение с ключом, удалите его сами.")
		}
	}
	if err != nil {
		b.sendText(ctx, msg.Chat.ID, b.errorText(ctx, err))
		return
	}
	if reply.notice != "" {
		b.sendText(ctx, msg.Chat.ID, reply.notice)
	}
	if reply.prompt != nil {
		b.ask(ctx, msg.Chat.ID, *reply.prompt)
	}
	if reply.next != nil {
		b.showNew(ctx, msg.Chat.ID, u, *reply.next)
	}
}

// showNew sends the route's screen as a new message in the private chat.
func (b *Bot) showNew(ctx context.Context, chatID int64, u settings.User, r route) {
	out, err := b.menu.handle(ctx, view{user: u, private: true}, r)
	if err != nil {
		b.sendText(ctx, chatID, b.errorText(ctx, err))
		return
	}
	if out.screen != nil {
		params := tu.Message(tu.ID(chatID), out.screen.text).WithParseMode(telego.ModeHTML).WithReplyMarkup(out.screen.markup())
		if _, err := b.api.SendMessage(ctx, params); err != nil {
			b.log.ErrorContext(ctx, "send menu", "err", err)
		}
	}
	if out.dialog != nil {
		b.ask(ctx, chatID, b.dialogs.start(u.ID, out.dialog))
	}
}

// ask sends a dialog question that the user answers with their next message.
func (b *Bot) ask(ctx context.Context, chatID int64, s step) {
	params := tu.Message(tu.ID(chatID), s.prompt+"\n\nОтменить: /cancel").
		WithReplyMarkup(&telego.ForceReply{ForceReply: true, InputFieldPlaceholder: s.placeholder})
	if _, err := b.api.SendMessage(ctx, params); err != nil {
		b.log.ErrorContext(ctx, "send prompt", "err", err)
	}
}

func (b *Bot) sendText(ctx context.Context, chatID int64, text string) {
	if _, err := b.api.SendMessage(ctx, tu.Message(tu.ID(chatID), text)); err != nil {
		b.log.ErrorContext(ctx, "send message", "err", err)
	}
}

// handleCallback carries out a menu button press. The callback data and the message are
// untrusted: anyone who sees the button may press it, and data can be forged.
func (b *Bot) handleCallback(ctx context.Context, q *telego.CallbackQuery) {
	answered := false
	answer := func(text string, alert bool) {
		if answered {
			return
		}
		answered = true
		err := b.api.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: q.ID, Text: text, ShowAlert: alert})
		if err != nil {
			b.log.WarnContext(ctx, "answer callback", "err", err)
		}
	}
	defer answer("", false)

	r, err := parseRoute(q.Data)
	if err != nil {
		answer(b.errorText(ctx, err), false)
		return
	}
	msg, ok := q.Message.(*telego.Message)
	if !ok {
		answer("Меню устарело. Откройте его заново: /settings", false)
		return
	}
	private := msg.Chat.Type == telego.ChatTypePrivate
	if !private {
		// A menu in a group only manages that group.
		otherChat := groupRoute(r.op) && r.id != 0 && r.id != msg.Chat.ID
		if !isGroup(msg.Chat) || !b.deps.Allowed(msg.Chat.ID) || otherChat {
			answer(b.errorText(ctx, errBadRoute), false)
			return
		}
	}

	u := toUser(q.From)
	switch r.op {
	case opClose:
		// Only its receiver sees an ephemeral menu; a regular one in a group is closed by its admins.
		if msg.EphemeralMessageID == 0 && !private {
			if _, err := b.deps.Settings.Chat(ctx, u, msg.Chat.ID); err != nil {
				answer(b.errorText(ctx, err), true)
				return
			}
		}
		b.deleteScreen(ctx, msg, u.ID)
		return
	case opModelTest:
		// The test may take a while; Telegram wants an answer sooner.
		answer("Проверяю модель…", false)
	}

	out, err := b.menu.handle(ctx, view{user: u, private: private}, r)
	if err != nil {
		text := b.errorText(ctx, err)
		if answered {
			b.sendText(ctx, msg.Chat.ID, text)
		}
		answer(text, true)
		return
	}
	if out.screen != nil {
		b.editScreen(ctx, msg, u.ID, *out.screen)
	}
	if out.dialog != nil {
		b.ask(ctx, msg.Chat.ID, b.dialogs.start(u.ID, out.dialog))
	}
	answer(out.toast, false)
}

// editScreen replaces the menu message with s.
func (b *Bot) editScreen(ctx context.Context, msg *telego.Message, receiverID int64, s screen) {
	var err error
	if msg.EphemeralMessageID != 0 {
		err = b.api.EditEphemeralMessageText(ctx, &telego.EditEphemeralMessageTextParams{
			ChatID:             msg.Chat.ChatID(),
			ReceiverUserID:     receiverID,
			EphemeralMessageID: msg.EphemeralMessageID,
			Text:               s.text,
			ParseMode:          telego.ModeHTML,
			ReplyMarkup:        s.markup(),
		})
	} else {
		_, err = b.api.EditMessageText(ctx, &telego.EditMessageTextParams{
			ChatID:      msg.Chat.ChatID(),
			MessageID:   msg.MessageID,
			Text:        s.text,
			ParseMode:   telego.ModeHTML,
			ReplyMarkup: s.markup(),
		})
	}
	if err != nil && !strings.Contains(err.Error(), "message is not modified") {
		b.log.ErrorContext(ctx, "edit menu", "chat_id", msg.Chat.ID, "err", err)
	}
}

// deleteScreen removes the menu message.
func (b *Bot) deleteScreen(ctx context.Context, msg *telego.Message, receiverID int64) {
	var err error
	if msg.EphemeralMessageID != 0 {
		err = b.api.DeleteEphemeralMessage(ctx, &telego.DeleteEphemeralMessageParams{
			ChatID: msg.Chat.ChatID(), ReceiverUserID: receiverID, EphemeralMessageID: msg.EphemeralMessageID,
		})
	} else {
		err = b.api.DeleteMessage(ctx, &telego.DeleteMessageParams{ChatID: msg.Chat.ChatID(), MessageID: msg.MessageID})
	}
	if err != nil {
		b.log.WarnContext(ctx, "delete menu", "chat_id", msg.Chat.ID, "err", err)
	}
}
