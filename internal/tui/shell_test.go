package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/zebracatai/zebracat-cli/internal/apitest"
	"github.com/zebracatai/zebracat-cli/internal/attach"
	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/config"
	"github.com/zebracatai/zebracat-cli/internal/version"
)

// TestMain pins the version, so no golden file changes when the CLI's
// version line is bumped.
func TestMain(m *testing.M) {
	version.Version = "0.0.0"
	os.Exit(m.Run())
}

// stubAPI answers like the audit's stub (reports/cli-audit/stub/stub.py): the
// error bodies the public API and its gateway really send. A key named
// "sk-good" passes GET /account; any other key is a typed 401.
func stubAPI(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		send := func(status int, ctype, body string, hdr ...string) {
			w.Header().Set("Content-Type", ctype)
			w.Header().Set("X-Request-ID", "req_stub123")
			for i := 0; i+1 < len(hdr); i += 2 {
				w.Header().Set(hdr[i], hdr[i+1])
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/m429/"):
			send(429, "application/json", `{"error": "You have reached today's plan limit.", "code": "daily_limit", "type": "rate_limited",
				"retryable": true, "request_id": "req_stub123", "hint": "Try again tomorrow."}`, "Retry-After", "2")
		case strings.HasPrefix(r.URL.Path, "/m429bare/"):
			send(429, "application/json", `{"detail": "Request was throttled."}`, "Retry-After", "30")
		case strings.HasPrefix(r.URL.Path, "/m403/"):
			send(403, "application/json", `{"detail": "You do not have permission to perform this action."}`)
		case strings.HasPrefix(r.URL.Path, "/m500html/"):
			send(502, "text/html", `<html><head><title>502 Bad Gateway</title></head><body>nginx</body></html>`)
		case strings.HasPrefix(r.URL.Path, "/m401/"):
			send(401, "application/json", `{"error": "Invalid API key.", "code": "authentication_required", "type": "authentication",
				"retryable": false, "request_id": "req_stub123"}`)
		case strings.HasPrefix(r.URL.Path, "/m500/"):
			send(500, "application/json", `{"error": "Something went wrong on our side.", "code": "internal", "type": "engine",
				"retryable": true, "request_id": "req_stub123"}`)
		case r.Header.Get("X-API-KEY") != "sk-good":
			send(401, "application/json", `{"error": "Invalid API key.", "code": "authentication_required", "type": "authentication",
				"retryable": false, "request_id": "req_stub123"}`)
		default:
			send(200, "application/json", `{"email": "stub@example.com", "plan": "Cat Mode", "api_dollar_balance": "208.25",
				"account_credit": {"remaining": 1240, "limit": 2000}}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// apiErr is the error the real client returns for one stub mode.
func apiErr(t *testing.T, base, mode string) error {
	t.Helper()
	cl := client.New(base+"/"+mode, &config.Credentials{}, "sk-good")
	_, err := cl.Do(context.Background(), "GET", "/api/v1/public/videos/t1", nil, nil)
	if err == nil {
		t.Fatalf("%s: no error", mode)
	}
	return err
}

// The shell shows the server's own words for a typed error, its Retry-After
// for an untyped 429, and only words what the server didn't (#9).
func TestServerErrorsKeepTheServersWords(t *testing.T) {
	srv := stubAPI(t)
	m, _ := against(t)
	cases := []struct {
		mode      string
		want, not []string
		rejected  bool
	}{
		{"m429", []string{"✗ You have reached today's plan limit.", "⎿ Try again tomorrow."}, []string{"Wait a minute", "Too many requests"}, false},
		{"m429bare", []string{"✗ Too many requests just now.", "Try again in 30 s."}, []string{"Wait a minute"}, false},
		{"m403", []string{"✗ You do not have permission to perform this action.", "isn't allowed to do this"}, []string{"rejected", "auth login"}, false},
		{"m500html", []string{"✗ Zebracat is having trouble right now.", "Try again in a minute.", "Request id: req_stub123."}, []string{"base URL", "non-JSON"}, false},
		{"m500", []string{"✗ Something went wrong on our side.", "Try again in a minute.", "Request id: req_stub123."}, nil, false},
		{"m401", []string{"✗ Your API key was rejected", "/login"}, nil, true},
	}
	// A hint or request id with a terminal escape prints as plain text: the
	// control bytes are dropped, so the rest survives printed()'s ANSI strip.
	evil := &clierr.Error{Exit: 1, Code: "x", Type: "engine", Message: "m", Hint: "h\x1b]0;pwned\x07", RequestID: "r\x1b[2J", Status: 500}
	if out := printed(msgs(m.sayAPIErr(evil, ""))); !strings.Contains(out, "h]0;pwned") || !strings.Contains(out, "r[2J") {
		t.Fatalf("raw escape in the fix line: %q", out)
	}
	for _, c := range cases {
		m.keyRejected = false
		out := printed(msgs(m.sayAPIErr(apiErr(t, srv.URL, c.mode), "")))
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: misses %q:\n%s", c.mode, w, out)
			}
		}
		for _, n := range c.not {
			if strings.Contains(out, n) {
				t.Errorf("%s: says %q:\n%s", c.mode, n, out)
			}
		}
		if m.keyRejected != c.rejected {
			t.Errorf("%s: keyRejected = %v", c.mode, m.keyRejected)
		}
	}
}

// A key the server rejects at launch shows as rejected (never "signed in"),
// and /login opens the key field even with ZEBRACAT_API_KEY set (#10).
func TestRejectedKeyAtLaunch(t *testing.T) {
	srv := stubAPI(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ZEBRACAT_API_KEY", "sk-dead")
	cl := client.New(srv.URL, &config.Credentials{}, "")
	a, rejected := launchAccount(context.Background(), cl)
	if a != nil || !rejected {
		t.Fatalf("launch: account %v rejected %v", a, rejected)
	}
	m := newModel(cl, srv.URL)
	m.width, m.height = 100, 30
	m.acct, m.keyRejected = a, rejected
	if w := ansi.Strip(m.welcomeView()); !strings.Contains(w, "Your API key was rejected · /login") || strings.Contains(w, "Signed in") {
		t.Fatalf("welcome:\n%s", w)
	}
	if s := ansi.Strip(m.footer()); !strings.Contains(s, "API key rejected · /login") || strings.Contains(s, "signed in") {
		t.Fatalf("footer:\n%s", s)
	}
	_, cmd := m.handleSlash("/login", nil)
	if !m.askKey || strings.Contains(printed(msgs(cmd)), "already signed in") {
		t.Fatalf("/login must open the key field: askKey %v", m.askKey)
	}

	// A good key pasted now wins over the environment's for this session.
	m.key.SetValue("sk-good")
	_, cmd = m.handleEnter()
	_, cmd = m.Update(find[keyCheckedMsg](t, msgs(cmd)))
	if out := printed(msgs(cmd)); !strings.Contains(out, "✓ Signed in") || !strings.Contains(out, "ZEBRACAT_API_KEY is set") {
		t.Fatalf("after a good key: %q", out)
	}
	if m.keyRejected || m.acct == nil || m.acct.email != "stub@example.com" {
		t.Fatalf("rejected %v acct %+v", m.keyRejected, m.acct)
	}
	var seen string
	check := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = r.Header.Get("X-API-KEY") }))
	defer check.Close()
	m.cl.BaseURL = check.URL
	_, _ = m.cl.Do(context.Background(), "GET", "/x", nil, nil)
	if seen != "sk-good" {
		t.Fatalf("the session sent %q, want the pasted key", seen)
	}

	// A slow or unreachable server is not a rejected key.
	if _, rejected := launchAccount(context.Background(), client.New("http://127.0.0.1:1", &config.Credentials{}, "sk-x")); rejected {
		t.Fatal("no network is not a rejected key")
	}
}

// A key that fails the check is never saved: credentials.json stays
// byte-identical and the session keeps the old key (#2, shell half).
func TestBadKeyKeepsTheSavedOne(t *testing.T) {
	srv := stubAPI(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZEBRACAT_API_KEY", "")
	if err := config.SaveCredentials(&config.Credentials{APIKey: "sk-good"}); err != nil {
		t.Fatal(err)
	}
	dir, _ := config.Dir()
	path := filepath.Join(dir, "credentials.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	creds, _ := config.LoadCredentials()
	m := newModel(client.New(srv.URL, creds, ""), srv.URL)
	m.width = 100
	m.handleSlash("/login", nil)
	if !m.askKey {
		t.Fatal("/login while signed in opens the key field")
	}
	m.key.SetValue("sk-wrong")
	_, cmd := m.handleEnter()
	_, cmd = m.Update(find[keyCheckedMsg](t, msgs(cmd)))
	if out := printed(msgs(cmd)); !strings.Contains(out, "That key didn't work. Your current key is kept.") {
		t.Fatalf("bad key: %q", out)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatalf("credentials.json changed:\n%s\n→\n%s", before, after)
	}
	if a, err := fetchAccountErr(context.Background(), m.cl); err != nil || a == nil {
		t.Fatalf("the session lost its good key: %v", err)
	}

	// No network: nothing is saved either.
	m.baseURL = "http://127.0.0.1:1"
	m.handleSlash("/login", nil)
	m.key.SetValue("sk-other")
	_, cmd = m.handleEnter()
	_, cmd = m.Update(find[keyCheckedMsg](t, msgs(cmd)))
	if out := printed(msgs(cmd)); !strings.Contains(out, "Couldn't check that key") {
		t.Fatalf("offline: %q", out)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("credentials.json changed offline")
	}
}

// A paste with Windows (or old Mac) line ends keeps single line breaks (#22).
func TestCRLFPasteKeepsSingleLineBreaks(t *testing.T) {
	m, srv := against(t)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("Scene one.\r\nScene two.\r\n\r\nScene three.\rEnd")})
	if got, want := m.in.Value(), "Scene one.\nScene two.\n\nScene three.\nEnd"; got != want {
		t.Fatalf("pasted %q, want %q", got, want)
	}
	_, cmd := m.handleEnter()
	msgs(cmd)
	post, _ := srv.Last("POST", "/videos")
	if b, _ := post.Body["brief"].(string); strings.Contains(b, "\n\n\n") || strings.Count(b, "\n") != 4 {
		t.Fatalf("brief sent %q", b)
	}
}

// On a 40×12 terminal the `/` menu fits the screen with its selection in view
// and says how many more there are (#21).
func TestMenuFitsASmallTerminal(t *testing.T) {
	m := layoutModel(t, 40, true)
	m.height = 12
	m.fitInput()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	for i := 0; i <= len(commands); i++ {
		v := ansi.Strip(m.liveView())
		if n := len(strings.Split(v, "\n")); n > m.height {
			t.Fatalf("step %d: %d rows on a %d-row terminal:\n%s", i, n, m.height, v)
		}
		if !strings.Contains(v, "› "+m.matches[m.sel].name) || !strings.Contains(v, " more (↑↓)") {
			t.Fatalf("step %d: selection %s not shown:\n%s", i, m.matches[m.sel].name, v)
		}
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	// When every match fits, there is no "more" line.
	m.in.SetValue("/st")
	m.refreshMatches()
	if v := m.menuView(); strings.Contains(v, "more") || !strings.Contains(v, "/status") || !strings.Contains(v, "/styles") {
		t.Fatalf("short menu:\n%s", v)
	}
}

// The small things of #30: the /help key column never runs into its text; no
// "pictures still attached" without pictures; /script expands ~; an auth
// failure while watching drops the dead job; /status escapes its id (#20).
func TestShellOddsAndEnds(t *testing.T) {
	for _, l := range strings.Split(ansi.Strip(helpText()), "\n") {
		if strings.HasPrefix(l, "  ↑↓ Enter") && !strings.HasPrefix(l, "  ↑↓ Enter  ") {
			t.Fatalf("help key column runs into its text: %q", l)
		}
		if strings.Contains(l, "g · d") || strings.Contains(l, "· g") {
			t.Fatalf("help still offers letter keys: %q", l)
		}
	}

	m, srv := against(t)
	srv.On("POST", "/videos", apitest.Answer{Status: "500", Raw: `{"error": "boom", "type": "engine", "code": "internal"}`})
	_, cmd := m.Update(find[createdMsg](t, say(m, "a video about tea")))
	if out := printed(msgs(cmd)); strings.Contains(out, "pictures") || !strings.Contains(out, "Nothing was charged.") {
		t.Fatalf("no pictures, but: %q", out)
	}
	m.images = []attach.Image{{Path: filepath.Join(t.TempDir(), "gone.png"), Format: "png"}}
	_, cmd = m.Update(find[createdMsg](t, say(m, "a video about tea")))
	if out := printed(msgs(cmd)); !strings.Contains(out, "Your pictures are still attached") {
		t.Fatalf("with a picture: %q", out)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, "script.txt"), []byte("A short script about tea."), 0o600); err != nil {
		t.Fatal(err)
	}
	m, srv = against(t)
	m.in.SetValue("/script ~/script.txt")
	_, cmd = m.handleEnter()
	find[createdMsg](t, msgs(cmd))
	if post, _ := srv.Last("POST", "/videos"); post.Body["brief"] != "A short script about tea." {
		t.Fatalf("script body %v", post.Body)
	}

	m, _ = against(t)
	m.job = &job{taskID: taskID, started: time.Now(), phase: "generating"}
	m.stage = stageMaking
	_, cmd = m.Update(pollMsg{taskID: taskID, untilDone: true, err: clierr.Auth("Invalid API key.")})
	out := printed(msgs(cmd))
	if m.job != nil || m.stage != stageIdle || !strings.Contains(out, "zebracat video status "+taskID[:9]) || !m.keyRejected {
		t.Fatalf("auth failure while watching: job %v stage %v rejected %v\n%s", m.job, m.stage, m.keyRejected, out)
	}

	m, srv = against(t)
	m.in.SetValue("/status t?x=1")
	_, cmd = m.handleEnter()
	msgs(cmd)
	if c, _ := srv.Last("GET", ""); c.Path != "/api/v1/public/videos/t?x=1" || c.Query != "" {
		t.Fatalf("/status sent path %q query %q", c.Path, c.Query)
	}
}
