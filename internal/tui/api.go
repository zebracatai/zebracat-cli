// Background API calls. Each returns a tea.Cmd that runs off the UI loop and
// reports back with one of the messages declared in tui.go.
package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zebracatai/zebracat-cli/internal/update"
	"github.com/zebracatai/zebracat-cli/internal/version"
)

// ---- commands -------------------------------------------------------------
func (m *model) runAPI(echo tea.Cmd, method, path string, body any) (tea.Model, tea.Cmd) {
	m.busy = true
	return m, tea.Batch(echo, m.apiCmd(method, path, body))
}

func (m *model) apiCmd(method, path string, body any) tea.Cmd {
	cl := m.cl
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var out any
		_, err := cl.Do(ctx, method, path, body, &out)
		return apiMsg{kind: "generic", out: out, err: err}
	}
}

func (m *model) createCmd(body map[string]any) tea.Cmd {
	cl := m.cl
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		var out map[string]any
		_, err := cl.Do(ctx, "POST", "/api/v1/public/video/agentic", body, &out)
		return apiMsg{kind: "create", out: out, err: err}
	}
}

func (m *model) pollCmd(taskID string) tea.Cmd {
	return func() tea.Msg { return m.poll(taskID) }
}

func (m *model) poll(taskID string) tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var out map[string]any
	_, err := m.cl.Do(ctx, "GET", "/api/v1/public/video/status?task_id="+taskID, nil, &out)
	return pollMsg{taskID: taskID, out: out, err: err}
}

// accountCmd fetches GET /account in the background to populate the header.
func (m *model) accountCmd() tea.Cmd {
	cl := m.cl
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var out map[string]any
		if _, err := cl.Do(ctx, "GET", "/api/v1/public/account", nil, &out); err != nil {
			return acctMsg{a: nil}
		}
		a := &account{}
		a.email, _ = out["email"].(string)
		a.plan, _ = out["plan"].(string)
		if ac, ok := out["account_credit"].(map[string]any); ok {
			if r, ok := ac["remaining"].(float64); ok {
				a.credits = fmtCredits(r)
			}
		}
		a.apiDollars, _ = out["api_dollar_balance"].(string)
		return acctMsg{a: a}
	}
}

// projectsCmd fetches the project list for the dashboard view.
func (m *model) projectsCmd() tea.Cmd {
	cl := m.cl
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var out map[string]any
		_, err := cl.Do(ctx, "GET", "/api/v1/public/projects?limit=15", nil, &out)
		return projectsMsg{out: out, err: err}
	}
}

// updateCheckCmd asks GitHub (cached, ≤1 network call/day) whether a newer
// version is out, for the startup notice.
func (m *model) updateCheckCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return updMsg{latest: update.LatestCached(ctx)}
	}
}

// runUpdate self-updates the binary from inside the shell.
func (m *model) runUpdate(echo tea.Cmd) (tea.Model, tea.Cmd) {
	m.busy = true
	return m, tea.Batch(echo, tea.Println(stMuted.Render("Checking for updates…")), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		latest, err := update.Latest(ctx)
		if err != nil {
			return updDoneMsg{err: err}
		}
		if !update.Newer("v"+version.Version, latest) {
			return updDoneMsg{} // already on the latest
		}
		if err := update.Apply(ctx, latest); err != nil {
			return updDoneMsg{err: err}
		}
		return updDoneMsg{tag: latest}
	})
}
