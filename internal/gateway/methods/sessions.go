package methods

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	httpapi "github.com/nextlevelbuilder/goclaw/internal/http"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// SessionsMethods handles sessions.list, sessions.preview, sessions.patch, sessions.delete, sessions.reset.
type SessionsMethods struct {
	sessions         store.SessionStore
	eventBus         bus.EventPublisher
	cfg              *config.Config
	channelInstances store.ChannelInstanceStore
}

func NewSessionsMethods(sess store.SessionStore, eventBus bus.EventPublisher, cfg *config.Config, channelInstances store.ChannelInstanceStore) *SessionsMethods {
	return &SessionsMethods{sessions: sess, eventBus: eventBus, cfg: cfg, channelInstances: channelInstances}
}

func (m *SessionsMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodSessionsList, m.handleList)
	router.Register(protocol.MethodSessionsPreview, m.handlePreview)
	router.Register(protocol.MethodSessionsPatch, m.handlePatch)
	router.Register(protocol.MethodSessionsDelete, m.handleDelete)
	router.Register(protocol.MethodSessionsReset, m.handleReset)
	router.Register(protocol.MethodSessionsCompact, m.handleCompact)
}

type sessionsListParams struct {
	AgentID string `json:"agentId"`
	Channel string `json:"channel"` // optional: filter by channel prefix ("ws", "telegram")
	Limit   int    `json:"limit"`
	Offset  int    `json:"offset"`
}

func (m *SessionsMethods) handleList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params sessionsListParams
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.Limit <= 0 {
		params.Limit = 20
	}

	opts := store.SessionListOpts{
		AgentID:  params.AgentID,
		Channel:  params.Channel,
		Limit:    params.Limit,
		Offset:   params.Offset,
		TenantID: store.TenantIDFromContext(ctx),
	}
	// Role-based filtering: admins/owners see all sessions; regular users see only their own.
	// Tenant scope is always applied above — admin sees all sessions within the tenant.
	if !canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, client.UserID()) {
		opts.UserID = client.UserID()
	}

		result := m.sessions.ListPagedRich(ctx, opts)
		m.enrichWhatsAppGroupNames(ctx, result.Sessions)
		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"sessions": result.Sessions,
		"total":    result.Total,
		"limit":    params.Limit,
		"offset":   params.Offset,
	}))
}

type sessionKeyParams struct {
	Key string `json:"key"`
}

func (m *SessionsMethods) handlePreview(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params sessionKeyParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if !canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, client.UserID()) {
		sess := m.sessions.Get(ctx, params.Key)
		if sess == nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "session", params.Key)))
			return
		}
		if sess.UserID != client.UserID() {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "session")))
			return
		}
	}

	history := m.sessions.GetHistory(ctx, params.Key)
	summary := m.sessions.GetSummary(ctx, params.Key)

	// Sign file URLs before delivery — sessions store clean paths.
	secret := httpapi.FileSigningKey()
	for i := range history {
		history[i].Content = httpapi.SignFileURLs(history[i].Content, secret)
		for j := range history[i].MediaRefs {
			history[i].MediaRefs[j].Path = httpapi.SignMediaPath(history[i].MediaRefs[j].Path, secret)
		}
	}
	summary = httpapi.SignFileURLs(summary, secret)

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"key":      params.Key,
		"messages": history,
		"summary":  summary,
	}))
}

