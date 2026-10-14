package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/replytext"
	"github.com/muesli/reflow/wordwrap"
)

const (
	chatIndent             = ""
	chatContinuationIndent = "  "
)

// blockKind discriminates displayBlock rendering.
type blockKind int

const (
	blockUser blockKind = iota
	blockAssistant
	blockTool
	blockSystem
	blockError
	blockCompletion
)

// toolState tracks one tool call's lifecycle inside a turn.
type toolState struct {
	ID      string
	Name    string
	Done    bool
	IsError bool
	Summary string
	Started time.Time
}

// displayBlock is one visual unit in the conversation transcript.
type displayBlock struct {
	Kind    blockKind
	Content string
	Tools   []*toolState
}

func renderUserBlock(content string, width int) string {
	var b strings.Builder
	b.WriteString(styleDim.Render(chatIndent+strings.Repeat("─", max(min(width-2, 60), 1))) + "\n")
	b.WriteString(chatIndent + styleUserPrompt.Render("•") + " ")

	if len(content) > 4000 {
		lines := strings.Count(content, "\n")
		content = content[:2000] + fmt.Sprintf("\n… +%d lines …", lines)
	}
	wrapped := wordwrap.String(content, max(width-4, 20))
	for idx, line := range strings.Split(wrapped, "\n") {
		if idx == 0 {
			b.WriteString(line + "\n")
		} else {
			b.WriteString(chatContinuationIndent + line + "\n")
		}
	}
	return b.String()
}

func renderAssistantBlock(content string, width int) string {
	return renderAssistantText(content, width, false)
}

func renderAssistantText(content string, width int, streaming bool) string {
	content = strings.Join(replytext.Split(content, streaming), "\n\n")
	if content == "" {
		return ""
	}
	var b strings.Builder
	for _, line := range strings.Split(RenderMarkdown(content, width-2), "\n") {
		b.WriteString(chatIndent + line + "\n")
	}
	return b.String()
}

func renderSystemBlock(content string, isErr bool) string {
	style := styleSystem
	prefix := ""
	if isErr {
		style = styleError
		prefix = "✗ "
	}
	var b strings.Builder
	for _, line := range strings.Split(content, "\n") {
		b.WriteString(chatIndent + style.Render(prefix+line) + "\n")
		prefix = "  "
	}
	return b.String()
}

// renderCompletionBlock gives the turn boundary a blank row above it. The
// composer's vertically-centred first row supplies the matching row below,
// avoiding two consecutive blank rows between the status and input text.
func renderCompletionBlock(content string) string {
	return "\n" + renderSystemBlock(content, false)
}

func renderToolBlock(tools []*toolState, spinnerView string) string {
	var b strings.Builder
	for _, t := range tools {
		switch {
		case !t.Done:
			b.WriteString(fmt.Sprintf("%s%s %s %s\n", chatIndent,
				spinnerView,
				styleToolName.Render(t.Name),
				styleDim.Render(fmt.Sprintf("(%s)", formatDuration(time.Since(t.Started))))))
		case t.IsError:
			b.WriteString(fmt.Sprintf("%s%s %s", chatIndent,
				styleError.Render("✗"), styleToolName.Render(t.Name)))
			if t.Summary != "" {
				b.WriteString(" " + styleMuted.Render(t.Summary))
			}
			b.WriteString("\n")
		default:
			b.WriteString(fmt.Sprintf("%s%s %s", chatIndent,
				styleSuccess.Render("✓"), styleToolName.Render(t.Name)))
			if t.Summary != "" {
				b.WriteString(" " + styleMuted.Render(t.Summary))
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// toolResultSummary compresses a tool result into one dim line.
func toolResultSummary(result string) string {
	result = strings.Join(strings.Fields(result), " ")
	const maxLen = 96
	if len(result) > maxLen {
		return result[:maxLen-1] + "…"
	}
	return result
}

func formatDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return "0s"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

func formatRelativeTime(ts int64) string {
	if ts <= 0 {
		return ""
	}
	d := time.Since(time.UnixMilli(ts))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
