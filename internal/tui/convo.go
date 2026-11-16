// The conversation, like the studio's v2 /chat. The person types an idea,
// pastes a link (or loads a script); Director v2 writes the plan (POST /videos,
// plan only); the CLI shows it as a card with the quote; plain words change it
// (the plan chat, POST /videos/{id}/plan/chat); Generate happens only after
// Ctrl+G and a y. When the plan asks something — a question card, or a brief
// with nothing to film ("What should your video be about?") — that is the
// assistant's turn and the person's next message answers it; it is never shown
// as a failure.
package tui

import (
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zebracatai/zebracat-cli/internal/brief"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
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

// The run's own codes for "this brief is a question back, not a failure"
// (backend engine/director_v2/run.py PLAN_BLOCKED; public_apis/v2/lifecycle.py
// "<request>_request"). Structured codes, never the message text.
const codeBlocked = "plan_blocked"

// requestHints: a brief that asked for another tool, by its code.
var requestHints = map[string]string{
	"translate_video_request": "That sounds like a translation. Run: zebracat video translate --url <video> --to <language>",
	"character_swap_request":  "That sounds like a character swap. Run: zebracat swap --video <url> --character <face-image-url> --consent",
}

// job is the one video the conversation is about.
type job struct {
	taskID       string
	text         string         // what the person sent (kept for a question back)
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
	offline      bool // the last poll failed; we keep trying quietly
}

// ---- messages (each carries its job, so a late answer for a job the person
// left is ignored) ------------------------------------------------------------

type createdMsg struct {
	j   *job
	out map[string]any
	err error
}
type planMsg struct { // a plan is ready (or changed): the task, its plan view, the quote
	j     *job
	task  map[string]any
	plan  map[string]any
	quote map[string]any
	reply string // the plan chat's answer, for a change
	err   error
}
type answeredMsg struct {
	j   *job
	err error
}
type generatedMsg struct {
	j   *job
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
	note := "Writing a plan — free, nothing is charged until you generate"
	if link, _ := body["source_video"].(string); link != "" {
		note = "Reading that video and writing a plan — free until you generate"
	} else if body["input_kind"] == "url" {
		note = "Reading that page and writing a plan — free until you generate"
	}
	m.job = &job{text: text, shouldRender: true, started: m.now(), phase: "pending"}
	m.stage = stagePlanning
	m.leaveArmed = false
	return m, tea.Batch(m.note(note), m.createCmd(m.job, body))
}

// ---- results --------------------------------------------------------------------

func (m *model) onCreated(msg createdMsg) (tea.Model, tea.Cmd) {
	if msg.j != m.job || m.job == nil {
		return m, nil
	}
	if msg.err != nil {
		m.stage, m.job = stageIdle, nil
		return m, m.sayAPIErr(msg.err, "Nothing was charged. Try again.")
	}
	id, _ := msg.out["task_id"].(string)
	if id == "" {
		m.stage, m.job = stageIdle, nil
		return m, m.sayErr("The server answered without a task id.", "Try again in a moment.")
	}
	m.job.taskID = id
	m.job.task = msg.out
	m.job.phase = "planning"
	return m, m.pollCmd(id, false)
}

// onPoll moves the conversation on from a task view.
func (m *model) onPoll(msg pollMsg) (tea.Model, tea.Cmd) {
	j := m.job
	if j == nil || j.taskID != msg.taskID || (msg.j != nil && msg.j != j) {
		return m, nil // a poll of a job the person left
	}
	again := tea.Tick(5*time.Second, func(time.Time) tea.Msg {
		pm := m.poll(msg.taskID, msg.untilDone).(pollMsg)
		pm.j = j
		return pm
	})
	if msg.err != nil {
		if ce, ok := msg.err.(*clierr.Error); ok && ce.Exit == clierr.CodeAuth {
			m.stage = stageIdle
			return m, m.sayAPIErr(msg.err, "")
		}
		if j.offline {
			return m, again // said once; keep trying quietly
		}
		j.offline = true
		return m, tea.Batch(m.note("Lost the connection — still trying. Your video is safe on our servers."), again)
	}
	var back tea.Cmd
	if j.offline {
		j.offline = false
		back = m.note("Back online.")
	}
	j.task = msg.out
	if p := videos.PhaseOrStatus(msg.out); p != "" {
		j.phase = p
	}
	if u := videos.EditorURL(msg.out); u != "" {
		j.editorURL = u
	}
	if !videos.StopsWait(msg.out, msg.untilDone) {
		return m, tea.Batch(back, again)
	}
	switch videos.PhaseOrStatus(msg.out) {
	case "plan_ready":
		return m, tea.Batch(back, m.planCmd(j, msg.out, ""))
	case "needs_input":
		m.stage = stageAsking
		qid, _, opts := videos.Question(msg.out)
		j.questionID, j.options = qid, opts
		return m, tea.Batch(back, m.assistant(questionCard(msg.out)))
	case "completed":
		m.stage = stageDone
		j.videoURL, _ = msg.out["video_url"].(string)
		return m, tea.Batch(back, m.say(doneCard(j, m.now().Sub(j.started))))
	}
	return m.ended(msg.out, msg.untilDone)
}

// ended handles a task that stopped without a plan or a video: a question back
// is a turn; a request for another tool says which; anything else is an error
// with what to do.
func (m *model) ended(task map[string]any, paid bool) (tea.Model, tea.Cmd) {
	j := m.job
	code, text := errCode(task), errText(task)
	m.stage, m.job = stageIdle, nil
	switch {
	case code == codeBlocked && !paid:
		m.carry = j.text // the answer goes with what they said first
		if text == "" {
			text = "What should your video show or be about? Tell me in a sentence or two."
		}
		return m, m.assistant(text)
	case requestHints[code] != "" && !paid:
		return m, m.assistant(requestHints[code])
	case videos.PhaseOrStatus(task) == "cancelled":
		return m, m.note("This video was cancelled. Nothing more will be charged.")
	}
	if text == "" {
		text = "something went wrong on our side"
	}
	if paid {
		return m, m.sayErr("The video couldn't be finished: "+text,
			"See `zebracat video status "+j.taskID+"`; quote that id to support if it stays failed.")
	}
	return m, m.sayErr("The plan couldn't be written: "+text, "Nothing was charged. Try again, or say it differently.")
}

func (m *model) onPlan(msg planMsg) (tea.Model, tea.Cmd) {
	j := m.job
	if j == nil || msg.j != j {
		return m, nil
	}
	if msg.err != nil {
		m.stage = stageReady
		return m, m.sayAPIErr(msg.err, "The plan is unchanged.")
	}
	if msg.task != nil {
		j.task = msg.task
	}
	if msg.plan != nil {
		j.plan = msg.plan
	}
	if msg.quote != nil {
		j.quote = msg.quote
	}
	m.stage = stageReady
	var out []tea.Cmd
	if msg.reply != "" {
		out = append(out, m.assistant(clean(msg.reply)))
	}
	if msg.plan != nil || msg.task != nil {
		out = append(out, m.say(planCard(j, m.width)))
	}
	return m, tea.Sequence(out...)
}

func (m *model) onAnswered(msg answeredMsg) (tea.Model, tea.Cmd) {
	if m.job == nil || msg.j != m.job {
		return m, nil
	}
	if msg.err != nil {
		m.stage = stageAsking
		return m, m.sayAPIErr(msg.err, "Answer again.")
	}
	return m, m.pollCmd(m.job.taskID, false)
}

func (m *model) onGenerated(msg generatedMsg) (tea.Model, tea.Cmd) {
	if m.job == nil || msg.j != m.job {
		return m, nil
	}
	if msg.err != nil {
		m.stage = stageReady
		return m, m.sayAPIErr(msg.err, "Nothing was generated; the plan is kept.")
	}
	m.stage = stageMaking
	m.job.started = m.now()
	m.job.phase = "generating"
	return m, tea.Batch(m.note("Generating — a few minutes. Esc stops watching; the video keeps going."),
		m.pollCmd(m.job.taskID, true))
}

// sayAPIErr turns a call's error into a plain sentence and one fix line.
func (m *model) sayAPIErr(err error, after string) tea.Cmd {
	ce, ok := err.(*clierr.Error)
	if !ok {
		return m.sayErr(err.Error(), after)
	}
	switch {
	case ce.Exit == clierr.CodeAuth:
		return m.sayErr("Your API key was rejected (expired or removed).", "Type /login to add a new one.")
	case ce.Code == "network_error" || ce.Exit == clierr.CodeTimeout:
		return m.sayErr("Can't reach Zebracat right now.", strings.TrimSpace("Check your internet connection. "+after))
	case ce.Code == "insufficient_funds" || ce.Type == "insufficient_funds":
		fix := ce.Hint
		if fix == "" {
			fix = "Top up at https://studio.zebracat.ai, then try again."
		}
		return m.sayErr("Your balance doesn't cover this.", fix)
	case ce.Code == "rate_limited" || ce.Type == "rate_limited":
		return m.sayErr("Too many requests just now.", strings.TrimSpace("Wait a minute. "+after))
	}
	fix := ce.Hint
	if fix == "" {
		fix = after
	} else if after != "" {
		fix += " " + after
	}
	return m.sayErr(ce.Message, strings.TrimSpace(fix))
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
		return m, m.answerCmd(m.job, m.job.taskID, m.job.questionID, answer)
	case stageReady:
		m.stage = stageEditing
		m.job.started = m.now()
		return m, m.chatCmd(m.job, m.job.taskID, text)
	case stagePlanning, stageEditing, stageMaking:
		return m, m.note("One moment — I'm still on the last step (Esc stops waiting).")
	}
	if m.carry != "" { // answering the plan's question about the first message
		text = m.carry + "\n" + text
		m.carry = ""
	}
	return m.startPlan(text, brief.KindIdea)
}

