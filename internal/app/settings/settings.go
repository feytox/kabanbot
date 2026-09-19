// Package settings manages LLM providers, models, and per-chat settings on behalf of users.
// All authorization decisions of the settings UI live here.
package settings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/feytox/kabanbot/internal/domain"
	"github.com/feytox/kabanbot/internal/llm"
)

var (
	// ErrForbidden means the user may see the resource but not change it.
	ErrForbidden = errors.New("forbidden")
	// ErrNotFound means the resource does not exist or the user may not see it.
	ErrNotFound = domain.ErrNotFound
)

// ValidationError describes invalid user input. Its message is safe to show to the user.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// Models persists providers and models.
type Models interface {
	Provider(ctx context.Context, id int64) (domain.Provider, error)
	ProvidersByOwner(ctx context.Context, userID int64) ([]domain.Provider, error)
	CreateProvider(ctx context.Context, p domain.Provider) (int64, error)
	UpdateProvider(ctx context.Context, p domain.Provider) error
	DeleteProvider(ctx context.Context, id int64) error

	Model(ctx context.Context, id int64) (domain.Model, error)
	ModelWithProvider(ctx context.Context, id int64) (domain.Model, domain.Provider, error)
	ModelOption(ctx context.Context, id int64) (domain.ModelOption, error)
	ModelsByOwner(ctx context.Context, userID int64) ([]domain.Model, error)
	UsableModels(ctx context.Context, userID int64) ([]domain.ModelOption, error)
	CreateModel(ctx context.Context, m domain.Model) (int64, error)
	UpdateModel(ctx context.Context, m domain.Model) error
	DeleteModel(ctx context.Context, id int64) error
}

// Chats persists chats and users.
type Chats interface {
	MemberChats(ctx context.Context) ([]domain.Chat, error)
	Chat(ctx context.Context, id int64) (domain.Chat, error)
	UpdateChat(ctx context.Context, c domain.Chat, updatedBy int64) error
	ChatsBySummaryModel(ctx context.Context, modelID int64) ([]domain.Chat, error)
	UnbindSummaryModel(ctx context.Context, chatID, modelID int64) error
	UpsertUser(ctx context.Context, id int64, username, firstName string) error
}

// Admins checks chat administrator rights.
type Admins interface {
	IsAdmin(ctx context.Context, chatID, userID int64) (bool, error)
}

// Clients creates LLM clients for providers and forgets them when providers change.
type Clients interface {
	Client(ctx context.Context, p domain.Provider) (llm.Client, error)
	Invalidate(providerID int64)
}

// User is the Telegram user acting in the settings UI.
type User struct {
	ID        int64
	Username  string
	FirstName string
}

// Service implements the settings use cases.
type Service struct {
	models  Models
	chats   Chats
	admins  Admins
	clients Clients
	ownerID int64
	log     *slog.Logger
}

// New creates a Service. ownerID is the bot owner, who alone may share providers with everyone.
func New(models Models, chats Chats, admins Admins, clients Clients, ownerID int64, log *slog.Logger) *Service {
	return &Service{models: models, chats: chats, admins: admins, clients: clients, ownerID: ownerID, log: log}
}

// IsOwner reports whether u is the bot owner.
func (s *Service) IsOwner(u User) bool { return s.ownerID != 0 && u.ID == s.ownerID }

// Seen records the user's current names so others see who owns a model.
func (s *Service) Seen(ctx context.Context, u User) error {
	return s.chats.UpsertUser(ctx, u.ID, u.Username, u.FirstName)
}

// ChatRef is a short reference to a chat.
type ChatRef struct {
	ID    int64
	Title string
}

// ModelView is one of the user's models with the chats it is bound to.
type ModelView struct {
	domain.Model
	Chats []ChatRef
}

// ProviderView is one of the user's providers with its models. It never carries the API key.
type ProviderView struct {
	domain.Provider
	Models []ModelView
}

