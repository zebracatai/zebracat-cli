// Theme: the Zebracat palette (brand #7c3aed), the styles every view uses, our
// zebra-cat mark and the frames of the animated "working" mark.
package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// ---- palette ------------------------------------------------------------------
// Adaptive colours read on dark and light terminals alike.
var (
	cPurple = lipgloss.Color("#7c3aed")
	cPurpLt = lipgloss.AdaptiveColor{Light: "#6d28d9", Dark: "#a78bfa"}
	cMuted  = lipgloss.AdaptiveColor{Light: "#6b6880", Dark: "#9a93b5"}
	cGreen  = lipgloss.AdaptiveColor{Light: "#15803d", Dark: "#4ade80"}
	cRed    = lipgloss.AdaptiveColor{Light: "#b91c1c", Dark: "#f87171"}
	cAmber  = lipgloss.AdaptiveColor{Light: "#a16207", Dark: "#facc15"}

	stMark    = lipgloss.NewStyle().Foreground(cPurple)
	stTitle   = lipgloss.NewStyle().Foreground(cPurpLt).Bold(true)
	stPrompt  = lipgloss.NewStyle().Foreground(cPurpLt).Bold(true)
	stMuted   = lipgloss.NewStyle().Foreground(cMuted)
	stErr     = lipgloss.NewStyle().Foreground(cRed)
	stOK      = lipgloss.NewStyle().Foreground(cGreen)
	stKey     = lipgloss.NewStyle().Foreground(cPurpLt)
	stHint    = lipgloss.NewStyle().Foreground(cMuted).Italic(true)
	stSel     = lipgloss.NewStyle().Foreground(cPurpLt).Bold(true)
	stWorking = lipgloss.NewStyle().Foreground(cAmber)
	stBox     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cPurple).Padding(0, 1)
	stInput   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cMuted).Padding(0, 1)
)

// mark is our zebra-cat: ears, a striped forehead, two eyes, a muzzle. Drawn in
// solid brand purple with the stripes cut out, so it reads on any background.
var mark = []string{
	"▗▖       ▗▖",
	"▐▙▄▄▄▄▄▄▄▟▌",
	"▐▀▀▄▀▀▀▄▀▀▌",
	"▐ ● ▀▀▀ ● ▌",
	" ▀▄▄▄▼▄▄▄▀ ",
}

func markView() string { return stMark.Render(strings.Join(mark, "\n")) }

// workFrames: a stripe that fills and empties — our own animated mark.
var workFrames = []string{"░", "▒", "▓", "█", "▓", "▒"}

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
