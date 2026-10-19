package cmd

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zebracatai/zebracat-cli/internal/apitest"
)

func TestStudioScriptWaitsOnTheSelfLink(t *testing.T) {
	srv, clk := setup(t)
	srv.On("GET", "/studio/scripts/{job_id}", apitest.Answer{Example: "running"}, apitest.Answer{Example: "done"})
	r := run(t, "studio", "script", "3 tips for better sleep", "--seconds", "30", "--language", "english", "--wait", "--idempotency-key", "s1")
	if r.exit() != 0 {
		t.Fatal(r.stderr)
	}
	c, _ := srv.Last("POST", "/studio/scripts")
	golden(t, "studio_script_request", c.Body)
	if c.Header.Get("Idempotency-Key") != "s1" || len(clk.sleeps) != 1 {
		t.Fatalf("key %q sleeps %v", c.Header.Get("Idempotency-Key"), clk.sleeps)
	}
	golden(t, "studio_script_output", r.JSON(t))
}

func TestStudioHooksReadsTheScriptFromStdin(t *testing.T) {
	srv, _ := setup(t)
	stdin = strings.NewReader("Tip one: keep a schedule.\n")
	r := run(t, "studio", "hooks", "--script", "-", "--platform", "youtube")
	c, _ := srv.Last("POST", "/studio/hooks")
	if r.exit() != 0 || c.Body["script"] != "Tip one: keep a schedule." || c.Body["platform"] != "youtube" {
		t.Fatalf("exit %d body %v", r.exit(), c.Body)
	}
	golden(t, "studio_hooks_output", r.JSON(t))
	if r := run(t, "studio", "hooks"); r.exit() != 2 {
		t.Fatalf("missing --script exit %d", r.exit())
	}
}

func TestStudioVoiceDryRunAndDownload(t *testing.T) {
	srv, _ := setup(t)
	dir := t.TempDir()
	text := filepath.Join(dir, "vo.txt")
	writeFile(t, text, "Hello there.")
	r := run(t, "studio", "voice", "--text", text, "--voice", "21m00Tcm4TlvDq8ikWAM", "--dry-run")
	c, _ := srv.Last("POST", "/studio/voices")
	if r.exit() != 0 || c.Body["dry_run"] != true || r.JSON(t)["quote"] == nil {
		t.Fatalf("dry run: exit %d body %v", r.exit(), c.Body)
	}

	srv.Handle("/files/vo.mp3", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ID3audio")) })
	srv.On("GET", "/studio/voices/{job_id}", apitest.Answer{Status: "200",
		Raw: `{"job_id":"vo_77aa","status":"completed","tool":"voice","result":{"audio_url":"` + srv.URL + `/files/vo.mp3"}}`})
	out := filepath.Join(dir, "out.mp3")
	r = run(t, "studio", "voice", "--text", text, "--voice", "v1", "-o", out)
	if r.exit() != 0 {
		t.Fatal(r.stderr)
	}
	if b, _ := os.ReadFile(out); string(b) != "ID3audio" {
		t.Fatalf("saved %q", b)
	}
}

func TestStudioPaidCallsOnAnEmptyBalance(t *testing.T) {
	srv, _ := setup(t)
	srv.On("POST", "/studio/clips", apitest.Answer{Status: "402"})
	r := run(t, "studio", "clip", "a cat surfing", "--seconds", "5")
	if r.exit() != 1 || r.err.Type != "insufficient_funds" {
		t.Fatalf("%+v", r.err)
	}
	if r := run(t, "studio", "clip", "a cat surfing"); r.exit() != 2 {
		t.Fatalf("missing --seconds exit %d", r.exit())
	}
	srv.On("POST", "/studio/clips", apitest.Answer{}) // back to the schema's default answer
	r = run(t, "studio", "clip", "a cat surfing", "--seconds", "5", "--dry-run", "--image", "https://x.com/a.png")
	c, _ := srv.Last("POST", "/studio/clips")
	if r.exit() != 0 || c.Body["dry_run"] != true || c.Body["image_url"] != "https://x.com/a.png" {
		t.Fatalf("clip dry run %v", c.Body)
	}
}

func TestStudioImageAndJob(t *testing.T) {
	srv, _ := setup(t)
	r := run(t, "studio", "image", "a neon city", "--aspect", "vertical", "--count", "2", "--model", "flux")
	c, _ := srv.Last("POST", "/studio/images")
	if r.exit() != 0 || c.Body["count"] != float64(2) || c.Body["aspect"] != "vertical" || c.Body["model"] != "flux" {
		t.Fatalf("image body %v", c.Body)
	}
	if r := run(t, "studio", "image", "x", "--aspect", "diagonal"); r.exit() != 2 {
		t.Fatalf("bad aspect exit %d", r.exit())
	}
	r = run(t, "studio", "job", "image", "med_1a2b")
	if r.exit() != 0 || r.JSON(t)["status"] != "completed" {
		t.Fatalf("job: %s", r.stdout)
	}
	if c, _ := srv.Last("GET", "/studio/media/med_1a2b"); c.Path == "" {
		t.Fatal("studio job image must read /studio/media/{id}")
	}
	if r := run(t, "studio", "job", "podcast", "x"); r.exit() != 2 {
		t.Fatalf("unknown tool exit %d", r.exit())
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

func TestScriptAliasUsesStudioWhenTheServerHasIt(t *testing.T) {
	srv, _ := setup(t)
	openapiWith(srv, "/api/v1/public/studio/scripts", "/api/v1/public/studio/images")
	srv.On("GET", "/studio/scripts/{job_id}", apitest.Answer{Example: "done"})
	r := run(t, "script", "better sleep", "--duration", "30")
	if r.exit() != 0 {
		t.Fatal(r.stderr)
	}
	c, _ := srv.Last("POST", "/studio/scripts")
	if c.Body["idea"] != "better sleep" || c.Body["seconds"] != float64(30) {
		t.Fatalf("studio body %v", c.Body)
	}
	if !strings.HasPrefix(r.JSON(t)["script"].(string), "Tip one") {
		t.Fatalf("the alias must keep a script key: %s", r.stdout)
	}
	r = run(t, "image", "a neon city", "--n", "2")
	if c, _ := srv.Last("POST", "/studio/images"); r.exit() != 0 || c.Body["count"] != float64(2) {
		t.Fatalf("image alias body %v (%s)", c.Body, r.stderr)
	}
	if n := srv.Count("GET", "/docs/openapi.json"); n != 1 {
		t.Fatalf("schema fetched %d times; it is cached for a day", n)
	}
}

func TestScriptAliasKeepsTheOldEndpointOtherwise(t *testing.T) {
	srv, _ := setup(t)
	openapiWith(srv, "/api/v1/public/script_generator")
	srv.Handle("/api/v1/public/script_generator", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"script":"old script"}`))
	})
	r := run(t, "script", "better sleep", "--duration", "30", "--mood", "calm")
	c, _ := srv.Last("POST", "/script_generator")
	if r.exit() != 0 || c.Body["duration"] != float64(30) || c.Body["mood"] != "calm" || srv.Count("POST", "/studio/scripts") != 0 {
		t.Fatalf("legacy body %v (%s)", c.Body, r.stderr)
	}
}
