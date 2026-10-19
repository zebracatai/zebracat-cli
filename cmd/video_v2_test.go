package cmd

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zebracatai/zebracat-cli/internal/apitest"
)

func TestCreateOneLinerPostsVideosWithIdempotencyKey(t *testing.T) {
	srv, _ := setup(t)
	r := run(t, "video", "create", "why cats knock things off tables", "--seconds", "20")
	if r.exit() != 0 {
		t.Fatalf("exit %d: %s", r.exit(), r.stderr)
	}
	call, ok := srv.Last("POST", "/api/v1/public/videos")
	if !ok {
		t.Fatal("no POST /videos")
	}
	golden(t, "create_one_liner_request", call.Body)
	out := r.JSON(t)
	key, _ := out["idempotency_key"].(string)
	if len(key) != 36 || call.Header.Get("Idempotency-Key") != key {
		t.Fatalf("Idempotency-Key header %q vs printed %q", call.Header.Get("Idempotency-Key"), key)
	}
	out["idempotency_key"] = "<uuid>"
	golden(t, "create_one_liner_output", out)
}

func TestCreateIdempotencyKeyFlagReachesTheHeader(t *testing.T) {
	srv, _ := setup(t)
	r := run(t, "video", "create", "--brief", "x", "--idempotency-key", "my-key-1")
	if r.exit() != 0 {
		t.Fatal(r.stderr)
	}
	call, _ := srv.Last("POST", "/videos")
	if call.Header.Get("Idempotency-Key") != "my-key-1" || r.JSON(t)["idempotency_key"] != "my-key-1" {
		t.Fatalf("header %q, output %v", call.Header.Get("Idempotency-Key"), r.JSON(t)["idempotency_key"])
	}
}

func TestCreateEveryFlagMapsToTheBody(t *testing.T) {
	srv, _ := setup(t)
	r := run(t, "video", "create", "--brief", "a street interview about coffee",
		"--preset", "street_interview", "--media-type", "ai_video", "--audio", "dialogue",
		"--aspect", "horizontal", "--language", "spanish", "--seconds", "45", "--quality", "cinematic",
		"--voice", "v_123", "--style", "77", "--background", "minecraft_parkour_1",
		"--upload", "https://cdn.example.com/me.png:person",
		"--upload", "https://cdn.example.com/cafe.jpg:place",
		"--upload", "https://cdn.example.com/logo.png",
		"--brand", "9", "--plan-only", "--no-render")
	if r.exit() != 0 {
		t.Fatal(r.stderr)
	}
	call, _ := srv.Last("POST", "/videos")
	golden(t, "create_all_flags_request", call.Body)
}

func TestCreateInputKinds(t *testing.T) {
	cases := []struct {
		args []string
		kind string
	}{
		{[]string{"--brief", "x"}, "idea"},
		{[]string{"--brief", "x", "--input", "vo-script"}, "voiceover_script"},
		{[]string{"--brief", "x", "--input", "script-scenes"}, "script_with_scenes"},
		{[]string{"--url", "https://example.com/post"}, "url"},
	}
	for _, c := range cases {
		srv, _ := setup(t)
		r := run(t, append([]string{"video", "create"}, c.args...)...)
		if r.exit() != 0 {
			t.Fatalf("%v: %s", c.args, r.stderr)
		}
		call, _ := srv.Last("POST", "/videos")
		if call.Body["input_kind"] != c.kind {
			t.Fatalf("%v: input_kind = %v, want %s", c.args, call.Body["input_kind"], c.kind)
		}
	}
}

