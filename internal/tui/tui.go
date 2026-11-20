// Package tui is the interactive Zebracat conversation — run `zebracat`.
//
// An inline REPL (no alt-screen: the conversation stays in your scrollback),
// with a welcome box, a bordered input with history, multi-line paste and `/`
// completion, an animated mark while it works, and a conversation that plans,
// changes and generates a video the way the studio's /chat does (convo.go).
// There are no menus: type, style, voice and length come from the plan and are
// changed in plain words.
package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/zebracatai/zebracat-cli/internal/attach"
	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/update"
	"github.com/zebracatai/zebracat-cli/internal/version"
)

// ---- messages -------------------------------------------------------------
type apiMsg struct { // a slash command's generic API answer
	out any
	err error
}
type pollMsg struct {
	j         *job // the job this poll is for (nil: the current one)
	taskID    string
	out       map[string]any
	err       error
	untilDone bool // keep polling past plan_ready (after Generate)
}
type acctMsg struct {
	a     *account
	err   error
	quiet bool // a background refresh: update the footer, print nothing
}

// keyCheckedMsg is a pasted API key checked against GET /account before it is
// saved: a key that fails never replaces the stored one.
type keyCheckedMsg struct {
	key string
	a   *account
	err error
}
type resizeMsg struct{ gen int } // a resize settled (debounced)

// block is one printed piece of the conversation, kept so a resize can print
// the visible part again at the new width (the welcome is re-rendered).
type block struct{ render func() string }
type projectsMsg struct {
	out map[string]any
	err error
}
type updDoneMsg struct { // result of an in-shell update
	tag  string
	path string // the binary that was replaced, to relaunch
	err  error
}
type firstMsg struct{}

// account is the slice of GET /account shown in the welcome box and footer.
type account struct {
	email      string
	plan       string
	credits    string // remaining plan credit, pretty-printed
	apiDollars string // pay-as-you-go balance (api_dollar_balance)
}

// Launch is what the CLI knows before the conversation opens.
type Launch struct {
	First       string         // a message to send at once (`zebracat create "<idea>"`)
	JustUpdated string         // the version we were updated from (relaunch after an update)
	Images      []attach.Image // pictures given with --image, sent with the first message
}

// pastedMsg is the clipboard picture Ctrl+V read (or why there is none).
type pastedMsg struct {
	im  attach.Image
	err error
}

// clipboardImage reads the clipboard's picture (a test swaps it).
var clipboardImage = attach.Clipboard

// Result tells the caller what to do after the conversation ends.
type Result struct {
	Relaunch string // the path of a freshly installed binary to start, or ""
	From     string // the version it replaced
}

type model struct {
	cl      *client.Client
	baseURL string
	in      textarea.Model
	key     textinput.Model // the API-key field (hidden)
	sp      spinner.Model
	width   int
	height  int
	blocks  []block // the conversation printed so far
	printed int     // lines printed since the screen was last cleared
	resizes int     // resize generation, to redraw once dragging stops
	busy    bool    // a slash command's API call is running
	stage   stage
	job     *job
	carry   string         // the words of a brief the plan asked about, sent with the answer
	images  []attach.Image // pictures attached to the next message (chips in the input)
	acct    *account
	askKey  bool // waiting for the user to paste an API key

	keyRejected bool // the server refused the key in use (401): say so, never "signed in"
	confirming  bool // Generate asked "Generate for N credits? y/N" and waits for the key

	history []string
	histAt  int // len(history) = the line being written

	matches []slashCmd
	sel     int // the highlighted completion

	quitAt     time.Time // first Ctrl-C (a second within quitWindow leaves)
	leaveArmed bool      // first Ctrl-C while a paid job runs
	choice     int       // the highlighted choice under a plan card (Generate / Discard)

	quitting bool   // leaving: the live view is cleared
	welcomed bool   // the welcome box has been printed
	launch   Launch // what Run was told
	offer    string // a newer release the person can install with `u`
	shadow   string // another zebracat earlier on PATH
	result   Result
	clk      func() time.Time
}

const quitWindow = 2 * time.Second

func (m *model) now() time.Time {
	if m.clk != nil {
		return m.clk()
	}
	return time.Now()
}

