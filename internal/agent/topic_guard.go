package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// TopicGuardResult is returned by TopicGuard.Check.
type TopicGuardResult struct {
	Allowed      bool
	Reason       string // "allow_keyword" | "block_keyword" | "default_allow" | "default_block" | "llm_allow" | "llm_block"
	RejectionMsg string
}

// TopicGuard checks user messages against keyword allow/block lists
// with optional LLM classification fallback for unmatched messages.
type TopicGuard struct {
	allowKeywords      []string         // pre-lowercased
	blockKeywords      []string         // pre-lowercased
	allowRegexps       []*regexp.Regexp // pre-compiled word-boundary patterns
	blockRegexps       []*regexp.Regexp // pre-compiled word-boundary patterns
	mode               string           // "keyword" or "keyword_and_llm"
	defaultAction      string           // "allow" or "block"
	intercept          string           // "before", "after", or "both"
	rejectionMsg       string
	llmProvider        string
	llmModel           string
	llmMaxTokens       int
	llmTimeoutMs       int
	provider           providers.Provider
}

// ShouldCheckBefore returns true if the guard should check the user message before the LLM call.
func (g *TopicGuard) ShouldCheckBefore() bool {
	return g.intercept != "after"
}

// ShouldCheckAfter returns true if the guard should check the LLM response after generation.
func (g *TopicGuard) ShouldCheckAfter() bool {
	return g.intercept == "after" || g.intercept == "both"
}

// NewTopicGuard creates a guard from config. Returns nil if disabled.
func NewTopicGuard(cfg *config.TopicGuardConfig, provider providers.Provider) *TopicGuard {
	if cfg == nil || cfg.Enabled == nil || !*cfg.Enabled {
		return nil
	}
	mode := cfg.Mode
	if mode == "" {
		mode = "keyword"
	}
	defaultAction := cfg.DefaultAction
	if defaultAction == "" {
		defaultAction = "allow"
	}
	llmMaxTokens := cfg.LLMMaxTokens
	if llmMaxTokens <= 0 {
		llmMaxTokens = 10
	}
	llmTimeoutMs := cfg.LLMTimeoutMs
	if llmTimeoutMs <= 0 {
		llmTimeoutMs = 5000
	}
	allowKws := lowercaseAll(cfg.AllowKeywords)
	blockKws := lowercaseAll(cfg.BlockKeywords)
	g := &TopicGuard{
		allowKeywords: allowKws,
		blockKeywords: blockKws,
		allowRegexps:  compileKeywordRegexps(allowKws),
		blockRegexps:  compileKeywordRegexps(blockKws),
		mode:          mode,
		defaultAction: defaultAction,
		intercept:     normalizeIntercept(cfg.Intercept),
		rejectionMsg:  cfg.RejectionMessage,
		llmProvider:   cfg.LLMProvider,
		llmModel:      cfg.LLMModel,
		llmMaxTokens:  llmMaxTokens,
		llmTimeoutMs:  llmTimeoutMs,
		provider:      provider,
	}

	// Soft check: warn if the model looks too large for fast classification.
	if mode == "keyword_and_llm" && cfg.LLMModel != "" && !isSmallModel(cfg.LLMModel) {
		slog.Warn("topic_guard.large_model",
			"model", cfg.LLMModel,
			"hint", "classification model should be <7B params for low latency/cost",
		)
	}

	return g
}

// isSmallModel returns true if the model name matches known small model patterns (<7B params).
func isSmallModel(model string) bool {
	m := strings.ToLower(model)
	smallPatterns := []string{
		// Ollama-style: model:quant
		"1b", "2b", "3b", "4b",
		// Known small model families
		"tinyllama", "phi-2", "phi2", "phi-1", "phi1",
		"qwen2.5:0.5b", "qwen2.5:1.5b", "qwen2.5:3b",
		"qwen2:0.5b", "qwen2:1.5b",
		"gemma:2b", "gemma2:2b", "gemma2:9b",
		"llama3.2:1b", "llama3.2:3b",
		"ministral", "mistral:7b",
		"granite3-dense:2b",
		"smollm",
	}
	for _, p := range smallPatterns {
		if strings.Contains(m, p) {
			return true
		}
	}
	return false
}

// Check evaluates whether a user message is within the agent's topic scope.
func (g *TopicGuard) Check(ctx context.Context, message string) *TopicGuardResult {
	msgLower := strings.ToLower(message)

	// Block takes priority over allow.
	for i, re := range g.blockRegexps {
		if re.MatchString(msgLower) {
			slog.Debug("topic_guard.block_keyword", "keyword", g.blockKeywords[i], "message", message)
			return &TopicGuardResult{
				Allowed:      false,
				Reason:       "block_keyword",
				RejectionMsg: g.rejectionMsg,
			}
		}
	}

	for i, re := range g.allowRegexps {
		if re.MatchString(msgLower) {
			slog.Debug("topic_guard.allow_keyword", "keyword", g.allowKeywords[i], "message", message)
			return &TopicGuardResult{Allowed: true, Reason: "allow_keyword"}
		}
	}

	// No keyword match — use LLM fallback if configured.
	if g.mode == "keyword_and_llm" && g.provider != nil {
		inContext, err := g.classifyWithLLM(ctx, message)
		if err != nil {
			slog.Warn("topic_guard.llm_error", "error", err)
			// Fall back to default action on LLM failure.
			return g.defaultResult()
		}
		if inContext {
			return &TopicGuardResult{Allowed: true, Reason: "llm_allow"}
		}
		return &TopicGuardResult{
			Allowed:      false,
			Reason:       "llm_block",
			RejectionMsg: g.rejectionMsg,
		}
	}

	return g.defaultResult()
}

