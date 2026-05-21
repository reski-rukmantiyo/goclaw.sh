package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// mockGuardProvider returns a ChatResponse with a structured tool call.
type mockGuardProvider struct {
	allowRules []string
	denyRules  []string
	decision   string
	reason     string
	err        error
}

func (m *mockGuardProvider) Chat(_ context.Context, _ providers.ChatRequest) (*providers.ChatResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &providers.ChatResponse{
		ToolCalls: []providers.ToolCall{{
			Name: "evaluate_context",
			Arguments: map[string]any{
				"matched_allow_rules": m.allowRules,
				"matched_deny_rules":  m.denyRules,
				"decision":            m.decision,
				"reason":              m.reason,
			},
		}},
	}, nil
}

func (m *mockGuardProvider) ChatStream(_ context.Context, _ providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return m.Chat(context.Background(), providers.ChatRequest{})
}
func (m *mockGuardProvider) DefaultModel() string { return "mock" }
func (m *mockGuardProvider) Name() string          { return "mock" }

func TestContextGuard_Disabled(t *testing.T) {
	g := NewContextGuard(
		&config.ContextGuardConfig{Enabled: false},
		&mockGuardProvider{},
		"",
	)
	res, _, _, err := g.Evaluate(context.Background(), "hello", nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Blocked || res.Warning {
		t.Fatalf("expected allowed when disabled")
	}
}

func TestContextGuard_NoRules(t *testing.T) {
	g := NewContextGuard(
		&config.ContextGuardConfig{Enabled: true},
		&mockGuardProvider{},
		"",
	)
	res, _, _, err := g.Evaluate(context.Background(), "hello", nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Blocked || res.Warning {
		t.Fatalf("expected allowed when no rules")
	}
}

func TestContextGuard_NoProvider(t *testing.T) {
	g := NewContextGuard(
		&config.ContextGuardConfig{Enabled: true, Rules: []config.ContextGuardRule{{Name: "x", Type: "deny"}}},
		nil,
		"",
	)
	_, _, _, err := g.Evaluate(context.Background(), "hello", nil, "")
	if err == nil {
		t.Fatalf("expected error when provider is nil")
	}
}

func TestContextGuard_ProviderError(t *testing.T) {
	g := NewContextGuard(
		&config.ContextGuardConfig{Enabled: true, Rules: []config.ContextGuardRule{{Name: "x", Type: "deny"}}},
		&mockGuardProvider{err: errors.New("boom")},
		"",
	)
	_, _, _, err := g.Evaluate(context.Background(), "hello", nil, "")
	if err == nil {
		t.Fatalf("expected error on provider failure")
	}
}

func TestContextGuard_DenyBlock(t *testing.T) {
	g := NewContextGuard(
		&config.ContextGuardConfig{
			Enabled: true,
			Rules: []config.ContextGuardRule{
				{Name: "politics", Type: "deny", Action: "block"},
			},
		},
		&mockGuardProvider{denyRules: []string{"politics"}, decision: "block", reason: "political topic"},
		"",
	)
	res, _, _, err := g.Evaluate(context.Background(), "elections?", nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Blocked {
		t.Fatalf("expected blocked")
	}
	if res.Reason != "political topic" {
		t.Fatalf("expected reason 'political topic', got %q", res.Reason)
	}
}

func TestContextGuard_DenyWarn(t *testing.T) {
	g := NewContextGuard(
		&config.ContextGuardConfig{
			Enabled: true,
			Rules: []config.ContextGuardRule{
				{Name: "politics", Type: "deny", Action: "warn"},
			},
		},
		&mockGuardProvider{denyRules: []string{"politics"}, decision: "block", reason: "political topic"},
		"",
	)
	res, _, _, err := g.Evaluate(context.Background(), "elections?", nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Blocked {
		t.Fatalf("expected not blocked for warn action")
	}
	if !res.Warning {
		t.Fatalf("expected warning")
	}
}

func TestContextGuard_AllowlistFailClosed(t *testing.T) {
	g := NewContextGuard(
		&config.ContextGuardConfig{
			Enabled: true,
			Rules: []config.ContextGuardRule{
				{Name: "coding", Type: "allow"},
			},
		},
		&mockGuardProvider{allowRules: []string{}, decision: "allow", reason: ""},
		"",
	)
	res, _, _, err := g.Evaluate(context.Background(), "cooking recipes", nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Blocked {
		t.Fatalf("expected blocked when no allow rule matched")
	}
}

func TestContextGuard_AllowlistAllowed(t *testing.T) {
	g := NewContextGuard(
		&config.ContextGuardConfig{
			Enabled: true,
			Rules: []config.ContextGuardRule{
				{Name: "coding", Type: "allow"},
			},
		},
		&mockGuardProvider{allowRules: []string{"coding"}, decision: "allow", reason: ""},
		"",
	)
	res, _, _, err := g.Evaluate(context.Background(), "write Go code", nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Blocked || res.Warning {
		t.Fatalf("expected allowed")
	}
}

func TestContextGuard_Both_DenyBlockWins(t *testing.T) {
	g := NewContextGuard(
		&config.ContextGuardConfig{
			Enabled: true,
			Rules: []config.ContextGuardRule{
				{Name: "coding", Type: "allow"},
				{Name: "politics", Type: "deny", Action: "block"},
			},
		},
		&mockGuardProvider{allowRules: []string{"coding"}, denyRules: []string{"politics"}, decision: "block", reason: "politics"},
		"",
	)
	res, _, _, err := g.Evaluate(context.Background(), "mixed", nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Blocked {
		t.Fatalf("expected blocked because deny block rule matched")
	}
}

func TestContextGuard_Both_DenyWarnWithAllow(t *testing.T) {
	g := NewContextGuard(
		&config.ContextGuardConfig{
			Enabled: true,
			Rules: []config.ContextGuardRule{
				{Name: "coding", Type: "allow"},
				{Name: "politics", Type: "deny", Action: "warn"},
			},
		},
		&mockGuardProvider{allowRules: []string{"coding"}, denyRules: []string{"politics"}, decision: "block", reason: "politics"},
		"",
	)
	res, _, _, err := g.Evaluate(context.Background(), "mixed", nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Blocked {
		t.Fatalf("expected not blocked because deny is warn")
	}
	if !res.Warning {
		t.Fatalf("expected warning")
	}
}

func TestContextGuard_Both_DenyWarnNoAllow(t *testing.T) {
	g := NewContextGuard(
		&config.ContextGuardConfig{
			Enabled: true,
			Rules: []config.ContextGuardRule{
				{Name: "coding", Type: "allow"},
				{Name: "politics", Type: "deny", Action: "warn"},
			},
		},
		&mockGuardProvider{allowRules: []string{}, denyRules: []string{"politics"}, decision: "block", reason: "politics"},
		"",
	)
	res, _, _, err := g.Evaluate(context.Background(), "mixed", nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Blocked {
		t.Fatalf("expected blocked because no allow rule matched")
	}
}

func TestContextGuard_CacheHit(t *testing.T) {
	provider := &mockGuardProvider{denyRules: []string{"politics"}, decision: "block", reason: "political topic"}
	g := NewContextGuard(
		&config.ContextGuardConfig{
			Enabled: true,
			Rules: []config.ContextGuardRule{
				{Name: "politics", Type: "deny", Action: "block"},
			},
		},
		provider,
		"",
	)

	// Prime cache.
	_, _, _, _ = g.Evaluate(context.Background(), "elections?", nil, "")

	// Second call should hit cache without calling provider again.
	// We verify by breaking the provider.
	provider.err = errors.New("should not be called")
	res, _, _, err := g.Evaluate(context.Background(), "elections?", nil, "")
	if err != nil {
		t.Fatalf("cache hit should not error: %v", err)
	}
	if !res.Blocked {
		t.Fatalf("expected blocked from cache")
	}
}

func TestContextGuard_CacheTTLExpiry(t *testing.T) {
	provider := &mockGuardProvider{denyRules: []string{"politics"}, decision: "block", reason: "political topic"}
	g := NewContextGuard(
		&config.ContextGuardConfig{
			Enabled: true,
			Rules: []config.ContextGuardRule{
				{Name: "politics", Type: "deny", Action: "block"},
			},
		},
		provider,
		"",
	)
	// Override cache TTL to 0 for instant expiry.
	g.cache.init(0, func() time.Time { return time.Now() })

	_, _, _, _ = g.Evaluate(context.Background(), "elections?", nil, "")
	provider.err = errors.New("cache expired")
	_, _, _, err := g.Evaluate(context.Background(), "elections?", nil, "")
	if err == nil {
		t.Fatalf("expected error after cache expiry")
	}
}

func TestContextGuard_ParseError_FailClosed(t *testing.T) {
	g := NewContextGuard(
		&config.ContextGuardConfig{
			Enabled: true,
			Rules: []config.ContextGuardRule{
				{Name: "politics", Type: "deny", Action: "block"},
			},
		},
		&mockGuardProvider{
			decision: "invalid",
			reason:   "bad",
		},
		"",
	)
	res, _, _, err := g.Evaluate(context.Background(), "whatever", nil, "")
	if err != nil {
		t.Fatalf("parse error should not propagate: %v", err)
	}
	if !res.Blocked {
		t.Fatalf("expected fail-closed on parse error")
	}
}

func TestContextGuard_HistoryTruncation(t *testing.T) {
	g := NewContextGuard(
		&config.ContextGuardConfig{
			Enabled:         true,
			MaxHistoryTurns: 2,
			Rules:           []config.ContextGuardRule{{Name: "coding", Type: "allow"}},
		},
		&mockGuardProvider{allowRules: []string{"coding"}, decision: "allow"},
		"",
	)
	history := []providers.Message{
		{Role: "user", Content: strings.Repeat("a", 1000)},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "write code"},
	}
	_, _, _, err := g.Evaluate(context.Background(), "write code", history, "coding agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPreviewMessage(t *testing.T) {
	if previewMessage("short", 10) != "short" {
		t.Fatalf("expected no truncation")
	}
	long := strings.Repeat("a", 300)
	if len(previewMessage(long, 200)) != 203 {
		t.Fatalf("expected truncated with ellipsis")
	}
}
