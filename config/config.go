package config

import (
	"os"

	"github.com/joho/godotenv"
)

// Config holds all configuration parameters for the application.
type Config struct {
	BotToken   string
	LLMAPIKey  string
	LLMBaseURL string
	LLMModel   string
}

// LoadConfig loads the configuration from the environment and optional .env file.
func LoadConfig() (*Config, error) {
	// Attempt to load .env file; ignore error if it's missing (e.g. in container environments)
	_ = godotenv.Load()

	botToken := os.Getenv("BOT_TOKEN")

	return &Config{
		BotToken:   botToken,
		LLMAPIKey:  os.Getenv("LLM_API_KEY"),
		LLMBaseURL: getEnv("LLM_BASE_URL", "https://api.openai.com/v1"),
		LLMModel:   getEnv("LLM_MODEL", "gpt-4o-mini"),
	}, nil
}

func getEnv(key, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}