// Providers lists the user's providers and models.
func (s *Service) Providers(ctx context.Context, u User) ([]ProviderView, error) {
	providers, err := s.models.ProvidersByOwner(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	models, err := s.models.ModelsByOwner(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	out := make([]ProviderView, len(providers))
	for i, p := range providers {
		out[i].Provider = p
		for _, m := range models {
			if m.ProviderID != p.ID {
				continue
			}
			chats, err := s.chats.ChatsBySummaryModel(ctx, m.ID)
			if err != nil {
				return nil, err
			}
			refs := make([]ChatRef, len(chats))
			for j, c := range chats {
				refs[j] = ChatRef{ID: c.ID, Title: c.Title}
			}
			out[i].Models = append(out[i].Models, ModelView{Model: m, Chats: refs})
		}
	}
	return out, nil
}

// ProviderInput is the editable part of a provider.
type ProviderInput struct {
	Kind    domain.ProviderKind
	Name    string
	BaseURL string
	// APIKey is required on creation. On update, empty keeps the stored key.
	APIKey string
	Shared bool
}

// CreateProvider adds a provider owned by the user.
func (s *Service) CreateProvider(ctx context.Context, u User, in ProviderInput) (domain.Provider, error) {
	if err := s.validateProvider(u, in, true); err != nil {
		return domain.Provider{}, err
	}
	p := domain.Provider{
		OwnerID: u.ID,
		Kind:    in.Kind,
		Name:    strings.TrimSpace(in.Name),
		BaseURL: strings.TrimSpace(in.BaseURL),
		APIKey:  domain.Secret(in.APIKey),
		KeyHint: domain.KeyHint(in.APIKey),
		Shared:  in.Shared,
	}
	id, err := s.models.CreateProvider(ctx, p)
	if err != nil {
		return domain.Provider{}, err
	}
	p.ID, p.APIKey = id, ""
	return p, nil
}

// UpdateProvider changes one of the user's providers. The kind cannot change.
func (s *Service) UpdateProvider(ctx context.Context, u User, id int64, in ProviderInput) (domain.Provider, error) {
	p, err := s.ownProvider(ctx, u, id)
	if err != nil {
		return domain.Provider{}, err
	}
	in.Kind = p.Kind
	if err := s.validateProvider(u, in, false); err != nil {
		return domain.Provider{}, err
	}
	baseURL := strings.TrimSpace(in.BaseURL)
	// Otherwise a stored key could be redirected to a server that logs it.
	if baseURL != p.BaseURL && in.APIKey == "" {
		return domain.Provider{}, invalid("При смене адреса API нужно заново ввести ключ")
	}

	p.Name, p.BaseURL, p.Shared = strings.TrimSpace(in.Name), baseURL, in.Shared
	p.APIKey = domain.Secret(in.APIKey)
	if in.APIKey != "" {
		p.KeyHint = domain.KeyHint(in.APIKey)
	}
	if err := s.models.UpdateProvider(ctx, p); err != nil {
		return domain.Provider{}, err
	}
	s.clients.Invalidate(p.ID)
	p.APIKey = ""
	return p, nil
}

// DeleteProvider removes one of the user's providers with all its models.
func (s *Service) DeleteProvider(ctx context.Context, u User, id int64) error {
	if _, err := s.ownProvider(ctx, u, id); err != nil {
		return err
	}
	if err := s.models.DeleteProvider(ctx, id); err != nil {
		return err
	}
	s.clients.Invalidate(id)
	return nil
}

func (s *Service) validateProvider(u User, in ProviderInput, creating bool) error {
	if !in.Kind.Valid() {
		return invalid("Неизвестный тип провайдера")
	}
	if err := ValidateProviderName(in.Name); err != nil {
		return err
	}
	if creating || in.APIKey != "" {
		if err := ValidateAPIKey(in.APIKey); err != nil {
			return err
		}
	}
	if err := ValidateBaseURL(in.BaseURL); err != nil {
		return err
	}
	if in.Shared && !s.IsOwner(u) {
		return invalid("Делиться провайдером со всеми может только владелец бота")
	}
	return nil
}

// The Validate* functions check single fields, so a UI asking for them one at a time
// can reject bad input right away. The Service methods check them again.

// ValidateProviderName checks a provider name.
func ValidateProviderName(name string) error {
	if n := utf8.RuneCountInString(strings.TrimSpace(name)); n == 0 || n > 64 {
		return invalid("Название должно быть от 1 до 64 символов")
	}
	return nil
}

// ValidateAPIKey checks a non-optional API key.
func ValidateAPIKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return invalid("Нужен API-ключ")
	}
	if len(key) > 1024 {
		return invalid("Слишком длинный API-ключ")
	}
	return nil
}

// ValidateBaseURL checks a provider API address. Empty means the provider's default.
func ValidateBaseURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return invalid("Адрес API должен быть ссылкой вида https://host/path")
	}
	return nil
}

