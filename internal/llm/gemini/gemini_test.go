package gemini

import (
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/feytox/kabanbot/internal/llm"
)

func TestComplete(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models/gemini-test:generateContent") {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("X-Goog-Api-Key") != "key" {
			t.Errorf("api key header = %q", r.Header.Get("X-Goog-Api-Key"))
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"привет"}]}}],
			"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":2}}`)
	}))
	defer srv.Close()

	c, err := New(t.Context(), "key", srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	temp := 0.25
	resp, err := c.Complete(t.Context(), llm.Request{
		Model:       "gemini-test",
		System:      "sys",
		Messages:    []llm.Message{{Role: llm.RoleUser, Content: "hi"}, {Role: llm.RoleAssistant, Content: "yo"}},
		Temperature: &temp,
		MaxTokens:   64,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "привет" || resp.Usage != (llm.Usage{InputTokens: 7, OutputTokens: 2}) {
		t.Errorf("resp = %+v", resp)
	}

	contents, _ := got["contents"].([]any)
	if len(contents) != 2 || contents[1].(map[string]any)["role"] != "model" {
		t.Errorf("contents = %v", got["contents"])
	}
	if _, ok := got["systemInstruction"]; !ok {
		t.Errorf("no systemInstruction in %v", got)
	}
	cfg, _ := got["generationConfig"].(map[string]any)
	if cfg["maxOutputTokens"] != 64.0 || cfg["temperature"] != 0.25 {
		t.Errorf("generationConfig = %v", cfg)
	}
}

func TestCompleteRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"code":429,"message":"Quota exceeded","status":"RESOURCE_EXHAUSTED",
			"details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"37s"}]}}`)
	}))
	defer srv.Close()

	c, err := New(t.Context(), "key", srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Complete(t.Context(), llm.Request{Model: "gemini-test"})
	perr, ok := errors.AsType[*llm.Error](err)
	if !ok || perr.Status != 429 || perr.Message != "Quota exceeded" || perr.RetryAfter != 37*time.Second || !perr.Temporary() {
		t.Fatalf("err = %#v", err)
	}
}

func TestStreamWithToolCalls(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ":streamGenerateContent") {
			t.Errorf("path = %s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range []string{
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"хм","thought":true}]}}]}`,
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"При"}]}}]}`,
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"вет"}]}}]}`,
			`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"set_personality","args":{"text":"кабан"}},"thoughtSignature":"c2ln"}]}}],
			  "usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":3}}`,
		} {
			_, _ = io.WriteString(w, "data: "+strings.ReplaceAll(chunk, "\n", "")+"\n\n")
		}
	}))
	defer srv.Close()

	c, err := New(t.Context(), "key", srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	prev := llm.ToolCall{ID: "c0", Name: "get_personality", Arguments: "{}", Signature: []byte("prev")}
	resp, err := llm.Collect(c.Stream(t.Context(), llm.Request{
		Model: "gemini-test",
		Tools: []llm.Tool{{Name: "set_personality", Description: "d", Parameters: map[string]any{"type": "object"}}},
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "hi"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{prev}},
			{Role: llm.RoleTool, Content: "кабан", ToolCall: &prev},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "Привет" || len(resp.ToolCalls) != 1 || resp.Usage.OutputTokens != 3 {
		t.Fatalf("resp = %+v", resp)
	}
	if tc := resp.ToolCalls[0]; tc.Name != "set_personality" || tc.Arguments != `{"text":"кабан"}` || string(tc.Signature) != "sig" {
		t.Errorf("tool call = %+v", tc)
	}

	contents, _ := body["contents"].([]any)
	if len(contents) != 3 {
		t.Fatalf("contents = %v", body["contents"])
	}
	call := contents[1].(map[string]any)["parts"].([]any)[0].(map[string]any)
	if call["functionCall"] == nil || call["thoughtSignature"] != "cHJldg==" {
		t.Errorf("model turn must replay the call with its signature: %v", call)
	}
	answer := contents[2].(map[string]any)["parts"].([]any)[0].(map[string]any)
	if fr, _ := answer["functionResponse"].(map[string]any); fr["name"] != "get_personality" {
		t.Errorf("tool answer = %v", answer)
	}
}
