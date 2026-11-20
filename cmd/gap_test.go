package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zebracatai/zebracat-cli/internal/apitest"
	"github.com/zebracatai/zebracat-cli/internal/attach"
)

const gapTask = "7c1e9b2a-4f0d-4c55-9a1e-3d2f6b8a9c01"

// #12: a wait survives failed polls (a 502 HTML page, a 503) and keeps going
// with backoff until the video is done.
func TestWaitSurvivesTwoFailedPolls(t *testing.T) {
	srv, clk := setup(t)
	srv.On("GET", "/videos/{task_id}",
		apitest.Answer{Status: "502", Raw: "<html>Bad Gateway</html>"},
		apitest.Answer{Status: "503", Raw: `{"error":"busy","code":"unavailable"}`},
		apitest.Answer{Example: "completed"})
	r := run(t, "video", "status", gapTask, "--wait")
	if r.exit() != 0 || r.JSON(t)["phase"] != "completed" {
		t.Fatalf("exit %d %s %s", r.exit(), r.stdout, r.stderr)
	}
	if len(clk.sleeps) != 2 || clk.sleeps[1] <= clk.sleeps[0] {
		t.Fatalf("backoff sleeps %v", clk.sleeps)
	}
	// A real error (a 401) still ends the wait at once, with no more sleeps.
	n := len(clk.sleeps)
	srv.On("GET", "/videos/{task_id}", apitest.Answer{Status: "401", Raw: `{"error":"Invalid API key.","code":"invalid_api_key","type":"authentication"}`})
	if r := run(t, "video", "status", gapTask, "--wait"); r.exit() != 3 || len(clk.sleeps) != n {
		t.Fatalf("401: exit %d %+v sleeps %v", r.exit(), r.err, clk.sleeps)
	}
}

// fileServer serves the "finished MP4" and records whether the key reached it.
func fileServer(t *testing.T, srv *apitest.Server, body string) (url string, sawKey *bool) {
	t.Helper()
	saw := false
	srv.Handle("/files/v.mp4", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-KEY") != "" {
			saw = true
		}
		_, _ = w.Write([]byte(body))
	})
	return srv.URL + "/files/v.mp4", &saw
}

// G14 + #18: download goes through /videos/{id}/download, reads the redirect
// without sending the key to the file host, writes the whole file, answers
// {task_id, path, bytes}, and keeps an existing file unless --force.
func TestDownloadUsesTheVideosRoute(t *testing.T) {
	srv, _ := setup(t)
	mp4, sawKey := fileServer(t, srv, "MP4DATA")
	srv.Handle("/api/v1/public/videos/"+gapTask+"/download", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, mp4, http.StatusFound)
	})
	dest := filepath.Join(t.TempDir(), "out.mp4")
	r := run(t, "video", "download", gapTask, "-o", dest)
	if r.exit() != 0 {
		t.Fatalf("exit %d %s", r.exit(), r.stderr)
	}
	out := r.JSON(t)
	if out["path"] != dest || out["bytes"] != float64(7) {
		t.Fatalf("out %v", out)
	}
	if b, _ := os.ReadFile(dest); string(b) != "MP4DATA" || *sawKey {
		t.Fatalf("file %q, key sent to the file host: %v", b, *sawKey)
	}
	if r := run(t, "video", "download", gapTask, "-o", dest); r.exit() != 2 || r.err.Code != "file_exists" {
		t.Fatalf("existing file: exit %d %+v", r.exit(), r.err)
	}
	if r := run(t, "video", "download", gapTask, "-o", dest, "--force"); r.exit() != 0 {
		t.Fatalf("--force: exit %d", r.exit())
	}
	// Without -o it prints the URL; -o <folder> saves <task_id>.mp4 there.
	if r := run(t, "video", "download", gapTask); r.JSON(t)["video_url"] != mp4 {
		t.Fatalf("url: %s", r.stdout)
	}
	dir := t.TempDir()
	if r := run(t, "video", "download", gapTask, "-o", dir); r.exit() != 0 {
		t.Fatalf("folder: exit %d", r.exit())
	}
	if _, err := os.Stat(filepath.Join(dir, gapTask+".mp4")); err != nil {
		t.Fatal(err)
	}
}

