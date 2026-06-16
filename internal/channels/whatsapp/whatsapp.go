package whatsapp

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	wastore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	pairingDebounceTime = 60 * time.Second
	maxMessageLen       = 4096 // WhatsApp practical message length limit
)

func init() {
	// Set device name shown in WhatsApp's "Linked Devices" screen (once at package init).
	wastore.DeviceProps.Os = new("GoClaw")
}

// Channel connects directly to WhatsApp via go.mau.fi/whatsmeow.
// Auth state is stored in PostgreSQL (standard) or SQLite (desktop).
type Channel struct {
	*channels.BaseChannel
	client           *whatsmeow.Client
	container        *sqlstore.Container
	config           config.WhatsAppConfig
	mu               sync.Mutex
	ctx              context.Context
	cancel           context.CancelFunc
	parentCtx        context.Context             // stored from Start() for Reauth() context chain
	audioMgr         *audio.Manager              // unified STT via audio.Manager (nil = no STT)
	builtinToolStore store.BuiltinToolStore      // reads stt settings (whatsapp_enabled) per voice message; nil = opt-out
	configPermStore  store.ConfigPermissionStore // group file writer ACL (nil = no writer management)
	execApprovalMgr  *tools.ExecApprovalManager  // channel-based exec approval (nil = not wired)

	// QR state
	lastQRMu        sync.RWMutex
	lastQRB64       string    // base64-encoded PNG, empty when authenticated
	waAuthenticated bool      // true once WhatsApp account is connected
	myJID           types.JID // linked account's phone JID for mention detection
	myLID           types.JID // linked account's LID — WhatsApp's newer identifier

	// typingCancel tracks active typing-refresh loops per chatID.
	typingCancel sync.Map // chatID string → context.CancelFunc

	// reauthMu serializes Reauth() and StartQRFlow() to prevent race when user clicks reauth rapidly.
	reauthMu sync.Mutex

	// reconnectMu guards the reconnect watchdog goroutine.
	reconnectMu     sync.Mutex
	reconnectCancel context.CancelFunc
	// pairingService, pairingDebounce, approvedGroups, groupHistory are inherited from channels.BaseChannel.

	// listenBuf accumulates messages for KG extraction in listen-only mode (nil if not configured).
	listenBuf *ListenBuffer
	agentUUID string // agent UUID for KG scoping (set via SetAgentUUID)

	// captionBuf delays media messages briefly to merge follow-up caption text.
	// Nil when media_caption_delay_ms is -1 (disabled).
	captionBuf *MediaCaptionBuffer

	// agentKeyCache maps agent_key → agent UUID for group agent_id overrides.
	// Used by the listen-only path to store raw messages under the correct agent scope.
	// Reads the LIVE c.config.Groups, so runtime-added overrides (applyJoinRules) are
	// honored immediately. Refreshed on channel load/reload and on CacheKindAgent events.
	// Guarded by agentKeyMu.
	agentStore    store.AgentStore
	tenantDBMgr   store.TenantDBManager
	agentKeyMu    sync.RWMutex
	agentKeyCache map[string]string
}

// SetAgentUUID stores the agent UUID for KG scoping. Called by InstanceLoader.
func (c *Channel) SetAgentUUID(uuid string) {
	c.agentUUID = uuid
	if c.listenBuf != nil {
		c.listenBuf.SetAgentUUID(uuid)
	}
}

// SetConfigPermStore wires the config permission store for group file writer management.
func (c *Channel) SetConfigPermStore(s store.ConfigPermissionStore) {
	c.configPermStore = s
}

// SetExecApprovalManager wires the exec approval manager for channel-based approval.
func (c *Channel) SetExecApprovalManager(mgr any) {
	if m, ok := mgr.(*tools.ExecApprovalManager); ok {
		c.execApprovalMgr = m
	}
}

// GetLastQRB64 returns the most recent QR PNG (base64).
func (c *Channel) GetLastQRB64() string {
	c.lastQRMu.RLock()
	defer c.lastQRMu.RUnlock()
	return c.lastQRB64
}

// IsAuthenticated reports whether the WhatsApp account is currently authenticated.
func (c *Channel) IsAuthenticated() bool {
	c.lastQRMu.RLock()
	defer c.lastQRMu.RUnlock()
	return c.waAuthenticated
}

