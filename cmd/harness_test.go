package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/zebracatai/zebracat-cli/internal/apitest"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files in testdata/golden")

// fakeClock records sleeps and moves time forward without sleeping.
type fakeClock struct {
	now    time.Time
	sleeps []time.Duration
}

func (f *fakeClock) Now() time.Time        { return f.now }
func (f *fakeClock) Sleep(d time.Duration) { f.sleeps = append(f.sleeps, d); f.now = f.now.Add(d) }

// result is one CLI run.
type result struct {
	stdout, stderr string
	err            *clierr.Error
}

func (r result) exit() int {
	if r.err == nil {
		return 0
	}
	return r.err.Exit
}

// JSON decodes stdout.
func (r result) JSON(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &m); err != nil {
		t.Fatalf("stdout is not a JSON object: %v\n%s\nstderr: %s", err, r.stdout, r.stderr)
	}
	return m
}

// setup points the CLI at a fresh fake API with an API key and a clean HOME.
func setup(t *testing.T) (*apitest.Server, *fakeClock) {
	t.Helper()
	srv := apitest.New(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ZEBRACAT_API_KEY", "sk-dummy")
	t.Setenv("ZEBRACAT_BASE_URL", srv.URL)
	t.Setenv("ZEBRACAT_OUTPUT", "json")
	clk := &fakeClock{now: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)}
	oldClock, oldInteractive, oldConfirm, oldStdin := clock, interactive, confirm, stdin
	clock = clk
	interactive = func() bool { return false }
	t.Cleanup(func() { clock, interactive, confirm, stdin = oldClock, oldInteractive, oldConfirm, oldStdin })
	return srv, clk
}

// resetFlags puts every flag of every command back to its default, since
// cobra keeps flag values between Execute calls in one process.
func resetFlags(c *cobra.Command) {
	reset := func(f *pflag.Flag) {
		if sv, ok := f.Value.(pflag.SliceValue); ok {
			_ = sv.Replace(nil)
		} else {
			_ = f.Value.Set(f.DefValue)
		}
		f.Changed = false
	}
	c.Flags().VisitAll(reset)
	c.PersistentFlags().VisitAll(reset)
	for _, sub := range c.Commands() {
		resetFlags(sub)
	}
}

// run executes the CLI with args and captures stdout, stderr and the error.
func run(t *testing.T, args ...string) result {
	t.Helper()
	resetFlags(rootCmd)
	co = createOpts{timeout: 30 * time.Minute}

	oldOut, oldErr := os.Stdout, os.Stderr
	ro, wo, _ := os.Pipe()
	re, we, _ := os.Pipe()
	os.Stdout, os.Stderr = wo, we
	outC, errC := make(chan string), make(chan string)
	go func() { b, _ := io.ReadAll(ro); outC <- string(b) }()
	go func() { b, _ := io.ReadAll(re); errC <- string(b) }()

	rootCmd.SetArgs(args)
	err := rootCmd.Execute()

	wo.Close()
	we.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	r := result{stdout: <-outC, stderr: <-errC}
	if err != nil {
		var ce *clierr.Error
		if !errors.As(err, &ce) {
			ce = clierr.API("%v", err) // what Execute prints for a plain error
		}
		r.err = ce
	}
	return r
}

// golden compares v (as indented JSON) with testdata/golden/<name>.json.
func golden(t *testing.T, name string, v any) {
	t.Helper()
	got, _ := json.MarshalIndent(v, "", "  ")
	got = append(got, '\n')
	path := filepath.Join("testdata", "golden", name+".json")
	if *updateGolden {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (run go test ./cmd -update): %v", path, err)
	}
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		t.Fatalf("%s differs from golden:\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

func mustContain(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Fatalf("expected %q in:\n%s", sub, s)
	}
}
