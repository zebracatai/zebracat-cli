// Background API calls. Each returns a tea.Cmd that runs off the UI loop and
// reports back with one of the messages declared in tui.go / convo.go.
package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zebracatai/zebracat-cli/internal/attach"
	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/update"
	"github.com/zebracatai/zebracat-cli/internal/version"
	"github.com/zebracatai/zebracat-cli/internal/videos"
)

// ---- commands -------------------------------------------------------------
func (m *model) runAPI(echo tea.Cmd, method, path string, body any) (tea.Model, tea.Cmd) {
	m.busy = true
	return m, tea.Sequence(echo, m.apiCmd(method, path, body))
}

func (m *model) apiCmd(method, path string, body any) tea.Cmd {
	cl := m.cl
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var out any
		_, err := cl.Do(ctx, method, path, body, &out)
		return apiMsg{out: out, err: err}
	}
}

// createCmd posts a message as a Director v2 plan (auto_generate=false): the
// person sees the plan and its quote before anything is spent.
func (m *model) createCmd(j *job, body map[string]any, imgs []attach.Image) tea.Cmd {
	cl := m.cl
	key := newKey()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if len(imgs) > 0 { // the pictures first, the way the studio uploads them
			var urls []string
			for _, im := range imgs {
				u, err := attach.Upload(ctx, cl, im)
				if err != nil {
					return createdMsg{j: j, err: err}
				}
				urls = append(urls, u)
			}
			body["uploads"] = attach.Uploads(urls, imgs)
		}
		var out map[string]any
		_, err := cl.DoWith(ctx, "POST", videos.Base+"/videos", body, map[string]string{"Idempotency-Key": key}, &out)
		return createdMsg{j: j, out: out, err: err}
	}
}

// generateCmd presses Generate on a plan (only ever after the person's y).
func (m *model) generateCmd(j *job, taskID string, shouldRender bool) tea.Cmd {
	cl := m.cl
	key := newKey()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		var out map[string]any
		_, err := cl.DoWith(ctx, "POST", videos.Path(taskID, "generate"), map[string]any{"should_render": shouldRender},
			map[string]string{"Idempotency-Key": key}, &out)
		return generatedMsg{j: j, out: out, err: err}
	}
}

// planCmd reads a ready plan (GET /plan) and what Generate will charge (GET /quote).
func (m *model) planCmd(j *job, task map[string]any, reply string) tea.Cmd {
	cl := m.cl
	return func() tea.Msg {
		id, _ := task["task_id"].(string)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var plan map[string]any
		if _, err := cl.Do(ctx, "GET", videos.Path(id, "plan"), nil, &plan); err != nil {
			plan = nil // the card falls back to the task's plan_summary
		}
		return planMsg{j: j, task: task, plan: plan, quote: readQuote(ctx, cl, id, task), reply: reply}
	}
}

// readQuote is GET /quote, else the quote the task view carries.
func readQuote(ctx context.Context, cl *client.Client, id string, task map[string]any) map[string]any {
	var q map[string]any
	if _, err := cl.Do(ctx, "GET", videos.Path(id, "quote"), nil, &q); err != nil {
		q, _ = task["quote"].(map[string]any)
	}
	return q
}

// chatCmd sends a plain-words change through the plan chat (the studio's edits
// path), then re-reads the quote.
func (m *model) chatCmd(j *job, taskID, message string) tea.Cmd {
	cl := m.cl
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		var out map[string]any
		if _, err := cl.Do(ctx, "POST", videos.Path(taskID, "plan", "chat"), map[string]any{"message": message}, &out); err != nil {
			return planMsg{j: j, err: err}
		}
		reply, _ := out["reply"].(string)
		var plan map[string]any
		if _, ok := out["screenplay"].(map[string]any); ok {
			plan = out
		}
		return planMsg{j: j, plan: plan, quote: readQuote(ctx, cl, taskID, nil), reply: reply}
	}
}

// answerCmd answers the question a plan stopped on.
func (m *model) answerCmd(j *job, taskID, questionID, answer string) tea.Cmd {
	cl := m.cl
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var out map[string]any
		_, err := cl.Do(ctx, "POST", videos.Path(taskID, "answer"), map[string]any{"question_id": questionID, "answer": answer}, &out)
		return answeredMsg{j: j, err: err}
	}
}

// pollCmd polls for the current job; the answer carries that job, so a poll
// left running for a job the person abandoned is ignored.
func (m *model) pollCmd(taskID string, untilDone bool) tea.Cmd {
	j := m.job
	return func() tea.Msg {
		pm := m.poll(taskID, untilDone).(pollMsg)
		pm.j = j
		return pm
	}
}

func (m *model) poll(taskID string, untilDone bool) tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := videos.Fetch(ctx, m.cl, taskID)
	return pollMsg{taskID: taskID, out: out, err: err, untilDone: untilDone}
}

// fetchAccount reads GET /account for the welcome box and footer (nil on error).
func fetchAccount(ctx context.Context, cl *client.Client) *account {
	var out map[string]any
	if _, err := cl.Do(ctx, "GET", "/api/v1/public/account", nil, &out); err != nil {
		return nil
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
	return a
}

// accountCmd fetches GET /account in the background.
func (m *model) accountCmd() tea.Cmd {
	cl := m.cl
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return acctMsg{a: fetchAccount(ctx, cl)}
	}
}

// refreshAccount re-reads the account quietly (the footer's balance after a spend).
func (m *model) refreshAccount() tea.Cmd {
	cl := m.cl
	if cl == nil || !cl.IsAuthenticated() {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return acctMsg{a: fetchAccount(ctx, cl), quiet: true}
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

// applyUpdate installs a release over this binary (a test swaps it).
var applyUpdate = update.Apply

// latestRelease is the newest release tag (a test swaps it).
var latestRelease = update.Latest

// runUpdate installs the newest release; on success the conversation ends and
// the caller relaunches the new binary.
func (m *model) runUpdate(echo tea.Cmd) (tea.Model, tea.Cmd) {
	if m.stage == stageMaking || m.stage == stagePlanning || m.stage == stageEditing {
		return m, tea.Sequence(echo, m.note("Finish this step first, then /update."))
	}
	m.busy = true
	return m, tea.Sequence(echo, m.note("Checking for updates…"), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		latest, err := latestRelease(ctx)
		if err != nil {
			return updDoneMsg{err: err}
		}
		if !update.Newer("v"+version.Version, latest) {
			return updDoneMsg{} // already on the latest
		}
		path, err := applyUpdate(ctx, latest)
		if err != nil {
			return updDoneMsg{err: err}
		}
		return updDoneMsg{tag: latest, path: path}
	})
}
