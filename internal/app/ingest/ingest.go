// Package ingest stores incoming chat messages for later use.
package ingest

import (
	"context"

	"github.com/feytox/kabanbot/internal/domain"
)

// Store persists messages, keeping only the newest keep per chat.
type Store interface {
	Add(ctx context.Context, m domain.Message, keep int) error
}

// Service stores incoming messages.
type Service struct {
	store Store
	keep  int
}

// New creates a Service that keeps up to keep messages per chat.
func New(store Store, keep int) *Service { return &Service{store: store, keep: keep} }

// Ingest stores the message. Messages without text are skipped.
func (s *Service) Ingest(ctx context.Context, m domain.Message) error {
	if m.Text == "" {
		return nil
	}
	return s.store.Add(ctx, m, s.keep)
}
