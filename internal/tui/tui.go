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
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/update"
	"github.com/zebracatai/zebracat-cli/internal/version"
)

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
