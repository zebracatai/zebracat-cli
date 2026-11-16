package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v0.1.0", "v0.1.1", true},
		{"0.1.0", "v0.1.0", false},
		{"v0.1.0", "0.1.0", false},
		{"v0.2.0", "v0.1.9", false},
		{"v1.0.0", "v1.0.1", true},
		{"v0.9.0", "v0.10.0", true}, // numeric, not lexical
		{"v0.1.0", "v0.1.0-rc1", false},
		{"v0.1.0", "", false},
	}
	for _, c := range cases {
		if got := Newer(c.current, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

// fakeGitHub answers /releases/latest with a redirect to tag (and counts hits).
func fakeGitHub(t *testing.T, tag string, delay time.Duration) (*int32, func()) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if delay > 0 {
			time.Sleep(delay)
		}
		if r.URL.Path == "/"+Repo+"/releases/latest" {
			http.Redirect(w, r, "/"+Repo+"/releases/tag/"+tag, http.StatusFound)
			return
		}
		http.NotFound(w, r)
	}))
	old := GitHub
	GitHub = srv.URL
	return &hits, func() { GitHub = old; srv.Close() }
}

func cleanEnv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ZEBRACAT_NO_UPDATE_CHECK", "")
	for _, k := range ciVars {
		t.Setenv(k, "")
	}
}

func TestLatestFollowsTheRedirect(t *testing.T) {
	_, done := fakeGitHub(t, "v0.3.0", 0)
	defer done()
	tag, err := Latest(context.Background())
	if err != nil || tag != "v0.3.0" {
		t.Fatalf("Latest = %q, %v", tag, err)
	}
}

func TestLatestCachedChecksAtMostOnceADay(t *testing.T) {
	cleanEnv(t)
	hits, done := fakeGitHub(t, "v0.3.0", 0)
	defer done()
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	if got := latestCached(context.Background(), now); got != "v0.3.0" || atomic.LoadInt32(hits) != 1 {
		t.Fatalf("first check = %q hits %d", got, *hits)
	}
	if got := latestCached(context.Background(), now.Add(23*time.Hour)); got != "v0.3.0" || atomic.LoadInt32(hits) != 1 {
		t.Fatalf("within a day: %q hits %d (want the cache, no network)", got, *hits)
	}
	if latestCached(context.Background(), now.Add(25*time.Hour)); atomic.LoadInt32(hits) != 2 {
		t.Fatalf("after a day the check runs again; hits %d", *hits)
	}
	p, _ := cachePath()
	if b, err := os.ReadFile(p); err != nil || !strings.Contains(string(b), "v0.3.0") {
		t.Fatalf("cache file: %s %v", b, err)
	}
}

func TestAFailedCheckIsCachedToo(t *testing.T) {
	cleanEnv(t)
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(500)
	}))
	defer srv.Close()
	old := GitHub
	GitHub = srv.URL
	defer func() { GitHub = old }()
	now := time.Now()
	if got := latestCached(context.Background(), now); got != "" {
		t.Fatalf("a failed check has no tag, got %q", got)
	}
	latestCached(context.Background(), now.Add(time.Hour))
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("an offline machine is asked once a day, hits %d", hits)
	}
}

func TestCheckNeverWaitsPastItsTimeout(t *testing.T) {
	cleanEnv(t)
	_, done := fakeGitHub(t, "v9.0.0", 2500*time.Millisecond)
	defer done()
	start := time.Now()
	n := Check("v0.3.0")()
	if took := time.Since(start); took > CheckTimeout+500*time.Millisecond {
		t.Fatalf("the check took %v (cap %v)", took, CheckTimeout)
	}
	if n != "" {
		t.Fatalf("a check that ran out of time prints nothing, got %q", n)
	}
}

func TestCheckPrintsOneLine(t *testing.T) {
	cleanEnv(t)
	_, done := fakeGitHub(t, "v0.3.0", 0)
	defer done()
	if n := Check("v0.2.0")(); n != "Zebracat CLI v0.3.0 is available — run `zebracat update`" {
		t.Fatalf("notice = %q", n)
	}
	if n := Check("v0.3.0")(); n != "" {
		t.Fatalf("up to date prints nothing, got %q", n)
	}
}

func TestCheckIsSkipped(t *testing.T) {
	for _, tc := range []struct{ env, val string }{{"ZEBRACAT_NO_UPDATE_CHECK", "1"}, {"CI", "true"}, {"GITHUB_ACTIONS", "true"}} {
		cleanEnv(t)
		hits, done := fakeGitHub(t, "v9.0.0", 0)
		t.Setenv(tc.env, tc.val)
		if Disabled("v0.2.0") == "" {
			t.Fatalf("%s=%s should disable the check", tc.env, tc.val)
		}
		if n := Check("v0.2.0")(); n != "" || atomic.LoadInt32(hits) != 0 {
			t.Fatalf("%s: notice %q hits %d", tc.env, n, *hits)
		}
		done()
	}
	cleanEnv(t)
	if Disabled("vdev") == "" {
		t.Fatal("a dev build has no version to compare")
	}
	if Disabled("v0.2.0") != "" {
		t.Fatal("a release build outside CI checks")
	}
}

func TestMethodOf(t *testing.T) {
	cases := map[string]string{
		"/usr/local/lib/node_modules/@zebracat/cli/bin/zebracat": "npm",
		"/opt/homebrew/Cellar/zebracat/0.3.0/bin/zebracat":       "brew",
		"/home/me/go/bin/zebracat-cli":                           "go",
		"/home/me/.local/bin/zebracat":                           "binary",
	}
	for p, want := range cases {
		if got := methodOf(p); got != want {
			t.Errorf("methodOf(%s) = %s, want %s", p, got, want)
		}
	}
}

// tarball is a release archive holding a zebracat binary with the given body.
func tarball(t *testing.T, body string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "zebracat", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte(body))
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestApplyToReplacesTheBinaryAndChecksTheSum(t *testing.T) {
	arch := tarball(t, "new-binary")
	sum := sha256.Sum256(arch)
	sums := hex.EncodeToString(sum[:]) + "  zebracat_linux_amd64.tar.gz\n"
	bad := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + Repo + "/releases/download/v0.3.0/zebracat_linux_amd64.tar.gz":
			_, _ = w.Write(arch)
		case "/" + Repo + "/releases/download/v0.3.0/checksums.txt":
			if bad {
				_, _ = w.Write([]byte(strings.Repeat("0", 64) + "  zebracat_linux_amd64.tar.gz\n"))
				return
			}
			_, _ = w.Write([]byte(sums))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := GitHub
	GitHub = srv.URL
	defer func() { GitHub = old }()

	exe := filepath.Join(t.TempDir(), "zebracat")
	_ = os.WriteFile(exe, []byte("old-binary"), 0o755)
	if err := ApplyTo(context.Background(), "v0.3.0", exe, "linux", "amd64"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new-binary" {
		t.Fatalf("binary = %q", b)
	}

	bad = true
	_ = os.WriteFile(exe, []byte("old-binary"), 0o755)
	if err := ApplyTo(context.Background(), "v0.3.0", exe, "linux", "amd64"); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("a bad sum must refuse, got %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old-binary" {
		t.Fatal("a refused download leaves the old binary")
	}
}
