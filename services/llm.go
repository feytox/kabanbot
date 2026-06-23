package services

import (
	"context"
	"fmt"

	"github.com/sashabaranov/go-openai"
	"kabanbot/config"
)

// LLMService manages interactions with the configured LLM provider.
type LLMService struct {
	client *openai.Client
	model  string
}

// NewLLMService instantiates an LLMService configured with settings from config.
func NewLLMService(cfg *config.Config) *LLMService {
	openaiCfg := openai.DefaultConfig(cfg.LLMAPIKey)
	if cfg.LLMBaseURL != "" {
		openaiCfg.BaseURL = cfg.LLMBaseURL
	}

	client := openai.NewClientWithConfig(openaiCfg)
	return &LLMService{
		client: client,
		model:  cfg.LLMModel,
	}
}

// GenerateGreeting generates a dynamic greeting using the LLM.
func (s *LLMService) GenerateGreeting(ctx context.Context, firstName string) (string, error) {
	resp, err := s.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: s.model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "You are a helpful and friendly assistant. Generate a warm, welcoming, and slightly playful greeting message for a Telegram user in response to their /start command. Address them by their first name.",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: fmt.Sprintf("My name is %s. Start the conversation!", firstName),
				},
			},
		},
	)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices returned from LLM")
	}

	return resp.Choices[0].Message.Content, nil
}
