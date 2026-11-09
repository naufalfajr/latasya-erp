package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// config is the client's persisted credential. Plaintext by design (same
// tradeoff as the Telegram bot's state file): encrypting would need a key
// stored on the same machine. The file is 0600 and atomically written.
type config struct {
	BaseURL   string   `json:"base_url"`
	Token     string   `json:"token"`
	TokenID   int      `json:"token_id"`
	Username  string   `json:"username"`
	Scopes    []string `json:"scopes"`
	ExpiresAt string   `json:"expires_at,omitempty"` // RFC3339 or "" for never
}

func configPath() string {
	if p := os.Getenv("LATASYA_MCP_CONFIG"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "latasya-mcp.json"
	}
	return filepath.Join(dir, "latasya", "mcp.json")
}

func loadConfig() (*config, error) {
	envTok := os.Getenv("LATASYA_MCP_TOKEN")
	data, err := os.ReadFile(configPath())
	if os.IsNotExist(err) {
		// Headless/CI mode: a token in the environment is enough — there is
		// no browser to run the device login, so no config file is expected.
		if envTok != "" {
			return &config{BaseURL: envBaseURL(), Token: envTok}, nil
		}
		return nil, fmt.Errorf("not logged in — run `latasya-mcp login --base-url <url>`")
	}
	if err != nil {
		return nil, err
	}
	var c config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("config %s is malformed: %w", configPath(), err)
	}
	// A token in the environment wins over the file.
	if envTok != "" {
		c.Token = envTok
	}
	if c.Token == "" {
		return nil, fmt.Errorf("not logged in — run `latasya-mcp login --base-url <url>`")
	}
	if c.BaseURL == "" {
		c.BaseURL = envBaseURL()
	}
	return &c, nil
}

func envBaseURL() string {
	if u := os.Getenv("LATASYA_API_URL"); u != "" {
		return u
	}
	return "http://127.0.0.1:8080"
}

// save writes the config atomically with 0600 permissions.
func (c *config) save() error {
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mcp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
