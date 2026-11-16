package tui

import (
	"strings"
	"testing"

	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/config"
)

// signedOut builds a model whose client has no credentials at all.
func signedOut(t *testing.T) *model {
	t.Setenv("ZEBRACAT_API_KEY", "")
	cl := client.New("http://example.invalid", &config.Credentials{}, "")
	m := newModel(cl, "")
	m.width = 100
	return m
}

// signedIn builds a model authenticated via an API key flag.
func signedIn(t *testing.T) *model {
	t.Setenv("ZEBRACAT_API_KEY", "")
	cl := client.New("http://example.invalid", &config.Credentials{}, "sk-test")
	m := newModel(cl, "")
	m.width = 100
	return m
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
	m := newModel(client.New("http://example.invalid", &config.Credentials{}, ""), "")
	m.in.SetValue("/g")
	m.refreshMatches()
	found := false
	for _, x := range m.matches {
		if x.name == "/generate" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected /generate in autocomplete matches, got %v", m.matches)
	}
	// A non-slash input yields no command matches.
	m.in.SetValue("hello")
	m.refreshMatches()
	if len(m.matches) != 0 {
		t.Fatalf("expected no matches for plain text, got %v", m.matches)
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

func TestGatedWhenSignedOut(t *testing.T) {
	m := signedOut(t)
	if m.cl.IsAuthenticated() {
		t.Fatal("test setup: client should be signed out")
	}
	// A command that spends credits must not start anything while signed out.
	m.handleSlash("/new", nil)
	if m.job != nil || m.stage != stageIdle {
		t.Fatal("/new should be gated when signed out")
	}
	if m.busy {
		t.Fatal("gated command should not mark the model busy")
	}
	// Bare text (a video idea) must be gated too.
	m.in.SetValue("a cat skateboarding")
	m.handleEnter()
	if m.job != nil || m.stage != stageIdle {
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
	if _, cmd := m.handleSlash("/new", nil); cmd == nil || m.stage != stageIdle || m.job != nil {
		t.Fatalf("/new should start over when signed in, got stage %v job %+v", m.stage, m.job)
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