// cacheQR stores the latest QR PNG (base64) for late-joining wizard clients.
func (c *Channel) cacheQR(pngB64 string) {
	c.lastQRMu.Lock()
	c.lastQRB64 = pngB64
	c.lastQRMu.Unlock()
}

// New creates a new WhatsApp channel backed by whatsmeow.
// dialect must be "pgx" (PostgreSQL) or "sqlite3" (SQLite/desktop).
// audioMgr is optional (nil = STT disabled).
// builtinToolStore is optional (nil = STT permanently opt-out regardless of admin toggle).
func New(cfg config.WhatsAppConfig, msgBus *bus.MessageBus,
	pairingSvc store.PairingStore, db *sql.DB,
	pendingStore store.PendingMessageStore, dialect string, audioMgr *audio.Manager,
	builtinToolStore store.BuiltinToolStore) (*Channel, error) {

	base := channels.NewBaseChannel(channels.TypeWhatsApp, msgBus, cfg.AllowFrom)
	base.ValidatePolicy(cfg.DMPolicy, cfg.GroupPolicy)

	container := sqlstore.NewWithDB(db, dialect, nil)
	if err := container.Upgrade(context.Background()); err != nil {
		return nil, fmt.Errorf("whatsapp sqlstore upgrade: %w", err)
	}

	ch := &Channel{
		BaseChannel:      base,
		config:           cfg,
		container:        container,
		audioMgr:         audioMgr,
		builtinToolStore: builtinToolStore,
	}

	// Initialize listen-only buffer if configured at any level.
	if ch.hasListenOnlyConfig() {
		ch.listenBuf = NewListenBuffer(
			nil, // rawMsgStore wired later via SetListenOnlyDeps
			"",  // agentKey set later via SetAgentID
			cfg.ListenFlushSec,
		)
		globalListenOnly := cfg.ListenOnly != nil && *cfg.ListenOnly
		slog.Info("whatsapp: listen-only buffer initialized",
			"global", globalListenOnly,
			"groups_configured", len(cfg.Groups),
			"flush_sec", cfg.ListenFlushSec)
		for jid, gc := range cfg.Groups {
			if gc != nil && gc.ListenOnly != nil {
				slog.Info("whatsapp: listen-only group", "jid", jid, "listen_only", *gc.ListenOnly)
			}
		}
	} else {
		slog.Info("whatsapp: listen-only NOT configured",
			"listen_only_nil", cfg.ListenOnly == nil,
			"groups_count", len(cfg.Groups))
	}

	// Media caption buffer: disabled by default. The gateway-level InboundDebouncer
	// handles media+caption merging. Set media_caption_delay_ms > 0 to enable
	// an additional WhatsApp-specific buffer for longer merge windows.
	if cfg.MediaCaptionDelayMs > 0 {
		ch.captionBuf = NewMediaCaptionBuffer(
			time.Duration(cfg.MediaCaptionDelayMs)*time.Millisecond,
			ch.publishBufferedMessage,
			scheduleMediaCleanup,
		)
		slog.Info("whatsapp: media caption buffer initialized", "delay_ms", cfg.MediaCaptionDelayMs)
	}

	ch.SetPairingService(pairingSvc)
	ch.SetGroupHistory(channels.MakeHistory("whatsapp", pendingStore, base.TenantID()))
	return ch, nil
}

// Start initializes the whatsmeow client and connects to WhatsApp.
func (c *Channel) Start(ctx context.Context) error {
	slog.Info("starting whatsapp channel (whatsmeow)")
	c.MarkStarting("Initializing WhatsApp connection")

	c.parentCtx = ctx
	c.ctx, c.cancel = context.WithCancel(ctx)

	deviceStore, err := c.container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("whatsapp get device: %w", err)
	}

	c.client = whatsmeow.NewClient(deviceStore, nil)
	c.client.AddEventHandler(c.handleEvent)

	if c.client.Store.ID == nil {
		// Not paired yet — QR flow will be triggered by qr_methods.go.
		slog.Info("whatsapp: not paired yet, waiting for QR scan", "channel", c.Name())
		c.MarkDegraded("Awaiting QR scan", "Scan QR code to authenticate",
			channels.ChannelFailureKindAuth, false)
	} else {
		if err := c.client.Connect(); err != nil {
			slog.Warn("whatsapp: initial connect failed", "error", err)
			c.MarkDegraded("Connection failed", err.Error(),
				channels.ChannelFailureKindNetwork, true)
		}
	}

	if gh := c.GroupHistory(); gh != nil {
		gh.StartFlusher()
	}
	c.SetRunning(true)
	return nil
}

