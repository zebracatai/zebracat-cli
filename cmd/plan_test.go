package cmd

import (
	"strings"
	"testing"

	"github.com/zebracatai/zebracat-cli/internal/apitest"
)

const tid = "7c1e9b2a-4f0d-4c55-9a1e-3d2f6b8a9c01"

func TestPlanShow(t *testing.T) {
	setup(t)
	r := run(t, "plan", "show", tid)
	if r.exit() != 0 {
		t.Fatal(r.stderr)
	}
	golden(t, "plan_show_output", r.JSON(t))
}

func TestPlanEditUsesTheCurrentRevByDefault(t *testing.T) {
	srv, _ := setup(t)
	r := run(t, "plan", "edit", tid, "--tool", "set_duration", "--args", `{"seconds":15}`)
	if r.exit() != 0 {
		t.Fatal(r.stderr)
	}
	call, _ := srv.Last("POST", "/plan/edit")
	golden(t, "plan_edit_request", call.Body)
	if r.JSON(t)["rev"] != float64(4) {
		t.Fatalf("rev = %v", r.JSON(t)["rev"])
	}
	// An explicit --base-rev is sent as is, without reading the plan first.
	before := srv.Count("GET", "/plan")
	run(t, "plan", "edit", tid, "--tool", "set_aspect", "--args", `{"aspect":"square"}`, "--base-rev", "2")
	call, _ = srv.Last("POST", "/plan/edit")
	if call.Body["base_rev"] != float64(2) || srv.Count("GET", "/plan") != before {
		t.Fatalf("base_rev %v", call.Body["base_rev"])
	}
}

func TestPlanEditStaleRevIsAConflict(t *testing.T) {
	srv, _ := setup(t)
	srv.On("POST", "/videos/{task_id}/plan/edit", apitest.Answer{Status: "409"})
	r := run(t, "plan", "edit", tid, "--tool", "set_duration", "--args", `{"seconds":15}`, "--base-rev", "2")
	if r.exit() != 1 || r.err.Type != "conflict" || r.err.Code != "stale_rev" {
		t.Fatalf("%+v", r.err)
	}
}

func TestPlanEditUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"plan", "edit", tid, "--tool", "make_it_pop"},
		{"plan", "edit", tid, "--tool", "set_duration", "--args", "{seconds:15"},
		{"plan", "edit", tid},
	} {
		srv, _ := setup(t)
		if r := run(t, args...); r.exit() != 2 || len(srv.Calls()) != 0 {
			t.Fatalf("%v: exit %d, calls %d", args, r.exit(), len(srv.Calls()))
		}
	}
}

func TestPlanChatAndUndo(t *testing.T) {
	srv, _ := setup(t)
	r := run(t, "plan", "chat", tid, "make it", "funnier")
	if r.exit() != 0 || r.JSON(t)["reply"] == nil {
		t.Fatalf("chat: %s %s", r.stdout, r.stderr)
	}
	if c, _ := srv.Last("POST", "/plan/chat"); c.Body["message"] != "make it funnier" {
		t.Fatalf("chat body %v", c.Body)
	}
	if _, ok := r.JSON(t)["screenplay"]; !ok {
		t.Fatal("the turn answers the new plan flat")
	}
	if r := run(t, "plan", "chat", tid, strings.Repeat("x", 4001)); r.exit() != 2 {
		t.Fatalf("long message exit %d", r.exit())
	}
	r = run(t, "plan", "undo", tid, "--to-rev", "1")
	c, _ := srv.Last("POST", "/plan/undo")
	if r.exit() != 0 || c.Body["base_rev"] != float64(3) || c.Body["to_rev"] != float64(1) {
		t.Fatalf("undo body %v (%s)", c.Body, r.stderr)
	}
}

func TestPlanChatAllowanceIsRetryable(t *testing.T) {
	srv, _ := setup(t)
	srv.On("POST", "/videos/{task_id}/plan/chat", apitest.Answer{Status: "429",
		Raw: `{"error":"Plan-chat allowance used up.","code":"studio_allowance","type":"rate_limited","retryable":true,"retry_after_s":3600,"request_id":"r"}`})
	r := run(t, "plan", "chat", tid, "again")
	if r.exit() != 1 || r.err.Code != "studio_allowance" || !r.err.Retryable || r.err.RetryAfter.Hours() != 1 {
		t.Fatalf("%+v", r.err)
	}
}

func TestVideoQuote(t *testing.T) {
	setup(t)
	r := run(t, "video", "quote", tid)
	if r.exit() != 0 {
		t.Fatal(r.stderr)
	}
	golden(t, "video_quote_output", r.JSON(t))
}

