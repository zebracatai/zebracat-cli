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
	m := &model{in: textinput.New(), wiz: newWizard(0, "")}
	// Step 0 is the free-text idea.
	m.in.SetValue("a cat video")
	m.wizAdvance(m.in.Value())
	if m.wiz == nil || m.wiz.step != 1 || m.wiz.vals["idea"] != "a cat video" {
		t.Fatalf("after idea: %+v", m.wiz)
	}
	// Remaining steps are choice steps — accept the highlighted option each time.
	guard := 0
	for m.wiz != nil && guard < 20 {
		m.wizAdvance("")
		guard++
	}
	if m.wiz != nil {
		t.Fatal("wizard should be cleared after the final step")
	}
	if !m.busy {
		t.Fatal("model should be busy after submitting the video")
	}
}

func TestWizardChoiceRecordsValue(t *testing.T) {
	m := &model{in: textinput.New(), wiz: newWizard(1, "x")} // step 1 = video_type (choices)
	if len(m.wiz.cur().choices) == 0 {
		t.Fatal("step 1 should be a choice step")
	}
	m.wiz.cursor = 2 // arrow down twice
	want := m.wiz.cur().choices[2].value
	m.wizAdvance("")
	if m.wiz == nil || m.wiz.step != 2 {
		t.Fatalf("expected to advance to step 2, got %+v", m.wiz)
	}
	if m.wiz.vals["video_type"] != want {
		t.Fatalf("expected video_type=%q, got %q", want, m.wiz.vals["video_type"])
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
