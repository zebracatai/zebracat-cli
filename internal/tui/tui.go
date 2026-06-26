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

	"github.com/zebracatai/zebracat-cli/internal/auth"
	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/config"
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
	stKey    = lipgloss.NewStyle().Foreground(cPurpLt)
)

const logo = `   __ ___ _ ___ _ _ __ __ _ ___
  |_ / -_) '_ \ '_/ _` + "`" + `/ _/ _` + "`" + `|  _|
  /__\___|_.__/_| \__,_\__\__,_|\__|`

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
	{"/clear", "Clear the screen"},
	{"/help", "Show this help"},
	{"/quit", "Exit the shell"},
}

// openCmds are the only commands allowed while signed out. Everything else is
// gated behind /login so we never try to spend credits for an anonymous user.
var openCmds = map[string]bool{
	"/login": true, "/logout": true, "/help": true,
	"/clear": true, "/quit": true, "/exit": true,
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
type loginMsg struct{ err error }
type acctMsg struct{ a *account }
type printMsg string // emitted to scrollback from a goroutine

type wizard struct {
	step   int
	idea   string
	dur    int
	render bool
}

// account is the slice of GET /account we surface in the header + status line.
type account struct {
	email      string
	plan       string
	credits    string // remaining plan credit, pretty-printed
	apiDollars string // pay-as-you-go balance (api_dollar_balance)
}

type model struct {
	prog    *tea.Program
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
	p := tea.NewProgram(m)
	m.prog = p
	return p
}

func (m *model) Init() tea.Cmd {
	fmt.Print(splash())
	fmt.Print(m.authLine() + "\n\n")
	cmds := []tea.Cmd{textinput.Blink, m.sp.Tick}
	if m.cl.IsAuthenticated() {
		cmds = append(cmds, m.accountCmd())
	}
	return tea.Batch(cmds...)
}

func splash() string {
	body := stTitle.Render(logo) + "\n\n" +
		stMuted.Render("AI video generation, right in your terminal.") + "\n\n" +
		"Type " + stKey.Render("/help") + " for commands · " + stKey.Render("/quit") + " to exit\n" +
		stMuted.Render("Try ") + stKey.Render("/video") + stMuted.Render(" — or just describe the video you want.")
	return stBox.Render(body) + "\n\n"
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
			stKey.Render("/login") + stMuted.Render(" to start — it uses your plan credits, no API key needed.")
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.in.Width = msg.Width - 8
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			fmt.Println(stMuted.Render("Goodbye 🦓"))
			return m, tea.Quit
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

	case printMsg:
		return m, tea.Println(string(msg))

	case loginMsg:
		m.busy = false
		if msg.err != nil {
			return m, tea.Println(stErr.Render("✗ Sign-in failed: ") + msg.err.Error())
		}
		// Reload credentials into a fresh client, then re-greet from /account.
		creds, _ := config.LoadCredentials()
		m.cl = client.New(m.baseURL, creds, "")
		m.acct = nil
		m.greeted = false
		m.syncPlaceholder()
		return m, tea.Batch(
			tea.Println(stOK.Render("✓ Signed in. ")+stMuted.Render("You're spending your plan credits.")),
			m.accountCmd(),
		)

	case acctMsg:
		if msg.a == nil {
			return m, nil
		}
		m.acct = msg.a
		if m.greeted {
			return m, nil
		}
		m.greeted = true
		return m, tea.Println(m.greeting())

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
				url, _ := msg.out["video_url"].(string)
				return m, tea.Println(stOK.Render("✓ Done! ") + url)
			}
			return m, tea.Println(stErr.Render("✗ Video " + status))
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
	var b strings.Builder
	// autocomplete hint line
	if len(m.matches) > 0 {
		var parts []string
		for _, mtc := range m.matches {
			parts = append(parts, stKey.Render(mtc))
		}
		b.WriteString(stMuted.Render("  ↹ ") + strings.Join(parts, stMuted.Render(" · ")) + "\n")
	} else if m.wiz != nil {
		b.WriteString(stMuted.Render("  "+m.wizPrompt()) + "\n")
	}
	b.WriteString(m.in.View() + "\n")
	if m.busy {
		b.WriteString(m.sp.View() + stMuted.Render(" working…") + "\n")
	}
	b.WriteString(m.statusLine())
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
	text := strings.TrimSpace(m.in.Value())
	m.in.Reset()
	m.matches = nil
	if text == "" {
		return m, nil
	}
	echo := tea.Println(stPrompt.Render("🦓 › ") + text)

	if m.wiz != nil {
		return m.handleWizard(text, echo)
	}
	if strings.HasPrefix(text, "/") {
		return m.handleSlash(text, echo)
	}
	// Bare text = a video idea — but that spends credits, so require sign-in.
	if !m.cl.IsAuthenticated() {
		return m, tea.Batch(echo, tea.Println(m.lockMsg()))
	}
	m.wiz = &wizard{step: 1, idea: text}
	return m, tea.Batch(echo, tea.Println(stMuted.Render("Length in seconds? 15 / 30 / 60 / 120 / 180  (Enter for 30)")))
}

