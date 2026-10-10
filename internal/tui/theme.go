// Theme: the Zebracat purple palette, the lipgloss styles every view uses, and
// the splash logo.
package tui

import (
	"github.com/charmbracelet/lipgloss"
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

var stWorking = lipgloss.NewStyle().Foreground(lipgloss.Color("#eab308"))
