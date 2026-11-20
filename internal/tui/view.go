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
	"github.com/zebracatai/zebracat-cli/internal/logo"
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

// say prints a block above the input and keeps it in the transcript.
func (m *model) say(s string) tea.Cmd { return m.sayR(func() string { return s }) }

// sayR prints a block that renders itself for the current width, so a resize
// can draw it again properly (the welcome logo, a card, a wrapped message).
func (m *model) sayR(render func() string) tea.Cmd {
	f := m.fit(render())
	m.blocks = append(m.blocks, block{render: render})
	m.printed += lineCount(f)
	return tea.Println(f)
}

// sayWelcome prints the welcome (logo sized to the window) as the first block.
func (m *model) sayWelcome() tea.Cmd { return m.sayR(m.welcomeView) }

func lineCount(s string) int { return strings.Count(s, "\n") + 1 }

// redraw follows a resize: clear the screen and print again, at the new size,
// as much of the conversation as fits above the input (the welcome logo picks
// its size afresh). Older lines stay in the terminal's scrollback.
func (m *model) redraw() tea.Cmd {
	room := m.height - lipgloss.Height(m.liveView())
	var parts []string
	used := 0
	for i := len(m.blocks) - 1; i >= 0; i-- {
		f := m.fit(m.blocks[i].render())
		n := lineCount(f)
		if used+n > room && len(parts) > 0 {
			break
		}
		parts = append([]string{f}, parts...)
		used += n
	}
	m.printed = used
	if len(parts) == 0 {
		return tea.ClearScreen
	}
	return tea.Sequence(tea.ClearScreen, tea.Println(strings.Join(parts, "\n")))
}

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
	return m.sayR(func() string { return m.hang(stMuted.Render("› "), stMuted.Render(strings.TrimSpace(text))) })
}

// assistant is Zebracat's turn: a purple dot, then the words.
func (m *model) assistant(text string) tea.Cmd {
	return m.sayR(func() string { return m.hang(stKey.Render("● "), text) })
}

// note is a dimmed tool/progress line under a turn.
func (m *model) note(text string) tea.Cmd {
	return m.sayR(func() string { return m.hang(stProg.Render("  ⎿ "), stProg.Render(text)) })
}

// sayErr is a red line with what happened and one line with what to do.
func (m *model) sayErr(what, fix string) tea.Cmd {
	return m.sayR(func() string {
		s := m.hang(stErr.Render("✗ "), stErr.Render(clean(what)))
		if fix != "" {
			s += "\n" + m.hang(stMuted.Render("  ⎿ "), stMuted.Render(fix))
		}
		return s
	})
}

// ---- welcome --------------------------------------------------------------------

// welcomeView is the launch screen: the real logo, and beside it the name and
// version, who is signed in with their balance, the folder, then two tips (an
// update offer or a PATH warning when there is one). No box: quiet, like the
// rest of the conversation.
func (m *model) welcomeView() string {
	var text []string
	text = append(text, stTitle.Render("Zebracat")+stMuted.Render(" v"+version.Version))
	if m.launch.JustUpdated != "" {
		text = append(text, stOK.Render("Updated from "+m.launch.JustUpdated))
	}
	if m.cl != nil && m.cl.IsAuthenticated() && m.keyRejected {
		text = append(text, stErr.Render("Your API key was rejected · ")+stKey.Render("/login"))
	} else if m.cl != nil && m.cl.IsAuthenticated() {
		if a := m.accountLine(); a != "" {
			text = append(text, stMuted.Render(strings.TrimPrefix(a, " · ")))
		} else {
			text = append(text, stMuted.Render("Signed in"))
		}
	} else {
		text = append(text, stMuted.Render("Not signed in · ")+stKey.Render("/login"))
	}
	if d := cwd(); d != "" {
		if max := m.welcomeTextWidth(); ansi.StringWidth(d) > max && max > 10 {
			r := []rune(d)
			d = "…" + string(r[len(r)-(max-1):]) // keep the end of the path: it names the folder
		}
		text = append(text, stMuted.Render(d))
	}
	text = append(text, "",
		"Describe a video, paste a script or a link.",
		stMuted.Render("/help for commands · ctrl+c twice to quit"))
	if m.offer != "" {
		text = append(text, "", stKey.Render("↑ ")+"Zebracat CLI "+m.offer+" is available"+stMuted.Render(" · press ")+
			stKey.Render("u")+stMuted.Render(" to update"))
	}
	if m.shadow != "" {
		text = append(text, "", stKey.Render("! ")+stMuted.Render("Another zebracat runs first on your PATH: "+m.shadow+
			" · run ")+stKey.Render("zebracat doctor"))
	}

	size := m.logoSize()
	col := m.welcomeTextWidth()
	gap := 4
	var out string
	switch {
	case size > 0 && m.sideBySide(): // the words centred beside the logo
		mark := logo.Sized(size)
		pad := (len(mark) - len(text)) / 2
		if pad < 0 {
			pad = 0
		}
		block := strings.Repeat("\n", pad) + strings.Join(text, "\n")
		words := lipgloss.NewStyle().Width(col).Render(block)
		out = lipgloss.JoinHorizontal(lipgloss.Top, stMark.Render(strings.Join(mark, "\n")), strings.Repeat(" ", gap), words)
	case size > 0: // the logo, then the words under it
		out = stMark.Render(strings.Join(logo.Sized(size), "\n")) + "\n\n" +
			lipgloss.NewStyle().Width(m.cols()-1).Render(strings.Join(text, "\n"))
	default: // tiny window: the 1-row mark before the name
		text[0] = smallMark() + "  " + text[0]
		out = lipgloss.NewStyle().Width(m.cols() - 1).Render(strings.Join(text, "\n"))
	}
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(" "+l, " ")
	}
	return m.fit(strings.Join(lines, "\n"))
}

