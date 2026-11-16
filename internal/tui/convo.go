// The conversation: what replaced the old "pick the type, pick the length…"
// wizard. Like the studio's v2 /chat, the person types an idea, pastes a link
// (or loads a script); Director v2 writes the plan (POST /videos, plan only);
// the CLI shows it as a card with the quote; plain words change it (the plan
// chat, POST /videos/{id}/plan/chat); Generate happens only after Ctrl+G or
// /generate AND a y on the "spend N credits?" line.
package tui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/zebracatai/zebracat-cli/internal/brief"
	"github.com/zebracatai/zebracat-cli/internal/videos"
)

// stage is where the conversation is.
type stage int

const (
	stageIdle     stage = iota // waiting for an idea, a script or a link
	stagePlanning              // the plan is being written (free)
	stageAsking                // the plan asked a question: the next message answers it
	stageReady                 // the plan is shown: words change it, Ctrl+G generates
	stageEditing               // a plan-chat change is being applied (free)
	stageConfirm               // "spend N credits?" y/n
	stageMaking                // generating / rendering (paid, running on our servers)
	stageDone                  // the video is ready
)

// job is the one video the conversation is about.
type job struct {
	taskID       string
	task         map[string]any // the last task view
	plan         map[string]any // the last plan view (screenplay, summary_lines)
	quote        map[string]any
	shouldRender bool
	started      time.Time // when the current wait (planning, editing, making) began
	phase        string
	questionID   string
	options      []string
	videoURL     string
	editorURL    string
}

// ---- messages ---------------------------------------------------------------

type createdMsg struct {
	out map[string]any
	err error
}
type planMsg struct { // a plan is ready (or changed): the task, its plan view, the quote
	task  map[string]any
	plan  map[string]any
	quote map[string]any
	reply string // the plan chat's answer, for a change
	err   error
}
type answeredMsg struct{ err error }
type generatedMsg struct {
	out map[string]any
	err error
}

// openURL opens a link in the browser (a test swaps it).
var openURL = func(u string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", u).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	}
	return exec.Command("xdg-open", u).Start()
}

// ---- starting -----------------------------------------------------------------

// startPlan sends a message (or a loaded script) to Director v2 as a plan only.
func (m *model) startPlan(text string, kind brief.Kind) (tea.Model, tea.Cmd) {
	body := brief.Body(text, kind, true)
	note := "Planning your video…"
	if link, _ := body["source_video"].(string); link != "" {
		note = "Reading that video and planning…"
	} else if body["input_kind"] == "url" {
		note = "Reading that page and planning…"
	}
	m.job = &job{shouldRender: true, started: m.now(), phase: "planning"}
	m.stage = stagePlanning
	return m, tea.Batch(tea.Println(stMuted.Render("  "+note+" (free — nothing is charged until you generate)")), m.createCmd(body))
}

// ---- results --------------------------------------------------------------------

func (m *model) onCreated(msg createdMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.stage, m.job = stageIdle, nil
		return m, tea.Println(stErr.Render("✗ ") + clean(msg.err.Error()))
	}
	id, _ := msg.out["task_id"].(string)
	if id == "" {
		m.stage, m.job = stageIdle, nil
		return m, tea.Println(stErr.Render("✗ The server answered without a task id."))
	}
	m.job.taskID = id
	m.job.task = msg.out
	return m, m.pollCmd(id, false)
}

// onPoll moves the conversation on from a task view.
func (m *model) onPoll(msg pollMsg) (tea.Model, tea.Cmd) {
	j := m.job
	if j == nil || j.taskID != msg.taskID {
		return m, nil // a poll of a job the person left
	}
	if msg.err != nil {
		return m, tea.Batch(
			tea.Println(stErr.Render("✗ ")+clean(msg.err.Error())+stMuted.Render(" — retrying…")),
			tea.Tick(5*time.Second, func(time.Time) tea.Msg { return m.poll(msg.taskID, msg.untilDone) }))
	}
	j.task = msg.out
	if p := videos.PhaseOrStatus(msg.out); p != "" {
		j.phase = p
	}
	if u := videos.EditorURL(msg.out); u != "" {
		j.editorURL = u
	}
	if !videos.StopsWait(msg.out, msg.untilDone) {
		return m, tea.Tick(5*time.Second, func(time.Time) tea.Msg { return m.poll(msg.taskID, msg.untilDone) })
	}
	switch videos.PhaseOrStatus(msg.out) {
	case "plan_ready":
		return m, m.planCmd(msg.out, "")
	case "needs_input":
		m.stage = stageAsking
		qid, _, opts := videos.Question(msg.out)
		j.questionID, j.options = qid, opts
		return m, tea.Println(questionCard(msg.out))
	case "completed":
		m.stage = stageDone
		j.videoURL, _ = msg.out["video_url"].(string)
		return m, tea.Println(doneCard(j))
	}
	m.stage = stageIdle
	line := stErr.Render("✗ Video " + videos.PhaseOrStatus(msg.out))
	if e := errText(msg.out); e != "" {
		line += stMuted.Render(" — " + e)
	}
	return m, tea.Println(line)
}

