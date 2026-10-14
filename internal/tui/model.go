package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/fastclaw-ai/fastclaw/internal/cliclient"
)

// Client is the slice of cliclient.Client the TUI consumes; an
// interface so tests can fake the gateway.
type Client interface {
	Models(context.Context, string) (cliclient.ModelCatalog, error)
	SetAgentModel(context.Context, string, string) error
	Agents(ctx context.Context) ([]cliclient.Agent, error)
	Sessions(ctx context.Context, agentID string) ([]cliclient.Session, error)
	History(ctx context.Context, agentID, sessionID string) ([]cliclient.HistoryMessage, error)
	Stream(ctx context.Context, agentID, sessionID, message string, on func(cliclient.Event)) error
	StreamImages(ctx context.Context, agentID, sessionID, message string, imageURLs []string, on func(cliclient.Event)) error
	Steer(ctx context.Context, agentID, sessionID, message string) (bool, error)
	RenameSession(ctx context.Context, sessionID, title string) error
	BaseURL() string
}

// Options configures a TUI run.
type Options struct {
	Client    Client
	Agent     cliclient.Agent
	Agents    []cliclient.Agent
	SessionID string
	// WorkingDir is shown in the persistent footer. Empty uses os.Getwd.
	WorkingDir string
	// LoadHistory replays the session's archived turns on startup
	// (used by --resume/--continue).
	LoadHistory bool
	Version     string
}

// Tea messages.
type streamEvtMsg struct{ ev cliclient.Event }
type streamDoneMsg struct{ err error }
type historyMsg struct {
	msgs []cliclient.HistoryMessage
	err  error
}
type sessionPickerMsg struct {
	sessions []cliclient.Session
	err      error
}
type shellResultMsg struct {
	output string
	err    error
}
type steerResultMsg struct {
	text     string
	buffered bool
	err      error
}
type clipboardImageMsg struct {
	dataURL string
	err     error
}
type tickMsg time.Time

type pickerKind int

const (
	pickerNone pickerKind = iota
	pickerSessions
	pickerAgents
	pickerModels
)

// Model is the Bubble Tea model for fastclaw chat.
type Model struct {
	opts    Options
	client  Client
	program *tea.Program

	agent     cliclient.Agent
	agents    []cliclient.Agent
	sessionID string
	// sessionTitle mirrors the picker/rename title.
	sessionTitle string
	workingDir   string

	width  int
	height int
	ready  bool

	spin  spinner.Model
	input *inputModel

	// blocks is the whole transcript, but only blocks[committed:] are
	// still ours to draw: everything before that has been printed into
	// the terminal's scrollback and can no longer be changed. pending
	// holds blocks rendered this tick, waiting for drainPending.
	blocks    []displayBlock
	committed int
	pending   []string
	// printCh serializes scrollback writes; nil in tests, which read
	// pending directly.
	printCh chan string

	// In-flight turn state.
	querying        bool
	turnStart       time.Time
	turnPending     bool
	streamCancel    context.CancelFunc
	streamed        strings.Builder
	streamedContent bool
	toolsByID       map[string]*toolState
	subagentNote    string
	queued          []string
	// pendingImages are native clipboard images attached to the compose box.
	pendingImages []string

	// Overlay picker.
	picker     *pickerModel
	pickerType pickerKind

	// Slash autocomplete.
	slashMatches []slashCommand

	modelBusy bool
	errMsg    string
	ctrlCArm  time.Time
}

// NewModel builds the chat model. Call SetProgram before Run.
func NewModel(opts Options) *Model {
	workingDir := opts.WorkingDir
	if workingDir == "" {
		workingDir, _ = os.Getwd()
	}
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = lipgloss.NewStyle().Foreground(colPrimary)
	return &Model{
		opts:       opts,
		client:     opts.Client,
		agent:      opts.Agent,
		agents:     opts.Agents,
		sessionID:  opts.SessionID,
		workingDir: compactWorkingDir(workingDir),
		spin:       sp,
		input:      newInputModel(),
		toolsByID:  make(map[string]*toolState),
	}
}

