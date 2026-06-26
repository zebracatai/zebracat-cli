// Package update implements the CLI's self-update: checking GitHub Releases for
// a newer version and replacing the running binary in place.
//
// The latest-version check is cached under ~/.zebracat/update.json and refreshed
// at most once per 24h, so it never slows down ordinary commands.
package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/zebracatai/zebracat-cli/internal/config"
)

// Repo is the GitHub repo releases are published to.
const Repo = "zebracatai/zebracat-cli"

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

// Latest fetches the newest release tag directly from GitHub (no cache).
func Latest(ctx context.Context) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/"+Repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GitHub returned %d", resp.StatusCode)
	}
	var r struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	if r.TagName == "" {
		return "", fmt.Errorf("no release published yet")
	}
	return r.TagName, nil
}

// LatestCached returns the newest release tag, refreshing the on-disk cache at
// most once per 24h. Network errors fall back to the cached value (maybe "").
func LatestCached(ctx context.Context) string {
	p, err := cachePath()
	if err != nil {
		return ""
	}
	var c cacheFile
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.Latest != "" && time.Since(c.CheckedAt) < 24*time.Hour {
		return c.Latest
	}
	tag, err := Latest(ctx)
	if err != nil {
		return c.Latest
	}
	c.Latest, c.CheckedAt = tag, time.Now()
	if b, err := json.MarshalIndent(c, "", "  "); err == nil {
		_ = os.WriteFile(p, b, 0o600)
	}
	return tag
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

// Apply downloads the release `tag` for this OS/arch and replaces the running
// executable in place. Linux/macOS only.
func Apply(ctx context.Context, tag string) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("self-update isn't supported on Windows yet — download the .exe from %s/releases", "https://github.com/"+Repo)
	}
	asset := fmt.Sprintf("zebracat_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", Repo, tag, asset)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed: HTTP %d (%s)", resp.StatusCode, url)
	}

	bin, err := extractBinary(resp.Body)
	if err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not locate the current binary: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
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