func (m *model) onPlan(msg planMsg) (tea.Model, tea.Cmd) {
	j := m.job
	if j == nil {
		return m, nil
	}
	if msg.err != nil {
		m.stage = stageReady
		return m, tea.Println(stErr.Render("✗ ") + clean(msg.err.Error()) + stMuted.Render(" — the plan is unchanged."))
	}
	if msg.task != nil {
		j.task = msg.task
	}
	if msg.plan != nil {
		j.plan = msg.plan
	}
	j.quote = msg.quote
	m.stage = stageReady
	var out []tea.Cmd
	if msg.reply != "" {
		out = append(out, tea.Println(stTitle.Render("🦓 ")+clean(msg.reply)))
	}
	out = append(out, tea.Println(planCard(j, m.width)))
	return m, tea.Sequence(out...)
}

func (m *model) onGenerated(msg generatedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.stage = stageReady
		return m, tea.Println(stErr.Render("✗ ") + clean(msg.err.Error()) + stMuted.Render(" — nothing was generated."))
	}
	m.stage = stageMaking
	m.job.started = m.now()
	m.job.phase = "generating"
	return m, tea.Batch(
		tea.Println(stOK.Render("✓ Generating. ")+stMuted.Render("A few minutes. Ctrl+C leaves; it keeps going.")),
		m.pollCmd(m.job.taskID, true))
}

// ---- input ----------------------------------------------------------------------

// converse handles a plain (non-slash) message by stage.
func (m *model) converse(text string) (tea.Model, tea.Cmd) {
	switch m.stage {
	case stageAsking:
		answer := text
		if n, err := strconv.Atoi(text); err == nil && n >= 1 && n <= len(m.job.options) {
			answer = m.job.options[n-1]
		}
		m.stage = stagePlanning
		m.job.started = m.now()
		return m, m.answerCmd(m.job.taskID, m.job.questionID, answer)
	case stageReady:
		m.stage = stageEditing
		m.job.started = m.now()
		return m, m.chatCmd(m.job.taskID, text)
	case stagePlanning, stageEditing, stageMaking:
		return m, tea.Println(stHint.Render("  One moment — I'm still working on the last step."))
	}
	return m.startPlan(text, brief.KindIdea)
}

// askGenerate shows the explicit spend question.
func (m *model) askGenerate() (tea.Model, tea.Cmd) {
	if m.stage != stageReady || m.job == nil {
		return m, tea.Println(stHint.Render("  There's no plan to generate yet — type an idea first."))
	}
	if ok, known := m.job.quote["sufficient"].(bool); known && !ok {
		return m, tea.Println(stErr.Render("✗ Your balance doesn't cover this video ") +
			stMuted.Render("("+strings.SplitN(videos.QuoteLine(m.job.quote), " — ", 2)[0]+"). Top up at ") +
			stKey.Render("https://studio.zebracat.ai") + stMuted.Render(", then Ctrl+G again. The plan is kept."))
	}
	m.stage = stageConfirm
	return m, nil
}

// confirmKey answers the spend question: y generates, n / Esc goes back.
func (m *model) confirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch strings.ToLower(msg.String()) {
	case "y":
		m.stage = stageMaking
		m.job.started = m.now()
		return m, tea.Batch(tea.Println(stMuted.Render("  Generate?  ")+stKey.Render("yes")), m.generateCmd(m.job.taskID, m.job.shouldRender))
	case "n", "esc":
		m.stage = stageReady
		return m, tea.Println(stMuted.Render("  Not now — the plan is kept and nothing was charged."))
	case "ctrl+c":
		return m.interrupt()
	}
	return m, nil
}

