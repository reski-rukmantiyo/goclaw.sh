package whatsapp

import (
	"context"
	"errors"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

// TestStartQRFlow_PairedButDisconnected proves the core fix for the QR-rescan
// bug (docs/srs/013-bugfix-whatsapp-qr-rescan-paired-session.md): when the
// device store still holds a paired identity (Store.ID != nil) but the account
// is not connected, StartQRFlow must return the structured
// ErrAlreadyPairedDisconnected sentinel instead of leaking whatsmeow's raw
// ErrQRStoreContainsID ("GetQRChannel can only be called when there's no user
// ID in the client's Store"). The guard returns before any DB/network call, so
// no container or live connection is required.
func TestStartQRFlow_PairedButDisconnected(t *testing.T) {
	jid := types.JID{User: "6281234567890", Server: "s.whatsapp.net"}
	c := &Channel{
		client: whatsmeow.NewClient(&store.Device{ID: &jid}, nil),
		// waAuthenticated left at zero value (false) → paired but disconnected.
	}

	_, err := c.StartQRFlow(context.Background())
	if !errors.Is(err, ErrAlreadyPairedDisconnected) {
		t.Fatalf("expected ErrAlreadyPairedDisconnected, got %v", err)
	}
}

// TestStartQRFlow_NotAuthenticatedFlagDoesNotSuppressPairedGuard ensures the
// IsAuthenticated() connection check does not mask the pairing-state guard:
// a disconnected client (waAuthenticated=false) with a paired identity still
// hits the guard rather than proceeding to GetQRChannel.
func TestStartQRFlow_NotAuthenticatedFlagDoesNotSuppressPairedGuard(t *testing.T) {
	c := &Channel{
		client:        whatsmeow.NewClient(&store.Device{ID: &types.JID{}}, nil),
		waAuthenticated: false,
	}

	_, err := c.StartQRFlow(context.Background())
	// types.JID{} is still a non-nil pointer target → Store.ID != nil → guard fires.
	if !errors.Is(err, ErrAlreadyPairedDisconnected) {
		t.Fatalf("expected ErrAlreadyPairedDisconnected even with empty JID, got %v", err)
	}
}

// TestClientNeedsRecreate covers the second QR-rescan failure mode (docs/srs/013
// §3.8): after a signout, whatsmeow marks Store.Deleted=true (and removes the DB
// row). A deleted client cannot be Connected (Connect returns
// store.ErrDeviceDeleted), so StartQRFlow must recreate it. These tests cover
// the recreate decision; the full recreate (GetFirstDevice) needs a container
// and is validated by build + live rescan.
func TestClientNeedsRecreate(t *testing.T) {
	t.Run("nil client", func(t *testing.T) {
		c := &Channel{}
		if !c.clientNeedsRecreate() {
			t.Fatal("nil client must need recreate")
		}
	})
	t.Run("fresh device", func(t *testing.T) {
		c := &Channel{client: whatsmeow.NewClient(&store.Device{}, nil)}
		if c.clientNeedsRecreate() {
			t.Fatal("fresh (non-deleted) device must not need recreate")
		}
	})
	t.Run("deleted device", func(t *testing.T) {
		dev := &store.Device{}
		c := &Channel{client: whatsmeow.NewClient(dev, nil)}
		dev.Deleted = true // whatsmeow sets this on events.LoggedOut
		if !c.clientNeedsRecreate() {
			t.Fatal("deleted device must need recreate")
		}
	})
}
