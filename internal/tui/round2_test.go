package tui

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/zebracatai/zebracat-cli/internal/apitest"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/logo"
	"github.com/zebracatai/zebracat-cli/internal/version"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/golden")

// printed is everything the commands printed above the input, plain.
func printed(ms []tea.Msg) string {
	var b strings.Builder
	for _, m := range ms {
		v := reflect.ValueOf(m)
		if v.Kind() == reflect.Struct && v.Type().Name() == "printLineMessage" {
			b.WriteString(ansi.Strip(v.FieldByName("messageBody").String()) + "\n")
		}
	}
	return b.String()
}

// pollTo drives a created plan to the given task view and returns what printed.
func pollTo(t *testing.T, m *model, srv *apitest.Server, idea, example string) string {
	t.Helper()
	srv.On("GET", "/videos/{task_id}", apitest.Answer{Example: example})
	created := find[createdMsg](t, say(m, idea))
	_, cmd := m.Update(created)
	pm := find[pollMsg](t, msgs(cmd))
	_, cmd = m.Update(pm)
	return printed(msgs(cmd))
}

// "hi" → the plan asks what the video is about: that's Zebracat's turn, and the
// answer goes back with the first message. Never "✗ Video failed".
func TestAQuestionBackIsATurnNotAFailure(t *testing.T) {
	m, srv := against(t)
	out := pollTo(t, m, srv, "hi", "plan_blocked")
	if !strings.Contains(out, "● Hi! What should your video be about?") || strings.Contains(out, "✗") ||
		strings.Contains(strings.ToLower(out), "failed") {
		t.Fatalf("a question back must read as a turn:\n%s", out)
	}
	if m.stage != stageIdle || m.carry != "hi" || m.job != nil {
		t.Fatalf("stage %v carry %q", m.stage, m.carry)
	}
	if !strings.Contains(m.View(), "Your answer") {
		t.Fatalf("the input waits for the answer:\n%s", m.View())
	}
	find[createdMsg](t, say(m, "a 20 second video about why cats love boxes"))
	post, _ := srv.Last("POST", "/videos")
	if post.Body["brief"] != "hi\na 20 second video about why cats love boxes" || m.carry != "" {
		t.Fatalf("the answer goes with the first message: %v", post.Body["brief"])
	}
}

// A brief that asks for another tool says which command does it.
func TestARequestForAnotherToolSaysWhichCommand(t *testing.T) {
	m, srv := against(t)
	out := pollTo(t, m, srv, "translate my video to spanish", "translate_request")
	if !strings.Contains(out, "zebracat video translate") || strings.Contains(out, "✗") {
		t.Fatalf("translate request:\n%s", out)
	}
}

// A real failure is red, with what happened and what to do.
func TestARealFailureSaysWhatToDo(t *testing.T) {
	m, srv := against(t)
	out := pollTo(t, m, srv, "a 10 minute film", "failed")
	if !strings.Contains(out, "✗ The plan couldn't be written: The plan runs longer than this account allows.") ||
		!strings.Contains(out, "⎿ Nothing was charged.") {
		t.Fatalf("failure:\n%s", out)
	}
}

// No interactive path shows a menu: an idea, /new, /video and `create "<idea>"`
// all go straight to a plan; nothing asks "what kind of video" with ↑/↓.
func TestNoMenuWizardAnywhere(t *testing.T) {
	m, srv := against(t)
	for _, step := range []string{"/new", "/video", "a cat video"} {
		say(m, step)
		if v := m.View(); strings.Contains(v, "↑/↓") || strings.Contains(v, "step ") || strings.Contains(v, "What kind of video") {
			t.Fatalf("%s shows a menu:\n%s", step, v)
		}
	}
	if srv.Count("POST", "/videos") != 1 {
		t.Fatal("the idea goes straight to a plan")
	}
	m, _ = against(t)
	m.launch.First = "a dog video"
	_, cmd := m.Update(firstMsg{})
	find[createdMsg](t, msgs(cmd))
	// And no source file of the CLI carries the old wizard.
	for _, dir := range []string{".", "../../cmd"} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			b, _ := os.ReadFile(f)
			for _, bad := range []string{"What kind of video", "wizSteps", "step %d/%d"} {
				if strings.Contains(string(b), bad) {
					t.Fatalf("%s still has the menu wizard (%q)", f, bad)
				}
			}
		}
	}
}