// interrupt is Ctrl+C. It never cancels anything on the server: a paid job keeps
// going, and leaving while one runs takes a second Ctrl+C.
func (m *model) interrupt() (tea.Model, tea.Cmd) {
	bye := func(line string) (tea.Model, tea.Cmd) {
		if line == "" {
			return m, tea.Sequence(tea.Println(stMuted.Render("Goodbye 🦓")), tea.Quit)
		}
		return m, tea.Sequence(tea.Println(line), tea.Println(stMuted.Render("Goodbye 🦓")), tea.Quit)
	}
	if m.job == nil || m.job.taskID == "" {
		return bye("")
	}
	id := m.job.taskID
	switch m.stage {
	case stageMaking:
		if !m.leaveArmed {
			m.leaveArmed = true
			return m, tea.Println(stWorking.Render("! ") + "It keeps being made on our servers; nothing is cancelled.\n" +
				stMuted.Render("  Ctrl+C again to leave, or keep waiting."))
		}
		return bye(stMuted.Render("  Follow it with ") + stKey.Render("zebracat video status "+id+" --wait"))
	case stageReady, stageConfirm, stageEditing, stageAsking:
		return bye(stMuted.Render("  Your plan is saved. Generate it later with ") + stKey.Render("zebracat video generate "+id))
	case stagePlanning:
		return bye(stMuted.Render("  Planning continues; see it with ") + stKey.Render("zebracat video status "+id+" --wait"))
	}
	return bye("")
}

// openVideo opens the finished video (or its studio page).
func (m *model) openVideo() (tea.Model, tea.Cmd) {
	if m.job == nil {
		return m, tea.Println(stHint.Render("  Nothing to open yet."))
	}
	u := m.job.videoURL
	if u == "" {
		u = m.job.editorURL
	}
	if u == "" {
		return m, tea.Println(stHint.Render("  Nothing to open yet."))
	}
	if err := openURL(u); err != nil {
		return m, tea.Println(stErr.Render("✗ Couldn't open a browser: ") + stKey.Render(u))
	}
	return m, tea.Println(stMuted.Render("  Opened ") + stKey.Render(u))
}

// ---- rendering ------------------------------------------------------------------

var phaseLabels = map[string]string{
	"pending":     "Starting",
	"planning":    "Writing the plan",
	"needs_input": "Waiting for your answer",
	"plan_ready":  "Plan ready",
	"generating":  "Making the scenes",
	"processing":  "Making the scenes",
	"rendering":   "Exporting the MP4",
	"completed":   "Done",
}

// progressLine is the live line under the input while something runs:
// "⠋ Writing the plan · 0:42" (+ " · 37%" when the task reports one).
func (m *model) progressLine() string {
	j := m.job
	if j == nil {
		return ""
	}
	label := phaseLabels[j.phase]
	if label == "" {
		label = j.phase
	}
	if m.stage == stageEditing {
		label = "Changing the plan"
	}
	line := m.sp.View() + " " + stTitle.Render(label) + stMuted.Render(" · "+clock(m.now().Sub(j.started)))
	if p, ok := j.task["progress"].(float64); ok && p > 0 && m.stage == stageMaking {
		if p <= 1 {
			p *= 100
		}
		line += stMuted.Render(fmt.Sprintf(" · %d%%", int(p)))
	}
	return "  " + line
}

func clock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d.Seconds())
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// planCard is the plan as a bordered card: title, the facts line, the scenes,
// the voices, the price, then what the person can do next.
func planCard(j *job, width int) string {
	src := j.plan
	if src == nil {
		src = j.task
	}
	sp, _ := src["screenplay"].(map[string]any)
	title, _ := sp["title"].(string)
	if title == "" {
		if s, ok := j.task["plan_summary"].(map[string]any); ok {
			title, _ = s["title"].(string)
		}
	}
	if title == "" {
		title = "Your plan"
	}
	var b strings.Builder
	b.WriteString(stTitle.Render("🎬 "+clean(title)) + "\n")
	if facts := planFacts(sp, j.task); facts != "" {
		b.WriteString(stMuted.Render(clean(facts)) + "\n")
	}
	if beats := videos.Beats(src); len(beats) > 0 {
		b.WriteString("\n")
		for _, l := range beats {
			b.WriteString(wrapBeat(clean(l), width) + "\n")
		}
	} else {
		for _, l := range videos.SummaryLines(src) {
			b.WriteString(clean(l) + "\n")
		}
	}
	b.WriteString("\n")
	if v := voices(sp); v != "" {
		b.WriteString(stMuted.Render("Voice  ") + clean(v) + "\n")
	}
	if q := videos.QuoteLine(j.quote); q != "" {
		b.WriteString(stMuted.Render("Price  ") + stKey.Render(clean(q)) + "\n")
	}
	if j.editorURL != "" {
		b.WriteString(stMuted.Render("Studio ") + stKey.Render(clean(j.editorURL)) + "\n")
	}
	w := width - 2
	if w < 40 || w > 100 {
		w = 80
	}
	card := stBox.Width(w).Render(strings.TrimRight(b.String(), "\n"))
	return card + "\n" + stHint.Render("  Tell me what to change in plain words · Ctrl+G or /generate to make it · /new to start over")
}

