package update

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// install.ps1 checks the zip against the release's checksums.txt and installs
// nothing when the sum is wrong, the line is missing or checksums.txt is gone.
// It runs wherever PowerShell is on PATH (GitHub's Linux, macOS and Windows
// runners all have pwsh); without one the test skips.
func TestInstallPs1ChecksTheSum(t *testing.T) {
	shell := ""
	for _, name := range []string{"pwsh", "powershell"} {
		if p, err := exec.LookPath(name); err == nil {
			shell = p
			break
		}
	}
	if shell == "" {
		t.Skip("no PowerShell")
	}
	arch := "amd64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}
	asset := "zebracat_windows_" + arch + ".zip"

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("zebracat.exe")
	_, _ = f.Write([]byte("fake-zebracat-exe"))
	_ = zw.Close()
	archive := buf.Bytes()
	sum := sha256.Sum256(archive)

	mode := "good"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + Repo + "/releases/latest":
			http.Redirect(w, r, "/"+Repo+"/releases/tag/v0.3.0", http.StatusFound)
		case "/" + Repo + "/releases/download/v0.3.0/" + asset:
			_, _ = w.Write(archive)
		case "/" + Repo + "/releases/download/v0.3.0/checksums.txt":
			switch mode {
			case "missing":
				http.NotFound(w, r)
			case "bad":
				_, _ = w.Write([]byte(strings.Repeat("0", 64) + "  " + asset + "\n"))
			case "other":
				_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  zebracat_plan9_mips.zip\n"))
			default:
				_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  " + asset + "\n"))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	script, _ := filepath.Abs("../../install.ps1")
	install := func(t *testing.T) (string, string, error) {
		dir := t.TempDir()
		cmd := exec.Command(shell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script)
		cmd.Env = append(os.Environ(),
			"ZEBRACAT_RELEASES_URL="+srv.URL,
			"ZEBRACAT_INSTALL_VERSION=",
			"ZEBRACAT_BIN_DIR="+dir,
			"ZEBRACAT_NO_MODIFY_PATH=1",
		)
		out, err := cmd.CombinedOutput()
		return filepath.Join(dir, "zebracat.exe"), string(out), err
	}

	exe, out, err := install(t)
	if err != nil {
		t.Fatalf("a good sum must install: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Downloading v0.3.0") || !strings.Contains(out, "Checksum verified") {
		t.Fatalf("output: %s", out)
	}
	if b, err := os.ReadFile(exe); err != nil || string(b) != "fake-zebracat-exe" {
		t.Fatalf("zebracat.exe not installed: %q %v", b, err)
	}

	for _, m := range []string{"bad", "missing", "other"} {
		mode = m
		exe, out, err := install(t)
		if err == nil {
			t.Fatalf("%s checksums.txt must refuse:\n%s", m, out)
		}
		if _, err := os.Stat(exe); err == nil {
			t.Fatalf("%s checksums.txt: zebracat.exe was installed anyway", m)
		}
		if !strings.Contains(out, "error:") {
			t.Fatalf("%s: no error line:\n%s", m, out)
		}
	}
}
