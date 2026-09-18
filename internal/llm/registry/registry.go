// Package registry resolves which LLM client and model a chat should use.
package registry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/feytox/kabanbot/internal/domain"
	"github.com/feytox/kabanbot/internal/llm"
	"github.com/feytox/kabanbot/internal/llm/gemini"
	"github.com/feytox/kabanbot/internal/llm/openai"
	"github.com/feytox/kabanbot/internal/netguard"
)

// ModelStore loads the model bound to a chat.
type ModelStore interface {
	// SummaryModel returns the model bound to the chat for summaries, or domain.ErrNotFound.
	SummaryModel(ctx context.Context, chatID int64) (domain.Model, domain.Provider, error)
}

// Registry resolves chats to LLM targets, falling back to a default target.
type Registry struct {
	store    ModelStore
	fallback llm.Target
	// trustedOwner may point providers at any address, e.g. a local model server.
	// Everyone else's providers may only reach public addresses.
	trustedOwner int64
	guarded      *http.Client

	mu      sync.Mutex
	clients map[int64]llm.Client // by provider ID
}

// New creates a Registry. Providers owned by trustedOwner are not restricted to public addresses.
func New(store ModelStore, fallback llm.Target, trustedOwner int64) *Registry {
	return &Registry{
		store:        store,
		fallback:     fallback,
		trustedOwner: trustedOwner,
		guarded:      netguard.NewClient(),
		clients:      make(map[int64]llm.Client),
	}
}

// SummaryTarget returns the target to use for summaries in the chat.
func (r *Registry) SummaryTarget(ctx context.Context, chatID int64) (llm.Target, error) {
	model, provider, err := r.store.SummaryModel(ctx, chatID)
	if errors.Is(err, domain.ErrNotFound) {
		return r.fallback, nil
	}
	if err != nil {
		return llm.Target{}, fmt.Errorf("load chat model: %w", err)
	}
	client, err := r.Client(ctx, provider)
	if err != nil {
		return llm.Target{}, err
	}
	return llm.Target{Client: client, Model: model}, nil
}

// Client returns a cached client for the provider, creating it if needed.
func (r *Registry) Client(ctx context.Context, p domain.Provider) (llm.Client, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.clients[p.ID]; ok {
		return c, nil
	}
	var httpClient *http.Client
	if r.trustedOwner == 0 || p.OwnerID != r.trustedOwner {
		httpClient = r.guarded
	}
	c, err := NewClient(ctx, p, httpClient)
	if err != nil {
		return nil, err
	}
	r.clients[p.ID] = c
	return c, nil
}

// Invalidate drops the cached client of a provider after it has changed.
func (r *Registry) Invalidate(providerID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, providerID)
}

// NewClient creates an llm.Client for the provider. httpClient may be nil to use the default.
func NewClient(ctx context.Context, p domain.Provider, httpClient *http.Client) (llm.Client, error) {
	cfg := openai.Config{APIKey: p.APIKey.Reveal(), BaseURL: p.BaseURL, HTTPClient: httpClient}
	switch p.Kind {
	case domain.ProviderOpenAI:
		return openai.New(cfg), nil
	case domain.ProviderOpenRouter:
		return openai.NewOpenRouter(cfg), nil
	case domain.ProviderGemini:
		return gemini.New(ctx, p.APIKey.Reveal(), p.BaseURL, httpClient)
	default:
		return nil, fmt.Errorf("unknown provider kind %q", p.Kind)
	}
}
