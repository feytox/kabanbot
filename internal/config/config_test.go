package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Setenv("BOT_TOKEN", "123:abc")
	t.Setenv("ALLOWED_GROUPS", "-100,-200")
	t.Setenv("MASTER_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CacheSize != 1000 || cfg.DBPath != "data/messages.db" {
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
	t.Setenv("CACHE_SIZE", "0")
	t.Setenv("MASTER_KEY", "c2hvcnQ=")

	_, err := Load()
	if err == nil {
		t.Fatal("want error")
	}
	for _, want := range []string{"CACHE_SIZE", "MASTER_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestMasterKeyIsRequired(t *testing.T) {
	t.Setenv("BOT_TOKEN", "123:abc")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "MASTER_KEY") {
		t.Fatalf("err = %v, want MASTER_KEY required", err)
	}
}

func TestEmptyWhitelistAllowsAll(t *testing.T) {
	if !(Config{}).IsAllowed(-1) {
		t.Error("empty whitelist must allow every chat")
	}
}
