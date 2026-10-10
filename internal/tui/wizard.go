// The guided "make a video" wizard: its questions, its state, how an answer
// advances it, and how it renders.
package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ---- guided "make a video" wizard -----------------------------------------
type wizChoice struct{ label, value string }

// wizStep is one question. No choices => a free-text step (the idea).
type wizStep struct {
	key     string
	prompt  string
	choices []wizChoice
}

var wizSteps = []wizStep{
	{key: "idea", prompt: "What should your video be about?"},
	{key: "video_type", prompt: "What kind of video?", choices: []wizChoice{
		{"🎬  AI Video — cinematic AI-generated footage", "ai_video"},
		{"🖼  Moving AI Images — animated still scenes", "moving_ai_images"},
		{"🧑  Avatar — a presenter delivers your script", "ai_avatar"},
		{"📹  Stock footage — real-world clips", "stock_footage"},
		{"🧠  Brainrot — fast, punchy, viral style", "brainrot"},
	}},
	{key: "duration", prompt: "How long should it be?", choices: []wizChoice{
		{"15 seconds — quick hook", "15"},
		{"30 seconds — standard short", "30"},
		{"60 seconds — full short", "60"},
		{"2 minutes", "120"},
		{"3 minutes", "180"},
	}},
	{key: "aspect_ratio", prompt: "Which format?", choices: []wizChoice{
		{"📱  Vertical · 9:16 — TikTok, Reels, Shorts", "vertical"},
		{"⬛  Square · 1:1 — feed posts", "square"},
		{"🖥  Horizontal · 16:9 — YouTube", "horizontal"},
	}},
	{key: "should_render", prompt: "Render the final video now?", choices: []wizChoice{
		{"✨  Yes — render the MP4 now", "yes"},
		{"📝  No — save an editable draft", "no"},
	}},
}

type wizard struct {
	step   int
	cursor int
	vals   map[string]string
}

func (w *wizard) cur() wizStep { return wizSteps[w.step] }

func newWizard(startStep int, idea string) *wizard {
	w := &wizard{step: startStep, vals: map[string]string{}}
	if idea != "" {
		w.vals["idea"] = idea
	}
	return w
}

// wizView renders the guided wizard: a bold prompt, then either a text box (the
// idea) or an arrow-key choice list with the current row highlighted.
func (m *model) wizView() string {
	step := m.wiz.cur()
	var b strings.Builder
	b.WriteString("\n" + stTitle.Render("  "+step.prompt) +
		stMuted.Render(fmt.Sprintf("    step %d/%d", m.wiz.step+1, len(wizSteps))) + "\n\n")

	if len(step.choices) == 0 { // free-text step (the idea)
		w := m.width
		if w < 24 {
			w = 60
		}
		b.WriteString(stInput.Width(w-2).Render(m.in.View()) + "\n")
		b.WriteString(stHint.Render("  Enter to continue · Esc to cancel"))
		return b.String()
	}

	for i, c := range step.choices {
		if i == m.wiz.cursor {
			b.WriteString("  " + stSel.Render(" ▶ "+c.label+" ") + "\n")
		} else {
			b.WriteString("     " + stMuted.Render(c.label) + "\n")
		}
	}
	b.WriteString("\n" + stHint.Render("  ↑/↓ move · Enter select · Esc cancel"))
	return b.String()
}

// wizAdvance records the current step's answer (a typed idea, or the highlighted
// choice), echoes it to scrollback, and either moves to the next step or submits.
func (m *model) wizAdvance(textVal string) (tea.Model, tea.Cmd) {
	step := m.wiz.cur()
	var answer, display string
	if len(step.choices) == 0 {
		answer = strings.TrimSpace(textVal)
		if answer == "" {
			return m, nil // wait for a non-empty idea
		}
		m.in.Reset()
		display = answer
	} else {
		c := step.choices[m.wiz.cursor]
		answer, display = c.value, c.label
	}
	m.wiz.vals[step.key] = answer
	echo := tea.Println(stMuted.Render("  "+step.prompt+"  ") + stKey.Render(display))

	m.wiz.step++
	m.wiz.cursor = 0
	if m.wiz.step < len(wizSteps) {
		return m, echo
	}

	// All answered — build the request and submit.
	v := m.wiz.vals
	m.wiz = nil
	m.busy = true
	body := map[string]any{
		"prompt":        v["idea"],
		"video_type":    v["video_type"],
		"duration":      atoiOr(v["duration"], 30),
		"aspect_ratio":  v["aspect_ratio"],
		"should_render": v["should_render"] == "yes",
	}
	return m, tea.Batch(echo, tea.Println(stOK.Render("✓ ")+stMuted.Render("Building your video…")), m.createCmd(body))
}

func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}
