package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tokencount"
)

// compactionSummaryPrompt is the structured summarization instruction used by both
// mid-loop compaction and background summarization. Matching OpenClaw TS compaction.ts
// MERGE_SUMMARIES_INSTRUCTIONS + IDENTIFIER_PRESERVATION_INSTRUCTIONS.
const compactionSummaryPrompt = `Summarize this conversation concisely for the AI agent to resume work.

MUST PRESERVE:
- Active tasks and their current status (in-progress, blocked, pending)
- Pending subagent tasks (IDs, labels, statuses) — agent needs to know what is still running
- Pending team task results awaiting delivery (task IDs, assignees, statuses)
- Any "waiting for..." state — do NOT drop expectations of future results
- Batch operation progress (e.g., "5/17 items completed")
- The last thing the user requested and what was being done about it
- Decisions made and their rationale
- TODOs, open questions, and constraints
- Any commitments or follow-ups promised

IDENTIFIER PRESERVATION:
Preserve all opaque identifiers exactly as written (no shortening or reconstruction),
including UUIDs, hashes, IDs, tokens, API keys, hostnames, IPs, ports, URLs, and file names.

PRIORITIZE recent context over older history. The agent needs to know
what it was doing, not just what was discussed.

Conversation to summarize:

`

// CompactMessagesWithProvider summarizes the first ~70% of messages into a condensed
// summary, keeping the last ~30% intact. It uses the given provider for the LLM call.
// Returns nil on failure (caller keeps original messages).
func CompactMessagesWithProvider(
	ctx context.Context,
	provider providers.Provider,
	model string,
	messages []providers.Message,
	keepLast int,
	tokenCounter tokencount.TokenCounter,
	logKey string,
) []providers.Message {
	if len(messages) < 6 {
		return nil
	}

	if keepLast <= 0 {
		keepLast = 4
	}
	// Cap keepLast so splitIdx stays non-negative (need at least 2 messages to summarize).
	if keepLast > len(messages)-2 {
		keepLast = len(messages) - 2
	}
	// Ensure we keep at least 30% of messages.
	if minKeep := len(messages) * 3 / 10; minKeep > keepLast {
		keepLast = minKeep
	}

	// Find a clean split boundary. Walk forward from initial position to skip
	// tool result messages, ensuring the kept section starts on a non-tool message.
	// assistant+tool_calls is a valid start — LLMs handle assistant messages at any
	// context position. Only tool results are problematic (orphaned without preceding
	// assistant+tool_calls). Must leave at least 2 messages to summarize.
	splitIdx := len(messages) - keepLast
	maxSplit := len(messages) - 2
	initialSplit := splitIdx
	for splitIdx <= maxSplit {
		if messages[splitIdx].Role == "tool" {
			splitIdx++
			continue
		}
		break
	}
	if splitIdx > maxSplit {
		// Fallback: force initial position. Summary builder already skips tool
		// messages; most LLMs tolerate orphaned tool results in context.
		slog.Warn("compaction_forced_split", "key", logKey, "messages", len(messages), "split_idx", initialSplit)
		splitIdx = initialSplit
	}

	// Build summary input (same pattern as maybeSummarize in loop_history.go).
	toSummarize := messages[:splitIdx]
	var sb strings.Builder
	for _, m := range toSummarize {
		switch m.Role {
		case "user":
			fmt.Fprintf(&sb, "user: %s\n", m.Content)
		case "assistant":
			fmt.Fprintf(&sb, "assistant: %s\n", SanitizeAssistantContent(m.Content))
		}
	}

	sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	inTokens := estimateSummaryInputTokens(tokenCounter, model, toSummarize)
	slog.Info("compact_budget", "key", logKey, "in_tokens", inTokens, "out_tokens", dynamicSummaryMax(inTokens))
	resp, err := provider.Chat(sctx, providers.ChatRequest{
		Messages: []providers.Message{{
			Role:    "user",
			Content: compactionSummaryPrompt + sb.String(),
		}},
		Model:   model,
		Options: map[string]any{"max_tokens": dynamicSummaryMax(inTokens), "temperature": 0.3},
	})
	if err != nil {
		slog.Warn("compaction_failed", "key", logKey, "error", err)
		return nil
	}

	summaryContent := SanitizeAssistantContent(resp.Content)
	if summaryContent == "" {
		slog.Warn("compaction_empty_summary", "key", logKey, "original_msgs", len(messages))
		return nil
	}

	// Collect MediaRefs from compacted messages (keep up to 30 most recent).
	const maxPreservedMediaRefs = 30
	var preservedRefs []providers.MediaRef
	for i := len(toSummarize) - 1; i >= 0 && len(preservedRefs) < maxPreservedMediaRefs; i-- {
		for _, ref := range toSummarize[i].MediaRefs {
			preservedRefs = append(preservedRefs, ref)
			if len(preservedRefs) >= maxPreservedMediaRefs {
				break
			}
		}
	}

	summary := providers.Message{
		Role:      "user",
		Content:   "[Summary of earlier conversation]\n" + summaryContent,
		MediaRefs: preservedRefs,
	}
	keepLast = len(messages) - splitIdx
	result := make([]providers.Message, 0, 1+keepLast)
	result = append(result, summary)
	result = append(result, messages[splitIdx:]...)

	slog.Info("compacted",
		"key", logKey,
		"original_msgs", len(messages),
		"summarized", splitIdx,
		"kept", len(result))

	return result
}

// compactMessagesInPlace wraps CompactMessagesWithProvider using Loop fields.
func (l *Loop) compactMessagesInPlace(ctx context.Context, messages []providers.Message) []providers.Message {
	keepLast := 4
	if l.compactionCfg != nil && l.compactionCfg.KeepLastMessages > 0 {
		keepLast = l.compactionCfg.KeepLastMessages
	}
	return CompactMessagesWithProvider(ctx, l.provider, l.model, messages, keepLast, l.tokenCounter, l.id)
}

// dynamicSummaryMax returns the output-token budget for a compaction or
// summarization call, scaled to input size. Formula: in/25 (~4% compression),
// clamped to [1024, 8192]. Floor keeps short summaries coherent; cap prevents
// runaway output billing on pathological inputs.
func dynamicSummaryMax(inputTokens int) int {
	out := min(max(inputTokens/25, 1024), 8192)
	return out
}

// estimateSummaryInputTokens returns a best-effort input-token count. Prefers
// TokenCounter when attached; else rune/3 fallback (~±15% for UTF-8).
func estimateSummaryInputTokens(counter tokencount.TokenCounter, model string, messages []providers.Message) int {
	if counter != nil {
		return counter.CountMessages(model, messages)
	}
	total := 0
	for _, m := range messages {
		total += len([]rune(m.Content)) / 3
	}
	return total
}

// estimateSummaryInputTokens returns a best-effort input-token count. Prefers
// TokenCounter when attached; else rune/3 fallback (~±15% for UTF-8).
func (l *Loop) estimateSummaryInputTokens(messages []providers.Message) int {
	return estimateSummaryInputTokens(l.tokenCounter, l.model, messages)
}
