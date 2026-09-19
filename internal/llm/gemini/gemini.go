// Package gemini implements llm.Client for the Gemini API.
package gemini

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"net/url"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/feytox/kabanbot/internal/llm"
)

// Client is an llm.Client backed by the Gemini API.
type Client struct {
	api *genai.Client
}

var _ llm.Client = (*Client)(nil)

// New creates a Client. baseURL may be empty to use the default endpoint,
// and httpClient may be nil to use the default HTTP client.
func New(ctx context.Context, apiKey, baseURL string, httpClient *http.Client) (*Client, error) {
	api, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:      apiKey,
		Backend:     genai.BackendGeminiAPI,
		HTTPOptions: genai.HTTPOptions{BaseURL: baseURL},
		HTTPClient:  httpClient,
	})
	if err != nil {
		return nil, fmt.Errorf("gemini: new client: %w", err)
	}
	return &Client{api: api}, nil
}

// Complete implements llm.Client.
func (c *Client) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	contents, cfg, err := request(req)
	if err != nil {
		return llm.Response{}, err
	}
	resp, err := c.api.Models.GenerateContent(ctx, req.Model, contents, cfg)
	if err != nil {
		return llm.Response{}, wrap(err)
	}
	var out llm.Response
	var chunk llm.Chunk
	collect(resp, &chunk)
	out.Text, out.ToolCalls = chunk.Text, chunk.ToolCalls
	if chunk.Usage != nil {
		out.Usage = *chunk.Usage
	}
	if out.Text == "" && len(out.ToolCalls) == 0 {
		return llm.Response{}, errors.New("gemini: empty response")
	}
	return out, nil
}

// Stream implements llm.Client.
func (c *Client) Stream(ctx context.Context, req llm.Request) iter.Seq2[llm.Chunk, error] {
	return func(yield func(llm.Chunk, error) bool) {
		contents, cfg, err := request(req)
		if err != nil {
			yield(llm.Chunk{}, err)
			return
		}
		var last llm.Chunk
		for resp, err := range c.api.Models.GenerateContentStream(ctx, req.Model, contents, cfg) {
			if err != nil {
				yield(llm.Chunk{}, wrap(err))
				return
			}
			var chunk llm.Chunk
			collect(resp, &chunk)
			last.ToolCalls = append(last.ToolCalls, chunk.ToolCalls...)
			if chunk.Usage != nil {
				last.Usage = chunk.Usage
			}
			if chunk.Text != "" && !yield(llm.Chunk{Text: chunk.Text}, nil) {
				return
			}
		}
		yield(last, nil)
	}
}

// collect copies the visible text, tool calls and usage of resp into out. Thoughts are skipped.
func collect(resp *genai.GenerateContentResponse, out *llm.Chunk) {
	if u := resp.UsageMetadata; u != nil {
		out.Usage = &llm.Usage{InputTokens: int64(u.PromptTokenCount), OutputTokens: int64(u.CandidatesTokenCount)}
	}
	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return
	}
	for _, part := range resp.Candidates[0].Content.Parts {
		switch {
		case part.FunctionCall != nil:
			args, _ := json.Marshal(part.FunctionCall.Args)
			out.ToolCalls = append(out.ToolCalls, llm.ToolCall{
				ID:        part.FunctionCall.ID,
				Name:      part.FunctionCall.Name,
				Arguments: string(args),
				Signature: part.ThoughtSignature,
			})
		case part.Text != "" && !part.Thought:
			out.Text += part.Text
		}
	}
}

func request(req llm.Request) ([]*genai.Content, *genai.GenerateContentConfig, error) {
	contents := make([]*genai.Content, 0, len(req.Messages))
	for _, m := range req.Messages {
		switch m.Role {
		case llm.RoleAssistant:
			var parts []*genai.Part
			if m.Content != "" {
				parts = append(parts, genai.NewPartFromText(m.Content))
			}
			for _, tc := range m.ToolCalls {
				var args map[string]any
				if tc.Arguments != "" {
					if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
						return nil, nil, fmt.Errorf("gemini: tool call arguments: %w", err)
					}
				}
				part := genai.NewPartFromFunctionCall(tc.Name, args)
				part.FunctionCall.ID = tc.ID
				part.ThoughtSignature = tc.Signature
				parts = append(parts, part)
			}
			contents = append(contents, genai.NewContentFromParts(parts, genai.RoleModel))
		case llm.RoleTool:
			var call llm.ToolCall
			if m.ToolCall != nil {
				call = *m.ToolCall
			}
			part := genai.NewPartFromFunctionResponse(call.Name, map[string]any{"result": m.Content})
			part.FunctionResponse.ID = call.ID
			// Answers to calls made together go back together.
			if n := len(contents); n > 0 && isToolResponse(contents[n-1]) {
				contents[n-1].Parts = append(contents[n-1].Parts, part)
			} else {
				contents = append(contents, genai.NewContentFromParts([]*genai.Part{part}, genai.RoleUser))
			}
		default:
			contents = append(contents, genai.NewContentFromText(m.Content, genai.RoleUser))
		}
	}

	cfg := &genai.GenerateContentConfig{MaxOutputTokens: int32(min(req.MaxTokens, 1<<31-1))}
	if req.System != "" {
		cfg.SystemInstruction = genai.NewContentFromText(req.System, genai.RoleUser)
	}
	if req.Temperature != nil {
		cfg.Temperature = new(float32(*req.Temperature))
	}
	if len(req.Tools) > 0 {
		decls := make([]*genai.FunctionDeclaration, len(req.Tools))
		for i, t := range req.Tools {
			decls[i] = &genai.FunctionDeclaration{Name: t.Name, Description: t.Description, ParametersJsonSchema: t.Parameters}
		}
		cfg.Tools = []*genai.Tool{{FunctionDeclarations: decls}}
	}
	return contents, cfg, nil
}

func isToolResponse(c *genai.Content) bool {
	return len(c.Parts) > 0 && c.Parts[0].FunctionResponse != nil
}

const providerName = "Gemini"

// wrap describes an API or network failure as *llm.Error.
func wrap(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if apiErr, ok := errors.AsType[genai.APIError](err); ok {
		return &llm.Error{
			Provider:   providerName,
			Status:     apiErr.Code,
			Message:    apiErr.Message,
			RetryAfter: retryDelay(apiErr.Details),
			Err:        err,
		}
	}
	if _, ok := errors.AsType[*url.Error](err); ok {
		return &llm.Error{Provider: providerName, Err: err}
	}
	return fmt.Errorf("gemini: generate content: %w", err)
}

// retryDelay finds the wait Google suggests in a google.rpc.RetryInfo error detail,
// which rate-limit errors carry, e.g. {"@type": ".../google.rpc.RetryInfo", "retryDelay": "37s"}.
func retryDelay(details []map[string]any) time.Duration {
	for _, d := range details {
		if t, _ := d["@type"].(string); !strings.HasSuffix(t, "google.rpc.RetryInfo") {
			continue
		}
		if s, ok := d["retryDelay"].(string); ok {
			if v, err := time.ParseDuration(s); err == nil {
				return v
			}
		}
	}
	return 0
}
