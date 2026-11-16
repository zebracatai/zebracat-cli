package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/zebracatai/zebracat-cli/internal/logo"
)

// sized builds a signed-in model for a cols×rows terminal.
func sized(t *testing.T, cols, rows int) *model {
	t.Helper()
	m := layoutModel(t, cols, true)
	m.height = rows
	m.acct = &account{email: "admin@example.com", plan: "Cat Mode", credits: "1,240", apiDollars: "208.25"}
	m.fitInput()
	return m
}

// Every logo size keeps whole stripes and matching ears.
func TestLogoSizesGolden(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	var b strings.Builder
	for _, s := range logo.Sizes {
		c, r := logo.Dimensions(s)
		fmt.Fprintf(&b, "== size %d (%d×%d)\n%s\n", s, c, r, strings.Join(logo.Sized(s), "\n"))
		for _, l := range logo.Sized(s) {
			rs := []rune(l)
			for i := 0; i < len(rs)/2; i++ { // symmetric: a cell and its mirror both filled or both empty
				if (rs[i] == ' ') != (rs[len(rs)-1-i] == ' ') && len(rs) == c {
					t.Fatalf("size %d row %q is not symmetric", s, l)
				}
			}
		}
	}
	fmt.Fprintf(&b, "== 1-row\n%s\n", logo.Small())
	golden(t, "logo_sizes", b.String())
}

// The welcome picks its logo from the window: big, normal, small, the 1-row mark.
func TestWelcomeLogoFollowsTheWindow(t *testing.T) {
	cases := []struct {
		cols, rows, size int
	}{{200, 60, 4}, {120, 40, 2}, {80, 24, 2}, {60, 20, 1}, {38, 12, 0}}
	for _, c := range cases {
		m := sized(t, c.cols, c.rows)
		if got := m.logoSize(); got != c.size {
			t.Fatalf("%d×%d: logo size %d, want %d", c.cols, c.rows, got, c.size)
		}
		w := m.welcomeView()
		if mw := maxWidth(w); mw > c.cols-1 {
			t.Fatalf("%d×%d welcome is %d wide", c.cols, c.rows, mw)
		}
		if c.cols >= 60 {
			golden(t, fmt.Sprintf("welcome_%dx%d", c.cols, c.rows), w)
		}
	}
}

// The whole screen: the welcome on top, the input and footer on the LAST rows.
func TestInputSitsOnTheLastRows(t *testing.T) {
	for _, sz := range [][2]int{{60, 20}, {80, 24}, {120, 40}, {200, 60}} {
		m := sized(t, sz[0], sz[1])
		welcome := m.welcomeView()
		m.printed = lineCount(welcome)
		screen := welcome + "\n" + m.View()
		lines := strings.Split(screen, "\n")
		if len(lines) != sz[1] {
			t.Fatalf("%d×%d: the screen is %d lines, want exactly %d", sz[0], sz[1], len(lines), sz[1])
		}
		n := len(lines)
		if !strings.HasPrefix(lines[n-5], "─") || !strings.HasPrefix(lines[n-4], "› ") || !strings.HasPrefix(lines[n-3], "─") {
			t.Fatalf("%d×%d: input not on the last rows:\n%s", sz[0], sz[1], screen)
		}
		if sz[0] == 80 {
			golden(t, "screen_80x24", screen)
		}
	}
}

// As the conversation grows the input stays at the bottom; once the screen is
// full there is no padding left and output scrolls above it.
func TestConversationScrollsAboveTheInput(t *testing.T) {
	m := sized(t, 80, 24)
	m.printed = lineCount(m.welcomeView())
	before := lipgloss.Height(m.View())
	m.say("one line")
	if lipgloss.Height(m.View()) != before-1 {
		t.Fatal("a printed line takes one row from the padding")
	}
	for i := 0; i < 40; i++ {
		m.say("more")
	}
	if lipgloss.Height(m.View()) != lipgloss.Height(m.liveView()) {
		t.Fatal("a full screen has no padding left")
	}
}