// BlockReplyEnabled returns the per-channel block_reply override (nil = inherit gateway default).
func (c *Channel) BlockReplyEnabled() *bool { return c.config.BlockReply }

// SetPendingCompaction configures LLM-based auto-compaction for pending messages.
func (c *Channel) SetPendingCompaction(cfg *channels.CompactionConfig) {
	if gh := c.GroupHistory(); gh != nil {
		gh.SetCompactionConfig(cfg)
	}
}

// SetPendingHistoryTenantID propagates tenant_id to the pending history for DB operations.
func (c *Channel) SetPendingHistoryTenantID(id uuid.UUID) {
	if gh := c.GroupHistory(); gh != nil {
		gh.SetTenantID(id)
	}
}

// publishBufferedMessage is the flush callback for the media caption buffer.
func (c *Channel) publishBufferedMessage(msg bus.InboundMessage) {
	c.Bus().PublishInbound(msg)
}

// Stop gracefully shuts down the WhatsApp channel.
func (c *Channel) Stop(_ context.Context) error {
	slog.Info("stopping whatsapp channel")

	c.stopReconnectWatchdog()

	if c.cancel != nil {
		c.cancel()
	}
	if c.client != nil {
		c.client.Disconnect()
	}

	// Flush pending listen-only messages before shutdown.
	if c.listenBuf != nil {
		c.listenBuf.Close()
	}

	// Flush pending caption-buffered media messages before shutdown.
	if c.captionBuf != nil {
		c.captionBuf.FlushAll()
	}

	// Cancel all active typing goroutines.
	c.typingCancel.Range(func(key, value any) bool {
		if fn, ok := value.(context.CancelFunc); ok {
			fn()
		}
		c.typingCancel.Delete(key)
		return true
	})

	// Flush pending message history to DB before shutdown.
	if gh := c.GroupHistory(); gh != nil {
		gh.StopFlusher()
	}

	c.SetRunning(false)
	c.MarkStopped("Stopped")
	return nil
}

// handleEvent dispatches whatsmeow events.
func (c *Channel) handleEvent(evt any) {
	switch v := evt.(type) {
	case *events.Message:
		c.handleIncomingMessage(v)
	case *events.Connected:
		c.handleConnected()
	case *events.Disconnected:
		c.handleDisconnected()
	case *events.LoggedOut:
		c.handleLoggedOut(v)
	case *events.PairSuccess:
		slog.Info("whatsapp: pair success", "channel", c.Name())
	case *events.StreamError:
		slog.Warn("whatsapp: stream error", "code", v.Code, "channel", c.Name())
		c.MarkDegraded("WhatsApp stream error", fmt.Sprintf("Code: %s", v.Code),
			channels.ChannelFailureKindNetwork, true)
		c.startReconnectWatchdog()
	case *events.StreamReplaced:
		slog.Warn("whatsapp: session replaced by another client", "channel", c.Name())
		c.MarkDegraded("WhatsApp session replaced", "Another client connected",
			channels.ChannelFailureKindAuth, false)
	case *events.KeepAliveTimeout:
		slog.Debug("whatsapp: keepalive timeout", "error_count", v.ErrorCount,
			"channel", c.Name())
	case *events.KeepAliveRestored:
		slog.Info("whatsapp: keepalive restored", "channel", c.Name())
	case *events.CATRefreshError:
		slog.Warn("whatsapp: CAT refresh error", "error", v.Error, "channel", c.Name())
		c.MarkDegraded("WhatsApp CAT refresh failed", v.Error.Error(),
			channels.ChannelFailureKindNetwork, true)
		c.startReconnectWatchdog()
	case *events.JoinedGroup:
		c.handleJoinedGroup(v)
	case *events.GroupInfo:
		c.handleGroupInfoChange(v)
	}
}

