package telegram

import (
	"cmp"
	"strings"
	"time"

	"github.com/mymmrac/telego"

	"github.com/feytox/kabanbot/internal/domain"
)

// toDomain converts a Telegram message to a cacheable domain message.
func toDomain(m *telego.Message) domain.Message {
	out := domain.Message{
		ChatID:    m.Chat.ID,
		MessageID: m.MessageID,
		Text:      content(m),
		SentAt:    time.Unix(m.Date, 0),
	}
	out.UserID, out.Username, out.IsBot = author(m)
	if r := m.ReplyToMessage; r != nil {
		_, name, _ := author(r)
		out.ReplyTo = &domain.Quote{Username: name, Text: content(r)}
	}
	return out
}

// author returns who sent the message. Messages sent on behalf of a chat get user ID 0.
func author(m *telego.Message) (id int64, name string, isBot bool) {
	if c := m.SenderChat; c != nil {
		return 0, cmp.Or(c.Title, c.Username, "Unknown"), false
	}
	if u := m.From; u != nil {
		return u.ID, cmp.Or(u.Username, fullName(*u), "Unknown"), u.IsBot
	}
	return 0, "Unknown", false
}

func fullName(u telego.User) string {
	return strings.TrimSpace(u.FirstName + " " + u.LastName)
}

// content renders the message as text, describing media with placeholders.
func content(m *telego.Message) string {
	text := cmp.Or(m.Text, m.Caption)
	if p := placeholder(m); p != "" {
		return strings.TrimSpace(p + " " + text)
	}
	return text
}

func placeholder(m *telego.Message) string {
	switch {
	case m.Photo != nil:
		return "[Photo]"
	case m.Video != nil:
		return "[Video]"
	case m.Voice != nil:
		return "[Voice]"
	case m.Audio != nil:
		return "[Audio]"
	case m.Animation != nil:
		// Checked before Document: Telegram sets both for animations.
		return "[GIF]"
	case m.Document != nil:
		return "[Document]"
	case m.Sticker != nil:
		return strings.TrimSpace("[Sticker] " + m.Sticker.Emoji)
	case m.VideoNote != nil:
		return "[Video Note]"
	case m.Poll != nil:
		return "[Poll] " + m.Poll.Question
	case m.Location != nil:
		return "[Location]"
	case m.Contact != nil:
		return "[Contact]"
	case m.RichMessage != nil:
		return "[Rich Message]"
	}
	return ""
}

// command is a parsed bot command like "/summary@kabanbot args".
type command struct {
	Name string
	// Bot is the explicitly addressed bot username, if any.
	Bot string
}

// parseCommand extracts a command from the start of text.
func parseCommand(text string) (command, bool) {
	if !strings.HasPrefix(text, "/") {
		return command{}, false
	}
	word, _, _ := strings.Cut(text[1:], " ")
	word, _, _ = strings.Cut(word, "\n")
	name, bot, _ := strings.Cut(word, "@")
	if name == "" {
		return command{}, false
	}
	return command{Name: strings.ToLower(name), Bot: bot}, true
}

// commandArgs returns the text after a command, e.g. the question in "/ask как дела?".
func commandArgs(text string) string {
	_, args, _ := strings.Cut(text, " ")
	return strings.TrimSpace(args)
}

// isFor reports whether the command is addressed to this bot.
func (c command) isFor(botUsername string, known map[string]bool) bool {
	if c.Bot != "" {
		return strings.EqualFold(c.Bot, botUsername)
	}
	return known[c.Name]
}
