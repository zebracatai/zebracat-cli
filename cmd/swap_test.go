package cmd

import (
	"testing"

	"github.com/zebracatai/zebracat-cli/internal/apitest"
)

func TestSwapAnalysesThenSwapsTheMainPerson(t *testing.T) {
	srv, _ := setup(t)
	r := run(t, "swap", "--video", "https://cdn.example.com/clip.mp4", "--character", "https://cdn.example.com/face.jpg",
		"--consent", "--yes", "--idempotency-key", "sw1")
	if r.exit() != 0 {
		t.Fatal(r.stderr)
	}
	a, _ := srv.Last("POST", "/character-swap/analyze")
	if a.Body["video_url"] != "https://cdn.example.com/clip.mp4" {
		t.Fatalf("analyze body %v", a.Body)
	}
	c, _ := srv.Last("POST", "/api/v1/public/character-swap")
	golden(t, "swap_request", c.Body)
	if c.Header.Get("Idempotency-Key") != "sw1" {
		t.Fatal("swap idempotency key missing")
	}
}

func TestSwapDryRunAndGuards(t *testing.T) {
	srv, _ := setup(t)
	r := run(t, "swap", "--video", "https://x.com/c.mp4", "--character", "https://x.com/f.jpg", "--consent", "--dry-run")
	c, _ := srv.Last("POST", "/api/v1/public/character-swap")
	if r.exit() != 0 || c.Body["dry_run"] != true || r.JSON(t)["quote"] == nil {
		t.Fatalf("dry run: exit %d body %v (%s)", r.exit(), c.Body, r.stderr)
	}
	if r := run(t, "swap", "--video", "https://x.com/c.mp4", "--character", "https://x.com/f.jpg", "--yes"); r.exit() != 2 || r.err.Code != "consent_required" {
		t.Fatalf("no consent: %+v", r.err)
	}
	for _, args := range [][]string{
		{"swap", "--video", "https://x.com/c.mp4", "--character", "https://x.com/f.jpg"},              // no consent
		{"swap", "--video", "https://x.com/c.mp4", "--character", "https://x.com/f.jpg", "--consent"}, // no --yes off a TTY
		{"swap", "--video", "x", "--character", "https://x.com/f.jpg", "--consent", "--yes"},
	} {
		before := len(srv.Calls())
		if r := run(t, args...); r.exit() != 2 || len(srv.Calls()) != before {
			t.Fatalf("%v: exit %d, calls %d", args, r.exit(), len(srv.Calls())-before)
		}
	}
	srv.On("POST", "/character-swap", apitest.Answer{Status: "402"})
	if r := run(t, "swap", "--video", "https://x.com/c.mp4", "--character", "https://x.com/f.jpg", "--consent", "--yes"); r.exit() != 1 || r.err.Type != "insufficient_funds" {
		t.Fatalf("402: %+v", r.err)
	}
}

func TestSwapAsksOnATerminal(t *testing.T) {
	srv, _ := setup(t)
	interactive = func() bool { return true }
	var q string
	confirm = func(s string) bool { q = s; return false }
	r := run(t, "swap", "--video", "https://x.com/c.mp4", "--character", "https://x.com/f.jpg", "--consent")
	if r.exit() != 0 || srv.Count("POST", "/api/v1/public/character-swap") != 0 {
		t.Fatalf("declined swap posted: exit %d", r.exit())
	}
	mustContain(t, q, "105 credits")
}

func TestSwapPicksTheMainPerson(t *testing.T) {
	srv, _ := setup(t)
	srv.On("POST", "/character-swap/analyze", apitest.Answer{Status: "200", Raw: `{"analysis_id":"a1","people":[` +
		`{"id":"p1","is_main":false,"selectable":true},{"id":"p2","is_main":true,"selectable":true}],"quote":{"credits":40,"seconds":4}}`})
	run(t, "swap", "--video", "https://x.com/c.mp4", "--character", "https://x.com/f.jpg", "--consent", "--yes")
	if c, _ := srv.Last("POST", "/api/v1/public/character-swap"); c.Body["person"] != "p2" || c.Body["quoted_credits"] != float64(40) {
		t.Fatalf("body %v", c.Body)
	}
	run(t, "swap", "--video", "https://x.com/c.mp4", "--character", "https://x.com/f.jpg", "--consent", "--yes", "--person", "p1")
	if c, _ := srv.Last("POST", "/api/v1/public/character-swap"); c.Body["person"] != "p1" {
		t.Fatalf("--person ignored: %v", c.Body)
	}
}