func TestCreateScriptAndFileReadTheirFiles(t *testing.T) {
	srv, _ := setup(t)
	dir := t.TempDir()
	path := dir + "/script.txt"
	writeFile(t, path, "Line one.\nLine two.\n")
	r := run(t, "video", "create", "--script", path)
	if r.exit() != 0 {
		t.Fatal(r.stderr)
	}
	call, _ := srv.Last("POST", "/videos")
	if call.Body["brief"] != "Line one.\nLine two." || call.Body["input_kind"] != "script" {
		t.Fatalf("body = %v", call.Body)
	}
	stdin = strings.NewReader("an idea from stdin")
	r = run(t, "video", "create", "--file", "-")
	call, _ = srv.Last("POST", "/videos")
	if r.exit() != 0 || call.Body["brief"] != "an idea from stdin" || call.Body["input_kind"] != "idea" {
		t.Fatalf("stdin brief: %v %s", call.Body, r.stderr)
	}
}

func TestCreateDryRunSendsDryRunAndPrintsTheQuote(t *testing.T) {
	srv, _ := setup(t)
	r := run(t, "video", "create", "--brief", "x", "--plan-only", "--dry-run", "--wait", "--idempotency-key", "k")
	if r.exit() != 0 {
		t.Fatal(r.stderr)
	}
	call, _ := srv.Last("POST", "/videos")
	if call.Body["dry_run"] != true {
		t.Fatalf("dry_run not sent: %v", call.Body)
	}
	if n := srv.Count("GET", "/videos/7c1e9b2a-4f0d-4c55-9a1e-3d2f6b8a9c01"); n != 0 {
		t.Fatalf("a dry run must not wait/poll (%d polls)", n)
	}
	golden(t, "create_dry_run_output", r.JSON(t))
}

func TestCreateDeprecatedFlagsMap(t *testing.T) {
	cases := []struct {
		args  []string
		check func(map[string]any) bool
		note  string
	}{
		{[]string{"--prompt", "x", "--type", "moving_ai_images"}, func(b map[string]any) bool {
			return opt(b, "media_type") == "ai_image_video"
		}, "--type moving_ai_images → --media-type ai_image_video"},
		{[]string{"--prompt", "x", "--footage", "stock_footage"}, func(b map[string]any) bool {
			return opt(b, "media_type") == "stock"
		}, "--footage stock_footage → --media-type stock"},
		{[]string{"--prompt", "x", "--type", "ai_avatar"}, func(b map[string]any) bool {
			return opt(b, "media_type") == "ai_video" && opt(b, "audio_mode") == "dialogue"
		}, "ai_avatar → --media-type ai_video --audio dialogue"},
		{[]string{"--prompt", "x", "--duration", "60"}, func(b map[string]any) bool {
			return opt(b, "seconds") == float64(60)
		}, "--duration has been deprecated"},
		{[]string{"--from", "blog", "--url", "https://example.com/p"}, func(b map[string]any) bool {
			return b["input_kind"] == "url" && b["brief"] == "https://example.com/p"
		}, "--from has been deprecated"},
		{[]string{"--prompt", "x", "--mood", "happy"}, func(b map[string]any) bool {
			_, has := b["mood"]
			return !has
		}, "ignored"},
	}
	for _, c := range cases {
		srv, _ := setup(t)
		r := run(t, append([]string{"video", "create"}, c.args...)...)
		if r.exit() != 0 {
			t.Fatalf("%v: %s", c.args, r.stderr)
		}
		call, _ := srv.Last("POST", "/videos")
		if !c.check(call.Body) {
			t.Fatalf("%v: body = %v", c.args, call.Body)
		}
		mustContain(t, r.stderr, c.note)
	}
}

func TestCreateAudioFileKeepsTheV1Endpoint(t *testing.T) {
	srv, _ := setup(t)
	srv.Handle("/api/v1/public/video/audio", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(202)
		_, _ = w.Write([]byte(`{"task_id":"v1-audio","status":"pending"}`))
	})
	r := run(t, "video", "create", "--audio-file", "https://example.com/talk.mp3", "--language", "english")
	if r.exit() != 0 {
		t.Fatal(r.stderr)
	}
	call, _ := srv.Last("POST", "/video/audio")
	if call.Body["audio_url"] != "https://example.com/talk.mp3" || srv.Count("POST", "/api/v1/public/videos") != 0 {
		t.Fatalf("audio body = %v", call.Body)
	}
}

