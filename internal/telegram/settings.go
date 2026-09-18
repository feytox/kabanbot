package telegram

import (
	"context"
	"strconv"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

// startParamChatPrefix prefixes the chat ID in Mini App start parameters, e.g. "chat_-1001234".
const startParamChatPrefix = "chat_"

// handleGroupSettings shows the caller a link to this chat's settings.
// Web App buttons only work in private chats, so the link opens the bot's Main Mini App.
func (b *Bot) handleGroupSettings(ctx context.Context, msg *telego.Message) {
	if b.deps.WebAppURL == "" {
		b.notify(ctx, msg, "Настройки через Mini App пока не подключены.")
		return
	}
	link := "https://t.me/" + b.me.Username + "?startapp=" + startParamChatPrefix + strconv.FormatInt(msg.Chat.ID, 10)
	markup := tu.InlineKeyboard(tu.InlineKeyboardRow(tu.InlineKeyboardButton("Открыть настройки").WithURL(link)))
	b.notifyWithMarkup(ctx, msg, "Настройки бота в этом чате доступны администраторам.", markup)
}

// onPrivateMessage answers /start and /settings in a private chat with a Mini App button.
func (b *Bot) onPrivateMessage(ctx context.Context, msg *telego.Message) {
	cmd, ok := parseCommand(msg.Text)
	if !ok || (cmd.Name != "start" && cmd.Name != "settings") {
		return
	}
	b.spawn(ctx, func(ctx context.Context) {
		text := "Привет! Я делаю пересказы переписки в группах: ответьте командой /summary на сообщение, с которого начать."
		params := tu.Message(msg.Chat.ChatID(), text)
		if b.deps.WebAppURL != "" {
			params.Text += "\n\nВ настройках можно подключить свои модели и выбрать, какую модель использовать в каждой группе."
			params.ReplyMarkup = tu.InlineKeyboard(tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("Открыть настройки").WithWebApp(tu.WebAppInfo(b.deps.WebAppURL)),
			))
		}
		if _, err := b.api.SendMessage(ctx, params); err != nil {
			b.log.ErrorContext(ctx, "send start message", "err", err)
		}
	})
}
