package memory

import "strings"

// continuityTerms are substrings that signal a user message likely refers to
// prior work / a past session. When matched, the auto-injector runs a broader
// recall and surfaces L1 content so the agent can answer "do you remember what
// we discussed on Monday?" without the LLM having to call memory_search.
//
// Locale coverage: English, Vietnamese, Chinese (en / vi / zh) per the project
// i18n rule. Matching is case-insensitive substring Contains — locale-tolerant
// for CJK (no word boundaries) and cheap. False positives cost one extra
// bounded store search on the rare continuity-shaped turn; never on the common
// trivial path. See 009-bugfix-agent-episodic-recall-not-surfaced.md FR-04.
var continuityTerms = []string{
	// English — recall verbs + anaphora
	"remember", "recall", "recap", "earlier", "before", "previously",
	"last time", "last week", "last month", "yesterday", "the other day",
	"days ago", "weeks ago", "a while ago", "ago",
	"what did we", "did we discuss", "did we talk", "did we decide",
	"did you say", "you said", "you mentioned", "you told me",
	"we discussed", "we talked", "we decided", "we agreed", "as discussed",
	"follow up", "following up", "continue", "continuing", "picking up",
	// English — weekday names (common in "on Monday" references)
	"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday",

	// Vietnamese — recall/temporal
	"nhớ", "nhớ không", "nhớ lại", "nhắc lại", "hôm qua", "hôm kia",
	"tuần trước", "tháng trước", "lần trước", "lần trước", "hôm trước",
	"đã nói", "đã thảo luận", "đã quyết định", "đã đề cập",
	"chúng ta đã", "bạn đã nói", "bạn đã đề cập", "tiếp tục", "theo dõi",

	// Chinese — recall/temporal
	"记得", "还记得", "上次", "昨天", "前天", "上周", "上个月", "之前",
	"之前说过", "之前讨论", "我们讨论过", "我们说过", "你说过", "你提到",
	"刚才", "前几天", "继续", "接着",
}

// isContinuityQuery reports whether the user message looks like a reference to
// prior work or a past session. Used by the auto-injector to trigger a deeper
// recall (009 FR-04).
func isContinuityQuery(msg string) bool {
	if msg == "" {
		return false
	}
	low := strings.ToLower(msg)
	for _, term := range continuityTerms {
		if strings.Contains(low, term) {
			return true
		}
	}
	return false
}
