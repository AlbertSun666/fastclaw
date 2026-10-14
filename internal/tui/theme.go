package tui

import "github.com/charmbracelet/lipgloss"

// Adaptive colors keep the TUI readable on both light and dark
// terminal backgrounds (lipgloss picks per the detected background).
var (
	colText    = lipgloss.AdaptiveColor{Light: "#24292F", Dark: "#E2E6ED"}
	colPrimary = lipgloss.AdaptiveColor{Light: "#B8860B", Dark: "#D4A574"} // warm amber
	colAccent  = lipgloss.AdaptiveColor{Light: "#2B5FB0", Dark: "#7AA2F7"} // blue
	colSuccess = lipgloss.AdaptiveColor{Light: "#3A7D22", Dark: "#9ECE6A"}
	colError   = lipgloss.AdaptiveColor{Light: "#C4275A", Dark: "#F7768E"}
	colMuted   = lipgloss.AdaptiveColor{Light: "#626873", Dark: "#929AAB"}
	colDim     = lipgloss.AdaptiveColor{Light: "#737985", Dark: "#747D8C"}

	styleText    = lipgloss.NewStyle().Foreground(colText)
	stylePrimary = lipgloss.NewStyle().Foreground(colPrimary)
	styleAccent  = lipgloss.NewStyle().Foreground(colAccent)
	styleSuccess = lipgloss.NewStyle().Foreground(colSuccess)
	styleError   = lipgloss.NewStyle().Foreground(colError).Bold(true)
	styleMuted   = lipgloss.NewStyle().Foreground(colMuted)
	styleDim     = lipgloss.NewStyle().Foreground(colDim)

	styleUserPrompt = lipgloss.NewStyle().Foreground(colPrimary).Bold(true)
	styleToolName   = lipgloss.NewStyle().Foreground(colAccent)
	styleSystem     = lipgloss.NewStyle().Foreground(colMuted).Italic(true)

	stylePickerBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colDim).
			Padding(0, 1).
			MarginLeft(2)

	stylePickerSelected = lipgloss.NewStyle().Foreground(colPrimary).Bold(true)
)
