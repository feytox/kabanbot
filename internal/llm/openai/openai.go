// Package openai implements llm.Client for OpenAI-compatible APIs, including OpenRouter.
package openai

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"net/url"
	"strconv"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/shared"

	"github.com/feytox/kabanbot/internal/llm"
)

// OpenRouterBaseURL is the OpenAI-compatible endpoint of OpenRouter.
const OpenRouterBaseURL = "https://openrouter.ai/api/v1"

// Config configures a Client.
type Config struct {
	// Name is the provider name shown in errors. Empty means "OpenAI".
	Name    string
	APIKey  string
	BaseURL string
	// Headers are sent with every request, e.g. OpenRouter attribution headers.
	Headers map[string]string
	// HTTPClient overrides the default HTTP client.
	HTTPClient *http.Client
}

// Client is an llm.Client backed by the Chat Completions API.
type Client struct {
	api  openai.Client
	name string
}

var _ llm.Client = (*Client)(nil)

// New creates a Client.
func New(cfg Config) *Client {
	// Retries are left to llm.Retrying, which does them the same way for every provider.
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey), option.WithMaxRetries(0)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	if cfg.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(cfg.HTTPClient))
	}
	for k, v := range cfg.Headers {
		opts = append(opts, option.WithHeader(k, v))
	}
	name := cfg.Name
	if name == "" {
		name = "OpenAI"
	}
	return &Client{api: openai.NewClient(opts...), name: name}
}

// NewOpenRouter creates a Client for OpenRouter. It fills in the OpenRouter base URL and attribution headers.
func NewOpenRouter(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = OpenRouterBaseURL
	}
	cfg.Name = "OpenRouter"
	cfg.Headers = map[string]string{
		"HTTP-Referer": "https://github.com/feytox/kabanbot",
		"X-Title":      "kabanbot",
	}
	return New(cfg)
}

// Complete implements llm.Client.
func (c *Client) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	resp, err := c.api.Chat.Completions.New(ctx, params(req))
	if err != nil {
		return llm.Response{}, c.wrap(err)
	}
	if len(resp.Choices) == 0 {
		return llm.Response{}, errors.New("openai: empty response")
	}
	msg := resp.Choices[0].Message
	out := llm.Response{
		Text: msg.Content,
		Usage: llm.Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	}
	for _, tc := range msg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, llm.ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
	}
	return out, nil
}

// Stream implements llm.Client.
func (c *Client) Stream(ctx context.Context, req llm.Request) iter.Seq2[llm.Chunk, error] {
	return func(yield func(llm.Chunk, error) bool) {
		p := params(req)
		p.StreamOptions.IncludeUsage = openai.Bool(true)
		stream := c.api.Chat.Completions.NewStreaming(ctx, p)
		defer func() { _ = stream.Close() }()

		// Tool calls arrive in pieces, keyed by their index.
		var calls []llm.ToolCall
		var usage *llm.Usage
		for stream.Next() {
			chunk := stream.Current()
			if u := chunk.Usage; u.PromptTokens > 0 || u.CompletionTokens > 0 {
				usage = &llm.Usage{InputTokens: u.PromptTokens, OutputTokens: u.CompletionTokens}
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			delta := chunk.Choices[0].Delta
			for _, tc := range delta.ToolCalls {
				i := int(tc.Index)
				for len(calls) <= i {
					calls = append(calls, llm.ToolCall{})
				}
				calls[i].ID += tc.ID
				calls[i].Name += tc.Function.Name
				calls[i].Arguments += tc.Function.Arguments
			}
			if delta.Content != "" && !yield(llm.Chunk{Text: delta.Content}, nil) {
				return
			}
		}
		if err := stream.Err(); err != nil {
			yield(llm.Chunk{}, c.wrap(err))
			return
		}
		yield(llm.Chunk{ToolCalls: calls, Usage: usage}, nil)
	}
}

func params(req llm.Request) openai.ChatCompletionNewParams {
	msgs := make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, openai.SystemMessage(req.System))
	}
	for _, m := range req.Messages {
		switch m.Role {
		case llm.RoleAssistant:
			msg := openai.ChatCompletionAssistantMessageParam{}
			if m.Content != "" {
				msg.Content.OfString = openai.String(m.Content)
			}
			for _, tc := range m.ToolCalls {
				msg.ToolCalls = append(msg.ToolCalls, openai.ChatCompletionMessageToolCallUnionParam{
					OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
						ID:       tc.ID,
						Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{Name: tc.Name, Arguments: tc.Arguments},
					},
				})
			}
			msgs = append(msgs, openai.ChatCompletionMessageParamUnion{OfAssistant: &msg})
		case llm.RoleTool:
			id := ""
			if m.ToolCall != nil {
				id = m.ToolCall.ID
			}
			msgs = append(msgs, openai.ToolMessage(m.Content, id))
		default:
			msgs = append(msgs, openai.UserMessage(m.Content))
		}
	}

	p := openai.ChatCompletionNewParams{Model: req.Model, Messages: msgs}
	if req.Temperature != nil {
		p.Temperature = openai.Float(*req.Temperature)
	}
	if req.MaxTokens > 0 {
		// max_tokens is understood by far more OpenAI-compatible servers than max_completion_tokens.
		p.MaxTokens = openai.Int(req.MaxTokens)
	}
	for _, t := range req.Tools {
		p.Tools = append(p.Tools, openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        t.Name,
			Description: openai.String(t.Description),
			Parameters:  shared.FunctionParameters(t.Parameters),
		}))
	}
	return p
}

// wrap describes an API or network failure as *llm.Error.
func (c *Client) wrap(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if apiErr, ok := errors.AsType[*openai.Error](err); ok {
		out := &llm.Error{Provider: c.name, Status: apiErr.StatusCode, Message: apiErr.Message, Err: err}
		if resp := apiErr.Response; resp != nil {
			out.RetryAfter = llm.ParseRetryAfter(resp.Header)
			if out.Message == "" {
				out.Message = resp.Status
			}
		}
		return out
	}
	if serr, ok := errors.AsType[*ssestream.StreamError](err); ok {
		return c.streamError(serr)
	}
	if _, ok := errors.AsType[*url.Error](err); ok {
		return &llm.Error{Provider: c.name, Err: err}
	}
	return fmt.Errorf("openai: chat completion: %w", err)
}

// streamError describes an error sent inside a stream that began with HTTP 200, as OpenRouter
// does when the upstream provider fails, e.g. {"error":{"code":503,"message":"…overloaded"}}.
func (c *Client) streamError(serr *ssestream.StreamError) *llm.Error {
	var body struct {
		Error struct {
			Code    jsontext.Value `json:"code"`
			Message string         `json:"message"`
		} `json:"error"`
	}
	out := &llm.Error{Provider: c.name, Message: serr.Message, Err: serr}
	if err := json.Unmarshal(serr.Event.Data, &body); err != nil {
		// Something went wrong on the server side mid-answer; worth another try.
		out.Status = http.StatusInternalServerError
		return out
	}
	out.Message = cmp.Or(body.Error.Message, out.Message)
	if code, err := strconv.Atoi(string(body.Error.Code)); err == nil && code >= 400 && code < 600 {
		out.Status = code
	} else {
		// OpenAI sends names like "server_error" instead of statuses.
		out.Status = http.StatusInternalServerError
	}
	return out
}
