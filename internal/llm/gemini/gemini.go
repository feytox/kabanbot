// Package gemini implements llm.Client for the Gemini API.
package gemini

import (
	"context"
	"errors"
	"fmt"
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
		return llm.Response{}, wrap(err)
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

const providerName = "Gemini"

// wrap describes an API or network failure as *llm.Error.
func wrap(err error) error {
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
