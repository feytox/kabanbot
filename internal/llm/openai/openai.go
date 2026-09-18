// Package openai implements llm.Client for OpenAI-compatible APIs, including OpenRouter.
package openai

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/feytox/kabanbot/internal/llm"
)

// OpenRouterBaseURL is the OpenAI-compatible endpoint of OpenRouter.
const OpenRouterBaseURL = "https://openrouter.ai/api/v1"

// Config configures a Client.
type Config struct {
	APIKey  string
	BaseURL string
	// Headers are sent with every request, e.g. OpenRouter attribution headers.
	Headers map[string]string
	// HTTPClient overrides the default HTTP client.
	HTTPClient *http.Client
}

// Client is an llm.Client backed by the Chat Completions API.
type Client struct {
	api openai.Client
}

var _ llm.Client = (*Client)(nil)

// New creates a Client.
func New(cfg Config) *Client {
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	if cfg.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(cfg.HTTPClient))
	}
	for k, v := range cfg.Headers {
		opts = append(opts, option.WithHeader(k, v))
	}
	return &Client{api: openai.NewClient(opts...)}
}

// NewOpenRouter creates a Client for OpenRouter. It fills in the OpenRouter base URL and attribution headers.
func NewOpenRouter(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = OpenRouterBaseURL
	}
	cfg.Headers = map[string]string{
		"HTTP-Referer": "https://github.com/feytox/kabanbot",
		"X-Title":      "kabanbot",
	}
	return New(cfg)
}

// Complete implements llm.Client.
func (c *Client) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	msgs := make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, openai.SystemMessage(req.System))
	}
	for _, m := range req.Messages {
		switch m.Role {
		case llm.RoleAssistant:
			msgs = append(msgs, openai.AssistantMessage(m.Content))
		default:
			msgs = append(msgs, openai.UserMessage(m.Content))
		}
	}

	params := openai.ChatCompletionNewParams{Model: req.Model, Messages: msgs}
	if req.Temperature != nil {
		params.Temperature = openai.Float(*req.Temperature)
	}
	if req.MaxTokens > 0 {
		// max_tokens is understood by far more OpenAI-compatible servers than max_completion_tokens.
		params.MaxTokens = openai.Int(req.MaxTokens)
	}

	resp, err := c.api.Chat.Completions.New(ctx, params)
	if err != nil {
		return llm.Response{}, fmt.Errorf("openai: chat completion: %w", err)
	}
	if len(resp.Choices) == 0 {
		return llm.Response{}, errors.New("openai: empty response")
	}
	return llm.Response{
		Text: resp.Choices[0].Message.Content,
		Usage: llm.Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	}, nil
}
