package telegram

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

const (
	// richMessageLimit is the maximum text length of a rich message (Bot API 10.1).
	richMessageLimit = 32768
	// plainMessageLimit is the maximum text length of a regular message.
	plainMessageLimit = 4096
	// anonymousAdminID is the user Telegram shows for messages of anonymous group admins.
	anonymousAdminID = 1087968824
)

// replyMarkdown sends Markdown as rich messages replying to msg and returns the first one.
// If Telegram rejects the formatting, the text is resent as plain messages.
func (b *Bot) replyMarkdown(ctx context.Context, msg *telego.Message, markdown string) (*telego.Message, error) {
	reply := &telego.ReplyParameters{MessageID: msg.MessageID, AllowSendingWithoutReply: true}
	parts := split(markdown, richMessageLimit)
	var first *telego.Message
	for i, part := range parts {
		sent, err := b.api.SendRichMessage(ctx, &telego.SendRichMessageParams{
			ChatID:          msg.Chat.ChatID(),
			MessageThreadID: msg.MessageThreadID,
			RichMessage:     telego.InputRichMessage{Markdown: part},
			ReplyParameters: reply,
		})
		if err != nil {
			b.log.WarnContext(ctx, "send rich message, falling back to plain text", "chat_id", msg.Chat.ID, "err", err)
			plain, err := b.replyPlain(ctx, msg, strings.Join(parts[i:], "\n\n"))
			return cmp.Or(first, plain), err
		}
		first = cmp.Or(first, sent)
		reply = nil // only the first part replies to the command
	}
	return first, nil
}

// replyPlain sends unformatted text replying to msg and returns the first message.
func (b *Bot) replyPlain(ctx context.Context, msg *telego.Message, text string) (*telego.Message, error) {
	var first *telego.Message
	for _, part := range split(text, plainMessageLimit) {
		params := tu.Message(msg.Chat.ChatID(), part).
			WithMessageThreadID(msg.MessageThreadID).
			WithReplyParameters(&telego.ReplyParameters{MessageID: msg.MessageID, AllowSendingWithoutReply: true})
		sent, err := b.api.SendMessage(ctx, params)
		if err != nil {
			return first, fmt.Errorf("send message: %w", err)
		}
		first = cmp.Or(first, sent)
	}
	return first, nil
}

// notify sends a short service message that only the author of msg can see.
// It falls back to a regular reply when the author cannot receive ephemeral messages.
func (b *Bot) notify(ctx context.Context, msg *telego.Message, text string) {
	b.notifyParams(ctx, msg, tu.Message(msg.Chat.ChatID(), text))
}

// notifyScreen is notify with a menu screen.
func (b *Bot) notifyScreen(ctx context.Context, msg *telego.Message, s screen) {
	b.notifyParams(ctx, msg, tu.Message(msg.Chat.ChatID(), s.text).WithParseMode(telego.ModeHTML).WithReplyMarkup(s.markup()))
}

func (b *Bot) notifyParams(ctx context.Context, msg *telego.Message, params *telego.SendMessageParams) {
	params.MessageThreadID = msg.MessageThreadID
	if u, ok := sender(msg); ok {
		params.EphemeralMessageParameters = &telego.EphemeralMessageParameters{ReceiverUserID: int(u.ID)}
		_, err := b.api.SendMessage(ctx, params)
		if err == nil {
			return
		}
		b.log.WarnContext(ctx, "send ephemeral message, falling back to reply", "chat_id", msg.Chat.ID, "err", err)
		params.EphemeralMessageParameters = nil
	}
	params.ReplyParameters = &telego.ReplyParameters{MessageID: msg.MessageID, AllowSendingWithoutReply: true}
	if _, err := b.api.SendMessage(ctx, params); err != nil {
		b.log.ErrorContext(ctx, "send notice", "chat_id", msg.Chat.ID, "err", err)
	}
}

// split cuts text into parts of at most limit characters,
// preferring paragraph, then line, then word boundaries.
func split(text string, limit int) []string {
	var parts []string
	for utf8.RuneCountInString(text) > limit {
		cut := byteOffset(text, limit)
		head := text[:cut]
		for _, sep := range []string{"\n\n", "\n", " "} {
			if i := strings.LastIndex(head, sep); i > 0 {
				cut = i
				break
			}
		}
		parts = append(parts, strings.TrimSpace(text[:cut]))
		text = strings.TrimSpace(text[cut:])
	}
	if text != "" {
		parts = append(parts, text)
	}
	return parts
}

// byteOffset returns the byte offset of the n-th rune in s.
func byteOffset(s string, n int) int {
	for i := range s {
		if n == 0 {
			return i
		}
		n--
	}
	return len(s)
}

// chunk joins items with sep into strings of at most limit bytes without splitting an item.
// An item longer than limit gets a chunk of its own.
func chunk(items []string, sep string, limit int) []string {
	var out []string
	var cur strings.Builder
	for _, it := range items {
		if cur.Len() > 0 && cur.Len()+len(sep)+len(it) > limit {
			out = append(out, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteString(sep)
		}
		cur.WriteString(it)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