func TestGenerateDryRunSendsDryRunAndNeedsNoYes(t *testing.T) {
	srv, _ := setup(t)
	r := run(t, "video", "generate", tid, "--dry-run", "--idempotency-key", "k")
	if r.exit() != 0 {
		t.Fatal(r.stderr)
	}
	c, _ := srv.Last("POST", "/generate")
	if c.Body["dry_run"] != true {
		t.Fatalf("body %v", c.Body)
	}
	golden(t, "generate_dry_run_output", r.JSON(t))
}

func TestGenerateNeverAsksWhenNotInteractive(t *testing.T) {
	srv, _ := setup(t)
	asked := false
	confirm = func(string) bool { asked = true; return true }
	r := run(t, "video", "generate", tid)
	if r.exit() != 2 || r.err.Code != "confirmation_required" || asked || srv.Count("POST", "/generate") != 0 {
		t.Fatalf("exit %d err %+v asked %v", r.exit(), r.err, asked)
	}
	r = run(t, "video", "generate", tid, "--yes", "--no-render", "--idempotency-key", "gen-1")
	c, _ := srv.Last("POST", "/generate")
	if r.exit() != 0 || asked || c.Body["should_render"] != false || c.Header.Get("Idempotency-Key") != "gen-1" {
		t.Fatalf("--yes: exit %d body %v", r.exit(), c.Body)
	}
}

func TestGenerateOnATerminalShowsTheQuoteAndAsks(t *testing.T) {
	srv, _ := setup(t)
	interactive = func() bool { return true }
	var question string
	confirm = func(q string) bool { question = q; return false }
	r := run(t, "video", "generate", tid)
	if r.exit() != 0 || srv.Count("POST", "/generate") != 0 || r.JSON(t)["generated"] != false {
		t.Fatalf("declined: exit %d posts %d", r.exit(), srv.Count("POST", "/generate"))
	}
	mustContain(t, question, "96 credits ($4.80) from your API balance")
	confirm = func(string) bool { return true }
	srv.On("GET", "/videos/{task_id}", apitest.Answer{Example: "generating"}, apitest.Answer{Example: "completed"})
	r = run(t, "video", "generate", tid, "--wait")
	if r.exit() != 0 || srv.Count("POST", "/generate") != 1 || r.JSON(t)["phase"] != "completed" {
		t.Fatalf("accepted: exit %d %s", r.exit(), r.stdout)
	}
}

func TestVideoAnswer(t *testing.T) {
	srv, _ := setup(t)
	r := run(t, "video", "answer", tid, "--question", "q_audio", "--answer", "Voice-over")
	c, _ := srv.Last("POST", "/answer")
	if r.exit() != 0 || c.Body["question_id"] != "q_audio" || c.Body["answer"] != "Voice-over" {
		t.Fatalf("exit %d body %v", r.exit(), c.Body)
	}
	if r := run(t, "video", "answer", tid, "--question", "q"); r.exit() != 2 {
		t.Fatalf("missing --answer exit %d", r.exit())
	}
}

func TestVideoCloneAndCancel(t *testing.T) {
	srv, _ := setup(t)
	r := run(t, "video", "clone", "--url", "https://www.tiktok.com/@x/video/1", "--mode", "generate_similar",
		"--topic", "home coffee", "--language", "english", "--idempotency-key", "c1")
	if r.exit() != 0 {
		t.Fatal(r.stderr)
	}
	c, _ := srv.Last("POST", "/video/clone")
	golden(t, "clone_request", c.Body)
	if c.Header.Get("Idempotency-Key") != "c1" {
		t.Fatal("clone idempotency key missing")
	}
	if r := run(t, "video", "clone", "--url", "https://x.com/v", "--mode", "copy"); r.exit() != 2 {
		t.Fatalf("bad mode exit %d", r.exit())
	}
	r = run(t, "video", "cancel", tid)
	if r.exit() != 0 || r.JSON(t)["phase"] != "cancelled" {
		t.Fatalf("cancel: %s %s", r.stdout, r.stderr)
	}
}

func TestCatalogues(t *testing.T) {
	setup(t)
	r := run(t, "models", "list")
	if r.exit() != 0 {
		t.Fatal(r.stderr)
	}
	golden(t, "models_list_output", r.JSON(t))
	r = run(t, "presets", "list")
	if r.exit() != 0 || !strings.Contains(r.stdout, "street_interview") {
		t.Fatalf("presets: %s", r.stdout)
	}
}

// The plan-chat turn answers the new plan flat; the human view prints its scenes.
func TestPlanChatHumanPrintsTheNewPlan(t *testing.T) {
	setup(t)
	r := run(t, "--human", "plan", "chat", tid, "make it funnier")
	if r.exit() != 0 {
		t.Fatal(r.stderr)
	}
	mustContain(t, r.stdout, "1. (5s) Ever wondered why your cat swipes your mug off the table?")
}
