package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newZaiTestServer(t *testing.T) (*httptest.Server, *map[string]any) {
	t.Helper()
	captured := &map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(captured); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		if v, _ := (*captured)["stream"].(bool); !v {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	return server, captured
}

func callZaiStream(t *testing.T, req ChatRequest) map[string]any {
	t.Helper()
	server, captured := newZaiTestServer(t)
	p := NewZaiProvider("zai-test", "test-key", server.URL, "glm-5")
	p.retryConfig.Attempts = 1
	p.ChatStream(context.Background(), req, nil) //nolint:errcheck
	return *captured
}

func TestZaiModelSupportsThinking(t *testing.T) {
	p := NewZaiProvider("zai", "key", "", "")

	tests := []struct {
		model string
		want  bool
	}{
		{"glm-5.1", true},
		{"glm-5", true},
		{"glm-4.7", true},
		{"glm-4.5", false},
		{"glm-4", false},
		{"other", false},
	}

	for _, tt := range tests {
		got := p.ModelSupportsThinking(tt.model)
		if got != tt.want {
			t.Errorf("ModelSupportsThinking(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}

func TestZaiThinkingInjected_WhenEnabled(t *testing.T) {
	body := callZaiStream(t, ChatRequest{
		Model:    "glm-5",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptThinkingLevel: "high"},
	})

	thinking, _ := body["thinking"].(map[string]any)
	if thinking == nil {
		t.Fatal("thinking object missing from request body")
	}
	if thinking["type"] != "enabled" {
		t.Errorf("thinking.type = %v, want enabled", thinking["type"])
	}
}

func TestZaiThinkingDisabled_WhenOff(t *testing.T) {
	body := callZaiStream(t, ChatRequest{
		Model:    "glm-5",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptThinkingLevel: "off"},
	})

	thinking, _ := body["thinking"].(map[string]any)
	if thinking == nil {
		t.Fatal("thinking object missing from request body")
	}
	if thinking["type"] != "disabled" {
		t.Errorf("thinking.type = %v, want disabled", thinking["type"])
	}
}

func TestZaiNoThinking_WithoutLevel(t *testing.T) {
	body := callZaiStream(t, ChatRequest{
		Model:    "glm-5",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{},
	})

	if _, has := body["thinking"]; has {
		t.Error("thinking should NOT be in body without thinking_level option")
	}
}

func TestZaiNoThinking_WhenModelUnsupported(t *testing.T) {
	body := callZaiStream(t, ChatRequest{
		Model:    "glm-4",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptThinkingLevel: "high"},
	})

	if _, has := body["thinking"]; has {
		t.Error("thinking should NOT be sent for unsupported model glm-4")
	}
}

func TestZaiNoReasoningEffort(t *testing.T) {
	body := callZaiStream(t, ChatRequest{
		Model:    "glm-5",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptThinkingLevel: "medium"},
	})

	if _, has := body["reasoning_effort"]; has {
		t.Error("reasoning_effort should NOT be sent for Z.ai provider")
	}
}
