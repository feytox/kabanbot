package gemini

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

	c, err := New(t.Context(), "key", srv.URL)
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