// askGenerate shows the explicit spend question.
func (m *model) askGenerate() (tea.Model, tea.Cmd) {
	if m.stage != stageReady || m.job == nil {
		return m, m.note("There's no plan to generate yet — describe a video first.")
	}
	if ok, known := m.job.quote["sufficient"].(bool); known && !ok {
		return m, m.sayErr("Your balance doesn't cover this video ("+strings.SplitN(videos.QuoteLine(m.job.quote), " — ", 2)[0]+").",
			"Top up at https://studio.zebracat.ai, then Ctrl+G again. The plan is kept.")
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
		m.job.phase = "generating"
		return m, tea.Batch(m.note("Generate: yes"), m.generateCmd(m.job, m.job.taskID, m.job.shouldRender))
	case "n", "esc":
		m.stage = stageReady
		return m, m.note("Not now — the plan is kept and nothing was charged.")
	case "ctrl+c":
		m.stage = stageReady
		return m.interrupt()
	}
	return m, nil
}

// escape is Esc: stop waiting. A plan keeps being written on the server (it's
// free); a paid video keeps being made; nothing is ever cancelled.
func (m *model) escape() (tea.Model, tea.Cmd) {
	j := m.job
	if j == nil {
		return m, nil
	}
	switch m.stage {
	case stagePlanning:
		m.stage, m.job = stageIdle, nil
		if j.taskID != "" {
			return m, m.note("Stopped waiting. Nothing was charged. See that plan later with `zebracat video status " + j.taskID + "`.")
		}
		return m, m.note("Stopped waiting. Nothing was charged.")
	case stageMaking:
		m.stage, m.job = stageIdle, nil
		return m, m.note("Stopped watching — the video keeps being made. Follow it with `zebracat video status " + j.taskID + " --wait`.")
	}
	return m, nil
}

