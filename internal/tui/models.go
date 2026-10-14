package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fastclaw-ai/fastclaw/internal/cliclient"
)

type modelsMsg struct {
	agentID string
	catalog cliclient.ModelCatalog
	err     error
}
type modelChangedMsg struct {
	agentID, model string
	err            error
}

func (m *Model) modelCommand(value string) (tea.Model, tea.Cmd) {
	if m.querying || m.modelBusy {
		m.errMsg = "Wait for the current operation to finish before switching models"
		return m, nil
	}
	m.errMsg = ""
	if value != "" {
		if value == "default" {
			value = ""
		}
		return m, m.setModelCmd(value)
	}
	m.modelBusy = true
	client, agentID := m.client, m.agent.ID
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		catalog, err := client.Models(ctx, agentID)
		return modelsMsg{agentID: agentID, catalog: catalog, err: err}
	}
}

func (m *Model) setModelCmd(model string) tea.Cmd {
	m.modelBusy = true
	client, agentID := m.client, m.agent.ID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return modelChangedMsg{agentID: agentID, model: model, err: client.SetAgentModel(ctx, agentID, model)}
	}
}

func (m *Model) showModels(msg modelsMsg) {
	m.modelBusy = false
	if msg.agentID != m.agent.ID {
		return
	}
	if msg.err != nil {
		m.errMsg = "load models: " + msg.err.Error()
		return
	}
	m.errMsg = ""
	m.agent.Model = msg.catalog.Current
	for i := range m.agents {
		if m.agents[i].ID == m.agent.ID {
			m.agents[i].Model = msg.catalog.Current
		}
	}
	items := []pickerItem{{ID: "", Title: "default model", Desc: "Inherit the default"}}
	found := m.agent.Model == ""
	for _, model := range msg.catalog.Options {
		items = append(items, pickerItem{ID: model.ID, Title: model.ID, Desc: model.Name})
		found = found || model.ID == m.agent.Model
	}
	if !found {
		m.errMsg = "Current model " + m.agent.Model + " is not in the configured model list"
	}
	for i := range items {
		if items[i].ID == m.agent.Model {
			items[i].Desc = strings.TrimSpace(items[i].Desc + " (current)")
		}
	}
	m.picker = newPicker("Model for "+m.agent.Name+" (saved to agent)", items, m.width)
	for i := range items {
		if items[i].ID == m.agent.Model {
			m.picker.cursor = i
		}
	}
	m.pickerType = pickerModels
	m.input.Blur()
}

func (m *Model) modelChanged(msg modelChangedMsg) {
	m.modelBusy = false
	if msg.err != nil {
		m.errMsg = "switch model: " + msg.err.Error()
		return
	}
	for i := range m.agents {
		if m.agents[i].ID == msg.agentID {
			m.agents[i].Model = msg.model
		}
	}
	if m.agent.ID != msg.agentID {
		return
	}
	m.agent.Model = msg.model
	label := msg.model
	if label == "" {
		label = "default model (inherited)"
	}
	m.errMsg = ""
	m.appendSystem("Model set to "+label+" for "+m.agent.Name+"; applies to the next turn", false)
	m.sync()
}
