// Package config handles the CLI's on-disk settings and credentials.
//
// Files live under ~/.zebracat/ :
//
//	config.json       — { base_url, mcp_url, output }
//	credentials.json  — { api_key, access_token, refresh_token, expires_at, client_id }
//
// Environment variables override files: ZEBRACAT_API_KEY, ZEBRACAT_BASE_URL,
// ZEBRACAT_OUTPUT. With ZEBRACAT_API_KEY set, nothing reads the TTY — the CLI is
// safe for CI and agents out of the box.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const (
	DefaultBaseURL = "https://api.zebracat.ai"
	DefaultMCPURL  = "https://mcp.zebracat.ai"
)

// Settings is the non-secret configuration.
type Settings struct {
	BaseURL string `json:"base_url,omitempty"`
	MCPURL  string `json:"mcp_url,omitempty"`
	Output  string `json:"output,omitempty"` // "json" (default) or "human"
}

// Credentials holds the secrets used to authenticate requests.
type Credentials struct {
	// API key path (pay-as-you-go, billed from api_dollar_balance).
	APIKey string `json:"api_key,omitempty"`
	// OAuth path (billed from the account's plan credit).
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	ClientID     string    `json:"client_id,omitempty"`
}

// Dir returns ~/.zebracat, creating it if needed.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".zebracat")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// LoadSettings reads config.json (missing file -> zero Settings, no error).
func LoadSettings() (*Settings, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	s := &Settings{}
	if err := readJSON(filepath.Join(dir, "config.json"), s); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return s, nil
}

// SaveSettings persists config.json.
func SaveSettings(s *Settings) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, "config.json"), s)
}

// LoadCredentials reads credentials.json (missing file -> empty creds, no error).
func LoadCredentials() (*Credentials, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	c := &Credentials{}
	if err := readJSON(filepath.Join(dir, "credentials.json"), c); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return c, nil
}

// SaveCredentials persists credentials.json with 0600 perms.
func SaveCredentials(c *Credentials) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, "credentials.json"), c)
}

// ClearCredentials removes credentials.json (logout).
func ClearCredentials() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(dir, "credentials.json"))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ResolveBaseURL: ZEBRACAT_BASE_URL env > config > default.
func ResolveBaseURL(s *Settings) string {
	if v := os.Getenv("ZEBRACAT_BASE_URL"); v != "" {
		return v
	}
	if s != nil && s.BaseURL != "" {
		return s.BaseURL
	}
	return DefaultBaseURL
}

// ResolveMCPURL: config > default.
func ResolveMCPURL(s *Settings) string {
	if s != nil && s.MCPURL != "" {
		return s.MCPURL
	}
	return DefaultMCPURL
}

// ResolveOutput: ZEBRACAT_OUTPUT env > config > "json".
func ResolveOutput(s *Settings) string {
	if v := os.Getenv("ZEBRACAT_OUTPUT"); v != "" {
		return v
	}
	if s != nil && s.Output != "" {
		return s.Output
	}
	return "json"
}