// handleConnected processes the Connected event.
func (c *Channel) handleConnected() {
	c.stopReconnectWatchdog()

	c.lastQRMu.Lock()
	c.waAuthenticated = true
	c.lastQRB64 = ""
	if c.client.Store.ID != nil {
		c.myJID = *c.client.Store.ID
		c.myLID = c.client.Store.GetLID()
		slog.Info("whatsapp: connected", "jid", c.myJID.String(),
			"lid", c.myLID.String(), "channel", c.Name())
	}
	c.lastQRMu.Unlock()

	c.MarkHealthy("WhatsApp authenticated and connected")
}

// handleDisconnected processes the Disconnected event.
func (c *Channel) handleDisconnected() {
	c.lastQRMu.Lock()
	c.waAuthenticated = false
	c.lastQRMu.Unlock()

	c.MarkDegraded("WhatsApp disconnected", "Waiting for reconnect",
		channels.ChannelFailureKindNetwork, true)
	// Start safety-net watchdog in case whatsmeow's auto-reconnect fails.
	c.startReconnectWatchdog()
}

// handleLoggedOut processes the LoggedOut event.
func (c *Channel) handleLoggedOut(evt *events.LoggedOut) {
	slog.Warn("whatsapp: logged out", "reason", evt.Reason, "channel", c.Name())
	c.lastQRMu.Lock()
	c.waAuthenticated = false
	c.lastQRMu.Unlock()

	c.MarkDegraded("WhatsApp logged out", "Re-scan QR to reconnect",
		channels.ChannelFailureKindAuth, false)
}

// handleJoinedGroup processes JoinedGroup events emitted when the bot joins
// or is added to a group. It matches the adder against configured join rules
// and auto-creates per-group overrides.
func (c *Channel) handleJoinedGroup(evt *events.JoinedGroup) {
	groupJID := evt.JID
	groupJIDStr := groupJID.String()
	groupName := evt.GroupName.Name

	// Prefer SenderPN (phone number) over Sender (may be LID).
	var adderJID types.JID
	if evt.SenderPN != nil {
		adderJID = *evt.SenderPN
	} else if evt.Sender != nil {
		adderJID = *evt.Sender
	}

	slog.Info("whatsapp: joined group",
		"group_jid", groupJIDStr, "group_name", groupName,
		"added_by", adderJID.String(), "reason", evt.Reason, "type", evt.Type)

	c.applyJoinRules(groupJID, groupName, adderJID)
}

// handleGroupInfoChange processes GroupInfo events. When the bot is in the
// Join list (someone added us to an existing group), it applies join rules.
func (c *Channel) handleGroupInfoChange(evt *events.GroupInfo) {
	if len(evt.Join) == 0 {
		return
	}

	c.lastQRMu.RLock()
	myJID := c.myJID
	c.lastQRMu.RUnlock()

	var wasMeAdded bool
	for _, jid := range evt.Join {
		if !myJID.IsEmpty() && jid.User == myJID.User {
			wasMeAdded = true
			break
		}
	}
	if !wasMeAdded {
		return
	}

	var adderJID types.JID
	if evt.SenderPN != nil {
		adderJID = *evt.SenderPN
	} else if evt.Sender != nil {
		adderJID = *evt.Sender
	}

	groupName := ""
	if evt.Name != nil {
		groupName = evt.Name.Name
	}

	slog.Info("whatsapp: added to existing group",
		"group_jid", evt.JID.String(), "added_by", adderJID.String())

	c.applyJoinRules(evt.JID, groupName, adderJID)
}

