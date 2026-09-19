package openai

import (
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/feytox/kabanbot/internal/llm"
)

func TestComplete(t *testing.T) {
	var got map[string]any
	var headers http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		headers = r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"x","object":"chat.completion","created":1,"model":"m",
			"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hello"}}],
			"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`)
	}))
	defer srv.Close()

	c := NewOpenRouter(Config{APIKey: "sk-test", BaseURL: srv.URL})
	temp := 0.5
	resp, err := c.Complete(t.Context(), llm.Request{
		Model:       "vendor/model",
		System:      "sys",
		Messages:    []llm.Message{{Role: llm.RoleUser, Content: "hi"}, {Role: llm.RoleAssistant, Content: "yo"}},
		Temperature: &temp,
		MaxTokens:   100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hello" || resp.Usage != (llm.Usage{InputTokens: 10, OutputTokens: 3}) {
		t.Errorf("resp = %+v", resp)
	}

	if headers.Get("Authorization") != "Bearer sk-test" || headers.Get("X-Title") != "kabanbot" {
		t.Errorf("headers = %v", headers)
	}
	if got["model"] != "vendor/model" || got["max_tokens"] != 100.0 || got["temperature"] != 0.5 {
		t.Errorf("body = %v", got)
	}
	msgs, _ := got["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %v", got["messages"])
	}
	roles := []string{"system", "user", "assistant"}
	for i, m := range msgs {
		if r := m.(map[string]any)["role"]; r != roles[i] {
			t.Errorf("message %d role = %v, want %s", i, r, roles[i])
		}
	}
}

func TestCompleteHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"message":"bad key"}}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New(Config{APIKey: "bad", BaseURL: srv.URL})
	_, err := c.Complete(t.Context(), llm.Request{Model: "m"})
	perr, ok := errors.AsType[*llm.Error](err)
	if !ok || perr.Status != http.StatusUnauthorized || perr.Message != "bad key" || perr.Temporary() {
		t.Fatalf("err = %#v", err)
	}
}

func TestCompleteRateLimitIsNotRetriedBySDK(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Retry-After", "7")
		http.Error(w, `{"error":{"message":"slow down"}}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewOpenRouter(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := c.Complete(t.Context(), llm.Request{Model: "m"})
	perr, ok := errors.AsType[*llm.Error](err)
	if !ok || perr.Provider != "OpenRouter" || perr.RetryAfter != 7*time.Second || !perr.Temporary() {
		t.Fatalf("err = %#v", err)
	}
	if calls != 1 {
		t.Errorf("the SDK made %d calls; retries belong to llm.Retrying", calls)
	}
}
