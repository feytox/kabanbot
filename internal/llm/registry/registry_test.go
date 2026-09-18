package registry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/feytox/kabanbot/internal/domain"
	"github.com/feytox/kabanbot/internal/llm"
	"github.com/feytox/kabanbot/internal/netguard"
)

type fakeStore struct {
	model    domain.Model
	provider domain.Provider
	err      error
	calls    int
}

func (f *fakeStore) SummaryModel(context.Context, int64) (domain.Model, domain.Provider, error) {
	f.calls++
	return f.model, f.provider, f.err
}

type nopClient struct{}

func (nopClient) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, nil
}

func TestSummaryTargetFallsBackWhenUnbound(t *testing.T) {
	fallback := llm.Target{Client: nopClient{}, Model: domain.Model{Name: "default"}}
	r := New(&fakeStore{err: domain.ErrNotFound}, fallback, 0)

	got, err := r.SummaryTarget(t.Context(), 1)
	if err != nil || got.Model.Name != "default" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestSummaryTargetPropagatesStoreErrors(t *testing.T) {
	boom := errors.New("boom")
	r := New(&fakeStore{err: boom}, llm.Target{}, 0)
	if _, err := r.SummaryTarget(t.Context(), 1); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestSummaryTargetCachesClientsPerProvider(t *testing.T) {
	store := &fakeStore{
		model:    domain.Model{Name: "gpt"},
		provider: domain.Provider{ID: 3, Kind: domain.ProviderOpenAI, APIKey: "k"},
	}
	r := New(store, llm.Target{}, 0)

	a, err := r.SummaryTarget(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := r.SummaryTarget(t.Context(), 2)
	if a.Client != b.Client {
		t.Error("client was not reused for the same provider")
	}
	r.Invalidate(3)
	c, _ := r.SummaryTarget(t.Context(), 1)
	if c.Client == a.Client {
		t.Error("client was reused after Invalidate")
	}
}

func TestNewClientRejectsUnknownKind(t *testing.T) {
	if _, err := NewClient(t.Context(), domain.Provider{Kind: "nope"}, nil); err == nil {
		t.Fatal("want error")
	}
}

func TestUntrustedProvidersCannotReachLocalAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()
	r := New(&fakeStore{}, llm.Target{}, 42)
	req := llm.Request{Model: "m"}

	untrusted, _ := r.Client(t.Context(), domain.Provider{ID: 1, OwnerID: 7, Kind: domain.ProviderOpenAI, BaseURL: srv.URL})
	if _, err := untrusted.Complete(t.Context(), req); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("untrusted provider: err = %v, want ErrBlocked", err)
	}

	trusted, _ := r.Client(t.Context(), domain.Provider{ID: 2, OwnerID: 42, Kind: domain.ProviderOpenAI, BaseURL: srv.URL})
	if _, err := trusted.Complete(t.Context(), req); err != nil {
		t.Errorf("trusted owner's provider: %v", err)
	}
}
