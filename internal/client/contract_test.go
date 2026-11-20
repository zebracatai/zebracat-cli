package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/config"
	"github.com/zebracatai/zebracat-cli/internal/version"
)

// A gateway's HTML 5xx is the server having trouble, not a wrong base URL.
func TestHTML5xxSaysTryAgain(t *testing.T) {
	e := apiError(502, []byte("<html><head><title>502 Bad Gateway</title></head><body>nginx</body></html>"), http.Header{"X-Request-Id": {"rq"}})
	if !strings.Contains(e.Message, "Zebracat is having trouble") || strings.Contains(e.Message, "base URL") {
		t.Fatalf("message %q", e.Message)
	}
	if e.Exit != 1 || e.Type != clierr.TypeUnavailable || !e.Retryable || e.RequestID != "rq" {
		t.Fatalf("%+v", e)
	}
}

// 403 is "this key may not do that", not "your key was rejected".
func TestUntyped403IsPermissionNot401(t *testing.T) {
	e403 := apiError(403, []byte(`{"detail":"You do not have permission to perform this action."}`), http.Header{})
	if e403.Exit != 3 || e403.Type != "permission" || e403.Code != "permission_denied" || strings.Contains(e403.Hint, "auth login") {
		t.Fatalf("403: %+v", e403)
	}
	e401 := apiError(401, []byte(`{"detail":"Invalid API key."}`), http.Header{})
	if e401.Exit != 3 || e401.Type != "authentication" || !strings.Contains(e401.Hint, "auth login") {
		t.Fatalf("401: %+v", e401)
	}
}

// The server's field, allowed_values, docs_url and errors[] reach the envelope.
func TestTypedErrorPassesServerFieldsThrough(t *testing.T) {
	raw := []byte(`{"error":"mood must be one of happy, sad","code":"invalid_choice","type":"invalid_request","retryable":false,
		"field":"mood","allowed_values":["happy","sad"],"docs_url":"https://api.zebracat.ai/docs#invalid_choice",
		"errors":[{"field":"mood","code":"invalid_choice"},{"field":"aspect","code":"invalid_choice"}],"request_id":"req_9"}`)
	e := apiError(400, raw, http.Header{})
	if e.Exit != 2 || e.Field != "mood" || len(e.AllowedValues) != 2 || e.DocsURL == "" || len(e.Errors) != 2 || e.RequestID != "req_9" {
		t.Fatalf("%+v", e)
	}
}

// Untyped answers still get a type and retryable.
func TestUntypedAnswersGetATypeAndRetryable(t *testing.T) {
	for status, want := range map[int]struct {
		typ       string
		retryable bool
	}{
		400: {"api_error", false}, 402: {"insufficient_funds", false}, 404: {"not_found", false},
		409: {"conflict", false}, 429: {"rate_limited", true}, 500: {"unavailable", true}, 503: {"unavailable", true},
	} {
		e := apiError(status, []byte(`{"detail":"x"}`), http.Header{})
		if e.Type != want.typ || e.Retryable != want.retryable {
			t.Errorf("%d: type %q retryable %v, want %q %v", status, e.Type, e.Retryable, want.typ, want.retryable)
		}
	}
}

// A request that never reaches a server is a retryable network error.
func TestNetworkErrorIsRetryableUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing listens there now
	c := New(url, &config.Credentials{}, "sk-x")
	_, err := c.Do(context.Background(), "GET", "/api/v1/public/account", nil, nil)
	ce, ok := err.(*clierr.Error)
	if !ok || ce.Code != "network_error" || ce.Type != clierr.TypeUnavailable || !ce.Retryable || ce.Exit != 1 {
		t.Fatalf("%#v", err)
	}
}

// User-Agent names the version and platform; the client has no 60 s cap
// (callers set their own deadline), and a bad JSON body is a typed error.
func TestUserAgentNoOverallTimeoutAndBadJSON(t *testing.T) {
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{not json"))
	}))
	defer srv.Close()
	c := New(srv.URL, &config.Credentials{}, "sk-x")
	if c.HTTP.Timeout != 0 {
		t.Fatalf("client timeout %v: a slow upload would be cut", c.HTTP.Timeout)
	}
	var out map[string]any
	_, err := c.Do(context.Background(), "GET", "/x", nil, &out)
	want := "zebracat-cli/" + version.Version + " (" + runtime.GOOS + "; " + runtime.GOARCH + ")"
	if ua != want {
		t.Fatalf("User-Agent %q, want %q", ua, want)
	}
	ce, ok := err.(*clierr.Error)
	if !ok || ce.Code != "bad_response" || ce.Type == "" {
		t.Fatalf("%#v", err)
	}
}

// Old OAuth tokens in credentials.json are not a sign-in any more.
func TestOnlyAnAPIKeyAuthenticates(t *testing.T) {
	t.Setenv("ZEBRACAT_API_KEY", "")
	c := New("http://x", &config.Credentials{AccessToken: "old-oauth"}, "")
	if c.IsAuthenticated() || c.AuthMode() != "none" {
		t.Fatalf("authenticated=%v mode=%s", c.IsAuthenticated(), c.AuthMode())
	}
	c = New("http://x", &config.Credentials{APIKey: "sk-1"}, "")
	if !c.IsAuthenticated() || c.AuthMode() != "api_key" {
		t.Fatalf("authenticated=%v mode=%s", c.IsAuthenticated(), c.AuthMode())
	}
}