func (m *Model) SetProgram(p *tea.Program) { m.program = p }

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spin.Tick, m.tickCmd()}
	if m.opts.LoadHistory {
		cmds = append(cmds, m.loadHistoryCmd())
	}
	return tea.Batch(cmds...)
}

func (m *Model) tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) loadHistoryCmd() tea.Cmd {
	agentID, sessionID := m.agent.ID, m.sessionID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		msgs, err := m.client.History(ctx, agentID, sessionID)
		return historyMsg{msgs: msgs, err: err}
	}
}

// ─── Update ─────────────────────────────────────────────

// Update wraps the real handler so every block committed while handling
// this message is handed to the printer in order.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := m.update(msg)
	m.drainPending()
	return model, cmd
}

// drainPending hands committed blocks to the printer goroutine.
//
// It deliberately does not go through tea.Println: Bubble Tea runs each
// Update's cmd in its own goroutine, so print cmds from two consecutive
// messages race and the transcript comes out shuffled (a turn's "Done"
// line beating the reply it belongs to). One channel with one consumer
// is the only way to keep scrollback in order.
//
// The send must never block — the consumer hands messages to the event
// loop, so blocking here would deadlock it. On a full buffer the lines
// stay pending and go out with the next message; the 1s tick guarantees
// there is always one coming.
func (m *Model) drainPending() {
	if len(m.pending) == 0 || m.printCh == nil {
		return
	}
	out := strings.TrimRight(strings.Join(m.pending, ""), "\n")
	select {
	case m.printCh <- out:
		m.pending = m.pending[:0]
	default:
	}
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case modelsMsg:
		m.showModels(msg)
		return m, nil
	case modelChangedMsg:
		m.modelChanged(msg)
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)

	case clipboardImageMsg:
		if msg.err != nil {
			m.errMsg = "paste image: " + msg.err.Error()
		} else {
			m.pendingImages = append(m.pendingImages, msg.dataURL)
			m.errMsg = ""
		}
		return m, nil

	case tea.WindowSizeMsg:
		// Degenerate ptys (script/expect, some CI shells) report 0x0;
		// rendering assumes sane minimums.
		m.width, m.height = max(msg.Width, 20), max(msg.Height, 8)
		first := !m.ready
		m.ready = true
		m.input.SetWidth(m.width)
		if first {
			// The welcome screen is scrollback, not a live frame: it is
			// printed once and then scrolls away like any other block.
			m.pending = append(m.pending, m.renderWelcome())
		}
		m.sync()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		if m.querying {
			m.sync()
		}
		return m, cmd

	case tickMsg:
		if m.querying {
			m.sync()
		}
		return m, m.tickCmd()

	case historyMsg:
		if msg.err != nil {
			m.errMsg = "load history: " + msg.err.Error()
		} else {
			m.resetTranscript()
			for _, h := range msg.msgs {
				switch h.Role {
				case "user":
					m.blocks = append(m.blocks, displayBlock{Kind: blockUser, Content: h.Content})
				case "assistant":
					m.blocks = append(m.blocks, displayBlock{Kind: blockAssistant, Content: h.Content})
				}
			}
			if len(msg.msgs) > 0 {
				m.appendSystem(fmt.Sprintf("Resumed session (%d messages)", len(msg.msgs)), false)
			}
		}
		m.sync()
		return m, nil

	case sessionPickerMsg:
		if msg.err != nil {
			m.errMsg = "load sessions: " + msg.err.Error()
			m.sync()
			return m, nil
		}
		items := make([]pickerItem, 0, len(msg.sessions))
		for _, s := range msg.sessions {
			title := strings.TrimSpace(s.Title)
			if title == "" {
				title = strings.TrimSpace(s.Preview)
			}
			if title == "" {
				title = s.ID
			}
			items = append(items, pickerItem{ID: s.ID, Title: truncateANSI(title, 48), Desc: formatRelativeTime(s.UpdatedAt)})
		}
		m.picker = newPicker("Switch session", items, m.width)
		m.pickerType = pickerSessions
		m.input.Blur()
		return m, nil

	case streamEvtMsg:
		m.handleStreamEvent(msg.ev)
		m.sync()
		return m, nil

	case streamDoneMsg:
		return m.finishTurn(msg.err)

	case steerResultMsg:
		if msg.err != nil {
			m.errMsg = "steer: " + msg.err.Error()
		} else if msg.buffered {
			m.appendSystem("↪ Steered into the current turn: "+msg.text, false)
		} else {
			// No in-flight turn on the server; send it as a normal
			// turn once the local stream settles.
			m.queued = append(m.queued, msg.text)
			m.appendSystem("Queued; will send when this turn finishes: "+msg.text, false)
		}
		m.sync()
		return m, nil

	case shellResultMsg:
		out := strings.TrimRight(msg.output, "\n")
		if msg.err != nil {
			if out != "" {
				out += "\n"
			}
			out += msg.err.Error()
		}
		if out == "" {
			out = "(no output)"
		}
		m.appendSystem(out, msg.err != nil)
		m.sync()
		return m, nil
	}

	return m, nil
}