// handlePatch updates session metadata fields.
// Matching TS sessions.patch (src/gateway/server-methods/sessions.ts:237-287).
func (m *SessionsMethods) handlePatch(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		Key      string            `json:"key"`
		Label    *string           `json:"label,omitempty"`
		Model    *string           `json:"model,omitempty"`
		Metadata map[string]string `json:"metadata,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if params.Key == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "key")))
		return
	}

	if !canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, client.UserID()) {
		sess := m.sessions.Get(ctx, params.Key)
		if sess == nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "session", params.Key)))
			return
		}
		if sess.UserID != client.UserID() {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "session")))
			return
		}
	}

	// Apply label patch
	if params.Label != nil {
		m.sessions.SetLabel(ctx, params.Key, *params.Label)
	}

	// Apply model patch
	if params.Model != nil {
		m.sessions.UpdateMetadata(ctx, params.Key, *params.Model, "", "")
	}

	// Apply metadata patch
	if len(params.Metadata) > 0 {
		m.sessions.SetSessionMetadata(ctx, params.Key, params.Metadata)
	}

	// Save changes to DB
	m.sessions.Save(ctx, params.Key)

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok":  true,
		"key": params.Key,
	}))
	emitAudit(m.eventBus, client, "session.patched", "session", params.Key)
}

func (m *SessionsMethods) handleDelete(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params sessionKeyParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if !canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, client.UserID()) {
		sess := m.sessions.Get(ctx, params.Key)
		if sess == nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "session", params.Key)))
			return
		}
		if sess.UserID != client.UserID() {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "session")))
			return
		}
	}

	if err := m.sessions.Delete(ctx, params.Key); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok": true,
	}))
	emitAudit(m.eventBus, client, "session.deleted", "session", params.Key)
}

func (m *SessionsMethods) handleReset(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params sessionKeyParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if !canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, client.UserID()) {
		sess := m.sessions.Get(ctx, params.Key)
		if sess == nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "session", params.Key)))
			return
		}
		if sess.UserID != client.UserID() {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "session")))
			return
		}
	}

	m.sessions.Reset(ctx, params.Key)

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok": true,
	}))
	emitAudit(m.eventBus, client, "session.reset", "session", params.Key)
}

type sessionCompactParams struct {
	Key      string `json:"key"`
	KeepLast int    `json:"keepLast,omitempty"` // default 4
}

// handleCompact truncates session history to the last N messages.
// Issue 958: Manual session compaction API (truncate-only, no LLM summarization).
func (m *SessionsMethods) handleCompact(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params sessionCompactParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if params.Key == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "key is required"))
		return
	}

	keepLast := params.KeepLast
	if keepLast <= 0 {
		keepLast = 4 // default: keep last 2 exchanges
	}

	// Auth check
	sess := m.sessions.Get(ctx, params.Key)
	if sess == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "session", params.Key)))
		return
	}
	if !canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, client.UserID()) {
		if sess.UserID != client.UserID() {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "session")))
			return
		}
	}

	history := m.sessions.GetHistory(ctx, params.Key)
	originalLen := len(history)
	slog.Info("session_compact_start", "key", params.Key, "original", originalLen, "keep_last", keepLast)

	if originalLen < 6 {
		slog.Info("session_compact_too_short", "key", params.Key, "original", originalLen)
		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
			"ok":      true,
			"message": "session too short to compact",
			"kept":    originalLen,
		}))
		return
	}

	// Truncate history to last N messages
	m.sessions.TruncateHistory(ctx, params.Key, keepLast)
	m.sessions.IncrementCompaction(ctx, params.Key)
	if err := m.sessions.Save(ctx, params.Key); err != nil {
		slog.Warn("session_compact_save_failed", "key", params.Key, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	slog.Info("session_compact_done", "key", params.Key, "original", originalLen, "kept", keepLast)
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok":       true,
		"original": originalLen,
		"kept":     keepLast,
	}))
	emitAudit(m.eventBus, client, "session.compacted", "session", params.Key)
}

// enrichWhatsAppGroupNames resolves WhatsApp group JIDs to human-readable names
// from channel instance configs. Skips sessions that already have chat_title metadata.
func (m *SessionsMethods) enrichWhatsAppGroupNames(ctx context.Context, sessions []store.SessionInfoRich) {
	if m.channelInstances == nil || len(sessions) == 0 {
		return
	}

	// Collect unique WhatsApp channel instance names from session keys.
	channelNames := make(map[string]bool)
	for _, s := range sessions {
		if s.Metadata != nil && s.Metadata[tools.MetaChatTitle] != "" {
			continue
		}
		chName, groupJID := parseWhatsAppGroupKey(s.Key)
		if chName != "" && groupJID != "" {
			channelNames[chName] = true
		}
	}
	if len(channelNames) == 0 {
		return
	}

	// Load configs for those channel instances.
	groupMap := make(map[string]map[string]string) // channelName → jid → name
	for chName := range channelNames {
		inst, err := m.channelInstances.GetByName(ctx, chName)
		if err != nil || inst == nil {
			continue
		}
		groups := parseWhatsAppGroupsFromConfig(inst.Config)
		if len(groups) > 0 {
			groupMap[chName] = groups
		}
	}
	if len(groupMap) == 0 {
		return
	}

	// Enrich sessions.
	for i := range sessions {
		s := &sessions[i]
		if s.Metadata != nil && s.Metadata[tools.MetaChatTitle] != "" {
			continue
		}
		chName, groupJID := parseWhatsAppGroupKey(s.Key)
		if chName == "" || groupJID == "" {
			continue
		}
		if groups, ok := groupMap[chName]; ok {
			if name, ok := groups[groupJID]; ok && name != "" {
				if s.Metadata == nil {
					s.Metadata = make(map[string]string)
				}
				s.Metadata[tools.MetaChatTitle] = name
			}
		}
	}
}

// parseWhatsAppGroupKey extracts channel instance name and group JID from a session key.
// Returns ("", "") if not a WhatsApp group session.
// Key format: agent:{agentId}:{channelName}:group:{jid}
func parseWhatsAppGroupKey(key string) (channelName, groupJID string) {
	// Split: agent:{agentId}:{scope...}
	parts := strings.SplitN(key, ":", 3)
	if len(parts) < 3 || parts[0] != "agent" {
		return "", ""
	}
	scope := parts[2]
	// Split scope: {channelName}:group:{jid}
	scopeParts := strings.SplitN(scope, ":", 3)
	if len(scopeParts) < 3 || scopeParts[1] != "group" {
		return "", ""
	}
	chName := scopeParts[0]
	if !strings.HasPrefix(chName, "whatsapp") {
		return "", ""
	}
	return chName, scopeParts[2]
}

// parseWhatsAppGroupsFromConfig extracts group JID→name mappings from a channel instance config JSONB.
func parseWhatsAppGroupsFromConfig(raw json.RawMessage) map[string]string {
	var wrapper struct {
		Groups map[string]*config.WhatsAppGroupConfig `json:"groups"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil || len(wrapper.Groups) == 0 {
		return nil
	}
	result := make(map[string]string, len(wrapper.Groups))
	for jid, grp := range wrapper.Groups {
		if grp != nil && grp.Name != "" {
			result[jid] = grp.Name
		}
	}
	return result
}
