// Input handling: Enter, the API-key capture, and the slash-command table and
// dispatcher (including the signed-out gate).
package tui

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/zebracatai/zebracat-cli/internal/brief"
	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/config"
)

type slashCmd struct{ name, desc string }

// commands is the completion list, in the order shown. Older names (/account,
// /projects, /quit, /video, /whoami) still work.
var commands = []slashCmd{
	{"/help", "Commands and keys"},
	{"/new", "Start a new video"},
	{"/generate", "Make the planned video (as choosing Generate)"},
	{"/plan", "Show the plan again"},
	{"/open", "Open the finished video"},
	{"/script", "Plan from a script file: /script <path>"},
	{"/videos", "Your recent videos"},
	{"/status", "One video: /status <task_id>"},
	{"/balance", "Your account and balance"},
	{"/voices", "Available voices"},
	{"/styles", "Visual styles"},
	{"/login", "Add your API key"},
	{"/logout", "Sign out"},
	{"/update", "Update the CLI and relaunch"},
	{"/clear", "Clear the screen"},
	{"/exit", "Leave"},
}

// openCmds are the only commands allowed while signed out. Everything else is
// gated behind /login so we never try to spend credits for an anonymous user.
var openCmds = map[string]bool{
	"/login": true, "/logout": true, "/help": true,
	"/clear": true, "/quit": true, "/exit": true, "/update": true,
}

// ---- input handling -------------------------------------------------------
func (m *model) handleEnter() (tea.Model, tea.Cmd) {
	if m.askKey { // capturing a pasted API key — never echo the secret
		key := strings.TrimSpace(m.key.Value())
		m.key.Reset()
		return m.saveKey(key)
	}
	text := strings.TrimSpace(m.in.Value())
	if text == "" && len(m.images) > 0 {
		return m, m.note("Say what the video should be — your pictures go with it.")
	}
	m.in.Reset()
	m.matches = nil
	if text == "" {
		return m, nil
	}
	m.remember(text)
	echoed := text
	if len(m.images) > 0 && !strings.HasPrefix(text, "/") {
		echoed += "  " + m.chips()
	}
	echo := m.youSaid(echoed)

	if strings.HasPrefix(text, "/") && !strings.Contains(strings.Fields(text)[0], "\n") {
		return m.handleSlash(text, echo)
	}
	// Bare text talks to the plan: a new idea, an answer, or a change.
	if !m.cl.IsAuthenticated() {
		return m, tea.Sequence(echo, m.sayErr("You're not signed in.", "Type /login to add your API key (or set ZEBRACAT_API_KEY)."))
	}
	mm, cmd := m.converse(text)
	return mm, tea.Sequence(echo, cmd)
}

// saveKey checks a pasted API key against GET /account and only then saves
// it: a key that fails leaves the stored key (and this session's) as they were.
func (m *model) saveKey(key string) (tea.Model, tea.Cmd) {
	m.askKey = false
	m.in.Focus()
	if key == "" {
		return m, m.note("Cancelled.")
	}
	m.busy = true
	cl := client.New(m.baseURL, &config.Credentials{}, key) // the pasted key alone, even with ZEBRACAT_API_KEY set
	return m, tea.Batch(m.note("Checking that key…"), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		a, err := fetchAccountErr(ctx, cl)
		return keyCheckedMsg{key: key, a: a, err: err}
	})
}