func (g *TopicGuard) defaultResult() *TopicGuardResult {
	if g.defaultAction == "block" {
		return &TopicGuardResult{
			Allowed:      false,
			Reason:       "default_block",
			RejectionMsg: g.rejectionMsg,
		}
	}
	return &TopicGuardResult{Allowed: true, Reason: "default_allow"}
}

func (g *TopicGuard) classifyWithLLM(ctx context.Context, message string) (bool, error) {
	timeout := time.Duration(g.llmTimeoutMs) * time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	topics := strings.Join(g.allowKeywords, ", ")
	prompt := fmt.Sprintf(
		"You are a topic classifier. The agent handles: %s.\n"+
			"Is the following user question related to these topics? Answer ONLY \"yes\" or \"no\".\n\n"+
			"User question: %s",
		topics, message,
	)

	req := providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "user", Content: prompt},
		},
		Model:   g.llmModel,
		Options: map[string]any{"max_tokens": g.llmMaxTokens},
	}

	resp, err := g.provider.Chat(ctx, req)
	if err != nil {
		return false, fmt.Errorf("LLM classification call failed: %w", err)
	}

	answer := strings.TrimSpace(strings.ToLower(resp.Content))
	return answer == "yes" || strings.Contains(answer, "yes"), nil
}

// CheckResponse evaluates whether an LLM response is within the agent's topic scope.
// Only checks blocklist against response — allowlist is not relevant for output checking.
func (g *TopicGuard) CheckResponse(ctx context.Context, response string) *TopicGuardResult {
	respLower := strings.ToLower(response)

	// Check blocklist keywords in response.
	for i, re := range g.blockRegexps {
		if re.MatchString(respLower) {
			slog.Debug("topic_guard.block_keyword_response", "keyword", g.blockKeywords[i])
			return &TopicGuardResult{
				Allowed:      false,
				Reason:       "block_keyword_response",
				RejectionMsg: g.rejectionMsg,
			}
		}
	}

	// LLM fallback for response classification.
	if g.mode == "keyword_and_llm" && g.provider != nil {
		inContext, err := g.classifyResponseWithLLM(ctx, response)
		if err != nil {
			slog.Warn("topic_guard.llm_response_error", "error", err)
			return &TopicGuardResult{Allowed: true, Reason: "llm_response_error"}
		}
		if !inContext {
			return &TopicGuardResult{
				Allowed:      false,
				Reason:       "llm_block_response",
				RejectionMsg: g.rejectionMsg,
			}
		}
		return &TopicGuardResult{Allowed: true, Reason: "llm_allow_response"}
	}

	return &TopicGuardResult{Allowed: true, Reason: "response_passed"}
}

func (g *TopicGuard) classifyResponseWithLLM(ctx context.Context, response string) (bool, error) {
	timeout := time.Duration(g.llmTimeoutMs) * time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	topics := strings.Join(g.allowKeywords, ", ")
	prompt := fmt.Sprintf(
		"You are a topic classifier. The agent handles: %s.\n"+
			"Is the following AI response on-topic for these domains? Answer ONLY \"yes\" or \"no\".\n\n"+
			"AI response: %s",
		topics, response,
	)

	req := providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "user", Content: prompt},
		},
		Model:   g.llmModel,
		Options: map[string]any{"max_tokens": g.llmMaxTokens},
	}

	resp, err := g.provider.Chat(ctx, req)
	if err != nil {
		return false, fmt.Errorf("LLM response classification failed: %w", err)
	}

	answer := strings.TrimSpace(strings.ToLower(resp.Content))
	return answer == "yes" || strings.Contains(answer, "yes"), nil
}

func normalizeIntercept(v string) string {
	switch v {
	case "after", "both":
		return v
	default:
		return "before"
	}
}

// ParseTopicGuardConfig parses raw JSON into TopicGuardConfig.
func ParseTopicGuardConfig(data []byte) *config.TopicGuardConfig {
	if len(data) == 0 {
		return nil
	}
	var cfg config.TopicGuardConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return &cfg
}

func lowercaseAll(keywords []string) []string {
	out := make([]string, len(keywords))
	for i, kw := range keywords {
		out[i] = strings.ToLower(kw)
	}
	return out
}

// compileKeywordRegexps pre-compiles word-boundary regexps for each keyword.
func compileKeywordRegexps(keywords []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(keywords))
	for i, kw := range keywords {
		pattern := `\b` + regexp.QuoteMeta(kw) + `\b`
		out[i] = regexp.MustCompile(pattern)
	}
	return out
}
