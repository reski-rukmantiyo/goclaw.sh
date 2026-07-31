package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.mau.fi/whatsmeow"
)

// ErrAlreadyPairedDisconnected is returned by StartQRFlow when the device store
// still holds a paired identity (client.Store.ID != nil) but the account is not
// connected. whatsmeow's GetQRChannel refuses to issue a QR in this state
// (ErrQRStoreContainsID), so the caller must clear the identity via Reauth (the
// force_reauth path) before retrying, or reconnect the existing session.
// Surfaced to the UI as the structured reason "already_paired_disconnected"
// rather than leaking whatsmeow's raw internal error.
var ErrAlreadyPairedDisconnected = errors.New("whatsapp: device already paired but disconnected; re-link (force reauth) to clear the session before QR scan")

// StartQRFlow initiates the QR authentication flow.
// Returns a channel that emits QR code strings and auth events.
// Lazily initializes the whatsmeow client if Start() hasn't been called yet
// (handles timing race between async instance reload and wizard auto-start).
// Serialized with Reauth via reauthMu to prevent races on rapid double-clicks.
func (c *Channel) StartQRFlow(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error) {
	c.reauthMu.Lock()
	defer c.reauthMu.Unlock()
	if c.clientNeedsRecreate() {
		// Lazy init (wizard fired QR before Start) OR replace a deleted device.
		// whatsmeow sets Store.Deleted=true and removes the DB row on signout
		// (events.LoggedOut -> cli.Store.Delete in connectionevents.go). A
		// deleted client cannot be Connected (Connect returns ErrDeviceDeleted),
		// so swap in a fresh device store before the QR flow. Signout already
		// destroyed the old identity, so this is recovery, not a destructive
		// re-pair — no operator prompt needed (unlike Store.ID != nil below).
		c.mu.Lock()
		if c.clientNeedsRecreate() {
			if c.ctx == nil {
				c.ctx, c.cancel = context.WithCancel(context.Background())
			}
			if c.client != nil {
				c.client.Disconnect()
				slog.Info("whatsapp: replacing deleted device store for QR flow", "channel", c.Name())
			}
			deviceStore, err := c.container.GetFirstDevice(ctx)
			if err != nil {
				c.mu.Unlock()
				return nil, fmt.Errorf("whatsapp get device: %w", err)
			}
			c.client = whatsmeow.NewClient(deviceStore, c.whatsmeowLogger())
			c.client.AddEventHandler(c.handleEvent)
		}
		c.mu.Unlock()
	}

	if c.IsAuthenticated() {
		return nil, nil // caller checks this
	}

	// whatsmeow GetQRChannel requires Store.ID == nil. If a paired identity
	// lingers while the account is disconnected (e.g. logged out from the
	// phone, evicted companion, or mid-reconnect), GetQRChannel would return
	// ErrQRStoreContainsID. Surface a structured error instead so the caller
	// can prompt the operator to re-link (which clears Store.ID via Reauth)
	// rather than replaying the failing QR request.
	if c.client.Store.ID != nil {
		return nil, ErrAlreadyPairedDisconnected
	}

	// GetQRChannel must run BEFORE Connect. A prior QR attempt or the reconnect
	// watchdog may have left this client connected; calling GetQRChannel on a
	// connected client returns ErrQRAlreadyConnected. We only reach here when
	// !IsAuthenticated() && Store.ID == nil, so any live socket is an anonymous
	// pre-pairing one — safe to tear down so GetQRChannel can drive a fresh
	// pairing connection. (A paired+connected client never reaches here: it is
	// caught by the IsAuthenticated() early-return above or the Store.ID guard.)
	if c.client.IsConnected() {
		c.client.Disconnect()
	}

	qrChan, err := c.client.GetQRChannel(ctx)
	if err != nil {
		return nil, fmt.Errorf("whatsapp get QR channel: %w", err)
	}

	if !c.client.IsConnected() {
		if err := c.client.Connect(); err != nil {
			return nil, fmt.Errorf("whatsapp connect for QR: %w", err)
		}
	}

	return qrChan, nil
}

// clientNeedsRecreate reports whether the whatsmeow client must be (re)created
// before a QR flow can run:
//   - client == nil: never started (wizard fired QR before Start).
//   - Store.Deleted: the account was signed out. whatsmeow marks Store.Deleted
//     and removes the DB row on events.LoggedOut; such a client cannot be
//     Connected (Connect returns store.ErrDeviceDeleted), so it must be replaced
//     with a fresh device store (GetFirstDevice returns a brand-new device when
//     the container is empty).
func (c *Channel) clientNeedsRecreate() bool {
	return c.client == nil || c.client.Store.Deleted
}

// Reauth clears the current session and prepares for a fresh QR scan.
// Serialized with StartQRFlow via reauthMu to prevent races on rapid double-clicks.
func (c *Channel) Reauth() error {
	c.reauthMu.Lock()
	defer c.reauthMu.Unlock()

	slog.Info("whatsapp: reauth requested", "channel", c.Name())

	c.lastQRMu.Lock()
	c.waAuthenticated = false
	c.lastQRB64 = ""
	c.lastQRMu.Unlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		c.client.Disconnect()
	}

	// Delete device from store to force fresh QR on next connect.
	// A failed delete is a hard error: if Store.ID survives, the next
	// GetQRChannel call will fail with ErrQRStoreContainsID, so the operator
	// gets stuck relinking. Fail fast with a structured reason instead.
	if c.client != nil && c.client.Store.ID != nil {
		if err := c.client.Store.Delete(context.Background()); err != nil {
			return fmt.Errorf("whatsapp: delete paired device store: %w", err)
		}
	}

	// Reset context so the new client gets a fresh lifecycle.
	if c.cancel != nil {
		c.cancel()
	}
	// Use parentCtx if available so the new lifecycle is still bound to the gateway.
	parent := c.parentCtx
	if parent == nil {
		parent = context.Background()
	}
	c.ctx, c.cancel = context.WithCancel(parent)

	// Re-create client with fresh device store.
	deviceStore, err := c.container.GetFirstDevice(context.Background())
	if err != nil {
		return fmt.Errorf("whatsapp: get fresh device: %w", err)
	}
	c.client = whatsmeow.NewClient(deviceStore, c.whatsmeowLogger())
	c.client.AddEventHandler(c.handleEvent)

	return nil
}
