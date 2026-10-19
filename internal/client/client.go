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
	"strconv"
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
	return c.DoWith(ctx, method, path, body, nil, out)
}

// DoWith is Do with extra request headers (e.g. Idempotency-Key).
func (c *Client) DoWith(ctx context.Context, method, path string, body any, headers map[string]string, out any) ([]byte, error) {
	raw, resp, err := c.doOnce(ctx, method, path, body, headers)
	if err != nil {
		return nil, err
	}
	// One transparent retry on 401 for OAuth (token may have just expired).
	if resp.StatusCode == http.StatusUnauthorized && c.AuthMode() == "oauth" {
		if rerr := c.refresh(ctx); rerr == nil {
			raw, resp, err = c.doOnce(ctx, method, path, body, headers)
			if err != nil {
				return nil, err
			}
		}
	}
	if resp.StatusCode >= 400 {
		return raw, apiError(resp.StatusCode, raw, resp.Header)
	}
	if out != nil && len(raw) > 0 {
		if jerr := json.Unmarshal(raw, out); jerr != nil {
			return raw, clierr.API("could not parse response: %v", jerr)
		}
	}
	return raw, nil
}

func (c *Client) doOnce(ctx context.Context, method, path string, body any, headers map[string]string) ([]byte, *http.Response, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, nil, clierr.Usage("could not encode request body: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	u := c.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return nil, nil, clierr.Usage("bad request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "zebracat-cli")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if err := c.setAuth(req); err != nil {
		return nil, nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, nil, clierr.Timeout("request timed out")
		}
		return nil, nil, clierr.API("network error: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return raw, resp, nil
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

func apiError(status int, raw []byte, header http.Header) *clierr.Error {
	// Try the {"error": "...", "error_description": "..."} / {"error": {...}} shapes,
	// plus the typed public-API siblings (type, code, retryable, hint, request_id).
	var generic map[string]any
	msg := strings.TrimSpace(string(raw))
	if strings.HasPrefix(msg, "<") {
		// A proxy/CDN returned HTML (e.g. an nginx 404) instead of JSON — almost
		// always a wrong base URL or a gateway that isn't routing to the API, or a
		// server that does not have this endpoint yet.
		msg = "the server returned a non-JSON response — check your base URL (ZEBRACAT_BASE_URL / --base-url)"
		if status == http.StatusNotFound {
			msg = "this server has no such endpoint: it is not deployed there yet, or the base URL is wrong (ZEBRACAT_BASE_URL / --base-url)"
		}
	}
	str := func(k string) string { s, _ := generic[k].(string); return s }
	if json.Unmarshal(raw, &generic) == nil {
		if e, ok := generic["error"].(string); ok {
			msg = e
			if d := str("error_description"); d != "" {
				msg = e + ": " + d
			}
		} else if e, ok := generic["error"].(map[string]any); ok {
			if m, ok := e["message"].(string); ok {
				msg = m
			}
		} else if d := str("detail"); d != "" {
			msg = d
		}
	}
	retryAfter := parseRetryAfter(header.Get("Retry-After"))
	if retryAfter == 0 {
		// Studio allowances answer retry_after_s in the body.
		if v, ok := generic["retry_after_s"].(float64); ok && v > 0 {
			retryAfter = time.Duration(v * float64(time.Second))
		}
	}
	requestID := str("request_id")
	if requestID == "" {
		requestID = header.Get("X-Request-ID")
	}

	// A typed error (PLAN §1.6): the server's own type decides the exit code,
	// and its code and hint reach stderr verbatim.
	if typ := str("type"); typ != "" {
		code := str("code")
		if code == "" {
			code = typ
		}
		retryable, _ := generic["retryable"].(bool)
		return &clierr.Error{
			Exit: clierr.ExitForType(typ), Code: code, Message: msg, Hint: str("hint"),
			Type: typ, Retryable: retryable, RequestID: requestID,
			Status: status, RetryAfter: retryAfter,
		}
	}

	var e *clierr.Error
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		e = clierr.Auth("%s", msg)
	case http.StatusPaymentRequired:
		e = clierr.APIHint("Top up at https://studio.zebracat.ai/billing", "%s", msg)
	default:
		e = clierr.API("API error (%d): %s", status, msg)
	}
	if code := str("code"); code != "" {
		e.Code = code
	}
	if h := str("hint"); h != "" {
		e.Hint = h
	}
	e.RequestID, e.Status, e.RetryAfter = requestID, status, retryAfter
	if status == http.StatusTooManyRequests {
		e.Retryable = true
	}
	return e
}

// parseRetryAfter reads a Retry-After header: delay-seconds or an HTTP date.
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil && n >= 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