// ─── Key handling ───────────────────────────────────────

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	debugLog("key=%q querying=%v", key, m.querying)

	// Overlay picker swallows everything while open.
	if m.picker != nil {
		chosen, done := m.picker.Handle(key)
		if done {
			kind := m.pickerType
			m.picker = nil
			m.pickerType = pickerNone
			m.input.Focus()
			if chosen != nil {
				return m.applyPickerChoice(kind, *chosen)
			}
		}
		return m, nil
	}

	switch key {
	case "ctrl+v":
		return m, func() tea.Msg {
			dataURL, err := clipboardImage()
			return clipboardImageMsg{dataURL: dataURL, err: err}
		}

	case "ctrl+c":
		if m.querying {
			m.detachTurn()
			return m, nil
		}
		if time.Since(m.ctrlCArm) < 2*time.Second {
			return m, tea.Quit
		}
		m.ctrlCArm = time.Now()
		m.appendSystem("Press Ctrl+C again to quit", false)
		m.sync()
		return m, nil

	case "ctrl+d":
		if m.input.Value() == "" && len(m.pendingImages) == 0 {
			return m, tea.Quit
		}

	case "ctrl+l":
		m.resetTranscript()
		m.errMsg = ""
		return m, tea.ClearScreen

	case "esc":
		if m.querying {
			m.detachTurn()
			return m, nil
		}
		if len(m.slashMatches) > 0 {
			m.slashMatches = nil
			return m, nil
		}
		m.input.Reset()
		m.pendingImages = nil
		return m, nil

	case "tab":
		if len(m.slashMatches) > 0 {
			m.input.SetValue(m.slashMatches[0].Name + " ")
			m.slashMatches = nil
			return m, nil
		}
	}

	if key == "enter" && m.modelBusy {
		m.errMsg = "Updating model selection; please wait"
		return m, nil
	}
	submitted, cmd := m.input.Update(msg)
	if key == "enter" && len(m.pendingImages) > 0 {
		submitted = true
	}
	m.updateSlashMatches()
	if !submitted {
		return m, cmd
	}

	text := m.input.Value()
	images := append([]string(nil), m.pendingImages...)
	if text == "" && len(images) == 0 {
		return m, nil
	}
	if m.querying && len(images) > 0 {
		m.errMsg = "image attachments cannot steer an active turn; wait for it to finish or press Esc to detach"
		return m, nil
	}
	if len(images) > 0 && (strings.HasPrefix(text, "!") || strings.HasPrefix(text, "/")) {
		m.errMsg = "image attachments cannot be used with local commands; press Esc to remove them"
		return m, nil
	}
	m.input.Reset()
	m.pendingImages = nil
	m.slashMatches = nil

	// Local shell escape.
	if strings.HasPrefix(text, "!") {
		shellCmd := strings.TrimSpace(strings.TrimPrefix(text, "!"))
		if shellCmd == "" {
			return m, nil
		}
		m.appendSystem("$ "+shellCmd, false)
		m.sync()
		return m, func() tea.Msg {
			out, err := exec.Command("bash", "-lc", shellCmd).CombinedOutput()
			return shellResultMsg{output: string(out), err: err}
		}
	}

	if name, args := canonicalSlash(text); name != "" {
		return m.handleSlash(name, args)
	}
	if strings.HasPrefix(text, "/") {
		m.appendSystem("Unknown command "+text+"; type /help for available commands", true)
		m.sync()
		return m, nil
	}

	if m.querying {
		// Turn in flight: steer it (Claude Code-style follow-up).
		return m, m.steerCmd(text)
	}
	return m, m.sendTurn(text, images)
}