// interrupt is Ctrl-C: it clears a half-typed line; otherwise the first press
// warns and a second within two seconds leaves. It never cancels anything.
func (m *model) interrupt() (tea.Model, tea.Cmd) {
	if strings.TrimSpace(m.in.Value()) != "" && !m.askKey {
		m.in.Reset()
		m.matches = nil
		return m, nil
	}
	if m.askKey {
		m.askKey = false
		m.key.Reset()
		m.in.Focus()
		return m, m.note("Cancelled.")
	}
	if m.stage == stageMaking && !m.leaveArmed {
		m.leaveArmed = true
		m.quitAt = m.now()
		return m, m.say(stWorking.Render("! ") + "Your video keeps being made on our servers; nothing is cancelled.")
	}
	if !m.quitAt.IsZero() && m.now().Sub(m.quitAt) <= quitWindow {
		return m.leave("")
	}
	m.quitAt = m.now()
	return m, nil
}

// leave quits with a line on how to pick up where the person left off.
func (m *model) leave(extra string) (tea.Model, tea.Cmd) {
	var lines []string
	if extra != "" {
		lines = append(lines, extra)
	}
	if j := m.job; j != nil && j.taskID != "" {
		switch m.stage {
		case stageMaking:
			lines = append(lines, stMuted.Render("Your video keeps being made. Follow it with ")+stKey.Render("zebracat video status "+j.taskID+" --wait"))
		case stageReady, stageConfirm, stageEditing, stageAsking:
			lines = append(lines, stMuted.Render("Your plan is saved. Generate it later with ")+stKey.Render("zebracat video generate "+j.taskID))
		case stagePlanning:
			lines = append(lines, stMuted.Render("The plan keeps being written. See it with ")+stKey.Render("zebracat video status "+j.taskID+" --wait"))
		}
	}
	lines = append(lines, stMuted.Render("Bye."))
	m.quitting = true
	return m, tea.Sequence(m.say(strings.Join(lines, "\n")), tea.Quit)
}

