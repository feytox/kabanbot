package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/feytox/kabanbot/internal/app/summary"
	"github.com/feytox/kabanbot/internal/domain"
	"github.com/feytox/kabanbot/internal/llm"
)

const summaryPending = "⏳ Делаю пересказ…"

// handleSummary answers /summary at once with a status message that everyone in the group sees,
// keeps it updated while the model is retried, and finally replaces it with the summary or the error.
func (b *Bot) handleSummary(ctx context.Context, msg *telego.Message) {
	if s := b.features(ctx, msg.Chat.ID); !s.Enabled || !s.Summary {
		b.notify(ctx, msg, "Пересказы выключены в настройках этого чата.")
		return
	}
	if msg.ReplyToMessage == nil {
		b.notify(ctx, msg, "Ответьте командой /summary на сообщение, с которого начать пересказ.")
		return
	}

	status, err := b.api.SendMessage(ctx, tu.Message(msg.Chat.ChatID(), summaryPending).
		WithMessageThreadID(msg.MessageThreadID).
		WithReplyParameters(&telego.ReplyParameters{MessageID: msg.MessageID, AllowSendingWithoutReply: true}))
	if err != nil {
		b.log.WarnContext(ctx, "send summary status", "chat_id", msg.Chat.ID, "err", err)
		status = nil
	}

	retryCtx := llm.WithRetryObserver(ctx, func(e llm.RetryEvent) {
		b.setStatus(ctx, msg, status, fmt.Sprintf("%s\n\n%s: %s. Повторю через %.0f с (попытка %d из %d).",
			summaryPending, e.Err.Provider, problem(e.Err), e.Delay.Seconds(), e.Attempt+1, e.Attempts))
	})
	userID, _, _ := author(msg)
	text, err := b.deps.Summary.Summarize(retryCtx, msg.Chat.ID, userID, msg.ReplyToMessage.MessageID)
	if err != nil {
		b.setStatus(ctx, msg, status, b.summaryError(ctx, msg.Chat.ID, err))
		return
	}
	b.showSummary(ctx, msg, status, text)
}

func (b *Bot) summaryError(ctx context.Context, chatID int64, err error) string {
	switch {
	case errors.Is(err, summary.ErrNoHistory):
		return "Не нашёл сохранённых сообщений начиная с этого. Я вижу только сообщения, отправленные после моего добавления в чат."
	case errors.Is(err, domain.ErrNoModel):
		return "В этом чате не выбрана модель для пересказов. Администратор может выбрать её командой /settings."
	}
	b.log.ErrorContext(ctx, "summarize", "chat_id", chatID, "err", err)
	text := "❌ Не получилось сделать пересказ."
	if d := describeLLMError(err); d != "" {
		return text + "\n\n" + d
	}
	return text + " Попробуйте позже."
}

// setStatus replaces the status message's text, or sends the text as a reply if there is no status.
func (b *Bot) setStatus(ctx context.Context, msg, status *telego.Message, text string) {
	if status == nil {
		if err := b.replyPlain(ctx, msg, text); err != nil {
			b.log.ErrorContext(ctx, "send summary status", "chat_id", msg.Chat.ID, "err", err)
		}
		return
	}
	_, err := b.api.EditMessageText(ctx, &telego.EditMessageTextParams{
		ChatID: msg.Chat.ChatID(), MessageID: status.MessageID, Text: text,
	})
	if err != nil && !strings.Contains(err.Error(), "message is not modified") {
		b.log.WarnContext(ctx, "edit summary status", "chat_id", msg.Chat.ID, "err", err)
	}
}

// showSummary turns the status message into the summary. If Telegram will not edit it
// into a rich message, the status is deleted and the summary sent anew.
func (b *Bot) showSummary(ctx context.Context, msg, status *telego.Message, markdown string) {
	if status != nil {
		parts := split(markdown, richMessageLimit)
		_, err := b.api.EditMessageText(ctx, &telego.EditMessageTextParams{
			ChatID: msg.Chat.ChatID(), MessageID: status.MessageID,
			RichMessage: &telego.InputRichMessage{Markdown: parts[0]},
		})
		if err == nil {
			if len(parts) == 1 {
				return
			}
			markdown = strings.Join(parts[1:], "\n\n")
		} else {
			b.log.WarnContext(ctx, "edit status into summary, sending anew", "chat_id", msg.Chat.ID, "err", err)
			if err := b.api.DeleteMessage(ctx, &telego.DeleteMessageParams{ChatID: msg.Chat.ChatID(), MessageID: status.MessageID}); err != nil {
				b.log.WarnContext(ctx, "delete summary status", "chat_id", msg.Chat.ID, "err", err)
			}
		}
	}
	if err := b.replyMarkdown(ctx, msg, markdown); err != nil {
		b.log.ErrorContext(ctx, "send summary", "chat_id", msg.Chat.ID, "err", err)
	}
}

// describeLLMError explains a failed model call in a few lines safe to show in a group.
// It returns "" for errors it knows nothing about.
func describeLLMError(err error) string {
	perr, ok := errors.AsType[*llm.Error](err)
	switch {
	case ok:
	case errors.Is(err, context.DeadlineExceeded):
		return "Модель не успела ответить."
	default:
		return ""
	}

	var s strings.Builder
	s.WriteString(perr.Provider + ": " + problem(perr) + ".")
	if hint := advice(perr); hint != "" {
		s.WriteString(" " + hint)
	}
	if m := strings.TrimSpace(perr.Message); m != "" && perr.Status != 0 {
		s.WriteString("\nОтвет провайдера: «" + truncate(m, 300) + "»")
	}
	if perr.Attempts > 1 {
		fmt.Fprintf(&s, "\nПопыток: %d.", perr.Attempts)
	}
	return s.String()
}

// problem names what went wrong, with the HTTP status for those who want to look it up.
func problem(e *llm.Error) string {
	switch e.Status {
	case 0:
		return "не удалось связаться с сервером"
	case 400:
		return "запрос отклонён (400)"
	case 401, 403:
		return fmt.Sprintf("ключ API не подошёл (%d)", e.Status)
	case 404:
		return "модель не найдена (404)"
	case 429:
		return "превышен лимит запросов (429)"
	case 500:
		return "внутренняя ошибка сервера (500)"
	case 503:
		return "модель перегружена (503)"
	}
	if e.Status > 500 {
		return fmt.Sprintf("сервер недоступен (%d)", e.Status)
	}
	return fmt.Sprintf("ошибка %d", e.Status)
}

func advice(e *llm.Error) string {
	switch {
	case e.Status == 401 || e.Status == 403:
		return "Владельцу модели стоит проверить ключ."
	case e.Status == 404:
		return "Владельцу модели стоит проверить её ID."
	case e.Status == 429:
		return "На бесплатных тарифах минутный лимит скоро сбросится, дневной — через сутки."
	case e.Temporary():
		return "Обычно это ненадолго, попробуйте через пару минут."
	}
	return ""
}