func (m *Model) updateSlashMatches() {
	val := m.input.Value()
	if strings.HasPrefix(val, "/") && !strings.Contains(val, " ") && !strings.Contains(val, "\n") {
		m.slashMatches = matchSlashCommands(val)
	} else {
		m.slashMatches = nil
	}
}

func (m *Model) handleSlash(name, args string) (tea.Model, tea.Cmd) {
	switch name {
	case "/model":
		return m.modelCommand(args)
	case "/help":
		m.appendSystem(helpText(), false)

	case "/new":
		m.sessionID = cliclient.NewSessionID()
		m.sessionTitle = ""
		m.resetTranscript()
		m.appendSystem("Started a new session", false)
		m.sync()
		return m, tea.ClearScreen

	case "/clear":
		m.resetTranscript()
		m.sync()
		return m, tea.ClearScreen

	case "/web":
		m.appendSystem("Web dashboard: "+m.client.BaseURL(), false)

	case "/rename":
		if args == "" {
			m.appendSystem("Usage: /rename <title>", true)
			break
		}
		sessionID := m.sessionID
		client := m.client
		m.sessionTitle = args
		m.appendSystem("Renamed session: "+args, false)
		m.sync()
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := client.RenameSession(ctx, sessionID, args); err != nil {
				return shellResultMsg{err: fmt.Errorf("rename: %w", err)}
			}
			return nil
		}

	case "/sessions":
		agentID := m.agent.ID
		client := m.client
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			sessions, err := client.Sessions(ctx, agentID)
			return sessionPickerMsg{sessions: sessions, err: err}
		}

	case "/agents":
		items := make([]pickerItem, 0, len(m.agents))
		for _, a := range m.agents {
			desc := a.Model
			if a.ID == m.agent.ID {
				desc += " (current)"
			}
			items = append(items, pickerItem{ID: a.ID, Title: a.Name, Desc: desc})
		}
		m.picker = newPicker("Switch agent", items, m.width)
		m.pickerType = pickerAgents
		m.input.Blur()

	case "/exit":
		return m, tea.Quit
	}
	m.sync()
	return m, nil
}

func (m *Model) applyPickerChoice(kind pickerKind, it pickerItem) (tea.Model, tea.Cmd) {
	switch kind {
	case pickerModels:
		return m, m.setModelCmd(it.ID)
	case pickerSessions:
		m.sessionID = it.ID
		m.sessionTitle = it.Title
		m.resetTranscript()
		return m, tea.Batch(tea.ClearScreen, m.loadHistoryCmd())
	case pickerAgents:
		for _, a := range m.agents {
			if a.ID == it.ID {
				m.agent = a
				break
			}
		}
		m.sessionID = cliclient.NewSessionID()
		m.sessionTitle = ""
		m.resetTranscript()
		m.appendSystem(fmt.Sprintf("Switched to %s; started a new session", m.agent.Name), false)
		m.sync()
		return m, tea.ClearScreen
	}
	return m, nil
}

// ─── Turn lifecycle ─────────────────────────────────────

func (m *Model) sendTurn(text string, images []string) tea.Cmd {
	debugLog("sendTurn %q session=%s", text, m.sessionID)
	m.querying = true
	m.turnStart = time.Now()
	m.turnPending = false
	m.errMsg = ""
	m.subagentNote = ""
	m.streamed.Reset()
	m.streamedContent = false
	m.toolsByID = make(map[string]*toolState)
	displayText := text
	if displayText == "" {
		displayText = "[image]"
	}
	if len(images) > 0 && text != "" {
		displayText = fmt.Sprintf("[image ×%d]\n%s", len(images), text)
	}
	m.blocks = append(m.blocks, displayBlock{Kind: blockUser, Content: displayText})
	m.sync()

	ctx, cancel := context.WithCancel(context.Background())
	m.streamCancel = cancel
	client, agentID, sessionID := m.client, m.agent.ID, m.sessionID
	program := m.program
	return func() tea.Msg {
		err := client.StreamImages(ctx, agentID, sessionID, text, images, func(ev cliclient.Event) {
			if program != nil {
				program.Send(streamEvtMsg{ev: ev})
			}
		})
		return streamDoneMsg{err: err}
	}
}