// The launch offer: `u` on an empty line installs the update and the
// conversation ends asking to relaunch the new binary.
func TestUpdateOfferRelaunches(t *testing.T) {
	m, _ := against(t)
	m.offer = "v9.9.9"
	oldL, oldA := latestRelease, applyUpdate
	defer func() { latestRelease, applyUpdate = oldL, oldA }()
	latestRelease = func(context.Context) (string, error) { return "v9.9.9", nil }
	var installed string
	applyUpdate = func(_ context.Context, tag string) (string, error) { installed = tag; return "/tmp/zebracat", nil }

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	done := find[updDoneMsg](t, msgs(cmd))
	if installed != "v9.9.9" || done.path != "/tmp/zebracat" {
		t.Fatalf("installed %q path %q", installed, done.path)
	}
	_, cmd = m.Update(done)
	find[tea.QuitMsg](t, msgs(cmd))
	if m.result.Relaunch != "/tmp/zebracat" || m.result.From != "v"+version.Version {
		t.Fatalf("result %+v", m.result)
	}
}

// Any other first key withdraws the offer, so "u…" later is just typing.
func TestUpdateOfferNeverStealsTyping(t *testing.T) {
	m, _ := against(t)
	m.offer = "v9.9.9"
	called := false
	oldA := applyUpdate
	defer func() { applyUpdate = oldA }()
	applyUpdate = func(context.Context, string) (string, error) { called = true; return "", nil }
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m.in.Reset()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	if called || m.offer != "" || m.in.Value() != "u" {
		t.Fatalf("offer %q value %q called %v", m.offer, m.in.Value(), called)
	}
}

// Esc while the plan is written stops waiting; the late answer is ignored.
func TestEscStopsWaitingAndIgnoresTheLateAnswer(t *testing.T) {
	m, srv := against(t)
	created := find[createdMsg](t, say(m, "a cat video"))
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.stage != stageIdle || m.job != nil {
		t.Fatalf("stage %v", m.stage)
	}
	if _, cmd := m.Update(created); cmd != nil {
		t.Fatal("a late answer for a job the person left does nothing")
	}
	if srv.Count("POST", "/cancel") != 0 {
		t.Fatal("Esc never cancels")
	}
}

// ↑ brings back what was sent; Ctrl-C clears a half-typed line.
func TestHistoryAndCtrlCClears(t *testing.T) {
	m, _ := against(t)
	m.stage = stageMaking // so sending doesn't start anything
	m.job = &job{taskID: taskID, started: time.Now()}
	say(m, "first idea")
	say(m, "second idea")
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.in.Value() != "second idea" {
		t.Fatalf("↑ = %q", m.in.Value())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.in.Value() != "first idea" {
		t.Fatalf("↑↑ = %q", m.in.Value())
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if m.in.Value() != "" || hasQuit(msgs(cmd)) {
		t.Fatal("Ctrl-C on a typed line clears it")
	}
}

// A pasted script keeps its lines (multi-line input).
func TestPastedScriptKeepsItsLines(t *testing.T) {
	m, srv := against(t)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("NARRATOR: line one\nNARRATOR: line two"), Paste: true})
	if m.in.LineCount() != 2 {
		t.Fatalf("lines %d: %q", m.in.LineCount(), m.in.Value())
	}
	find[createdMsg](t, say(m, m.in.Value()))
	post, _ := srv.Last("POST", "/videos")
	if post.Body["brief"] != "NARRATOR: line one\nNARRATOR: line two" {
		t.Fatalf("brief %q", post.Body["brief"])
	}
}

