// Package client is the thin HTTP layer over the Zebracat public API.
//
// Auth is an API key, sent as X-API-KEY, resolved in this order: --api-key
// flag > ZEBRACAT_API_KEY env > the stored key (~/.zebracat/credentials.json).
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/config"
	"github.com/zebracatai/zebracat-cli/internal/version"
)

// Client talks to the Zebracat public API.
type Client struct {
	BaseURL    string
	HTTP       *http.Client
	creds      *config.Credentials
	apiKeyFlag string
}

// UserAgent names the CLI, its version and platform, e.g.
// "zebracat-cli/0.7.0 (linux; amd64)".
func UserAgent() string {
	return fmt.Sprintf("zebracat-cli/%s (%s; %s)", version.Version, runtime.GOOS, runtime.GOARCH)
}

// New builds a client. apiKeyFlag (from --api-key) takes top precedence.
//
// The client has no overall timeout: every caller passes a context with its
// own deadline, so a 12 MB upload on a slow link or a 3-minute plan call is not
// cut at 60 s. Connecting and the TLS handshake still give up on their own.
func New(baseURL string, creds *config.Credentials, apiKeyFlag string) *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	tr.TLSHandshakeTimeout = 15 * time.Second
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTP:       &http.Client{Transport: tr, CheckRedirect: keepKeyOnHost},
		creds:      creds,
		apiKeyFlag: apiKeyFlag,
	}
}

// keepKeyOnHost follows a redirect, but the API key goes only to the API's own
// host: Go forwards custom headers like X-API-KEY to any host by default.
func keepKeyOnHost(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if req.URL.Host != via[0].URL.Host {
		req.Header.Del("X-API-KEY")
		req.Header.Del("Authorization")
	}
	return nil
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

// IsAuthenticated reports whether an API key is available.
func (c *Client) IsAuthenticated() bool { return c.apiKey() != "" }

// AuthMode returns "api_key" or "none" for display.
func (c *Client) AuthMode() string {
	if c.apiKey() != "" {
		return "api_key"
	}
	return "none"
}

func (c *Client) setAuth(req *http.Request) error {
	if key := c.apiKey(); key != "" {
		req.Header.Set("X-API-KEY", key)
		return nil
	}
	return clierr.Auth("not authenticated")
}

// RawBody is a request body sent as is, with its own content type.
type RawBody struct {
	ContentType string
	Data        []byte
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
	if resp.StatusCode >= 400 {
		return raw, apiError(resp.StatusCode, raw, resp.Header)
	}
	if out != nil && len(raw) > 0 {
		if jerr := json.Unmarshal(raw, out); jerr != nil {
			e := clierr.API("the server's answer is not valid JSON (HTTP %d): %v", resp.StatusCode, jerr)
			e.Code, e.Status = "bad_response", resp.StatusCode
			e.RequestID = resp.Header.Get("X-Request-ID")
			return raw, e
		}
	}
	return raw, nil
}

func (c *Client) doOnce(ctx context.Context, method, path string, body any, headers map[string]string) ([]byte, *http.Response, error) {
	var reader io.Reader
	contentType := "application/json"
	if raw, ok := body.(RawBody); ok { // a body sent as is (a multipart upload)
		reader, contentType = bytes.NewReader(raw.Data), raw.ContentType
	} else if body != nil {
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
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent())
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
		return nil, nil, clierr.Network("network error: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, nil, clierr.Timeout("request timed out while reading the answer")
		}
		return nil, nil, clierr.Network("the connection broke while reading the answer: %v", err)
	}
	return raw, resp, nil
}

func apiError(status int, raw []byte, header http.Header) *clierr.Error {
	// Try the {"error": "...", "error_description": "..."} / {"error": {...}} shapes,
	// plus the typed public-API siblings (type, code, retryable, hint, request_id,
	// field, allowed_values, docs_url, errors).
	var generic map[string]any
	msg := strings.TrimSpace(string(raw))
	html := strings.HasPrefix(msg, "<")
	if html {
		// A proxy/CDN answered with an HTML page instead of JSON.
		switch {
		case status >= 500:
			// nginx/Cloudflare in front of a server that is down or restarting.
			msg = fmt.Sprintf("Zebracat is having trouble right now (HTTP %d). Try again in a minute.", status)
		case status == http.StatusNotFound:
			msg = "this server has no such endpoint: it is not deployed there yet, or the base URL is wrong (ZEBRACAT_BASE_URL / --base-url)"
		default:
			msg = "the server returned a non-JSON response — check your base URL (ZEBRACAT_BASE_URL / --base-url)"
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
		} else if m := str("message"); m != "" {
			msg = m
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

	var e *clierr.Error
	if typ := str("type"); typ != "" {
		// A typed error (PLAN §1.6): the server's own type decides the exit
		// code, and its code, hint and retryable reach stderr verbatim.
		code := str("code")
		if code == "" {
			code = typ
		}
		retryable, _ := generic["retryable"].(bool)
		e = &clierr.Error{
			Exit: clierr.ExitForType(typ), Code: code, Message: msg, Hint: str("hint"),
			Type: typ, Retryable: retryable,
		}
	} else {
		e = untypedError(status, msg)
		if code := str("code"); code != "" {
			e.Code = code
		}
		if h := str("hint"); h != "" {
			e.Hint = h
		}
	}
	e.RequestID, e.Status, e.RetryAfter = requestID, status, retryAfter
	e.Field, e.DocsURL = str("field"), str("docs_url")
	if v, ok := generic["allowed_values"].([]any); ok {
		e.AllowedValues = v
	}
	if v, ok := generic["errors"].([]any); ok {
		e.Errors = v
	}
	return e
}

// untypedError maps an answer without a "type" (an older server, a proxy, a
// framework default) by its HTTP status, so type and retryable are never empty.
func untypedError(status int, msg string) *clierr.Error {
	var e *clierr.Error
	switch {
	case status == http.StatusUnauthorized:
		e = clierr.Auth("%s", msg)
		e.Code = "authentication_failed"
	case status == http.StatusForbidden:
		// The key was read and is valid; it may not do this. Logging in again
		// does not help, so there is no login hint.
		e = clierr.Auth("%s", msg)
		e.Code, e.Type, e.Hint = "permission_denied", clierr.TypePermission, ""
	case status == http.StatusPaymentRequired:
		e = clierr.APIHint("Top up at https://studio.zebracat.ai/billing", "%s", msg)
		e.Code, e.Type = "insufficient_funds", "insufficient_funds"
	case status == http.StatusNotFound:
		e = clierr.API("API error (%d): %s", status, msg)
		e.Code, e.Type = "not_found", "not_found"
	case status == http.StatusConflict:
		e = clierr.API("API error (%d): %s", status, msg)
		e.Type = "conflict"
	case status == http.StatusTooManyRequests:
		e = clierr.API("API error (%d): %s", status, msg)
		e.Code, e.Type, e.Retryable = "rate_limited", "rate_limited", true
	case status >= 500:
		e = clierr.API("%s", msg)
		if !strings.HasPrefix(msg, "Zebracat is having trouble") {
			e.Message = fmt.Sprintf("API error (%d): %s", status, msg)
		}
		e.Code, e.Type, e.Retryable = "server_error", clierr.TypeUnavailable, true
	default:
		e = clierr.API("API error (%d): %s", status, msg)
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
