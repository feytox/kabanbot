// Package llm defines a provider-agnostic interface for chat completion models.
package llm

import (
	"context"
	"iter"

	"github.com/feytox/kabanbot/internal/domain"
)

// Role is the author of a message in a conversation.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	// RoleTool carries a tool's result back to the model.
	RoleTool Role = "tool"
)

// Message is a single conversation turn.
type Message struct {
	Role    Role
	Content string
	// ToolCalls are the tools an assistant message asked to run.
	ToolCalls []ToolCall
	// ToolCall is the call a RoleTool message answers.
	ToolCall *ToolCall
}

// Tool is a function the model may ask to run.
type Tool struct {
	Name        string
	Description string
	// Parameters is a JSON Schema object describing the arguments.
	Parameters map[string]any
}

// ToolCall is the model's request to run a tool.
type ToolCall struct {
	ID   string
	Name string
	// Arguments is a JSON object.
	Arguments string
	// Signature is opaque provider state that must be sent back with the call,
	// e.g. Gemini's thought signature.
	Signature []byte
}

// Request is a chat completion request.
type Request struct {
	Model       string
	System      string
	Messages    []Message
	Temperature *float64
	// MaxTokens limits the response length. Zero means the provider default.
	MaxTokens int64
	// Tools the model may call. The caller runs them and sends the results back.
	Tools []Tool
}

// Usage is the token accounting reported by the provider.
type Usage struct {
	InputTokens  int64
	OutputTokens int64
}

// Response is a chat completion result.
type Response struct {
	Text      string
	ToolCalls []ToolCall
	Usage     Usage
}

// Chunk is a piece of a streamed response. Text chunks come first; the last chunk
// carries the tool calls and the usage.
type Chunk struct {
	Text      string
	ToolCalls []ToolCall
	Usage     *Usage
}

// Client is a chat completion model backend.
type Client interface {
	Complete(ctx context.Context, req Request) (Response, error)
	// Stream is Complete that yields the response as it is generated.
	Stream(ctx context.Context, req Request) iter.Seq2[Chunk, error]
}

// Collect reads a stream into a Response.
func Collect(stream iter.Seq2[Chunk, error]) (Response, error) {
	var out Response
	for c, err := range stream {
		if err != nil {
			return Response{}, err
		}
		out.Text += c.Text
		out.ToolCalls = append(out.ToolCalls, c.ToolCalls...)
		if c.Usage != nil {
			out.Usage = *c.Usage
		}
	}
	return out, nil
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

// Single turns a finished response into a one-chunk stream, e.g. for clients that cannot stream.
func Single(resp Response, err error) iter.Seq2[Chunk, error] {
	return func(yield func(Chunk, error) bool) {
		if err != nil {
			yield(Chunk{}, err)
			return
		}
		if resp.Text != "" && !yield(Chunk{Text: resp.Text}, nil) {
			return
		}
		yield(Chunk{ToolCalls: resp.ToolCalls, Usage: &resp.Usage}, nil)
	}
}
