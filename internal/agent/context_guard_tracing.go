package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// emitContextGuardSpan records a tracing span for a context guard evaluation.
//
// Span name includes provider/model when available:
//   "context_guard.evaluation (anthropic/haiku)"
//
// Metadata contains request (message preview, scope, rules) and response
// (decision, matched_rules, reason) so operators can debug guard decisions.
//
// No-op when ctx has no collector attached.
func emitContextGuardSpan(
	ctx context.Context,
	startedAt time.Time,
	message string,
	cfg *config.ContextGuardConfig,
	providerName string,
	model string,
	result *ContextGuardResult,
	systemPromptPreview string,
	inputPreview string,
	evalErr error,
) {
	collector := tracing.CollectorFromContext(ctx)
	if collector == nil {
		return
	}

	end := time.Now().UTC()
	durationMS := max(int(end.Sub(startedAt)/time.Millisecond), 0)

	status := store.SpanStatusCompleted
	errMsg := ""
	if evalErr != nil {
		status = store.SpanStatusError
		errMsg = evalErr.Error()
	}

	// Build span name with provider/model when available.
	name := "context_guard.evaluation"
	if providerName != "" && model != "" {
		name = fmt.Sprintf("context_guard.evaluation (%s/%s)", providerName, model)
	} else if providerName != "" {
		name = fmt.Sprintf("context_guard.evaluation (%s)", providerName)
	}

	// Request metadata.
	requestMeta := map[string]any{
		"message_preview":   tracing.TruncateMid(message, 500),
		"scope":             cfg.ScopeDescription,
		"rules_count":       len(cfg.Rules),
		"max_history_turns": cfg.MaxHistoryTurns,
	}

	// Response metadata.
	responseMeta := map[string]any{
		"blocked":       result != nil && result.Blocked,
		"warning":       result != nil && result.Warning,
		"matched_rules": []string{},
		"reason":        "",
		"decision":      "allow",
	}
	if evalErr != nil {
		responseMeta["error"] = evalErr.Error()
		responseMeta["decision"] = "error"
	}
	if result != nil {
		responseMeta["blocked"] = result.Blocked
		responseMeta["warning"] = result.Warning
		responseMeta["matched_rules"] = result.MatchedRules
		responseMeta["reason"] = result.Reason
		if result.Blocked {
			responseMeta["decision"] = "block"
		} else if result.Warning {
			responseMeta["decision"] = "warn"
		}
	}

	metadata := map[string]any{
		"request":  requestMeta,
		"response": responseMeta,
	}
	metaJSON, _ := json.Marshal(metadata)

	span := store.SpanData{
		TraceID:             tracing.TraceIDFromContext(ctx),
		SpanType:            store.SpanTypeEvent,
		Name:                name,
		StartTime:           startedAt,
		EndTime:             &end,
		DurationMS:          durationMS,
		Status:              status,
		Error:               errMsg,
		SystemPromptPreview: systemPromptPreview,
		InputPreview:        inputPreview,
		Metadata:            metaJSON,
		TeamID:              tracing.TraceTeamIDPtrFromContext(ctx),
		TenantID:            store.TenantIDFromContext(ctx),
		CreatedAt:           end,
	}
	if parent := tracing.ParentSpanIDFromContext(ctx); parent != uuid.Nil {
		p := parent
		span.ParentSpanID = &p
	}

	collector.EmitSpan(span)
}
