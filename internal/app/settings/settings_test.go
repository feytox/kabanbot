package settings_test

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
	"github.com/feytox/kabanbot/internal/llm"
	"github.com/feytox/kabanbot/internal/secrets"
	"github.com/feytox/kabanbot/internal/storage/sqlite"
)

const (
	owner   = 1 // bot owner
	alice   = 2 // admin of the group
	bob     = 3 // another admin of the group
	mallory = 4 // not an admin
	group   = -100
)

type fakeAdmins map[int64][]int64

func (f fakeAdmins) IsAdmin(_ context.Context, chatID, userID int64) (bool, error) {
	if slices.Contains(f[chatID], userID) {
		return true, nil
	}
	return false, nil
}

type fakeClients struct {
	invalidated []int64
	got         domain.Provider
}

func (f *fakeClients) Client(_ context.Context, p domain.Provider) (llm.Client, error) {
	f.got = p
	return echoClient{}, nil
}

func (f *fakeClients) Invalidate(id int64) { f.invalidated = append(f.invalidated, id) }

type echoClient struct{}

func (echoClient) Complete(_ context.Context, req llm.Request) (llm.Response, error) {
	return llm.Response{Text: "привет от " + req.Model}, nil
}

type env struct {
	svc     *settings.Service
	chats   *sqlite.ChatStore
	clients *fakeClients
}