// lockMsg is the friendly "you need to sign in" nudge for gated actions.
func (m *model) lockMsg() string {
	return stErr.Render("🔒 Not signed in. ") +
		stMuted.Render("Type ") + stKey.Render("/login") +
		stMuted.Render(" to continue — it uses your plan credits (or set ") +
		stKey.Render("ZEBRACAT_API_KEY") + stMuted.Render(" for pay-as-you-go).")
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
		return m, tea.Batch(echo, tea.ClearScreen, func() tea.Msg {
			fmt.Print(splash())
			fmt.Print(m.authLine() + "\n\n")
			return nil
		})
	case "/login":
		if m.cl.IsAuthenticated() {
			return m, tea.Batch(echo, tea.Println(stOK.Render("✓ You're already signed in. ")+stMuted.Render("Use /logout to switch accounts.")))
		}
		m.busy = true
		return m, tea.Batch(echo, tea.Println(stMuted.Render("Opening your browser to sign in…")), m.loginCmd())
	case "/logout":
		_ = config.ClearCredentials()
		m.cl = client.New(m.baseURL, &config.Credentials{}, "")
		m.acct = nil
		m.greeted = false
		m.syncPlaceholder()
		return m, tea.Batch(echo, tea.Println(stOK.Render("✓ Signed out.")), tea.Println(m.authLine()))
	case "/whoami", "/account":
		return m.runAPI(echo, "GET", "/api/public/account", nil)
	case "/voices":
		return m.runAPI(echo, "GET", "/api/public/voices", nil)
	case "/styles":
		return m.runAPI(echo, "GET", "/api/public/visual_styles", nil)
	case "/projects":
		return m.runAPI(echo, "GET", "/api/public/projects?limit=10", nil)
	case "/status":
		if len(fields) < 2 {
			return m, tea.Batch(echo, tea.Println(stErr.Render("Usage: /status <task_id>")))
		}
		return m.runAPI(echo, "GET", "/api/public/video/status?task_id="+fields[1], nil)
	case "/video":
		m.wiz = &wizard{step: 0}
		return m, tea.Batch(echo, tea.Println(stMuted.Render("What should the video be about?")))
	default:
		return m, tea.Batch(echo, tea.Println(stErr.Render("Unknown command ")+cmd+stMuted.Render("  — try /help")))
	}
}

func (m *model) handleWizard(val string, echo tea.Cmd) (tea.Model, tea.Cmd) {
	w := m.wiz
	switch w.step {
	case 0:
		if val == "" {
			return m, tea.Batch(echo, tea.Println(stMuted.Render("Tell me what the video is about:")))
		}
		w.idea = val
		w.step = 1
		return m, tea.Batch(echo, tea.Println(stMuted.Render("Length in seconds? 15 / 30 / 60 / 120 / 180  (Enter for 30)")))
	case 1:
		dur := 30
		if val != "" {
			n, err := strconv.Atoi(val)
			if err != nil || !validDur(n) {
				return m, tea.Batch(echo, tea.Println(stErr.Render("Pick one of 15 / 30 / 60 / 120 / 180")))
			}
			dur = n
		}
		w.dur = dur
		w.step = 2
		return m, tea.Batch(echo, tea.Println(stMuted.Render("Render the final MP4 now? y / N  (N saves an editable draft)")))
	case 2:
		w.render = val == "y" || val == "Y" || strings.EqualFold(val, "yes")
		idea, dur, render := w.idea, w.dur, w.render
		m.wiz = nil
		m.busy = true
		body := map[string]any{"prompt": idea, "duration": dur, "should_render": render}
		return m, tea.Batch(echo, tea.Println(stMuted.Render("Building your video…")), m.createCmd(body))
	}
	return m, echo
}

func (m *model) wizPrompt() string {
	switch m.wiz.step {
	case 0:
		return "describe the video"
	case 1:
		return "15 / 30 / 60 / 120 / 180"
	case 2:
		return "y / N"
	}
	return ""
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
		_, err := cl.Do(ctx, "POST", "/api/public/video/agentic", body, &out)
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
	_, err := m.cl.Do(ctx, "GET", "/api/public/video/status?task_id="+taskID, nil, &out)
	return pollMsg{taskID: taskID, out: out, err: err}
}

// accountCmd fetches GET /account in the background to populate the header.
func (m *model) accountCmd() tea.Cmd {
	cl := m.cl
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var out map[string]any
		if _, err := cl.Do(ctx, "GET", "/api/public/account", nil, &out); err != nil {
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

func (m *model) loginCmd() tea.Cmd {
	prog, base := m.prog, m.baseURL
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		creds, err := auth.Login(ctx, base, false,
			func(u string) {
				if prog != nil {
					prog.Send(printMsg(stMuted.Render("If your browser didn't open, visit:\n") + u))
				}
			},
			func() (string, error) { return "", nil },
		)
		if err != nil {
			return loginMsg{err: err}
		}
		_ = config.SaveCredentials(creds)
		return loginMsg{}
	}
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

func validDur(n int) bool { return n == 15 || n == 30 || n == 60 || n == 120 || n == 180 }

func isTerminal(s string) bool {
	switch s {
	case "completed", "failed", "render_failed", "avatar_render_failed", "cancelled":
		return true
	}
	return false
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
