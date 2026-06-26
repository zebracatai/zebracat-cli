// Package ui handles all terminal output: machine-readable JSON by default,
// pretty "human" rendering on request, plus the Zebracat brand (purple + zebra).
//
// Design contract (shared with HeyGen-style agent tooling):
//   - JSON on stdout (stable, unaltered) so output is pipeable.
//   - Structured error envelope on stderr.
//   - Color/spinners only on a TTY and only in human mode.
package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// Brand colors (Zebracat purple) + basics. Empty when color is disabled.
var (
	purple = "\x1b[38;5;141m"
	bold   = "\x1b[1m"
	dim    = "\x1b[2m"
	green  = "\x1b[32m"
	red    = "\x1b[31m"
	yellow = "\x1b[33m"
	reset  = "\x1b[0m"
)

func init() {
	if !colorEnabled() {
		purple, bold, dim, green, red, yellow, reset = "", "", "", "", "", "", ""
	}
}

func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return isTTY(os.Stdout)
}

// isTTY reports whether f is a character device (a real terminal).
func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// IsTTY reports whether f is an interactive terminal (exported for callers that
// want to gate human-only output, e.g. the update notice).
func IsTTY(f *os.File) bool { return isTTY(f) }

// Logo is the Zebracat wordmark + zebra, shown on `--help` and `version`.
const logoArt = `   ____     _                          _
  |_  /___ | |__ _ _ __ _ __ __ _ _ __| |_
   / // -_)| '_ \ '_/ _` + "`" + ` / _/ _` + "`" + ` |  _|
  /___\___||_.__/_| \__,_\__\__,_|\__|`

// Banner returns the branded banner for help/version screens.
func Banner() string {
	return fmt.Sprintf("%s%s%s\n  %sAI video generation, from your terminal.%s\n", purple+bold, logoArt, reset, dim, reset)
}

// PrintJSON writes v as indented JSON to stdout.
func PrintJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// PrintError writes the structured error envelope to stderr.
func PrintError(code, message, hint string) {
	env := map[string]any{"error": map[string]string{"code": code, "message": message}}
	if hint != "" {
		env["error"].(map[string]string)["hint"] = hint
	}
	b, _ := json.MarshalIndent(env, "", "  ")
	fmt.Fprintln(os.Stderr, string(b))
}

// Success prints a green check line (human mode) to stderr so it never pollutes
// piped JSON on stdout.
func Success(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s✓%s %s\n", green, reset, fmt.Sprintf(format, args...))
}

// Info prints a dim informational line to stderr.
func Info(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s%s%s\n", dim, fmt.Sprintf(format, args...), reset)
}

// Warn prints a yellow warning to stderr.
func Warn(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s!%s %s\n", yellow, reset, fmt.Sprintf(format, args...))
}

// Heading prints a purple bold heading (human mode).
func Heading(s string) {
	fmt.Printf("%s%s%s%s\n", purple, bold, s, reset)
}

// KV prints aligned key/value pairs (human mode). order preserves field order.
func KV(pairs [][2]string) {
	w := 0
	for _, p := range pairs {
		if len(p[0]) > w {
			w = len(p[0])
		}
	}
	for _, p := range pairs {
		fmt.Printf("  %s%-*s%s  %s\n", purple, w, p[0], reset, p[1])
	}
}

// Link prints a labelled, highlighted URL (human mode).
func Link(label, url string) {
	fmt.Printf("  %s%s%s %s%s%s\n", dim, label, reset, purple+bold, url, reset)
}

// Colour wrappers (no-ops when colour is disabled, e.g. piped output).
func Green(s string) string  { return green + s + reset }
func Red(s string) string    { return red + s + reset }
func Yellow(s string) string { return yellow + s + reset }
func Dim(s string) string    { return dim + s + reset }
func Purple(s string) string { return purple + s + reset }

// ---- generic themed rendering (for commands without a bespoke renderer) -----

// Auto renders decoded JSON the themed way: an object becomes a key/value block,
// an array of objects becomes a table, a wrapper {count, items:[...]} shows both.
func Auto(v any) {
	switch t := v.(type) {
	case map[string]any:
		if key, arr := wrappedList(t); arr != nil {
			var meta [][2]string
			for _, k := range SortedKeys(t) {
				if k == key {
					continue
				}
				if s, ok := scalarStr(t[k]); ok {
					meta = append(meta, [2]string{k, s})
				}
			}
			if len(meta) > 0 {
				KV(meta)
				fmt.Println()
			}
			renderRows(arr)
			Info("%d %s", len(arr), key)
			return
		}
		renderObject(t)
	case []any:
		renderRows(t)
		Info("%d items", len(t))
	default:
		fmt.Println(v)
	}
}