// newModel builds the model (tests use it directly).
func newModel(cl *client.Client, baseURL string) *model {
	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = 20000
	ta.MaxHeight = 8
	ta.SetHeight(1)
	ta.SetWidth(76)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Placeholder = stMuted
	ta.BlurredStyle = ta.FocusedStyle
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j"))
	ta.Focus()

	ki := textinput.New()
	ki.EchoMode = textinput.EchoPassword
	ki.Placeholder = "Paste your API key and press Enter"
	ki.Prompt = ""

	sp := spinner.New()
	sp.Spinner = spinner.Spinner{Frames: workFrames, FPS: 120 * time.Millisecond}
	sp.Style = stMark

	m := &model{cl: cl, baseURL: baseURL, in: ta, key: ki, sp: sp}
	m.syncPlaceholder()
	return m
}

// Run opens the conversation and returns when the person leaves.
func Run(cl *client.Client, baseURL string, l Launch) (Result, error) {
	applyColorEnv()
	m := newModel(cl, baseURL)
	m.launch = l
	m.images = append(m.images, l.Images...)
	m.shadow = shadowOnPath()

	// Account and update check in parallel, capped, before the welcome box.
	type launched struct {
		a        *account
		rejected bool
	}
	acctCh := make(chan launched, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		a, rejected := launchAccount(ctx, cl)
		acctCh <- launched{a, rejected}
	}()
	notice := update.Check("v" + version.Version)
	if tag := notice(); tag != "" {
		m.offer = update.LatestCached(context.Background())
	}
	first := <-acctCh
	m.acct, m.keyRejected = first.a, first.rejected

	final, err := tea.NewProgram(m).Run()
	if fm, ok := final.(*model); ok {
		return fm.result, err
	}
	return Result{}, err
}

// launchAccount is the check before the welcome: the account, and whether the
// server rejected the key (so the shell never says "signed in" with a dead key).
// A slow or unreachable server is not a rejection.
func launchAccount(ctx context.Context, cl *client.Client) (*account, bool) {
	if !cl.IsAuthenticated() {
		return nil, false
	}
	a, err := fetchAccountErr(ctx, cl)
	return a, keyRejected(err)
}

// shadowOnPath is the path of another zebracat that runs first from PATH, or "".
func shadowOnPath() string {
	first, err := exec.LookPath("zebracat")
	if err != nil {
		return ""
	}
	me := update.CurrentPath()
	if r, err := filepath.EvalSymlinks(first); err == nil {
		first = r
	}
	if abs, err := filepath.Abs(first); err == nil {
		first = abs
	}
	if first == me {
		return ""
	}
	return first
}

func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.sp.Tick, textarea.Blink}
	if m.launch.First != "" {
		cmds = append(cmds, func() tea.Msg { return firstMsg{} })
	}
	return tea.Batch(cmds...)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	mm, cmd := m.update(msg)
	if !m.askKey {
		m.syncPlaceholder() // the input hint follows the conversation
	}
	m.fitInput()
	return mm, cmd
}

