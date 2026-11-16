// Package tui is the interactive Zebracat shell — run `zebracat` with no args.
//
// It's an inline REPL (no alt-screen, so your results stay in the terminal after
// you exit), themed in Zebracat purple, with a branded splash, slash commands,
// autocomplete hints, and a conversation that plans, changes and generates a
// video the way the studio's /chat does (convo.go).
//
// It is auth-aware: when you're not signed in it shows that in the header and the
// status line, and it refuses to run anything that would spend credits until you
// /login. Once signed in it greets you with your plan + balance.
package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/zebracatai/zebracat-cli/internal/client"
)

// ---- messages -------------------------------------------------------------
type apiMsg struct { // a slash command's generic API answer
	kind   string
	out    any
	err    error
	taskID string
}
type pollMsg struct {
	taskID    string
	out       map[string]any
	err       error
	untilDone bool // keep polling past plan_ready (after Generate)
}
type acctMsg struct{ a *account }
type projectsMsg struct {
	out map[string]any
	err error
}
type updMsg struct{ notice string } // the launch update check's one line, or ""
type updDoneMsg struct {            // result of an in-shell /update
	tag string
	err error
}

// account is the slice of GET /account we surface in the header + status line.
type account struct {
	email      string
	plan       string
	credits    string // remaining plan credit, pretty-printed
	apiDollars string // pay-as-you-go balance (api_dollar_balance)
}

type model struct {
	cl         *client.Client
	baseURL    string
	in         textinput.Model
	sp         spinner.Model
	busy       bool // a slash command's API call is running
	width      int
	matches    []string
	stage      stage
	job        *job
	leaveArmed bool // Ctrl+C once while a paid job runs
	acct       *account
	greeted    bool
	askKey     bool   // waiting for the user to paste an API key
	valid      bool   // the next acctMsg is validating a just-entered key
	first      string // a message to send as soon as the shell opens (`zebracat create "<idea>"`)
	clk        func() time.Time
}

func (m *model) now() time.Time {
	if m.clk != nil {
		return m.clk()
	}
	return time.Now()
}

// New builds the TUI model + program. first, when set, is sent as the first
// message (`zebracat create "<idea>"`).
func New(cl *client.Client, baseURL, first string) *tea.Program {
	ti := textinput.New()
	ti.Prompt = "🦓 › "
	ti.PromptStyle = stPrompt
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(cPurpLt)
	ti.CharLimit = 20000
	ti.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(cPurpLt)

	m := &model{cl: cl, baseURL: baseURL, in: ti, sp: sp, first: first}
	m.syncPlaceholder()
	return tea.NewProgram(m)
}

type firstMsg struct{}

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
	if m.first != "" {
		cmds = append(cmds, func() tea.Msg { return firstMsg{} })
	}
	return tea.Batch(cmds...)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	mm, cmd := m.update(msg)
	if !m.askKey {
		m.syncPlaceholder() // the input hint follows the conversation
	}
	return mm, cmd
}

func (m *model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		iw := msg.Width - 10
		if iw < 10 {
			iw = 10
		}
		m.in.Width = iw
		return m, nil

	case firstMsg:
		text := m.first
		m.first = ""
		m.in.SetValue(text)
		return m.handleEnter()

	case tea.KeyMsg:
		if m.stage == stageConfirm {
			return m.confirmKey(msg)
		}
		switch msg.Type {
		case tea.KeyCtrlC:
			return m.interrupt()
		case tea.KeyCtrlG:
			return m.askGenerate()
		case tea.KeyCtrlO:
			return m.openVideo()
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
		if msg.notice != "" {
			return m, tea.Println(stWorking.Render("↑ ") + stMuted.Render(msg.notice))
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
		return m, tea.Println(stOK.Render("✓ Updated to "+msg.tag+". ") +
			stMuted.Render("Quit and relaunch zebracat to use it."))

	case apiMsg:
		m.busy = false
		if msg.err != nil {
			return m, tea.Println(stErr.Render("✗ ") + msg.err.Error())
		}
		return m, tea.Println(msg.title())

	case createdMsg:
		return m.onCreated(msg)
	case planMsg:
		return m.onPlan(msg)
	case answeredMsg:
		if msg.err != nil {
			m.stage = stageAsking
			return m, tea.Println(stErr.Render("✗ ") + msg.err.Error())
		}
		return m, m.pollCmd(m.job.taskID, false)
	case generatedMsg:
		return m.onGenerated(msg)
	case pollMsg:
		return m.onPoll(msg)

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