// ownProvider loads a provider owned by the user. Other users' providers look nonexistent.
func (s *Service) ownProvider(ctx context.Context, u User, id int64) (domain.Provider, error) {
	p, err := s.models.Provider(ctx, id)
	if err != nil {
		return domain.Provider{}, err
	}
	if p.OwnerID != u.ID {
		return domain.Provider{}, ErrNotFound
	}
	return p, nil
}

// ModelInput is the editable part of a model.
type ModelInput struct {
	Name        string
	DisplayName string
	Params      domain.ModelParams
}

// CreateModel adds a model to one of the user's providers.
func (s *Service) CreateModel(ctx context.Context, u User, providerID int64, in ModelInput) (domain.Model, error) {
	if _, err := s.ownProvider(ctx, u, providerID); err != nil {
		return domain.Model{}, err
	}
	m, err := modelFromInput(in)
	if err != nil {
		return domain.Model{}, err
	}
	m.ProviderID = providerID
	if m.ID, err = s.models.CreateModel(ctx, m); err != nil {
		return domain.Model{}, err
	}
	return m, nil
}

// UpdateModel changes one of the user's models.
func (s *Service) UpdateModel(ctx context.Context, u User, id int64, in ModelInput) (domain.Model, error) {
	old, err := s.ownModel(ctx, u, id)
	if err != nil {
		return domain.Model{}, err
	}
	m, err := modelFromInput(in)
	if err != nil {
		return domain.Model{}, err
	}
	m.ID, m.ProviderID = old.ID, old.ProviderID
	if err := s.models.UpdateModel(ctx, m); err != nil {
		return domain.Model{}, err
	}
	return m, nil
}

// DeleteModel removes one of the user's models. Chats using it are left without a model.
func (s *Service) DeleteModel(ctx context.Context, u User, id int64) error {
	if _, err := s.ownModel(ctx, u, id); err != nil {
		return err
	}
	return s.models.DeleteModel(ctx, id)
}