func (m *model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if !m.welcomed {
			m.welcomed = true
			return m, m.sayWelcome()
		}
		m.resizes++
		gen := m.resizes
		return m, tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg { return resizeMsg{gen} })

	case resizeMsg:
		if msg.gen != m.resizes {
			return m, nil // still resizing
		}
		return m, m.redraw()

	case firstMsg:
		text := m.launch.First
		m.launch.First = ""
		m.in.SetValue(text)
		return m.handleEnter()

	case tea.KeyMsg:
		return m.onKey(msg)

	case acctMsg:
		m.busy = false
		if keyRejected(msg.err) {
			m.keyRejected = true
		}
		if msg.a == nil {
			if msg.quiet || msg.err == nil {
				return m, nil
			}
			return m, m.sayAPIErr(msg.err, "")
		}
		m.keyRejected = false
		m.acct = msg.a
		if msg.quiet {
			return m, nil
		}
		return m, m.say(stOK.Render("✓ ") + "Signed in" + stMuted.Render(m.accountLine()))

	case keyCheckedMsg:
		return m.onKeyChecked(msg)

	case projectsMsg:
		m.busy = false
		if msg.err != nil {
			return m, m.sayAPIErr(msg.err, "")
		}
		return m, m.say(m.dashboardView(msg.out))

	case updDoneMsg:
		m.busy = false
		if msg.err != nil {
			if me, ok := msg.err.(*update.ErrManaged); ok {
				return m, m.say(stMuted.Render("⎿ ") + "This copy was installed with " + me.Method + ". Update it with " + stKey.Render(me.Command))
			}
			return m, m.sayErr("The update failed: "+msg.err.Error(), "Try `zebracat update` from your shell.")
		}
		if msg.tag == "" {
			return m, m.say(stOK.Render("✓ ") + "You're on the latest version.")
		}
		m.result = Result{Relaunch: msg.path, From: "v" + version.Version}
		m.quitting = true
		return m, tea.Sequence(m.say(stOK.Render("✓ ")+"Updated to "+msg.tag+stMuted.Render(" — relaunching…")), tea.Quit)

	case apiMsg:
		m.busy = false
		if msg.err != nil {
			return m, m.sayAPIErr(msg.err, "")
		}
		return m, m.say(render("Result", msg.out))

	case pastedMsg:
		if msg.err != nil {
			if msg.err == attach.ErrNoImage {
				return m, m.note("No picture in the clipboard. Text pastes with your terminal's paste (Cmd+V / Ctrl+Shift+V); you can also drag a picture file in.")
			}
			return m, m.sayErr(msg.err.Error(), "Drag the picture file into the terminal instead.")
		}
		m.images = append(m.images, msg.im)
		return m, nil
	case createdMsg:
		return m.onCreated(msg)
	case planMsg:
		return m.onPlan(msg)
	case answeredMsg:
		return m.onAnswered(msg)
	case generatedMsg:
		return m.onGenerated(msg)
	case discardedMsg:
		return m.onDiscarded(msg)
	case pollMsg:
		return m.onPoll(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	if m.askKey {
		m.key, cmd = m.key.Update(msg)
	} else {
		m.in, cmd = m.in.Update(msg)
	}
	return m, cmd
}

// onKey routes a key press: the spend question, Ctrl-C/Esc, the update offer,
// completion, history, Enter, then typing.
func (m *model) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type != tea.KeyCtrlC {
		m.quitAt = time.Time{}
	}
	if handled, model, cmd := m.choiceKey(msg); handled {
		return model, cmd
	}
	switch msg.Type {
	case tea.KeyCtrlC:
		return m.interrupt()
	case tea.KeyEsc:
		if m.askKey { // Esc leaves the key field, like Ctrl-C
			return m.interrupt()
		}
		if len(m.matches) > 0 {
			m.matches = nil
			return m, nil
		}
		return m.escape()
	case tea.KeyCtrlO:
		return m.openVideo()
	case tea.KeyCtrlV: // paste a picture from the clipboard
		if m.askKey {
			break
		}
		return m.pasteClipboard()
	}
	// Alt+V does the same: Windows Terminal keeps Ctrl+V for its own text paste.
	if msg.Alt && msg.Type == tea.KeyRunes && string(msg.Runes) == "v" && !m.askKey {
		return m.pasteClipboard()
	}
	if m.askKey {
		if msg.Type == tea.KeyEnter {
			return m.handleEnter()
		}
		var cmd tea.Cmd
		m.key, cmd = m.key.Update(msg)
		return m, cmd
	}
	// The launch update offer: `u` on an empty line installs it; any other key
	// withdraws the offer (so a sentence starting with "u" is never stolen later).
	if m.offer != "" && m.stage == stageIdle && strings.TrimSpace(m.in.Value()) == "" {
		if msg.Type == tea.KeyRunes && string(msg.Runes) == "u" {
			tag := m.offer
			m.offer = ""
			return m.runUpdate(m.say(stMuted.Render("⎿ Updating to " + tag + "…")))
		}
		m.offer = ""
	}
	// A paste (or a drag-and-drop) that names picture files becomes chips.
	if msg.Paste {
		msg.Runes = oneNewline(msg.Runes)
		imgs, err := attach.Paths(string(msg.Runes))
		if err != nil {
			return m, m.sayErr(err.Error(), "Pictures can be PNG, JPEG or WebP, up to "+attach.Size(attach.MaxBytes)+".")
		}
		if len(imgs) > 0 {
			m.images = append(m.images, imgs...)
			return m, nil
		}
	}
	// Backspace on an empty line removes the last picture.
	if msg.Type == tea.KeyBackspace && len(m.images) > 0 && m.in.Value() == "" {
		m.dropImage(len(m.images) - 1)
		return m, nil
	}
	switch msg.Type {
	case tea.KeyTab:
		if len(m.matches) > 0 {
			m.in.SetValue(m.matches[m.sel].name + " ")
			m.in.CursorEnd()
			m.refreshMatches()
		}
		return m, nil
	case tea.KeyUp:
		if len(m.matches) > 0 {
			m.sel = (m.sel + len(m.matches) - 1) % len(m.matches)
			return m, nil
		}
		if m.in.Line() == 0 && m.histAt > 0 {
			m.histAt--
			m.in.SetValue(m.history[m.histAt])
			return m, nil
		}
	case tea.KeyDown:
		if len(m.matches) > 0 {
			m.sel = (m.sel + 1) % len(m.matches)
			return m, nil
		}
		if m.in.Line() == m.in.LineCount()-1 && m.histAt < len(m.history) {
			m.histAt++
			if m.histAt == len(m.history) {
				m.in.Reset()
			} else {
				m.in.SetValue(m.history[m.histAt])
			}
			return m, nil
		}
	case tea.KeyEnter:
		if msg.Alt {
			break // alt+enter: a new line
		}
		if m.busy {
			return m, nil
		}
		if len(m.matches) > 0 && !strings.Contains(strings.TrimSpace(m.in.Value()), " ") {
			if exact := strings.TrimSpace(m.in.Value()); exact != m.matches[m.sel].name {
				m.in.SetValue(m.matches[m.sel].name)
			}
		}
		return m.handleEnter()
	}
	var cmd tea.Cmd
	m.in, cmd = m.in.Update(msg)
	m.refreshMatches()
	return m, cmd
}

