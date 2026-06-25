// Package auth implements the interactive OAuth 2.1 login used by `zebracat auth
// login`. It discovers the authorization server, dynamically registers a client,
// runs the PKCE authorization-code flow (browser loopback by default, or a
// copy-paste device code with --device), and stores the resulting tokens.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/config"
)

type asMetadata struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	RegistrationEndpoint  string `json:"registration_endpoint"`
}

const oobRedirect = "urn:ietf:wg:oauth:2.0:oob"

func randString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func pkce() (verifier, challenge string) {
	verifier = randString(48)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return
}

func discover(ctx context.Context, baseURL string) (*asMetadata, error) {
	u := strings.TrimRight(baseURL, "/") + "/.well-known/oauth-authorization-server"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, clierr.API("could not reach the authorization server: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, clierr.API("authorization server discovery failed (%d)", resp.StatusCode)
	}
	var m asMetadata
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, clierr.API("bad discovery document: %v", err)
	}
	if m.AuthorizationEndpoint == "" || m.TokenEndpoint == "" {
		return nil, clierr.API("discovery document missing endpoints")
	}
	return &m, nil
}

func register(ctx context.Context, endpoint, redirectURI string) (string, error) {
	if endpoint == "" {
		return "", clierr.API("server does not support dynamic client registration")
	}
	payload := map[string]any{
		"client_name":                "Zebracat CLI",
		"redirect_uris":              []string{redirectURI},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
		"scope":                      "video:read video:write account:read",
	}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", clierr.API("client registration failed: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		return "", clierr.API("client registration rejected (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.ClientID == "" {
		return "", clierr.API("registration response missing client_id")
	}
	return out.ClientID, nil
}

func exchange(ctx context.Context, tokenEndpoint, clientID, code, verifier, redirectURI string) (*config.Credentials, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", verifier)
	form.Set("client_id", clientID)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, clierr.API("token exchange failed: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, clierr.Auth("token exchange rejected: %s", strings.TrimSpace(string(raw)))
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(raw, &tok); err != nil {
		return nil, clierr.API("bad token response: %v", err)
	}
	creds := &config.Credentials{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		TokenType:    tok.TokenType,
		ClientID:     clientID,
	}
	if tok.ExpiresIn > 0 {
		creds.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	}
	return creds, nil
}

func authorizeURL(meta *asMetadata, clientID, redirectURI, challenge, state string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("scope", "video:read video:write account:read")
	return meta.AuthorizationEndpoint + "?" + q.Encode()
}

func openBrowser(u string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	_ = exec.Command(cmd, append(args, u)...).Start()
}

// Login runs the full interactive flow and returns stored-ready credentials.
// prompt is called with the URL the user should visit; readCode is used in
// device mode to read the pasted code from the user.
func Login(ctx context.Context, baseURL string, device bool, prompt func(string), readCode func() (string, error)) (*config.Credentials, error) {
	meta, err := discover(ctx, baseURL)
	if err != nil {
		return nil, err
	}
	verifier, challenge := pkce()
	state := randString(16)

	if device {
		clientID, err := register(ctx, meta.RegistrationEndpoint, oobRedirect)
		if err != nil {
			return nil, err
		}
		prompt(authorizeURL(meta, clientID, oobRedirect, challenge, state))
		code, err := readCode()
		if err != nil {
			return nil, clierr.Usage("could not read code: %v", err)
		}
		return exchange(ctx, meta.TokenEndpoint, clientID, strings.TrimSpace(code), verifier, oobRedirect)
	}

	// Browser loopback flow.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, clierr.API("could not start local callback server: %v", err)
	}
	defer ln.Close()
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", ln.Addr().(*net.TCPAddr).Port)

	clientID, err := register(ctx, meta.RegistrationEndpoint, redirectURI)
	if err != nil {
		return nil, err
	}

	type result struct {
		code string
		err  error
	}
	resCh := make(chan result, 1)
	srv := &http.Server{}
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			writeBrowserPage(w, false, q.Get("error_description"))
			resCh <- result{err: clierr.Auth("authorization denied: %s", e)}
			return
		}
		if q.Get("state") != state {
			writeBrowserPage(w, false, "state mismatch")
			resCh <- result{err: clierr.Auth("state mismatch — possible CSRF, aborting")}
			return
		}
		writeBrowserPage(w, true, "")
		resCh <- result{code: q.Get("code")}
	})
	srv.Handler = mux
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	prompt(authorizeURL(meta, clientID, redirectURI, challenge, state))
	openBrowser(authorizeURL(meta, clientID, redirectURI, challenge, state))

	select {
	case <-ctx.Done():
		return nil, clierr.Timeout("login timed out")
	case res := <-resCh:
		if res.err != nil {
			return nil, res.err
		}
		return exchange(ctx, meta.TokenEndpoint, clientID, res.code, verifier, redirectURI)
	}
}

func writeBrowserPage(w http.ResponseWriter, ok bool, detail string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	title, msg := "You're signed in", "You can close this tab and return to your terminal."
	if !ok {
		title, msg = "Sign-in failed", detail
	}
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>Zebracat CLI</title>
<body style="font-family:-apple-system,Segoe UI,Roboto,sans-serif;background:#0e0b16;color:#f5f3ff;display:flex;min-height:100vh;align-items:center;justify-content:center;text-align:center">
<div><div style="font-size:42px">🦓</div><h2 style="color:#a855f7">%s</h2><p style="color:#a99fc7">%s</p></div>`, title, msg)
}
