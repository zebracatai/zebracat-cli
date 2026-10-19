// Package apitest is an httptest server that answers like the public API by
// serving the response examples of the checked-in OpenAPI copy
// (internal/openapi/openapi.json). Tests pick which example (and status) a
// route answers, queue a sequence of answers, and read back every call made.
package apitest

import (
	_ "embed"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/zebracatai/zebracat-cli/internal/openapi"
)

// Call is one request the server received.
type Call struct {
	Method string
	Path   string // without the query
	Query  string
	Header http.Header
	Body   map[string]any
}

// Answer is what a route responds: a schema example at a status.
type Answer struct {
	Status  string // "200", "202", "402", …
	Example string // example name; "" = the first by name
	Header  map[string]string
	Raw     string // a body to send instead of the example (for URLs the test must own)
}

// Server is the fake API.
type Server struct {
	*httptest.Server
	t     testing.TB
	doc   *openapi.Doc
	mu    sync.Mutex
	calls []Call
	queue map[string][]Answer // "METHOD template" → answers, the last one repeats
	extra map[string]http.HandlerFunc
}

// New starts a server and closes it with the test.
func New(t testing.TB) *Server {
	t.Helper()
	doc, err := openapi.Load()
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{t: t, doc: doc, queue: map[string][]Answer{}, extra: map[string]http.HandlerFunc{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

// On queues answers for a route, by method and path template suffix
// (e.g. "GET", "/videos/{task_id}"). Each call takes the next answer; the last repeats.
func (s *Server) On(method, suffix string, answers ...Answer) {
	key, ok := s.template(method, suffix)
	if !ok {
		s.t.Fatalf("apitest: the schema has no %s …%s", method, suffix)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queue[key] = append([]Answer(nil), answers...)
}

// Handle serves a path that is not in the schema (e.g. /docs/openapi.json).
func (s *Server) Handle(path string, h http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.extra[path] = h
}

// Calls returns every request so far.
func (s *Server) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

// Last returns the last call to a path ending in suffix (method "" = any).
func (s *Server) Last(method, suffix string) (Call, bool) {
	calls := s.Calls()
	for i := len(calls) - 1; i >= 0; i-- {
		c := calls[i]
		if (method == "" || c.Method == method) && strings.HasSuffix(c.Path, suffix) {
			return c, true
		}
	}
	return Call{}, false
}

// Count is how many calls hit a path ending in suffix.
func (s *Server) Count(method, suffix string) int {
	n := 0
	for _, c := range s.Calls() {
		if c.Method == method && strings.HasSuffix(c.Path, suffix) {
			n++
		}
	}
	return n
}

func (s *Server) template(method, suffix string) (string, bool) {
	p, op := s.doc.Find(method, suffix)
	if op == nil {
		return "", false
	}
	return method + " " + p, true
}

// match finds the schema path template for a concrete request path.
func (s *Server) match(method, path string) (string, *openapi.Operation) {
	keys := make([]string, 0, len(s.doc.Paths))
	for k := range s.doc.Paths {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	got := strings.Split(strings.TrimSuffix(path, "/"), "/")
	for _, k := range keys {
		want := strings.Split(k, "/")
		if len(want) != len(got) {
			continue
		}
		ok := true
		for i := range want {
			if want[i] != got[i] && !(strings.HasPrefix(want[i], "{") && strings.HasSuffix(want[i], "}")) {
				ok = false
				break
			}
		}
		if op := s.doc.Paths[k][strings.ToLower(method)]; ok && op != nil {
			return k, op
		}
	}
	return "", nil
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	s.mu.Lock()
	s.calls = append(s.calls, Call{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Header: r.Header.Clone(), Body: body})
	h := s.extra[r.URL.Path]
	s.mu.Unlock()
	if h != nil {
		h(w, r)
		return
	}

	tmpl, op := s.match(r.Method, r.URL.Path)
	var ans Answer
	if op != nil {
		ans = s.next(r.Method + " " + tmpl)
	}
	if ans.Raw != "" {
		w.Header().Set("Content-Type", "application/json")
		code := 200
		_ = json.Unmarshal([]byte(ans.Status), &code)
		w.WriteHeader(code)
		_, _ = w.Write([]byte(ans.Raw))
		return
	}
	if op == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"Not found.","code":"not_found","type":"not_found","retryable":false}`))
		return
	}
	if ans.Status == "" {
		ans.Status = defaultStatus(op, body)
	}
	examples := s.examples(r.Method+" "+tmpl, op, ans.Status)
	var value []byte
	switch {
	case len(examples) > 0:
		name := ans.Example
		if name == "" {
			names := make([]string, 0, len(examples))
			for n := range examples {
				names = append(names, n)
			}
			sort.Strings(names)
			name = names[0]
		}
		if value = examples[name]; value == nil {
			s.t.Errorf("apitest: %s %s %s has no example %q", r.Method, tmpl, ans.Status, name)
			w.WriteHeader(500)
			return
		}
	case cannedErrors[ans.Status] != "":
		value = []byte(cannedErrors[ans.Status])
	default:
		s.t.Errorf("apitest: %s %s has no example for %s", r.Method, tmpl, ans.Status)
		w.WriteHeader(500)
		return
	}
	for k, v := range ans.Header {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Type", "application/json")
	code := 200
	_ = json.Unmarshal([]byte(ans.Status), &code)
	w.WriteHeader(code)
	_, _ = w.Write(value)
}

// examples are the named answers for one route and status: the schema's own
// examples, else the test fixtures (fixtures.json) for routes the schema
// documents without examples.
func (s *Server) examples(key string, op *openapi.Operation, status string) map[string][]byte {
	out := map[string][]byte{}
	if resp := op.Responses[status]; resp != nil {
		if media := resp.Content["application/json"]; media != nil {
			for n, ex := range media.Examples {
				out[n] = ex.Value
			}
		}
	}
	if len(out) > 0 {
		return out
	}
	for n, v := range fixtures[key][status] {
		out[n] = v
	}
	return out
}

func (s *Server) next(key string) Answer {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := s.queue[key]
	if len(q) == 0 {
		return Answer{}
	}
	a := q[0]
	if len(q) > 1 {
		s.queue[key] = q[1:]
	}
	return a
}

// fixtures are test-owned answers keyed "METHOD path" → status → name, for the
// routes the live schema documents without examples. They follow the
// backend's real shapes (public_apis/v2/lifecycle.py, catalogue.py).
//
//go:embed fixtures.json
var fixturesRaw []byte

var fixtures = func() map[string]map[string]map[string]json.RawMessage {
	f := map[string]map[string]map[string]json.RawMessage{}
	if err := json.Unmarshal(fixturesRaw, &f); err != nil {
		panic("apitest: fixtures.json: " + err.Error())
	}
	return f
}()

// defaultStatus: a dry run answers 200; otherwise 202 if the operation is
// async, else 200.
func defaultStatus(op *openapi.Operation, body map[string]any) string {
	if dry, _ := body["dry_run"].(bool); dry && op.Responses["200"] != nil {
		return "200"
	}
	if op.Responses["202"] != nil {
		return "202"
	}
	return "200"
}
