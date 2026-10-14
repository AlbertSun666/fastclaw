package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	maxInputHistory = 100
	minInputHeight  = 3
)

// inputModel wraps bubbles/textarea: multiline compose with Shift+Enter,
// prompt history on up/down, and full IME support (CJK composition is
// handled inside textarea, so we never intercept rune keys ourselves).
type inputModel struct {
	ta         textarea.Model
	history    []string
	historyIdx int
	historyTmp string
	width      int
}

func newInputModel() *inputModel {
	ta := textarea.New()
	ta.Placeholder = "Ask FastClaw to do anything"
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.MaxHeight = 10
	ta.SetHeight(1)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.BlurredStyle.Base = lipgloss.NewStyle()
	ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(colMuted)
	ta.BlurredStyle.Placeholder = ta.FocusedStyle.Placeholder
	ta.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(colText).Bold(true)
	ta.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(colDim)
	ta.Prompt = "› "
	ta.Focus()
	ta.SetWidth(80)
	return &inputModel{ta: ta, historyIdx: -1, width: 80}
}

func (i *inputModel) Focus()        { i.ta.Focus() }
func (i *inputModel) Blur()         { i.ta.Blur() }
func (i *inputModel) Value() string { return strings.TrimSpace(i.ta.Value()) }
func (i *inputModel) RawValue() string {
	return i.ta.Value()
}

func (i *inputModel) SetValue(s string) {
	i.ta.Reset()
	i.ta.InsertString(s)
	i.resize()
}

func (i *inputModel) Reset() {
	if v := i.Value(); v != "" {
		i.history = append(i.history, v)
		if len(i.history) > maxInputHistory {
			i.history = i.history[1:]
		}
	}
	i.ta.Reset()
	i.ta.SetHeight(1)
	i.historyIdx = -1
	i.historyTmp = ""
}

func (i *inputModel) SetWidth(w int) {
	i.width = w
	if w > 6 {
		i.ta.SetWidth(w - 2)
	}
}

func (i *inputModel) Height() int { return max(i.ta.Height(), minInputHeight) }

// Update handles one key. Returns submit=true when Enter was pressed on
// non-empty input.
func (i *inputModel) Update(msg tea.Msg) (submit bool, cmd tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			return i.Value() != "", nil
		case "shift+enter", "alt+enter", "ctrl+j":
			i.ta.InsertString("\n")
			i.resize()
			return false, nil
		case "up":
			if i.ta.Line() == 0 {
				i.navigateHistory(-1)
				return false, nil
			}
		case "down":
			if i.historyIdx >= 0 {
				i.navigateHistory(1)
				return false, nil
			}
		}
	}
	i.ta, cmd = i.ta.Update(msg)
	i.resize()
	return false, cmd
}

func (i *inputModel) resize() {
	lines := strings.Count(i.ta.Value(), "\n") + 1
	if lines > 10 {
		lines = 10
	}
	if lines < 1 {
		lines = 1
	}
	i.ta.SetHeight(lines)
}

func (i *inputModel) navigateHistory(direction int) {
	if len(i.history) == 0 {
		return
	}
	if direction < 0 {
		if i.historyIdx == -1 {
			i.historyTmp = i.ta.Value()
			i.historyIdx = len(i.history) - 1
		} else if i.historyIdx > 0 {
			i.historyIdx--
		}
		i.SetValue(i.history[i.historyIdx])
		return
	}
	if i.historyIdx < 0 {
		return
	}
	if i.historyIdx < len(i.history)-1 {
		i.historyIdx++
		i.SetValue(i.history[i.historyIdx])
	} else {
		i.historyIdx = -1
		i.SetValue(i.historyTmp)
	}
}

func (i *inputModel) View() string {
	view := chatIndent + i.ta.View()
	contentHeight := strings.Count(view, "\n") + 1
	padding := max(i.Height()-contentHeight, 0)
	top := padding / 2
	bottom := padding - top
	return strings.Repeat("\n", top) + view + strings.Repeat("\n", bottom)
}
