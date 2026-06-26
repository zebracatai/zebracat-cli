package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"

	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/config"
)

// signedOut builds a model whose client has no credentials at all.
func signedOut(t *testing.T) *model {
	t.Setenv("ZEBRACAT_API_KEY", "")
	cl := client.New("http://example.invalid", &config.Credentials{}, "")
	return &model{in: textinput.New(), cl: cl}
}

// signedIn builds a model authenticated via an API key flag.
func signedIn(t *testing.T) *model {
	t.Setenv("ZEBRACAT_API_KEY", "")
	cl := client.New("http://example.invalid", &config.Credentials{}, "sk-test")
	return &model{in: textinput.New(), cl: cl}
}

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

func TestGatedWhenSignedOut(t *testing.T) {
	m := signedOut(t)
	if m.cl.IsAuthenticated() {
		t.Fatal("test setup: client should be signed out")
	}
	// A command that spends credits must not start the wizard while signed out.
	m.handleSlash("/video", nil)
	if m.wiz != nil {
		t.Fatal("/video should be gated (no wizard) when signed out")
	}
	if m.busy {
		t.Fatal("gated command should not mark the model busy")
	}
	// Bare text (a video idea) must be gated too.
	m.in.SetValue("a cat skateboarding")
	m.handleEnter()
	if m.wiz != nil {
		t.Fatal("bare-text idea should be gated when signed out")
	}
	// But /help and /login stay available.
	if _, cmd := m.handleSlash("/help", nil); cmd == nil {
		t.Fatal("/help should work when signed out")
	}
}

func TestAllowedWhenSignedIn(t *testing.T) {
	m := signedIn(t)
	if !m.cl.IsAuthenticated() {
		t.Fatal("test setup: client should be signed in")
	}
	m.handleSlash("/video", nil)
	if m.wiz == nil || m.wiz.step != 0 {
		t.Fatalf("/video should start the wizard when signed in, got %+v", m.wiz)
	}
}

func TestGroupThousands(t *testing.T) {
	cases := map[string]string{"0": "0", "100": "100", "1000": "1,000", "1234567": "1,234,567", "-1500": "-1,500"}
	for in, want := range cases {
		if got := groupThousands(in); got != want {
			t.Fatalf("groupThousands(%q) = %q, want %q", in, got, want)
		}
	}
}
