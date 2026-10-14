package tui

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/fastclaw-ai/fastclaw/internal/cliclient"
)

type fakeClient struct{}

func (fakeClient) Models(context.Context, string) (cliclient.ModelCatalog, error) {
	return cliclient.ModelCatalog{Current: "claude", Options: []cliclient.ModelOption{{ID: "test/model"}}}, nil
}
func (fakeClient) SetAgentModel(context.Context, string, string) error { return nil }

func (fakeClient) Agents(context.Context) ([]cliclient.Agent, error) { return nil, nil }
func (fakeClient) Sessions(context.Context, string) ([]cliclient.Session, error) {
	return nil, nil
}
func (fakeClient) History(context.Context, string, string) ([]cliclient.HistoryMessage, error) {
	return nil, nil
}
func (fakeClient) Stream(context.Context, string, string, string, func(cliclient.Event)) error {
	return nil
}
func (fakeClient) StreamImages(context.Context, string, string, string, []string, func(cliclient.Event)) error {
	return nil
}
func (fakeClient) Steer(context.Context, string, string, string) (bool, error) { return false, nil }
func (fakeClient) RenameSession(context.Context, string, string) error         { return nil }
func (fakeClient) BaseURL() string                                             { return "http://127.0.0.1:18953" }

func newTestModel() *Model {
	m := NewModel(Options{
		Client:     fakeClient{},
		Agent:      cliclient.Agent{ID: "agt_1", Name: "Coder", Model: "claude"},
		SessionID:  "cli-test",
		WorkingDir: "/work/fastclaw",
	})
	m.width, m.height, m.ready = 100, 40, true
	return m
}

func TestStatusBarShowsAgentModelAndWorkingDirectory(t *testing.T) {
	m := newTestModel()
	m.querying = true
	m.queued = []string{"next"}
	m.pendingImages = []string{"data:image/png;base64,eA=="}

	got := plain(m.renderStatusBar())
	if !strings.Contains(got, "Coder · claude · /work/fastclaw") {
		t.Fatalf("status bar missing agent, model or cwd: %q", got)
	}
	for _, unwanted := range []string{"replying", "127.0.0.1", "cli-test", "queued", "image"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("status bar contains %q: %q", unwanted, got)
		}
	}
}

func TestTruncatePathLeftPreservesProjectName(t *testing.T) {
	got := truncatePathLeft("/Users/me/code/项目/fastclaw", 12)
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "fastclaw") {
		t.Fatalf("truncated path = %q", got)
	}
	if lipgloss.Width(got) > 12 {
		t.Fatalf("truncated path width = %d, want <= 12", lipgloss.Width(got))
	}
}

func TestCompactChatPrompts(t *testing.T) {
	user := plain(renderUserBlock("hello", 80))
	if !strings.Contains(user, "\n• hello\n") || strings.Contains(user, "\n •") {
		t.Fatalf("user prompt is not compact or does not use a bullet:\n%s", user)
	}

	composer := newInputModel()
	input := plain(composer.View())
	inputLines := strings.Split(input, "\n")
	if len(inputLines) != minInputHeight || !strings.HasPrefix(inputLines[1], "› ") {
		t.Fatalf("composer prompt = %q, want compact › prompt", input)
	}
	if strings.Contains(inputLines[0], "›") || strings.Contains(inputLines[2], "›") {
		t.Fatalf("composer repeated its prompt: %q", input)
	}
	if composer.Height() != minInputHeight {
		t.Fatalf("composer height = %d, want %d", composer.Height(), minInputHeight)
	}
}

func TestCompletionBlockHasVerticalPadding(t *testing.T) {
	completion := plain(renderCompletionBlock("✦ Done in 5s"))
	if !strings.HasPrefix(completion, "\n✦ Done in 5s\n") {
		t.Fatalf("completion block lacks top padding: %q", completion)
	}
	composer := plain(newInputModel().View())
	if !strings.Contains(completion+composer, "Done in 5s\n\n› ") {
		t.Fatalf("completion and composer lack bottom padding: %q", completion+composer)
	}
}

// plain strips SGR sequences so assertions can match rendered text; the
// markdown renderer styles word by word, splitting phrases with escapes.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plain(s string) string { return ansiRe.ReplaceAllString(s, "") }

