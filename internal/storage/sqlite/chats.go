package sqlite

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/feytox/kabanbot/internal/domain"
	"github.com/feytox/kabanbot/internal/storage/sqlite/sqlcgen"
)

// ChatStore persists chats the bot is in, their settings, and known users.
type ChatStore struct {
	db *DB
}

// NewChatStore creates a ChatStore.
func NewChatStore(db *DB) *ChatStore { return &ChatStore{db: db} }

// settingsJSON is the stored form of per-chat feature flags.
// Pointers tell "never set" apart from "off", so new features default to on.
type settingsJSON struct {
	Summary    *bool `json:"summary,omitzero"`
	MentionAll *bool `json:"mention_all,omitzero"`
}

// TouchChat records the chat's title and whether the bot is a member.
func (s *ChatStore) TouchChat(ctx context.Context, id int64, title string, member bool) error {
	if err := s.db.q.TouchChat(ctx, sqlcgen.TouchChatParams{ID: id, Title: title, Member: member}); err != nil {
		return fmt.Errorf("touch chat: %w", err)
	}
	return nil
}

// Chat returns the chat, or domain.ErrNotFound if the bot has never seen it.
func (s *ChatStore) Chat(ctx context.Context, id int64) (domain.Chat, error) {
	c, err := s.db.q.GetChat(ctx, id)
	if err != nil {
		return domain.Chat{}, notFound(err, "chat")
	}
	return toChat(c)
}

// ChatSettings returns the chat's settings, or the defaults for unknown chats.
func (s *ChatStore) ChatSettings(ctx context.Context, id int64) (domain.ChatSettings, error) {
	c, err := s.Chat(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.DefaultChatSettings(), nil
		}
		return domain.ChatSettings{}, err
	}
	return c.Settings, nil
}

// MemberChats lists chats the bot is currently in.
func (s *ChatStore) MemberChats(ctx context.Context) ([]domain.Chat, error) {
	rows, err := s.db.q.MemberChats(ctx)
	if err != nil {
		return nil, fmt.Errorf("member chats: %w", err)
	}
	return toChats(rows)
}

// ChatsBySummaryModel lists chats that use the model for summaries.
func (s *ChatStore) ChatsBySummaryModel(ctx context.Context, modelID int64) ([]domain.Chat, error) {
	rows, err := s.db.q.ChatsBySummaryModel(ctx, sql.NullInt64{Int64: modelID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("chats by model: %w", err)
	}
	return toChats(rows)
}

// UpdateChat saves the chat's settings and model binding.
func (s *ChatStore) UpdateChat(ctx context.Context, c domain.Chat, updatedBy int64) error {
	flags, err := json.Marshal(settingsJSON{Summary: &c.Settings.Summary, MentionAll: &c.Settings.MentionAll})
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	err = s.db.q.UpdateChatSettings(ctx, sqlcgen.UpdateChatSettingsParams{
		ID:             c.ID,
		Enabled:        c.Settings.Enabled,
		SettingsJson:   string(flags),
		SummaryModelID: nullInt(c.SummaryModelID),
		UpdatedBy:      sql.NullInt64{Int64: updatedBy, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("update chat: %w", err)
	}
	return nil
}

// UnbindSummaryModel removes the model from the chat if it is still bound there.
func (s *ChatStore) UnbindSummaryModel(ctx context.Context, chatID, modelID int64) error {
	err := s.db.q.UnbindSummaryModel(ctx, sqlcgen.UnbindSummaryModelParams{
		ID: chatID, SummaryModelID: sql.NullInt64{Int64: modelID, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("unbind model: %w", err)
	}
	return nil
}

// UpsertUser records a user's current names.
func (s *ChatStore) UpsertUser(ctx context.Context, id int64, username, firstName string) error {
	err := s.db.q.UpsertUser(ctx, sqlcgen.UpsertUserParams{ID: id, Username: username, FirstName: firstName})
	if err != nil {
		return fmt.Errorf("upsert user: %w", err)
	}
	return nil
}

func toChat(c sqlcgen.Chat) (domain.Chat, error) {
	var flags settingsJSON
	if err := json.Unmarshal([]byte(c.SettingsJson), &flags); err != nil {
		return domain.Chat{}, fmt.Errorf("chat %d settings: %w", c.ID, err)
	}
	settings := domain.DefaultChatSettings()
	settings.Enabled = c.Enabled
	if flags.Summary != nil {
		settings.Summary = *flags.Summary
	}
	if flags.MentionAll != nil {
		settings.MentionAll = *flags.MentionAll
	}
	out := domain.Chat{ID: c.ID, Title: c.Title, Settings: settings}
	if c.SummaryModelID.Valid {
		out.SummaryModelID = &c.SummaryModelID.Int64
	}
	return out, nil
}

func toChats(rows []sqlcgen.Chat) ([]domain.Chat, error) {
	out := make([]domain.Chat, len(rows))
	for i, r := range rows {
		c, err := toChat(r)
		if err != nil {
			return nil, err
		}
		out[i] = c
	}
	return out, nil
}

func nullInt(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}
