// Package config loads application configuration from the environment.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/caarlos0/env/v11"

	"github.com/feytox/kabanbot/internal/domain"
)

// Config is the full application configuration.
type Config struct {
	BotToken string `env:"BOT_TOKEN,required,notEmpty"`
	// OwnerID is the Telegram user ID of the bot owner, whose providers may reach local servers.
	OwnerID int64 `env:"OWNER_ID"`
	// AllowedGroups restricts the bot to these chats. Empty means all chats are allowed.
	AllowedGroups []int64 `env:"ALLOWED_GROUPS" envSeparator:","`

	DBPath    string `env:"DB_PATH" envDefault:"data/messages.db"`
	CacheSize int    `env:"CACHE_SIZE" envDefault:"1000"`

	// MasterKey is a base64-encoded 32-byte key used to encrypt provider API keys at rest.
	MasterKey string `env:"MASTER_KEY"`

	// HTTPAddr serves /healthz.
	HTTPAddr string     `env:"HTTP_ADDR" envDefault:":8080"`
	LogLevel slog.Level `env:"LOG_LEVEL" envDefault:"info"`

	LLM DefaultLLM `envPrefix:"LLM_"`
}

// DefaultLLM is the model used by chats that have no model bound to them.
type DefaultLLM struct {
	Provider domain.ProviderKind `env:"PROVIDER" envDefault:"openai"`
	BaseURL  string              `env:"BASE_URL"`
	APIKey   string              `env:"API_KEY"`
	Model    string              `env:"MODEL"`
}

// Load reads and validates the configuration.
func Load() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("parse env: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// MasterKeyBytes decodes MasterKey. It returns nil if no key is configured.
func (c Config) MasterKeyBytes() ([]byte, error) {
	if c.MasterKey == "" {
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(c.MasterKey)
	if err != nil {
		return nil, fmt.Errorf("MASTER_KEY: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("MASTER_KEY: want 32 bytes, got %d", len(key))
	}
	return key, nil
}

func (c Config) validate() error {
	var errs []error
	if c.CacheSize <= 0 {
		errs = append(errs, errors.New("CACHE_SIZE must be positive"))
	}
	if !c.LLM.Provider.Valid() {
		errs = append(errs, fmt.Errorf("LLM_PROVIDER: unknown provider %q", c.LLM.Provider))
	}
	if c.LLM.Model == "" {
		errs = append(errs, errors.New("LLM_MODEL is required"))
	}
	if _, err := c.MasterKeyBytes(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// IsAllowed reports whether the bot may operate in the chat.
func (c Config) IsAllowed(chatID int64) bool {
	return len(c.AllowedGroups) == 0 || slices.Contains(c.AllowedGroups, chatID)
}