// Offline while polling: one calm line, then quiet retries.
func TestOfflinePollSaysItOnce(t *testing.T) {
	m, _ := against(t)
	m.job = &job{taskID: taskID, started: time.Now(), phase: "planning"}
	m.stage = stagePlanning
	netErr := clierr.API("network error: dial tcp: connection refused")
	netErr.Code = "network_error"
	_, cmd := m.Update(pollMsg{taskID: taskID, err: netErr})
	first := printed(msgs(cmd))
	_, cmd = m.Update(pollMsg{taskID: taskID, err: netErr})
	second := printed(msgs(cmd))
	if !strings.Contains(first, "Lost the connection") || second != "" || !strings.Contains(m.View(), "reconnecting") {
		t.Fatalf("first %q second %q", first, second)
	}
}

// Errors are plain sentences with one fix line.
func TestErrorsArePlain(t *testing.T) {
	m, _ := against(t)
	cases := map[string]*clierr.Error{
		"Your API key was rejected":  clierr.Auth("Invalid API key."),
		"Can't reach Zebracat":       {Exit: 1, Code: "network_error", Message: "network error: x"},
		"Your balance doesn't cover": {Exit: 1, Code: "insufficient_funds", Message: "x", Hint: "Top up at https://studio.zebracat.ai/billing"},
	}
	for want, e := range cases {
		out := printed(msgs(m.sayAPIErr(e, "")))
		if !strings.Contains(out, "✗ "+want) || !strings.Contains(out, "⎿") {
			t.Fatalf("%s:\n%s", want, out)
		}
	}
}

// ---- layout -------------------------------------------------------------------

