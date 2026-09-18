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

// Crypter encrypts provider API keys at rest.
type Crypter interface {
	Seal(plaintext []byte) []byte
	Open(sealed []byte) ([]byte, error)
}

// ModelStore persists LLM providers and models. API keys are encrypted transparently.
type ModelStore struct {
	db  *DB
	box Crypter // nil when no master key is configured
}

// NewModelStore creates a ModelStore. box may be nil if no master key is configured;
// then providers cannot be created or used.
func NewModelStore(db *DB, box Crypter) *ModelStore { return &ModelStore{db: db, box: box} }

// SummaryModel returns the model bound to the chat for summaries, or domain.ErrNotFound.
func (s *ModelStore) SummaryModel(ctx context.Context, chatID int64) (domain.Model, domain.Provider, error) {
	row, err := s.db.q.ChatSummaryModel(ctx, chatID)
	if err != nil {
		return domain.Model{}, domain.Provider{}, notFound(err, "chat summary model")
	}
	return s.modelAndProvider(row.Model, row.Provider)
}

// ModelWithProvider returns a model and its provider with the decrypted API key.
func (s *ModelStore) ModelWithProvider(ctx context.Context, modelID int64) (domain.Model, domain.Provider, error) {
	row, err := s.db.q.ModelWithProvider(ctx, modelID)
	if err != nil {
		return domain.Model{}, domain.Provider{}, notFound(err, "model")
	}
	return s.modelAndProvider(row.Model, row.Provider)
}

func (s *ModelStore) modelAndProvider(m sqlcgen.Model, p sqlcgen.Provider) (domain.Model, domain.Provider, error) {
	model, err := toModel(m)
	if err != nil {
		return domain.Model{}, domain.Provider{}, err
	}
	provider := toProvider(p)
	if s.box == nil {
		return domain.Model{}, domain.Provider{}, domain.ErrNoMasterKey
	}
	key, err := s.box.Open(p.ApiKeyEnc)
	if err != nil {
		return domain.Model{}, domain.Provider{}, fmt.Errorf("provider %d key: %w", p.ID, err)
	}
	provider.APIKey = domain.Secret(key)
	return model, provider, nil
}

// Provider returns a provider without its API key.
func (s *ModelStore) Provider(ctx context.Context, id int64) (domain.Provider, error) {
	p, err := s.db.q.GetProvider(ctx, id)
	if err != nil {
		return domain.Provider{}, notFound(err, "provider")
	}
	return toProvider(p), nil
}

// ProvidersByOwner lists the user's providers without API keys.
func (s *ModelStore) ProvidersByOwner(ctx context.Context, userID int64) ([]domain.Provider, error) {
	rows, err := s.db.q.ProvidersByOwner(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("providers by owner: %w", err)
	}
	out := make([]domain.Provider, len(rows))
	for i, r := range rows {
		out[i] = toProvider(r)
	}
	return out, nil
}

// CreateProvider stores a new provider and returns its ID.
func (s *ModelStore) CreateProvider(ctx context.Context, p domain.Provider) (int64, error) {
	if s.box == nil {
		return 0, domain.ErrNoMasterKey
	}
	id, err := s.db.q.InsertProvider(ctx, sqlcgen.InsertProviderParams{
		OwnerUserID: p.OwnerID,
		Kind:        string(p.Kind),
		Name:        p.Name,
		BaseUrl:     p.BaseURL,
		ApiKeyEnc:   s.box.Seal([]byte(p.APIKey.Reveal())),
		ApiKeyHint:  p.KeyHint,
		Shared:      p.Shared,
	})
	if err != nil {
		return 0, fmt.Errorf("insert provider: %w", err)
	}
	return id, nil
}

// UpdateProvider updates a provider. The stored API key is replaced only if p.APIKey is set.
func (s *ModelStore) UpdateProvider(ctx context.Context, p domain.Provider) error {
	if p.APIKey == "" {
		err := s.db.q.UpdateProvider(ctx, sqlcgen.UpdateProviderParams{
			ID: p.ID, Name: p.Name, BaseUrl: p.BaseURL, Shared: p.Shared,
		})
		if err != nil {
			return fmt.Errorf("update provider: %w", err)
		}
		return nil
	}
	if s.box == nil {
		return domain.ErrNoMasterKey
	}
	err := s.db.q.UpdateProviderKey(ctx, sqlcgen.UpdateProviderKeyParams{
		ID:         p.ID,
		Name:       p.Name,
		BaseUrl:    p.BaseURL,
		ApiKeyEnc:  s.box.Seal([]byte(p.APIKey.Reveal())),
		ApiKeyHint: p.KeyHint,
		Shared:     p.Shared,
	})
	if err != nil {
		return fmt.Errorf("update provider: %w", err)
	}
	return nil
}

