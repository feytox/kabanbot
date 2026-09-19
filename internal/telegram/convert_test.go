package telegram

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mymmrac/telego"
)

func TestParseCommand(t *testing.T) {
	known := map[string]bool{"summary": true}
	tests := []struct {
		text  string
		isCmd bool
		name  string
		forMe bool
	}{
		{"/summary", true, "summary", true},
		{"/Summary@KabanBot please", true, "summary", true},
		{"/summary@otherbot", true, "summary", false},
		{"/start", true, "start", false},
		{"/start@kabanbot", true, "start", true},
		{"/summary\nmore", true, "summary", true},
		{"hello /summary", false, "", false},
		{"/", false, "", false},
		{"", false, "", false},
	}
	for _, tt := range tests {
		cmd, ok := parseCommand(tt.text)
		if ok != tt.isCmd || cmd.Name != tt.name {
			t.Errorf("parseCommand(%q) = %+v, %v", tt.text, cmd, ok)
			continue
		}
		if ok && cmd.isFor("kabanbot", known) != tt.forMe {
			t.Errorf("parseCommand(%q).isFor = %v, want %v", tt.text, !tt.forMe, tt.forMe)
		}
	}
}

func TestContent(t *testing.T) {
	tests := []struct {
		name string
		msg  telego.Message
		want string
	}{
		{"text", telego.Message{Text: "hi"}, "hi"},
		{"photo with caption", telego.Message{Photo: []telego.PhotoSize{{}}, Caption: "моя кошка"}, "[Photo] моя кошка"},
		{"photo", telego.Message{Photo: []telego.PhotoSize{{}}}, "[Photo]"},
		{"gif is not a document", telego.Message{Animation: &telego.Animation{}, Document: &telego.Document{}}, "[GIF]"},
		{"sticker", telego.Message{Sticker: &telego.Sticker{Emoji: "🐈"}}, "[Sticker] 🐈"},
		{"poll", telego.Message{Poll: &telego.Poll{Question: "Пицца?"}}, "[Poll] Пицца?"},
		{"empty", telego.Message{}, ""},
	}
	for _, tt := range tests {
		if got := content(&tt.msg); got != tt.want {
			t.Errorf("%s: content = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestToDomain(t *testing.T) {
	m := &telego.Message{
		MessageID: 5,
		Date:      1700000000,
		Chat:      telego.Chat{ID: -100},
		From:      &telego.User{ID: 7, FirstName: "Ann", LastName: "Lee"},
		Text:      "yes",
		ReplyToMessage: &telego.Message{
			From:  &telego.User{ID: 8, Username: "bob"},
			Photo: []telego.PhotoSize{{}},
		},
	}
	got := toDomain(m)
	if got.ChatID != -100 || got.MessageID != 5 || got.UserID != 7 || got.Username != "Ann Lee" || got.SentAt.Unix() != 1700000000 {
		t.Errorf("message = %+v", got)
	}
	if got.ReplyTo == nil || got.ReplyTo.Username != "bob" || got.ReplyTo.Text != "[Photo]" {
		t.Errorf("reply = %+v", got.ReplyTo)
	}

	channel := toDomain(&telego.Message{SenderChat: &telego.Chat{Title: "News"}, From: &telego.User{ID: 136817688}})
	if channel.UserID != 0 || channel.Username != "News" {
		t.Errorf("channel post = %+v", channel)
	}
}

func TestSplit(t *testing.T) {
	if got := split("short", 10); len(got) != 1 || got[0] != "short" {
		t.Errorf("short: %q", got)
	}
	if got := split("", 10); len(got) != 0 {
		t.Errorf("empty: %q", got)
	}

	text := strings.Repeat("абв ", 10) + "\n\n" + strings.Repeat("где ", 10)
	parts := split(text, 45)
	if len(parts) != 2 || !strings.HasPrefix(parts[1], "где") {
		t.Errorf("paragraph split: %q", parts)
	}

	for _, p := range split(strings.Repeat("я", 25), 10) {
		if n := utf8.RuneCountInString(p); n > 10 || !utf8.ValidString(p) {
			t.Errorf("hard split produced %q (%d runes)", p, n)
		}
	}
}

func TestChunk(t *testing.T) {
	got := chunk([]string{"aaa", "bbb", "ccc", "d"}, " ", 7)
	want := []string{"aaa bbb", "ccc d"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("chunk = %q, want %q", got, want)
	}
}
