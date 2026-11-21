// Package update implements the CLI's self-update: checking GitHub Releases for
// a newer version and replacing the running binary in place.
//
// The latest-version check is cached under ~/.zebracat/update.json and refreshed
// at most once per 24h, so it never slows down ordinary commands.
package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/zebracatai/zebracat-cli/internal/config"
)

// Repo is the GitHub repo releases are published to.
const Repo = "zebracatai/zebracat-cli"

// GitHub is the host releases are read from. A test points it at a fake server.
// A shipped binary always reads github.com: ZEBRACAT_RELEASES_URL is honoured only
// by a build with the `testreleases` tag (see releases_*.go), so nothing in the
// environment can send `zebracat update` to another host.
var GitHub = releasesHost()

// WindowsInstaller is the PowerShell installer a Windows copy is updated with.
const WindowsInstaller = "https://raw.githubusercontent.com/" + Repo + "/main/install.ps1"

// CheckTimeout bounds the launch check: it never delays a command by more.
const CheckTimeout = 1500 * time.Millisecond

// CheckEvery is how often the launch check may touch the network.
const CheckEvery = 24 * time.Hour

// ciVars are environment variables CI systems set; the launch check is off there.
var ciVars = []string{"CI", "GITHUB_ACTIONS", "GITLAB_CI", "BUILDKITE", "CIRCLECI", "TRAVIS", "JENKINS_URL",
	"TEAMCITY_VERSION", "TF_BUILD", "CODEBUILD_BUILD_ID", "BITBUCKET_BUILD_NUMBER"}

type cacheFile struct {
	Latest    string    `json:"latest"`
	CheckedAt time.Time `json:"checked_at"`
}

func cachePath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "update.json"), nil
}

// Disabled reports why the launch check must not run ("" = it may):
// ZEBRACAT_NO_UPDATE_CHECK=1, a CI environment, or a development build.
func Disabled(currentVersion string) string {
	if v := os.Getenv("ZEBRACAT_NO_UPDATE_CHECK"); v != "" && v != "0" && v != "false" {
		return "ZEBRACAT_NO_UPDATE_CHECK"
	}
	for _, k := range ciVars {
		if v := os.Getenv(k); v != "" && v != "0" && v != "false" {
			return "ci:" + k
		}
	}
	if parseSemver(currentVersion) == [3]int{} {
		return "dev build"
	}
	return ""
}

// noRedirect stops at GitHub's /releases/latest redirect so its Location names the tag.
var noRedirect = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// Latest finds the newest release tag through the /releases/latest redirect
// (the same one install.sh follows): no API call, so no 60/h anonymous limit.
func Latest(ctx context.Context) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodHead, GitHub+"/"+Repo+"/releases/latest", nil)
	resp, err := noRedirect.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	if resp.StatusCode < 300 || resp.StatusCode >= 400 || loc == "" {
		return "", fmt.Errorf("GitHub returned %d without a release redirect", resp.StatusCode)
	}
	u, err := url.Parse(loc)
	if err != nil {
		return "", err
	}
	dir, tag := path.Split(strings.TrimSuffix(u.Path, "/"))
	if !strings.HasSuffix(dir, "/releases/tag/") || tag == "" {
		return "", fmt.Errorf("no release published yet")
	}
	return tag, nil
}

// LatestCached returns the newest release tag, touching the network at most once
// per CheckEvery. A failed check is cached too (with the last known tag), so an
// offline machine is not asked again on every command.
func LatestCached(ctx context.Context) string {
	return latestCached(ctx, time.Now())
}

func latestCached(ctx context.Context, now time.Time) string {
	p, err := cachePath()
	if err != nil {
		return ""
	}
	var c cacheFile
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if !c.CheckedAt.IsZero() && now.Sub(c.CheckedAt) < CheckEvery && now.After(c.CheckedAt.Add(-time.Minute)) {
		return c.Latest
	}
	if tag, err := Latest(ctx); err == nil {
		c.Latest = tag
	}
	c.CheckedAt = now
	if b, err := json.MarshalIndent(c, "", "  "); err == nil {
		_ = os.WriteFile(p, b, 0o600)
	}
	return c.Latest
}

// Notice is the one line printed when a newer release is out, or "".
func Notice(current, latest string) string {
	if latest == "" || !Newer(current, latest) {
		return ""
	}
	return fmt.Sprintf("Zebracat CLI %s is available — run `%s`", latest, UpgradeCommand())
}

// Check runs the launch check in the background and returns a function that
// waits for it, never past CheckTimeout from the start, and returns the notice
// ("" when there is none, it is disabled, or it ran out of time).
func Check(current string) func() string {
	if Disabled(current) != "" {
		return func() string { return "" }
	}
	start := time.Now()
	done := make(chan string, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), CheckTimeout)
		defer cancel()
		done <- Notice(current, LatestCached(ctx))
	}()
	return func() string {
		left := CheckTimeout - time.Since(start)
		if left < 0 {
			left = 0
		}
		select {
		case n := <-done:
			return n
		case <-time.After(left):
			return ""
		}
	}
}