// DeleteProvider deletes a provider together with its models.
func (s *ModelStore) DeleteProvider(ctx context.Context, id int64) error {
	if err := s.db.q.DeleteProvider(ctx, id); err != nil {
		return fmt.Errorf("delete provider: %w", err)
	}
	return nil
}

// CreateModel stores a new model and returns its ID.
func (s *ModelStore) CreateModel(ctx context.Context, m domain.Model) (int64, error) {
	params, err := json.Marshal(m.Params)
	if err != nil {
		return 0, fmt.Errorf("marshal params: %w", err)
	}
	id, err := s.db.q.InsertModel(ctx, sqlcgen.InsertModelParams{
		ProviderID: m.ProviderID, ModelName: m.Name, DisplayName: m.DisplayName, ParamsJson: string(params),
	})
	if err != nil {
		return 0, fmt.Errorf("insert model: %w", err)
	}
	return id, nil
}

// UpdateModel updates a model. Its provider cannot change.
func (s *ModelStore) UpdateModel(ctx context.Context, m domain.Model) error {
	params, err := json.Marshal(m.Params)
	if err != nil {
		return fmt.Errorf("marshal params: %w", err)
	}
	err = s.db.q.UpdateModel(ctx, sqlcgen.UpdateModelParams{
		ID: m.ID, ModelName: m.Name, DisplayName: m.DisplayName, ParamsJson: string(params),
	})
	if err != nil {
		return fmt.Errorf("update model: %w", err)
	}
	return nil
}

// DeleteModel deletes a model. Chats using it fall back to the default model.
func (s *ModelStore) DeleteModel(ctx context.Context, id int64) error {
	if err := s.db.q.DeleteModel(ctx, id); err != nil {
		return fmt.Errorf("delete model: %w", err)
	}
	return nil
}

// ModelsByOwner lists models of all the user's providers.
func (s *ModelStore) ModelsByOwner(ctx context.Context, userID int64) ([]domain.Model, error) {
	rows, err := s.db.q.ModelsByOwner(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("models by owner: %w", err)
	}
	out := make([]domain.Model, len(rows))
	for i, r := range rows {
		if out[i], err = toModel(r); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// UsableModels lists the models the user may bind to a chat: their own and shared ones.
func (s *ModelStore) UsableModels(ctx context.Context, userID int64) ([]domain.ModelOption, error) {
	rows, err := s.db.q.UsableModels(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("usable models: %w", err)
	}
	out := make([]domain.ModelOption, len(rows))
	for i, r := range rows {
		if out[i], err = toModelOption(sqlcgen.ModelOptionByIDRow(r)); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Model returns a model without touching its provider's key.
func (s *ModelStore) Model(ctx context.Context, id int64) (domain.Model, error) {
	m, err := s.db.q.GetModel(ctx, id)
	if err != nil {
		return domain.Model{}, notFound(err, "model")
	}
	return toModel(m)
}

// ModelOption returns a model as shown in pickers, with its provider and owner names.
func (s *ModelStore) ModelOption(ctx context.Context, id int64) (domain.ModelOption, error) {
	r, err := s.db.q.ModelOptionByID(ctx, id)
	if err != nil {
		return domain.ModelOption{}, notFound(err, "model")
	}
	return toModelOption(r)
}

func toModelOption(r sqlcgen.ModelOptionByIDRow) (domain.ModelOption, error) {
	m, err := toModel(r.Model)
	if err != nil {
		return domain.ModelOption{}, err
	}
	return domain.ModelOption{
		Model:        m,
		ProviderName: r.ProviderName,
		ProviderKind: domain.ProviderKind(r.ProviderKind),
		OwnerID:      r.OwnerUserID,
		OwnerName:    displayName(r.OwnerUsername, r.OwnerFirstName),
		Shared:       r.Shared,
	}, nil
}

func toModel(m sqlcgen.Model) (domain.Model, error) {
	out := domain.Model{ID: m.ID, ProviderID: m.ProviderID, Name: m.ModelName, DisplayName: m.DisplayName}
	if err := json.Unmarshal([]byte(m.ParamsJson), &out.Params); err != nil {
		return domain.Model{}, fmt.Errorf("model %d params: %w", m.ID, err)
	}
	return out, nil
}

func toProvider(p sqlcgen.Provider) domain.Provider {
	return domain.Provider{
		ID:      p.ID,
		OwnerID: p.OwnerUserID,
		Kind:    domain.ProviderKind(p.Kind),
		Name:    p.Name,
		BaseURL: p.BaseUrl,
		KeyHint: p.ApiKeyHint,
		Shared:  p.Shared,
	}
}

func displayName(username, firstName string) string {
	if username != "" {
		return "@" + username
	}
	return firstName
}

// notFound maps sql.ErrNoRows to domain.ErrNotFound and wraps other errors.
func notFound(err error, what string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	return fmt.Errorf("%s: %w", what, err)
}
