package sqlite

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/feytox/kabanbot/internal/domain"
	"github.com/feytox/kabanbot/internal/secrets"
)

func TestMessageStoreRecent(t *testing.T) {
	s := NewMessageStore(openTest(t))
	for i := 1; i <= 5; i++ {
		if err := s.Add(t.Context(), msg(-1, i, 1, "m"), 100); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Recent(t.Context(), -1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if ids := messageIDs(got); len(ids) != 3 || ids[0] != 3 || ids[2] != 5 {
		t.Errorf("recent = %v, want the newest three oldest first", ids)
	}
}

func TestChatModelFallsBackToSummaryModel(t *testing.T) {
	ctx := t.Context()
	db := openTest(t)
	box, _ := secrets.New(bytes.Repeat([]byte{9}, 32))
	store := NewModelStore(db, box)
	exec(t, db,
		`INSERT INTO users (id) VALUES (42)`,
		`INSERT INTO providers (id, owner_user_id, kind, name, api_key_enc, api_key_hint) VALUES (1, 42, 'openai', 'p', x'00', '')`,
		`INSERT INTO models (id, provider_id, model_name, display_name) VALUES (7, 1, 'summarizer', 's'), (8, 1, 'talker', 't')`,
		`INSERT INTO chats (id, summary_model_id) VALUES (-100, 7)`,
		`INSERT INTO chats (id) VALUES (-200)`,
	)
	// The fake key cannot be decrypted, but the model is picked before that.
	check := func(chatID int64, want string) {
		t.Helper()
		row, err := db.q.ChatChatModel(ctx, chatID)
		if err != nil || row.Model.ModelName != want {
			t.Errorf("chat %d: model = %q, %v; want %q", chatID, row.Model.ModelName, err, want)
		}
	}
	check(-100, "summarizer")
	exec(t, db, `UPDATE chats SET chat_model_id = 8 WHERE id = -100`)
	check(-100, "talker")
	if _, _, err := store.ChatModel(ctx, -200); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("chat without models: err = %v", err)
	}

	chats := NewChatStore(db)
	if err := chats.UnbindModel(ctx, -100, 8); err != nil {
		t.Fatal(err)
	}
	if c, _ := chats.Chat(ctx, -100); c.ChatModelID != nil || c.SummaryModelID == nil {
		t.Errorf("unbinding the chat model touched the summary model: %+v", c)
	}
}

func TestChatStoreSettingsAndPersonality(t *testing.T) {
	ctx := t.Context()
	db := openTest(t)
	s := NewChatStore(db)
	if err := s.TouchChat(ctx, -1, "g", true); err != nil {
		t.Fatal(err)
	}
	c, _ := s.Chat(ctx, -1)
	if c.Settings != domain.DefaultChatSettings() {
		t.Errorf("new chat settings = %+v", c.Settings)
	}
	c.Settings.Chat = false
	c.Settings.Limits = domain.RateLimits{UserPerHour: 3}
	if err := s.UpdateChat(ctx, c, 1); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Chat(ctx, -1); got.Settings.Chat || got.Settings.Limits != (domain.RateLimits{UserPerHour: 3}) {
		t.Errorf("saved settings = %+v", got.Settings)
	}

	exec(t, db, `INSERT INTO users (id, username) VALUES (1, 'alice')`)
	for _, text := range []string{"кабан", "ёж", ""} {
		if err := s.SetPersonality(ctx, -1, text, 1, domain.ViaTool); err != nil {
			t.Fatal(err)
		}
	}
	history, err := s.PersonalityHistory(ctx, -1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Text != "" || history[1].Text != "ёж" || history[0].ChangedByName != "@alice" ||
		history[0].Via != domain.ViaTool {
		t.Fatalf("history = %+v", history)
	}
	change, err := s.PersonalityChange(ctx, history[1].ID)
	if err != nil || change.Text != "ёж" || change.ChatID != -1 {
		t.Errorf("change = %+v, %v", change, err)
	}
	if err := s.SetSummaryStyle(ctx, -1, "стихами", 1); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Chat(ctx, -1); got.Personality != "" || got.SummaryStyle != "стихами" {
		t.Errorf("chat = %+v", got)
	}
}

func TestUsageStore(t *testing.T) {
	ctx := t.Context()
	db := openTest(t)
	s := NewUsageStore(db)
	exec(t, db, `INSERT INTO users (id, first_name) VALUES (1, 'Алиса')`)
	for _, u := range []domain.Usage{
		{ChatID: -1, UserID: 1, Kind: domain.UsageChat, TokensIn: 10, TokensOut: 5},
		{ChatID: -1, UserID: 1, Kind: domain.UsageSummary, TokensIn: 100, TokensOut: 20},
		{ChatID: -1, UserID: 2, Kind: domain.UsageChat, TokensIn: 1, TokensOut: 1},
		{ChatID: -2, UserID: 1, Kind: domain.UsageChat, TokensIn: 1000, TokensOut: 1000},
	} {
		if err := s.RecordUsage(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	since := time.Now().Add(-time.Hour)
	totals, err := s.UsageTotals(ctx, -1, since)
	if err != nil || totals != (domain.UsageTotals{Requests: 3, TokensIn: 111, TokensOut: 26}) {
		t.Errorf("totals = %+v, %v", totals, err)
	}
	if empty, _ := s.UsageTotals(ctx, -1, time.Now().Add(time.Hour)); empty != (domain.UsageTotals{}) {
		t.Errorf("future totals = %+v", empty)
	}
	top, err := s.UsageByUser(ctx, -1, since, 5)
	if err != nil || len(top) != 2 || top[0].Name != "Алиса" || top[0].Requests != 2 || top[0].Tokens != 135 {
		t.Errorf("top = %+v, %v", top, err)
	}
}

func exec(t *testing.T, db *DB, stmts ...string) {
	t.Helper()
	for _, q := range stmts {
		if _, err := db.db.ExecContext(t.Context(), q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}
