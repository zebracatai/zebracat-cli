package cmd

import (
	"net/http"
	"strings"
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

// openapiWith serves a schema listing the given paths at /docs/openapi.json.
func openapiWith(srv *apitest.Server, paths ...string) {
	srv.Handle("/docs/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		b.WriteString(`{"openapi":"3.1.0","paths":{`)
		for i, p := range paths {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(`"` + p + `":{}`)
		}
		b.WriteString("}}")
		_, _ = w.Write([]byte(b.String()))
	})
}

// A server whose schema lists no Character Swap routes is told apart from a 404:
// nothing is analysed or charged.
func TestSwapSaysSoWhenTheServerHasNoSwapAPI(t *testing.T) {
	srv, _ := setup(t)
	openapiWith(srv, "/api/v1/public/videos")
	r := run(t, "swap", "--video", "https://x.com/c.mp4", "--character", "https://x.com/f.jpg", "--consent", "--yes")
	if r.exit() != 1 || r.err == nil || !strings.Contains(r.err.Message, "has no Character Swap API") {
		t.Fatalf("exit %d err %+v", r.exit(), r.err)
	}
	if n := srv.Count("POST", "/api/v1/public/character-swap/analyze"); n != 0 {
		t.Fatalf("analyze sent %d times on a server without swap", n)
	}
	if r := run(t, "swap", "status", "sw_1"); r.exit() != 1 || !strings.Contains(r.err.Message, "has no Character Swap API") {
		t.Fatalf("status: exit %d err %+v", r.exit(), r.err)
	}
}

// With the routes listed the swap goes ahead, and `swap status` reads (and waits on) a job.
func TestSwapStatus(t *testing.T) {
	srv, clk := setup(t)
	openapiWith(srv, "/api/v1/public/character-swap", "/api/v1/public/character-swap/analyze", "/api/v1/public/character-swap/{job_id}")
	srv.Handle("/api/v1/public/character-swap/sw_9", func(w http.ResponseWriter, r *http.Request) {
		if srv.Count("GET", "/api/v1/public/character-swap/sw_9") < 3 {
			_, _ = w.Write([]byte(`{"job_id":"sw_9","status":"processing"}`))
			return
		}
		_, _ = w.Write([]byte(`{"job_id":"sw_9","status":"completed","result":{"video_url":"https://cdn.example.com/s.mp4"}}`))
	})
	r := run(t, "swap", "status", "sw_9")
	if r.exit() != 0 || r.JSON(t)["status"] != "processing" {
		t.Fatalf("status: exit %d %s %s", r.exit(), r.stdout, r.stderr)
	}
	r = run(t, "swap", "status", "sw_9", "--wait")
	if r.exit() != 0 || r.JSON(t)["status"] != "completed" || len(clk.sleeps) != 1 {
		t.Fatalf("wait: exit %d %s sleeps %v", r.exit(), r.stdout, clk.sleeps)
	}
	r = run(t, "swap", "--video", "https://x.com/c.mp4", "--character", "https://x.com/f.jpg", "--consent", "--dry-run")
	if r.exit() != 0 || srv.Count("POST", "/api/v1/public/character-swap/analyze") != 1 {
		t.Fatalf("swap with the routes listed: exit %d %s", r.exit(), r.stderr)
	}
}

// Studio is gone from the CLI: `zebracat studio` is an unknown command, and the
// script/image aliases never call /studio even on a server that still lists it.
func TestNoStudioCommands(t *testing.T) {
	srv, _ := setup(t)
	openapiWith(srv, "/api/v1/public/studio/scripts", "/api/v1/public/studio/images", "/api/v1/public/script_generator")
	if r := run(t, "studio", "script", "x"); r.exit() == 0 {
		t.Fatalf("studio still runs: %s", r.stdout)
	}
	srv.Handle("/api/v1/public/script_generator", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"script":"old script"}`))
	})
	r := run(t, "script", "better sleep", "--duration", "30", "--mood", "calm", "--yes")
	c, _ := srv.Last("POST", "/script_generator")
	if r.exit() != 0 || c.Body["duration"] != float64(30) || c.Body["mood"] != "calm" {
		t.Fatalf("script body %v (%s)", c.Body, r.stderr)
	}
	for _, p := range []string{"/studio/scripts", "/studio/images"} {
		if n := srv.Count("POST", p); n != 0 {
			t.Fatalf("POST %s sent %d times", p, n)
		}
	}
}
