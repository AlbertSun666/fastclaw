package tui

import "github.com/charmbracelet/lipgloss"

// Keep the persistent hints to a single row, including in narrow panes.
func (m *Model) renderFooter() string {
	if m.modelBusy {
		return "  " + styleMuted.Render("Updating model selection…")
	}
	hints := "/help commands"
	if m.width >= 68 {
		hints = "/agents switch agent · /help commands · Ctrl+J newline"
	} else if m.width >= 42 {
		hints = "/agents switch agent · /help commands"
	}
	if m.querying {
		hints = "Ctrl+C interrupt · Esc detach"
	}
	return "  " + styleMuted.Render(lipgloss.NewStyle().MaxWidth(max(m.width-4, 1)).Render(hints))
}
