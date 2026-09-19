package sqlite

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"

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
	Summary     *bool `json:"summary,omitzero"`
	MentionAll  *bool `json:"mention_all,omitzero"`
	Chat        *bool `json:"chat,omitzero"`
	UserPerHour *int  `json:"user_per_hour,omitzero"`
	ChatPerHour *int  `json:"chat_per_hour,omitzero"`
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

// ChatsByModel lists chats that use the model for summaries or chatting.
func (s *ChatStore) ChatsByModel(ctx context.Context, modelID int64) ([]domain.Chat, error) {
	rows, err := s.db.q.ChatsByModel(ctx, sql.NullInt64{Int64: modelID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("chats by model: %w", err)
	}
	return toChats(rows)
}

// UpdateChat saves the chat's settings and model bindings.
func (s *ChatStore) UpdateChat(ctx context.Context, c domain.Chat, updatedBy int64) error {
	st := c.Settings
	flags, err := json.Marshal(settingsJSON{
		Summary: &st.Summary, MentionAll: &st.MentionAll, Chat: &st.Chat,
		UserPerHour: &st.Limits.UserPerHour, ChatPerHour: &st.Limits.ChatPerHour,
	})
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	err = s.db.q.UpdateChatSettings(ctx, sqlcgen.UpdateChatSettingsParams{
		ID:             c.ID,
		Enabled:        c.Settings.Enabled,
		SettingsJson:   string(flags),
		SummaryModelID: nullInt(c.SummaryModelID),
		ChatModelID:    nullInt(c.ChatModelID),
		UpdatedBy:      sql.NullInt64{Int64: updatedBy, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("update chat: %w", err)
	}
	return nil
}

// UnbindModel removes the model from the chat wherever it is still bound there.
func (s *ChatStore) UnbindModel(ctx context.Context, chatID, modelID int64) error {
	err := s.db.q.UnbindModel(ctx, sqlcgen.UnbindModelParams{
		ChatID: chatID, ModelID: sql.NullInt64{Int64: modelID, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("unbind model: %w", err)
	}
	return nil
}

// SetPersonality changes the chat's personality and records the change in its history.
func (s *ChatStore) SetPersonality(ctx context.Context, chatID int64, text string, by int64, via domain.ChangeVia) error {
	tx, err := s.db.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := s.db.q.WithTx(tx)
	err = q.SetPersonality(ctx, sqlcgen.SetPersonalityParams{
		ID: chatID, Personality: text, UpdatedBy: sql.NullInt64{Int64: by, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("set personality: %w", err)
	}
	err = q.InsertPersonalityChange(ctx, sqlcgen.InsertPersonalityChangeParams{
		ChatID: chatID, Text: text, ChangedBy: by, Via: string(via),
	})
	if err != nil {
		return fmt.Errorf("record personality change: %w", err)
	}
	return tx.Commit()
}

// PersonalityHistory returns the chat's latest personality changes, newest first.
func (s *ChatStore) PersonalityHistory(ctx context.Context, chatID int64, limit int) ([]domain.PersonalityChange, error) {
	rows, err := s.db.q.PersonalityHistory(ctx, sqlcgen.PersonalityHistoryParams{ChatID: chatID, Limit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("personality history: %w", err)
	}
	out := make([]domain.PersonalityChange, len(rows))
	for i, r := range rows {
		out[i] = domain.PersonalityChange{
			ID: r.ID, ChatID: r.ChatID, Text: r.Text, ChangedBy: r.ChangedBy,
			ChangedByName: displayName(r.Username, r.FirstName),
			Via:           domain.ChangeVia(r.Via), At: time.Unix(r.CreatedAt, 0),
		}
	}
	return out, nil
}

// PersonalityChange returns one entry of a personality history.
func (s *ChatStore) PersonalityChange(ctx context.Context, id int64) (domain.PersonalityChange, error) {
	r, err := s.db.q.GetPersonalityChange(ctx, id)
	if err != nil {
		return domain.PersonalityChange{}, notFound(err, "personality change")
	}
	return domain.PersonalityChange{
		ID: r.ID, ChatID: r.ChatID, Text: r.Text, ChangedBy: r.ChangedBy,
		Via: domain.ChangeVia(r.Via), At: time.Unix(r.CreatedAt, 0),
	}, nil
}

// SetSummaryStyle changes the chat's summary style.
func (s *ChatStore) SetSummaryStyle(ctx context.Context, chatID int64, style string, by int64) error {
	err := s.db.q.SetSummaryStyle(ctx, sqlcgen.SetSummaryStyleParams{
		ID: chatID, SummaryStyle: style, UpdatedBy: sql.NullInt64{Int64: by, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("set summary style: %w", err)
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
	setIf(&settings.Summary, flags.Summary)
	setIf(&settings.MentionAll, flags.MentionAll)
	setIf(&settings.Chat, flags.Chat)
	setIf(&settings.Limits.UserPerHour, flags.UserPerHour)
	setIf(&settings.Limits.ChatPerHour, flags.ChatPerHour)
	out := domain.Chat{
		ID: c.ID, Title: c.Title, Settings: settings,
		SummaryModelID: fromNullInt(c.SummaryModelID),
		ChatModelID:    fromNullInt(c.ChatModelID),
		Personality:    c.Personality,
		SummaryStyle:   c.SummaryStyle,
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

// setIf overwrites a default with a stored value, if there is one.
func setIf[T any](dst, src *T) {
	if src != nil {
		*dst = *src
	}
}

func fromNullInt(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func nullInt(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}
