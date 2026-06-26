// Package tui is the interactive Zebracat shell — run `zebracat` with no args.
//
// It's an inline REPL (no alt-screen, so your results stay in the terminal after
// you exit), themed in Zebracat purple, with a branded splash, slash commands,
// autocomplete hints, and a guided "make a video" flow.
//
// It is auth-aware: when you're not signed in it shows that in the header and the
// status line, and it refuses to run anything that would spend credits until you
// /login. Once signed in it greets you with your plan + balance.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/config"
	"github.com/zebracatai/zebracat-cli/internal/dash"
	"github.com/zebracatai/zebracat-cli/internal/update"
	"github.com/zebracatai/zebracat-cli/internal/version"
)

// ---- theme (Zebracat purple) ----------------------------------------------
var (
	cPurple = lipgloss.Color("#7c3aed")
	cPurpLt = lipgloss.Color("#a855f7")
	cMuted  = lipgloss.Color("#9a8fc0")
	cGreen  = lipgloss.Color("#22c55e")
	cRed    = lipgloss.Color("#ef4444")

	stTitle  = lipgloss.NewStyle().Foreground(cPurpLt).Bold(true)
	stPrompt = lipgloss.NewStyle().Foreground(cPurpLt).Bold(true)
	stMuted  = lipgloss.NewStyle().Foreground(cMuted)
	stErr    = lipgloss.NewStyle().Foreground(cRed)
	stOK     = lipgloss.NewStyle().Foreground(cGreen)
	stBox    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cPurple).Padding(0, 2)
	stInput  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cPurple).Padding(0, 1)
	stKey    = lipgloss.NewStyle().Foreground(cPurpLt)
	stSel    = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(cPurple).Bold(true)
	stHint   = lipgloss.NewStyle().Foreground(cMuted).Italic(true)
)

const logo = `╺━┓ ┏━╸ ┣━┓ ┏━┓ ┏━┓ ┏━╸ ┏━┓ ╺┳╸
 ┏┛ ┣━╸ ┣━┫ ┣┳┛ ┣━┫ ┃   ┣━┫  ┃
┗━╸ ┗━╸ ┗━┛ ┻┗╸ ┻ ┻ ┗━╸ ┻ ┻  ╹ `

type slashCmd struct{ name, desc string }

var commands = []slashCmd{
	{"/video", "Create a video (guided)"},
	{"/status", "Check a video: /status <task_id>"},
	{"/projects", "List your recent videos"},
	{"/voices", "List available voices"},
	{"/styles", "List visual styles"},
	{"/account", "Show plan + credit balances"},
	{"/login", "Sign in with your Zebracat account"},
	{"/logout", "Sign out"},
	{"/whoami", "Show the signed-in account"},
	{"/update", "Update the CLI to the latest version"},
	{"/clear", "Clear the screen"},
	{"/help", "Show this help"},
	{"/quit", "Exit the shell"},
}

// openCmds are the only commands allowed while signed out. Everything else is
// gated behind /login so we never try to spend credits for an anonymous user.
var openCmds = map[string]bool{
	"/login": true, "/logout": true, "/help": true,
	"/clear": true, "/quit": true, "/exit": true, "/update": true,
}

// ---- messages -------------------------------------------------------------
type apiMsg struct {
	kind string // "create" | "generic"
	out  any
	err  error
}
type pollMsg struct {
	taskID string
	out    map[string]any
	err    error
}
type acctMsg struct{ a *account }
type projectsMsg struct {
	out map[string]any
	err error
}
type updMsg struct{ latest string } // background "is a newer version out?"
type updDoneMsg struct {            // result of an in-shell /update
	tag string
	err error
}

// ---- guided "make a video" wizard -----------------------------------------
type wizChoice struct{ label, value string }

// wizStep is one question. No choices => a free-text step (the idea).
type wizStep struct {
	key     string
	prompt  string
	choices []wizChoice
}