func evt(typ string, kv ...string) cliclient.Event {
	data := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		data[kv[i]] = kv[i+1]
	}
	return cliclient.Event{Type: typ, Data: data}
}

func TestStreamEventFolding(t *testing.T) {
	m := newTestModel()
	m.querying = true

	m.handleStreamEvent(evt("content_delta", "delta", "让我看一下"))
	m.handleStreamEvent(evt("tool_call", "id", "c1", "name", "exec_command"))
	m.handleStreamEvent(evt("tool_result", "id", "c1", "result", "ok"))
	m.handleStreamEvent(evt("content_delta", "delta", "结论：没问题"))
	m.handleStreamEvent(evt("content", "content", "should be ignored, deltas streamed"))

	// delta → assistant block, tool block, then final assistant block
	if len(m.blocks) != 3 {
		t.Fatalf("blocks = %d, want 3: %#v", len(m.blocks), m.blocks)
	}
	if m.blocks[0].Kind != blockAssistant || m.blocks[0].Content != "让我看一下" {
		t.Fatalf("first block = %+v", m.blocks[0])
	}
	if m.blocks[1].Kind != blockTool || len(m.blocks[1].Tools) != 1 {
		t.Fatalf("tool block = %+v", m.blocks[1])
	}
	tool := m.blocks[1].Tools[0]
	if tool.Name != "exec_command" || !tool.Done || tool.Summary != "ok" {
		t.Fatalf("tool state = %+v", tool)
	}
	if m.blocks[2].Content != "结论：没问题" {
		t.Fatalf("final block = %+v", m.blocks[2])
	}
}

func TestStreamContentWithoutDeltas(t *testing.T) {
	m := newTestModel()
	m.querying = true
	m.handleStreamEvent(evt("content", "content", "整段返回"))
	if len(m.blocks) != 1 || m.blocks[0].Content != "整段返回" {
		t.Fatalf("blocks = %#v", m.blocks)
	}
}

func TestConsecutiveToolCallsShareBlock(t *testing.T) {
	m := newTestModel()
	m.querying = true
	m.handleStreamEvent(evt("tool_call", "id", "c1", "name", "read_file"))
	m.handleStreamEvent(evt("tool_call", "id", "c2", "name", "write_file"))
	if len(m.blocks) != 1 || len(m.blocks[0].Tools) != 2 {
		t.Fatalf("expected one tool block with two tools, got %#v", m.blocks)
	}
}

// A block may only reach scrollback once it can no longer change, and
// never out of order — a running tool has to hold back the text behind it.
func TestSyncCommitsOnlyFinalBlocksInOrder(t *testing.T) {
	m := newTestModel()
	m.querying = true

	m.blocks = append(m.blocks, displayBlock{Kind: blockUser, Content: "hi"})
	m.sync()
	if m.committed != 1 {
		t.Fatalf("user block should commit immediately, committed = %d", m.committed)
	}

	m.handleStreamEvent(evt("tool_call", "id", "c1", "name", "read_file"))
	m.sync()
	if m.committed != 1 {
		t.Fatalf("unfinished tool block committed early, committed = %d", m.committed)
	}

	// Text after the pending tool must wait for it, not jump ahead.
	m.handleStreamEvent(evt("content", "content", "done reading"))
	m.sync()
	if m.committed != 1 {
		t.Fatalf("text jumped ahead of a pending tool, committed = %d", m.committed)
	}

	m.handleStreamEvent(evt("tool_result", "id", "c1", "result", "ok"))
	m.sync()
	if m.committed != len(m.blocks) {
		t.Fatalf("committed = %d, want all %d blocks", m.committed, len(m.blocks))
	}

	out := plain(strings.Join(m.pending, ""))
	if i, j := strings.Index(out, "read_file"), strings.Index(out, "done reading"); i < 0 || j < 0 || i > j {
		t.Fatalf("tool line should precede the text that followed it:\n%s", out)
	}
}