// onKeyChecked saves a key that worked and signs in with it for this session
// (it wins over ZEBRACAT_API_KEY here); a key that didn't is never saved.
func (m *model) onKeyChecked(msg keyCheckedMsg) (tea.Model, tea.Cmd) {
	m.busy = false
	if msg.err != nil || msg.a == nil {
		kept := "Nothing was saved."
		if m.cl.IsAuthenticated() {
			kept = "Your current key is kept."
		}
		if keyRejected(msg.err) || msg.err == nil {
			return m, m.sayErr("That key didn't work. "+kept, "Check it and /login again.")
		}
		return m, m.sayErr("Couldn't check that key: "+clean(msg.err.Error())+". "+kept, "Try /login again in a moment.")
	}
	creds, _ := config.LoadCredentials()
	if creds == nil {
		creds = &config.Credentials{}
	}
	creds.APIKey = msg.key
	creds.AccessToken, creds.RefreshToken, creds.ClientID = "", "", ""
	m.cl = client.New(m.baseURL, creds, msg.key)
	m.acct, m.keyRejected = msg.a, false
	out := []tea.Cmd{m.say(stOK.Render("✓ ") + "Signed in" + stMuted.Render(m.accountLine()))}
	if err := config.SaveCredentials(creds); err != nil {
		out = append(out, m.sayErr("The key works but couldn't be saved: "+clean(err.Error()), "It is used until you leave; /login again after fixing the folder."))
	} else if os.Getenv("ZEBRACAT_API_KEY") != "" {
		out = append(out, m.note("Saved. ZEBRACAT_API_KEY is set in your environment: this session uses the new key, new shells use the environment's."))
	}
	return m, tea.Sequence(out...)
}

// keyRejected: the server refused the key itself (401 / authentication), not
// a key that may not do one thing (403).
func keyRejected(err error) bool {
	ce, ok := err.(*clierr.Error)
	return ok && ce.Exit == clierr.CodeAuth && !forbidden(ce)
}

func (m *model) handleSlash(text string, echo tea.Cmd) (tea.Model, tea.Cmd) {
	fields := strings.Fields(text)
	cmd := fields[0]

	// Gate everything that talks to the API behind sign-in.
	if !openCmds[cmd] && !m.cl.IsAuthenticated() {
		return m, tea.Sequence(echo, m.sayErr("You're not signed in.", "Type /login to add your API key."))
	}
	then := func(c tea.Cmd) tea.Cmd { return tea.Sequence(echo, c) }

	switch cmd {
	case "/help":
		return m, then(m.say(helpText()))
	case "/quit", "/exit":
		return m.leave("")
	case "/clear":
		m.blocks, m.printed = nil, 0
		return m, tea.Sequence(tea.ClearScreen, m.sayWelcome())
	case "/login": // always opens the key field: a rejected or env key is never a dead end
		ask := "Paste your Zebracat API key and press Enter. Create one at " + stKey.Render("https://studio.zebracat.ai") + " → API Keys."
		if m.cl.IsAuthenticated() && !m.keyRejected {
			ask = "Paste the API key to use instead and press Enter (Esc cancels). It replaces the saved one only if it works."
		}
		m.askKey = true
		m.in.Blur()
		return m, tea.Sequence(echo, m.assistant(ask), m.key.Focus())
	case "/update":
		return m.runUpdate(echo)
	case "/logout":
		_ = config.ClearCredentials()
		m.cl = client.New(m.baseURL, &config.Credentials{}, "")
		m.acct, m.keyRejected = nil, false
		if m.cl.IsAuthenticated() { // the key comes from the environment, not from a login
			m.busy = true
			return m, tea.Sequence(echo, m.note("Your saved key is removed, but ZEBRACAT_API_KEY is set in your environment, so this shell uses it. Unset it to sign out, or /login to use another key."), m.accountCmd())
		}
		return m, then(m.say(stOK.Render("✓ ") + "Signed out." + stMuted.Render(" /login to sign in again.")))
	case "/whoami", "/account", "/balance":
		m.busy = true
		return m, tea.Sequence(echo, m.accountCmd())
	case "/voices":
		return m.runAPI(echo, "GET", "/api/v1/public/voices", nil)
	case "/styles":
		return m.runAPI(echo, "GET", "/api/v1/public/visual_styles", nil)
	case "/projects", "/videos":
		m.busy = true
		return m, tea.Sequence(echo, m.projectsCmd())
	case "/status":
		if len(fields) < 2 {
			return m, then(m.sayErr("Which video?", "Usage: /status <task_id>"))
		}
		return m.runAPI(echo, "GET", "/api/v1/public/videos/"+url.PathEscape(fields[1]), nil)
	case "/new", "/video":
		if m.stage == stageMaking {
			return m, then(m.note("Your video is still being made. Wait for it, or press Esc to stop watching (it keeps going)."))
		}
		m.stage, m.job, m.carry = stageIdle, nil, ""
		return m, then(m.assistant("New video — what should it be about? Type an idea, paste a script or a link."))
	case "/generate":
		if m.stage != stageReady || m.job == nil {
			return m, then(m.note("There's no plan to generate yet — describe a video first."))
		}
		mm, c := m.choose(choiceGenerate)
		return mm, then(c)
	case "/plan":
		if m.job == nil || (m.job.plan == nil && m.job.task == nil) || m.stage < stageReady {
			return m, then(m.note("No plan yet — describe a video first."))
		}
		snap := *m.job
		return m, then(m.sayR(func() string { return planCard(&snap, m.width) }))
	case "/open":
		mm, c := m.openVideo()
		return mm, then(c)
	case "/script":
		if len(fields) < 2 {
			return m, then(m.sayErr("Which file?", "Usage: /script <path to a text file>"))
		}
		if m.stage != stageIdle && m.stage != stageDone {
			return m, then(m.note("Finish this video first, or /new to start over."))
		}
		b, err := os.ReadFile(homePath(strings.TrimSpace(strings.TrimPrefix(text, "/script"))))
		if err != nil || strings.TrimSpace(string(b)) == "" {
			return m, then(m.sayErr("Couldn't read that file.", "Check the path; it must be a text file with your script."))
		}
		mm, c := m.startPlan(string(b), brief.KindScript)
		return mm, then(c)
	default:
		return m, then(m.sayErr("Unknown command "+cmd+".", "Type / to see the commands."))
	}
}

