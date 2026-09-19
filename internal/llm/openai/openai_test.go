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

func TestStreamWithToolCalls(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range []string{
			`{"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"При"}}]}`,
			`{"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"вет"}}]}`,
			`{"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"set_personality","arguments":"{\"te"}}]}}]}`,
			`{"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"xt\":\"кабан\"}"}}]}}]}`,
			`{"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`,
		} {
			_, _ = io.WriteString(w, "data: "+chunk+"\n\n")
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := New(Config{APIKey: "k", BaseURL: srv.URL})
	call := llm.ToolCall{ID: "call_0", Name: "get_personality", Arguments: "{}"}
	resp, err := llm.Collect(c.Stream(t.Context(), llm.Request{
		Model: "m",
		Tools: []llm.Tool{{Name: "set_personality", Description: "d", Parameters: map[string]any{"type": "object"}}},
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "hi"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}},
			{Role: llm.RoleTool, Content: "кабан", ToolCall: &call},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := llm.ToolCall{ID: "call_1", Name: "set_personality", Arguments: `{"text":"кабан"}`}
	if resp.Text != "Привет" || len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != want.Name ||
		resp.ToolCalls[0].ID != want.ID || resp.ToolCalls[0].Arguments != want.Arguments || resp.Usage.OutputTokens != 3 {
		t.Fatalf("resp = %+v", resp)
	}

	msgs, _ := body["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %v", body["messages"])
	}
	if calls, _ := msgs[1].(map[string]any)["tool_calls"].([]any); len(calls) != 1 {
		t.Errorf("assistant message = %v", msgs[1])
	}
	if m := msgs[2].(map[string]any); m["role"] != "tool" || m["tool_call_id"] != "call_0" {
		t.Errorf("tool message = %v", m)
	}
	if tools, _ := body["tools"].([]any); len(tools) != 1 {
		t.Errorf("tools = %v", body["tools"])
	}
}

func TestStreamErrorInsideStream(t *testing.T) {
	for body, wantStatus := range map[string]int{
		`{"error":{"code":503,"message":"Upstream error from Nvidia: Service temporarily overloaded","metadata":{"error_type":"provider_overloaded"}}}`: 503,
		`{"error":{"code":"server_error","message":"The server had an error"}}`:                                                                         500,
		`{"error":{"code":400,"message":"bad"}}`: 400,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: "+body+"\n\n")
		}))
		c := NewOpenRouter(Config{APIKey: "k", BaseURL: srv.URL})
		_, err := llm.Collect(c.Stream(t.Context(), llm.Request{Model: "m"}))
		srv.Close()
		perr, ok := errors.AsType[*llm.Error](err)
		if !ok || perr.Status != wantStatus || perr.Message == "" || perr.Temporary() != (wantStatus >= 500) {
			t.Errorf("%s: err = %#v", body, err)
		}
	}
}