// A tool block stays open while the turn runs so consecutive calls fold
// into it; once the turn ends it must commit even if nothing follows.
func TestSyncCommitsTrailingToolBlockAfterTurn(t *testing.T) {
	m := newTestModel()
	m.querying = true
	m.handleStreamEvent(evt("tool_call", "id", "c1", "name", "read_file"))
	m.handleStreamEvent(evt("tool_result", "id", "c1", "result", "ok"))
	m.sync()
	if m.committed != 0 {
		t.Fatalf("trailing tool block committed mid-turn, committed = %d", m.committed)
	}

	m.querying = false
	m.sync()
	if m.committed != 1 {
		t.Fatalf("trailing tool block did not commit after the turn, committed = %d", m.committed)
	}
}

// renderLive draws only what is still uncommitted; committed blocks are
// the terminal's now, and redrawing them would duplicate them on screen.
func TestRenderLiveExcludesCommittedBlocks(t *testing.T) {
	m := newTestModel()
	m.blocks = append(m.blocks, displayBlock{Kind: blockUser, Content: "committed text"})
	m.sync()
	if got := m.renderLive(); got != "" {
		t.Fatalf("live region redrew committed content:\n%s", got)
	}

	m.querying = true
	m.streamed.WriteString("streaming now")
	if got := plain(m.renderLive()); !strings.Contains(got, "streaming now") {
		t.Fatalf("live region missing the streaming tail:\n%s", got)
	}
}

func TestRenderLiveIsCappedToLiveHeight(t *testing.T) {
	m := newTestModel()
	m.querying = true
	m.streamed.WriteString(strings.Repeat("line\n", 200))

	got := strings.Count(strings.TrimRight(m.renderLive(), "\n"), "\n") + 1
	if got > m.liveHeight() {
		t.Fatalf("live region = %d lines, exceeds cap %d", got, m.liveHeight())
	}
}

// drainPending must never block the event loop: the printer it feeds
// hands messages back to that same loop, so a blocking send deadlocks.
// A full buffer has to leave the lines pending for the next message.
func TestDrainPendingNeverBlocksAndRetries(t *testing.T) {
	m := newTestModel()
	m.printCh = make(chan string, 1)

	m.blocks = append(m.blocks, displayBlock{Kind: blockSystem, Content: "first"})
	m.sync()
	m.drainPending()
	if len(m.pending) != 0 {
		t.Fatalf("first block was not handed to the printer: %v", m.pending)
	}

	// Buffer is full now; the next block must stay pending, not block.
	m.blocks = append(m.blocks, displayBlock{Kind: blockSystem, Content: "second"})
	m.sync()
	m.drainPending()
	if len(m.pending) == 0 {
		t.Fatal("block was dropped instead of staying pending on a full buffer")
	}

	if got := plain(<-m.printCh); !strings.Contains(got, "first") {
		t.Fatalf("printer got %q, want the first block", got)
	}
	m.drainPending()
	if len(m.pending) != 0 {
		t.Fatalf("deferred block was not retried once the buffer drained: %v", m.pending)
	}
	if got := plain(<-m.printCh); !strings.Contains(got, "second") {
		t.Fatalf("printer got %q, want the second block", got)
	}
}

func TestMatchSlashCommands(t *testing.T) {
	if got := matchSlashCommands("/se"); len(got) != 1 || got[0].Name != "/sessions" {
		t.Fatalf("matches for /se = %#v", got)
	}
	// Alias prefix should surface the canonical command.
	if got := matchSlashCommands("/res"); len(got) != 1 || got[0].Name != "/sessions" {
		t.Fatalf("matches for /res = %#v", got)
	}
	if got := matchSlashCommands("hello"); got != nil {
		t.Fatalf("non-slash input matched %#v", got)
	}
}

func TestCanonicalSlash(t *testing.T) {
	name, args := canonicalSlash("/rename 我的会话")
	if name != "/rename" || args != "我的会话" {
		t.Fatalf("canonicalSlash = %q %q", name, args)
	}
	if name, _ := canonicalSlash("/quit"); name != "/exit" {
		t.Fatalf("alias /quit resolved to %q", name)
	}
	if name, _ := canonicalSlash("/nonsense"); name != "" {
		t.Fatalf("unknown command resolved to %q", name)
	}
}

