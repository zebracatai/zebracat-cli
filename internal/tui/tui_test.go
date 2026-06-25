package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
)

func TestValidDur(t *testing.T) {
	if !validDur(30) || !validDur(180) {
		t.Fatal("expected 30 and 180 to be valid")
	}
	if validDur(45) || validDur(0) {
		t.Fatal("expected 45 and 0 to be invalid")
	}
}

func TestIsTerminal(t *testing.T) {
	for _, s := range []string{"completed", "failed", "cancelled"} {
		if !isTerminal(s) {
			t.Fatalf("%q should be terminal", s)
		}
	}
	for _, s := range []string{"pending", "processing", "rendering"} {
		if isTerminal(s) {
			t.Fatalf("%q should not be terminal", s)
		}
	}
}

func TestRefreshMatchesAutocomplete(t *testing.T) {
	m := &model{in: textinput.New()}
	m.in.SetValue("/v")
	m.refreshMatches()
	found := false
	for _, x := range m.matches {
		if x == "/video" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected /video in autocomplete matches, got %v", m.matches)
	}
	// A non-slash input yields no command matches.
	m.in.SetValue("hello")
	m.refreshMatches()
	if len(m.matches) != 0 {
		t.Fatalf("expected no matches for plain text, got %v", m.matches)
	}
}

func TestWizardFlow(t *testing.T) {
	m := &model{in: textinput.New(), wiz: &wizard{step: 0}}
	m.handleWizard("a cat video", nil)
	if m.wiz == nil || m.wiz.step != 1 || m.wiz.idea != "a cat video" {
		t.Fatalf("after idea: %+v", m.wiz)
	}
	m.handleWizard("30", nil)
	if m.wiz == nil || m.wiz.step != 2 || m.wiz.dur != 30 {
		t.Fatalf("after duration: %+v", m.wiz)
	}
	m.handleWizard("y", nil)
	if m.wiz != nil {
		t.Fatal("wizard should be cleared after submit")
	}
	if !m.busy {
		t.Fatal("model should be busy after submitting the video")
	}
}

func TestWizardRejectsBadDuration(t *testing.T) {
	m := &model{in: textinput.New(), wiz: &wizard{step: 1, idea: "x"}}
	m.handleWizard("45", nil)
	if m.wiz.step != 1 {
		t.Fatal("bad duration should keep the wizard on the duration step")
	}
}

func TestHelpListsEveryCommand(t *testing.T) {
	h := helpText()
	for _, c := range commands {
		if !strings.Contains(h, c.name) {
			t.Fatalf("help text missing %q", c.name)
		}
	}
}

func TestSplashNonEmpty(t *testing.T) {
	if !strings.Contains(splash(), "AI video generation") {
		t.Fatal("splash should contain the tagline")
	}
}
