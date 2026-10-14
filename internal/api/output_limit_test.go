package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/provider"
)

func (h *v1Harness) chatReply(id string) string {
	h.t.Helper()
	code, out := h.do("POST", "/v1/chat/completions", h.keyA, map[string]any{
		"agent_id": id, "messages": []map[string]string{{"role": "user", "content": "go"}},
	}, "X-Fastclaw-Session-Key", "t:1")
	if code != http.StatusOK {
		h.t.Fatalf("chat: %d %v", code, out)
	}
	return out["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"].(string)
}

// lastCall is every message the model got on its latest call.
func (h *v1Harness) lastCall() []provider.Message {
	h.rt.prov.mu.Lock()
	defer h.rt.prov.mu.Unlock()
	return h.rt.prov.last
}

// A tool call cut off at the output limit isn't run: the model is told
// to work in smaller pieces and asked again.
func TestCutOffToolCallIsRetried(t *testing.T) {
	h := newV1Harness(t)
	id := h.createAgent(h.keyA, "Cut", nil)
	h.rt.prov.mu.Lock()
	h.rt.prov.script = []provider.Response{
		{Truncated: true, ToolCalls: []provider.ToolCall{{ID: "call_1", Type: "function", Function: provider.FunctionCall{Name: "write_file", Arguments: `{"path":"a.py","content":"print(`}}}},
		{Content: "Done."},
	}
	h.rt.prov.mu.Unlock()
	if got := h.chatReply(id); got != "Done." {
		t.Fatalf("reply = %q", got)
	}
	if sys := h.rt.prov.lastSystem(); !strings.Contains(sys, "cut off at the output limit") {
		t.Fatal("the retry doesn't tell the model its reply was cut off")
	}
	for _, m := range h.lastCall() {
		if m.Role == "tool" || len(m.ToolCalls) > 0 {
			t.Fatalf("the cut-off call stayed in the conversation: %s %+v", m.Role, m)
		}
	}
}

// When every retry is cut off too, the turn fails saying so.
func TestTurnCutOffEveryTimeIsRecorded(t *testing.T) {
	h := newV1Harness(t)
	id := h.createAgent(h.keyA, "Cut", nil)
	h.rt.prov.mu.Lock()
	h.rt.prov.script = []provider.Response{{Truncated: true}, {Truncated: true}, {Truncated: true}}
	h.rt.prov.mu.Unlock()
	code, out := h.do("POST", "/v1/chat/completions", h.keyA, map[string]any{
		"agent_id": id, "messages": []map[string]string{{"role": "user", "content": "go"}},
	})
	if code != http.StatusBadGateway || errCode(out) != "agent_error" ||
		!strings.Contains(out["error"].(map[string]any)["message"].(string), "output limit") {
		t.Fatalf("chat: %d %v", code, out)
	}
}