// The footer: hints, then plan · balance · credits · account · version, cut
// from the end on narrow terminals, never wrapped.
func TestFooterGolden(t *testing.T) {
	for _, w := range []int{60, 80, 120} {
		m := sized(t, w, 30)
		f := m.footer()
		if strings.Count(f, "\n") != 1 {
			t.Fatalf("footer at %d is not two lines:\n%s", w, f)
		}
		for _, l := range strings.Split(f, "\n") {
			if ansi.StringWidth(l) > m.inputWidth() {
				t.Fatalf("footer line at %d is %d wide: %q", w, ansi.StringWidth(l), l)
			}
		}
		if !strings.Contains(f, "Cat Mode") || !strings.Contains(f, "$208.25 API balance") {
			t.Fatalf("footer at %d lost the plan or balance:\n%s", w, f)
		}
		golden(t, fmt.Sprintf("footer_%d", w), f)
	}
	m := sized(t, 120, 30)
	if f := m.footer(); !strings.Contains(f, "1,240 credits") || !strings.Contains(f, "admin@example.com") || !strings.Contains(f, "v") {
		t.Fatalf("a wide footer shows everything:\n%s", f)
	}
}

// A resize re-renders the welcome at the new size, once the dragging stops.
func TestResizeRedrawsTheWelcome(t *testing.T) {
	m := sized(t, 200, 60)
	m.welcomed = true
	m.blocks = []block{{render: m.welcomeView}, {render: func() string { return "› hi" }}}
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	if cmd == nil || m.logoSize() != 1 {
		t.Fatalf("resize: size %d", m.logoSize())
	}
	_, cmd = m.Update(resizeMsg{gen: m.resizes})
	if cmd == nil || m.printed == 0 || m.printed > 20 {
		t.Fatalf("redraw printed %d lines", m.printed)
	}
	if _, cmd := m.Update(resizeMsg{gen: m.resizes - 1}); cmd != nil {
		t.Fatal("a stale resize does nothing")
	}
}

// A plan card printed wide is drawn again as a proper box after shrinking.
func TestResizeRedrawsCardsAtTheNewWidth(t *testing.T) {
	m := sized(t, 120, 40)
	j := &job{taskID: taskID, plan: map[string]any{"screenplay": map[string]any{"title": "Cats",
		"beats": []any{map[string]any{"n": float64(1), "seconds": float64(5), "narration": strings.Repeat("words ", 30)}}}}}
	snap := *j
	m.sayR(func() string { return planCard(&snap, m.width) })
	m.width, m.height = 60, 20
	m.redraw()
	card := m.fit(m.blocks[0].render())
	lines := strings.Split(card, "\n")
	if !strings.HasPrefix(lines[0], "╭") || maxWidth(card) > 59 {
		t.Fatalf("card after shrink:\n%s", card)
	}
	for _, l := range lines[1 : len(lines)-2] {
		if !strings.HasPrefix(l, "│") || !strings.HasSuffix(strings.TrimRight(l, " "), "│") {
			t.Fatalf("broken box line %q in:\n%s", l, card)
		}
	}
}

// The `/` menu opens above the input and the footer stays under it.
func TestMenuKeepsTheFooter(t *testing.T) {
	m := sized(t, 80, 24)
	m.in.SetValue("/")
	m.refreshMatches()
	v := m.liveView()
	lines := strings.Split(v, "\n")
	n := len(lines)
	if !strings.Contains(lines[n-1], "Cat Mode") || !strings.HasPrefix(lines[n-4], "› /") || !strings.Contains(lines[0], "› /help") {
		t.Fatalf("menu layout:\n%s", v)
	}
}

// After a spend the footer's balance updates quietly (nothing printed).
func TestFooterUpdatesAfterASpend(t *testing.T) {
	m := sized(t, 120, 30)
	_, cmd := m.Update(acctMsg{a: &account{email: "admin@example.com", plan: "Cat Mode", credits: "1,240", apiDollars: "203.45"}, quiet: true})
	if cmd != nil || !strings.Contains(m.footer(), "$203.45 API balance") {
		t.Fatalf("footer after refresh: %s", m.footer())
	}
}
