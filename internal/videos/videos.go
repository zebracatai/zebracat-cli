// Package videos is the Director v2 video lifecycle as the CLI and the TUI read
// it: fetch a task (falling back to the v1 status endpoint for v1 task ids),
// decide when a wait stops, and format a plan and a quote for people.
//
// A v2 task moves through phases (planning → needs_input → plan_ready →
// generating → rendering → completed | failed | cancelled). A wait stops at
// plan_ready (unless the task generates by itself), at needs_input, or at a
// terminal state.
package videos

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
)

// Base is the public API prefix.
const Base = "/api/v1/public"

// Path is the v2 resource path for a task, plus an optional sub-path.
func Path(taskID string, sub ...string) string {
	p := Base + "/videos/" + url.PathEscape(taskID)
	for _, s := range sub {
		p += "/" + s
	}
	return p
}

// Clock is the time source for waits; tests inject a fake one.
type Clock interface {
	Now() time.Time
	Sleep(time.Duration)
}

type realClock struct{}

func (realClock) Now() time.Time        { return time.Now() }
func (realClock) Sleep(d time.Duration) { time.Sleep(d) }

// RealClock is the wall clock.
var RealClock Clock = realClock{}

// Fetch reads a task: GET /videos/{id} for v2 tasks, and GET /video/status for
// v1 task ids (the v2 route answers 404 for them, as does a server without it).
func Fetch(ctx context.Context, c *client.Client, taskID string) (map[string]any, error) {
	var out map[string]any
	_, err := c.Do(ctx, "GET", Path(taskID), nil, &out)
	if err == nil && Phase(out) != "" {
		return out, nil
	}
	var ce *clierr.Error
	if err != nil && !(errors.As(err, &ce) && ce.Status == 404) {
		return nil, err
	}
	out = nil
	if _, err := c.Do(ctx, "GET", Base+"/video/status?task_id="+url.QueryEscape(taskID), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Phase is the task's v2 phase, or "" for a v1 task.
func Phase(m map[string]any) string {
	p, _ := m["phase"].(string)
	return p
}

// Status is the task's status.
func Status(m map[string]any) string {
	s, _ := m["status"].(string)
	return s
}

var terminalStatuses = map[string]bool{
	"completed": true, "failed": true, "render_failed": true,
	"avatar_render_failed": true, "cancelled": true,
}

var terminalPhases = map[string]bool{"completed": true, "failed": true, "cancelled": true}

// IsTerminal reports whether nothing more will happen to the task.
func IsTerminal(m map[string]any) bool {
	if p := Phase(m); p != "" {
		return terminalPhases[p]
	}
	return terminalStatuses[Status(m)]
}

// StopsWait reports whether a wait ends at this state. untilDone keeps going
// past plan_ready (a one-call video generates by itself after its plan).
func StopsWait(m map[string]any, untilDone bool) bool {
	switch Phase(m) {
	case "needs_input":
		return true
	case "plan_ready":
		if auto, ok := m["auto_generate"].(bool); ok && auto {
			return false
		}
		return !untilDone
	}
	return IsTerminal(m)
}

// WaitOptions drive Wait.
type WaitOptions struct {
	Timeout   time.Duration
	Interval  time.Duration // between polls (default 5s)
	UntilDone bool          // don't stop at plan_ready
	Clock     Clock         // default RealClock
	OnPoll    func(map[string]any)
}

// Wait polls the task until StopsWait, honouring Retry-After on a 429.
// Past the timeout it returns the last state and a Timeout error (exit 4).
func Wait(c *client.Client, taskID string, o WaitOptions) (map[string]any, error) {
	clk := o.Clock
	if clk == nil {
		clk = RealClock
	}
	interval := o.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := clk.Now().Add(o.Timeout)
	var last map[string]any
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		out, err := Fetch(ctx, c, taskID)
		cancel()
		sleep := interval
		if err != nil {
			var ce *clierr.Error
			if !(errors.As(err, &ce) && ce.Status == 429) {
				return nil, err
			}
			if ce.RetryAfter > 0 {
				sleep = ce.RetryAfter
			}
		} else {
			last = out
			if o.OnPoll != nil {
				o.OnPoll(out)
			}
			if StopsWait(out, o.UntilDone) {
				return out, nil
			}
		}
		if !clk.Now().Add(sleep).Before(deadline) {
			state := "unknown"
			if last != nil {
				state = PhaseOrStatus(last)
			}
			return last, clierr.Timeout("timed out after %s; last state: %s (still running — poll later with `zebracat video status %s`)", o.Timeout, state, taskID)
		}
		clk.Sleep(sleep)
	}
}

// PhaseOrStatus is the phase for a v2 task, else the status.
func PhaseOrStatus(m map[string]any) string {
	if p := Phase(m); p != "" {
		return p
	}
	return Status(m)
}

// EditorURL is the studio link to open or edit the video.
func EditorURL(m map[string]any) string {
	if s, _ := m["editor_url"].(string); s != "" {
		return s
	}
	if l, ok := m["links"].(map[string]any); ok {
		if s, _ := l["editor"].(string); s != "" {
			return s
		}
	}
	switch v := m["project_id"].(type) {
	case float64:
		return fmt.Sprintf("https://studio.zebracat.ai/storyboard/%d", int64(v))
	case string:
		if v != "" {
			return "https://studio.zebracat.ai/storyboard/" + v
		}
	}
	return ""
}

// QuoteLine is one sentence for a quote envelope (PLAN §1.5):
// "96 credits ($4.80) from the API balance — 4 AI-video scenes …".
func QuoteLine(q map[string]any) string {
	if q == nil {
		return ""
	}
	s := fmt.Sprintf("%s credits", num(q["credits"]))
	if usd, _ := q["usd"].(string); usd != "" {
		s += " ($" + usd + ")"
	}
	switch q["pool"] {
	case "api_dollar":
		s += " from your API balance"
	case "account":
		s += " from your plan credits"
	}
	if b, _ := q["basis"].(string); b == "estimate" {
		s += " (estimate)"
	}
	if suf, ok := q["sufficient"].(bool); ok && !suf {
		s += " — not enough funds"
	}
	if why, _ := q["why"].(string); why != "" {
		s += " — " + why
	}
	return s
}

func num(v any) string {
	switch n := v.(type) {
	case float64:
		if n == float64(int64(n)) {
			return fmt.Sprintf("%d", int64(n))
		}
		return fmt.Sprintf("%.2f", n)
	case nil:
		return "?"
	}
	return fmt.Sprint(v)
}

// SummaryLines is the short plan summary: the task's plan_summary (an object of
// title, seconds, beats, media_type, audio_mode, language), a plan view's
// summary_lines, or a line built from its screenplay.
func SummaryLines(m map[string]any) []string {
	for _, k := range []string{"plan_summary", "summary_lines"} {
		switch v := m[k].(type) {
		case map[string]any:
			return []string{summaryObject(v)}
		case string:
			if v != "" {
				return []string{v}
			}
		case []any:
			var out []string
			for _, x := range v {
				if s, ok := x.(string); ok && s != "" {
					out = append(out, s)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	sp, _ := m["screenplay"].(map[string]any)
	if sp == nil {
		return nil
	}
	beats, _ := sp["beats"].([]any)
	var secs float64
	for _, b := range beats {
		if bm, ok := b.(map[string]any); ok {
			s, _ := bm["seconds"].(float64)
			secs += s
		}
	}
	parts := []string{}
	if t, _ := sp["title"].(string); t != "" {
		parts = append(parts, t)
	}
	parts = append(parts, fmt.Sprintf("%d scenes, %s s", len(beats), num(secs)))
	return []string{strings.Join(parts, " — ")}
}

// summaryObject renders the task view's plan_summary object as one line:
// "Easier mornings — 4 scenes, 20 s · ai_video · narration · english".
func summaryObject(v map[string]any) string {
	line := fmt.Sprintf("%s scenes, %s s", num(v["beats"]), num(v["seconds"]))
	if t, _ := v["title"].(string); t != "" {
		line = t + " — " + line
	}
	for _, k := range []string{"media_type", "audio_mode", "language"} {
		if s, _ := v[k].(string); s != "" {
			line += " · " + s
		}
	}
	return line
}

// Question is a needs_input question's wording and its offered answers.
// The run sends the wording as "question" (older shapes: "text"); an option
// is a string or an object with a label.
func Question(m map[string]any) (id, text string, options []string) {
	q, _ := m["question"].(map[string]any)
	if q == nil {
		return "", "", nil
	}
	id = fmt.Sprint(q["id"])
	for _, k := range []string{"question", "text"} {
		if s, _ := q[k].(string); s != "" {
			text = s
			break
		}
	}
	opts, _ := q["options"].([]any)
	for _, o := range opts {
		switch v := o.(type) {
		case string:
			options = append(options, v)
		case map[string]any:
			for _, k := range []string{"label", "value", "id"} {
				if s, _ := v[k].(string); s != "" {
					options = append(options, s)
					break
				}
			}
		}
	}
	return id, text, options
}

// Beats returns "n. (Ns) words" lines for a plan view's screenplay.
func Beats(plan map[string]any) []string {
	sp, _ := plan["screenplay"].(map[string]any)
	beats, _ := sp["beats"].([]any)
	var out []string
	for i, b := range beats {
		bm, _ := b.(map[string]any)
		if bm == nil {
			continue
		}
		n := num(bm["n"])
		if bm["n"] == nil {
			n = fmt.Sprint(i + 1)
		}
		words, _ := bm["narration"].(string)
		if words == "" {
			var said []string
			lines, _ := bm["lines"].([]any)
			for _, l := range lines {
				if lm, ok := l.(map[string]any); ok {
					said = append(said, fmt.Sprintf("%v: %v", lm["speaker"], lm["text"]))
				}
			}
			words = strings.Join(said, " / ")
		}
		if words == "" {
			words = "(no words)"
		}
		out = append(out, fmt.Sprintf("%s. (%ss) %s", n, num(bm["seconds"]), words))
	}
	return out
}
