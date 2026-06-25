// Package client is the thin HTTP layer over the Zebracat public API.
//
// It resolves auth in this order: --api-key flag > ZEBRACAT_API_KEY env >
// stored API key > stored OAuth access token. OAuth tokens are refreshed
// transparently (proactively when expired, and once on a 401).
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/config"
)

// Client talks to the Zebracat public API.
type Client struct {
	BaseURL    string
	HTTP       *http.Client
	creds      *config.Credentials
	apiKeyFlag string
	saveCreds  bool
}

// New builds a client. apiKeyFlag (from --api-key) takes top precedence.
func New(baseURL string, creds *config.Credentials, apiKeyFlag string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTP:       &http.Client{Timeout: 60 * time.Second},
		creds:      creds,
		apiKeyFlag: apiKeyFlag,
		saveCreds:  true,
	}
}

func envAPIKey() string { return strings.TrimSpace(os.Getenv("ZEBRACAT_API_KEY")) }

// apiKey returns the effective API key (flag > env > stored), or "".
func (c *Client) apiKey() string {
	if c.apiKeyFlag != "" {
		return c.apiKeyFlag
	}
	if v := envAPIKey(); v != "" {
		return v
	}
	if c.creds != nil {
		return c.creds.APIKey
	}
	return ""
}

// IsAuthenticated reports whether any credential is available.
func (c *Client) IsAuthenticated() bool {
	if c.apiKey() != "" {
		return true
	}
	return c.creds != nil && c.creds.AccessToken != ""
}

// AuthMode returns "api_key", "oauth", or "none" for display.
func (c *Client) AuthMode() string {
	if c.apiKey() != "" {
		return "api_key"
	}
	if c.creds != nil && c.creds.AccessToken != "" {
		return "oauth"
	}
	return "none"
}

func (c *Client) setAuth(req *http.Request) error {
	if key := c.apiKey(); key != "" {
		req.Header.Set("X-API-KEY", key)
		return nil
	}
	if c.creds != nil && c.creds.AccessToken != "" {
		// Refresh proactively if expired.
		if !c.creds.ExpiresAt.IsZero() && time.Now().After(c.creds.ExpiresAt.Add(-30*time.Second)) {
			_ = c.refresh(req.Context())
		}
		req.Header.Set("Authorization", "Bearer "+c.creds.AccessToken)
		return nil
	}
	return clierr.Auth("not authenticated")
}

// Do issues a request and decodes the JSON body into out (may be nil).
// Returns the parsed body bytes and a *clierr.Error on failure.
func (c *Client) Do(ctx context.Context, method, path string, body any, out any) ([]byte, error) {
	raw, status, err := c.doOnce(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	// One transparent retry on 401 for OAuth (token may have just expired).
	if status == http.StatusUnauthorized && c.AuthMode() == "oauth" {
		if rerr := c.refresh(ctx); rerr == nil {
			raw, status, err = c.doOnce(ctx, method, path, body)
			if err != nil {
				return nil, err
			}
		}
	}
	if status >= 400 {
		return raw, apiError(status, raw)
	}
	if out != nil && len(raw) > 0 {
		if jerr := json.Unmarshal(raw, out); jerr != nil {
			return raw, clierr.API("could not parse response: %v", jerr)
		}
	}
	return raw, nil
}

func (c *Client) doOnce(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, clierr.Usage("could not encode request body: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	u := c.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return nil, 0, clierr.Usage("bad request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "zebracat-cli")
	if err := c.setAuth(req); err != nil {
		return nil, 0, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, 0, clierr.Timeout("request timed out")
		}
		return nil, 0, clierr.API("network error: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return raw, resp.StatusCode, nil
}

// refresh exchanges the stored refresh token for a new access token.
func (c *Client) refresh(ctx context.Context) error {
	if c.creds == nil || c.creds.RefreshToken == "" || c.creds.ClientID == "" {
		return clierr.Auth("session expired")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", c.creds.RefreshToken)
	form.Set("client_id", c.creds.ClientID)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return clierr.API("token refresh failed: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return clierr.Auth("token refresh rejected")
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &tok); err != nil {
		return clierr.API("bad refresh response")
	}
	c.creds.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		c.creds.RefreshToken = tok.RefreshToken
	}
	if tok.ExpiresIn > 0 {
		c.creds.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	}
	if c.saveCreds {
		_ = config.SaveCredentials(c.creds)
	}
	return nil
}

func apiError(status int, raw []byte) *clierr.Error {
	// Try the {"error": "...", "error_description": "..."} / {"error": {...}} shapes.
	var generic map[string]any
	msg := strings.TrimSpace(string(raw))
	if json.Unmarshal(raw, &generic) == nil {
		if e, ok := generic["error"].(string); ok {
			msg = e
			if d, ok := generic["error_description"].(string); ok && d != "" {
				msg = e + ": " + d
			}
		} else if e, ok := generic["error"].(map[string]any); ok {
			if m, ok := e["message"].(string); ok {
				msg = m
			}
		} else if d, ok := generic["detail"].(string); ok {
			msg = d
		}
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return clierr.Auth("%s", msg)
	case http.StatusPaymentRequired:
		return clierr.APIHint("Top up at https://studio.zebracat.ai/billing", "%s", msg)
	default:
		return clierr.API("API error (%d): %s", status, msg)
	}
}
