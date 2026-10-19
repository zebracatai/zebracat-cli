// The plan-first ending of the wizard: once Director v2 has written the plan,
// show its summary and the Generate quote, then ask "Generate?" with ↑/↓.
package tui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zebracatai/zebracat-cli/internal/videos"
)

type genPrompt struct {
	taskID       string
	task         map[string]any
	quote        map[string]any
	shouldRender bool
	cursor       int // 0 = generate, 1 = keep the plan
}

func newGenPrompt(task, quote map[string]any) *genPrompt {
	id, _ := task["task_id"].(string)
	render := true
	if v, ok := task["should_render"].(bool); ok {
		render = v
	}
	return &genPrompt{taskID: id, task: task, quote: quote, shouldRender: render}
}

func (g *genPrompt) choices() []string {
	yes := "✨  Yes — generate it"
	if q := videos.QuoteLine(g.quote); q != "" {
		yes += " (" + strings.SplitN(q, " — ", 2)[0] + ")"
	}
	return []string{yes, "📝  Not now — keep the plan"}
}

// genKey drives the Generate? menu.
func (m *model) genKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	g := m.gen
	switch {
	case msg.Type == tea.KeyCtrlC:
		return m, tea.Sequence(tea.Println(stMuted.Render("Goodbye 🦓")), tea.Quit)
	case msg.Type == tea.KeyUp || msg.String() == "k":
		if g.cursor > 0 {
			g.cursor--
		}
	case msg.Type == tea.KeyDown || msg.String() == "j":
		if g.cursor < len(g.choices())-1 {
			g.cursor++
		}
	case msg.Type == tea.KeyEsc:
		return m.genAnswer(false)
	case msg.Type == tea.KeyEnter:
		return m.genAnswer(g.cursor == 0)
	}
	return m, nil
}

func (m *model) genAnswer(yes bool) (tea.Model, tea.Cmd) {
	g := m.gen
	m.gen = nil
	if !yes {
		line := stMuted.Render("Plan kept — nothing was charged. Generate later with ") +
			stKey.Render("zebracat video generate "+g.taskID)
		if u := videos.EditorURL(g.task); u != "" {
			line += "\n" + stMuted.Render("  or open it in the studio: ") + stKey.Render(u)
		}
		return m, tea.Println(line)
	}
	m.busy = true
	return m, tea.Batch(tea.Println(stMuted.Render("  Generate?  ")+stKey.Render("yes")), m.generateCmd(g.taskID, g.shouldRender))
}

func (m *model) genView() string {
	var b strings.Builder
	b.WriteString("\n" + stTitle.Render("  Generate this video?") + "\n\n")
	for i, c := range m.gen.choices() {
		if i == m.gen.cursor {
			b.WriteString("  " + stSel.Render(" ▶ "+c+" ") + "\n")
		} else {
			b.WriteString("     " + stMuted.Render(c) + "\n")
		}
	}
	b.WriteString("\n" + stHint.Render("  ↑/↓ move · Enter select · Esc keep the plan"))
	return b.String()
}

// planCard is the plan summary and its quote, printed to scrollback.
func planCard(task, quote map[string]any) string {
	var b strings.Builder
	b.WriteString(stOK.Render("✓ Your plan is ready.") + "\n")
	for _, l := range videos.SummaryLines(task) {
		b.WriteString("  " + l + "\n")
	}
	if q := videos.QuoteLine(quote); q != "" {
		b.WriteString(stMuted.Render("  Quote: ") + stKey.Render(q) + "\n")
	}
	if u := videos.EditorURL(task); u != "" {
		b.WriteString(stMuted.Render("  See every scene: ") + stKey.Render(u) + "\n")
	}
	return b.String()
}

// questionCard shows the question a plan stopped on.
func questionCard(task map[string]any) string {
	qid, text, opts := videos.Question(task)
	var b strings.Builder
	b.WriteString(stTitle.Render("? ") + text + "\n")
	for _, o := range opts {
		b.WriteString(stMuted.Render("   • ") + o + "\n")
	}
	b.WriteString(stMuted.Render("  Answer with: ") +
		stKey.Render(fmt.Sprintf("zebracat video answer %v --question %s --answer \"…\"", task["task_id"], qid)))
	return b.String()
}

// errText is a task's error as one line (v1 sends a string, v2 an object).
func errText(m map[string]any) string {
	switch e := m["error"].(type) {
	case string:
		return e
	case map[string]any:
		if s, _ := e["message"].(string); s != "" {
			return s
		}
	}
	return ""
}

func newKey() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
