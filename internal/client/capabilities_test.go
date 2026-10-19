package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/zebracatai/zebracat-cli/internal/config"
)

func TestSchemaPathsReadsJSONAndSwaggerYAML(t *testing.T) {
	j := schemaPaths([]byte(`{"openapi":"3.1.0","paths":{"/api/v1/public/studio/scripts":{},"/api/v1/public/videos":{}}}`))
	if len(j) != 2 {
		t.Fatalf("json paths = %v", j)
	}
	y := schemaPaths([]byte("swagger: '2.0'\nbasePath: /api/v1/public\npaths:\n  /account:\n    get:\n      summary: x\n  /studio/scripts:\n    post: {}\ndefinitions:\n  /notapath:\n"))
	if len(y) != 2 || y[0] != "/account" || y[1] != "/studio/scripts" {
		t.Fatalf("yaml paths = %v", y)
	}
}

func TestHasPathCachesForADay(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/docs/openapi.json" {
			http.NotFound(w, r)
			return
		}
		hits++
		_, _ = w.Write([]byte(`{"paths":{"/api/v1/public/studio/scripts":{}}}`))
	}))
	defer srv.Close()
	dir := t.TempDir()
	old := capsFile
	capsFile = func() string { return filepath.Join(dir, "caps.json") }
	defer func() { capsFile = old }()

	c := New(srv.URL, &config.Credentials{}, "sk-dummy")
	if !c.HasPath(context.Background(), "/studio/scripts") || c.HasPath(context.Background(), "/studio/nope") {
		t.Fatal("HasPath answered wrong")
	}
	if hits != 1 {
		t.Fatalf("schema fetched %d times, want 1 (cached)", hits)
	}
}

func TestHasPathFalseWhenSchemaMissing(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	old := capsFile
	capsFile = func() string { return "" }
	defer func() { capsFile = old }()
	c := New(srv.URL, &config.Credentials{}, "sk-dummy")
	if c.HasPath(context.Background(), "/studio/scripts") {
		t.Fatal("a server without a schema has no studio routes")
	}
}