// homePath expands a leading ~ to the home folder, as a shell would.
func homePath(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(h, p[1:])
}

func helpText() string {
	var b strings.Builder
	b.WriteString(stTitle.Render("Talk to it") + "\n")
	b.WriteString(stMuted.Render("  Describe a video, paste a script, or paste a TikTok / YouTube / Reels link.\n"))
	b.WriteString(stMuted.Render("  Zebracat writes a plan; change anything in plain words; choose Generate or Discard under it.\n\n"))
	b.WriteString(stTitle.Render("Commands") + "\n")
	for _, c := range commands {
		b.WriteString("  " + stKey.Render(padTo(c.name, 11)) + stMuted.Render(c.desc) + "\n")
	}
	b.WriteString("\n" + stTitle.Render("Keys") + "\n")
	keys := [][2]string{
		{"Enter", "send"}, {"Alt+Enter", "new line (pasting a script keeps its lines)"}, {"↑ / ↓", "history"},
		{"Ctrl+V", "paste a picture (Alt+V on Windows); or drag a picture file in"},
		{"Tab", "complete a / command"}, {"↑↓ Enter", "under a plan: Generate (asks y/N with the price) or Discard"},
		{"Ctrl+O", "open the finished video"}, {"Esc", "stop waiting (a paid video keeps going)"},
		{"Ctrl+C", "clear the line; twice to quit"},
	}
	col := 0
	for _, k := range keys {
		if w := ansi.StringWidth(k[0]); w > col {
			col = w
		}
	}
	for _, k := range keys { // the key column is as wide as its widest key, plus a gap
		b.WriteString("  " + stKey.Render(padTo(k[0], col+2)) + stMuted.Render(k[1]) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// padTo pads s with spaces to w terminal cells (at least one space after it).
func padTo(s string, w int) string {
	n := w - ansi.StringWidth(s)
	if n < 1 {
		n = 1
	}
	return s + strings.Repeat(" ", n)
}