func (m *Model) steerCmd(text string) tea.Cmd {
	client, agentID, sessionID := m.client, m.agent.ID, m.sessionID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		buffered, err := client.Steer(ctx, agentID, sessionID, text)
		return steerResultMsg{text: text, buffered: buffered, err: err}
	}
}

// detachTurn stops watching the in-flight turn. The gateway keeps the
// agent running on its detached context and persists the reply.
func (m *Model) detachTurn() {
	if m.streamCancel != nil {
		m.streamCancel()
	}
}

func (m *Model) handleStreamEvent(ev cliclient.Event) {
	switch ev.Type {
	case "content_delta":
		if delta := ev.Str("delta"); delta != "" {
			m.streamed.WriteString(delta)
			m.streamedContent = true
		}

	case "content":
		// Full text for the segment; authoritative when no deltas
		// streamed (some providers only emit the final content).
		if !m.streamedContent {
			m.streamed.Reset()
			m.streamed.WriteString(ev.Str("content"))
		}
		m.flushStreamedText()

	case "tool_call":
		m.flushStreamedText()
		ts := &toolState{ID: ev.Str("id"), Name: ev.Str("name"), Started: time.Now()}
		if ts.Name == "" {
			return
		}
		if ts.ID != "" {
			m.toolsByID[ts.ID] = ts
		}
		if n := len(m.blocks); n > 0 && m.blocks[n-1].Kind == blockTool {
			m.blocks[n-1].Tools = append(m.blocks[n-1].Tools, ts)
		} else {
			m.blocks = append(m.blocks, displayBlock{Kind: blockTool, Tools: []*toolState{ts}})
		}

	case "tool_result":
		id := ev.Str("id")
		ts := m.toolsByID[id]
		if ts == nil {
			// Result for a call we never saw (reconnect edge); show it
			// as a bare completion line.
			ts = &toolState{ID: id, Name: "tool", Started: time.Now()}
			if n := len(m.blocks); n > 0 && m.blocks[n-1].Kind == blockTool {
				m.blocks[n-1].Tools = append(m.blocks[n-1].Tools, ts)
			} else {
				m.blocks = append(m.blocks, displayBlock{Kind: blockTool, Tools: []*toolState{ts}})
			}
		}
		ts.Done = true
		ts.Summary = toolResultSummary(ev.Str("result"))
		if isErr, ok := ev.Data["isError"].(bool); ok {
			ts.IsError = isErr
		}

	case "steer":
		if text := ev.Str("message"); text != "" {
			m.appendSystem("↪ Steered: "+text, false)
		}

	case "turn_pending":
		m.turnPending = true

	case "subagent_progress":
		if text := ev.Str("text"); text != "" {
			m.subagentNote = text
		} else if name := ev.Str("name"); name != "" {
			m.subagentNote = name
		}
	}
}

// flushStreamedText moves accumulated streaming text into a permanent
// assistant block.
func (m *Model) flushStreamedText() {
	text := strings.TrimSpace(m.streamed.String())
	m.streamed.Reset()
	m.streamedContent = false
	if text != "" {
		m.blocks = append(m.blocks, displayBlock{Kind: blockAssistant, Content: text})
	}
}

func (m *Model) finishTurn(err error) (tea.Model, tea.Cmd) {
	m.flushStreamedText()
	m.querying = false
	m.turnPending = false
	m.subagentNote = ""
	if m.streamCancel != nil {
		m.streamCancel()
		m.streamCancel = nil
	}
	switch {
	case err == nil:
		m.blocks = append(m.blocks, displayBlock{
			Kind:    blockCompletion,
			Content: "✦ Done in " + formatDuration(time.Since(m.turnStart)),
		})
	case errIsCancel(err):
		m.appendSystem("Detached from this turn; the server keeps running and saves the reply (see /sessions)", false)
	default:
		m.appendSystem(err.Error(), true)
	}
	m.toolsByID = make(map[string]*toolState)
	m.input.Focus()
	m.sync()

	if len(m.queued) > 0 && err == nil {
		next := strings.Join(m.queued, "\n\n")
		m.queued = nil
		return m, m.sendTurn(next, nil)
	}
	return m, nil
}