func TestPickerFilterAndSelect(t *testing.T) {
	p := newPicker("test", []pickerItem{
		{ID: "a", Title: "修复登录问题"},
		{ID: "b", Title: "部署 sandbox"},
	}, 100)

	if chosen, done := p.Handle("s"); chosen != nil || done {
		t.Fatal("typing should not close the picker")
	}
	// "s" matches "sandbox" only (filter is case-insensitive substring).
	items := p.filtered()
	if len(items) != 1 || items[0].ID != "b" {
		t.Fatalf("filtered = %#v", items)
	}
	chosen, done := p.Handle("enter")
	if !done || chosen == nil || chosen.ID != "b" {
		t.Fatalf("enter selection = %v %v", chosen, done)
	}
}

func TestHelpTextListsCommands(t *testing.T) {
	help := helpText()
	for _, want := range []string{"/new", "/sessions", "/agents", "Shift+Enter"} {
		if !strings.Contains(help, want) {
			t.Fatalf("help text missing %q:\n%s", want, help)
		}
	}
}

func TestWelcomeRendersCompactSessionInfo(t *testing.T) {
	m := newTestModel()
	m.opts.Version = "v1.2.3"
	welcome := plain(m.renderWelcome())
	for _, want := range []string{">_ FastClaw (v1.2.3)", "/work/fastclaw", "agent: Coder"} {
		if !strings.Contains(welcome, want) {
			t.Fatalf("welcome missing %q:\n%s", want, welcome)
		}
	}
}

func TestWelcomeAndFooterFitNarrowTerminal(t *testing.T) {
	m := newTestModel()
	m.agent.Model = strings.Repeat("long-model-", 10)
	m.agent.Name = strings.Repeat("智能助手", 10)
	for _, width := range []int{20, 40, 80} {
		m.width = width
		for _, view := range []string{m.renderWelcome(), m.renderStatusBar(), m.renderFooter()} {
			for _, line := range strings.Split(view, "\n") {
				if lipgloss.Width(line) > width {
					t.Fatalf("line exceeds width %d: %q", width, line)
				}
			}
		}
	}
}

func TestEmptyWelcomeFrameFitsTerminal(t *testing.T) {
	m := newTestModel()
	for _, height := range []int{12, 24, 40} {
		m.height = height
		m.input.SetWidth(m.width)
		view := m.View()
		if got := lipgloss.Height(m.renderWelcome()) + lipgloss.Height(view); got >= height {
			t.Fatalf("welcome and input use %d rows in a %d-row terminal", got, height)
		}
		if strings.Count(plain(view), "Ask FastClaw to do anything") != 1 {
			t.Fatalf("expected a single composer: %q", plain(view))
		}
	}
}

func TestAssistantSplitMarkersInLiveAndHistory(t *testing.T) {
	m := newTestModel()
	m.querying = true
	for _, delta := range []string{"First", "<|", "split", "|>", "Second"} {
		m.handleStreamEvent(evt("content_delta", "delta", delta))
		if got := plain(m.renderLive()); strings.Contains(got, "<|") || strings.Contains(got, "split") {
			t.Fatalf("streaming marker leaked: %q", got)
		}
	}
	m.handleStreamEvent(evt("content"))
	for _, block := range []displayBlock{m.blocks[0], {Kind: blockAssistant, Content: "First\n<|split|>\nSecond"}} {
		got := plain(m.renderBlock(block))
		if strings.Contains(got, "<|split|>") || !strings.Contains(got, "First") || !strings.Contains(got, "Second") {
			t.Fatalf("invalid assistant display: %q", got)
		}
	}
	if got := renderUserBlock("<|split|>", 80); !strings.Contains(got, "<|split|>") {
		t.Fatal("user text changed")
	}
}

func TestStatusBarTracksAgentSwitchAndDefaultModel(t *testing.T) {
	m := newTestModel()
	m.agents = []cliclient.Agent{{ID: "ship", Name: "ShipAny"}}
	m.applyPickerChoice(pickerAgents, pickerItem{ID: "ship"})
	got := plain(m.renderStatusBar())
	if !strings.Contains(got, "ShipAny · default model · /work/fastclaw") || strings.Contains(got, "Coder") {
		t.Fatalf("status bar did not follow selected agent: %q", got)
	}
}