// planFacts is "20 s · vertical · ai_video · narration · english".
func planFacts(sp, task map[string]any) string {
	var parts []string
	secs := 0.0
	beats, _ := sp["beats"].([]any)
	for _, b := range beats {
		if bm, ok := b.(map[string]any); ok {
			s, _ := bm["seconds"].(float64)
			secs += s
		}
	}
	if secs == 0 {
		if s, ok := task["plan_summary"].(map[string]any); ok {
			secs, _ = s["seconds"].(float64)
		}
	}
	if secs > 0 {
		parts = append(parts, strconv.FormatFloat(secs, 'f', -1, 64)+" s")
	}
	if len(beats) > 0 {
		parts = append(parts, fmt.Sprintf("%d scenes", len(beats)))
	}
	for _, k := range []string{"aspect", "media_type", "language"} {
		if s, _ := sp[k].(string); s != "" {
			parts = append(parts, strings.ReplaceAll(s, "_", " "))
		}
	}
	if a, ok := sp["audio"].(map[string]any); ok {
		if s, _ := a["mode"].(string); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " · ")
}

// voices lists the cast's voices: "Narrator (Rachel) · Sam".
func voices(sp map[string]any) string {
	cast, _ := sp["cast"].([]any)
	var out []string
	for _, c := range cast {
		cm, _ := c.(map[string]any)
		name, _ := cm["name"].(string)
		if name == "" {
			continue
		}
		if v, ok := cm["voice"].(map[string]any); ok {
			for _, k := range []string{"name", "label", "voice_name"} {
				if s, _ := v[k].(string); s != "" {
					name += " (" + s + ")"
					break
				}
			}
		}
		out = append(out, name)
	}
	return strings.Join(out, " · ")
}

// wrapBeat wraps one "n. (Ns) words" line under its number.
func wrapBeat(l string, width int) string {
	w := width - 10
	if w < 30 || w > 92 {
		w = 72
	}
	return lipgloss.NewStyle().Width(w).Render(l)
}

// doneCard is the finished video: its link, the studio link and how to open it.
func doneCard(j *job) string {
	var b strings.Builder
	b.WriteString(stOK.Render("✓ Your video is ready") + stMuted.Render(" · "+clock(time.Since(j.started))) + "\n")
	if j.videoURL != "" {
		b.WriteString(stMuted.Render("  Video  ") + stKey.Render(clean(j.videoURL)) + "\n")
	}
	if j.editorURL != "" {
		b.WriteString(stMuted.Render("  Studio ") + stKey.Render(clean(j.editorURL)) + "\n")
	}
	b.WriteString(stHint.Render("  Ctrl+O or /open to watch it · type a new idea to make another"))
	return b.String()
}

// footer is the hint line for the current stage.
func (m *model) footer() string {
	switch m.stage {
	case stagePlanning, stageEditing, stageMaking:
		return m.progressLine()
	case stageAsking:
		return stHint.Render("  Type your answer (or its number) and press Enter")
	case stageReady:
		return stHint.Render("  Type a change · Ctrl+G generate · /new start over")
	case stageDone:
		return stHint.Render("  Ctrl+O open · type a new idea")
	}
	return ""
}

// confirmView is the spend question.
func (m *model) confirmView() string {
	q := videos.QuoteLine(m.job.quote)
	price := "this video"
	if q != "" {
		price = strings.SplitN(q, " — ", 2)[0]
	}
	return "\n" + stTitle.Render("  Generate it for "+clean(price)+"?") + stMuted.Render("  y / n") + "\n" +
		stHint.Render("  This spends credits. n or Esc keeps the plan.")
}
