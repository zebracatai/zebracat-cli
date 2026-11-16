// Rendering: printing helpers (wrapped to the terminal), the welcome box, the
// input box with its completion list and footer, the projects dashboard.
package tui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/zebracatai/zebracat-cli/internal/dash"
	"github.com/zebracatai/zebracat-cli/internal/version"
	"github.com/zebracatai/zebracat-cli/internal/videos"
)

// ---- printing ---------------------------------------------------------------
// Everything printed above the input goes through say, wrapped to the terminal,
// so a long line can never run into the input box.

// cols is the usable width (one column spare: a full-width line makes some
// terminals wrap early).
func (m *model) cols() int {
	if m.width <= 0 {
		return 79
	}
	if m.width < 20 {
		return 19
	}
	return m.width - 1
}

// fit wraps every line wider than the terminal.
func (m *model) fit(s string) string {
	w := m.cols()
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if ansi.StringWidth(l) > w {
			lines[i] = ansi.Wrap(l, w, "")
		}
	}
	return strings.Join(lines, "\n")
}

func (m *model) say(s string) tea.Cmd { return tea.Println(m.fit(s)) }

// hang wraps body under a prefix with a hanging indent.
func (m *model) hang(prefix, body string) string {
	pw := ansi.StringWidth(prefix)
	w := m.cols() - pw
	if w < 10 {
		w = 10
	}
	lines := strings.Split(ansi.Wrap(body, w, ""), "\n")
	pad := strings.Repeat(" ", pw)
	for i := range lines {
		if i == 0 {
			lines[i] = prefix + lines[i]
		} else {
			lines[i] = pad + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// youSaid echoes what the person sent, dimmed, like a chat transcript.
func (m *model) youSaid(text string) tea.Cmd {
	return m.say(m.hang(stMuted.Render("› "), stMuted.Render(strings.TrimSpace(text))))
}

// assistant is Zebracat's turn: a purple dot, then the words.
func (m *model) assistant(text string) tea.Cmd { return m.say(m.hang(stMark.Render("● "), text)) }

// note is a dimmed tool/progress line under a turn.
func (m *model) note(text string) tea.Cmd {
	return m.say(m.hang(stMuted.Render("  ⎿ "), stMuted.Render(text)))
}

// sayErr is a red line with what happened and one line with what to do.
func (m *model) sayErr(what, fix string) tea.Cmd {
	s := m.hang(stErr.Render("✗ "), stErr.Render(clean(what)))
	if fix != "" {
		s += "\n" + m.hang(stMuted.Render("  ⎿ "), stMuted.Render(fix))
	}
	return m.say(s)
}

// ---- welcome --------------------------------------------------------------------

// welcomeView is the launch box: our mark, the version, who is signed in and
// their balance, the folder, then tips (and an update offer when there is one).
func (m *model) welcomeView() string {
	inner := m.cols() - 4 // border + padding
	if inner > 72 {
		inner = 72
	}
	var text []string
	text = append(text, stTitle.Render("Welcome to Zebracat")+stMuted.Render("  v"+version.Version))
	if m.launch.JustUpdated != "" {
		text = append(text, stOK.Render("✓ Updated from "+m.launch.JustUpdated))
	}
	if m.cl != nil && m.cl.IsAuthenticated() {
		if a := m.accountLine(); a != "" {
			text = append(text, strings.TrimPrefix(a, " · "))
		} else {
			text = append(text, stMuted.Render("Signed in"))
		}
	} else {
		text = append(text, stWorking.Render("Not signed in")+stMuted.Render(" · type /login"))
	}
	if d := cwd(); d != "" {
		text = append(text, stMuted.Render(d))
	}
	markW := ansi.StringWidth(mark[0])
	var body string
	if inner >= markW+3+24 { // side by side
		col := lipgloss.NewStyle().Width(inner - markW - 3).Render(strings.Join(text, "\n"))
		body = lipgloss.JoinHorizontal(lipgloss.Center, markView(), "   ", col)
	} else { // narrow: the mark above the words
		col := lipgloss.NewStyle().Width(inner).Render(strings.Join(text, "\n"))
		body = markView() + "\n\n" + col
	}
	out := stBox.Render(body) + "\n"
	tips := []string{
		m.hang(stMuted.Render(" › "), stMuted.Render("Describe a video, paste a script or a link (TikTok, YouTube, Reels)")),
		m.hang(stMuted.Render(" › "), stKey.Render("/help")+stMuted.Render(" for commands and keys · ")+stKey.Render("Ctrl+C")+stMuted.Render(" twice to quit")),
	}
	if m.cl == nil || !m.cl.IsAuthenticated() {
		tips = append(tips, m.hang(stMuted.Render(" › "), stKey.Render("/login")+stMuted.Render(" to add your API key (studio.zebracat.ai → API Keys)")))
	}
	out += strings.Join(tips, "\n")
	if m.offer != "" {
		out += "\n" + m.hang(stWorking.Render(" ↑ "), "Zebracat CLI "+m.offer+" is available"+stMuted.Render(" — press ")+
			stKey.Render("u")+stMuted.Render(" to update now"))
	}
	if m.shadow != "" {
		out += "\n" + m.hang(stWorking.Render(" ! "), stMuted.Render("Another zebracat runs first on your PATH: "+m.shadow+" — run ")+
			stKey.Render("zebracat doctor"))
	}
	return m.fit(out)
}

// accountLine is " · you@x.com · Pro · $208.25 API balance" (or plan credits).
func (m *model) accountLine() string {
	if m.acct == nil {
		return ""
	}
	var parts []string
	if m.acct.email != "" {
		parts = append(parts, m.acct.email)
	}
	if m.cl != nil && m.cl.AuthMode() == "api_key" {
		if m.acct.apiDollars != "" {
			parts = append(parts, "$"+m.acct.apiDollars+" API balance")
		}
	} else {
		if m.acct.plan != "" {
			parts = append(parts, m.acct.plan)
		}
		if m.acct.credits != "" {
			parts = append(parts, m.acct.credits+" credits")
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " · " + strings.Join(parts, " · ")
}

// ---- the live view under the conversation ----------------------------------------

func (m *model) View() string {
	if m.quitting {
		return ""
	}
	if m.stage == stageConfirm {
		return m.confirmView()
	}
	var b strings.Builder
	if p := m.progressLine(); p != "" {
		b.WriteString(m.fit(p) + "\n")
	}
	w := m.inputWidth()
	var field string
	if m.askKey {
		field = stPrompt.Render("🔑 ") + m.key.View()
	} else {
		field = lipgloss.JoinHorizontal(lipgloss.Top, stPrompt.Render("› "), m.in.View())
	}
	b.WriteString(stInput.Width(w-2).Render(field) + "\n")
	if len(m.matches) > 0 {
		for i, c := range m.matches {
			line := fmt.Sprintf("  %-10s %s", c.name, c.desc)
			if i == m.sel {
				b.WriteString(stSel.Render(ansi.Truncate(line, w, "…")) + "\n")
			} else {
				b.WriteString(stMuted.Render(ansi.Truncate(line, w, "…")) + "\n")
			}
		}
		return b.String()
	}
	b.WriteString(ansi.Truncate(m.footer(), w, "…"))
	return b.String()
}

// inputWidth is the input box's full width (border included), at most 120.
func (m *model) inputWidth() int {
	w := m.cols()
	if w > 120 {
		w = 120
	}
	return w
}

// footer is the hint line under the input.
func (m *model) footer() string {
	if !m.quitAt.IsZero() {
		return stWorking.Render("  Press Ctrl-C again to exit")
	}
	var hint string
	switch m.stage {
	case stageAsking:
		hint = "type your answer · its number picks an option"
	case stageReady:
		hint = "type a change · ctrl+g generate · /new start over"
	case stageDone:
		hint = "ctrl+o open the video · type a new idea"
	case stagePlanning, stageEditing, stageMaking:
		hint = ""
	default:
		hint = "/ for commands · alt+enter new line · ↑ history"
	}
	if m.offer != "" && m.stage == stageIdle {
		hint = "u update to " + m.offer + " · " + hint
	}
	parts := []string{}
	if hint != "" {
		parts = append(parts, stMuted.Render(hint))
	}
	if a := m.accountLine(); a != "" {
		parts = append(parts, stMuted.Render(strings.TrimPrefix(a, " · ")))
	} else if m.cl == nil || !m.cl.IsAuthenticated() {
		parts = append(parts, stWorking.Render("not signed in"))
	}
	return "  " + strings.Join(parts, stMuted.Render(" · "))
}

// syncPlaceholder makes the input hint match the conversation.
func (m *model) syncPlaceholder() {
	switch {
	case m.cl == nil || !m.cl.IsAuthenticated():
		m.in.Placeholder = "Type /login to get started"
	case m.stage == stageReady:
		m.in.Placeholder = "Tell me what to change, or press Ctrl+G to generate"
	case m.stage == stageAsking:
		m.in.Placeholder = "Your answer"
	case m.carry != "":
		m.in.Placeholder = "Your answer"
	default:
		m.in.Placeholder = "Describe your video, paste a script or a link"
	}
}

// dashboardView renders the project list like a dashboard: a coloured status
// badge, a friendly type, when it was made, and a studio link (or failure
// reason) — no task IDs or JSON.
func (m *model) dashboardView(out map[string]any) string {
	vids, _ := out["videos"].([]any)
	var b strings.Builder
	b.WriteString(stTitle.Render("Your videos") + "\n\n")
	if len(vids) == 0 {
		return b.String() + stMuted.Render("  Nothing here yet — type ") + stKey.Render("an idea") + stMuted.Render(" to make one.")
	}
	for _, v := range vids {
		mm, _ := v.(map[string]any)
		status := fmt.Sprint(mm["status"])
		var st lipgloss.Style
		switch dash.Kind(status) {
		case "ok":
			st = stOK
		case "failed":
			st = stErr
		case "working":
			st = stWorking
		default:
			st = stMuted
		}
		badge := st.Render(fmt.Sprintf("%s %-10s", dash.Icon(status), dash.Label(status)))
		kind := stMuted.Render(fmt.Sprintf("%-16s", dash.HumanType(fmt.Sprint(mm["video_type"]))))
		when := stMuted.Render(fmt.Sprintf("%-9s", dash.RelTime(fmt.Sprint(mm["created_at"]))))
		tail := stKey.Render(dash.StudioURL(mm["project_id"]))
		if dash.Kind(status) == "failed" {
			if e, _ := mm["error"].(string); e != "" {
				if len(e) > 58 {
					e = e[:57] + "…"
				}
				tail = stErr.Render(strings.ReplaceAll(e, "\n", " "))
			}
		}
		b.WriteString("  " + badge + "  " + kind + " " + when + " " + tail + "\n")
	}
	b.WriteString("\n" + stMuted.Render(fmt.Sprintf("  %v total", out["total"])))
	return b.String()
}

// ---- rendering ------------------------------------------------------------

func render(title string, data any) string {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Sprint(data)
	}
	return stMuted.Render(title+":") + "\n" + string(b)
}

func isTerminal(s string) bool { return videos.IsTerminal(map[string]any{"status": s}) }

// bestLink turns a status payload into a human link: the rendered MP4 if there
// is one, else the studio project page (never the raw video.json).
func bestLink(m map[string]any) string {
	if u, _ := m["video_url"].(string); strings.HasSuffix(u, ".mp4") {
		return u
	}
	switch v := m["project_id"].(type) {
	case float64:
		return "https://studio.zebracat.ai/storyboard/" + strconv.FormatInt(int64(v), 10)
	case string:
		if v != "" {
			return "https://studio.zebracat.ai/storyboard/" + v
		}
	}
	if u, _ := m["video_url"].(string); u != "" {
		return u
	}
	return "(no link)"
}

// fmtCredits prints a credit count with thousands separators (and ≤1 decimal).
func fmtCredits(f float64) string {
	if f == float64(int64(f)) {
		return groupThousands(strconv.FormatInt(int64(f), 10))
	}
	return strconv.FormatFloat(f, 'f', 1, 64)
}

func groupThousands(s string) string {
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		return "-" + s
	}
	return s
}
