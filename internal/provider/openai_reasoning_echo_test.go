package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// DeepSeek thinking mode rejects any assistant message without
// reasoning_content. Steps with no reasoning (skipped thinking, or served
// by a non-thinking backend behind a routing model) must still carry an
// empty one.
func TestToAPIMessagesEchoesEmptyReasoning(t *testing.T) {
	call := []ToolCall{{ID: "call_1", Type: "function", Function: FunctionCall{Name: "ls", Arguments: "{}"}}}
	msgs := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Thinking: "look first", ToolCalls: call},
		{Role: "tool", ToolCallID: "call_1", Content: "a.txt"},
		{Role: "assistant", RawAssistant: json.RawMessage(`{"role":"assistant","content":"","tool_calls":[{"id":"call_2","type":"function","function":{"name":"ls","arguments":"{}"}}]}`)},
		{Role: "tool", ToolCallID: "call_2", Content: "b.txt"},
		{Role: "assistant", Content: "done"},
	}
	wire := toAPIMessages(msgs, "autojev/fast")
	for _, i := range []int{1, 3, 5} {
		var obj map[string]any
		if err := json.Unmarshal(wire[i], &obj); err != nil {
			t.Fatal(err)
		}
		if _, ok := obj["reasoning_content"]; !ok {
			t.Errorf("assistant %d missing reasoning_content: %s", i, wire[i])
		}
	}

	// Plain OpenAI-style history: no field added.
	plain := []Message{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}}
	if got := string(toAPIMessages(plain, "gpt-4o")[1]); got != `{"role":"assistant","content":"hello"}` {
		t.Errorf("non-thinking history changed: %s", got)
	}
	// DeepSeek by name, even before any reasoning was seen.
	var obj map[string]any
	json.Unmarshal(toAPIMessages(plain, "deepseek-chat")[1], &obj)
	if _, ok := obj["reasoning_content"]; !ok {
		t.Error("deepseek model should echo reasoning_content")
	}
}

// A routing model whose step lands on DeepSeek thinking mode rejects an
// assistant message without reasoning_content; the request is retried
// with it, and later calls for that model send it from the start.
func TestRetryWithReasoningEchoOnThinkingModeError(t *testing.T) {
	var calls, rejected int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req struct {
			Messages []map[string]any `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		for _, m := range req.Messages {
			if _, ok := m["reasoning_content"]; m["role"] == "assistant" && !ok {
				rejected++
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"error":{"message":"The `+"`reasoning_content`"+` in the thinking mode must be passed back to the API.","type":"invalid_request_error"}}`)
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	p := NewOpenAI("k", srv.URL)
	history := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
		{Role: "user", Content: "again"},
	}
	for i := 0; i < 2; i++ {
		resp, err := p.Chat(context.Background(), history, nil, "autojev/fast", 100, 0)
		if err != nil || resp.Content != "ok" {
			t.Fatalf("call %d: %v %+v", i, err, resp)
		}
	}
	if rejected != 1 || calls != 3 {
		t.Fatalf("rejected %d of %d requests, want 1 of 3", rejected, calls)
	}
}