// A server without /videos/{id}/download: the old route's redirect is used.
func TestDownloadFallsBackToTheOldRoute(t *testing.T) {
	srv, _ := setup(t)
	mp4, _ := fileServer(t, srv, "OLD")
	srv.On("GET", "/video/{task_id}/download", apitest.Answer{Status: "302", Raw: " ", Header: map[string]string{"Location": mp4}})
	if r := run(t, "video", "download", gapTask); r.exit() != 0 || r.JSON(t)["video_url"] != mp4 {
		t.Fatalf("exit %d %s %s", r.exit(), r.stdout, r.stderr)
	}
}

// #18: a cut connection leaves no file and no temporary file behind.
func TestDownloadCutLeavesNoFile(t *testing.T) {
	srv, _ := setup(t)
	srv.Handle("/files/cut.mp4", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte("only a little"))
		hj, _ := w.(http.Hijacker)
		conn, _, _ := hj.Hijack()
		conn.Close()
	})
	srv.Handle("/api/v1/public/videos/"+gapTask+"/download", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/files/cut.mp4", http.StatusFound)
	})
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.mp4")
	r := run(t, "video", "download", gapTask, "-o", dest)
	if r.exit() == 0 {
		t.Fatal("a cut download succeeded")
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("left behind: %v", ents)
	}
}

// #4: voice list answers 50 compact rows by default (well under 100 KB) out of
// the nested catalog; --search narrows it, --all lists everything.
func TestVoiceListIsSmallByDefault(t *testing.T) {
	srv, _ := setup(t)
	data := map[string]any{}
	for l := 0; l < 20; l++ {
		lang := fmt.Sprintf("lang%02d", l)
		regions := map[string]any{}
		var vs []any
		for i := 0; i < 100; i++ {
			vs = append(vs, map[string]any{"voice_id": fmt.Sprintf("%s-%d", lang, i), "name": fmt.Sprintf("Voice %d", i),
				"gender": "female", "accent": "neutral", "description": strings.Repeat("calm warm narration ", 20),
				"labels": map[string]any{"big": strings.Repeat("x", 500)}})
		}
		vs[7].(map[string]any)["description"] = "a pirate captain"
		regions["Region"] = vs
		data[lang] = regions
	}
	raw, _ := json.Marshal(map[string]any{"data": data})
	srv.On("GET", "/voices", apitest.Answer{Status: "200", Raw: string(raw)})

	r := run(t, "voice", "list")
	out := r.JSON(t)
	if r.exit() != 0 || out["count"] != float64(50) || out["total"] != float64(2000) || len(r.stdout) > 100_000 {
		t.Fatalf("exit %d count %v total %v size %d", r.exit(), out["count"], out["total"], len(r.stdout))
	}
	first := out["voices"].([]any)[0].(map[string]any)
	if first["language"] != "lang00" || first["region"] != "Region" || first["labels"] != nil {
		t.Fatalf("row %v", first)
	}
	if out := run(t, "voice", "list", "--search", "pirate captain").JSON(t); out["total"] != float64(20) {
		t.Fatalf("search total %v", out["total"])
	}
	if out := run(t, "voice", "list", "--all").JSON(t); out["count"] != float64(2000) || out["truncated"] != false {
		t.Fatalf("--all count %v", out["count"])
	}
}

func tmpFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// mp4Bytes is the start of an MP4 file (an ftyp box).
var mp4Bytes = append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 2, 0, 'i', 's', 'o', 'm', 'm', 'p', '4', '1'}, bytes.Repeat([]byte{1}, 64)...)

// linkServer answers POST /uploads/link and takes the file at the link.
func linkServer(t *testing.T, srv *apitest.Server, method string) (got *[]byte, sawKey *bool, linkBody *map[string]any) {
	t.Helper()
	var data []byte
	var key bool
	var lb map[string]any
	srv.Handle(attach.LinkRoute, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&lb)
		_, _ = fmt.Fprintf(w, `{"upload_url":%q,"url":"https://cdata.example.com/u/clip.mp4","method":%q}`, srv.URL+"/put/abc", method)
	})
	srv.Handle("/put/abc", func(w http.ResponseWriter, r *http.Request) {
		key = r.Header.Get("X-API-KEY") != ""
		if r.Method == "PUT" {
			data, _ = io.ReadAll(r.Body)
		} else {
			_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			mr := multipart.NewReader(r.Body, params["boundary"])
			for p, err := mr.NextPart(); err == nil; p, err = mr.NextPart() {
				if p.FormName() == "file" {
					data, _ = io.ReadAll(p)
				}
			}
		}
		w.WriteHeader(204)
	})
	return &data, &key, &lb
}

