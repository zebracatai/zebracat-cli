package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/zebracatai/zebracat-cli/internal/apitest"
	"github.com/zebracatai/zebracat-cli/internal/update"
)

// fakeLatest points the release lookup at a server whose latest is tag.
func fakeLatest(t *testing.T, tag string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/"+update.Repo+"/releases/tag/"+tag, http.StatusFound)
	}))
	old := update.GitHub
	update.GitHub = srv.URL
	t.Cleanup(func() { update.GitHub = old; srv.Close() })
}

func swapUpdate(t *testing.T) (applied *string, relaunched *[]string) {
	t.Helper()
	var a string
	var r []string
	oldA, oldR := applyUpdate, relaunchBinary
	applyUpdate = func(_ context.Context, tag string) (string, error) { a = tag; return "/opt/zebracat", nil }
	relaunchBinary = func(path, from string, args []string) error { r = []string{path, from}; return nil }
	t.Cleanup(func() { applyUpdate, relaunchBinary = oldA, oldR })
	return &a, &r
}

// On a terminal, `zebracat update` installs and starts the new version.
func TestUpdateRelaunchesOnATerminal(t *testing.T) {
	setup(t)
	fakeLatest(t, "v9.0.0")
	applied, relaunched := swapUpdate(t)
	interactive = func() bool { return true }
	r := run(t, "update")
	if r.exit() != 0 || *applied != "v9.0.0" {
		t.Fatalf("exit %d applied %q: %s", r.exit(), *applied, r.stderr)
	}
	if len(*relaunched) != 2 || (*relaunched)[0] != "/opt/zebracat" || (*relaunched)[1] == "" {
		t.Fatalf("relaunch %v", *relaunched)
	}
}

// Scripts, --json and --no-relaunch only update.
func TestUpdateNeverRelaunchesForScripts(t *testing.T) {
	setup(t)
	fakeLatest(t, "v9.0.0")
	_, relaunched := swapUpdate(t)
	if r := run(t, "update", "--json"); r.exit() != 0 || r.JSON(t)["relaunched"] != false {
		t.Fatalf("json: %s %s", r.stdout, r.stderr)
	}
	interactive = func() bool { return true }
	run(t, "update", "--no-relaunch")
	if len(*relaunched) != 0 {
		t.Fatalf("relaunched %v", *relaunched)
	}
}

// The relaunched binary is told which version it replaced, and clears it.
func TestRelaunchCarriesTheOldVersion(t *testing.T) {
	t.Setenv(update.UpdatedFromEnv, "v0.3.0")
	if got := update.TakeUpdatedFrom(); got != "v0.3.0" {
		t.Fatalf("got %q", got)
	}
	if got := update.TakeUpdatedFrom(); got != "" {
		t.Fatal("it is cleared after reading")
	}
}

// `doctor` and `version` say which binary runs.
func TestDoctorAndVersionNameTheBinary(t *testing.T) {
	srv, _ := setup(t)
	srv.On("GET", "/account", apitest.Answer{Status: "200", Raw: `{"email": "a@example.com"}`})
	fakeLatest(t, "v0.0.1")
	t.Setenv("PATH", t.TempDir())
	r := run(t, "doctor", "--json")
	out := r.JSON(t)
	if r.exit() != 0 || out["path"] == "" || out["install_method"] == nil || out["on_path"] == nil ||
		out["auth"].(map[string]any)["email"] != "a@example.com" {
		t.Fatalf("doctor: %v %s", out, r.stderr)
	}
	v := run(t, "version", "--json").JSON(t)
	if v["path"] == nil || v["install_method"] == nil || v["version"] == nil {
		t.Fatalf("version: %v", v)
	}
}

// doctor never runs other copies, skips relative PATH entries, and reads an npm
// copy's version from its package.json.
func TestDoctorReadsOtherCopiesWithoutRunningThem(t *testing.T) {
	srv, _ := setup(t)
	srv.On("GET", "/account", apitest.Answer{Status: "200", Raw: `{"email": "a@example.com"}`})
	fakeLatest(t, "v0.0.1")
	root := t.TempDir()
	pkg := filepath.Join(root, "lib", "node_modules", "@zebracat", "cli")
	_ = os.MkdirAll(filepath.Join(pkg, "bin"), 0o755)
	_ = os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"name":"@zebracat/cli","version":"0.1.6"}`), 0o644)
	marker := filepath.Join(root, "ran")
	script := "#!/bin/sh\ntouch " + marker + "\n"
	_ = os.WriteFile(filepath.Join(pkg, "bin", "zebracat"), []byte(script), 0o755)
	rel := t.TempDir()
	_ = os.WriteFile(filepath.Join(rel, "zebracat"), []byte(script), 0o755)
	wd, _ := os.Getwd()
	_ = os.Chdir(rel)
	t.Cleanup(func() { _ = os.Chdir(wd) })
	t.Setenv("PATH", ".:"+filepath.Join(pkg, "bin"))
	out := run(t, "doctor", "--json").JSON(t)
	bins, _ := out["on_path"].([]any)
	if len(bins) != 1 {
		t.Fatalf("on_path %v (the relative '.' entry must be skipped)", bins)
	}
	b := bins[0].(map[string]any)
	if b["version"] != "0.1.6" || b["install_method"] != "npm" || b["remove"] != "npm uninstall -g @zebracat/cli" {
		t.Fatalf("npm copy %v", b)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("doctor ran another binary")
	}
}
