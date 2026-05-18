package providers

import (
	"fmt"
	"net/http"
)

// ZaiAdapter implements ProviderAdapter for Z.ai GLM models.
// Thin wrapper around OpenAIAdapter — same wire format, different capabilities.
type ZaiAdapter struct {
	inner *OpenAIAdapter
	caps  ProviderCapabilities
}

// NewZaiAdapter creates a Z.ai adapter from ProviderConfig.
func NewZaiAdapter(cfg ProviderConfig) (ProviderAdapter, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = zaiDefaultBase
	}
	if cfg.Model == "" {
		cfg.Model = zaiDefaultModel
	}
	inner, err := NewOpenAIAdapter(cfg)
	if err != nil {
		return nil, err
	}
	oa, ok := inner.(*OpenAIAdapter)
	if !ok {
		return nil, fmt.Errorf("zai adapter: unexpected inner type %T", inner)
	}
	caps := oa.Capabilities()
	caps.Thinking = true
	return &ZaiAdapter{inner: oa, caps: caps}, nil
}

func (a *ZaiAdapter) Name() string { return "zai" }

func (a *ZaiAdapter) Capabilities() ProviderCapabilities {
	return a.caps
}

func (a *ZaiAdapter) ToRequest(req ChatRequest) ([]byte, http.Header, error) {
	return a.inner.ToRequest(req)
}

func (a *ZaiAdapter) FromResponse(data []byte) (*ChatResponse, error) {
	return a.inner.FromResponse(data)
}

func (a *ZaiAdapter) FromStreamChunk(data []byte) (*StreamChunk, error) {
	return a.inner.FromStreamChunk(data)
}
