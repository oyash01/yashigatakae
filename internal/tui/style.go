// Package tui hosts the Bubble Tea models that drive the no-args
// `yashigatakae` interactive menu, the sessions browser, and the conflict
// picker invoked from `resume`.
package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorAccent = lipgloss.Color("#8b5cf6") // brand purple — matches GhostNode/yashigatakae
	colorMuted  = lipgloss.Color("#6b7280")
	colorOK     = lipgloss.Color("#10b981")
	colorWarn   = lipgloss.Color("#f59e0b")
	colorError  = lipgloss.Color("#ef4444")
	colorBg     = lipgloss.Color("#0b0b14")
	colorFg     = lipgloss.Color("#e5e7eb")

	StyleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent).
			Padding(0, 1)

	StyleHint = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	StyleOK    = lipgloss.NewStyle().Foreground(colorOK).Bold(true)
	StyleWarn  = lipgloss.NewStyle().Foreground(colorWarn).Bold(true)
	StyleError = lipgloss.NewStyle().Foreground(colorError).Bold(true)

	StyleBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Padding(1, 2)

	StyleBadgeActive  = lipgloss.NewStyle().Background(colorOK).Foreground(colorBg).Padding(0, 1).Bold(true)
	StyleBadgeArchive = lipgloss.NewStyle().Background(colorMuted).Foreground(colorFg).Padding(0, 1).Bold(true)
)
