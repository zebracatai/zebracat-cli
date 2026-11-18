package cmd

import (
	"net/http"
	"testing"
)

// Owner 2026-11-18: Studio is removed. `zebracat studio …` is not a command any
// more and sends nothing; `script` and `image` use the classic endpoints only.
func TestStudioCommandsAreGone(t *testing.T) {
	srv, _ := setup(t)
	for _, args := range [][]string{
		{"studio", "script", "3 tips for better sleep"},
		{"studio", "voice", "--text", "x", "--voice", "v", "--dry-run"},
		{"studio", "job", "swap", "j1"},
	} {
		before := len(srv.Calls())
		if r := run(t, args...); r.exit() == 0 || len(srv.Calls()) != before {
			t.Fatalf("%v: exit %d, %d calls", args, r.exit(), len(srv.Calls())-before)
		}
	}
	srv.Handle("/api/v1/public/script_generator", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"script":"a script"}`))
	})
	if r := run(t, "script", "better sleep", "--duration", "30"); r.exit() != 0 || srv.Count("POST", "/script_generator") != 1 {
		t.Fatalf("script: exit %d (%s)", r.exit(), r.stderr)
	}
	for _, c := range srv.Calls() {
		if len(c.Path) >= len("/api/v1/public/studio/") && c.Path[:len("/api/v1/public/studio/")] == "/api/v1/public/studio/" {
			t.Fatalf("a Studio route was called: %s %s", c.Method, c.Path)
		}
	}
}

// `swap status` replaces `studio job swap`: it reads (and with --wait follows) a swap job.
func TestSwapStatusReadsTheJob(t *testing.T) {
	srv, _ := setup(t)
	r := run(t, "swap", "status", "j9")
	if r.exit() != 0 || srv.Count("GET", "/character-swap/j9") != 1 {
		t.Fatalf("swap status: exit %d (%s)", r.exit(), r.stderr)
	}
	if r := run(t, "swap", "status", "j9", "--wait"); r.exit() != 0 {
		t.Fatalf("swap status --wait: exit %d (%s)", r.exit(), r.stderr)
	}
}