// Method is how this binary was installed: "npm", "go" or "binary".
func Method() string { return methodOf(CurrentPath()) }

func methodOf(exe string) string {
	p := filepath.ToSlash(exe)
	switch {
	case strings.Contains(p, "/node_modules/"):
		return "npm"
	case filepath.Base(p) == "zebracat-cli":
		return "go"
	}
	return "binary"
}

// UpgradeCommand is what the person runs to update, by install method.
func UpgradeCommand() string {
	switch Method() {
	case "npm":
		return "npm install -g @zebracat/cli@latest"
	case "go":
		return "go install github.com/zebracatai/zebracat-cli@latest"
	}
	return "zebracat update"
}

// Newer reports whether latest is a higher semver than current. Both may carry
// a leading "v"; pre-release/build suffixes are ignored.
func Newer(current, latest string) bool {
	c, l := parseSemver(current), parseSemver(latest)
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parseSemver(s string) [3]int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	var out [3]int
	for i, p := range strings.SplitN(s, ".", 3) {
		if i > 2 {
			break
		}
		out[i], _ = strconv.Atoi(p)
	}
	return out
}

// ErrManaged is returned by Apply when a package manager owns this binary.
type ErrManaged struct{ Method, Command string }

func (e *ErrManaged) Error() string {
	return fmt.Sprintf("this zebracat was installed with %s — update it with: %s", e.Method, e.Command)
}

// Apply downloads the release `tag` for this OS/arch, checks it against the
// release's checksums.txt, and replaces the running executable in place.
// Linux/macOS only; an npm or go install is left to its package manager.
// It returns the path it replaced, to relaunch.
func Apply(ctx context.Context, tag string) (string, error) {
	if m := Method(); m != "binary" {
		return "", &ErrManaged{Method: m, Command: UpgradeCommand()}
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("could not locate the current binary: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, ApplyTo(ctx, tag, exe, runtime.GOOS, runtime.GOARCH)
}

// UpdatedFromEnv is set on a relaunched binary to the version it replaced.
const UpdatedFromEnv = "ZEBRACAT_UPDATED_FROM"

// Relaunch replaces this process with the binary at path, running args, and
// tells it which version it replaced. It only returns on failure.
func Relaunch(path, from string, args []string) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("relaunch isn't supported on Windows: start zebracat again")
	}
	env := append(os.Environ(), UpdatedFromEnv+"="+from)
	return execFn(path, append([]string{path}, args...), env)
}

// TakeUpdatedFrom reads (and clears, so children don't inherit it) the version
// a relaunch replaced; "" when this run is not a relaunch.
func TakeUpdatedFrom() string {
	v := os.Getenv(UpdatedFromEnv)
	os.Unsetenv(UpdatedFromEnv)
	return v
}

// ApplyTo installs release `tag` for goos/goarch over the file at exe.
func ApplyTo(ctx context.Context, tag, exe, goos, goarch string) error {
	if goos == "windows" {
		return fmt.Errorf("self-update isn't supported on Windows yet — run the installer again in PowerShell: irm %s | iex", WindowsInstaller)
	}
	asset := fmt.Sprintf("zebracat_%s_%s.tar.gz", goos, goarch)
	base := fmt.Sprintf("%s/%s/releases/download/%s/", GitHub, Repo, tag)

	archive, err := fetch(ctx, base+asset)
	if err != nil {
		return err
	}
	// No checksums.txt, no install: a release without one (or a blocked
	// download of it) must not slip an unchecked binary in.
	sums, err := fetch(ctx, base+"checksums.txt")
	if err != nil {
		return fmt.Errorf("could not read the release's checksums.txt, so the download was not installed: %w", err)
	}
	if err := verify(sums, asset, archive); err != nil {
		return err
	}
	bin, err := extractBinary(bytes.NewReader(archive))
	if err != nil {
		return err
	}

	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".zebracat-update-*")
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("can't write to %s — re-run with: sudo zebracat update", dir)
		}
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmpName, exe); err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("can't replace %s — re-run with: sudo zebracat update", exe)
		}
		return fmt.Errorf("could not replace %s: %w", exe, err)
	}
	return nil
}

func fetch(ctx context.Context, link string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download failed: HTTP %d (%s)", resp.StatusCode, link)
	}
	return io.ReadAll(resp.Body)
}

// verify checks data against its line in a goreleaser checksums.txt ("<sha256>  <name>").
func verify(sums []byte, name string, data []byte) error {
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[1] == name {
			if !strings.EqualFold(f[0], got) {
				return fmt.Errorf("checksum mismatch for %s — the download was not installed", name)
			}
			return nil
		}
	}
	return fmt.Errorf("checksums.txt has no line for %s — the download was not installed", name)
}

// CurrentPath returns the absolute path of the running binary (resolved through
// symlinks), for display in the update notice.
func CurrentPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "zebracat"
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved
	}
	return exe
}

func extractBinary(r io.Reader) ([]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("bad archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("bad archive: %w", err)
		}
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == "zebracat" {
			b, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("could not read binary from archive: %w", err)
			}
			return b, nil
		}
	}
	return nil, fmt.Errorf("archive did not contain the zebracat binary")
}
