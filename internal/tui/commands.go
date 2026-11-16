// Input handling: Enter, the API-key capture, and the slash-command table and
// dispatcher (including the signed-out gate).
package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/zebracatai/zebracat-cli/internal/brief"
	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/config"
)

type slashCmd struct{ name, desc string }

var commands = []slashCmd{
	{"/new", "Start a new video (or just type your idea)"},
	{"/generate", "Make the planned video (asks before spending)"},
	{"/plan", "Show the current plan again"},
	{"/script", "Plan from a script file: /script <path>"},
	{"/open", "Open the finished video in your browser"},
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

// ---- input handling -------------------------------------------------------
func (m *model) handleEnter() (tea.Model, tea.Cmd) {
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
	// Bare text talks to the plan: a new idea, an answer, or a change.
	if !m.cl.IsAuthenticated() {
		return m, tea.Batch(echo, tea.Println(m.lockMsg()))
	}
	mm, cmd := m.converse(text)
	return mm, tea.Sequence(echo, cmd)
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
	case "/new", "/video":
		if m.stage == stageMaking {
			return m, tea.Batch(echo, tea.Println(stHint.Render("  Your video is still being made — wait for it, or Ctrl+C to leave (it keeps going).")))
		}
		m.stage, m.job = stageIdle, nil
		return m, tea.Batch(echo, tea.Println(stMuted.Render("  New video — what should it be about? Type an idea, paste a script or a link.")))
	case "/generate":
		mm, cmd := m.askGenerate()
		return mm, tea.Batch(echo, cmd)
	case "/plan":
		if m.job == nil || (m.job.plan == nil && m.job.task == nil) || m.stage < stageReady {
			return m, tea.Batch(echo, tea.Println(stHint.Render("  No plan yet.")))
		}
		return m, tea.Batch(echo, tea.Println(planCard(m.job, m.width)))
	case "/open":
		mm, cmd := m.openVideo()
		return mm, tea.Batch(echo, cmd)
	case "/script":
		if len(fields) < 2 {
			return m, tea.Batch(echo, tea.Println(stErr.Render("Usage: /script <path to a text file>")))
		}
		if m.stage != stageIdle && m.stage != stageDone {
			return m, tea.Batch(echo, tea.Println(stHint.Render("  Finish this video first, or /new to start over.")))
		}
		b, err := os.ReadFile(strings.TrimSpace(strings.TrimPrefix(text, "/script")))
		if err != nil || strings.TrimSpace(string(b)) == "" {
			return m, tea.Batch(echo, tea.Println(stErr.Render("✗ Couldn't read that file.")))
		}
		mm, cmd := m.startPlan(string(b), brief.KindScript)
		return mm, tea.Sequence(echo, cmd)
	default:
		return m, tea.Batch(echo, tea.Println(stErr.Render("Unknown command ")+cmd+stMuted.Render("  — try /help")))
	}
}

func helpText() string {
	var b strings.Builder
	b.WriteString(stTitle.Render("Commands") + "\n")
	for _, c := range commands {
		b.WriteString("  " + stKey.Render(fmt.Sprintf("%-10s", c.name)) + stMuted.Render(c.desc) + "\n")
	}
	b.WriteString("\n" + stMuted.Render("Just type what you want — e.g. ") + stKey.Render("\"a 30s ad for my coffee brand\"") +
		stMuted.Render(", or paste a TikTok/YouTube/Reel link.") + "\n")
	b.WriteString(stMuted.Render("Keys: ") + stKey.Render("Ctrl+G") + stMuted.Render(" generate · ") + stKey.Render("Ctrl+O") +
		stMuted.Render(" open · ") + stKey.Render("Ctrl+C") + stMuted.Render(" leave (a running video keeps going)"))
	return b.String()
}