// applyJoinRules matches the adder against configured GroupJoinRules.
// First matching rule wins. If matched, auto-creates a per-group override
// and auto-approves the group if the rule policy is "open".
func (c *Channel) applyJoinRules(groupJID types.JID, groupName string, adderJID types.JID) {
	ctx := context.Background()
	if tid := c.TenantID(); tid != uuid.Nil {
		ctx = store.WithTenantID(ctx, tid)
	}
	groupJIDStr := groupJID.String()

	// Ensure group contact exists.
	if cc := c.ContactCollector(); cc != nil {
		cc.EnsureContact(ctx, c.Type(), c.Name(), groupJIDStr, "", groupName, "", "group", "group", "", "")
	}

	rule := c.matchJoinRule(adderJID)
	if rule == nil {
		slog.Info("whatsapp: no join rule matched, using default group policy",
			"group_jid", groupJIDStr, "added_by", adderJID.String())
		return
	}

	slog.Info("whatsapp: join rule matched",
		"group_jid", groupJIDStr, "rule_name", rule.Name, "added_by", adderJID.String())

	grpCfg := &config.WhatsAppGroupConfig{
		Name:           groupName,
		AgentID:        rule.AgentID,
		ListenOnly:     rule.ListenOnly,
		ListenGraphID:  rule.ListenGraphID,
		RequireMention: rule.RequireMention,
	}

	c.mu.Lock()
	if c.config.Groups == nil {
		c.config.Groups = make(map[string]*config.WhatsAppGroupConfig)
	}
	c.config.Groups[groupJIDStr] = grpCfg
	c.mu.Unlock()

	// Auto-approve if rule says "open" (default for matched rules).
	policy := rule.Policy
	if policy == "" {
		policy = "open"
	}
	if policy == "open" {
		c.MarkGroupApproved(groupJIDStr)
	}

	// Persist updated config to DB.
	if persist := c.ConfigPersister(); persist != nil {
		if err := persist(ctx, c.config); err != nil {
			slog.Error("whatsapp: failed to persist auto-configured group",
				"group_jid", groupJIDStr, "error", err)
		}
	}

	slog.Info("whatsapp: group auto-configured by join rule",
		"group_jid", groupJIDStr, "group_name", groupName,
		"rule_name", rule.Name, "agent_id", rule.AgentID,
		"listen_only", rule.ListenOnly, "policy", policy)
}

// matchJoinRule returns the first join rule matching the adder's JID.
// Matching is by phone number (user portion of the JID).
func (c *Channel) matchJoinRule(adderJID types.JID) *config.WhatsAppGroupJoinRule {
	if adderJID.IsEmpty() {
		return nil
	}
	adderUser := adderJID.User
	for i := range c.config.GroupJoinRules {
		rule := &c.config.GroupJoinRules[i]
		ruleUser := rule.AddedBy
		if idx := strings.IndexByte(ruleUser, '@'); idx > 0 {
			ruleUser = ruleUser[:idx]
		}
		if ruleUser == adderUser {
			return rule
		}
	}
	return nil
}

// startReconnectWatchdog starts a background goroutine that periodically
// attempts to reconnect the WhatsApp client with exponential backoff.
// Idempotent: if a watchdog is already running, this is a no-op.
func (c *Channel) startReconnectWatchdog() {
	c.reconnectMu.Lock()
	defer c.reconnectMu.Unlock()

	// Already running — skip.
	if c.reconnectCancel != nil {
		return
	}

	parentCtx := c.parentCtx
	if parentCtx == nil {
		parentCtx = context.Background()
	}

	ctx, cancel := context.WithCancel(parentCtx)
	c.reconnectCancel = cancel

	go func() {
		defer func() {
			c.reconnectMu.Lock()
			c.reconnectCancel = nil
			c.reconnectMu.Unlock()
		}()

		const maxAttempts = 5
		const maxBackoff = 60 * time.Second

		for attempt := range maxAttempts {
			backoff := min(time.Duration(attempt+1)*5*time.Second, maxBackoff)

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}

			// Already reconnected (e.g., by whatsmeow's own auto-reconnect)?
			if c.client != nil && c.client.IsConnected() {
				slog.Info("whatsapp: reconnect watchdog detected connection restored",
					"channel", c.Name())
				return
			}

			if c.client == nil {
				slog.Warn("whatsapp: reconnect watchdog: client is nil, giving up",
					"channel", c.Name())
				c.MarkFailed("WhatsApp reconnect failed", "Client not initialized",
					channels.ChannelFailureKindNetwork, false)
				return
			}

			slog.Info("whatsapp: reconnect watchdog attempting connect",
				"channel", c.Name(), "attempt", attempt+1, "max", maxAttempts)

			if err := c.client.Connect(); err != nil {
				slog.Warn("whatsapp: reconnect watchdog connect failed",
					"channel", c.Name(), "attempt", attempt+1, "error", err)
				continue
			}

			// Connect succeeded — handleConnected will fire via the event handler.
			slog.Info("whatsapp: reconnect watchdog connected",
				"channel", c.Name(), "attempt", attempt+1)
			return
		}

		// Exhausted all attempts.
		slog.Warn("whatsapp: reconnect watchdog exhausted retries",
			"channel", c.Name(), "attempts", maxAttempts)
		c.MarkFailed("WhatsApp reconnect failed",
			fmt.Sprintf("Failed after %d attempts — re-auth may be needed", maxAttempts),
			channels.ChannelFailureKindNetwork, true)
	}()
}

