package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"kabanbot/config"
	"kabanbot/services"
)

func TestLLMService_GenerateGreeting(t *testing.T) {
	expectedAPIKey := "test-api-key-123"
	expectedModel := "test-model-abc"
	expectedGreeting := "Hello, Alice! Welcome to Kabanbot from LLM!"

	// 1. Create a mock HTTP server to simulate the LLM API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		authHeader := r.Header.Get("Authorization")
		expectedAuth := "Bearer " + expectedAPIKey
		if authHeader != expectedAuth {
			t.Errorf("expected auth header %q, got %q", expectedAuth, authHeader)
		}

		// Verify request content
		var reqBody map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		if err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		model, ok := reqBody["model"].(string)
		if !ok || model != expectedModel {
			t.Errorf("expected model %q, got %q", expectedModel, model)
		}

		// Send mock response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		response := map[string]interface{}{
			"id":     "chatcmpl-123",
			"object": "chat.completion",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]string{
						"role":    "assistant",
						"content": expectedGreeting,
					},
					"finish_reason": "stop",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// 2. Build config pointing to the mock server
	cfg := &config.Config{
		BotToken:   "dummy-token",
		LLMAPIKey:  expectedAPIKey,
		LLMBaseURL: server.URL,
		LLMModel:   expectedModel,
	}

	// 3. Initialize service and call method
	svc := services.NewLLMService(cfg)
	greeting, err := svc.GenerateGreeting(context.Background(), "Alice")
	if err != nil {
		t.Fatalf("GenerateGreeting failed: %v", err)
	}

	if greeting != expectedGreeting {
		t.Errorf("expected greeting %q, got %q", expectedGreeting, greeting)
	}
}
