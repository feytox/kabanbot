// Package mention collects the users to ping for @all.
package mention

import (
	"context"
	"log/slog"

	"github.com/feytox/kabanbot/internal/domain"
)

// Users lists humans known from the chat's cached messages.
type Users interface {
	Users(ctx context.Context, chatID int64) ([]domain.User, error)
}

// Admins lists the chat's human administrators.
type Admins interface {
	Admins(ctx context.Context, chatID int64) ([]domain.User, error)
}

// Service resolves @all targets.
type Service struct {
	users  Users
	admins Admins
	log    *slog.Logger
}

// New creates a Service.
func New(users Users, admins Admins, log *slog.Logger) *Service {
	return &Service{users: users, admins: admins, log: log}
}

// Targets returns everyone to ping in the chat except the author.
func (s *Service) Targets(ctx context.Context, chatID, authorID int64) ([]domain.User, error) {
	cached, err := s.users.Users(ctx, chatID)
	if err != nil {
		return nil, err
	}
	// Admins are a best-effort addition: they may never have written anything.
	admins, err := s.admins.Admins(ctx, chatID)
	if err != nil {
		s.log.WarnContext(ctx, "list admins", "chat_id", chatID, "err", err)
	}

	seen := map[int64]bool{authorID: true}
	var out []domain.User
	for _, u := range append(cached, admins...) {
		if seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		out = append(out, u)
	}
	return out, nil
}
