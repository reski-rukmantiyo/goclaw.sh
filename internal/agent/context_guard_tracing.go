package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// emitContextGuardSpan records a tracing span for a context guard evaluation.
//
// The span name is "context_guard.evaluation". Metadata contains blocked,
// warning, matched_rules, and reason so operators can debug guard decisions.
//
// No-op when ctx has no collector attached.
func emitContextGuardSpan(
	ctx context.Context,
	startedAt time.Time,
	result *ContextGuardResult,
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

	metadata := map[string]any{
		"blocked":       false,
		"warning":       false,
		"matched_rules": []string{},
		"reason":        "",
	}
	if result != nil {
		metadata["blocked"] = result.Blocked
		metadata["warning"] = result.Warning
		metadata["matched_rules"] = result.MatchedRules
		metadata["reason"] = result.Reason
	}
	metaJSON, _ := json.Marshal(metadata)

	span := store.SpanData{
		TraceID:    tracing.TraceIDFromContext(ctx),
		SpanType:   store.SpanTypeEvent,
		Name:       "context_guard.evaluation",
		StartTime:  startedAt,
		EndTime:    &end,
		DurationMS: durationMS,
		Status:     status,
		Error:      errMsg,
		Metadata:   metaJSON,
		TeamID:     tracing.TraceTeamIDPtrFromContext(ctx),
		TenantID:   store.TenantIDFromContext(ctx),
		CreatedAt:  end,
	}
	if parent := tracing.ParentSpanIDFromContext(ctx); parent != uuid.Nil {
		p := parent
		span.ParentSpanID = &p
	}

	collector.EmitSpan(span)
}
