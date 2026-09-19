package telegram

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"

	"github.com/feytox/kabanbot/internal/app/settings"
	"github.com/feytox/kabanbot/internal/domain"
	"github.com/feytox/kabanbot/internal/secrets"
	"github.com/feytox/kabanbot/internal/storage/sqlite"
)

const (
	admin    = 1
	stranger = 2
	group    = -100
)

type fakeAdmins map[int64][]int64

func (f fakeAdmins) IsAdmin(_ context.Context, chatID, userID int64) (bool, error) {
	return slices.Contains(f[chatID], userID), nil
}

func newTestMenu(t *testing.T) (*menu, *sqlite.ChatStore) {
	t.Helper()
	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	chats := sqlite.NewChatStore(db)
	if err := chats.TouchChat(t.Context(), group, "Кабаны", true); err != nil {
		t.Fatal(err)
	}
	box, err := secrets.New(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	svc := settings.New(sqlite.NewModelStore(db, box), chats, fakeAdmins{group: {admin}}, nil, sqlite.NewUsageStore(db), 0, slog.New(slog.DiscardHandler))
	for _, id := range []int64{admin, stranger} {
		if err := svc.Seen(t.Context(), settings.User{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	return &menu{svc: svc, botUsername: "kabanbot"}, chats
}

func TestGroupCallbackFromNonAdminIsRejected(t *testing.T) {
	m, chats := newTestMenu(t)
	ctx := t.Context()

	for _, r := range []route{
		{op: opGroup, id: group},
		{op: opGroupToggle, id: group, word: toggleEnabled},
		{op: opGroupToggle, id: group, word: toggleSummary},
		{op: opGroupModels, id: group},
		{op: opGroupModel, id: group},
		{op: opGroupToggle, id: group, word: toggleChat},
		{op: opChatModels, id: group},
		{op: opChatModel, id: group},
		{op: opPersonality, id: group},
		{op: opPersonalityReset, id: group},
		{op: opPersonalityHistory, id: group},
		{op: opStyleReset, id: group},
		{op: opLimits, id: group},
		{op: opLimitUser, id: group},
		{op: opStats, id: group},
	} {
		for _, private := range []bool{false, true} {
			_, err := m.handle(ctx, view{user: settings.User{ID: stranger}, private: private}, r)
			if !errors.Is(err, settings.ErrForbidden) {
				t.Errorf("%s from a non-admin (private %v): err = %v, want ErrForbidden", r, private, err)
			}
		}
	}
	if s, _ := chats.ChatSettings(ctx, group); !s.Enabled || !s.Summary {
		t.Fatalf("settings changed by a non-admin: %+v", s)
	}

	out, err := m.handle(ctx, view{user: settings.User{ID: admin}}, route{op: opGroupToggle, id: group, word: toggleSummary})
	if err != nil || out.screen == nil {
		t.Fatalf("admin toggle: %+v, %v", out, err)
	}
	if s, _ := chats.ChatSettings(ctx, group); s.Summary {
		t.Error("admin toggle did not turn summaries off")
	}
}

func TestProviderRoutesArePrivateOnly(t *testing.T) {
	m, _ := newTestMenu(t)
	for _, r := range []route{
		{op: opProviders},
		{op: opProviderCreate, word: "openai"},
		{op: opProviderEdit, id: 1, word: fieldKey},
		{op: opModelTest, id: 1},
		{op: opPersonalityEdit, id: group},
		{op: opStyleEdit, id: group},
	} {
		_, err := m.handle(t.Context(), view{user: settings.User{ID: admin}}, r)
		if !errors.Is(err, errPrivateOnly) {
			t.Errorf("%s in a group: err = %v, want errPrivateOnly", r, err)
		}
	}
}

func TestOtherUsersProviderIsNotFound(t *testing.T) {
	m, _ := newTestMenu(t)
	ctx := t.Context()
	owner := settings.User{ID: admin}
	p, err := m.svc.CreateProvider(ctx, owner, settings.ProviderInput{Kind: domain.ProviderOpenRouter, Name: "Мой", APIKey: "sk-0123456789"})
	if err != nil {
		t.Fatal(err)
	}
	mdl, err := m.svc.CreateModel(ctx, owner, p.ID, settings.ModelInput{Name: "x/y"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []route{
		{op: opProvider, id: p.ID},
		{op: opProviderEdit, id: p.ID, word: fieldKey},
		{op: opProviderShare, id: p.ID},
		{op: opProviderDrop, id: p.ID},
		{op: opModelNew, id: p.ID},
		{op: opModel, id: mdl.ID},
		{op: opModelEdit, id: mdl.ID, word: fieldName},
		{op: opModelTest, id: mdl.ID},
		{op: opModelUnbind, id: mdl.ID, id2: group},
		{op: opModelDrop, id: mdl.ID},
	} {
		_, err := m.handle(ctx, view{user: settings.User{ID: stranger}, private: true}, r)
		if !errors.Is(err, settings.ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", r, err)
		}
	}
	if _, err := m.handle(ctx, view{user: owner, private: true}, route{op: opModel, id: mdl.ID}); err != nil {
		t.Errorf("the owner cannot open their model: %v", err)
	}
}

func TestToggleKeepsModelBindings(t *testing.T) {
	m, chats := newTestMenu(t)
	ctx := t.Context()
	u := settings.User{ID: admin}
	p, err := m.svc.CreateProvider(ctx, u, settings.ProviderInput{Kind: domain.ProviderOpenRouter, Name: "p", APIKey: "sk-0123456789"})
	if err != nil {
		t.Fatal(err)
	}
	mdl, err := m.svc.CreateModel(ctx, u, p.ID, settings.ModelInput{Name: "x/y"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []route{
		{op: opChatModel, id: group, id2: mdl.ID},
		{op: opGroupModel, id: group, id2: mdl.ID},
		{op: opGroupToggle, id: group, word: toggleChat},
		{op: opLimitUser, id: group},
	} {
		if _, err := m.handle(ctx, view{user: u}, r); err != nil {
			t.Fatalf("%s: %v", r, err)
		}
	}
	c, _ := chats.Chat(ctx, group)
	if c.ChatModelID == nil || c.SummaryModelID == nil || c.Settings.Chat || c.Settings.Limits.UserPerHour != 50 {
		t.Fatalf("chat = %+v", c)
	}
}

func TestStartRoute(t *testing.T) {
	for param, want := range map[string]route{
		"":          {op: opHome},
		"models":    {op: opProviders},
		"g_-100123": {op: opGroup, id: -100123},
		"g_abc":     {op: opHome},
		"-100123":   {op: opHome},
	} {
		if got := startRoute(param); got != want {
			t.Errorf("startRoute(%q) = %+v, want %+v", param, got, want)
		}
	}
}
