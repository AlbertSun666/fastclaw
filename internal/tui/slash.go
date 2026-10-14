package tui

import "strings"

// slashCommand is one /command the chat input recognises.
type slashCommand struct {
	Name        string
	Aliases     []string
	Description string
}

var slashCommands = []slashCommand{
	{Name: "/model", Description: "Switch model for this agent: /model [provider/model|default]"},
	{Name: "/new", Description: "Start a new session"},
	{Name: "/sessions", Aliases: []string{"/resume"}, Description: "Browse and switch sessions"},
	{Name: "/agents", Aliases: []string{"/agent"}, Description: "Switch agent"},
	{Name: "/rename", Description: "Rename the current session: /rename <title>"},
	{Name: "/clear", Description: "Clear the screen (server session is untouched)"},
	{Name: "/web", Description: "Show the web dashboard URL"},
	{Name: "/help", Description: "Show help"},
	{Name: "/exit", Aliases: []string{"/quit"}, Description: "Quit"},
}

// matchSlashCommands returns commands whose name or alias starts with
// the given prefix (e.g. "/se").
func matchSlashCommands(prefix string) []slashCommand {
	if !strings.HasPrefix(prefix, "/") {
		return nil
	}
	var out []slashCommand
	for _, c := range slashCommands {
		if strings.HasPrefix(c.Name, prefix) {
			out = append(out, c)
			continue
		}
		for _, a := range c.Aliases {
			if strings.HasPrefix(a, prefix) {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

// canonicalSlash resolves aliases to the primary command name and
// splits off the argument portion. Returns "" when text is not a
// recognised slash command.
func canonicalSlash(text string) (name, args string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", ""
	}
	head, rest, _ := strings.Cut(text, " ")
	for _, c := range slashCommands {
		if head == c.Name {
			return c.Name, strings.TrimSpace(rest)
		}
		for _, a := range c.Aliases {
			if head == a {
				return c.Name, strings.TrimSpace(rest)
			}
		}
	}
	return "", ""
}

func helpText() string {
	var b strings.Builder
	b.WriteString("Commands:\n")
	for _, c := range slashCommands {
		name := c.Name
		if len(c.Aliases) > 0 {
			name += " (" + strings.Join(c.Aliases, ", ") + ")"
		}
		b.WriteString("  " + padRight(name, 24) + c.Description + "\n")
	}
	b.WriteString("\nKeys:\n")
	b.WriteString("  Enter          send (during a reply: steer into the current turn)\n")
	b.WriteString("  Ctrl+J         newline (or Alt+Enter; Shift+Enter where supported)\n")
	b.WriteString("  Ctrl+V         attach an image from the system clipboard\n")
	b.WriteString("  ↑ / ↓          input history\n")
	b.WriteString("  PgUp / PgDn    scroll the transcript\n")
	b.WriteString("  Esc            during a reply: detach (the server finishes and saves it)\n")
	b.WriteString("  Ctrl+L         clear the screen\n")
	b.WriteString("  Ctrl+C ×2 / Ctrl+D  quit\n")
	b.WriteString("\nStart a line with ! to run a local shell command, e.g. ! git status\n")
	return strings.TrimRight(b.String(), "\n")
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s + " "
	}
	return s + strings.Repeat(" ", n-len(s))
}
