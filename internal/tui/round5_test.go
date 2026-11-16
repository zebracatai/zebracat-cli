package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Even full screen, the logo is at most 17×10.
func TestLogoIsCappedFullScreen(t *testing.T) {
	m := sized(t, 200, 60)
	if m.logoSize() != 2 {
		t.Fatalf("200×60 picks size %d", m.logoSize())
	}
	rows := 0
	for _, l := range strings.Split(m.welcomeView(), "\n") {
		if strings.ContainsAny(l, "█▙▟▀▄▌▐") {
			rows++
		}
	}
	if rows > 10 {
		t.Fatalf("the full-screen logo is %d rows", rows)
	}
}

// The plan card, footer and menu are in colour on a colour terminal: the price
// green when the balance covers it, red when it doesn't; labels in the accent.
func TestPlanCardColours(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	green, red, accent := ";222;128m", ";113;113m", ";123;255m" // truecolor rounds a channel by one
	j := &job{taskID: taskID, quote: map[string]any{"credits": float64(96), "usd": "4.80", "sufficient": true},
		plan: map[string]any{"screenplay": map[string]any{"title": "Cats", "media_type": "ai_video",
			"beats": []any{map[string]any{"n": float64(1), "seconds": float64(5), "narration": "hi"}}}}}
	card := planCard(j, 80)
	if !strings.Contains(card, green) || !strings.Contains(card, accent) {
		t.Fatalf("an affordable plan: green price, accent labels:\n%q", card)
	}
	j.quote["sufficient"] = false
	if card := planCard(j, 80); !strings.Contains(card, red) {
		t.Fatalf("over balance: red price:\n%q", card)
	}
	m := sized(t, 120, 30)
	lipgloss.SetColorProfile(termenv.TrueColor) // sized() switches to plain
	if f := m.footer(); !strings.Contains(f, accent) || !strings.Contains(f, green) {
		t.Fatalf("footer: plan in the accent, balance green:\n%q", f)
	}
	m.in.SetValue("/")
	m.refreshMatches()
	if !strings.Contains(m.menuView(), accent) {
		t.Fatal("menu names in the accent")
	}
	// NO_COLOR: plain.
	t.Setenv("NO_COLOR", "1")
	applyColorEnv()
	if card := planCard(j, 80); strings.Contains(card, "\x1b[") {
		t.Fatalf("NO_COLOR card has colour: %q", card)
	}
}