func TestCreateRejectsValuesOutsideTheSchema(t *testing.T) {
	for _, args := range [][]string{
		{"--brief", "x", "--media-type", "avatar"},
		{"--brief", "x", "--audio", "music"},
		{"--brief", "x", "--input", "blog"},
		{"--brief", "x", "--preset", "nope"},
		{"--brief", "x", "--upload", "not-a-url"},
		{"--seconds", "10"},
	} {
		srv, _ := setup(t)
		r := run(t, append([]string{"video", "create"}, args...)...)
		if r.exit() != 2 {
			t.Fatalf("%v: exit %d, want 2 (%s)", args, r.exit(), r.stderr)
		}
		if srv.Count("POST", "/videos") != 0 {
			t.Fatalf("%v: a usage error must not call the API", args)
		}
	}
}

func TestErrorTypesMapToExitCodes(t *testing.T) {
	cases := []struct {
		status, code, typ string
		exit              int
		retryable         bool
	}{
		{"402", "insufficient_funds", "insufficient_funds", 1, false},
		{"400", "invalid_choice", "invalid_request", 2, false},
		{"401", "invalid_api_key", "authentication", 3, false},
		{"429", "too_many_runs", "rate_limited", 1, true},
	}
	for _, c := range cases {
		srv, _ := setup(t)
		srv.On("POST", "/videos", apitest.Answer{Status: c.status})
		r := run(t, "video", "create", "--brief", "x")
		if r.exit() != c.exit {
			t.Fatalf("%s: exit %d, want %d", c.status, r.exit(), c.exit)
		}
		if r.err.Code != c.code || r.err.Type != c.typ || r.err.Retryable != c.retryable || r.err.RequestID != "req_01HZX3K9QW" {
			t.Fatalf("%s: error = %+v", c.status, r.err)
		}
		env := r.err.Envelope()["error"]
		golden(t, "error_"+c.status, env)
	}
}

func TestInsufficientFundsKeepsTheHintVerbatim(t *testing.T) {
	srv, _ := setup(t)
	srv.On("POST", "/videos/{task_id}/generate", apitest.Answer{Status: "402"})
	r := run(t, "video", "generate", "7c1e9b2a-4f0d-4c55-9a1e-3d2f6b8a9c01", "--yes")
	if r.exit() != 1 || r.err.Hint != "Top up your API balance at https://studio.zebracat.ai/api-keys, then run Generate again." {
		t.Fatalf("exit %d hint %q", r.exit(), r.err.Hint)
	}
}

func opt(b map[string]any, k string) any {
	o, _ := b["options"].(map[string]any)
	return o[k]
}

func TestWaitPlanOnlyStopsAtPlanReady(t *testing.T) {
	srv, clk := setup(t)
	srv.On("GET", "/videos/{task_id}",
		apitest.Answer{Example: "planning"}, apitest.Answer{Example: "planning"}, apitest.Answer{Example: "plan_ready"},
		apitest.Answer{Example: "generating"})
	r := run(t, "video", "create", "--brief", "x", "--plan-only", "--wait", "--idempotency-key", "k")
	if r.exit() != 0 {
		t.Fatal(r.stderr)
	}
	out := r.JSON(t)
	if out["phase"] != "plan_ready" || len(clk.sleeps) != 2 || clk.sleeps[0] != 5*time.Second {
		t.Fatalf("phase %v after sleeps %v", out["phase"], clk.sleeps)
	}
	golden(t, "create_plan_only_wait_output", out)
}

func TestWaitOneCallRunsPastPlanReadyToCompleted(t *testing.T) {
	srv, _ := setup(t)
	srv.On("GET", "/videos/{task_id}",
		apitest.Answer{Example: "planning"}, apitest.Answer{Example: "plan_ready"},
		apitest.Answer{Example: "generating"}, apitest.Answer{Example: "completed"})
	r := run(t, "video", "create", "--brief", "x", "--wait")
	if r.exit() != 0 || r.JSON(t)["phase"] != "completed" {
		t.Fatalf("exit %d, %s", r.exit(), r.stdout)
	}
}

