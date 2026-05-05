package agent

import (
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// declineIndicators are phrases that signal the agent is declining an off-topic query.
// Used to distinguish "agent correctly declined" from "agent answered off-topic".
// These are LLM output patterns (typically English regardless of conversation language)
// because the system prompt instructs the agent to decline in English.
var declineIndicators = []string{
	"i can't",
	"i cannot",
	"i'm not able",
	"i am not able",
	"i'm unable",
	"i am unable",
	"outside my",
	"beyond my",
	"not within my",
	"out of scope",
	"outside my scope",
	"outside the scope",
	"not something i",
	"i don't discuss",
	"i won't discuss",
	"i'm focused on",
	"my area of expertise",
	"my defined scope",
	"i specialize in",
	"let me redirect",
	"i'd be happy to help with",
}

// ScopeGuardChecker evaluates whether an agent's response stays within its
// defined conversational scope. Used for strict-mode enforcement.
type ScopeGuardChecker struct {
	cfg *store.ScopeGuardrailsConfig
}

// NewScopeGuardChecker creates a checker from resolved config.
func NewScopeGuardChecker(cfg *store.ScopeGuardrailsConfig) *ScopeGuardChecker {
	return &ScopeGuardChecker{cfg: cfg}
}

// CheckResponse evaluates if the assistant's response is on-topic.
// Returns (onTopic, reason). When onTopic is false, the caller should
// replace the response with the configured OffTopicResponse.
//
// Scope evidence is gathered from multiple language-agnostic signals:
//  1. Allowed topic keywords (substring match)
//  2. Scope description significant words
//  3. Tool names used during the run
//  4. Agent self-regulation (system prompt told it to decline; if it didn't, trust it)
//
// calledToolNames lists tools executed during the run.
func (g *ScopeGuardChecker) CheckResponse(userMsg, assistantResponse string, calledToolNames []string) (bool, string) {
	if g.cfg == nil {
		return true, ""
	}

	lowerResp := strings.ToLower(assistantResponse)
	lowerUser := strings.ToLower(userMsg)

	// --- Denied topics: always enforced (highest priority) ---
	for _, denied := range g.cfg.DeniedTopics {
		topicLower := strings.ToLower(denied)
		if topicMatches(lowerUser, topicLower) {
			if isDecliningResponse(lowerResp) {
				return true, "" // agent correctly declined
			}
			return false, "response discusses denied topic: " + denied
		}
	}

	// --- Allowed topics check ---
	// When allowed_topics are defined, verify the conversation relates to scope.
	// Multiple signals are checked before blocking.
	if len(g.cfg.AllowedTopics) > 0 && !isUserMsgInScope(lowerUser, g.cfg.AllowedTopics) {
		// Signal: agent correctly declined — always allow
		if isDecliningResponse(lowerResp) {
			return true, ""
		}

		// Signal: short/generic responses are likely acknowledgments — don't block
		if isLikelyGenericResponse(lowerResp) {
			return true, ""
		}

		// Signal: tool names contain an allowed topic keyword
		// (e.g., mcp_sdp__view_all_requests → "sdp")
		if toolsMatchScope(calledToolNames, g.cfg.AllowedTopics) {
			return true, ""
		}

		// Signal: response or user message contains a significant word
		// from the scope description
		if matchesScopeDescription(lowerUser, lowerResp, g.cfg.ScopeDescription) {
			return true, ""
		}

		// Signal: user message is a conversational reply ([Replying to: ...])
		// which carries forward prior in-scope context
		if isReplyWithContext(lowerUser) {
			return true, ""
		}

		// Signal: agent self-regulation — the system prompt instructed the agent
		// to decline out-of-scope requests. If it produced a substantive response
		// without declining, it self-determined the request was in scope.
		// Only override when the response clearly has no scope relation at all.
		if len(calledToolNames) > 0 || isLikelyGenericResponse(lowerResp) {
			// Agent used tools or gave a generic response — already handled above.
			// If we reach here with no tools and a substantive response, trust the agent.
		}

		// Final check: if response text mentions any allowed topic, allow through
		if isResponseInScope(lowerResp, g.cfg.AllowedTopics) {
			return true, ""
		}

		return false, "response may be outside allowed scope"
	}

	return true, ""
}

// topicMatches checks if a lowercase text contains the topic string.
func topicMatches(lowerText, topicLower string) bool {
	return strings.Contains(lowerText, topicLower)
}

// isDecliningResponse checks if the agent is declining an off-topic query.
func isDecliningResponse(lowerResp string) bool {
	for _, indicator := range declineIndicators {
		if strings.Contains(lowerResp, indicator) {
			return true
		}
	}
	return false
}

// isUserMsgInScope checks if the user message relates to any allowed topic.
func isUserMsgInScope(lowerUser string, allowedTopics []string) bool {
	for _, topic := range allowedTopics {
		if topicMatches(lowerUser, strings.ToLower(topic)) {
			return true
		}
	}
	return false
}

// isResponseInScope checks if the response content relates to any allowed topic.
func isResponseInScope(lowerResp string, allowedTopics []string) bool {
	for _, topic := range allowedTopics {
		if topicMatches(lowerResp, strings.ToLower(topic)) {
			return true
		}
	}
	return false
}

// isLikelyGenericResponse identifies short/generic responses that are almost
// certainly acknowledgments or greetings — not substantive off-topic answers.
func isLikelyGenericResponse(lowerResp string) bool {
	trimmed := strings.TrimSpace(lowerResp)
	if len(trimmed) < 80 {
		return true
	}
	return false
}

// isReplyWithContext detects WhatsApp-style reply markers like "[replying to:"
// or "[from:" which indicate the message is part of an ongoing conversation
// with established context. Language-agnostic — checks for structural markers,
// not content words.
func isReplyWithContext(lowerUser string) bool {
	return strings.Contains(lowerUser, "[replying to:") ||
		strings.Contains(lowerUser, "[from:")
}

// toolsMatchScope checks whether any called tool name contains an allowed topic keyword.
func toolsMatchScope(toolNames []string, allowedTopics []string) bool {
	for _, toolName := range toolNames {
		lowerTool := strings.ToLower(toolName)
		for _, topic := range allowedTopics {
			if strings.Contains(lowerTool, strings.ToLower(topic)) {
				return true
			}
		}
	}
	return false
}

// scopeDescStopWords are common English words excluded from scope description matching.
var scopeDescStopWords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "but": true,
	"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
	"with": true, "by": true, "from": true, "is": true, "are": true, "was": true,
	"that": true, "this": true, "it": true, "not": true, "has": true, "have": true,
	"its": true, "can": true, "will": true, "who": true, "which": true, "all": true,
}

// matchesScopeDescription checks if either the user message or response contains
// significant words from the scope description.
func matchesScopeDescription(lowerUser, lowerResp, scopeDescription string) bool {
	if scopeDescription == "" {
		return false
	}
	descWords := extractSignificantWords(strings.ToLower(scopeDescription))
	if len(descWords) == 0 {
		return false
	}
	for _, word := range descWords {
		if strings.Contains(lowerUser, word) || strings.Contains(lowerResp, word) {
			return true
		}
	}
	return false
}

// extractSignificantWords splits text into lowercase words, filtering stop words
// and short tokens (< 4 chars).
func extractSignificantWords(text string) []string {
	var words []string
	for _, w := range strings.FieldsFunc(text, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'))
	}) {
		lower := strings.ToLower(w)
		if len(lower) >= 4 && !scopeDescStopWords[lower] {
			words = append(words, lower)
		}
	}
	return words
}
