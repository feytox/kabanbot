package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/feytox/kabanbot/internal/domain"
	"github.com/feytox/kabanbot/internal/llm"
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
	r := New(&fakeStore{err: domain.ErrNotFound}, fallback)

	got, err := r.SummaryTarget(t.Context(), 1)
	if err != nil || got.Model.Name != "default" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestSummaryTargetPropagatesStoreErrors(t *testing.T) {
	boom := errors.New("boom")
	r := New(&fakeStore{err: boom}, llm.Target{})
	if _, err := r.SummaryTarget(t.Context(), 1); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestSummaryTargetCachesClientsPerProvider(t *testing.T) {
	store := &fakeStore{
		model:    domain.Model{Name: "gpt"},
		provider: domain.Provider{ID: 3, Kind: domain.ProviderOpenAI, APIKey: "k"},
	}
	r := New(store, llm.Target{})

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
	if _, err := NewClient(t.Context(), domain.Provider{Kind: "nope"}); err == nil {
		t.Fatal("want error")
	}
}