// TestModel sends a tiny request to one of the user's models and returns its reply.
func (s *Service) TestModel(ctx context.Context, u User, id int64) (string, time.Duration, error) {
	if _, err := s.ownModel(ctx, u, id); err != nil {
		return "", 0, err
	}
	model, provider, err := s.models.ModelWithProvider(ctx, id)
	if err != nil {
		return "", 0, err
	}
	client, err := s.clients.Client(ctx, provider)
	if err != nil {
		return "", 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	start := time.Now()
	target := llm.Target{Client: client, Model: model}
	resp, err := client.Complete(ctx, target.Request("Reply with a short greeting in Russian.",
		llm.Message{Role: llm.RoleUser, Content: "ping"}))
	if err != nil {
		return "", 0, fmt.Errorf("test model: %w", err)
	}
	return resp.Text, time.Since(start), nil
}

// UnbindModel detaches one of the user's models from a chat, whoever bound it there.
func (s *Service) UnbindModel(ctx context.Context, u User, modelID, chatID int64) error {
	if _, err := s.ownModel(ctx, u, modelID); err != nil {
		return err
	}
	return s.chats.UnbindSummaryModel(ctx, chatID, modelID)
}

// UsableModels lists the models the user may bind to chats.
func (s *Service) UsableModels(ctx context.Context, u User) ([]domain.ModelOption, error) {
	return s.models.UsableModels(ctx, u.ID)
}

func (s *Service) ownModel(ctx context.Context, u User, id int64) (domain.Model, error) {
	m, err := s.models.Model(ctx, id)
	if err != nil {
		return domain.Model{}, err
	}
	if _, err := s.ownProvider(ctx, u, m.ProviderID); err != nil {
		return domain.Model{}, err
	}
	return m, nil
}

func modelFromInput(in ModelInput) (domain.Model, error) {
	name := strings.TrimSpace(in.Name)
	display := strings.TrimSpace(in.DisplayName)
	if display == "" {
		display = name
	}
	for _, err := range []error{
		ValidateModelName(name),
		ValidateModelDisplayName(display),
		ValidateTemperature(in.Params.Temperature),
		ValidateMaxTokens(in.Params.MaxTokens),
	} {
		if err != nil {
			return domain.Model{}, err
		}
	}
	return domain.Model{Name: name, DisplayName: display, Params: in.Params}, nil
}

// ValidateModelName checks a model ID as the provider knows it.
func ValidateModelName(name string) error {
	if n := utf8.RuneCountInString(strings.TrimSpace(name)); n == 0 || n > 128 {
		return invalid("ID модели должен быть от 1 до 128 символов")
	}
	return nil
}

// ValidateModelDisplayName checks a model's display name. Empty means the model ID.
func ValidateModelDisplayName(name string) error {
	if utf8.RuneCountInString(strings.TrimSpace(name)) > 64 {
		return invalid("Название модели должно быть не длиннее 64 символов")
	}
	return nil
}

// ValidateTemperature checks a temperature. Nil means the provider's default.
func ValidateTemperature(t *float64) error {
	if t != nil && (*t < 0 || *t > 2) {
		return invalid("Temperature должна быть от 0 до 2")
	}
	return nil
}

// ValidateMaxTokens checks a response token limit. Zero means no limit.
func ValidateMaxTokens(n int64) error {
	if n < 0 || n > 1_000_000 {
		return invalid("Лимит токенов должен быть от 0 до 1 000 000")
	}
	return nil
}

// ChatView is a chat as its admins see it.
type ChatView struct {
	domain.Chat
	// SummaryModel describes the bound model; nil means none.
	SummaryModel *domain.ModelOption
}

// Chats lists the chats the bot is in where the user is an admin.
func (s *Service) Chats(ctx context.Context, u User) ([]ChatView, error) {
	chats, err := s.chats.MemberChats(ctx)
	if err != nil {
		return nil, err
	}
	var out []ChatView
	for _, c := range chats {
		ok, err := s.admins.IsAdmin(ctx, c.ID, u.ID)
		if err != nil {
			// The bot may have lost access to the chat; skip it rather than fail the whole list.
			s.log.WarnContext(ctx, "check admin", "chat_id", c.ID, "err", err)
			continue
		}
		if !ok {
			continue
		}
		v, err := s.view(ctx, c)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// Chat returns a chat where the user is an admin.
func (s *Service) Chat(ctx context.Context, u User, id int64) (ChatView, error) {
	c, err := s.adminChat(ctx, u, id)
	if err != nil {
		return ChatView{}, err
	}
	return s.view(ctx, c)
}

// ChatInput is the editable part of a chat.
type ChatInput struct {
	Settings       domain.ChatSettings
	SummaryModelID *int64
}

// UpdateChat changes a chat's settings. The user must be a chat admin, and a newly bound
// model must be one the user may use. Keeping a model someone else bound is allowed.
func (s *Service) UpdateChat(ctx context.Context, u User, id int64, in ChatInput) (ChatView, error) {
	c, err := s.adminChat(ctx, u, id)
	if err != nil {
		return ChatView{}, err
	}
	if m := in.SummaryModelID; m != nil && !equalPtr(m, c.SummaryModelID) {
		usable, err := s.models.UsableModels(ctx, u.ID)
		if err != nil {
			return ChatView{}, err
		}
		if !slices.ContainsFunc(usable, func(o domain.ModelOption) bool { return o.ID == *m }) {
			return ChatView{}, invalid("Эту модель нельзя подключить: она не ваша и не общая")
		}
	}
	c.Settings, c.SummaryModelID = in.Settings, in.SummaryModelID
	if err := s.chats.UpdateChat(ctx, c, u.ID); err != nil {
		return ChatView{}, err
	}
	return s.view(ctx, c)
}

func (s *Service) adminChat(ctx context.Context, u User, id int64) (domain.Chat, error) {
	c, err := s.chats.Chat(ctx, id)
	if err != nil {
		return domain.Chat{}, err
	}
	ok, err := s.admins.IsAdmin(ctx, id, u.ID)
	if err != nil {
		return domain.Chat{}, fmt.Errorf("check admin: %w", err)
	}
	if !ok {
		return domain.Chat{}, ErrForbidden
	}
	return c, nil
}

func (s *Service) view(ctx context.Context, c domain.Chat) (ChatView, error) {
	v := ChatView{Chat: c}
	if c.SummaryModelID == nil {
		return v, nil
	}
	m, err := s.models.ModelOption(ctx, *c.SummaryModelID)
	if errors.Is(err, domain.ErrNotFound) {
		return v, nil
	}
	if err != nil {
		return ChatView{}, err
	}
	v.SummaryModel = &m
	return v, nil
}

func equalPtr(a, b *int64) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