// stopReconnectWatchdog cancels any running reconnect watchdog goroutine.
func (c *Channel) stopReconnectWatchdog() {
	c.reconnectMu.Lock()
	defer c.reconnectMu.Unlock()
	if c.reconnectCancel != nil {
		c.reconnectCancel()
		c.reconnectCancel = nil
	}
}

// hasListenOnlyConfig checks if listen-only mode is enabled at any level (global or per-group).
func (c *Channel) hasListenOnlyConfig() bool {
	if c.config.ListenOnly != nil && *c.config.ListenOnly {
		return true
	}
	for _, grp := range c.config.Groups {
		if grp != nil && grp.ListenOnly != nil && *grp.ListenOnly {
			return true
		}
	}
	return false
}

// isListenOnly checks whether listen-only mode is active for the given chat.
func (c *Channel) isListenOnly(chatID, peerKind string) bool {
	// Per-group config is authoritative for groups.
	if peerKind == "group" && c.config.Groups != nil {
		if grp, ok := c.config.Groups[chatID]; ok && grp != nil {
			if grp.ListenOnly != nil {
				return *grp.ListenOnly
			}
			// Group is explicitly listed but has no listen_only override — default false.
			return false
		}
	}
	// Fall back to global setting (applies to DMs and unlisted groups).
	if c.config.ListenOnly != nil {
		return *c.config.ListenOnly
	}
	return false
}

// effectiveRequireMention returns the effective require_mention setting for a given chat.
// Per-group override takes precedence; nil = inherit from channel-level setting.
func (c *Channel) effectiveRequireMention(chatID, peerKind string) *bool {
	if peerKind == "group" && c.config.Groups != nil {
		if grp, ok := c.config.Groups[chatID]; ok && grp != nil && grp.RequireMention != nil {
			return grp.RequireMention
		}
	}
	return c.config.RequireMention
}

// resolveGraphID returns the graph scope ID for the given chat.
// Multiple chats sharing the same graphID will have their KG data in the same scope.
func (c *Channel) resolveGraphID(chatID, peerKind string) string {
	// Per-group override takes precedence.
	if peerKind == "group" && c.config.Groups != nil {
		if grp, ok := c.config.Groups[chatID]; ok && grp != nil {
			if grp.ListenGraphID != "" {
				return grp.ListenGraphID
			}
		}
	}
	// Fall back to global graph ID.
	if c.config.ListenGraphID != "" {
		return c.config.ListenGraphID
	}
	// Default: use chatID as scope.
	return chatID
}

// resolveAgentID returns the effective agent ID for a chat, applying
// group-specific overrides from config when applicable.
func (c *Channel) resolveAgentID(chatID, peerKind string) string {
	agentID := c.AgentID()
	if peerKind == "group" && c.config.Groups != nil {
		if grp, ok := c.config.Groups[chatID]; ok && grp != nil {
			if grp.AgentID != "" && grp.AgentID != "__default__" {
				agentID = grp.AgentID
			}
		}
	}
	return agentID
}

// RefreshedGroup is a lightweight summary of a joined WhatsApp group.
type RefreshedGroup struct {
	JID              string `json:"jid"`
	Name             string `json:"name"`
	ParticipantCount int    `json:"participant_count"`
}

