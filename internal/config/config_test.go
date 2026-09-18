package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Setenv("BOT_TOKEN", "123:abc")
	t.Setenv("LLM_MODEL", "gpt-5-mini")
	t.Setenv("ALLOWED_GROUPS", "-100,-200")
	t.Setenv("MASTER_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CacheSize != 1000 || cfg.DBPath != "data/messages.db" || cfg.LLM.Provider != "openai" {
		t.Errorf("defaults not applied: %+v", cfg)
	}
	if !cfg.IsAllowed(-200) || cfg.IsAllowed(-300) {
		t.Errorf("whitelist %v", cfg.AllowedGroups)
	}
	if key, err := cfg.MasterKeyBytes(); err != nil || len(key) != 32 {
		t.Errorf("master key: %v, %v", key, err)
	}
}

func TestLoadValidates(t *testing.T) {
	t.Setenv("BOT_TOKEN", "123:abc")
	t.Setenv("LLM_PROVIDER", "anthropic")
	t.Setenv("MASTER_KEY", "c2hvcnQ=")

	_, err := Load()
	if err == nil {
		t.Fatal("want error")
	}
	for _, want := range []string{"LLM_PROVIDER", "LLM_MODEL", "MASTER_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestEmptyWhitelistAllowsAll(t *testing.T) {
	if !(Config{}).IsAllowed(-1) {
		t.Error("empty whitelist must allow every chat")
	}
}
