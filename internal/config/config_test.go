package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsWithNoFiles(t *testing.T) {
	dir := t.TempDir()

	cfg, err := Load(filepath.Join(dir, "missing-config.json"), filepath.Join(dir, "missing.env"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.UserAgent != "reddit-archiver/0.1 (by u/replace-me)" {
		t.Errorf("UserAgent = %q, want default", cfg.UserAgent)
	}
	if cfg.DBPath != "reddit-archiver.db" {
		t.Errorf("DBPath = %q, want default", cfg.DBPath)
	}
	if cfg.DownloadPath != "downloads" {
		t.Errorf("DownloadPath = %q, want default", cfg.DownloadPath)
	}
}

func TestLoadFromConfigFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	body := `{"user_agent":"custom-agent/1.0","db_path":"custom.db","download_path":"media","subreddits":["golang","test"]}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath, filepath.Join(dir, "missing.env"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.UserAgent != "custom-agent/1.0" {
		t.Errorf("UserAgent = %q, want custom-agent/1.0", cfg.UserAgent)
	}
	if cfg.DBPath != "custom.db" {
		t.Errorf("DBPath = %q, want custom.db", cfg.DBPath)
	}
	if cfg.DownloadPath != "media" {
		t.Errorf("DownloadPath = %q, want media", cfg.DownloadPath)
	}
	if len(cfg.Subreddits) != 2 || cfg.Subreddits[0] != "golang" {
		t.Errorf("Subreddits = %v, unexpected", cfg.Subreddits)
	}
}

func TestLoadInvalidConfigFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte("not json"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(configPath, filepath.Join(dir, "missing.env")); err == nil {
		t.Fatal("Load: expected error for invalid JSON, got nil")
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Cleanup(func() {
		_ = os.Unsetenv("REDDIT_COOKIE")
		_ = os.Unsetenv("REDDIT_USER_AGENT")
	})

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	body := "REDDIT_COOKIE=session=abc123\nREDDIT_USER_AGENT=env-agent/1.0\n"
	if err := os.WriteFile(envPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	cfg, err := Load(filepath.Join(dir, "missing-config.json"), envPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RedditCookie != "session=abc123" {
		t.Errorf("RedditCookie = %q, want session=abc123", cfg.RedditCookie)
	}
	if cfg.UserAgent != "env-agent/1.0" {
		t.Errorf("UserAgent = %q, want env-agent/1.0 (overridden by env)", cfg.UserAgent)
	}
}
