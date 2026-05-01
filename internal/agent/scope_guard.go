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

	// Check allowed topics — if defined, response should stay in scope
	if len(g.cfg.AllowedTopics) > 0 && !isUserMsgInScope(lowerUser, g.cfg.AllowedTopics) {
		// User asked about something outside allowed topics
		if isDecliningResponse(lowerResp) {
			return true, "" // agent correctly declined
		}
		// Agent may have answered an off-topic query
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
	// If no topic matches, the response might be a generic greeting or small talk — allow it
	// Only flag when the response is substantive and off-topic
	return false
}