func renderObject(m map[string]any) {
	var pairs [][2]string
	for _, k := range objectKeys(m) {
		if s, ok := scalarStr(m[k]); ok {
			pairs = append(pairs, [2]string{k, s})
		} else {
			pairs = append(pairs, [2]string{k, dim + "{…}" + reset})
		}
	}
	KV(pairs)
}

func renderRows(arr []any) {
	if len(arr) == 0 {
		Info("(none)")
		return
	}
	first, ok := arr[0].(map[string]any)
	if !ok {
		for _, e := range arr {
			if s, ok := scalarStr(e); ok {
				fmt.Println("  " + s)
			}
		}
		return
	}
	cols := pickColumns(first)
	rows := make([][]string, 0, len(arr))
	for _, e := range arr {
		m, _ := e.(map[string]any)
		row := make([]string, len(cols))
		for i, c := range cols {
			s, _ := scalarStr(m[c])
			row[i] = truncate(s, 44)
		}
		rows = append(rows, row)
	}
	Table(cols, rows)
}

// wrappedList finds an array field inside a wrapper object (the response payload).
func wrappedList(m map[string]any) (string, []any) {
	for _, k := range []string{"videos", "voices", "avatars", "styles", "visual_styles", "templates", "characters", "brands", "music", "prices", "items", "results", "data"} {
		if a, ok := m[k].([]any); ok {
			return k, a
		}
	}
	for _, k := range SortedKeys(m) { // fall back to any array field
		if a, ok := m[k].([]any); ok {
			return k, a
		}
	}
	return "", nil
}

func pickColumns(m map[string]any) []string {
	pref := []string{"id", "task_id", "name", "title", "label", "voice_id", "status", "video_type", "type", "language", "gender", "accent", "category", "mood", "duration", "email", "plan", "service", "created_at"}
	var cols []string
	seen := map[string]bool{}
	for _, k := range pref {
		if _, ok := m[k]; ok {
			cols = append(cols, k)
			seen[k] = true
			if len(cols) >= 5 {
				return cols
			}
		}
	}
	for _, k := range SortedKeys(m) {
		if seen[k] {
			continue
		}
		if _, ok := scalarStr(m[k]); ok {
			cols = append(cols, k)
			if len(cols) >= 5 {
				break
			}
		}
	}
	return cols
}

func objectKeys(m map[string]any) []string {
	pref := []string{"id", "task_id", "name", "email", "plan", "status", "video_type"}
	var keys []string
	seen := map[string]bool{}
	for _, k := range pref {
		if _, ok := m[k]; ok {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	for _, k := range SortedKeys(m) {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	return keys
}

func scalarStr(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "—", true
	case string:
		return x, true
	case bool:
		return fmt.Sprintf("%v", x), true
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x)), true
		}
		return fmt.Sprintf("%g", x), true
	case json.Number:
		return x.String(), true
	}
	return "", false
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// Table prints a simple aligned table (human mode). headers + rows of strings.
func Table(headers []string, rows [][]string) {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, c := range row {
			if i < len(widths) && len(c) > widths[i] {
				widths[i] = len(c)
			}
		}
	}
	var sb strings.Builder
	for i, h := range headers {
		sb.WriteString(fmt.Sprintf("%s%-*s%s  ", purple+bold, widths[i], strings.ToUpper(h), reset))
	}
	fmt.Println(strings.TrimRight(sb.String(), " "))
	for _, row := range rows {
		var line strings.Builder
		for i := range headers {
			c := ""
			if i < len(row) {
				c = row[i]
			}
			line.WriteString(fmt.Sprintf("%-*s  ", widths[i], c))
		}
		fmt.Println(strings.TrimRight(line.String(), " "))
	}
}

// SortedKeys is a small helper for stable map rendering.
func SortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Spinner is a minimal stderr spinner shown only on a TTY in human mode.
type Spinner struct {
	stop chan struct{}
	done chan struct{}
	on   bool
}

// StartSpinner begins a spinner with the given label, or a no-op off a TTY.
func StartSpinner(label string) *Spinner {
	s := &Spinner{stop: make(chan struct{}), done: make(chan struct{})}
	if !isTTY(os.Stderr) {
		close(s.done)
		return s
	}
	s.on = true
	go func() {
		defer close(s.done)
		frames := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
		i := 0
		t := time.NewTicker(90 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-s.stop:
				fmt.Fprint(os.Stderr, "\r\033[K")
				return
			case <-t.C:
				fmt.Fprintf(os.Stderr, "\r%s%c%s %s", purple, frames[i%len(frames)], reset, label)
				i++
			}
		}
	}()
	return s
}

// Stop ends the spinner.
func (s *Spinner) Stop() {
	if s.on {
		close(s.stop)
	}
	<-s.done
}
