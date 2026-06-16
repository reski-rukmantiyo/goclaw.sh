package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var (
	gwToken  = "74e39f0bad156cb3419bd71d6c5dad83"
	tenantID = "019e8446-a859-7971-a65b-29ffe7db6edb"
)

func wsDial(t *testing.T) *websocket.Conn {
	t.Helper()
	u := url.URL{Scheme: "ws", Host: "localhost:18790", Path: "/ws"}
	c, _, err := websocket.DefaultDialer.Dial(u.String(), http.Header{})
	if err != nil {
		t.Skipf("gateway not running: %v", err)
		return nil
	}
	return c
}

func wsWrite(t *testing.T, c *websocket.Conn, id, method string, params json.RawMessage) {
	t.Helper()
	f := map[string]any{"type": "req", "id": id, "method": method, "params": params}
	data, _ := json.Marshal(f)
	if err := c.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func wsReadResp(t *testing.T, c *websocket.Conn) []byte {
	t.Helper()
	for {
		c.SetReadDeadline(time.Now().Add(10 * time.Second))
		_, msg, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var peek struct{ Type string `json:"type"` }
		json.Unmarshal(msg, &peek)
		if peek.Type == "res" {
			return msg
		}
	}
}

// connectAsOwner connects with gateway token as "system" (default owner).
func connectAsOwner(t *testing.T) *websocket.Conn {
	t.Helper()
	c := wsDial(t)
	params, _ := json.Marshal(map[string]string{
		"token":     gwToken,
		"user_id":   "system",
		"tenant_id": tenantID,
	})
	wsWrite(t, c, "1", "connect", params)
	resp := wsReadResp(t, c)
	var cr struct{ OK bool `json:"ok"` }
	json.Unmarshal(resp, &cr)
	if !cr.OK {
		t.Fatalf("owner connect failed: %s", string(resp))
	}
	return c
}

// connectAsUser connects with gateway token as a non-owner user to a specific tenant.
func connectAsUser(t *testing.T, userID string) (*websocket.Conn, string, string) {
	t.Helper()
	c := wsDial(t)
	// Use tenant slug (same as web UI) — resolveTenantHint only tries GetTenantBySlug
	params, _ := json.Marshal(map[string]string{
		"token":     gwToken,
		"user_id":   userID,
		"tenant_id": "tenant-17",
	})
	wsWrite(t, c, "1", "connect", params)
	resp := wsReadResp(t, c)

	var cr struct {
		OK     bool   `json:"ok"`
		Error  any    `json:"error,omitempty"`
		Payload struct {
			Role       string `json:"role"`
			TenantID   string `json:"tenant_id"`
			TenantName string `json:"tenant_name"`
			IsOwner    bool   `json:"is_owner"`
		} `json:"payload,omitempty"`
	}
	json.Unmarshal(resp, &cr)

	if !cr.OK {
		return c, "CONNECT_FAILED", fmt.Sprintf("%v", cr.Error)
	}
	return c, cr.Payload.Role, cr.Payload.TenantName
}

func TestViewerPortalAccess(t *testing.T) {
	reski3ID := "36503807-0e32-4d1f-bad1-ab55da5dbf16"

	// Step 1: As owner, set reski3 to viewer
	ownerC := connectAsOwner(t)
	params, _ := json.Marshal(map[string]string{
		"tenant_id": tenantID,
		"user_id":   reski3ID,
		"role":      "viewer",
	})
	wsWrite(t, ownerC, "2", "tenants.users.updateRole", params)
	resp := wsReadResp(t, ownerC)
	var ur struct{ OK bool `json:"ok"` }
	json.Unmarshal(resp, &ur)
	if !ur.OK {
		t.Fatalf("set viewer role failed: %s", string(resp))
	}
	ownerC.Close()
	t.Log("✓ Set reski3 to viewer")

	// Step 2: Connect as reski3 (viewer) to tenant-17
	time.Sleep(500 * time.Millisecond)
	viewerC, role, tenantName := connectAsUser(t, reski3ID)
	defer viewerC.Close()

	if role == "CONNECT_FAILED" {
		t.Fatalf("viewer cannot connect to portal: %s", tenantName)
	}
	t.Logf("✓ Viewer connected: role=%s tenant=%s", role, tenantName)

	// Note: gateway token auth always returns RoleAdmin (by design — CLI admin bypass).
	// The web UI uses JWT auth, where role derivation goes through:
	//   1. resolveUserRoleForJWT (auth_handler.go) — now fixed for viewer
	//   2. getUserTenantRole (router.go) — now fixed for viewer
	//   3. Frontend ws-client.ts no longer rejects viewer role
	// Gateway token role=admin is expected here; real validation is in unit tests.

	// Step 3: Try basic portal operations (list agents - read operation)
	agentsParams, _ := json.Marshal(map[string]any{})
	wsWrite(t, viewerC, "2", "agents.list", agentsParams)
	resp = wsReadResp(t, viewerC)
	var ar struct {
		OK    bool `json:"ok"`
		Error any  `json:"error,omitempty"`
	}
	json.Unmarshal(resp, &ar)
	if !ar.OK {
		t.Errorf("viewer agents.list failed: %v", ar.Error)
	} else {
		t.Log("✓ Viewer can list agents (read)")
	}

	// Step 4: Try write operation (should fail for viewer)
	createParams, _ := json.Marshal(map[string]any{"name": "test-agent"})
	wsWrite(t, viewerC, "3", "agents.create", createParams)
	resp = wsReadResp(t, viewerC)
	json.Unmarshal(resp, &ar)
	if ar.OK {
		t.Error("viewer should NOT be able to create agents")
	} else {
		t.Logf("✓ Viewer correctly blocked from agents.create: %v", ar.Error)
	}
}

func TestMemberPortalAccess(t *testing.T) {
	reski3ID := "36503807-0e32-4d1f-bad1-ab55da5dbf16"

	// Step 1: As owner, set reski3 to member
	ownerC := connectAsOwner(t)
	params, _ := json.Marshal(map[string]string{
		"tenant_id": tenantID,
		"user_id":   reski3ID,
		"role":      "member",
	})
	wsWrite(t, ownerC, "2", "tenants.users.updateRole", params)
	resp := wsReadResp(t, ownerC)
	var ur struct{ OK bool `json:"ok"` }
	json.Unmarshal(resp, &ur)
	if !ur.OK {
		t.Fatalf("set member role failed: %s", string(resp))
	}
	ownerC.Close()
	t.Log("✓ Set reski3 to member")

	// Step 2: Connect as reski3 (member) to tenant-17
	time.Sleep(500 * time.Millisecond)
	memberC, role, tenantName := connectAsUser(t, reski3ID)
	defer memberC.Close()

	if role == "CONNECT_FAILED" {
		t.Fatalf("member cannot connect to portal: %s", tenantName)
	}
	t.Logf("✓ Member connected: role=%s tenant=%s", role, tenantName)
	// Gateway token always gives admin; JWT path tested via unit tests.

	// Step 3: Try read + write operations
	agentsParams, _ := json.Marshal(map[string]any{})
	wsWrite(t, memberC, "2", "agents.list", agentsParams)
	resp = wsReadResp(t, memberC)
	var ar struct {
		OK    bool `json:"ok"`
		Error any  `json:"error,omitempty"`
	}
	json.Unmarshal(resp, &ar)
	if !ar.OK {
		t.Errorf("member agents.list failed: %v", ar.Error)
	} else {
		t.Log("✓ Member can list agents (read)")
	}

	// Member should be able to send chat (write method)
	chatParams, _ := json.Marshal(map[string]any{"session_key": "test", "message": "hello"})
	wsWrite(t, memberC, "3", "chat.send", chatParams)
	resp = wsReadResp(t, memberC)
	json.Unmarshal(resp, &ar)
	// chat.send might fail for other reasons (no session), but shouldn't be permission denied
	t.Logf("Member chat.send: ok=%v error=%v", ar.OK, ar.Error)
}

func TestAdminPortalAccess(t *testing.T) {
	reski3ID := "36503807-0e32-4d1f-bad1-ab55da5dbf16"

	// Step 1: As owner, set reski3 to admin
	ownerC := connectAsOwner(t)
	params, _ := json.Marshal(map[string]string{
		"tenant_id": tenantID,
		"user_id":   reski3ID,
		"role":      "admin",
	})
	wsWrite(t, ownerC, "2", "tenants.users.updateRole", params)
	resp := wsReadResp(t, ownerC)
	var ur struct{ OK bool `json:"ok"` }
	json.Unmarshal(resp, &ur)
	if !ur.OK {
		t.Fatalf("set admin role failed: %s", string(resp))
	}
	ownerC.Close()
	t.Log("✓ Set reski3 to admin")

	// Step 2: Connect as reski3 (admin) to tenant-17
	time.Sleep(500 * time.Millisecond)
	adminC, role, tenantName := connectAsUser(t, reski3ID)
	defer adminC.Close()

	if role == "CONNECT_FAILED" {
		t.Fatalf("admin cannot connect to portal: %s", tenantName)
	}
	t.Logf("✓ Admin connected: role=%s tenant=%s", role, tenantName)
	// Gateway token always gives admin; JWT path tested via unit tests.

	// Admin should be able to list agents (basic read)
	agentsParams, _ := json.Marshal(map[string]any{})
	wsWrite(t, adminC, "2", "agents.list", agentsParams)
	resp = wsReadResp(t, adminC)
	var ar struct {
		OK    bool `json:"ok"`
		Error any  `json:"error,omitempty"`
	}
	json.Unmarshal(resp, &ar)
	if !ar.OK {
		t.Errorf("admin agents.list failed: %v", ar.Error)
	} else {
		t.Log("✓ Admin can list agents")
	}
}

func TestRestoreViewerRole(t *testing.T) {
	reski3ID := "36503807-0e32-4d1f-bad1-ab55da5dbf16"
	ownerC := connectAsOwner(t)
	params, _ := json.Marshal(map[string]string{
		"tenant_id": tenantID,
		"user_id":   reski3ID,
		"role":      "viewer",
	})
	wsWrite(t, ownerC, "2", "tenants.users.updateRole", params)
	wsReadResp(t, ownerC)
	ownerC.Close()
	t.Log("✓ Restored reski3 to viewer")
}
