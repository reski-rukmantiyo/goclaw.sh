package providers

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestZaiAdapter_Basics(t *testing.T) {
	a, err := NewZaiAdapter(ProviderConfig{APIKey: "sk-zai"})
	if err != nil {
		t.Fatalf("NewZaiAdapter error: %v", err)
	}
	if a.Name() != "zai" {
		t.Errorf("Name() = %q, want zai", a.Name())
	}
	caps := a.Capabilities()
	if !caps.Thinking {
		t.Error("expected Thinking=true")
	}
	if !caps.Streaming {
		t.Error("expected Streaming=true")
	}
	if !caps.StreamWithTools {
		t.Error("expected StreamWithTools=true")
	}
}

func TestZaiAdapter_DefaultsApplied(t *testing.T) {
	a, err := NewZaiAdapter(ProviderConfig{APIKey: "sk-zai"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	body, _, err := a.ToRequest(ChatRequest{
		Messages: []Message{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("ToRequest: %v", err)
	}
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	model, _ := m["model"].(string)
	if model != zaiDefaultModel {
		t.Errorf("model = %q, want %q (default)", model, zaiDefaultModel)
	}
}

func TestZaiAdapter_ToRequestDelegates(t *testing.T) {
	a, _ := NewZaiAdapter(ProviderConfig{APIKey: "sk", Model: "glm-4.7"})
	body, headers, err := a.ToRequest(ChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("ToRequest: %v", err)
	}
	if !strings.HasPrefix(headers.Get("Authorization"), "Bearer sk") {
		t.Errorf("Authorization = %q, want Bearer sk...", headers.Get("Authorization"))
	}
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	if m["model"] != "glm-4.7" {
		t.Errorf("model = %v, want glm-4.7", m["model"])
	}
}

func TestZaiAdapter_FromResponseDelegates(t *testing.T) {
	a, _ := NewZaiAdapter(ProviderConfig{APIKey: "sk"})
	raw := []byte(`{
		"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}
	}`)
	resp, err := a.FromResponse(raw)
	if err != nil {
		t.Fatalf("FromResponse: %v", err)
	}
	if resp.Content != "hi" {
		t.Errorf("Content = %q, want hi", resp.Content)
	}
}

func TestZaiAdapter_FromStreamChunkDelegates(t *testing.T) {
	a, _ := NewZaiAdapter(ProviderConfig{APIKey: "sk"})

	sc, err := a.FromStreamChunk([]byte("[DONE]"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if sc == nil || !sc.Done {
		t.Errorf("want Done, got %+v", sc)
	}

	sc2, err := a.FromStreamChunk([]byte(`{"choices":[{"delta":{"content":"x"}}]}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if sc2 == nil || sc2.Content != "x" {
		t.Errorf("want Content=x, got %+v", sc2)
	}
}

var _ ProviderAdapter = (*ZaiAdapter)(nil)