// fitInput sizes the input to the terminal and to its text (1–8 rows).
func (m *model) fitInput() {
	w := m.inputWidth() - 2 // the "› " prompt
	if w < 10 {
		w = 10
	}
	m.in.SetWidth(w)
	m.key.Width = w
	h := m.in.LineCount()
	if h < 1 {
		h = 1
	}
	if h > 8 {
		h = 8
	}
	m.in.SetHeight(h)
}

func (m *model) refreshMatches() {
	m.matches = nil
	m.sel = 0
	v := strings.TrimSpace(m.in.Value())
	if !strings.HasPrefix(v, "/") || strings.ContainsAny(v, " \n") {
		return
	}
	for _, c := range commands {
		if strings.HasPrefix(c.name, v) {
			m.matches = append(m.matches, c)
		}
	}
}

// oneNewline makes a paste's Windows (\r\n) and old Mac (\r) line ends one
// \n each, so a pasted script keeps its lines instead of doubling them.
func oneNewline(r []rune) []rune {
	if !strings.ContainsRune(string(r), '\r') {
		return r
	}
	s := strings.ReplaceAll(string(r), "\r\n", "\n")
	return []rune(strings.ReplaceAll(s, "\r", "\n"))
}

// remember adds a sent message to the Up-arrow history.
func (m *model) remember(text string) {
	if text != "" && (len(m.history) == 0 || m.history[len(m.history)-1] != text) {
		m.history = append(m.history, text)
	}
	m.histAt = len(m.history)
}

// cwd is the working directory with $HOME shortened to ~ (a test pins it).
var cwd = func() string {
	d, err := os.Getwd()
	if err != nil {
		return ""
	}
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(d, h) {
		d = "~" + strings.TrimPrefix(d, h)
	}
	return d
}

// dropImage removes a picture (and its temp file, if it came from the clipboard).
func (m *model) dropImage(i int) {
	if i < 0 || i >= len(m.images) {
		return
	}
	if m.images[i].Temp {
		os.Remove(m.images[i].Path)
	}
	m.images = append(m.images[:i], m.images[i+1:]...)
}

// chips is the attached pictures as they show in the input.
func (m *model) chips() string {
	var parts []string
	for i, im := range m.images {
		parts = append(parts, stKey.Render(im.Chip(i+1)))
	}
	return strings.Join(parts, " ")
}

// pasteClipboard reads the clipboard's picture in the background.
func (m *model) pasteClipboard() (tea.Model, tea.Cmd) {
	return m, func() tea.Msg { im, err := clipboardImage(); return pastedMsg{im, err} }
}
