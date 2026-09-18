// Package domain holds the core entities shared across the application.
package domain

import (
	"errors"
	"log/slog"
	"time"
)

// ErrNotFound is returned by stores when a requested entity does not exist.
var ErrNotFound = errors.New("not found")

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
	Shared  bool
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
