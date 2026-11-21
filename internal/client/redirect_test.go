package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The API key follows a redirect on the API's own host, never to another host.
func TestRedirectKeepsTheKeyOnTheAPIHost(t *testing.T) {
	var other, same string
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		other = r.Header.Get("X-API-KEY")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer elsewhere.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/away":
			http.Redirect(w, r, elsewhere.URL+"/x", http.StatusFound)
		case "/here":
			http.Redirect(w, r, "/landed", http.StatusFound)
		case "/landed":
			same = r.Header.Get("X-API-KEY")
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer api.Close()
	c := New(api.URL, nil, "sk-secret")
	if _, err := c.Do(context.Background(), "GET", "/away", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Do(context.Background(), "GET", "/here", nil, nil); err != nil {
		t.Fatal(err)
	}
	if other != "" || same != "sk-secret" {
		t.Fatalf("key at the other host %q, on the API host %q", other, same)
	}
}