func setup(t *testing.T) env {
	t.Helper()
	ctx := t.Context()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	box, _ := secrets.New(bytes.Repeat([]byte{1}, 32))

	chats := sqlite.NewChatStore(db)
	if err := chats.TouchChat(ctx, group, "Кабаны", true); err != nil {
		t.Fatal(err)
	}
	clients := &fakeClients{}
	svc := settings.New(sqlite.NewModelStore(db, box), chats, fakeAdmins{group: {alice, bob}}, clients,
		owner, slog.New(slog.DiscardHandler))
	for _, u := range []settings.User{{ID: owner}, {ID: alice, Username: "alice"}, {ID: bob}, {ID: mallory}} {
		if err := svc.Seen(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	return env{svc: svc, chats: chats, clients: clients}
}

func (e env) addModel(t *testing.T, u settings.User, shared bool) (domain.Provider, domain.Model) {
	t.Helper()
	p, err := e.svc.CreateProvider(t.Context(), u, settings.ProviderInput{
		Kind: domain.ProviderOpenRouter, Name: "router", APIKey: "sk-secret-key-1234", Shared: shared,
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := e.svc.CreateModel(t.Context(), u, p.ID, settings.ModelInput{Name: "vendor/model"})
	if err != nil {
		t.Fatal(err)
	}
	return p, m
}

func user(id int64) settings.User { return settings.User{ID: id} }

func TestProvidersNeverExposeKeys(t *testing.T) {
	e := setup(t)
	p, _ := e.addModel(t, user(alice), false)
	if p.APIKey != "" || p.KeyHint != "1234" {
		t.Fatalf("created provider = %+v", p)
	}
	views, err := e.svc.Providers(t.Context(), user(alice))
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].APIKey != "" || len(views[0].Models) != 1 {
		t.Fatalf("views = %+v", views)
	}
	if others, _ := e.svc.Providers(t.Context(), user(bob)); len(others) != 0 {
		t.Fatalf("bob sees alice's providers: %+v", others)
	}
}

func TestOthersCannotTouchProvidersOrModels(t *testing.T) {
	e := setup(t)
	ctx := t.Context()
	p, m := e.addModel(t, user(alice), false)

	checks := map[string]error{}
	_, checks["update provider"] = e.svc.UpdateProvider(ctx, user(bob), p.ID, settings.ProviderInput{Name: "x"})
	checks["delete provider"] = e.svc.DeleteProvider(ctx, user(bob), p.ID)
	_, checks["create model"] = e.svc.CreateModel(ctx, user(bob), p.ID, settings.ModelInput{Name: "x"})
	_, checks["update model"] = e.svc.UpdateModel(ctx, user(bob), m.ID, settings.ModelInput{Name: "x"})
	checks["delete model"] = e.svc.DeleteModel(ctx, user(bob), m.ID)
	_, _, checks["test model"] = e.svc.TestModel(ctx, user(bob), m.ID)
	checks["unbind model"] = e.svc.UnbindModel(ctx, user(bob), m.ID, group)
	for op, err := range checks {
		if !errors.Is(err, settings.ErrNotFound) {
			t.Errorf("%s by another user: err = %v, want ErrNotFound", op, err)
		}
	}
}

func TestChangingBaseURLRequiresKey(t *testing.T) {
	e := setup(t)
	p, _ := e.addModel(t, user(alice), false)

	_, err := e.svc.UpdateProvider(t.Context(), user(alice), p.ID, settings.ProviderInput{
		Name: "router", BaseURL: "https://evil.example/v1",
	})
	if _, ok := errors.AsType[*settings.ValidationError](err); !ok {
		t.Fatalf("err = %v, want ValidationError", err)
	}

	got, err := e.svc.UpdateProvider(t.Context(), user(alice), p.ID, settings.ProviderInput{
		Name: "renamed", BaseURL: "https://proxy.example/v1", APIKey: "sk-new-key-99999",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "renamed" || got.KeyHint != "9999" || got.Kind != domain.ProviderOpenRouter {
		t.Errorf("updated = %+v", got)
	}
	if len(e.clients.invalidated) != 1 || e.clients.invalidated[0] != p.ID {
		t.Errorf("invalidated = %v", e.clients.invalidated)
	}
}

func TestOnlyOwnerShares(t *testing.T) {
	e := setup(t)
	_, err := e.svc.CreateProvider(t.Context(), user(alice), settings.ProviderInput{
		Kind: domain.ProviderGemini, Name: "g", APIKey: "key-key-key", Shared: true,
	})
	if _, ok := errors.AsType[*settings.ValidationError](err); !ok {
		t.Fatalf("alice sharing: err = %v", err)
	}
	e.addModel(t, user(owner), true)
}

func TestChatAccessAndBinding(t *testing.T) {
	e := setup(t)
	ctx := t.Context()
	_, aliceModel := e.addModel(t, user(alice), false)
	_, bobModel := e.addModel(t, user(bob), false)
	_, sharedModel := e.addModel(t, user(owner), true)

	if chats, _ := e.svc.Chats(ctx, user(mallory)); len(chats) != 0 {
		t.Fatalf("non-admin sees chats: %+v", chats)
	}
	if _, err := e.svc.Chat(ctx, user(mallory), group); !errors.Is(err, settings.ErrForbidden) {
		t.Fatalf("non-admin reads chat: err = %v", err)
	}

	bind := func(u settings.User, id *int64) (settings.ChatView, error) {
		return e.svc.UpdateChat(ctx, u, group, settings.ChatInput{Settings: domain.DefaultChatSettings(), SummaryModelID: id})
	}

	// Alice binds her own model; bob sees who owns it but not the key.
	if _, err := bind(user(alice), &aliceModel.ID); err != nil {
		t.Fatal(err)
	}
	view, err := e.svc.Chat(ctx, user(bob), group)
	if err != nil {
		t.Fatal(err)
	}
	if view.SummaryModel == nil || view.SummaryModel.ID != aliceModel.ID || view.SummaryModel.OwnerName != "@alice" {
		t.Fatalf("bob's view = %+v", view.SummaryModel)
	}

	// Bob may keep alice's model while changing other settings, but not bind it anew elsewhere.
	if _, err := bind(user(bob), &aliceModel.ID); err != nil {
		t.Fatalf("bob keeping alice's model: %v", err)
	}
	if _, err := bind(user(bob), &bobModel.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := bind(user(bob), &aliceModel.ID); !isValidation(err) {
		t.Fatalf("bob binding alice's model: err = %v, want ValidationError", err)
	}
	if _, err := bind(user(bob), &sharedModel.ID); err != nil {
		t.Fatalf("binding shared model: %v", err)
	}
	if _, err := bind(user(mallory), nil); !errors.Is(err, settings.ErrForbidden) {
		t.Fatalf("non-admin update: err = %v", err)
	}
}

func TestOwnerCanUnbindModelAnywhere(t *testing.T) {
	e := setup(t)
	ctx := t.Context()
	_, m := e.addModel(t, user(owner), true)
	if _, err := e.svc.UpdateChat(ctx, user(alice), group, settings.ChatInput{
		Settings: domain.DefaultChatSettings(), SummaryModelID: &m.ID,
	}); err != nil {
		t.Fatal(err)
	}

	views, _ := e.svc.Providers(ctx, user(owner))
	if chats := views[0].Models[0].Chats; len(chats) != 1 || chats[0].Title != "Кабаны" {
		t.Fatalf("bound chats = %+v", chats)
	}
	if err := e.svc.UnbindModel(ctx, user(owner), m.ID, group); err != nil {
		t.Fatal(err)
	}
	if c, _ := e.svc.Chat(ctx, user(alice), group); c.SummaryModelID != nil {
		t.Fatalf("still bound: %+v", c)
	}
}

func TestDeletingModelFallsBackToDefault(t *testing.T) {
	e := setup(t)
	ctx := t.Context()
	_, m := e.addModel(t, user(alice), false)
	if _, err := e.svc.UpdateChat(ctx, user(alice), group, settings.ChatInput{
		Settings: domain.DefaultChatSettings(), SummaryModelID: &m.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DeleteModel(ctx, user(alice), m.ID); err != nil {
		t.Fatal(err)
	}
	if c, _ := e.chats.Chat(ctx, group); c.SummaryModelID != nil {
		t.Fatalf("chat still points at deleted model: %+v", c)
	}
}

func TestTestModelUsesDecryptedKey(t *testing.T) {
	e := setup(t)
	_, m := e.addModel(t, user(alice), false)
	reply, _, err := e.svc.TestModel(t.Context(), user(alice), m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reply != "привет от vendor/model" || e.clients.got.APIKey.Reveal() != "sk-secret-key-1234" {
		t.Fatalf("reply = %q, provider = %+v", reply, e.clients.got)
	}
}

func TestChatSettingsRoundTrip(t *testing.T) {
	e := setup(t)
	ctx := t.Context()
	s := domain.ChatSettings{Enabled: true, Summary: false, MentionAll: true}
	if _, err := e.svc.UpdateChat(ctx, user(alice), group, settings.ChatInput{Settings: s}); err != nil {
		t.Fatal(err)
	}
	got, err := e.chats.ChatSettings(ctx, group)
	if err != nil || got != s {
		t.Fatalf("settings = %+v, %v; want %+v", got, err, s)
	}
	if def, _ := e.chats.ChatSettings(ctx, -999); def != domain.DefaultChatSettings() {
		t.Fatalf("unknown chat settings = %+v", def)
	}
}

func isValidation(err error) bool {
	_, ok := errors.AsType[*settings.ValidationError](err)
	return ok
}