// RefreshGroups fetches all joined groups from WhatsApp and upserts them as contacts.
// Returns the list of joined groups.
func (c *Channel) RefreshGroups(ctx context.Context) ([]RefreshedGroup, error) {
	if c.client == nil || !c.IsAuthenticated() {
		return nil, fmt.Errorf("whatsapp: not authenticated")
	}

	groups, err := c.client.GetJoinedGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("whatsapp: get joined groups: %w", err)
	}

	cc := c.ContactCollector()
	result := make([]RefreshedGroup, 0, len(groups))
	activeJIDs := make([]string, 0, len(groups))
	for _, g := range groups {
		jidStr := g.JID.String()
		slog.Info("whatsapp: group", "jid", jidStr, "name", g.Name, "isParent", g.IsParent, "participants", g.ParticipantCount)
		activeJIDs = append(activeJIDs, jidStr)
		if cc != nil {
			cc.EnsureContact(ctx, c.Type(), c.Name(), jidStr, "", g.Name, "", "group", "group", "", "")
		}
		result = append(result, RefreshedGroup{
			JID:              jidStr,
			Name:             g.Name,
			ParticipantCount: g.ParticipantCount,
		})
	}

	if cc != nil {
		deleted, err := cc.DeleteStaleGroupContacts(ctx, c.Type(), c.Name(), activeJIDs)
		if err != nil {
			slog.Warn("whatsapp: failed to delete stale group contacts", "error", err)
		} else if deleted > 0 {
			slog.Info("whatsapp: removed stale group contacts", "count", deleted, "channel", c.Name())
		}
	}

	slog.Info("whatsapp: refreshed groups", "count", len(result), "channel", c.Name())
	return result, nil
}

// SetListenOnlyDeps wires the raw message store for listen-only mode.
func (c *Channel) SetListenOnlyDeps(rawMsgStore store.ListenRawMessageStore) {
	if c.listenBuf == nil {
		slog.Warn("whatsapp: SetListenOnlyDeps called but listenBuf is nil — listen-only config not detected at construction",
			"channel", c.Name())
		return
	}
	c.listenBuf.SetRawMsgStore(rawMsgStore)
	c.listenBuf.SetAgentKey(c.AgentID())
	c.listenBuf.SetTenantID(c.TenantID())
	c.listenBuf.SetChannelName("whatsapp")
	slog.Info("whatsapp: listen-only deps wired",
		"channel", c.Name(), "agent", c.AgentID(),
		"rawMsgStore_nil", rawMsgStore == nil)
}

// SetMediaStore wires the media store for persisting listen-only media attachments.
func (c *Channel) SetMediaStore(ms channels.MediaStore) {
	if c.listenBuf == nil {
		return
	}
	c.listenBuf.SetMediaStore(ms)
}

// SetAgentStore wires the agent store used to resolve per-group agent_key overrides to
// UUIDs. Called by InstanceLoader after the channel is constructed.
func (c *Channel) SetAgentStore(s store.AgentStore) {
	c.agentKeyMu.Lock()
	c.agentStore = s
	if c.agentKeyCache == nil {
		c.agentKeyCache = make(map[string]string)
	}
	c.agentKeyMu.Unlock()
}

// SetTenantDBManager wires the tenant DB manager used to resolve the correct per-tenant
// database when looking up override agents. Required for non-master tenants whose agents
// live in a dedicated tenant database. Called by InstanceLoader.
func (c *Channel) SetTenantDBManager(mgr store.TenantDBManager) {
	c.tenantDBMgr = mgr
}

// tenantScopedCtx returns a context carrying this channel's tenant_id with the tenant DB pool
// resolved, mirroring the ctx InstanceLoader builds (WithTenantID + ResolveTenantDB). Agent
// store lookups route to the correct per-tenant database this way.
func (c *Channel) tenantScopedCtx() context.Context {
	ctx := context.Background()
	if tid := c.TenantID(); tid != uuid.Nil {
		ctx = store.WithTenantID(ctx, tid)
	}
	return store.ResolveTenantDB(ctx, c.tenantDBMgr)
}

