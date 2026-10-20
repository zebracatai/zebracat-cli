package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zebracatai/zebracat-cli/internal/config"
)

// capsTTL is how long the server's path list is trusted.
const capsTTL = 24 * time.Hour

type capsCache struct {
	BaseURL   string    `json:"base_url"`
	FetchedAt time.Time `json:"fetched_at"`
	Paths     []string  `json:"paths"`
}

// capsFile is ~/.zebracat/capabilities.json; tests point it elsewhere.
var capsFile = func() string {
	dir, err := config.Dir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "capabilities.json")
}

// HasPath reports whether the server's OpenAPI schema lists a path ending in
// suffix (e.g. "/studio/scripts"). The path list comes from /docs/openapi.json
// once a day and is cached on disk. Any failure answers false, so callers fall
// back to the endpoints every server has.
func (c *Client) HasPath(ctx context.Context, suffix string) bool {
	paths, ok := c.schemaPathList(ctx)
	return ok && listsPath(paths, suffix)
}

// LacksPath reports whether the server's schema was read and lists no path
// ending in suffix. A schema that could not be read answers false: unknown is
// not absent, so callers go ahead and let the server answer.
func (c *Client) LacksPath(ctx context.Context, suffix string) bool {
	paths, ok := c.schemaPathList(ctx)
	return ok && !listsPath(paths, suffix)
}

func (c *Client) schemaPathList(ctx context.Context) ([]string, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	paths, ok := c.cachedPaths()
	if !ok {
		var err error
		if paths, err = c.fetchPaths(ctx); err != nil {
			return nil, false
		}
		c.savePaths(paths)
	}
	return paths, true
}

func listsPath(paths []string, suffix string) bool {
	for _, p := range paths {
		if strings.HasSuffix(p, suffix) {
			return true
		}
	}
	return false
}

func (c *Client) cachedPaths() ([]string, bool) {
	f := capsFile()
	if f == "" {
		return nil, false
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return nil, false
	}
	var cc capsCache
	if json.Unmarshal(b, &cc) != nil || cc.BaseURL != c.BaseURL || time.Since(cc.FetchedAt) > capsTTL {
		return nil, false
	}
	return cc.Paths, true
}

func (c *Client) savePaths(paths []string) {
	if f := capsFile(); f != "" {
		b, _ := json.Marshal(capsCache{BaseURL: c.BaseURL, FetchedAt: time.Now(), Paths: paths})
		_ = os.WriteFile(f, b, 0o600)
	}
}

func (c *Client) fetchPaths(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+"/docs/openapi.json", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "zebracat-cli")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, io.ErrUnexpectedEOF
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return schemaPaths(raw), nil
}

// schemaPaths lists the path keys of an OpenAPI document: JSON (OpenAPI 3), or
// the Swagger 2 YAML older servers serve, where each path is a two-space
// indented key under the top-level "paths:" block.
func schemaPaths(raw []byte) []string {
	var doc struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if json.Unmarshal(raw, &doc) == nil {
		out := make([]string, 0, len(doc.Paths))
		for p := range doc.Paths {
			out = append(out, p)
		}
		return out
	}
	var out []string
	inPaths := false
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, " ") && line != "" {
			inPaths = line == "paths:"
			continue
		}
		if inPaths && strings.HasPrefix(line, "  /") && !strings.HasPrefix(line, "   ") {
			out = append(out, strings.Trim(strings.TrimSpace(line), `:'"`))
		}
	}
	return out
}