func errIsCancel(err error) bool {
	return err != nil && strings.Contains(err.Error(), context.Canceled.Error())
}

func (m *Model) appendSystem(content string, isErr bool) {
	kind := blockSystem
	if isErr {
		kind = blockError
	}
	m.blocks = append(m.blocks, displayBlock{Kind: kind, Content: content})
}

// ─── View ───────────────────────────────────────────────

// liveHeight caps the redrawn region so the frame always fits: anything
// taller than this has to reach the user through scrollback instead.
func (m *Model) liveHeight() int {
	return max(m.height-m.input.Height()-6, 4)
}

// sync commits every block that has reached its final form to
// scrollback. Order is preserved, so a block behind an unfinished one
// waits its turn — a still-running tool pins the text after it in the
// live region until the result lands.
func (m *Model) sync() {
	if !m.ready {
		return
	}
	for ; m.committed < len(m.blocks); m.committed++ {
		blk := m.blocks[m.committed]
		if !m.blockFinal(m.committed) {
			return
		}
		m.pending = append(m.pending, m.renderBlock(blk)+"\n")
	}
}

// blockFinal reports whether a block will never change again. Only tool
// blocks are mutable: tool_result fills in each entry, and a following
// tool_call appends to the same block.
func (m *Model) blockFinal(i int) bool {
	blk := m.blocks[i]
	if blk.Kind != blockTool {
		return true
	}
	if i == len(m.blocks)-1 && m.querying {
		return false // more tool calls may still join this block
	}
	for _, t := range blk.Tools {
		if !t.Done {
			return false
		}
	}
	return true
}

func (m *Model) renderBlock(blk displayBlock) string {
	switch blk.Kind {
	case blockUser:
		return renderUserBlock(blk.Content, m.width)
	case blockAssistant:
		return renderAssistantBlock(blk.Content, m.width)
	case blockTool:
		return renderToolBlock(blk.Tools, m.spin.View())
	case blockError:
		return renderSystemBlock(blk.Content, true)
	case blockCompletion:
		return renderCompletionBlock(blk.Content)
	default:
		return renderSystemBlock(blk.Content, false)
	}
}

// renderLive draws the uncommitted tail: blocks still in flux plus the
// text streaming in right now. Trimmed to its last liveHeight lines —
// the full text reaches scrollback once the block is committed.
func (m *Model) renderLive() string {
	var b strings.Builder
	for _, blk := range m.blocks[m.committed:] {
		b.WriteString(m.renderBlock(blk))
	}
	if m.querying {
		if text := m.streamed.String(); strings.TrimSpace(text) != "" {
			b.WriteString(renderAssistantText(text, m.width, true))
		}
	}
	out := strings.TrimRight(b.String(), "\n")
	if out == "" {
		return ""
	}
	lines := strings.Split(out, "\n")
	if n := m.liveHeight(); len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n") + "\n"
}

// resetTranscript drops the transcript. Already-committed blocks live in
// the terminal's scrollback, so the caller pairs this with tea.ClearScreen.
func (m *Model) resetTranscript() {
	m.blocks = nil
	m.committed = 0
}

func (m *Model) renderWelcome() string {
	width := max(m.width-4, 1)
	title := styleAccent.Bold(true).Render(">_ ") + styleText.Bold(true).Render("FastClaw")
	if v := m.opts.Version; v != "" {
		title += styleMuted.Render(" (" + v + ")")
	}
	return "\n  " + ansi.Truncate(title, width, "…") + "\n" +
		"     " + styleMuted.Render(truncatePathLeft(m.workingDir, max(m.width-6, 1))) + "\n" +
		"  " + ansi.Truncate(styleMuted.Render("agent: ")+styleText.Render(m.agent.Name), width, "…") + "\n"
}