func layoutModel(t *testing.T, width int, signedIn bool) *model {
	t.Helper()
	lipgloss.SetColorProfile(termenv.Ascii)
	m, _ := against(t)
	if !signedIn {
		m = signedOut(t)
	}
	m.width = width
	m.fitInput()
	m.acct = &account{email: "admin@example.com", apiDollars: "208.25"}
	old := cwd
	cwd = func() string { return "~/projects/coffee" }
	t.Cleanup(func() { cwd = old })
	return m
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".txt")
	if *updateGolden {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, []byte(got), 0o644)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing %s (go test ./internal/tui -update): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("%s differs:\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

func maxWidth(s string) int {
	w := 0
	for _, l := range strings.Split(s, "\n") {
		if n := ansi.StringWidth(l); n > w {
			w = n
		}
	}
	return w
}

// The welcome box and the plan card fit every terminal width (no line ever
// runs into the input box) and render the same each time.
func TestLayoutFitsEveryWidth(t *testing.T) {
	plan := &job{taskID: taskID, editorURL: "https://studio.zebracat.ai/storyboard/48213",
		quote: map[string]any{"credits": float64(96), "usd": "4.80", "pool": "api_dollar",
			"why": "4 AI-video scenes of 5 s at balanced quality."},
		plan: map[string]any{"screenplay": map[string]any{"title": "Why cats knock things off tables", "aspect": "vertical",
			"media_type": "ai_video", "language": "english", "audio": map[string]any{"mode": "narration"},
			"cast":  []any{map[string]any{"name": "Narrator", "voice": map[string]any{"name": "Rachel"}}},
			"beats": []any{map[string]any{"n": float64(1), "seconds": float64(5), "narration": "Ever wondered why your cat swipes your mug off the table? It starts with instinct: cats test what moves, and a falling cup always gets you running."}}}}}
	for _, w := range []int{60, 80, 100, 120} {
		m := layoutModel(t, w, true)
		welcome := m.welcomeView()
		card := m.fit(planCard(plan, w))
		long := m.fit(m.hang("✗ ", strings.Repeat("Hi! What should your video be about? ", 6)))
		for name, s := range map[string]string{"welcome": welcome, "plan": card, "long line": long} {
			if mw := maxWidth(s); mw > w-1 {
				t.Fatalf("%s at %d columns is %d wide:\n%s", name, w, mw, s)
			}
		}
		if v := m.View(); maxWidth(v) > w {
			t.Fatalf("view at %d columns is %d wide:\n%s", w, maxWidth(v), v)
		}
		golden(t, "welcome_"+strconv.Itoa(w), welcome)
		golden(t, "plan_card_"+strconv.Itoa(w), card)
	}
	m := layoutModel(t, 60, false)
	golden(t, "welcome_signed_out_60", m.welcomeView())
}

// NO_COLOR turns every colour off, even on a colour terminal.
func TestNoColor(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	if !strings.Contains(stMark.Render("x"), "\x1b[") {
		t.Skip("this lipgloss build renders no colour here")
	}
	t.Setenv("NO_COLOR", "1")
	applyColorEnv()
	m := layoutModel(t, 80, true)
	lipgloss.SetColorProfile(termenv.TrueColor)
	applyColorEnv()
	if s := m.welcomeView() + planCard(&job{taskID: taskID}, 80); strings.Contains(s, "\x1b[") {
		t.Fatalf("NO_COLOR output has colour codes: %q", s)
	}
}

// The empty input is one row tall at every width (its text never wraps the box).
func TestInputIsOneRow(t *testing.T) {
	for _, w := range []int{60, 80, 100, 120, 160} {
		m := layoutModel(t, w, true)
		m.fitInput()
		if got, room := ansi.StringWidth("› ")+m.in.Width(), m.inputWidth(); got > room {
			t.Fatalf("at %d columns the input line is %d wide but the rules are %d", w, got, room)
		}
		lines := strings.Split(m.View(), "\n")
		if len(lines) < 4 || !strings.HasPrefix(lines[0], "─") || !strings.HasPrefix(lines[2], "─") {
			t.Fatalf("at %d columns the input is not one row between two rules:\n%s", w, m.View())
		}
	}
}

// A poll left running for an abandoned job never lands on the next job, even
// when the server reuses the id.
func TestAnAbandonedPollIsIgnored(t *testing.T) {
	m, _ := against(t)
	old := &job{taskID: taskID}
	m.job = old
	stale := m.pollCmd(taskID, false)
	m.job = &job{taskID: taskID, started: time.Now()}
	m.stage = stagePlanning
	if _, cmd := m.Update(stale()); cmd != nil {
		t.Fatal("a stale poll must do nothing")
	}
}

// /logout with ZEBRACAT_API_KEY set says so instead of pretending.
func TestLogoutWithAnEnvKeyTellsTheTruth(t *testing.T) {
	m, _ := against(t) // ZEBRACAT_API_KEY=sk-dummy
	t.Setenv("HOME", t.TempDir())
	out := printed(msgs(func() tea.Cmd { _, c := m.handleSlash("/logout", nil); return c }()))
	if !strings.Contains(out, "ZEBRACAT_API_KEY") || strings.Contains(out, "Signed out") {
		t.Fatalf("logout:\n%s", out)
	}
}

// The real logo: 17×9 cells, the 1-row mark, symmetric, and plain under NO_COLOR.
func TestLogoGolden(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	golden(t, "logo", bigMark()+"\n"+smallMark()+"\n")
	for _, l := range logo.Big() {
		r := []rune(l)
		if len(r) > 17 {
			t.Fatalf("logo row too wide: %q", l)
		}
	}
}

// The `/` menu shows 8 rows at a time and scrolls with the selection.
func TestSlashMenuScrolls(t *testing.T) {
	m := layoutModel(t, 100, true)
	m.in.SetValue("/")
	m.refreshMatches()
	v := m.View()
	if strings.Count(v, "\n  /")+strings.Count(v, "› /") > menuRows+1 || !strings.Contains(v, "› /help") {
		t.Fatalf("menu:\n%s", v)
	}
	for i := 0; i < len(commands)-1; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if v := m.View(); !strings.Contains(v, "› /exit") || strings.Contains(v, "/help ") {
		t.Fatalf("scrolled menu:\n%s", v)
	}
}