var wizSteps = []wizStep{
	{key: "idea", prompt: "What should your video be about?"},
	{key: "video_type", prompt: "What kind of video?", choices: []wizChoice{
		{"🎬  AI Video — cinematic AI-generated footage", "ai_video"},
		{"🖼  Moving AI Images — animated still scenes", "moving_ai_images"},
		{"🧑  Avatar — a presenter delivers your script", "ai_avatar"},
		{"📹  Stock footage — real-world clips", "stock_footage"},
		{"🧠  Brainrot — fast, punchy, viral style", "brainrot"},
	}},
	{key: "duration", prompt: "How long should it be?", choices: []wizChoice{
		{"15 seconds — quick hook", "15"},
		{"30 seconds — standard short", "30"},
		{"60 seconds — full short", "60"},
		{"2 minutes", "120"},
		{"3 minutes", "180"},
	}},
	{key: "aspect_ratio", prompt: "Which format?", choices: []wizChoice{
		{"📱  Vertical · 9:16 — TikTok, Reels, Shorts", "vertical"},
		{"⬛  Square · 1:1 — feed posts", "square"},
		{"🖥  Horizontal · 16:9 — YouTube", "horizontal"},
	}},
	{key: "should_render", prompt: "Render the final video now?", choices: []wizChoice{
		{"✨  Yes — render the MP4 now", "yes"},
		{"📝  No — save an editable draft", "no"},
	}},
}

type wizard struct {
	step   int
	cursor int
	vals   map[string]string
}

func (w *wizard) cur() wizStep { return wizSteps[w.step] }

func newWizard(startStep int, idea string) *wizard {
	w := &wizard{step: startStep, vals: map[string]string{}}
	if idea != "" {
		w.vals["idea"] = idea
	}
	return w
}

// account is the slice of GET /account we surface in the header + status line.
type account struct {
	email      string
	plan       string
	credits    string // remaining plan credit, pretty-printed
	apiDollars string // pay-as-you-go balance (api_dollar_balance)
}

type model struct {
	cl      *client.Client
	baseURL string
	in      textinput.Model
	sp      spinner.Model
	busy    bool
	width   int
	matches []string
	wiz     *wizard
	acct    *account
	greeted bool
	askKey  bool   // waiting for the user to paste an API key
	valid   bool   // the next acctMsg is validating a just-entered key
	newer   string // a newer release tag, if the background check found one
}

// New builds the TUI model + program.
func New(cl *client.Client, baseURL string) *tea.Program {
	ti := textinput.New()
	ti.Prompt = "🦓 › "
	ti.PromptStyle = stPrompt
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(cPurpLt)
	ti.CharLimit = 2000
	ti.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(cPurpLt)

	m := &model{cl: cl, baseURL: baseURL, in: ti, sp: sp}
	m.syncPlaceholder()
	return tea.NewProgram(m)
}

