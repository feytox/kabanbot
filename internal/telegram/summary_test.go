package telegram

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/feytox/kabanbot/internal/llm"
)

func TestDescribeLLMError(t *testing.T) {
	overloaded := fmt.Errorf("summarize: %w", &llm.Error{
		Provider: "Gemini", Status: 503, Attempts: 5,
		Message: "This model is currently experiencing high demand.",
	})
	got := describeLLMError(overloaded)
	for _, want := range []string{"Gemini: модель перегружена (503)", "high demand", "Попыток: 5", "через пару минут"} {
		if !strings.Contains(got, want) {
			t.Errorf("description %q lacks %q", got, want)
		}
	}

	if got := describeLLMError(&llm.Error{Provider: "OpenRouter", Status: 401, Message: "bad key"}); !strings.Contains(got, "ключ") ||
		strings.Contains(got, "Попыток") {
		t.Errorf("401: %q", got)
	}
	if got := describeLLMError(errors.New("db locked")); got != "" {
		t.Errorf("unknown error described as %q", got)
	}
}
