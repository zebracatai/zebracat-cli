package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/version"
)

// cobra's own errors are usage errors: exit 2, code usage_error.
func TestCobraErrorsExitTwo(t *testing.T) {
	prepareCommands()
	for name, args := range map[string][]string{
		"unknown flag":        {"auth", "whoami", "--nosuch"},
		"unknown shorthand":   {"version", "-Z"},
		"missing argument":    {"config", "set", "output"},
		"extra argument":      {"version", "now"},
		"unknown command":     {"nosuchcmd"},
		"unknown subcommand":  {"auth", "nosuchsub"},
		"bad flag value":      {"video", "create", "--seconds", "abc"},
		"subcommand of group": {"video", "nosuchsub"},
	} {
		setup(t)
		r := run(t, args...)
		if r.exit() != clierr.CodeUsage || r.err.Code != "usage_error" || r.err.Type != clierr.TypeInvalidRequest {
			t.Errorf("%s %v: exit %d err %+v", name, args, r.exit(), r.err)
		}
	}
}

// An unknown command names the parent and suggests the near miss.
func TestUnknownCommandSuggests(t *testing.T) {
	prepareCommands()
	setup(t)
	r := run(t, "auth", "whoamj")
	if r.exit() != 2 || !strings.Contains(r.err.Message, `"whoamj"`) || !strings.Contains(r.err.Hint, "zebracat auth whoami") {
		t.Fatalf("%+v", r.err)
	}
	// A group with no subcommand still shows its help.
	if r := run(t, "auth"); r.exit() != 0 || !strings.Contains(r.stdout, "whoami") {
		t.Fatalf("exit %d: %s", r.exit(), r.stdout)
	}
}

// writeCreds stores a key file and returns its exact bytes.
func writeCreds(t *testing.T, key string) (string, []byte) {
	t.Helper()
	dir := filepath.Join(os.Getenv("HOME"), ".zebracat")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "credentials.json")
	b := []byte(`{"api_key": "` + key + `"}`)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, b
}

// accountAnswers serves /account: 200 for sk-good, the given answer otherwise.
func accountAnswers(srvHandle func(string, http.HandlerFunc), status int, body string) *[]string {
	var keys []string
	srvHandle("/api/v1/public/account", func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("X-API-KEY"))
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-API-KEY") == "sk-good" {
			_, _ = w.Write([]byte(`{"email":"me@example.com"}`))
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	return &keys
}

