package agent

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func boolPtr(b bool) *bool { return &b }

func TestNewTopicGuard_NilConfig(t *testing.T) {
	g := NewTopicGuard(nil, nil)
	if g != nil {
		t.Fatal("expected nil for nil config")
	}
}

func TestNewTopicGuard_Disabled(t *testing.T) {
	g := NewTopicGuard(&config.TopicGuardConfig{Enabled: boolPtr(false)}, nil)
	if g != nil {
		t.Fatal("expected nil for disabled config")
	}
}

func TestNewTopicGuard_NilEnabled(t *testing.T) {
	g := NewTopicGuard(&config.TopicGuardConfig{}, nil)
	if g != nil {
		t.Fatal("expected nil when Enabled is nil")
	}
}

func TestTopicGuard_BlockKeywordTakesPriority(t *testing.T) {
	g := NewTopicGuard(&config.TopicGuardConfig{
		Enabled:       boolPtr(true),
		AllowKeywords: []string{"python", "programming"},
		BlockKeywords: []string{"hack", "exploit"},
	}, nil)

	result := g.Check(context.Background(), "how to hack with python")
	if result.Allowed {
		t.Fatal("expected blocked — block takes priority over allow")
	}
	if result.Reason != "block_keyword" {
		t.Fatalf("expected block_keyword, got %s", result.Reason)
	}
}

func TestTopicGuard_AllowKeyword(t *testing.T) {
	g := NewTopicGuard(&config.TopicGuardConfig{
		Enabled:       boolPtr(true),
		AllowKeywords: []string{"python", "programming"},
	}, nil)

	result := g.Check(context.Background(), "how do I learn python?")
	if !result.Allowed {
		t.Fatal("expected allowed — matches allow keyword")
	}
	if result.Reason != "allow_keyword" {
		t.Fatalf("expected allow_keyword, got %s", result.Reason)
	}
}

func TestTopicGuard_NoMatch_DefaultAllow(t *testing.T) {
	g := NewTopicGuard(&config.TopicGuardConfig{
		Enabled:       boolPtr(true),
		AllowKeywords: []string{"python"},
		DefaultAction: "allow",
	}, nil)

	result := g.Check(context.Background(), "what's the weather today?")
	if !result.Allowed {
		t.Fatal("expected allowed by default")
	}
	if result.Reason != "default_allow" {
		t.Fatalf("expected default_allow, got %s", result.Reason)
	}
}

func TestTopicGuard_NoMatch_DefaultBlock(t *testing.T) {
	g := NewTopicGuard(&config.TopicGuardConfig{
		Enabled:       boolPtr(true),
		AllowKeywords: []string{"python"},
		DefaultAction: "block",
	}, nil)

	result := g.Check(context.Background(), "what's the weather today?")
	if result.Allowed {
		t.Fatal("expected blocked by default")
	}
	if result.Reason != "default_block" {
		t.Fatalf("expected default_block, got %s", result.Reason)
	}
}

func TestTopicGuard_CaseInsensitive(t *testing.T) {
	g := NewTopicGuard(&config.TopicGuardConfig{
		Enabled:       boolPtr(true),
		AllowKeywords: []string{"Python"},
		BlockKeywords: []string{"HACK"},
	}, nil)

	// Allow match — case insensitive
	result := g.Check(context.Background(), "I love PYTHON programming")
	if !result.Allowed {
		t.Fatal("expected allowed — case insensitive match")
	}

	// Block match — case insensitive
	result = g.Check(context.Background(), "how to Hack a server")
	if result.Allowed {
		t.Fatal("expected blocked — case insensitive match")
	}
}

func TestTopicGuard_CustomRejectionMessage(t *testing.T) {
	g := NewTopicGuard(&config.TopicGuardConfig{
		Enabled:         boolPtr(true),
		BlockKeywords:   []string{"hack"},
		RejectionMessage: "Custom rejection: not allowed!",
	}, nil)

	result := g.Check(context.Background(), "how to hack")
	if result.Allowed {
		t.Fatal("expected blocked")
	}
	if result.RejectionMsg != "Custom rejection: not allowed!" {
		t.Fatalf("unexpected rejection message: %s", result.RejectionMsg)
	}
}

func TestTopicGuard_EmptyKeywords(t *testing.T) {
	g := NewTopicGuard(&config.TopicGuardConfig{
		Enabled: boolPtr(true),
	}, nil)

	// No keywords at all → default allow
	result := g.Check(context.Background(), "anything goes")
	if !result.Allowed {
		t.Fatal("expected allowed — no keywords, default allow")
	}
}

