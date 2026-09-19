// Package domain holds the core entities shared across the application.
package domain

import (
	"errors"
	"log/slog"
	"time"
)

// ErrNotFound is returned by stores when a requested entity does not exist.
var ErrNotFound = errors.New("not found")

// ErrNoModel is returned when a chat has no model bound for the task.
var ErrNoModel = errors.New("no model bound to the chat")

// ErrNoMasterKey is returned when provider API keys cannot be encrypted or decrypted
// because MASTER_KEY is not configured.
var ErrNoMasterKey = errors.New("MASTER_KEY is not configured")

// Message is a cached chat message.
type Message struct {
	ChatID    int64
	MessageID int
	UserID    int64
	Username  string
	Text      string
	SentAt    time.Time
	IsBot     bool
	// ReplyTo is set when the message replies to another message.
	ReplyTo *Quote
}

// Quote is a short reference to a replied-to message.
type Quote struct {
	Username string
	Text     string
}

// User is a chat participant.
type User struct {
	ID   int64
	Name string
}

// ProviderKind identifies an LLM API flavor.
type ProviderKind string

const (
	ProviderOpenAI     ProviderKind = "openai"
	ProviderOpenRouter ProviderKind = "openrouter"
	ProviderGemini     ProviderKind = "gemini"
)

// Valid reports whether k is a known provider kind.
func (k ProviderKind) Valid() bool {
	switch k {
	case ProviderOpenAI, ProviderOpenRouter, ProviderGemini:
		return true
	}
	return false
}

// Provider is an LLM API endpoint with credentials owned by a user.
type Provider struct {
	ID      int64
	OwnerID int64
	Kind    ProviderKind
	Name    string
	BaseURL string
	APIKey  Secret
	// KeyHint is the tail of the API key, safe to show to its owner.
	KeyHint string
	Shared  bool
}

// KeyHint returns the part of an API key that may be shown back to its owner.
func KeyHint(key string) string {
	r := []rune(key)
	if len(r) <= 8 {
		return ""
	}
	return string(r[len(r)-4:])
}

// Model is a concrete model offered by a provider.
type Model struct {
	ID          int64
	ProviderID  int64
	Name        string
	DisplayName string
	Params      ModelParams
}

// ModelParams are optional generation parameters.
type ModelParams struct {
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   int64    `json:"max_tokens,omitempty"`
}

// Chat is a group the bot has been added to, or a user's private chat with the bot, with its settings.
type Chat struct {
	ID       int64
	Title    string
	Settings ChatSettings
	// SummaryModelID is the model bound for summaries; nil means none, so summaries are off.
	SummaryModelID *int64
	// ChatModelID is the model the bot talks with; nil means the summary model.
	ChatModelID *int64
	// Personality is extra instructions for the bot's character in this chat.
	Personality string
	// SummaryStyle is extra instructions for summaries in this chat.
	SummaryStyle string
}

// IsPrivateChat reports whether the chat ID is a private chat. Telegram gives private
// chats the ID of the user, and groups negative IDs.
func IsPrivateChat(id int64) bool { return id > 0 }

// ChatSettings are the per-chat switches editable by chat admins.
type ChatSettings struct {
	// Enabled turns the bot's features on or off in the chat as a whole.
	Enabled bool
	Features
	Limits RateLimits
}

// Features toggles individual bot features.
type Features struct {
	Summary    bool
	MentionAll bool
	// Chat lets people talk to the bot.
	Chat bool
}

// RateLimits bound how often the bot answers in a chat. Zero means no limit.
type RateLimits struct {
	UserPerHour int
	ChatPerHour int
}

// DefaultChatSettings are the settings of a chat nobody has configured yet.
func DefaultChatSettings() ChatSettings {
	s := ChatSettings{Enabled: true, Limits: RateLimits{UserPerHour: 20, ChatPerHour: 60}}
	s.Summary, s.MentionAll, s.Chat = true, true, true
	return s
}

// ChangeVia is how a setting was changed.
type ChangeVia string

const (
	ViaMenu ChangeVia = "menu"
	ViaTool ChangeVia = "tool"
)

// PersonalityChange is one entry of a chat's personality history.
type PersonalityChange struct {
	ID     int64
	ChatID int64
	// Text is the personality after the change; empty means it was reset.
	Text          string
	ChangedBy     int64
	ChangedByName string
	Via           ChangeVia
	At            time.Time
}

// UsageKind is what a model call was for.
type UsageKind string

const (
	UsageSummary UsageKind = "summary"
	UsageChat    UsageKind = "chat"
)

// Usage is the token accounting of one model call.
type Usage struct {
	ChatID int64
	UserID int64
	// ModelID is zero if the model is unknown.
	ModelID   int64
	Kind      UsageKind
	TokensIn  int64
	TokensOut int64
}

// UsageTotals sums up model calls.
type UsageTotals struct {
	Requests  int64
	TokensIn  int64
	TokensOut int64
}

// UserUsage is how much one user made the bot call models.
type UserUsage struct {
	UserID   int64
	Name     string
	Requests int64
	Tokens   int64
}

// ModelOption is a model a user may bind to a chat, as shown in pickers.
// It deliberately carries no provider URL or key.
type ModelOption struct {
	Model
	ProviderName string
	ProviderKind ProviderKind
	OwnerID      int64
	OwnerName    string
	Shared       bool
}

// Secret is a sensitive string that never shows up in logs or formatted output.
type Secret string

// String implements fmt.Stringer.
func (Secret) String() string { return "[REDACTED]" }

// GoString implements fmt.GoStringer.
func (Secret) GoString() string { return "[REDACTED]" }

// LogValue implements slog.LogValuer.
func (Secret) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

// Reveal returns the underlying value.
func (s Secret) Reveal() string { return string(s) }
