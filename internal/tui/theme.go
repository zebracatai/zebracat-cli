// Theme: one accent colour (the logo's #6B3BF4), dim secondary text, the real
// logo and the frames of the animated "working" mark.
package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/zebracatai/zebracat-cli/internal/logo"
)

// ---- palette ------------------------------------------------------------------
// The accent is the logo's #6B3BF4 (a lighter tint on dark backgrounds so text
// stays readable); green = affordable / done, yellow = warning, red = error, cyan
// = progress; dim grey for everything secondary. Truecolor, else the nearest
// 256 colour; plain with NO_COLOR.
var (
	cBrand  = lipgloss.Color(logo.Color)
	cAccent = lipgloss.AdaptiveColor{Light: logo.Color, Dark: "#9b7bff"}
	cMuted  = lipgloss.AdaptiveColor{Light: "#6b6b76", Dark: "#8a8a96"}
	cRule   = lipgloss.AdaptiveColor{Light: "#c4c4cc", Dark: "#4a4a55"}
	cRed    = lipgloss.AdaptiveColor{Light: "#b91c1c", Dark: "#f87171"}
	cGreen  = lipgloss.AdaptiveColor{Light: "#15803d", Dark: "#4ade80"}
	cYellow = lipgloss.AdaptiveColor{Light: "#a16207", Dark: "#facc15"}
	cCyan   = lipgloss.AdaptiveColor{Light: "#0e7490", Dark: "#67e8f9"}

	stMark    = lipgloss.NewStyle().Foreground(cBrand)
	stTitle   = lipgloss.NewStyle().Bold(true)
	stPrompt  = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	stMuted   = lipgloss.NewStyle().Foreground(cMuted)
	stRule    = lipgloss.NewStyle().Foreground(cRule)
	stErr     = lipgloss.NewStyle().Foreground(cRed)
	stOK      = lipgloss.NewStyle().Foreground(cGreen)
	stKey     = lipgloss.NewStyle().Foreground(cAccent)
	stHint    = lipgloss.NewStyle().Foreground(cMuted)
	stSel     = lipgloss.NewStyle().Foreground(cAccent)
	stWorking = lipgloss.NewStyle().Foreground(cYellow) // warnings
	stGood    = lipgloss.NewStyle().Foreground(cGreen)
	stWarn    = lipgloss.NewStyle().Foreground(cYellow)
	stProg    = lipgloss.NewStyle().Foreground(cCyan) // progress / tool lines
	stLabel   = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	stHead    = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	stBox     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).Padding(0, 1)
	stAsk     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).Padding(0, 1)
)

// bigMark is the real logo (squanchy public/logo.svg), rasterised: 17×10 cells.
func bigMark() string { return stMark.Render(strings.Join(logo.Big(), "\n")) }

// smallMark is the same shield in one row.
func smallMark() string { return stMark.Render(logo.Small()) }

// workFrames: the 1-row shield building up from its chin — our working mark.
var workFrames = []string{"▗▖", "▄▄", "▙▟", "▙▟", "▄▄", "▗▖", "  "}

// colorOff reports whether colour is turned off (https://no-color.org).
func colorOff() bool {
	_, ok := os.LookupEnv("NO_COLOR")
	return ok
}

// applyColorEnv makes NO_COLOR win even on a colour terminal.
func applyColorEnv() {
	if colorOff() {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
}