// A key that fails the check is never saved: the stored file stays byte for byte.
func TestFailedLoginKeepsTheStoredKey(t *testing.T) {
	srv, _ := setup(t)
	keys := accountAnswers(srv.Handle, 401, `{"error":"Invalid API key.","code":"invalid_api_key","type":"authentication","retryable":false}`)
	path, before := writeCreds(t, "sk-good")

	r := run(t, "auth", "login", "--api-key", "sk-bad")
	if r.exit() != clierr.CodeAuth || !strings.Contains(r.err.Message, "rejected") || !strings.Contains(r.err.Hint, "unchanged") {
		t.Fatalf("exit %d %+v", r.exit(), r.err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatalf("credentials.json changed:\n%s\n→\n%s", before, after)
	}
	// It checked the key being logged in, not ZEBRACAT_API_KEY (sk-dummy).
	if len(*keys) != 1 || (*keys)[0] != "sk-bad" {
		t.Fatalf("checked keys %v", *keys)
	}

	// No network: nothing saved either, and the error says it could not check.
	srv.Handle("/api/v1/public/account", func(w http.ResponseWriter, r *http.Request) {
		hj, _ := w.(http.Hijacker)
		c, _, _ := hj.Hijack()
		c.Close()
	})
	r = run(t, "auth", "login", "--api-key", "sk-other")
	if r.exit() != 1 || r.err.Code != "network_error" || !strings.Contains(r.err.Message, "could not check") {
		t.Fatalf("exit %d %+v", r.exit(), r.err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("credentials.json changed on a network error")
	}
}

// A key that works is saved (from stdin here), JSON only on stdout.
func TestLoginSavesAWorkingKey(t *testing.T) {
	srv, _ := setup(t)
	accountAnswers(srv.Handle, 401, `{}`)
	path, _ := writeCreds(t, "sk-old")
	stdin = strings.NewReader("sk-good\n")
	r := run(t, "auth", "login")
	if r.exit() != 0 || r.JSON(t)["email"] != "me@example.com" || strings.Contains(r.stderr, "✓") {
		t.Fatalf("exit %d out %s err %s", r.exit(), r.stdout, r.stderr)
	}
	var saved map[string]any
	b, _ := os.ReadFile(path)
	_ = json.Unmarshal(b, &saved)
	if saved["api_key"] != "sk-good" {
		t.Fatalf("saved %s", b)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
}

// The stub's failure answers (reports/cli-audit/stub/stub.py) give an
// envelope with code, message, type and retryable, and the right exit code.
func TestStubFailuresGiveTheFullEnvelope(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		ctype     string
		body      string
		exit      int
		typ       string
		retryable bool
	}{
		{"401", 401, "application/json", `{"error":"Invalid API key.","code":"authentication_required","type":"authentication","retryable":false,"request_id":"req_stub123"}`, 3, "authentication", false},
		{"403", 403, "application/json", `{"detail":"You do not have permission to perform this action."}`, 3, "permission", false},
		{"429", 429, "application/json", `{"error":"You have reached today's plan limit.","code":"daily_limit","type":"rate_limited","retryable":true,"hint":"Try again tomorrow.","request_id":"req_stub123"}`, 1, "rate_limited", true},
		{"502 html", 502, "text/html", `<html><head><title>502 Bad Gateway</title></head><body>nginx</body></html>`, 1, "unavailable", true},
		{"no network", 0, "", "", 1, "unavailable", true},
	}
	for _, c := range cases {
		srv, _ := setup(t)
		srv.Handle("/api/v1/public/account", func(w http.ResponseWriter, r *http.Request) {
			if c.status == 0 {
				hj, _ := w.(http.Hijacker)
				conn, _, _ := hj.Hijack()
				conn.Close()
				return
			}
			w.Header().Set("Content-Type", c.ctype)
			w.Header().Set("X-Request-ID", "req_stub123")
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(c.status)
			_, _ = io.WriteString(w, c.body)
		})
		r := run(t, "auth", "whoami")
		if r.exit() != c.exit || r.err.Type != c.typ || r.err.Retryable != c.retryable {
			t.Errorf("%s: exit %d %+v", c.name, r.exit(), r.err)
			continue
		}
		// What Execute prints off a terminal: the JSON envelope on stderr.
		out := captureStderr(t, func() { printError(r.err) })
		var env struct {
			Error map[string]any `json:"error"`
		}
		if err := json.Unmarshal([]byte(out), &env); err != nil {
			t.Fatalf("%s: stderr is not JSON: %v\n%s", c.name, err, out)
		}
		for _, k := range []string{"code", "message", "type", "retryable"} {
			if _, ok := env.Error[k]; !ok {
				t.Errorf("%s: envelope has no %q: %s", c.name, k, out)
			}
		}
		if c.status != 0 && env.Error["request_id"] != "req_stub123" {
			t.Errorf("%s: request_id %v", c.name, env.Error["request_id"])
		}
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	done := make(chan string)
	go func() { b, _ := io.ReadAll(r); done <- string(b) }()
	fn()
	w.Close()
	os.Stderr = old
	return <-done
}

// `zebracat commands --json` lists every command and marks the paid ones.
func TestCommandsJSONListsEveryCommand(t *testing.T) {
	setup(t)
	r := run(t, "commands", "--json")
	if r.exit() != 0 {
		t.Fatalf("exit %d: %s", r.exit(), r.stderr)
	}
	var out struct {
		Version  string `json:"version"`
		Commands []struct {
			Name     string `json:"name"`
			Group    string `json:"group"`
			Paid     bool   `json:"paid"`
			NeedsYes bool   `json:"needs_yes"`
			Flags    []struct {
				Name string   `json:"name"`
				Enum []string `json:"enum"`
			} `json:"flags"`
		} `json:"commands"`
		ExitCodes map[string]string `json:"exit_codes"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil {
		t.Fatal(err)
	}
	if out.Version != version.Version || len(out.ExitCodes) != 5 {
		t.Fatalf("version %q exit codes %v", out.Version, out.ExitCodes)
	}
	type row struct {
		Name     string `json:"name"`
		Group    string `json:"group"`
		Paid     bool   `json:"paid"`
		NeedsYes bool   `json:"needs_yes"`
	}
	var rows []row
	enumSeen := false
	for _, c := range out.Commands {
		rows = append(rows, row{c.Name, c.Group, c.Paid, c.NeedsYes})
		for _, f := range c.Flags {
			if c.Name == "video create" && f.Name == "media-type" && len(f.Enum) > 0 {
				enumSeen = true
			}
		}
	}
	if !enumSeen {
		t.Error("video create --media-type has no enum")
	}
	golden(t, "commands_list", rows)
}

// Every top-level command is in the one table of help.go, and every row of
// the table names a real command (so a rename can't leave a stale row).
func TestEveryCommandIsInTheTable(t *testing.T) {
	prepareCommands()
	for _, c := range rootCmd.Commands() {
		if c.IsAvailableCommand() || c.Name() == "help" {
			if groupOf(c.Name()) == "" {
				t.Errorf("top-level %q has no row in commandTable (cmd/help.go)", c.Name())
			}
		}
	}
	for _, r := range commandTable {
		c, rest, err := rootCmd.Find(strings.Fields(r.Command))
		if err != nil || len(rest) != 0 || strings.TrimPrefix(c.CommandPath(), "zebracat ") != r.Command {
			t.Errorf("commandTable row %q names no command", r.Command)
		}
	}
	var paid []string
	for _, r := range commandTable {
		if r.Paid {
			paid = append(paid, r.Command)
		}
	}
	sort.Strings(paid)
	want := "create image post schedule script swap video clone video create video generate video translate voice clone"
	if got := strings.Join(paid, " "); got != want {
		t.Fatalf("paid = %s", got)
	}
}

// Root help shows the product's categories (G1), in order, then Legacy and
// This CLI.
func TestRootHelpIsGrouped(t *testing.T) {
	setup(t)
	r := run(t, "--help")
	at := -1
	for _, g := range []string{"Videos:", "Remake a video:", "Inspiration:", "Catalogs:", "Library:", "Publish:",
		"Account:", "Webhooks:", "Legacy:", "This CLI:"} {
		i := strings.Index(r.stdout, "\n"+g+"\n")
		if i < 0 || i < at {
			t.Fatalf("group %q missing or out of order:\n%s", g, r.stdout)
		}
		at = i
	}
	if strings.Contains(r.stdout, "\n  login ") {
		t.Fatalf("the duplicate login is listed:\n%s", r.stdout)
	}
	if strings.Contains(r.stdout, "Additional Commands:") || strings.Contains(r.stdout, "plan credits") {
		t.Fatalf("ungrouped commands or stale text:\n%s", r.stdout)
	}
}

// --version is one line; doctor's problems is a list, never null; update
// --check always says update_available.
func TestStableJSONKeys(t *testing.T) {
	setup(t)
	if r := run(t, "--version"); r.stdout != "zebracat "+version.Version+"\n" {
		t.Fatalf("--version = %q", r.stdout)
	}

	t.Setenv("PATH", t.TempDir()) // no other zebracat on PATH
	fakeLatest(t, "v"+version.Version)
	r := run(t, "doctor", "--json")
	if r.exit() != 0 {
		t.Fatalf("doctor exit %d: %s", r.exit(), r.stderr)
	}
	if !strings.Contains(r.stdout, `"problems": []`) {
		t.Fatalf("doctor problems: %s", r.stdout)
	}

	r = run(t, "update", "--check", "--json")
	if v, ok := r.JSON(t)["update_available"]; !ok || v != false {
		t.Fatalf("update --check: %s", r.stdout)
	}
}

// Off a terminal, the ✓ lines stay off stderr: JSON is the whole answer.
func TestNoHumanLinesInJSONMode(t *testing.T) {
	setup(t)
	for _, args := range [][]string{{"auth", "logout"}, {"config", "set", "output", "json"}} {
		r := run(t, args...)
		if r.exit() != 0 || r.stderr != "" {
			t.Fatalf("%v: exit %d stderr %q", args, r.exit(), r.stderr)
		}
	}
}

// D2: one rule for every paid command.
func TestConfirmSpend(t *testing.T) {
	setup(t)
	asked := 0
	q := func() (string, error) { asked++; return "Spend 96 credits?", nil }

	if ok, err := confirmSpend(true, "generate", q); !ok || err != nil || asked != 0 {
		t.Fatalf("--yes: %v %v asked %d", ok, err, asked)
	}
	ok, err := confirmSpend(false, "generate", q)
	ce, _ := err.(*clierr.Error)
	if ok || ce == nil || ce.Exit != 2 || ce.Code != "confirmation_required" || !strings.Contains(ce.Message+ce.Hint, "--yes") || asked != 0 {
		t.Fatalf("off a terminal: %v %+v asked %d", ok, ce, asked)
	}
	interactive = func() bool { return true }
	confirm = func(string) bool { return false }
	if ok, err := confirmSpend(false, "generate", q); ok || err != nil || asked != 1 {
		t.Fatalf("said no: %v %v asked %d", ok, err, asked)
	}
	confirm = func(string) bool { return true }
	if ok, err := confirmSpend(false, "generate", q); !ok || err != nil || asked != 2 {
		t.Fatalf("said yes: %v %v asked %d", ok, err, asked)
	}
}
