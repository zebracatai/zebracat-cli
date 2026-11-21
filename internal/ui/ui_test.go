package ui

import (
	"io"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/zebracatai/zebracat-cli/internal/clierr"
)

// capture runs fn with os.Stdout or os.Stderr swapped for a pipe.
func capture(t *testing.T, f **os.File, fn func()) string {
	t.Helper()
	old := *f
	r, w, _ := os.Pipe()
	*f = w
	done := make(chan string)
	go func() { b, _ := io.ReadAll(r); done <- string(b) }()
	fn()
	w.Close()
	*f = old
	return <-done
}

// /dev/null is a character device but not a terminal: a run with </dev/null
// or 2>/dev/null must not prompt or draw a spinner.
func TestDevNullIsNotATerminal(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()
	if IsTTY(f) {
		t.Fatal("IsTTY(/dev/null) = true")
	}
	if IsTTY(nil) {
		t.Fatal("IsTTY(nil) = true")
	}
}

// Columns line up by display width: CJK takes two columns, accents one.
func TestTableAlignsNonLatinText(t *testing.T) {
	DisableColor()
	out := capture(t, &os.Stdout, func() {
		Table([]string{"voice_id", "name", "language"}, [][]string{
			{"v1", "Zoë — naïve café", "english"},
			{"v2", "田中さん", "japanese"},
			{"v3", "Ali", "farsi"},
		})
	})
	want := "" +
		"VOICE_ID  NAME              LANGUAGE\n" +
		"v1        Zoë — naïve café  english\n" +
		"v2        田中さん          japanese\n" +
		"v3        Ali               farsi\n"
	if out != want { // NAME is 16 columns wide on every row, whatever the script
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// Cutting text never splits a UTF-8 character and respects display width.
func TestTruncateKeepsUTF8(t *testing.T) {
	s := "Die Erzeugung ist fehlgeschlagen — ünïcödé ünïcödé ünïcödé ünïcödé"
	for n := 1; n < 60; n++ {
		got := Truncate(s, n)
		if !utf8.ValidString(got) {
			t.Fatalf("n=%d: invalid UTF-8 %q", n, got)
		}
		if Width(got) > n {
			t.Fatalf("n=%d: width %d", n, Width(got))
		}
	}
	if got := Truncate("田中さん田中さん", 5); got != "田中…" {
		t.Fatalf("CJK cut = %q", got)
	}
	if got := Truncate("short", 10); got != "short" {
		t.Fatalf("no cut = %q", got)
	}
}

// The JSON envelope keeps <, > and & as they are.
func TestEnvelopeIsNotHTMLEscaped(t *testing.T) {
	out := capture(t, &os.Stderr, func() {
		PrintErrorEnvelope(clierr.Usage("pass an <idea> & a --brief").Envelope())
	})
	if !strings.Contains(out, `"pass an <idea> & a --brief"`) {
		t.Fatalf("escaped: %s", out)
	}
}

// People on a terminal get a line, not a JSON blob.
func TestHumanErrorIsReadable(t *testing.T) {
	DisableColor()
	e := &clierr.Error{Message: "mood must be one of happy, sad", Hint: "Pick a value from allowed_values.",
		Field: "mood", AllowedValues: []any{"happy", "sad"}, RequestID: "req_1",
		Errors: []any{map[string]any{"error": "aspect is invalid"}}}
	out := capture(t, &os.Stderr, func() { PrintHumanError(e) })
	want := "✗ mood must be one of happy, sad\n" +
		"  Pick a value from allowed_values.\n" +
		"  field: mood\n" +
		"  allowed: happy, sad\n" +
		"  • aspect is invalid\n" +
		"  request id: req_1\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestNoColorWithTermDumb(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if colorEnabled() {
		t.Fatal("TERM=dumb must turn colour off")
	}
}