func TestTopicGuard_DefaultsApplied(t *testing.T) {
	g := NewTopicGuard(&config.TopicGuardConfig{
		Enabled: boolPtr(true),
	}, nil)
	if g == nil {
		t.Fatal("expected non-nil guard")
	}
	if g.mode != "keyword" {
		t.Fatalf("expected keyword mode, got %s", g.mode)
	}
	if g.defaultAction != "allow" {
		t.Fatalf("expected allow default, got %s", g.defaultAction)
	}
	if g.llmMaxTokens != 10 {
		t.Fatalf("expected 10 max tokens, got %d", g.llmMaxTokens)
	}
	if g.llmTimeoutMs != 5000 {
		t.Fatalf("expected 5000ms timeout, got %d", g.llmTimeoutMs)
	}
}

func TestParseTopicGuardConfig_Empty(t *testing.T) {
	cfg := ParseTopicGuardConfig(nil)
	if cfg != nil {
		t.Fatal("expected nil for nil input")
	}
	cfg = ParseTopicGuardConfig([]byte{})
	if cfg != nil {
		t.Fatal("expected nil for empty input")
	}
}

func TestParseTopicGuardConfig_InvalidJSON(t *testing.T) {
	cfg := ParseTopicGuardConfig([]byte("not json"))
	if cfg != nil {
		t.Fatal("expected nil for invalid JSON")
	}
}

func TestParseTopicGuardConfig_Valid(t *testing.T) {
	cfg := ParseTopicGuardConfig([]byte(`{"enabled":true,"allow_keywords":["python"]}`))
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if !*cfg.Enabled {
		t.Fatal("expected enabled")
	}
	if len(cfg.AllowKeywords) != 1 || cfg.AllowKeywords[0] != "python" {
		t.Fatalf("unexpected allow keywords: %v", cfg.AllowKeywords)
	}
}

func TestTopicGuard_InterceptDefaults(t *testing.T) {
	g := NewTopicGuard(&config.TopicGuardConfig{
		Enabled: boolPtr(true),
	}, nil)
	if g == nil {
		t.Fatal("expected non-nil guard")
	}
	if !g.ShouldCheckBefore() {
		t.Fatal("default should check before")
	}
	if g.ShouldCheckAfter() {
		t.Fatal("default should NOT check after")
	}
}

func TestTopicGuard_InterceptAfter(t *testing.T) {
	g := NewTopicGuard(&config.TopicGuardConfig{
		Enabled:   boolPtr(true),
		Intercept: "after",
	}, nil)
	if g.ShouldCheckBefore() {
		t.Fatal("after mode should NOT check before")
	}
	if !g.ShouldCheckAfter() {
		t.Fatal("after mode should check after")
	}
}

func TestTopicGuard_InterceptBoth(t *testing.T) {
	g := NewTopicGuard(&config.TopicGuardConfig{
		Enabled:   boolPtr(true),
		Intercept: "both",
	}, nil)
	if !g.ShouldCheckBefore() {
		t.Fatal("both mode should check before")
	}
	if !g.ShouldCheckAfter() {
		t.Fatal("both mode should check after")
	}
}

func TestTopicGuard_CheckResponse_BlockKeyword(t *testing.T) {
	g := NewTopicGuard(&config.TopicGuardConfig{
		Enabled:       boolPtr(true),
		BlockKeywords: []string{"hack", "exploit"},
	}, nil)

	result := g.CheckResponse(context.Background(), "Here is how to hack a server")
	if result.Allowed {
		t.Fatal("expected response to be blocked by blocklist keyword")
	}
	if result.Reason != "block_keyword_response" {
		t.Fatalf("expected block_keyword_response, got %s", result.Reason)
	}
}

func TestTopicGuard_CheckResponse_AllowKeywordIgnored(t *testing.T) {
	g := NewTopicGuard(&config.TopicGuardConfig{
		Enabled:       boolPtr(true),
		AllowKeywords: []string{"python"},
		BlockKeywords: []string{"hack"},
	}, nil)

	// Response with allow keyword but no block keyword should pass
	result := g.CheckResponse(context.Background(), "Python is a great language for programming")
	if !result.Allowed {
		t.Fatal("expected response to pass — allow keywords don't gate responses")
	}
}

func TestTopicGuard_CheckResponse_NoBlockMatch(t *testing.T) {
	g := NewTopicGuard(&config.TopicGuardConfig{
		Enabled:       boolPtr(true),
		BlockKeywords: []string{"hack"},
	}, nil)

	result := g.CheckResponse(context.Background(), "Python is a great programming language")
	if !result.Allowed {
		t.Fatal("expected response to pass — no block keyword match")
	}
}

func TestTopicGuard_CheckResponse_CustomRejection(t *testing.T) {
	g := NewTopicGuard(&config.TopicGuardConfig{
		Enabled:          boolPtr(true),
		BlockKeywords:    []string{"hack"},
		RejectionMessage: "Custom rejection!",
	}, nil)

	result := g.CheckResponse(context.Background(), "how to hack")
	if result.Allowed {
		t.Fatal("expected blocked")
	}
	if result.RejectionMsg != "Custom rejection!" {
		t.Fatalf("unexpected rejection: %s", result.RejectionMsg)
	}
}
