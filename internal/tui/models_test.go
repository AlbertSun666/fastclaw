package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fastclaw-ai/fastclaw/internal/cliclient"
)

func TestModelCommandPickerAndPersistence(t *testing.T) {
	m := newTestModel()
	m.agents = []cliclient.Agent{m.agent}
	session := m.sessionID
	if name, _ := canonicalSlash("/model"); name != "/model" {
		t.Fatal("missing command")
	}
	_, cmd := m.handleSlash("/model", "")
	if cmd == nil || !m.modelBusy {
		t.Fatal("no model list request")
	}
	m.Update(cmd())
	if m.picker == nil || m.pickerType != pickerModels {
		t.Fatal("model picker missing")
	}
	if m.picker.items[0].ID != "" {
		t.Fatal("inherit option missing")
	}
	m.picker = nil
	_, cmd = m.applyPickerChoice(pickerModels, pickerItem{ID: "test/model"})
	if m.agent.Model != "claude" {
		t.Fatal("updated before server success")
	}
	m.Update(cmd())
	if m.agent.Model != "test/model" || m.agents[0].Model != "test/model" || m.sessionID != session {
		t.Fatal("model switch lost session or failed to update agent")
	}
	_, cmd = m.handleSlash("/model", "default")
	m.Update(cmd())
	if m.agent.Model != "" {
		t.Fatal("default did not clear override")
	}
}

func TestModelChangeFailureAndBusyInput(t *testing.T) {
	m := newTestModel()
	m.querying = true
	if _, cmd := m.handleSlash("/model", "test/model"); cmd != nil {
		t.Fatal("changed model mid-turn")
	}
	m.querying = false
	m.modelBusy = true
	m.input.SetValue("next message")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.input.Value() != "next message" || m.querying {
		t.Fatal("sent message before model update finished")
	}
	m.Update(modelChangedMsg{agentID: m.agent.ID, model: "test/model", err: errors.New("forbidden")})
	if m.agent.Model != "claude" || !strings.Contains(m.errMsg, "forbidden") || m.modelBusy {
		t.Fatal("failed update altered model or hid error")
	}
	m.showModels(modelsMsg{agentID: "different-agent", catalog: cliclient.ModelCatalog{Options: []cliclient.ModelOption{{ID: "test/model"}}}})
	if m.picker != nil {
		t.Fatal("stale model list opened")
	}
}

func TestModelPickerRefreshesWebSelectionWithoutOfferingStaleModel(t *testing.T) {
	m := newTestModel()
	m.agents = []cliclient.Agent{m.agent}
	m.showModels(modelsMsg{agentID: m.agent.ID, catalog: cliclient.ModelCatalog{
		Current: "p/web", Options: []cliclient.ModelOption{{ID: "p/one"}, {ID: "p/web"}},
	}})
	if m.agent.Model != "p/web" || m.agents[0].Model != "p/web" || m.picker.items[m.picker.cursor].ID != "p/web" {
		t.Fatal("model selected on web was not reflected in TUI")
	}
	m.showModels(modelsMsg{agentID: m.agent.ID, catalog: cliclient.ModelCatalog{
		Current: "p/removed", Options: []cliclient.ModelOption{{ID: "p/one"}},
	}})
	for _, item := range m.picker.items {
		if item.ID == "p/removed" {
			t.Fatal("stale override offered as available model")
		}
	}
	if !strings.Contains(m.errMsg, "p/removed") {
		t.Fatal("missing stale override explanation")
	}
	m.showModels(modelsMsg{agentID: m.agent.ID, catalog: cliclient.ModelCatalog{}})
	if m.agent.Model != "" || m.picker.items[m.picker.cursor].ID != "" {
		t.Fatal("default selection was not refreshed")
	}
}