// openVideo opens the finished video (or its studio page).
func (m *model) openVideo() (tea.Model, tea.Cmd) {
	if m.job == nil {
		return m, m.note("Nothing to open yet.")
	}
	u := m.job.videoURL
	if u == "" {
		u = m.job.editorURL
	}
	if u == "" {
		return m, m.note("Nothing to open yet.")
	}
	if err := openURL(u); err != nil {
		return m, m.sayErr("Couldn't open a browser.", "Open it yourself: "+u)
	}
	return m, m.note("Opened " + u)
}

// ---- the animated mark ------------------------------------------------------------

// verbs: what the server says is happening, in words (no made-up steps).
var verbs = map[string]string{
	"pending":    "Starting",
	"planning":   "Writing the screenplay",
	"plan_ready": "Reading the plan",
	"generating": "Making the scenes",
	"processing": "Making the scenes",
	"rendering":  "Exporting the MP4",
}

// progressLine is the live line above the input while something runs:
// "▓ Writing the screenplay… (0:42 · esc to interrupt)".
func (m *model) progressLine() string {
	j := m.job
	if j == nil {
		return ""
	}
	var verb string
	switch m.stage {
	case stagePlanning, stageMaking:
		verb = verbs[j.phase]
		if verb == "" {
			verb = "Working"
		}
	case stageEditing:
		verb = "Changing the plan"
	default:
		return ""
	}
	tail := clock(m.now().Sub(j.started))
	if p, ok := j.task["progress"].(float64); ok && p > 0 && m.stage == stageMaking {
		if p <= 1 {
			p *= 100
		}
		tail += " · " + strconv.Itoa(int(p)) + "%"
	}
	if m.stage != stageEditing {
		tail += " · esc to interrupt"
	}
	if j.offline {
		tail += " · reconnecting"
	}
	return m.sp.View() + " " + stKey.Render(verb+"…") + stMuted.Render(" ("+tail+")")
}