// G19: `upload` sends a picture to POST /uploads with its kind and name, and a
// video through the one-time upload link (the link never gets the API key).
func TestUploadAnyMediaFile(t *testing.T) {
	srv, _ := setup(t)
	var form map[string]string
	srv.Handle("/api/v1/public/uploads", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		form = map[string]string{"kind": r.FormValue("kind"), "name": r.FormValue("name")}
		if _, _, err := r.FormFile("file"); err != nil {
			t.Errorf("no file part: %v", err)
		}
		_, _ = w.Write([]byte(`{"url":"https://cdata.example.com/u/mug.png","kind_hint":"product"}`))
	})
	png := tmpFile(t, "mug.png", pngBytes(t))
	r := run(t, "upload", png, "--kind", "product", "--name", "Mug")
	if r.exit() != 0 || r.JSON(t)["url"] != "https://cdata.example.com/u/mug.png" || form["kind"] != "product" || form["name"] != "Mug" {
		t.Fatalf("picture: exit %d %s form %v", r.exit(), r.stdout, form)
	}

	for _, method := range []string{"PUT", "POST"} {
		got, sawKey, lb := linkServer(t, srv, method)
		clip := tmpFile(t, "clip.mp4", mp4Bytes)
		r = run(t, "upload", clip)
		if r.exit() != 0 || r.JSON(t)["url"] != "https://cdata.example.com/u/clip.mp4" {
			t.Fatalf("%s video: exit %d %s %s", method, r.exit(), r.stdout, r.stderr)
		}
		if !bytes.Equal(*got, mp4Bytes) || *sawKey || (*lb)["content_type"] != "video/mp4" || (*lb)["size"] != float64(len(mp4Bytes)) {
			t.Fatalf("%s: bytes %d key %v link body %v", method, len(*got), *sawKey, *lb)
		}
	}
	txt := tmpFile(t, "notes.txt", []byte("hello"))
	if r := run(t, "upload", txt); r.exit() != 2 {
		t.Fatalf("a text file: exit %d", r.exit())
	}
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	// 1×1 PNG
	return []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00\x90wS\xde\x00\x00\x00\x0cIDATx\x9cc\xf8\xcf\xc0\x00\x00\x03\x01\x01\x00\xc9\xfe\x92\xef\x00\x00\x00\x00IEND\xaeB`\x82")
}

// A local file where a URL is taken is uploaded first (clone --url, create --upload).
func TestLocalFilesAreUploadedFirst(t *testing.T) {
	srv, _ := setup(t)
	linkServer(t, srv, "PUT")
	clip := tmpFile(t, "ref.mp4", mp4Bytes)
	r := run(t, "video", "clone", "--url", clip, "--yes")
	c, _ := srv.Last("POST", "/video/clone")
	if r.exit() != 0 || c.Body["video_url"] != "https://cdata.example.com/u/clip.mp4" {
		t.Fatalf("clone: exit %d body %v %s", r.exit(), c.Body, r.stderr)
	}
	r = run(t, "video", "create", "--brief", "x", "--no-wait", "--upload", clip+":place")
	post, _ := srv.Last("POST", "/videos")
	ups, _ := post.Body["uploads"].([]any)
	if r.exit() != 0 || len(ups) != 1 || ups[0].(map[string]any)["url"] != "https://cdata.example.com/u/clip.mp4" || ups[0].(map[string]any)["kind"] != "place" {
		t.Fatalf("create --upload: exit %d %v %s", r.exit(), post.Body["uploads"], r.stderr)
	}
}

// D2: every paid command refuses off a terminal without --yes, before any call.
func TestEveryPaidCommandNeedsYes(t *testing.T) {
	clip := "https://example.com/v.mp4"
	for _, args := range [][]string{
		{"video", "generate", gapTask},
		{"video", "clone", "--url", clip},
		{"video", "translate", "--url", clip, "--to", "es"},
		{"swap", "--video", clip, "--character", "https://example.com/f.png", "--consent"},
		{"voice", "clone", "--name", "Me", "--audio", "https://example.com/a.mp3"},
		{"script", "an idea"},
		{"image", "a cat"},
		{"post", "schedule", "--platform", "tiktok", "--caption", "hi", "--video-url", clip},
		{"create", "--audio-file", "https://example.com/a.mp3"},
		{"api", "POST", "/videos", "-d", "{}"},
	} {
		srv, _ := setup(t)
		r := run(t, args...)
		if r.exit() != 2 || r.err.Code != "confirmation_required" || !strings.Contains(r.err.Message+r.err.Hint, "--yes") {
			t.Fatalf("%v: exit %d %+v", args, r.exit(), r.err)
		}
		if n := len(srv.Calls()); n != 0 {
			t.Fatalf("%v: %d API calls before the refusal", args, n)
		}
	}
}

// G9: `create` and `video create` are one command: the same help, the same body.
func TestCreateAndVideoCreateAreOneCommand(t *testing.T) {
	setup(t)
	a := run(t, "create", "--help").stdout
	b := run(t, "video", "create", "--help").stdout
	if a == "" || strings.ReplaceAll(b, "zebracat video create", "zebracat create") != a {
		t.Fatalf("help differs:\n--- create\n%s\n--- video create\n%s", a, b)
	}
	var bodies []map[string]any
	for _, prefix := range [][]string{{"create"}, {"video", "create"}} {
		srv, _ := setup(t)
		noShell(t)
		srv.On("GET", "/videos/{task_id}", apitest.Answer{Example: "plan_ready"})
		r := run(t, append(prefix, "a tea video", "--seconds", "20", "--json")...)
		post, _ := srv.Last("POST", "/videos")
		if r.exit() != 0 || r.JSON(t)["phase"] != "plan_ready" || srv.Count("POST", "/generate") != 0 {
			t.Fatalf("%v: exit %d %s", prefix, r.exit(), r.stdout)
		}
		bodies = append(bodies, post.Body)
	}
	ja, _ := json.Marshal(bodies[0])
	jb, _ := json.Marshal(bodies[1])
	if string(ja) != string(jb) || bodies[0]["auto_generate"] != false {
		t.Fatalf("bodies differ: %s vs %s", ja, jb)
	}
}

// G8: every command name of the 0.7.0 help (cli-audit's 71 pages) still runs.
func TestOldCommandNamesStillRun(t *testing.T) {
	setup(t)
	for _, name := range []string{
		"account pricing", "account show", "account usage", "auth login", "auth logout", "auth status", "auth whoami",
		"avatar list", "brand list", "character create", "character list", "chat", "config list", "config set",
		"create", "doctor", "image", "login", "models list", "music list", "plan chat", "plan edit", "plan show",
		"plan undo", "presets list", "script", "style list", "swap", "swap status", "template list", "update",
		"version", "video answer", "video cancel", "video clone", "video create", "video download", "video generate",
		"video get", "video languages", "video list", "video prompt-styles", "video quote", "video status",
		"video translate", "voice clone", "voice list", "webhook create", "webhook delete", "webhook list",
		"commands", "completion",
	} {
		r := run(t, append(strings.Fields(name), "--help")...)
		if r.exit() != 0 || !strings.Contains(r.stdout, "Usage:") {
			t.Errorf("%s --help: exit %d", name, r.exit())
		}
	}
}

// G20, G21, G24 and C2's new commands: each calls its route with the right body.
func TestNewCommandsCallTheirRoutes(t *testing.T) {
	type call struct {
		args         []string
		method, path string
		check        func(apitest.Call) bool
	}
	ok := func(apitest.Call) bool { return true }
	q := func(want string) func(apitest.Call) bool {
		return func(c apitest.Call) bool { return strings.Contains(c.Query, want) }
	}
	body := func(k string, v any) func(apitest.Call) bool {
		return func(c apitest.Call) bool { return fmt.Sprint(c.Body[k]) == fmt.Sprint(v) }
	}
	cases := []call{
		{[]string{"inspiration", "search", "skincare", "routine", "--platform", "tiktok"}, "GET", "/inspiration", func(c apitest.Call) bool {
			return strings.Contains(c.Query, "keyword=skincare+routine") && strings.Contains(c.Query, "platforms=tiktok")
		}},
		{[]string{"inspiration", "transcript", "73000", "--language", "es"}, "GET", "/inspiration/73000/transcript", q("language=es")},
		{[]string{"background", "list", "--category", "minecraft"}, "GET", "/video/backgrounds", q("category=minecraft")},
		{[]string{"video", "types"}, "GET", "/video/types", ok},
		{[]string{"upload", "list", "--limit", "5"}, "GET", "/uploads", q("limit=5")},
		{[]string{"webhook", "test", "12"}, "POST", "/webhooks/12/test", ok},
		{[]string{"webhook", "update", "12", "--active", "false"}, "PATCH", "/webhooks/12", body("is_active", false)},
		{[]string{"webhook", "get", "12"}, "GET", "/webhooks/12", ok},
		{[]string{"template", "resolve", "product-hero", "--set", "subject=our espresso machine"}, "POST", "/templates/resolve",
			func(c apitest.Call) bool {
				a, _ := c.Body["answers"].(map[string]any)
				return c.Body["template"] == "product-hero" && a["subject"] == "our espresso machine"
			}},
		{[]string{"swap", "options"}, "GET", "/character-swap/options", ok},
		{[]string{"account", "usage", "--detail", "--page", "2"}, "GET", "/credit/usage/detail", q("page=2")},
		{[]string{"music", "search", "lofi", "piano", "--max-seconds", "60"}, "POST", "/music/search", body("duration_lte", 60)},
		{[]string{"language", "list", "--translate"}, "GET", "/video/translate/languages", ok},
		{[]string{"video", "translate", "--languages"}, "GET", "/video/translate/languages", ok},
		{[]string{"model", "list"}, "GET", "/video/models", ok},
		{[]string{"preset", "list"}, "GET", "/video/presets", ok},
		{[]string{"post", "connected"}, "GET", "/scheduling/connected", ok},
		{[]string{"post", "connect", "tiktok"}, "POST", "/scheduling/connect", body("platform", "tiktok")},
		{[]string{"post", "list", "--status", "scheduled"}, "GET", "/scheduling/posts/list", q("status=scheduled")},
		{[]string{"post", "get", "311"}, "GET", "/scheduling/posts/311", ok},
		{[]string{"post", "cancel", "311"}, "DELETE", "/scheduling/posts/311", ok},
		{[]string{"post", "schedule", "--platform", "tiktok", "--caption", "hi", "--video-url", "https://example.com/v.mp4", "--at", "2026-12-01T09:00:00Z", "--yes"},
			"POST", "/scheduling/posts", body("schedule_time", "2026-12-01T09:00:00Z")},
		{[]string{"brand", "create", "--name", "Bean Co", "--color", "#111111", "--logo", "https://example.com/l.png"}, "POST", "/brands", body("light_logo_src", "https://example.com/l.png")},
		{[]string{"brand", "update", "7", "--website", "https://bean.co"}, "PATCH", "/brands/7", body("website", "https://bean.co")},
		{[]string{"brand", "delete", "7"}, "DELETE", "/brands/7", ok},
		{[]string{"character", "update", "42", "--description", "a pirate cat"}, "PATCH", "/characters/42", body("description", "a pirate cat")},
		{[]string{"character", "delete", "42"}, "DELETE", "/characters/42", ok},
		{[]string{"character", "suggest", "--input", "a heist with cats", "--style", "263"}, "POST", "/characters/suggest", body("style_id", 263)},
		{[]string{"voice", "rename", "abc", "--name", "Podcast"}, "PATCH", "/voice/clone/abc", body("name", "Podcast")},
		{[]string{"voice", "delete", "abc"}, "DELETE", "/voice/clone/abc", ok},
		{[]string{"voice", "clones"}, "GET", "/voice/clone", ok},
		{[]string{"avatar", "clone", "--image", "https://example.com/me.jpg", "--name", "Me"}, "POST", "/avatars/clone", body("image_url", "https://example.com/me.jpg")},
		{[]string{"avatar", "clone", "status", "812"}, "GET", "/avatars/clone/812", ok},
		{[]string{"api", "GET", "/video/models", "--query", "a=b"}, "GET", "/video/models", q("a=b")},
		{[]string{"video", "create", "--brief", "x", "--no-wait", "--webhook-url", "https://example.com/hook"}, "POST", "/videos", body("webhook_url", "https://example.com/hook")},
		{[]string{"video", "clone", "--url", "https://x.com/v", "--yes", "--webhook-url", "https://example.com/hook"}, "POST", "/video/clone", body("webhook_url", "https://example.com/hook")},
		{[]string{"video", "list", "--status", "a b&x=1"}, "GET", "/projects", q("status=a+b%26x%3D1")},
	}
	for _, c := range cases {
		srv, _ := setup(t)
		srv.Handle("/api/v1/public"+c.path, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) })
		r := run(t, c.args...)
		got, found := srv.Last(c.method, "/api/v1/public"+c.path)
		if r.exit() != 0 || !found || got.Path != "/api/v1/public"+c.path || !c.check(got) {
			t.Errorf("%v: exit %d, %s %s found=%v query=%q body=%v\n%s", c.args, r.exit(), c.method, c.path, found, got.Query, got.Body, r.stderr)
		}
	}
}

// --webhook-url is https only; a bad one is a usage error before any call.
func TestWebhookURLMustBeHTTPS(t *testing.T) {
	for _, args := range [][]string{
		{"video", "create", "--brief", "x", "--webhook-url", "http://example.com/h"},
		{"video", "clone", "--url", "https://x.com/v", "--yes", "--webhook-url", "ftp://x"},
		{"webhook", "create", "--url", "http://example.com/h"},
	} {
		srv, _ := setup(t)
		if r := run(t, args...); r.exit() != 2 || len(srv.Calls()) != 0 {
			t.Fatalf("%v: exit %d calls %d", args, r.exit(), len(srv.Calls()))
		}
	}
}

// #17: music list falls back to the search when the mood list answers 500.
func TestMusicListFallsBackToSearch(t *testing.T) {
	srv, _ := setup(t)
	srv.On("POST", "/music", apitest.Answer{Status: "500", Raw: `{"status": false, "message": "Oops!"}`})
	srv.On("POST", "/music/search", apitest.Answer{Status: "200", Raw: `{"page":1,"total_page":1,"result":[]}`})
	r := run(t, "music", "list", "--mood", "happy")
	c, found := srv.Last("POST", "/music/search")
	if r.exit() != 0 || !found || c.Body["search"] != "happy" {
		t.Fatalf("exit %d found %v body %v %s", r.exit(), found, c.Body, r.stderr)
	}
}

// #19: --to is read against the server's language list.
func TestTranslateReadsTheServersLanguages(t *testing.T) {
	srv, _ := setup(t)
	srv.On("GET", "/video/translate/languages", apitest.Answer{Status: "200", Raw: `{"languages":[{"code":"es","name":"Spanish"},{"code":"tr","name":"Turkish"}],"count":2}`})
	srv.On("POST", "/video/translate", apitest.Answer{Status: "202", Raw: `{"task_id":"tr1","project_id":5,"status":"pending"}`})
	r := run(t, "video", "translate", "--url", "https://example.com/v.mp4", "--to", "Turkish", "--yes")
	c, _ := srv.Last("POST", "/video/translate")
	if r.exit() != 0 || c.Body["target_language"] != "tr" || c.Header.Get("Idempotency-Key") == "" {
		t.Fatalf("exit %d body %v %s", r.exit(), c.Body, r.stderr)
	}
	if r := run(t, "video", "translate", "--url", "https://example.com/v.mp4", "--to", "klingon", "--yes"); r.exit() != 2 || !strings.Contains(r.err.Hint, "tr (Turkish)") {
		t.Fatalf("unknown language: exit %d %+v", r.exit(), r.err)
	}
}

// Server text shown to a person can't carry terminal escapes.
func TestHumanOutputDropsControlCharacters(t *testing.T) {
	srv, _ := setup(t)
	t.Setenv("ZEBRACAT_OUTPUT", "human")
	srv.Handle("/api/v1/public/inspiration", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"videos":[{"id":"1","author":"x\u001b]0;pwned\u0007y"}]}`))
	})
	r := run(t, "inspiration", "search", "cats")
	if r.exit() != 0 || strings.ContainsRune(r.stdout, 0x1b) && strings.Contains(r.stdout, "pwned\a") || strings.ContainsRune(r.stdout, 0x07) {
		t.Fatalf("escape reached the terminal: %q", r.stdout)
	}
}
