package agent

import (
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/provider"
	"github.com/fastclaw-ai/fastclaw/internal/session"
)

func TestCutOffAtOutputLimit(t *testing.T) {
	call := []provider.ToolCall{{ID: "c", Function: provider.FunctionCall{Name: "exec"}}}
	for _, tt := range []struct {
		resp provider.Response
		want bool
	}{
		{provider.Response{Truncated: true, ToolCalls: call}, true},
		{provider.Response{Truncated: true}, true},
		{provider.Response{Truncated: true, Content: "Most of an answer"}, false},
		{provider.Response{ToolCalls: call}, false},
	} {
		if got := cutOffAtOutputLimit(&tt.resp); got != tt.want {
			t.Errorf("%+v: %v, want %v", tt.resp, got, tt.want)
		}
	}
}

// A turn that ends without an answer leaves its reason in the conversation.
func TestRecordTurnFailure(t *testing.T) {
	a := &Agent{sessions: session.NewManager(t.TempDir())}
	sess := a.sessions.Get("web", "", "s-1", "")
	a.recordTurnFailure(sess, "the model's reply was cut off")
	msgs := a.sessions.Get("web", "", "s-1", "").GetMessages()
	if len(msgs) != 1 || msgs[0].Role != "assistant" || msgs[0].Content != "the model's reply was cut off" || msgs[0].Metadata["error"] != true {
		t.Fatalf("saved = %+v", msgs)
	}
}
