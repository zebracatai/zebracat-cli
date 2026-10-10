// Rendering: the splash, auth banner, input box + footer, the projects
// dashboard, and small formatters for API payloads.
package tui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/zebracatai/zebracat-cli/internal/dash"
)

func splash() string {
	body := stTitle.Render(logo) + "\n\n" +
		stMuted.Render("🦓  AI video generation, right in your terminal.") + "\n\n" +
		stMuted.Render("Type ") + stKey.Render("/help") + stMuted.Render(" for commands") +
		stMuted.Render("   ·   ") + stKey.Render("/login") + stMuted.Render(" to start")
	return stBox.Render(body) + "\n"
}

// authLine is the one-shot banner under the splash: signed-in mode or a /login nudge.
func (m *model) authLine() string {
	switch m.cl.AuthMode() {
	case "oauth":
		return stOK.Render("● ") + stMuted.Render("Signed in — spending your plan credits.")
	case "api_key":
		return stOK.Render("● ") + stMuted.Render("Authenticated with an API key — pay-as-you-go.")
	default:
		return stErr.Render("○ ") + stMuted.Render("You're not signed in. Type ") +
			stKey.Render("/login") + stMuted.Render(" to add your Zebracat API key.")
	}
}

func (m *model) View() string {
	if m.wiz != nil {
		return m.wizView()
	}
	var b strings.Builder
	// autocomplete hint line
	if len(m.matches) > 0 {
		var parts []string
		for _, mtc := range m.matches {
			parts = append(parts, stKey.Render(mtc))
		}
		b.WriteString(stMuted.Render("  ↹ ") + strings.Join(parts, stMuted.Render(" · ")) + "\n")
	}
	w := m.width
	if w < 24 {
		w = 60
	}
	b.WriteString(stInput.Width(w-2).Render(m.in.View()) + "\n")
	foot := m.statusLine()
	if m.busy {
		foot += stMuted.Render("   ") + m.sp.View() + stMuted.Render(" working…")
	}
	b.WriteString(foot)
	return b.String()
}

// statusLine is the persistent footer: who you are + balance.
func (m *model) statusLine() string {
	switch m.cl.AuthMode() {
	case "oauth":
		s := "signed in"
		if m.acct != nil {
			if m.acct.email != "" {
				s = m.acct.email
			}
			if m.acct.plan != "" {
				s += " · " + m.acct.plan
			}
			if m.acct.credits != "" {
				s += " · " + m.acct.credits + " credits"
			}
		}
		return "  " + stOK.Render("●") + " " + stMuted.Render(s)
	case "api_key":
		s := "API key · pay-as-you-go"
		if m.acct != nil && m.acct.apiDollars != "" {
			s = "API key · $" + m.acct.apiDollars + " balance"
		}
		return "  " + stOK.Render("●") + " " + stMuted.Render(s)
	default:
		return "  " + stErr.Render("○") + " " + stMuted.Render("not signed in — type ") + stKey.Render("/login")
	}
}

// greeting is the welcome line printed once /account resolves.
func (m *model) greeting() string {
	who := "there"
	if m.acct != nil && m.acct.email != "" {
		who = m.acct.email
	}
	line := stOK.Render("👋 Welcome back, ") + stKey.Render(who)
	if m.acct == nil {
		return line
	}
	if m.cl.AuthMode() == "api_key" {
		if m.acct.apiDollars != "" {
			line += stMuted.Render(" · $" + m.acct.apiDollars + " API balance")
		}
		return line
	}
	if m.acct.plan != "" {
		line += stMuted.Render(" · " + m.acct.plan)
	}
	if m.acct.credits != "" {
		line += stMuted.Render(" · " + m.acct.credits + " credits left")
	}
	return line
}

// syncPlaceholder makes the input hint match the auth state.
func (m *model) syncPlaceholder() {
	if m.cl != nil && m.cl.IsAuthenticated() {
		m.in.Placeholder = "Describe a video, or type /help …"
	} else {
		m.in.Placeholder = "Type /login to get started …"
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
		return b.String() + stMuted.Render("  Nothing here yet — type ") + stKey.Render("/video") + stMuted.Render(" to make one.")
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
func (a apiMsg) title() string { return render("Result", a.out) }

func render(title string, data any) string {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Sprint(data)
	}
	return stMuted.Render(title+":") + "\n" + string(b)
}

func isTerminal(s string) bool {
	switch s {
	case "completed", "failed", "render_failed", "avatar_render_failed", "cancelled":
		return true
	}
	return false
}

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