func (m *model) Init() tea.Cmd {
	// Emit the banner through tea.Println (not fmt.Print): the terminal is in raw
	// mode, so a bare "\n" wouldn't return the cursor to column 0 and the box would
	// staircase. tea.Println writes proper CR+LF and keeps it above the input.
	cmds := []tea.Cmd{
		tea.Println(splash()),
		textinput.Blink,
		m.sp.Tick,
		m.updateCheckCmd(),
	}
	if m.cl.IsAuthenticated() {
		cmds = append(cmds, m.accountCmd())
	}
	return tea.Batch(cmds...)
}

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

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		iw := msg.Width - 10
		if iw < 10 {
			iw = 10
		}
		m.in.Width = iw
		return m, nil

	case tea.KeyMsg:
		// During a choice step, the arrow keys drive the menu (keyboard select).
		if m.wiz != nil && !m.busy && len(m.wiz.cur().choices) > 0 {
			switch {
			case msg.Type == tea.KeyCtrlC:
				return m, tea.Sequence(tea.Println(stMuted.Render("Goodbye 🦓")), tea.Quit)
			case msg.Type == tea.KeyEsc:
				m.wiz = nil
				return m, tea.Println(stMuted.Render("Cancelled — nothing was created."))
			case msg.Type == tea.KeyUp || msg.String() == "k":
				if m.wiz.cursor > 0 {
					m.wiz.cursor--
				}
				return m, nil
			case msg.Type == tea.KeyDown || msg.String() == "j":
				if m.wiz.cursor < len(m.wiz.cur().choices)-1 {
					m.wiz.cursor++
				}
				return m, nil
			case msg.Type == tea.KeyEnter:
				return m.wizAdvance("")
			}
			return m, nil // ignore stray typing while choosing
		}
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Sequence(tea.Println(stMuted.Render("Goodbye 🦓")), tea.Quit)
		case tea.KeyEsc:
			if m.wiz != nil {
				m.wiz = nil
				m.in.Reset()
				return m, tea.Println(stMuted.Render("Cancelled — nothing was created."))
			}
			return m, nil
		case tea.KeyTab:
			if len(m.matches) > 0 {
				m.in.SetValue(m.matches[0] + " ")
				m.in.CursorEnd()
				m.refreshMatches()
			}
			return m, nil
		case tea.KeyEnter:
			if m.busy {
				return m, nil
			}
			return m.handleEnter()
		}

	case acctMsg:
		m.busy = false
		if msg.a == nil {
			if m.valid { // a key we just saved failed to authenticate
				m.valid = false
				return m, tea.Println(stErr.Render("✗ That key didn't work. ") + stMuted.Render("Check it and /login again."))
			}
			return m, nil
		}
		m.valid = false
		m.acct = msg.a
		if m.greeted {
			return m, nil
		}
		m.greeted = true
		return m, tea.Println(m.greeting())

	case projectsMsg:
		m.busy = false
		if msg.err != nil {
			return m, tea.Println(stErr.Render("✗ ") + msg.err.Error())
		}
		return m, tea.Println(m.dashboardView(msg.out))

	case updMsg:
		if msg.latest != "" && update.Newer("v"+version.Version, msg.latest) {
			m.newer = msg.latest
			return m, tea.Println(stMuted.Render("↑ ") + stKey.Render(msg.latest) +
				stMuted.Render(" is available — run ") + stKey.Render("/update"))
		}
		return m, nil

	case updDoneMsg:
		m.busy = false
		if msg.err != nil {
			return m, tea.Println(stErr.Render("✗ Update failed: ") + msg.err.Error())
		}
		if msg.tag == "" {
			return m, tea.Println(stOK.Render("✓ You're already on the latest version."))
		}
		m.newer = ""
		return m, tea.Println(stOK.Render("✓ Updated to "+msg.tag+". ") +
			stMuted.Render("Quit and relaunch zebracat to use it."))

	case apiMsg:
		m.busy = false
		if msg.err != nil {
			return m, tea.Println(stErr.Render("✗ ") + msg.err.Error())
		}
		if msg.kind == "create" {
			out, _ := msg.out.(map[string]any)
			taskID, _ := out["task_id"].(string)
			if taskID == "" {
				return m, tea.Println(render("Result", msg.out))
			}
			m.busy = true
			return m, tea.Batch(
				tea.Println(stOK.Render("✓ Submitted. ")+stMuted.Render("Generating your video…")),
				m.pollCmd(taskID),
			)
		}
		return m, tea.Println(msg.title())

	case pollMsg:
		if msg.err != nil {
			m.busy = false
			return m, tea.Println(stErr.Render("✗ ") + msg.err.Error())
		}
		status, _ := msg.out["status"].(string)
		if isTerminal(status) {
			m.busy = false
			if status == "completed" {
				return m, tea.Println(stOK.Render("✓ Done! ") + stKey.Render(bestLink(msg.out)))
			}
			emsg, _ := msg.out["error"].(string)
			line := stErr.Render("✗ Video " + status)
			if emsg != "" {
				line += stMuted.Render(" — " + emsg)
			}
			return m, tea.Println(line)
		}
		return m, tea.Tick(5*time.Second, func(time.Time) tea.Msg { return m.poll(msg.taskID) })

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.in, cmd = m.in.Update(msg)
	m.refreshMatches()
	return m, cmd
}

