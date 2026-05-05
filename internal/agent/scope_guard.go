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
// calledToolNames lists tools executed during the run — if any tool name
// contains an allowed topic keyword, the response is considered in scope.
func (g *ScopeGuardChecker) CheckResponse(userMsg, assistantResponse string, calledToolNames []string) (bool, string) {
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

	// Check allowed topics — if defined, verify the conversation is in scope.
	// We use multiple signals to determine scope, not just keyword matching.
	if len(g.cfg.AllowedTopics) > 0 && !isUserMsgInScope(lowerUser, g.cfg.AllowedTopics) {
		if isDecliningResponse(lowerResp) {
			return true, "" // agent correctly declined
		}
		// Short/generic responses are likely acknowledgments — don't block them
		if isLikelyGenericResponse(lowerResp) {
			return true, ""
		}
		// If the agent used tools whose names contain an allowed topic keyword,
		// the response is considered in scope (e.g., mcp_sdp__view_all_requests → "sdp").
		if toolsMatchScope(calledToolNames, g.cfg.AllowedTopics) {
			return true, ""
		}
		// Match against scope_description as additional evidence.
		// The description captures broader domain language that individual topic keywords miss
		// (e.g., "handles email, calendar, CRM approvals, and administrative tasks").
		if matchesScopeDescription(lowerUser, lowerResp, g.cfg.ScopeDescription) {
			return true, ""
		}
		// When scope_description mentions administrative/operational work,
		// check for common admin task patterns (forward, draft, meeting, etc.)
		// as additional scope evidence.
		lowerDesc := strings.ToLower(g.cfg.ScopeDescription)
		if (strings.Contains(lowerDesc, "admin") || strings.Contains(lowerDesc, "operat") ||
			strings.Contains(lowerDesc, "task") || strings.Contains(lowerDesc, "assistant")) &&
			matchesAdminPatterns(lowerUser, lowerResp) {
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

// toolsMatchScope checks whether any called tool name contains an allowed topic keyword.
// This handles cases like "mcp_sdp__view_all_requests" matching allowed topic "sdp".
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
	"reski's": true, "reski": true, "right": true, "hand": true, "speed": true,
	"precision": true, "focused": true, "specializing": true, "specialist": true,
	"handles": true, "agent": true,
}

// matchesScopeDescription checks if either the user message or response contains
// significant words from the scope description. This catches cases where the
// conversation is clearly in-domain but doesn't match the short allowed_topics keywords.
func matchesScopeDescription(lowerUser, lowerResp, scopeDescription string) bool {
	if scopeDescription == "" {
		return false
	}

	// Extract significant words from scope description (>= 4 chars, not stop words).
	descWords := extractSignificantWords(strings.ToLower(scopeDescription))
	if len(descWords) == 0 {
		return false
	}

	// Check if either message contains any significant scope description word.
	for _, word := range descWords {
		if strings.Contains(lowerUser, word) || strings.Contains(lowerResp, word) {
			return true
		}
	}
	return false
}

// adminTaskPatterns are patterns commonly associated with administrative/operational
// assistant tasks. When the scope description mentions administrative or operational
// work, these patterns serve as additional scope evidence.
var adminTaskPatterns = []string{
	"forward",
	"draft",
	"meeting",
	"schedule",
	"invite",
	"attendee",
	"calendar",
	"reminder",
	"follow-up",
	"followup",
	"approve",
	"approval",
	"decline",
	"accept",
	"request",
	"ticket",
	"assign",
	"notify",
	"organizer",
	"subject:",
	"to:",
	"cc:",
	"regards",
	"best regards",
	"thanks",
}

// matchesAdminPatterns checks if the user message or response matches patterns
// typical of administrative/operational assistant work. Used as fallback evidence
// when the scope description mentions admin/ops domains.
func matchesAdminPatterns(lowerUser, lowerResp string) bool {
	for _, pat := range adminTaskPatterns {
		if strings.Contains(lowerUser, pat) || strings.Contains(lowerResp, pat) {
			return true
		}
	}
	return false
}

// extractSignificantWords splits text into lowercase words, filtering stop words
// and short tokens (< 4 chars).
func extractSignificantWords(text string) []string {
	// Split on non-alphanumeric boundaries
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