func TestWaitStopsAtEveryPhaseThatNeedsYou(t *testing.T) {
	for _, phase := range []string{"needs_input", "plan_ready", "completed", "failed", "cancelled"} {
		srv, _ := setup(t)
		srv.On("GET", "/videos/{task_id}", apitest.Answer{Example: "planning"}, apitest.Answer{Example: phase})
		r := run(t, "video", "status", "7c1e9b2a-4f0d-4c55-9a1e-3d2f6b8a9c01", "--wait")
		if r.exit() != 0 || r.JSON(t)["phase"] != phase {
			t.Fatalf("%s: exit %d, %s", phase, r.exit(), r.stdout)
		}
	}
}

func TestWaitHonoursRetryAfter(t *testing.T) {
	srv, clk := setup(t)
	srv.On("GET", "/videos/{task_id}",
		apitest.Answer{Status: "429", Header: map[string]string{"Retry-After": "17"}}, apitest.Answer{Example: "completed"})
	r := run(t, "video", "status", "7c1e9b2a-4f0d-4c55-9a1e-3d2f6b8a9c01", "--wait")
	if r.exit() != 0 || len(clk.sleeps) != 1 || clk.sleeps[0] != 17*time.Second {
		t.Fatalf("exit %d sleeps %v (%s)", r.exit(), clk.sleeps, r.stderr)
	}
}

func TestCreateWaitRetriesA429WithTheSameKey(t *testing.T) {
	srv, clk := setup(t)
	srv.On("POST", "/videos",
		apitest.Answer{Status: "429", Header: map[string]string{"Retry-After": "30"}}, apitest.Answer{Status: "202"})
	srv.On("GET", "/videos/{task_id}", apitest.Answer{Example: "plan_ready"})
	r := run(t, "video", "create", "--brief", "x", "--plan-only", "--wait")
	if r.exit() != 0 {
		t.Fatal(r.stderr)
	}
	var keys []string
	for _, c := range srv.Calls() {
		if c.Method == "POST" {
			keys = append(keys, c.Header.Get("Idempotency-Key"))
		}
	}
	if len(keys) != 2 || keys[0] != keys[1] || clk.sleeps[0] != 30*time.Second {
		t.Fatalf("keys %v sleeps %v", keys, clk.sleeps)
	}
	// Without --wait a 429 is answered at once.
	srv.On("POST", "/videos", apitest.Answer{Status: "429"})
	if r := run(t, "video", "create", "--brief", "x"); r.exit() != 1 || r.err.Code != "too_many_runs" {
		t.Fatalf("no-wait 429: %+v", r.err)
	}
}

func TestWaitTimesOutWithExit4(t *testing.T) {
	srv, _ := setup(t)
	srv.On("GET", "/videos/{task_id}", apitest.Answer{Example: "planning"})
	r := run(t, "video", "status", "7c1e9b2a-4f0d-4c55-9a1e-3d2f6b8a9c01", "--wait", "--timeout", "1m")
	if r.exit() != 4 {
		t.Fatalf("exit %d, want 4", r.exit())
	}
	mustContain(t, r.err.Message, "last state: planning")
}

func TestStatusFallsBackToV1ForV1Tasks(t *testing.T) {
	srv, _ := setup(t)
	srv.On("GET", "/videos/{task_id}", apitest.Answer{Status: "404"})
	srv.Handle("/api/v1/public/video/status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"task_id":"old-1","status":"completed","video_type":"idea","video_url":"https://x/y.mp4"}`))
	})
	r := run(t, "video", "status", "old-1")
	if r.exit() != 0 || r.JSON(t)["status"] != "completed" {
		t.Fatalf("exit %d %s %s", r.exit(), r.stdout, r.stderr)
	}
	if c, _ := srv.Last("GET", "/video/status"); c.Query != "task_id=old-1" {
		t.Fatalf("v1 query = %q", c.Query)
	}
}

func writeFile(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}
