package providers

import (
	"context"
	"log/slog"
	"maps"
)

const (
	zaiDefaultBase  = "https://api.z.ai/api/paas/v4"
	zaiDefaultModel = "glm-5"
)

// zaiThinkingModels lists Z.ai GLM models that support the "thinking" parameter.
var zaiThinkingModels = map[string]bool{
	"glm-5.1": true,
	"glm-5":   true,
	"glm-4.7": true,
}

// ZaiProvider wraps OpenAIProvider to handle Z.ai-specific thinking behaviors.
// Z.ai uses {"thinking": {"type": "enabled"|"disabled"}} in the request body.
type ZaiProvider struct {
	*OpenAIProvider
}

func NewZaiProvider(name, apiKey, apiBase, defaultModel string) *ZaiProvider {
	if apiBase == "" {
		apiBase = zaiDefaultBase
	}
	if defaultModel == "" {
		defaultModel = zaiDefaultModel
	}
	return &ZaiProvider{
		OpenAIProvider: NewOpenAIProvider(name, apiKey, apiBase, defaultModel),
	}
}

func (p *ZaiProvider) SupportsThinking() bool { return true }

func (p *ZaiProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		Streaming:        true,
		ToolCalling:      true,
		StreamWithTools:  true,
		Thinking:         true,
		Vision:           true,
		CacheControl:     false,
		MaxContextWindow: 128_000,
		TokenizerID:      "cl100k_base",
	}
}

// ModelSupportsThinking returns true only for GLM models that accept the thinking parameter.
func (p *ZaiProvider) ModelSupportsThinking(model string) bool {
	return zaiThinkingModels[p.resolveModel(model)]
}

// applyThinkingGuard ensures OptThinkingLevel is set so buildRequestBody can
// inject the Z.ai "thinking" object. For models not in the allowlist, clears
// the option to avoid sending unsupported parameters.
func (p *ZaiProvider) applyThinkingGuard(req ChatRequest) ChatRequest {
	level, ok := req.Options[OptThinkingLevel].(string)
	if !ok || level == "" {
		return req
	}

	if p.ModelSupportsThinking(req.Model) {
		// Level survives into buildRequestBody where zaiPassthrough injects the JSON.
		return req
	}

	slog.Debug("zai: model does not support thinking, clearing thinking_level",
		"model", p.resolveModel(req.Model), "requested_level", level)
	opts := make(map[string]any, len(req.Options))
	maps.Copy(opts, req.Options)
	delete(opts, OptThinkingLevel)
	req.Options = opts
	return req
}

func (p *ZaiProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	return p.OpenAIProvider.Chat(ctx, p.applyThinkingGuard(req))
}

func (p *ZaiProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	return p.OpenAIProvider.ChatStream(ctx, p.applyThinkingGuard(req), onChunk)
}
