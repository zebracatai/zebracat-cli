package cmd

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zebracatai/zebracat-cli/internal/apitest"
	"github.com/zebracatai/zebracat-cli/internal/client"
)

// noShell fails the test if anything opens the interactive conversation.
func noShell(t *testing.T) {
	t.Helper()
	old := launchShell
	launchShell = func(*client.Client, string, string) error {
		t.Fatal("the interactive shell was opened without a terminal")
		return nil
	}
	t.Cleanup(func() { launchShell = old })
}

// Bare `zebracat` without a terminal never prompts: a usage error, exit 2.
func TestBareZebracatWithoutATerminalNeverPrompts(t *testing.T) {
	srv, _ := setup(t)
	noShell(t)
	for _, args := range [][]string{{}, {"chat"}, {"create"}} {
		r := run(t, args...)
		if r.exit() != 2 {
			t.Fatalf("%v: exit %d, stderr %s", args, r.exit(), r.stderr)
		}
	}
	if n := len(srv.Calls()); n != 0 {
		t.Fatalf("no API call without a terminal, got %d", n)
	}
	r := run(t, []string{}...)
	if r.err == nil || r.err.Code != "needs_terminal" {
		t.Fatalf("code %q", r.err.Code)
	}
}

// `zebracat create "<idea>"` in a script: a plan with its quote, never Generate.
func TestCreateWithoutATerminalWritesAPlanOnly(t *testing.T) {
	srv, _ := setup(t)
	noShell(t)
	srv.On("GET", "/videos/{task_id}", apitest.Answer{Example: "planning"}, apitest.Answer{Example: "plan_ready"})
	r := run(t, "create", "why cats knock things off tables", "--json")
	if r.exit() != 0 {
		t.Fatalf("exit %d: %s", r.exit(), r.stderr)
	}
	post, _ := srv.Last("POST", "/videos")
	if post.Body["auto_generate"] != false || post.Body["input_kind"] != "idea" || post.Body["brief"] != "why cats knock things off tables" {
		t.Fatalf("create body %v", post.Body)
	}
	if srv.Count("POST", "/generate") != 0 {
		t.Fatal("create without --yes never generates")
	}
	out := r.JSON(t)
	delete(out, "idempotency_key") // random per run
	golden(t, "create_conversation_plan_output", out)
}

// A link given to `zebracat create` rides as source_video, like the studio.
func TestCreateWithAVideoLink(t *testing.T) {
	srv, _ := setup(t)
	noShell(t)
	srv.On("GET", "/videos/{task_id}", apitest.Answer{Example: "plan_ready"})
	r := run(t, "create", "https://youtu.be/abc123", "--json")
	if r.exit() != 0 {
		t.Fatalf("exit %d: %s", r.exit(), r.stderr)
	}
	post, _ := srv.Last("POST", "/videos")
	if post.Body["source_video"] != "https://youtu.be/abc123" || post.Body["auto_generate"] != false {
		t.Fatalf("create body %v", post.Body)
	}
}

// --yes is the explicit confirm for scripts: one create that plans and
// generates (auto_generate), waited to the finished video.
func TestCreateYesGeneratesOnce(t *testing.T) {
	srv, _ := setup(t)
	noShell(t)
	srv.On("GET", "/videos/{task_id}", apitest.Answer{Example: "plan_ready"}, apitest.Answer{Example: "plan_ready"},
		apitest.Answer{Example: "generating"}, apitest.Answer{Example: "completed"})
	r := run(t, "create", "a tea video", "--yes")
	if r.exit() != 0 {
		t.Fatalf("exit %d: %s", r.exit(), r.stderr)
	}
	if srv.Count("POST", "/api/v1/public/videos") != 1 || srv.Count("POST", "/generate") != 0 {
		t.Fatalf("creates %d generates %d", srv.Count("POST", "/api/v1/public/videos"), srv.Count("POST", "/generate"))
	}
	if post, _ := srv.Last("POST", "/videos"); post.Body["auto_generate"] != true {
		t.Fatalf("body %v", post.Body)
	}
	if out := r.JSON(t); out["phase"] != "completed" || out["video_url"] == nil {
		t.Fatalf("out %v", out)
	}
}

// A plan that asks a question stops there with exit 0 and the question.
func TestCreateStopsOnAQuestion(t *testing.T) {
	srv, _ := setup(t)
	noShell(t)
	srv.On("GET", "/videos/{task_id}", apitest.Answer{Example: "needs_input"})
	r := run(t, "create", "a tea video", "--json")
	if r.exit() != 0 || r.JSON(t)["phase"] != "needs_input" {
		t.Fatalf("exit %d out %s", r.exit(), r.stdout)
	}
}

// --image uploads the picture to /uploads and sends it with the plan.
func TestCreateWithAnImage(t *testing.T) {
	srv, _ := setup(t)
	noShell(t)
	srv.On("GET", "/videos/{task_id}", apitest.Answer{Example: "plan_ready"})
	srv.Handle("/api/v1/public/uploads", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"url": "https://cdn.example.com/ai-chat/uploads/mug.png", "kind_hint": "person"}`))
	})
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 10, 10)))
	p := filepath.Join(t.TempDir(), "mug.png")
	_ = os.WriteFile(p, b.Bytes(), 0o644)
	r := run(t, "create", "a launch video for this mug", "--image", p+":product", "--json")
	if r.exit() != 0 {
		t.Fatalf("exit %d: %s", r.exit(), r.stderr)
	}
	post, _ := srv.Last("POST", "/videos")
	ups, _ := post.Body["uploads"].([]any)
	if len(ups) != 1 || ups[0].(map[string]any)["kind"] != "product" || ups[0].(map[string]any)["url"] != "https://cdn.example.com/ai-chat/uploads/mug.png" {
		t.Fatalf("uploads %v", post.Body["uploads"])
	}
	_ = os.WriteFile(p+".txt", []byte("hi"), 0o644)
	if r := run(t, "create", "x", "--image", p+".txt", "--json"); r.exit() != 2 || !strings.Contains(r.err.Message, "PNG, JPEG or WebP") {
		t.Fatalf("a text file: exit %d %v", r.exit(), r.err)
	}
}
