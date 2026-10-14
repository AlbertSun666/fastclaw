package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func sseServer(t *testing.T, events []string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			fmt.Fprintf(w, "data: %s\n\n", e)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func finalChunk(t *testing.T, sr *StreamReader) StreamChunk {
	t.Helper()
	for {
		c, ok := sr.Next()
		if !ok {
			t.Fatal("stream ended without a Done chunk")
		}
		if c.Done {
			return c
		}
	}
}

// A reply that stops at the output limit says so, on both the streamed
// and the collected response; a normal stop doesn't.
func TestOpenAIReportsOutputLimit(t *testing.T) {
	for _, tt := range []struct {
		finish string
		want   bool
	}{{"length", true}, {"tool_calls", false}} {
		events := []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"write_file","arguments":"{\"content\":\"abc"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"` + tt.finish + `"}]}`,
			`[DONE]`,
		}
		p := NewOpenAI("k", sseServer(t, events).URL)
		resp, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, "m", 100, 0)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Truncated != tt.want {
			t.Errorf("Chat finish %q: Truncated = %v", tt.finish, resp.Truncated)
		}
		sr, err := p.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, "m", 100, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got := finalChunk(t, sr).Truncated; got != tt.want {
			t.Errorf("ChatStream finish %q: Truncated = %v", tt.finish, got)
		}
	}
}

func TestAnthropicReportsOutputLimit(t *testing.T) {
	for _, tt := range []struct {
		stop string
		want bool
	}{{"max_tokens", true}, {"end_turn", false}} {
		events := []string{
			`{"type":"message_start","message":{"usage":{"input_tokens":10}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"` + tt.stop + `"},"usage":{"output_tokens":100}}`,
			`{"type":"message_stop"}`,
		}
		p := NewAnthropic("k", sseServer(t, events).URL)
		resp, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, "m", 100, 0)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Truncated != tt.want {
			t.Errorf("Chat stop %q: Truncated = %v", tt.stop, resp.Truncated)
		}
		sr, err := p.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, "m", 100, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got := finalChunk(t, sr).Truncated; got != tt.want {
			t.Errorf("ChatStream stop %q: Truncated = %v", tt.stop, got)
		}
	}
}
