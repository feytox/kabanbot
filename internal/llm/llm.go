// Package llm defines a provider-agnostic interface for chat completion models.
package llm

import (
	"context"

	"github.com/feytox/kabanbot/internal/domain"
)

// Role is the author of a message in a conversation.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is a single conversation turn.
type Message struct {
	Role    Role
	Content string
}

// Request is a chat completion request.
type Request struct {
	Model       string
	System      string
	Messages    []Message
	Temperature *float64
	// MaxTokens limits the response length. Zero means the provider default.
	MaxTokens int64
}

// Usage is the token accounting reported by the provider.
type Usage struct {
	InputTokens  int64
	OutputTokens int64
}

// Response is a chat completion result.
type Response struct {
	Text  string
	Usage Usage
}

// Client is a chat completion model backend.
type Client interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

// Target is a ready-to-use client together with the model it should be called with.
type Target struct {
	Client Client
	Model  domain.Model
}

// Request builds a Request for this target's model.
func (t Target) Request(system string, msgs ...Message) Request {
	return Request{
		Model:       t.Model.Name,
		System:      system,
		Messages:    msgs,
		Temperature: t.Model.Params.Temperature,
		MaxTokens:   t.Model.Params.MaxTokens,
	}
}