// ResolveGroupAgentOverrides resolves group override agent_keys to UUIDs using the agent store.
// Called by InstanceLoader after the channel is created and the primary agent is resolved.
// It stores the agent store reference and warms the agent_key → UUID cache from the
// currently-configured groups. Runtime group mutations (e.g. applyJoinRules) do NOT need
// to call this — resolveGroupAgentUUID reads the live config and resolves on demand.
func (c *Channel) ResolveGroupAgentOverrides(ctx context.Context, agentStore store.AgentStore) {
	c.agentKeyMu.Lock()
	c.agentStore = agentStore
	c.agentKeyCache = make(map[string]string)
	c.agentKeyMu.Unlock()
	c.RefreshGroupAgentCache(ctx)
}

// RefreshGroupAgentCache rebuilds the agent_key → UUID cache from the live group config.
// Called on agent create/update (CacheKindAgent) and on channel reload so renamed/recreated
// agents resolve correctly without a gateway restart.
func (c *Channel) RefreshGroupAgentCache(_ context.Context) {
	c.agentKeyMu.RLock()
	as := c.agentStore
	c.agentKeyMu.RUnlock()
	if as == nil {
		return
	}
	c.mu.Lock()
	groups := c.config.Groups
	c.mu.Unlock()
	if groups == nil {
		return
	}
	ctx := c.tenantScopedCtx()
	resolved := make(map[string]string)
	for _, grp := range groups {
		if grp == nil || grp.AgentID == "" || grp.AgentID == "__default__" {
			continue
		}
		ag, err := as.GetByKey(ctx, grp.AgentID)
		if err != nil {
			// Fallback: config may contain a UUID instead of agent_key (e.g. UI WS fallback).
			if id, parseErr := uuid.Parse(grp.AgentID); parseErr == nil {
				ag, err = as.GetByID(ctx, id)
			}
		}
		if err != nil {
			slog.Warn("whatsapp: failed to resolve group override agent",
				"agent_key", grp.AgentID, "error", err)
			continue
		}
		resolved[grp.AgentID] = ag.ID.String()
	}
	c.agentKeyMu.Lock()
	c.agentKeyCache = resolved
	c.agentKeyMu.Unlock()
	slog.Info("whatsapp: group agent cache refreshed",
		"resolved", len(resolved), "channel", c.Name())
}

// resolveGroupAgentUUID returns the agent UUID for a group override, or empty string if the
// group has no override (the caller then falls back to the channel default agent — correct).
//
// This reads the LIVE c.config.Groups, so groups added/changed at runtime (e.g. by
// applyJoinRules) are honored on the very next inbound message, without a full channel
// reload. The agent_key → UUID mapping is cached; a cache miss resolves on demand through
// the agent store and stores the result, so no DB lookup happens per message after warmup.
func (c *Channel) resolveGroupAgentUUID(chatID string) string {
	// Read the override agent_key from the live group config.
	agentKey := ""
	c.mu.Lock()
	if c.config.Groups != nil {
		if grp, ok := c.config.Groups[chatID]; ok && grp != nil {
			if grp.AgentID != "" && grp.AgentID != "__default__" {
				agentKey = grp.AgentID
			}
		}
	}
	c.mu.Unlock()
	if agentKey == "" {
		return ""
	}

	// Fast path: cache hit.
	c.agentKeyMu.RLock()
	uuidStr, ok := c.agentKeyCache[agentKey]
	c.agentKeyMu.RUnlock()
	if ok {
		return uuidStr
	}

	// Slow path: resolve on demand + cache.
	c.agentKeyMu.RLock()
	as := c.agentStore
	c.agentKeyMu.RUnlock()
	if as == nil {
		return ""
	}
	ctx := c.tenantScopedCtx()
	ag, err := as.GetByKey(ctx, agentKey)
	if err != nil {
		if id, parseErr := uuid.Parse(agentKey); parseErr == nil {
			ag, err = as.GetByID(ctx, id)
		}
	}
	if err != nil {
		slog.Warn("whatsapp: failed to resolve group override agent on demand",
			"chat_id", chatID, "agent_key", agentKey, "error", err)
		return ""
	}
	uuidStr = ag.ID.String()
	c.agentKeyMu.Lock()
	c.agentKeyCache[agentKey] = uuidStr
	c.agentKeyMu.Unlock()
	return uuidStr
}
