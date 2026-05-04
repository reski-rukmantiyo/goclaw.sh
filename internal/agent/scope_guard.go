package agent

import (
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// declineIndicators are phrases that signal the agent is declining an off-topic query.
// Used to distinguish "agent correctly declined" from "agent answered off-topic".
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
func (g *ScopeGuardChecker) CheckResponse(userMsg, assistantResponse string) (bool, string) {
	if g.cfg == nil {
		return true, ""
	}

	lowerResp := strings.ToLower(assistantResponse)
	lowerUser := strings.ToLower(userMsg)

	// Check denied topics — highest priority
	for _, denied := range g.cfg.DeniedTopics {
		topicLower := strings.ToLower(denied)
		if topicMatches(lowerUser, topicLower) {
			// User asked about a denied topic — check if agent declined
			if isDecliningResponse(lowerResp) {
				return true, "" // agent correctly declined
			}
			// Agent engaged with denied topic
			return false, "response discusses denied topic: " + denied
		}
	}

	// Check allowed topics — if defined, user message must relate to at least one.
	// We only flag substantive off-topic responses, not short acknowledgments or greetings.
	if len(g.cfg.AllowedTopics) > 0 && !isUserMsgInScope(lowerUser, g.cfg.AllowedTopics) {
		if isDecliningResponse(lowerResp) {
			return true, "" // agent correctly declined
		}
		// Short/generic responses are likely acknowledgments — don't block them
		if isLikelyGenericResponse(lowerResp) {
			return true, ""
		}
		// Longer response that doesn't mention any allowed topic — flag it
		if !isResponseInScope(lowerResp, g.cfg.AllowedTopics) {
			return false, "response may be outside allowed scope"
		}
	}

	return true, ""
}

// topicMatches checks if a lowercase text contains the topic string.
// Supports simple substring matching — topics should be specific enough
// to avoid false positives (e.g. "medical advice" not just "medical").
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
// These should not be flagged by the scope guard.
func isLikelyGenericResponse(lowerResp string) bool {
	trimmed := strings.TrimSpace(lowerResp)
	// Very short responses (< 80 chars) are likely acknowledgments
	if len(trimmed) < 80 {
		return true
	}
	// Check for common acknowledgment patterns
	genericPatterns := []string{
		"sure,",
		"of course",
		"let me",
		"i'll ",
		"i will",
		"give me",
		"one moment",
		"just a",
		"checking",
		"looking",
	}
	lower := strings.ToLower(trimmed)
	for _, p := range genericPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}
