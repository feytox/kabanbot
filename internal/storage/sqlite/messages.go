package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/feytox/kabanbot/internal/domain"
	"github.com/feytox/kabanbot/internal/storage/sqlite/sqlcgen"
)

// MessageStore caches chat messages.
type MessageStore struct {
	db *DB
}

// NewMessageStore creates a MessageStore.
func NewMessageStore(db *DB) *MessageStore { return &MessageStore{db: db} }

// Add stores the message and prunes the chat down to the newest keep messages.
func (s *MessageStore) Add(ctx context.Context, m domain.Message, keep int) error {
	tx, err := s.db.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := s.db.q.WithTx(tx)

	params := sqlcgen.InsertMessageParams{
		ChatID:    m.ChatID,
		MessageID: int64(m.MessageID),
		UserID:    m.UserID,
		Username:  m.Username,
		Text:      m.Text,
		SentAt:    m.SentAt.Unix(),
		IsBot:     m.IsBot,
	}
	if m.ReplyTo != nil {
		params.ReplyToUsername = sql.NullString{String: m.ReplyTo.Username, Valid: true}
		params.ReplyToText = sql.NullString{String: m.ReplyTo.Text, Valid: true}
	}
	if err := q.InsertMessage(ctx, params); err != nil {
		return fmt.Errorf("insert message: %w", err)
	}

	boundary, err := q.PruneBoundary(ctx, sqlcgen.PruneBoundaryParams{ChatID: m.ChatID, Offset: int64(keep)})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// Fewer than keep messages, nothing to prune.
	case err != nil:
		return fmt.Errorf("prune boundary: %w", err)
	default:
		if err := q.DeleteMessagesUpTo(ctx, sqlcgen.DeleteMessagesUpToParams{ChatID: m.ChatID, MessageID: boundary}); err != nil {
			return fmt.Errorf("prune: %w", err)
		}
	}
	return tx.Commit()
}

// Since returns the chat's cached messages starting at messageID, oldest first.
func (s *MessageStore) Since(ctx context.Context, chatID int64, messageID int) ([]domain.Message, error) {
	rows, err := s.db.q.MessagesSince(ctx, sqlcgen.MessagesSinceParams{ChatID: chatID, MessageID: int64(messageID)})
	if err != nil {
		return nil, fmt.Errorf("messages since: %w", err)
	}
	out := make([]domain.Message, len(rows))
	for i, r := range rows {
		out[i] = domain.Message{
			ChatID:    r.ChatID,
			MessageID: int(r.MessageID),
			UserID:    r.UserID,
			Username:  r.Username,
			Text:      r.Text,
			SentAt:    time.Unix(r.SentAt, 0),
			IsBot:     r.IsBot,
		}
		if r.ReplyToUsername.Valid {
			out[i].ReplyTo = &domain.Quote{Username: r.ReplyToUsername.String, Text: r.ReplyToText.String}
		}
	}
	return out, nil
}

// Users returns every human who has a cached message in the chat.
func (s *MessageStore) Users(ctx context.Context, chatID int64) ([]domain.User, error) {
	rows, err := s.db.q.ChatUsers(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("chat users: %w", err)
	}
	out := make([]domain.User, len(rows))
	for i, r := range rows {
		out[i] = domain.User{ID: r.UserID, Name: r.Username}
	}
	return out, nil
}
