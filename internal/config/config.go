// Package config loads runtime settings from a JSON file with .env overrides.
package config

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// Config holds everything a scrape or download run needs.
type Config struct {
	// UserAgent is sent on every Reddit request. Reddit rate-limits or
	// blocks requests with a generic/default Go user agent.
	UserAgent string `json:"user_agent"`

	// RedditCookie authenticates Reddit requests. Optional: unauthenticated
	// requests work but are rate-limited more aggressively.
	RedditCookie string `json:"-"`

	// DBPath is the SQLite database file location.
	DBPath string `json:"db_path"`

	// DownloadPath is the root directory media files are saved under.
	DownloadPath string `json:"download_path"`

	// Subreddits is the default set of subreddits to scrape when none are
	// passed on the command line.
	Subreddits []string `json:"subreddits"`
}

// Load reads path as JSON, then applies .env overrides (via godotenv) on
// top. Both files are optional: a missing config file yields zero-value
// defaults, and a missing .env file is silently skipped.
func Load(path, envPath string) (*Config, error) {
	cfg := &Config{
		UserAgent:    "reddit-archiver/0.1 (by u/replace-me)",
		DBPath:       "reddit-archiver.db",
		DownloadPath: "downloads",
	}

	if data, err := os.ReadFile(path); err == nil { // #nosec G304 -- path is an operator-supplied CLI flag, not untrusted input
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse config %q: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	_ = godotenv.Load(envPath) // missing .env is not an error

	if v := os.Getenv("REDDIT_COOKIE"); v != "" {
		cfg.RedditCookie = v
	}
	if v := os.Getenv("REDDIT_USER_AGENT"); v != "" {
		cfg.UserAgent = v
	}

	return cfg, nil
}