// logoSize picks the logo for the window: 2 (17×10, the cap) on a normal or
// big terminal, 1 (9×5) on a small one, 0 (the 1-row mark) on a tiny one. It needs the columns for the logo and the rows for it plus the input.
func (m *model) logoSize() int {
	w, h := m.cols(), m.height
	if h <= 0 {
		h = 40 // unknown (tests): judge by width
	}
	switch {
	case w >= 70 && h >= 24: // the largest: 17×10, even full screen
		return 2
	case w >= 40 && h >= 14:
		return 1
	}
	return 0
}

// logoWidth is the current logo's width in cells (0 for the 1-row mark).
func (m *model) logoWidth() int {
	if s := m.logoSize(); s > 0 {
		c, _ := logo.Dimensions(s)
		return c
	}
	return 0
}

// sideBySide: the words fit beside the logo.
func (m *model) sideBySide() bool {
	return m.logoWidth() > 0 && m.cols()-1-m.logoWidth()-4 >= 44
}

// welcomeTextWidth is the room for the welcome words (beside or under the logo).
func (m *model) welcomeTextWidth() int {
	if m.sideBySide() {
		return m.cols() - 1 - m.logoWidth() - 4
	}
	return m.cols() - 2
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

// rule is a dim full-width line; the input sits between two of them.
func (m *model) rule() string { return stRule.Render(strings.Repeat("─", m.inputWidth())) }

// View is the live area: the progress line, the input between two rules, the
// footer. It is padded with blank lines so it always sits on the last rows of
// the terminal, under the conversation; once the conversation fills the
// screen, it scrolls up above the input like any terminal output.
func (m *model) View() string {
	if m.quitting {
		return ""
	}
	live := m.liveView()
	if pad := m.height - m.printed - lipgloss.Height(live); pad > 0 {
		live = strings.Repeat("\n", pad) + live
	}
	return live
}

func (m *model) liveView() string {
	var b strings.Builder
	if menu := m.menuView(); menu != "" { // the `/` menu opens above the input
		b.WriteString(menu + "\n")
	} else if p := m.progressLine(); p != "" {
		b.WriteString(m.fit(p) + "\n\n")
	} else if c := m.choiceView(); c != "" { // Generate / Discard under the plan card
		b.WriteString(m.fit(c) + "\n")
	}
	var field string
	if m.askKey {
		field = stPrompt.Render("› ") + m.key.View()
	} else {
		field = lipgloss.JoinHorizontal(lipgloss.Top, stPrompt.Render("› "), m.in.View())
	}
	b.WriteString(m.rule() + "\n")
	if len(m.images) > 0 { // attached pictures, as chips above the text
		b.WriteString(m.chipLines() + "\n")
	}
	b.WriteString(field + "\n" + m.rule() + "\n")
	b.WriteString(m.footer())
	return b.String()
}

// chipLines lays the picture chips out in rows, never splitting a chip, with a
// hint on how to remove one when it fits.
func (m *model) chipLines() string {
	w := m.inputWidth()
	items := make([]string, 0, len(m.images)+1)
	for i, im := range m.images {
		items = append(items, stKey.Render(im.Chip(i+1)))
	}
	items = append(items, stMuted.Render("backspace removes the last"))
	var lines []string
	line := " "
	for _, it := range items {
		if ansi.StringWidth(line)+1+ansi.StringWidth(it) > w && line != " " {
			lines = append(lines, line)
			line = " "
		}
		line += " " + it
	}
	lines = append(lines, line)
	return strings.Join(lines, "\n")
}

// menuView is the `/` completion list: two aligned columns, as many rows as fit
// (menuRows at most), scrolled to keep the selection in view, with "… N more"
// when some are hidden ("" when closed).
func (m *model) menuView() string {
	if len(m.matches) == 0 {
		return ""
	}
	nameW := 0
	for _, c := range m.matches {
		if n := ansi.StringWidth(c.name); n > nameW {
			nameW = n
		}
	}
	rows := m.menuFit()
	first := 0
	if m.sel >= rows {
		first = m.sel - rows + 1
	}
	last := first + rows
	if last > len(m.matches) {
		last = len(m.matches)
	}
	var lines []string
	for i := first; i < last; i++ {
		c := m.matches[i]
		name := fmt.Sprintf("%-*s", nameW+3, c.name)
		var line string
		if i == m.sel {
			line = stSel.Bold(true).Render("› "+name) + c.desc
		} else {
			line = "  " + stKey.Render(name) + stMuted.Render(c.desc)
		}
		lines = append(lines, ansi.Truncate(line, m.inputWidth(), "…"))
	}
	if hidden := len(m.matches) - (last - first); hidden > 0 {
		lines = append(lines, stMuted.Render(fmt.Sprintf("  … %d more (↑↓)", hidden)))
	}
	return strings.Join(lines, "\n")
}

// menuRows is how many `/` commands show at most.
const menuRows = 8

// menuFit is how many `/` commands fit: the terminal's rows less the input,
// its rules, the footer and the "… more" line, so the selection never sits
// above the top of the screen (menuRows when the height is unknown).
func (m *model) menuFit() int {
	rows := menuRows
	if m.height > 0 {
		room := m.height - (m.in.Height() + 4) - 1 // rules 2 + footer 2; the "… more" line
		if len(m.matches) <= room && room < rows {
			room++ // everything fits: no "… more" line needed
		}
		if room < rows {
			rows = room
		}
	}
	if rows < 1 {
		rows = 1
	}
	return rows
}

// inputWidth is the input's full width: the whole terminal, like the rules.
func (m *model) inputWidth() int { return m.cols() }

// footer is two dim lines under the input, like a status bar: what you can do
// now, then your plan, balance, account and the version. On a narrow terminal
// the least important items go first; it never wraps.
func (m *model) footer() string {
	w := m.inputWidth()
	if !m.quitAt.IsZero() {
		return stMuted.Render("  Press Ctrl-C again to exit")
	}
	var hints []string
	switch m.stage {
	case stageAsking:
		hints = []string{"type your answer", "a number picks an option"}
	case stageReady:
		hints = []string{"↑↓ enter choose", "or type a change"}
		if m.confirming {
			hints = []string{"y generate", "any other key keeps the plan"}
		}
	case stageDone:
		hints = []string{"ctrl+o open the video", "type a new idea"}
	case stagePlanning, stageMaking:
		hints = []string{"esc to interrupt", "/ for commands"}
	case stageEditing:
		hints = []string{"changing the plan…"}
	default:
		hints = []string{"/ for commands", "alt+enter new line", "↑ history"}
	}
	if m.offer != "" && m.stage == stageIdle {
		hints = append([]string{"u update to " + m.offer}, hints...)
	}
	return stMuted.Render(fitItems(hints, w)) + "\n" + m.colourStatus(fitItems(m.statusItems(), w))
}

// colourStatus colours the status line's items: the plan in the accent, the
// balances green, the rest dim. The items were fitted as plain text first.
func (m *model) colourStatus(line string) string {
	items := strings.Split(strings.TrimPrefix(line, "  "), " · ")
	for i, it := range items {
		switch {
		case m.acct != nil && it == m.acct.plan:
			items[i] = stLabel.Render(it)
		case strings.HasSuffix(it, " API balance") || strings.HasSuffix(it, " credits"):
			items[i] = stGood.Render(it)
		default:
			items[i] = stMuted.Render(it)
		}
	}
	return "  " + strings.Join(items, stMuted.Render(" · "))
}

// statusItems are the status line's items, most important first: plan, the
// balance you spend, the other balance, the account, the version.
func (m *model) statusItems() []string {
	ver := "v" + version.Version
	if m.cl == nil || !m.cl.IsAuthenticated() {
		return []string{"not signed in · /login", ver}
	}
	if m.keyRejected {
		return []string{"API key rejected · /login", ver}
	}
	if m.acct == nil {
		return []string{"signed in", ver}
	}
	var items []string
	if m.acct.plan != "" {
		items = append(items, m.acct.plan)
	}
	credits := ""
	if m.acct.credits != "" {
		credits = m.acct.credits + " credits"
	}
	dollars := ""
	if m.acct.apiDollars != "" {
		dollars = "$" + m.acct.apiDollars + " API balance"
	}
	if m.cl.AuthMode() == "api_key" { // an API key spends dollars
		items = append(items, dollars, credits)
	} else {
		items = append(items, credits, dollars)
	}
	items = append(items, m.acct.email, ver)
	var out []string
	for _, it := range items {
		if it != "" {
			out = append(out, it)
		}
	}
	return out
}

// fitItems joins items with " · " after two spaces, dropping items from the end
// until the line fits width (the first item is cut with … as a last resort).
func fitItems(items []string, width int) string {
	for n := len(items); n > 0; n-- {
		line := "  " + strings.Join(items[:n], " · ")
		if ansi.StringWidth(line) <= width {
			return line
		}
	}
	if len(items) == 0 {
		return ""
	}
	return ansi.Truncate("  "+items[0], width, "…")
}

// syncPlaceholder makes the input hint match the conversation.
func (m *model) syncPlaceholder() {
	switch {
	case m.cl == nil || !m.cl.IsAuthenticated():
		m.in.Placeholder = "Type /login to get started"
	case m.stage == stageReady:
		m.in.Placeholder = "Choose above, or type a change"
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
