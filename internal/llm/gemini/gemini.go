// Package gemini implements llm.Client for the Gemini API.
package gemini

import (
	"context"
	"errors"
	"fmt"
	"net/http"

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
	contents := make([]*genai.Content, 0, len(req.Messages))
	for _, m := range req.Messages {
		role := genai.Role(genai.RoleUser)
		if m.Role == llm.RoleAssistant {
			role = genai.RoleModel
		}
		contents = append(contents, genai.NewContentFromText(m.Content, role))
	}

	cfg := &genai.GenerateContentConfig{MaxOutputTokens: int32(min(req.MaxTokens, 1<<31-1))}
	if req.System != "" {
		cfg.SystemInstruction = genai.NewContentFromText(req.System, genai.RoleUser)
	}
	if req.Temperature != nil {
		cfg.Temperature = new(float32(*req.Temperature))
	}

	resp, err := c.api.Models.GenerateContent(ctx, req.Model, contents, cfg)
	if err != nil {
		return llm.Response{}, fmt.Errorf("gemini: generate content: %w", err)
	}
	text := resp.Text()
	if text == "" {
		return llm.Response{}, errors.New("gemini: empty response")
	}
	out := llm.Response{Text: text}
	if u := resp.UsageMetadata; u != nil {
		out.Usage = llm.Usage{InputTokens: int64(u.PromptTokenCount), OutputTokens: int64(u.CandidatesTokenCount)}
	}
	return out, nil
}
