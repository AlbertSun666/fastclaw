package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const modelAPIError = `API error 400: {"error":{"message":"The reasoning_content in the thinking mode must be passed back to the API."}}`

// A turn whose model call fails is an error for the caller, not a 200
// reply apologising for it.
func TestFailedTurnIsAnError(t *testing.T) {
	h := newV1Harness(t)
	id := h.createAgent(h.keyA, "Broken", nil)
	h.rt.prov.mu.Lock()
	h.rt.prov.err = errors.New(modelAPIError)
	h.rt.prov.mu.Unlock()

	code, out := h.do("POST", "/v1/chat/completions", h.keyA, map[string]any{
		"agent_id": id, "messages": []map[string]string{{"role": "user", "content": "go"}},
	})
	if code != http.StatusBadGateway || errCode(out) != "agent_error" {
		t.Fatalf("non-streaming: %d %v", code, out)
	}
	if msg := out["error"].(map[string]any)["message"].(string); !strings.Contains(msg, "reasoning_content") {
		t.Fatalf("error message = %q", msg)
	}

	blob, _ := json.Marshal(map[string]any{
		"agent_id": id, "stream": true,
		"messages": []map[string]string{{"role": "user", "content": "go"}},
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(blob))
	req.Header.Set("Authorization", "Bearer "+h.keyA)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	var final map[string]any
	var text strings.Builder
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: {") {
			continue
		}
		var chunk map[string]any
		_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &chunk)
		if c, _ := chunk["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["content"].(string); c != "" {
			text.WriteString(c)
		}
		if strings.Contains(line, `"finish_reason":"stop"`) {
			final = chunk
		}
	}
	e, _ := final["error"].(map[string]any)
	if e == nil || e["code"] != "agent_error" || !strings.Contains(e["message"].(string), "reasoning_content") {
		t.Fatalf("final chunk error = %v\n%s", final["error"], rec.Body.String())
	}
	if text.Len() != 0 {
		t.Fatalf("streamed an apology as the reply: %q", text.String())
	}
}
