package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const (
	DefaultAPIURL = "https://api.gora8.com"
	configDir     = ".gora8"
	configFile    = "config.json"
)

// resolveDefaultAPIURL lets local development point the CLI at a non-production
// API without touching the saved config file — GORA8_API_URL overrides the
// real default entirely (used by nothing in production; every real user gets
// DefaultAPIURL).
func resolveDefaultAPIURL() string {
	if v := os.Getenv("GORA8_API_URL"); v != "" {
		return v
	}
	return DefaultAPIURL
}

// Config holds the persisted CLI configuration.
type Config struct {
	APIKey    string `json:"api_key"`
	APIURL    string `json:"api_url"`
	UserEmail string `json:"user_email"`
	UserID    string `json:"user_id"`
}

// configPath returns the full path to the config file.
func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, configDir, configFile), nil
}

// Load reads the config from disk. Returns an empty config if the file does
// not exist yet (not an error).
func Load() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{APIURL: resolveDefaultAPIURL()}, nil
		}
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.APIURL == "" {
		cfg.APIURL = resolveDefaultAPIURL()
	}
	return &cfg, nil
}

// Save writes the config to disk, creating the directory if needed.
func Save(cfg *Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// Clear removes all authentication fields from the config and saves.
func Clear() error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	cfg.APIKey = ""
	cfg.UserEmail = ""
	cfg.UserID = ""
	return Save(cfg)
}

// IsAuthenticated returns true when an API key is present.
func (c *Config) IsAuthenticated() bool {
	return c.APIKey != ""
}
