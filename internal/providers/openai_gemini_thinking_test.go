package providers

import (
	"testing"
)

// TestBuildRequestBody_GeminiForwardsReasoningEffort verifies that
// `OptThinkingLevel` maps to `reasoning_effort` on the wire for Gemini routes.
// Without this forwarding, Gemini 3 defaults to its highest thinking budget and
// consumes the full max_tokens allocation, leaving no tokens for tool args.
// Trace: 019d8f33-2de1-7ab2-9a32-9df92cd610dd.
func TestBuildRequestBody_GeminiForwardsReasoningEffort(t *testing.T) {
	cases := []struct {
		name       string
		level      string
		wantValue  string
		wantExists bool
	}{
		{"low_verbatim", "low", "low", true},
		{"minimal_verbatim", "minimal", "minimal", true},
		{"high_verbatim", "high", "high", true},
		{"medium_maps_to_high", "medium", "high", true},
		{"off_maps_to_low", "off", "low", true},
		{"empty_omitted", "", "", false},
		{"unknown_omitted", "garbage", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := NewOpenAIProvider("test-gemini", "key",
				"https://generativelanguage.googleapis.com/v1beta/openai", "gemini-3-flash-preview")
			req := ChatRequest{
				Messages: []Message{{Role: "user", Content: "hi"}},
				Options:  map[string]any{OptThinkingLevel: tc.level},
			}
			body := p.buildRequestBody("gemini-3-flash-preview", req, false)
			got, exists := body[OptReasoningEffort]
			if exists != tc.wantExists {
				t.Fatalf("level=%q: reasoning_effort presence = %v, want %v (body=%v)", tc.level, exists, tc.wantExists, body)
			}
			if exists {
				if str, ok := got.(string); !ok || str != tc.wantValue {
					t.Fatalf("level=%q: reasoning_effort = %v, want %q", tc.level, got, tc.wantValue)
				}
			}
		})
	}
}

// TestBuildRequestBody_OpenRouterReasoning verifies that OpenRouter uses the
// unified "reasoning" object (not the deprecated "reasoning_effort" string)
// for all model families, and that effort="off" sends nothing.
func TestBuildRequestBody_OpenRouterReasoning(t *testing.T) {
	t.Run("openrouter_gemini_sends_reasoning_object", func(t *testing.T) {
		p := NewOpenAIProvider("openrouter", "key",
			"https://openrouter.ai/api/v1", "google/gemini-2.5-pro")
		req := ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
			Options:  map[string]any{OptThinkingLevel: "low"},
		}
		body := p.buildRequestBody("google/gemini-2.5-pro", req, false)
		// Must use unified "reasoning" object, not deprecated "reasoning_effort"
		if _, exists := body[OptReasoningEffort]; exists {
			t.Fatalf("openrouter must NOT send deprecated reasoning_effort; body=%v", body)
		}
		reasoning, ok := body["reasoning"].(map[string]any)
		if !ok {
			t.Fatalf("openrouter must send reasoning object; body=%v", body)
		}
		if reasoning["effort"] != "low" {
			t.Fatalf("openrouter reasoning.effort = %v, want low", reasoning["effort"])
		}
	})

	t.Run("openrouter_claude_sends_reasoning_object", func(t *testing.T) {
		p := NewOpenAIProvider("openrouter", "key",
			"https://openrouter.ai/api/v1", "anthropic/claude-sonnet-4")
		req := ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
			Options:  map[string]any{OptThinkingLevel: "low"},
		}
		body := p.buildRequestBody("anthropic/claude-sonnet-4", req, false)
		reasoning, ok := body["reasoning"].(map[string]any)
		if !ok {
			t.Fatalf("openrouter claude must send reasoning object; body=%v", body)
		}
		if reasoning["effort"] != "low" {
			t.Fatalf("openrouter claude reasoning.effort = %v, want low", reasoning["effort"])
		}
		if _, exists := body[OptReasoningEffort]; exists {
			t.Fatalf("openrouter claude must NOT send deprecated reasoning_effort; body=%v", body)
		}
	})

	t.Run("openrouter_deepseek_sends_reasoning_object", func(t *testing.T) {
		p := NewOpenAIProvider("openrouter", "key",
			"https://openrouter.ai/api/v1", "deepseek/deepseek-r1")
		req := ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
			Options:  map[string]any{OptThinkingLevel: "high"},
		}
		body := p.buildRequestBody("deepseek/deepseek-r1", req, false)
		reasoning, ok := body["reasoning"].(map[string]any)
		if !ok {
			t.Fatalf("openrouter deepseek must send reasoning object; body=%v", body)
		}
		if reasoning["effort"] != "high" {
			t.Fatalf("openrouter deepseek reasoning.effort = %v, want high", reasoning["effort"])
		}
	})

	t.Run("openrouter_o3_sends_reasoning_object", func(t *testing.T) {
		p := NewOpenAIProvider("openrouter", "key",
			"https://openrouter.ai/api/v1", "openai/o3")
		req := ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
			Options:  map[string]any{OptThinkingLevel: "medium"},
		}
		body := p.buildRequestBody("openai/o3", req, false)
		// Must use unified "reasoning" object, not deprecated "reasoning_effort"
		if _, exists := body[OptReasoningEffort]; exists {
			t.Fatalf("openrouter o3 must NOT send deprecated reasoning_effort; body=%v", body)
		}
		reasoning, ok := body["reasoning"].(map[string]any)
		if !ok {
			t.Fatalf("openrouter o3 must send reasoning object; body=%v", body)
		}
		if reasoning["effort"] != "medium" {
			t.Fatalf("openrouter o3 reasoning.effort = %v, want medium", reasoning["effort"])
		}
	})

	t.Run("openrouter_effort_off_sends_none", func(t *testing.T) {
		p := NewOpenAIProvider("openrouter", "key",
			"https://openrouter.ai/api/v1", "anthropic/claude-sonnet-4")
		req := ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
			Options:  map[string]any{OptThinkingLevel: "off"},
		}
		body := p.buildRequestBody("anthropic/claude-sonnet-4", req, false)
		if _, exists := body[OptReasoningEffort]; exists {
			t.Fatalf("openrouter effort=off must NOT send reasoning_effort; body=%v", body)
		}
		reasoning, ok := body["reasoning"].(map[string]any)
		if !ok {
			t.Fatalf("openrouter effort=off must send reasoning object with effort=none; body=%v", body)
		}
		if reasoning["effort"] != "none" {
			t.Fatalf("openrouter effort=off reasoning.effort = %v, want none", reasoning["effort"])
		}
	})
}

// TestBuildRequestBody_NonGeminiUnaffected verifies that vanilla OpenAI-compat
// hosts (Together, Groq, vLLM, etc.) are not affected by the reasoning branch —
// they should continue to reject `reasoning_effort` via the existing gate.
func TestBuildRequestBody_NonGeminiUnaffected(t *testing.T) {
	p := NewOpenAIProvider("together", "key",
		"https://api.together.xyz/v1", "Qwen/Qwen2.5-72B-Instruct-Turbo")
	req := ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptThinkingLevel: "low"},
	}
	body := p.buildRequestBody("Qwen/Qwen2.5-72B-Instruct-Turbo", req, false)
	if _, exists := body[OptReasoningEffort]; exists {
		t.Fatalf("together/qwen must NOT receive reasoning_effort; body=%v", body)
	}
	if _, exists := body["reasoning"]; exists {
		t.Fatalf("together/qwen must NOT receive reasoning object; body=%v", body)
	}
}
