// Package registry resolves which LLM client and model a chat should use.
package registry

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/feytox/kabanbot/internal/domain"
	"github.com/feytox/kabanbot/internal/llm"
	"github.com/feytox/kabanbot/internal/llm/gemini"
	"github.com/feytox/kabanbot/internal/llm/openai"
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

	mu      sync.Mutex
	clients map[int64]llm.Client // by provider ID
}

// New creates a Registry.
func New(store ModelStore, fallback llm.Target) *Registry {
	return &Registry{store: store, fallback: fallback, clients: make(map[int64]llm.Client)}
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
	client, err := r.client(ctx, provider)
	if err != nil {
		return llm.Target{}, err
	}
	return llm.Target{Client: client, Model: model}, nil
}

// Invalidate drops the cached client of a provider after it has changed.
func (r *Registry) Invalidate(providerID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, providerID)
}

func (r *Registry) client(ctx context.Context, p domain.Provider) (llm.Client, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.clients[p.ID]; ok {
		return c, nil
	}
	c, err := NewClient(ctx, p)
	if err != nil {
		return nil, err
	}
	r.clients[p.ID] = c
	return c, nil
}

// NewClient creates an llm.Client for the provider.
func NewClient(ctx context.Context, p domain.Provider) (llm.Client, error) {
	switch p.Kind {
	case domain.ProviderOpenAI:
		return openai.New(openai.Config{APIKey: p.APIKey.Reveal(), BaseURL: p.BaseURL}), nil
	case domain.ProviderOpenRouter:
		return openai.NewOpenRouter(p.APIKey.Reveal(), p.BaseURL), nil
	case domain.ProviderGemini:
		return gemini.New(ctx, p.APIKey.Reveal(), p.BaseURL)
	default:
		return nil, fmt.Errorf("unknown provider kind %q", p.Kind)
	}
}
