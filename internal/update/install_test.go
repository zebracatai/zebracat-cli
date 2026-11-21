package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// shells are the POSIX shells on this machine install.sh must run under:
// `curl … | sh` on Debian/Ubuntu is dash, on Alpine BusyBox ash.
func shells(t *testing.T) map[string][]string {
	out := map[string][]string{}
	for name, argv := range map[string][]string{"sh": {"sh"}, "dash": {"dash"}, "bash": {"bash"}, "busybox": {"busybox", "sh"}} {
		if p, err := exec.LookPath(argv[0]); err == nil {
			out[name] = append([]string{p}, argv[1:]...)
		}
	}
	if len(out) == 0 {
		t.Skip("no shell")
	}
	return out
}

// toolsPath is a PATH directory with links to the tools install.sh uses,
// leaving out `without` (to test the curl-less and download-less paths).
func toolsPath(t *testing.T, without ...string) string {
	dir := t.TempDir()
	skip := map[string]bool{}
	for _, w := range without {
		skip[w] = true
	}
	for _, tool := range []string{"uname", "tr", "mktemp", "rm", "curl", "wget", "awk", "sha256sum", "shasum",
		"tar", "gzip", "mkdir", "install", "mv", "chmod", "cat", "sh", "dash", "bash", "busybox"} {
		if skip[tool] {
			continue
		}
		if p, err := exec.LookPath(tool); err == nil {
			_ = os.Symlink(p, filepath.Join(dir, tool))
		}
	}
	return dir
}

// fakeRelease serves release v0.3.0 (with /releases/latest redirecting to it);
// *mode picks what checksums.txt answers.
func fakeRelease(t *testing.T, mode *string) *httptest.Server {
	goos, goarch := runtime.GOOS, runtime.GOARCH
	asset := fmt.Sprintf("zebracat_%s_%s.tar.gz", goos, goarch)
	arch := tarball(t, "#!/bin/sh\necho installed-binary\n")
	sum := sha256.Sum256(arch)
	sums := hex.EncodeToString(sum[:]) + "  " + asset + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + Repo + "/releases/latest":
			http.Redirect(w, r, "/"+Repo+"/releases/tag/v0.3.0", http.StatusFound)
		case "/" + Repo + "/releases/tag/v0.3.0":
			_, _ = w.Write([]byte("<html>release page</html>"))
		case "/" + Repo + "/releases/download/v0.3.0/" + asset:
			_, _ = w.Write(arch)
		case "/" + Repo + "/releases/download/v0.3.0/checksums.txt":
			switch *mode {
			case "missing":
				http.NotFound(w, r)
			case "bad":
				_, _ = w.Write([]byte(strings.Repeat("0", 64) + "  " + asset + "\n"))
			case "other":
				_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  zebracat_plan9_mips.tar.gz\n"))
			default:
				_, _ = w.Write([]byte(sums))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// runInstall runs install.sh with shell argv and PATH, pinning version when set.
func runInstall(t *testing.T, srv *httptest.Server, argv []string, path, version string) (string, string, error) {
	script, _ := filepath.Abs("../../install.sh")
	dir := t.TempDir()
	cmd := exec.Command(argv[0], append(argv[1:], script)...)
	cmd.Env = []string{
		"PATH=" + path,
		"ZEBRACAT_RELEASES_URL=" + srv.URL,
		"ZEBRACAT_BIN_DIR=" + dir,
		"HOME=" + t.TempDir(),
	}
	if version != "" {
		cmd.Env = append(cmd.Env, "ZEBRACAT_INSTALL_VERSION="+version)
	}
	out, err := cmd.CombinedOutput()
	return filepath.Join(dir, "zebracat"), string(out), err
}

// install.sh checks the archive against the release's checksums.txt and
// installs nothing when the sum is wrong or the file is missing, under every
// POSIX shell here.
func TestInstallScriptChecksTheSum(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is for Linux and macOS")
	}
	mode := "good"
	srv := fakeRelease(t, &mode)
	for name, argv := range shells(t) {
		t.Run(name, func(t *testing.T) {
			mode = "good"
			bin, out, err := runInstall(t, srv, argv, os.Getenv("PATH"), "v0.3.0")
			if err != nil {
				t.Fatalf("a good sum must install: %v\n%s", err, out)
			}
			if !strings.Contains(out, "Checksum verified") || !strings.Contains(out, "installed-binary") {
				t.Fatalf("output: %s", out)
			}
			if _, err := os.Stat(bin); err != nil {
				t.Fatal("binary not installed")
			}
			for _, m := range []string{"bad", "missing", "other"} {
				mode = m
				bin, out, err := runInstall(t, srv, argv, os.Getenv("PATH"), "v0.3.0")
				if err == nil {
					t.Fatalf("%s checksums.txt must refuse:\n%s", m, out)
				}
				if _, err := os.Stat(bin); err == nil {
					t.Fatalf("%s checksums.txt: binary was installed anyway", m)
				}
			}
		})
	}
}

// Without curl (Alpine's default image) install.sh finds the latest release and
// downloads with wget; with neither it says what to install.
func TestInstallScriptWithoutCurl(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is for Linux and macOS")
	}
	if _, err := exec.LookPath("wget"); err != nil {
		t.Skip("no wget")
	}
	mode := "good"
	srv := fakeRelease(t, &mode)
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}

	bin, out, err := runInstall(t, srv, []string{sh}, toolsPath(t, "curl"), "")
	if err != nil {
		t.Fatalf("wget alone must install the latest release: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Downloading v0.3.0") || !strings.Contains(out, "Checksum verified") {
		t.Fatalf("output: %s", out)
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatal("binary not installed")
	}

	mode = "bad"
	if bin, out, err := runInstall(t, srv, []string{sh}, toolsPath(t, "curl"), ""); err == nil {
		t.Fatalf("wget path must refuse a bad sum:\n%s", out)
	} else if _, err := os.Stat(bin); err == nil {
		t.Fatal("wget path installed a bad download")
	}

	_, out, err = runInstall(t, srv, []string{sh}, toolsPath(t, "curl", "wget"), "")
	if err == nil || !strings.Contains(out, "needs curl or wget") {
		t.Fatalf("no curl and no wget must say so: %v\n%s", err, out)
	}
}
