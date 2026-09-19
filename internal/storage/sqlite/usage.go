package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/feytox/kabanbot/internal/domain"
	"github.com/feytox/kabanbot/internal/storage/sqlite/sqlcgen"
)

// UsageStore records how many tokens model calls used.
type UsageStore struct {
	db *DB
}

// NewUsageStore creates a UsageStore.
func NewUsageStore(db *DB) *UsageStore { return &UsageStore{db: db} }

// RecordUsage stores one model call.
func (s *UsageStore) RecordUsage(ctx context.Context, u domain.Usage) error {
	err := s.db.q.InsertUsage(ctx, sqlcgen.InsertUsageParams{
		ChatID: u.ChatID, UserID: u.UserID, ModelID: sql.NullInt64{Int64: u.ModelID, Valid: u.ModelID != 0},
		Kind: string(u.Kind), TokensIn: u.TokensIn, TokensOut: u.TokensOut,
	})
	if err != nil {
		return fmt.Errorf("record usage: %w", err)
	}
	return nil
}

// UsageTotals sums up the chat's model calls since the given time.
func (s *UsageStore) UsageTotals(ctx context.Context, chatID int64, since time.Time) (domain.UsageTotals, error) {
	r, err := s.db.q.UsageTotals(ctx, sqlcgen.UsageTotalsParams{ChatID: chatID, CreatedAt: since.Unix()})
	if err != nil {
		return domain.UsageTotals{}, fmt.Errorf("usage totals: %w", err)
	}
	return domain.UsageTotals{Requests: r.Requests, TokensIn: r.TokensIn, TokensOut: r.TokensOut}, nil
}

// UsageByUser lists the users who made the most model calls in the chat since the given time.
func (s *UsageStore) UsageByUser(ctx context.Context, chatID int64, since time.Time, limit int) ([]domain.UserUsage, error) {
	rows, err := s.db.q.UsageByUser(ctx, sqlcgen.UsageByUserParams{ChatID: chatID, CreatedAt: since.Unix(), Limit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("usage by user: %w", err)
	}
	out := make([]domain.UserUsage, len(rows))
	for i, r := range rows {
		out[i] = domain.UserUsage{UserID: r.UserID, Name: displayName(r.Username, r.FirstName), Requests: r.Requests, Tokens: r.Tokens}
	}
	return out, nil
}
