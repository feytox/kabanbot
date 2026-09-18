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

// Opener decrypts provider API keys.
type Opener interface {
	Open(sealed []byte) ([]byte, error)
}

// ModelStore reads LLM providers and models.
type ModelStore struct {
	db  *DB
	box Opener // nil when no master key is configured
}

// NewModelStore creates a ModelStore. box may be nil if no master key is configured.
func NewModelStore(db *DB, box Opener) *ModelStore { return &ModelStore{db: db, box: box} }

// SummaryModel returns the model bound to the chat for summaries, or domain.ErrNotFound.
func (s *ModelStore) SummaryModel(ctx context.Context, chatID int64) (domain.Model, domain.Provider, error) {
	row, err := s.db.q.ChatSummaryModel(ctx, chatID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Model{}, domain.Provider{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Model{}, domain.Provider{}, fmt.Errorf("chat summary model: %w", err)
	}
	model, err := toModel(row.Model)
	if err != nil {
		return domain.Model{}, domain.Provider{}, err
	}
	provider, err := s.toProvider(row.Provider)
	if err != nil {
		return domain.Model{}, domain.Provider{}, err
	}
	return model, provider, nil
}

func toModel(m sqlcgen.Model) (domain.Model, error) {
	out := domain.Model{ID: m.ID, ProviderID: m.ProviderID, Name: m.ModelName, DisplayName: m.DisplayName}
	if err := json.Unmarshal([]byte(m.ParamsJson), &out.Params); err != nil {
		return domain.Model{}, fmt.Errorf("model %d params: %w", m.ID, err)
	}
	return out, nil
}

func (s *ModelStore) toProvider(p sqlcgen.Provider) (domain.Provider, error) {
	if s.box == nil {
		return domain.Provider{}, errors.New("MASTER_KEY is not configured, cannot decrypt provider keys")
	}
	key, err := s.box.Open(p.ApiKeyEnc)
	if err != nil {
		return domain.Provider{}, fmt.Errorf("provider %d key: %w", p.ID, err)
	}
	return domain.Provider{
		ID:      p.ID,
		OwnerID: p.OwnerUserID,
		Kind:    domain.ProviderKind(p.Kind),
		Name:    p.Name,
		BaseURL: p.BaseUrl,
		APIKey:  domain.Secret(key),
		Shared:  p.Shared,
	}, nil
}
