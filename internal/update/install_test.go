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

// install.sh checks the archive against the release's checksums.txt and
// installs nothing when the sum is wrong or the file is missing.
func TestInstallScriptChecksTheSum(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is for Linux and macOS")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	goos, goarch := runtime.GOOS, runtime.GOARCH
	asset := fmt.Sprintf("zebracat_%s_%s.tar.gz", goos, goarch)
	arch := tarball(t, "#!/bin/sh\necho installed-binary\n")
	sum := sha256.Sum256(arch)
	sums := hex.EncodeToString(sum[:]) + "  " + asset + "\n"
	mode := "good"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + Repo + "/releases/download/v0.3.0/" + asset:
			_, _ = w.Write(arch)
		case "/" + Repo + "/releases/download/v0.3.0/checksums.txt":
			switch mode {
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
	defer srv.Close()

	script, _ := filepath.Abs("../../install.sh")
	install := func(t *testing.T) (string, string, error) {
		dir := t.TempDir()
		cmd := exec.Command(bash, script)
		cmd.Env = append(os.Environ(),
			"ZEBRACAT_RELEASES_URL="+srv.URL,
			"ZEBRACAT_INSTALL_VERSION=v0.3.0",
			"ZEBRACAT_BIN_DIR="+dir,
			"HOME="+t.TempDir(),
		)
		out, err := cmd.CombinedOutput()
		return filepath.Join(dir, "zebracat"), string(out), err
	}

	bin, out, err := install(t)
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
		bin, out, err := install(t)
		if err == nil {
			t.Fatalf("%s checksums.txt must refuse:\n%s", m, out)
		}
		if _, err := os.Stat(bin); err == nil {
			t.Fatalf("%s checksums.txt: binary was installed anyway", m)
		}
	}
}
