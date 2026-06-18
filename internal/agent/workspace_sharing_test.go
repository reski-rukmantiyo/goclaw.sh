package agent

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestShouldShareWorkspace_NilConfig(t *testing.T) {
	l := &Loop{workspaceSharing: nil}
	if l.shouldShareWorkspace("user1", "direct") {
		t.Error("nil config should never share")
	}
}

func TestShouldShareWorkspace_SharedDM(t *testing.T) {
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{SharedDM: true}}

	if !l.shouldShareWorkspace("user1", "direct") {
		t.Error("shared_dm=true should share for direct peer")
	}
	if l.shouldShareWorkspace("user1", "group") {
		t.Error("shared_dm=true should NOT share for group peer")
	}
}

func TestShouldShareWorkspace_SharedGroup(t *testing.T) {
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{SharedGroup: true}}

	if !l.shouldShareWorkspace("group:telegram:-100", "group") {
		t.Error("shared_group=true should share for group peer")
	}
	if l.shouldShareWorkspace("user1", "direct") {
		t.Error("shared_group=true should NOT share for direct peer")
	}
}

func TestShouldShareWorkspace_SharedUsers(t *testing.T) {
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{
		SharedUsers: []string{"telegram:386246614", "group:telegram:-100"},
	}}

	if !l.shouldShareWorkspace("telegram:386246614", "direct") {
		t.Error("user in shared_users should share regardless of peerKind")
	}
	if !l.shouldShareWorkspace("group:telegram:-100", "group") {
		t.Error("group in shared_users should share")
	}
	if l.shouldShareWorkspace("unknown-user", "direct") {
		t.Error("user NOT in shared_users should not share")
	}
}

func TestShouldShareWorkspace_SharedUsersTakesPriority(t *testing.T) {
	// shared_dm=false, shared_group=false, but user is in shared_users
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{
		SharedDM:    false,
		SharedGroup: false,
		SharedUsers: []string{"special-user"},
	}}

	if !l.shouldShareWorkspace("special-user", "direct") {
		t.Error("shared_users should override shared_dm=false")
	}
	if l.shouldShareWorkspace("other-user", "direct") {
		t.Error("non-listed user should not share when shared_dm=false")
	}
}

func TestShouldShareWorkspace_UnknownPeerKind(t *testing.T) {
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{SharedDM: true, SharedGroup: true}}

	if l.shouldShareWorkspace("user1", "unknown") {
		t.Error("unknown peerKind should default to not sharing")
	}
	if l.shouldShareWorkspace("user1", "") {
		t.Error("empty peerKind should default to not sharing")
	}
}

// shouldShareMemory tests — independent of workspace sharing.
// See 009-bugfix-agent-episodic-recall-not-surfaced.md FR-01.

func boolPtr(b bool) *bool { return &b }

func TestShouldShareMemory_NilConfig(t *testing.T) {
	// nil config + non-predefined agent → not shared.
	l := &Loop{workspaceSharing: nil}
	if l.shouldShareMemory() {
		t.Error("nil config + non-predefined agent should not share memory")
	}
}

func TestShouldShareMemory_Enabled(t *testing.T) {
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{ShareMemory: boolPtr(true)}}
	if !l.shouldShareMemory() {
		t.Error("share_memory=true should share memory")
	}
}

func TestShouldShareMemory_DisabledByDefault(t *testing.T) {
	// ShareMemory unset, non-predefined, no shared KG → not shared.
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{SharedDM: true}}
	if l.shouldShareMemory() {
		t.Error("SharedDM without ShareMemory (non-predefined) should not share memory")
	}
}

func TestShouldShareMemory_IndependentOfWorkspace(t *testing.T) {
	// share_memory=true but no workspace sharing → memory shared, workspace per-user
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{ShareMemory: boolPtr(true)}}
	if !l.shouldShareMemory() {
		t.Error("share_memory should work without workspace sharing")
	}
	if l.shouldShareWorkspace("user1", "direct") {
		t.Error("workspace should NOT be shared when only share_memory is set")
	}

	// workspace shared but share_memory=false → workspace shared, memory per-user
	l2 := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{SharedDM: true, ShareMemory: boolPtr(false)}}
	if l2.shouldShareMemory() {
		t.Error("memory should NOT be shared when share_memory=false (explicit override)")
	}
	if !l2.shouldShareWorkspace("user1", "direct") {
		t.Error("workspace should be shared when SharedDM=true")
	}
}

// FR-01: predefined (shared-context) agents share memory by default so an
// episode created under one invocation user_id (e.g. a WhatsApp group) is
// recallable from any other invocation context (cron, web, another channel).
func TestShouldShareMemory_PredefinedNilConfig(t *testing.T) {
	l := &Loop{agentType: store.AgentTypePredefined, workspaceSharing: nil}
	if !l.shouldShareMemory() {
		t.Error("predefined agent with nil config should share memory")
	}
}

func TestShouldShareMemory_PredefinedUnsetShareMemory(t *testing.T) {
	// Config present but ShareMemory unset (Raka's case: share_knowledge_graph only).
	l := &Loop{agentType: store.AgentTypePredefined,
		workspaceSharing: &store.WorkspaceSharingConfig{ShareKnowledgeGraph: true, SharedKGIDs: []string{"project-x"}}}
	if !l.shouldShareMemory() {
		t.Error("predefined agent with unset share_memory should share memory")
	}
}

func TestShouldShareMemory_PredefinedExplicitFalseOverrides(t *testing.T) {
	// Explicit share_memory=false reverts a predefined agent to per-user memory.
	l := &Loop{agentType: store.AgentTypePredefined,
		workspaceSharing: &store.WorkspaceSharingConfig{ShareMemory: boolPtr(false), ShareKnowledgeGraph: true}}
	if l.shouldShareMemory() {
		t.Error("explicit share_memory=false must override predefined default")
	}
}

func TestShouldShareMemory_OpenAgentDoesNotShare(t *testing.T) {
	l := &Loop{agentType: store.AgentTypeOpen,
		workspaceSharing: &store.WorkspaceSharingConfig{SharedDM: true}}
	if l.shouldShareMemory() {
		t.Error("open (per-user) agent must not share memory by default")
	}
}

func TestShouldShareMemory_SharedKGImpliesSharedMemory(t *testing.T) {
	// Consistency: an agent sharing its KG also shares memory, even if open.
	l := &Loop{agentType: store.AgentTypeOpen,
		workspaceSharing: &store.WorkspaceSharingConfig{ShareKnowledgeGraph: true}}
	if !l.shouldShareMemory() {
		t.Error("share_knowledge_graph=true should imply shared memory")
	}
}

func TestShouldShareWorkspace_BothEnabled(t *testing.T) {
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{
		SharedDM:    true,
		SharedGroup: true,
		SharedUsers: []string{"extra-user"},
	}}

	if !l.shouldShareWorkspace("user1", "direct") {
		t.Error("should share DM")
	}
	if !l.shouldShareWorkspace("group:tg:-100", "group") {
		t.Error("should share group")
	}
	if !l.shouldShareWorkspace("extra-user", "direct") {
		t.Error("should share listed user")
	}
}
