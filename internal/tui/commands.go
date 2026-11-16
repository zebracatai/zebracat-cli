// Input handling: Enter, the API-key capture, and the slash-command table and
// dispatcher (including the signed-out gate).
package tui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zebracatai/zebracat-cli/internal/brief"
	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/config"
)

type slashCmd struct{ name, desc string }

// commands is the completion list, in the order shown. Older names (/account,
// /projects, /quit, /video, /whoami) still work.
var commands = []slashCmd{
	{"/help", "Commands and keys"},
	{"/new", "Start a new video"},
	{"/generate", "Make the planned video (asks first)"},
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
	m.in.Reset()
	m.matches = nil
	if text == "" {
		return m, nil
	}
	m.remember(text)
	echo := m.youSaid(text)

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

// saveKey persists a pasted API key, restores normal input, and verifies it
// against /account.
func (m *model) saveKey(key string) (tea.Model, tea.Cmd) {
	m.askKey = false
	m.in.Focus()
	if key == "" {
		return m, m.note("Cancelled.")
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
	m.valid = true
	m.busy = true
	return m, tea.Batch(m.note("API key saved — checking it…"), m.accountCmd())
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
		return m, tea.Sequence(tea.ClearScreen, m.say(m.welcomeView()))
	case "/login":
		if m.cl.IsAuthenticated() {
			return m, then(m.note("You're already signed in. /logout first to switch accounts."))
		}
		m.askKey = true
		m.in.Blur()
		return m, tea.Sequence(echo, m.assistant("Paste your Zebracat API key and press Enter. Create one at "+
			stKey.Render("https://studio.zebracat.ai")+" → API Keys."), m.key.Focus())
	case "/update":
		return m.runUpdate(echo)
	case "/logout":
		_ = config.ClearCredentials()
		m.cl = client.New(m.baseURL, &config.Credentials{}, "")
		if m.cl.IsAuthenticated() { // the key comes from the environment, not from a login
			return m, then(m.note("Your saved key is removed, but ZEBRACAT_API_KEY is set in your environment, so you're still signed in with it. Unset it to sign out."))
		}
		m.acct = nil
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
		return m.runAPI(echo, "GET", "/api/v1/public/videos/"+fields[1], nil)
	case "/new", "/video":
		if m.stage == stageMaking {
			return m, then(m.note("Your video is still being made. Wait for it, or press Esc to stop watching (it keeps going)."))
		}
		m.stage, m.job, m.carry = stageIdle, nil, ""
		return m, then(m.assistant("New video — what should it be about? Type an idea, paste a script or a link."))
	case "/generate":
		mm, c := m.askGenerate()
		return mm, then(c)
	case "/plan":
		if m.job == nil || (m.job.plan == nil && m.job.task == nil) || m.stage < stageReady {
			return m, then(m.note("No plan yet — describe a video first."))
		}
		return m, then(m.say(planCard(m.job, m.width)))
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
		b, err := os.ReadFile(strings.TrimSpace(strings.TrimPrefix(text, "/script")))
		if err != nil || strings.TrimSpace(string(b)) == "" {
			return m, then(m.sayErr("Couldn't read that file.", "Check the path; it must be a text file with your script."))
		}
		mm, c := m.startPlan(string(b), brief.KindScript)
		return mm, then(c)
	default:
		return m, then(m.sayErr("Unknown command "+cmd+".", "Type / to see the commands."))
	}
}

func helpText() string {
	var b strings.Builder
	b.WriteString(stTitle.Render("Talk to it") + "\n")
	b.WriteString(stMuted.Render("  Describe a video, paste a script, or paste a TikTok / YouTube / Reels link.\n"))
	b.WriteString(stMuted.Render("  Zebracat writes a plan; change anything in plain words; Ctrl+G makes it (asks first).\n\n"))
	b.WriteString(stTitle.Render("Commands") + "\n")
	for _, c := range commands {
		b.WriteString("  " + stKey.Render(fmt.Sprintf("%-10s", c.name)) + stMuted.Render(c.desc) + "\n")
	}
	b.WriteString("\n" + stTitle.Render("Keys") + "\n")
	for _, k := range [][2]string{
		{"Enter", "send"}, {"Alt+Enter", "new line (pasting a script keeps its lines)"}, {"↑ / ↓", "history"},
		{"Tab", "complete a / command"}, {"Ctrl+G", "generate the plan (asks before spending)"},
		{"Ctrl+O", "open the finished video"}, {"Esc", "stop waiting (a paid video keeps going)"},
		{"Ctrl+C", "clear the line; twice to quit"},
	} {
		b.WriteString("  " + stKey.Render(fmt.Sprintf("%-10s", k[0])) + stMuted.Render(k[1]) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
