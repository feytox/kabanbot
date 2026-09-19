package sqlite

import (
	"bytes"
	"database/sql"
	"errors"
	"net/url"
	"slices"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/vfs/memdb"

	"github.com/feytox/kabanbot/internal/domain"
	"github.com/feytox/kabanbot/internal/secrets"
)

// memoryDSN names a private in-memory database shared by connections of this process.
func memoryDSN(name string) string { return "file:/" + url.PathEscape(name) + ".db?vfs=memdb" }

func openTest(t *testing.T) *DB {
	t.Helper()
	db, err := open(t.Context(), memoryDSN(t.Name()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func msg(chatID int64, id int, user int64, text string) domain.Message {
	return domain.Message{
		ChatID: chatID, MessageID: id, UserID: user, Username: "u" + string(rune('0'+user)),
		Text: text, SentAt: time.Unix(int64(1000+id), 0),
	}
}

func TestMessageStoreAddSincePrune(t *testing.T) {
	ctx := t.Context()
	s := NewMessageStore(openTest(t))

	for i := 1; i <= 5; i++ {
		if err := s.Add(ctx, msg(1, i, 1, "m"), 3); err != nil {
			t.Fatal(err)
		}
	}
	// Other chats are pruned independently.
	if err := s.Add(ctx, msg(2, 1, 1, "other"), 3); err != nil {
		t.Fatal(err)
	}

	got, err := s.Since(ctx, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ids := messageIDs(got); !slices.Equal(ids, []int{3, 4, 5}) {
		t.Fatalf("after prune got ids %v, want [3 4 5]", ids)
	}

	got, _ = s.Since(ctx, 1, 4)
	if ids := messageIDs(got); !slices.Equal(ids, []int{4, 5}) {
		t.Fatalf("since 4 got %v", ids)
	}
	if got[0].SentAt.Unix() != 1004 {
		t.Errorf("SentAt = %v", got[0].SentAt)
	}

	other, _ := s.Since(ctx, 2, 0)
	if len(other) != 1 {
		t.Fatalf("chat 2 has %d messages", len(other))
	}
}

func TestMessageStoreDuplicateAndReply(t *testing.T) {
	ctx := t.Context()
	s := NewMessageStore(openTest(t))

	m := msg(1, 1, 1, "first")
	m.ReplyTo = &domain.Quote{Username: "bob", Text: "hi"}
	if err := s.Add(ctx, m, 10); err != nil {
		t.Fatal(err)
	}
	dup := msg(1, 1, 1, "duplicate")
	if err := s.Add(ctx, dup, 10); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Since(ctx, 1, 0)
	if len(got) != 1 || got[0].Text != "first" {
		t.Fatalf("got %+v", got)
	}
	if got[0].ReplyTo == nil || *got[0].ReplyTo != (domain.Quote{Username: "bob", Text: "hi"}) {
		t.Fatalf("reply = %+v", got[0].ReplyTo)
	}
}

func TestMessageStoreUsers(t *testing.T) {
	ctx := t.Context()
	s := NewMessageStore(openTest(t))

	add := func(id int, user int64, name string, isBot bool) {
		m := msg(1, id, user, "x")
		m.Username, m.IsBot = name, isBot
		if err := s.Add(ctx, m, 100); err != nil {
			t.Fatal(err)
		}
	}
	add(1, 1, "old_name", false)
	add(2, 1, "new_name", false)
	add(3, 2, "bob", false)
	add(4, 3, "somebot", true)
	add(5, 0, "Channel", false)

	users, err := s.Users(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	names := map[int64]string{}
	for _, u := range users {
		names[u.ID] = u.Name
	}
	if len(names) != 2 || names[1] != "new_name" || names[2] != "bob" {
		t.Fatalf("users = %v", names)
	}
}

func TestMigratesLegacyPythonSchema(t *testing.T) {
	ctx := t.Context()

	// Keep a connection open so the in-memory database outlives the setup.
	legacy, err := sql.Open("sqlite3", memoryDSN(t.Name()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = legacy.Close() })
	_, err = legacy.ExecContext(ctx, `
		CREATE TABLE messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT, chat_id INTEGER, message_id INTEGER, user_id INTEGER,
			username TEXT, text TEXT, timestamp REAL, reply_to_text TEXT, reply_to_username TEXT);
		CREATE INDEX idx_chat_timestamp ON messages (chat_id, timestamp);
		INSERT INTO messages (chat_id, message_id, user_id, username, text, timestamp, reply_to_text, reply_to_username)
		VALUES (-100, 10, 5, 'alice', 'hello', 1700000000.5, NULL, NULL),
		       (-100, 11, 6, 'bob', 'hi', 1700000001.0, 'hello', 'alice'),
		       (-100, 11, 6, 'bob', 'duplicate row', 1700000001.0, NULL, NULL);`)
	if err != nil {
		t.Fatal(err)
	}

	db, err := open(ctx, memoryDSN(t.Name()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	got, err := NewMessageStore(db).Since(ctx, -100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d messages, want 2: %+v", len(got), got)
	}
	if got[0].Username != "alice" || got[0].SentAt.Unix() != 1700000000 {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].ReplyTo == nil || got[1].ReplyTo.Username != "alice" {
		t.Errorf("second reply = %+v", got[1].ReplyTo)
	}
}

func TestModelStoreSummaryModel(t *testing.T) {
	ctx := t.Context()
	db := openTest(t)
	box, err := secrets.New(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	store := NewModelStore(db, box)

	if _, _, err := store.SummaryModel(ctx, -100); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unbound chat: err = %v, want ErrNotFound", err)
	}

	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users (id) VALUES (42)`, nil},
		{`INSERT INTO providers (id, owner_user_id, kind, name, api_key_enc, api_key_hint)
		  VALUES (1, 42, 'gemini', 'my gemini', ?, 'abcd')`, []any{box.Seal([]byte("secret-key"))}},
		{`INSERT INTO models (id, provider_id, model_name, display_name, params_json)
		  VALUES (7, 1, 'gemini-3-flash', 'Flash', '{"temperature":0.3,"max_tokens":2000}')`, nil},
		{`INSERT INTO chats (id, summary_model_id) VALUES (-100, 7)`, nil},
	} {
		if _, err := db.db.ExecContext(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}

	model, provider, err := store.SummaryModel(ctx, -100)
	if err != nil {
		t.Fatal(err)
	}
	if model.Name != "gemini-3-flash" || model.Params.MaxTokens != 2000 || *model.Params.Temperature != 0.3 {
		t.Errorf("model = %+v", model)
	}
	if provider.Kind != domain.ProviderGemini || provider.APIKey.Reveal() != "secret-key" || provider.OwnerID != 42 {
		t.Errorf("provider = %+v", provider)
	}

	if _, _, err := NewModelStore(db, nil).SummaryModel(ctx, -100); err == nil {
		t.Error("without master key: want error")
	}
}

func messageIDs(ms []domain.Message) []int {
	ids := make([]int, len(ms))
	for i, m := range ms {
		ids[i] = m.MessageID
	}
	return ids
}