func (m *model) refreshMatches() {
	m.matches = nil
	v := strings.TrimSpace(m.in.Value())
	if !strings.HasPrefix(v, "/") || strings.Contains(v, " ") {
		return
	}
	for _, c := range commands {
		if strings.HasPrefix(c.name, v) {
			m.matches = append(m.matches, c.name)
		}
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

// wizView renders the guided wizard: a bold prompt, then either a text box (the
// idea) or an arrow-key choice list with the current row highlighted.
func (m *model) wizView() string {
	step := m.wiz.cur()
	var b strings.Builder
	b.WriteString("\n" + stTitle.Render("  "+step.prompt) +
		stMuted.Render(fmt.Sprintf("    step %d/%d", m.wiz.step+1, len(wizSteps))) + "\n\n")

	if len(step.choices) == 0 { // free-text step (the idea)
		w := m.width
		if w < 24 {
			w = 60
		}
		b.WriteString(stInput.Width(w-2).Render(m.in.View()) + "\n")
		b.WriteString(stHint.Render("  Enter to continue · Esc to cancel"))
		return b.String()
	}

	for i, c := range step.choices {
		if i == m.wiz.cursor {
			b.WriteString("  " + stSel.Render(" ▶ "+c.label+" ") + "\n")
		} else {
			b.WriteString("     " + stMuted.Render(c.label) + "\n")
		}
	}
	b.WriteString("\n" + stHint.Render("  ↑/↓ move · Enter select · Esc cancel"))
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

// ---- input handling -------------------------------------------------------
func (m *model) handleEnter() (tea.Model, tea.Cmd) {
	// Wizard free-text step (choice steps are driven by arrow keys in Update).
	if m.wiz != nil && !m.askKey {
		return m.wizAdvance(m.in.Value())
	}

	text := strings.TrimSpace(m.in.Value())
	m.in.Reset()
	m.matches = nil

	if m.askKey { // capturing a pasted API key — never echo the secret
		return m.saveKey(text)
	}
	if text == "" {
		return m, nil
	}
	echo := tea.Println(stPrompt.Render("🦓 › ") + text)

	if strings.HasPrefix(text, "/") {
		return m.handleSlash(text, echo)
	}
	// Bare text = a video idea — but that spends credits, so require sign-in.
	if !m.cl.IsAuthenticated() {
		return m, tea.Batch(echo, tea.Println(m.lockMsg()))
	}
	// Use the description as the idea and jump straight into the choices.
	m.wiz = newWizard(1, text)
	return m, tea.Batch(echo, tea.Println(stHint.Render("Got it — just a few quick choices…")))
}

// lockMsg is the friendly "you need to sign in" nudge for gated actions.
func (m *model) lockMsg() string {
	return stErr.Render("🔒 Not signed in. ") +
		stMuted.Render("Type ") + stKey.Render("/login") +
		stMuted.Render(" to add your API key (or set ") +
		stKey.Render("ZEBRACAT_API_KEY") + stMuted.Render(").")
}

// saveKey persists a pasted API key, restores normal input, and verifies it
// against /account.
func (m *model) saveKey(key string) (tea.Model, tea.Cmd) {
	m.askKey = false
	m.in.EchoMode = textinput.EchoNormal
	if key == "" {
		m.syncPlaceholder()
		return m, tea.Println(stMuted.Render("Cancelled."))
	}
	creds, _ := config.LoadCredentials()
	if creds == nil {
		creds = &config.Credentials{}
	}
	creds.APIKey = key
	creds.AccessToken, creds.RefreshToken, creds.ClientID = "", "", ""
	_ = config.SaveCredentials(creds)
	m.cl = client.New(m.baseURL, creds, "")
	m.acct = nil
	m.greeted = false
	m.valid = true
	m.busy = true
	m.syncPlaceholder()
	return m, tea.Batch(
		tea.Println(stOK.Render("✓ API key saved. ")+stMuted.Render("Verifying…")),
		m.accountCmd(),
	)
}

func (m *model) handleSlash(text string, echo tea.Cmd) (tea.Model, tea.Cmd) {
	fields := strings.Fields(text)
	cmd := fields[0]

	// Gate everything that talks to the API behind sign-in.
	if !openCmds[cmd] && !m.cl.IsAuthenticated() {
		return m, tea.Batch(echo, tea.Println(m.lockMsg()))
	}

	switch cmd {
	case "/help":
		return m, tea.Batch(echo, tea.Println(helpText()))
	case "/quit", "/exit":
		return m, tea.Quit
	case "/clear":
		return m, tea.Sequence(tea.ClearScreen, tea.Println(splash()))
	case "/login":
		if m.cl.IsAuthenticated() {
			return m, tea.Batch(echo, tea.Println(stOK.Render("✓ You're already signed in. ")+stMuted.Render("Use /logout to switch accounts.")))
		}
		m.askKey = true
		m.in.EchoMode = textinput.EchoPassword
		m.in.Placeholder = "Paste your API key and press Enter"
		return m, tea.Batch(echo, tea.Println(stMuted.Render("Paste your Zebracat API key — create one at ")+
			stKey.Render("https://studio.zebracat.ai")+stMuted.Render(" → API Keys.")))
	case "/update":
		return m.runUpdate(echo)
	case "/logout":
		_ = config.ClearCredentials()
		m.cl = client.New(m.baseURL, &config.Credentials{}, "")
		m.acct = nil
		m.greeted = false
		m.syncPlaceholder()
		return m, tea.Batch(echo, tea.Println(stOK.Render("✓ Signed out.")), tea.Println(m.authLine()))
	case "/whoami", "/account":
		return m.runAPI(echo, "GET", "/api/v1/public/account", nil)
	case "/voices":
		return m.runAPI(echo, "GET", "/api/v1/public/voices", nil)
	case "/styles":
		return m.runAPI(echo, "GET", "/api/v1/public/visual_styles", nil)
	case "/projects":
		m.busy = true
		return m, tea.Batch(echo, m.projectsCmd())
	case "/status":
		if len(fields) < 2 {
			return m, tea.Batch(echo, tea.Println(stErr.Render("Usage: /status <task_id>")))
		}
		return m.runAPI(echo, "GET", "/api/v1/public/video/status?task_id="+fields[1], nil)
	case "/video":
		m.wiz = newWizard(0, "")
		return m, echo
	default:
		return m, tea.Batch(echo, tea.Println(stErr.Render("Unknown command ")+cmd+stMuted.Render("  — try /help")))
	}
}

// wizAdvance records the current step's answer (a typed idea, or the highlighted
// choice), echoes it to scrollback, and either moves to the next step or submits.
func (m *model) wizAdvance(textVal string) (tea.Model, tea.Cmd) {
	step := m.wiz.cur()
	var answer, display string
	if len(step.choices) == 0 {
		answer = strings.TrimSpace(textVal)
		if answer == "" {
			return m, nil // wait for a non-empty idea
		}
		m.in.Reset()
		display = answer
	} else {
		c := step.choices[m.wiz.cursor]
		answer, display = c.value, c.label
	}
	m.wiz.vals[step.key] = answer
	echo := tea.Println(stMuted.Render("  "+step.prompt+"  ") + stKey.Render(display))

	m.wiz.step++
	m.wiz.cursor = 0
	if m.wiz.step < len(wizSteps) {
		return m, echo
	}

	// All answered — build the request and submit.
	v := m.wiz.vals
	m.wiz = nil
	m.busy = true
	body := map[string]any{
		"prompt":        v["idea"],
		"video_type":    v["video_type"],
		"duration":      atoiOr(v["duration"], 30),
		"aspect_ratio":  v["aspect_ratio"],
		"should_render": v["should_render"] == "yes",
	}
	return m, tea.Batch(echo, tea.Println(stOK.Render("✓ ")+stMuted.Render("Building your video…")), m.createCmd(body))
}

func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

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

var stWorking = lipgloss.NewStyle().Foreground(lipgloss.Color("#eab308"))

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

// ---- rendering ------------------------------------------------------------
func (a apiMsg) title() string { return render("Result", a.out) }

func render(title string, data any) string {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Sprint(data)
	}
	return stMuted.Render(title+":") + "\n" + string(b)
}

func helpText() string {
	var b strings.Builder
	b.WriteString(stTitle.Render("Commands") + "\n")
	for _, c := range commands {
		b.WriteString("  " + stKey.Render(fmt.Sprintf("%-10s", c.name)) + stMuted.Render(c.desc) + "\n")
	}
	b.WriteString("\n" + stMuted.Render("Tip: just type what you want — e.g. ") + stKey.Render("\"a 30s ad for my coffee brand\""))
	return b.String()
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