func (m *Model) renderActivity() string {
	elapsed := formatDuration(time.Since(m.turnStart))
	label := "Thinking…"
	for id := range m.toolsByID {
		if t := m.toolsByID[id]; t != nil && !t.Done {
			label = "Running " + t.Name + "…"
			break
		}
	}
	if m.turnPending {
		label = "Waiting for the follow-up turn…"
	}
	line := fmt.Sprintf("%s%s %s %s", chatIndent,
		m.spin.View(),
		stylePrimary.Render(label),
		styleMuted.Render("("+elapsed+" · Esc to detach)"))
	if m.subagentNote != "" {
		line += styleDim.Render("  " + truncateANSI(m.subagentNote, 48))
	}
	return line
}

func (m *Model) renderSlashSuggestions() string {
	maxShow := min(len(m.slashMatches), 6)
	var inner strings.Builder
	for i := 0; i < maxShow; i++ {
		c := m.slashMatches[i]
		inner.WriteString(styleAccent.Bold(true).Render(c.Name) + styleMuted.Render("  "+c.Description))
		if i < maxShow-1 {
			inner.WriteString("\n")
		}
	}
	return stylePickerBox.Render(inner.String()) + "\n"
}

// renderStatusBar identifies the selected agent, its configured model, and
// the working directory. An empty model means the agent inherits its default.
func (m *Model) renderStatusBar() string {
	agent := strings.TrimSpace(m.agent.Name)
	if agent == "" {
		agent = m.agent.ID
	}
	if agent == "" {
		agent = "Agent"
	}
	model := strings.TrimSpace(m.agent.Model)
	if model == "" {
		model = "default model"
	}
	width := max(m.width-4, 1)
	separator := " · "
	// Reserve space for all three fields, even in narrow terminal panes.
	available := max(width-2*lipgloss.Width(separator), 3)
	agent = ansi.Truncate(agent, max(available/3, 1), "…")
	model = ansi.Truncate(model, max((available-lipgloss.Width(agent))/2, 1), "…")
	dir := truncatePathLeft(m.workingDir, max(available-lipgloss.Width(agent)-lipgloss.Width(model), 1))
	return "  " + styleText.Bold(true).Render(agent) + styleMuted.Render(separator) +
		stylePrimary.Render(model) + styleMuted.Render(separator) + styleText.Render(dir)
}

func compactWorkingDir(dir string) string {
	if dir == "" {
		return "."
	}
	home, err := os.UserHomeDir()
	if err == nil && (dir == home || strings.HasPrefix(dir, home+string(filepath.Separator))) {
		return "~" + strings.TrimPrefix(dir, home)
	}
	return dir
}

func truncatePathLeft(path string, width int) string {
	if lipgloss.Width(path) <= width {
		return path
	}
	if width <= 1 {
		return "…"
	}
	runes := []rune(path)
	start, used := len(runes), 1 // one cell for the ellipsis
	for start > 0 {
		runeWidth := lipgloss.Width(string(runes[start-1]))
		if used+runeWidth > width {
			break
		}
		start--
		used += runeWidth
	}
	return "…" + string(runes[start:])
}

func (m *Model) View() string {
	if !m.ready {
		return "\n  " + m.spin.View() + " Starting…\n"
	}
	var b strings.Builder
	b.WriteString(m.renderLive())

	if m.picker != nil {
		b.WriteString(m.picker.View())
		b.WriteString("\n")
	} else {
		if m.querying {
			b.WriteString(m.renderActivity())
			b.WriteString("\n")
		}
		if len(m.slashMatches) > 0 {
			b.WriteString(m.renderSlashSuggestions())
		}
		if m.errMsg != "" {
			b.WriteString(styleError.Render("  ✗ "+m.errMsg) + "\n")
		}
		b.WriteString(m.input.View())
		b.WriteString("\n")
	}
	b.WriteString(m.renderStatusBar())
	b.WriteString("\n" + m.renderFooter())
	frame := b.String()
	// Leave breathing room on the empty welcome screen, while retaining
	// native scrollback once the conversation begins.
	if len(m.blocks) == 0 && !m.querying && !m.opts.LoadHistory {
		welcomeHeight := lipgloss.Height(m.renderWelcome())
		padding := max(m.height-welcomeHeight-lipgloss.Height(frame)-1, 0)
		frame = strings.Repeat("\n", padding) + frame
	}
	return frame
}
