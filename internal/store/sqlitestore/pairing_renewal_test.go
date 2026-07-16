//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// newPairingTestStore builds an isolated SQLite pairing store with the given
// ttl/window (SRS 012). Schema (incl. master tenant seed) is ensured.
func newPairingTestStore(t *testing.T, ttl, window time.Duration) (*SQLitePairingStore, context.Context, *sql.DB) {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "pairing.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	return NewSQLitePairingStore(db, ttl, window), ctx, db
}

func insertPaired(t *testing.T, db *sql.DB, tenant uuid.UUID, senderID, channel string, expiresAt interface{}) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO paired_devices (id, sender_id, channel, chat_id, paired_by, paired_at, metadata, expires_at, tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.Must(uuid.NewV7()), senderID, channel, "chat", "tester", time.Now().Round(0), "{}", expiresAt, tenant,
	)
	if err != nil {
		t.Fatalf("insert paired device: %v", err)
	}
}

func readExpiry(t *testing.T, db *sql.DB, senderID, channel string, tenant uuid.UUID) sql.NullString {
	t.Helper()
	var ns sql.NullString
	err := db.QueryRow(
		"SELECT expires_at FROM paired_devices WHERE sender_id=? AND channel=? AND tenant_id=?",
		senderID, channel, tenant,
	).Scan(&ns)
	if err != nil {
		t.Fatalf("read expires_at: %v", err)
	}
	return ns
}

func expiryMillis(t *testing.T, ns sql.NullString) int64 {
	t.Helper()
	if !ns.Valid || ns.String == "" {
		t.Fatalf("expected valid expiry, got %+v", ns)
	}
	return parseTimeToMillis(ns.String)
}

// TestSQLitePairingStore_RenewsWithinWindow — FR-00: a device whose expiry is
// within the renewal window has its expires_at advanced to ~now+ttl on an
// IsPaired hit (sliding renewal).
func TestSQLitePairingStore_RenewsWithinWindow(t *testing.T) {
	ttl := 10 * time.Minute
	window := 5 * time.Minute
	s, ctx, db := newPairingTestStore(t, ttl, window)

	sender := "s-within"
	insertPaired(t, db, store.MasterTenantID, sender, "telegram", time.Now().Add(2*time.Minute).Round(0))
	before := expiryMillis(t, readExpiry(t, db, sender, "telegram", store.MasterTenantID))

	paired, err := s.IsPaired(ctx, sender, "telegram")
	if err != nil || !paired {
		t.Fatalf("IsPaired=%v err=%v", paired, err)
	}

	after := expiryMillis(t, readExpiry(t, db, sender, "telegram", store.MasterTenantID))
	if after <= before {
		t.Fatalf("expiry not advanced: before=%d after=%d", before, after)
	}
	// renewed expiry should be ~now+ttl (within ±3 min tolerance).
	if d := time.Duration(after-time.Now().UnixMilli()) * time.Millisecond; d < ttl-3*time.Minute || d > ttl+3*time.Minute {
		t.Errorf("renewed expiry not ~now+ttl: got %v want ~%v", d, ttl)
	}
}

// TestSQLitePairingStore_NoRenewalOutsideWindow — FR-00: a fresh device whose
// expiry is outside the window is NOT written, even across 100 hits (no churn;
// the window gate bounds renewal to ≤~1/week/device).
func TestSQLitePairingStore_NoRenewalOutsideWindow(t *testing.T) {
	ttl := 10 * time.Minute
	window := 5 * time.Minute
	s, ctx, db := newPairingTestStore(t, ttl, window)

	sender := "s-fresh"
	insertPaired(t, db, store.MasterTenantID, sender, "telegram", time.Now().Add(8*time.Minute).Round(0)) // > window
	before := expiryMillis(t, readExpiry(t, db, sender, "telegram", store.MasterTenantID))

	for i := 0; i < 100; i++ {
		paired, err := s.IsPaired(ctx, sender, "telegram")
		if err != nil || !paired {
			t.Fatalf("iter %d: IsPaired=%v err=%v", i, paired, err)
		}
	}
	after := expiryMillis(t, readExpiry(t, db, sender, "telegram", store.MasterTenantID))
	if after != before {
		t.Errorf("fresh device expiry changed after 100 hits: before=%d after=%d", before, after)
	}
}

// TestSQLitePairingStore_WindowGateStopsRepeatRenewal — FR-00: after a renewal
// the expiry is moved outside the window, so an immediate second hit is a no-op
// (the gate yields at most one renewal per window entry).
func TestSQLitePairingStore_WindowGateStopsRepeatRenewal(t *testing.T) {
	s, ctx, db := newPairingTestStore(t, 10*time.Minute, 5*time.Minute)
	sender := "s-gate"
	insertPaired(t, db, store.MasterTenantID, sender, "telegram", time.Now().Add(2*time.Minute).Round(0))

	if _, err := s.IsPaired(ctx, sender, "telegram"); err != nil {
		t.Fatal(err)
	}
	after1 := expiryMillis(t, readExpiry(t, db, sender, "telegram", store.MasterTenantID))

	if _, err := s.IsPaired(ctx, sender, "telegram"); err != nil {
		t.Fatal(err)
	}
	after2 := expiryMillis(t, readExpiry(t, db, sender, "telegram", store.MasterTenantID))
	if after2 != after1 {
		t.Errorf("repeat renewal occurred (gate failed): %d -> %d", after1, after2)
	}
}

// TestSQLitePairingStore_NeverExpireNull — FR-00/FR-01: a NULL expiry device is
// admitted forever and never renewed.
func TestSQLitePairingStore_NeverExpireNull(t *testing.T) {
	s, ctx, db := newPairingTestStore(t, 10*time.Minute, 5*time.Minute)
	sender := "s-never"
	insertPaired(t, db, store.MasterTenantID, sender, "telegram", nil) // NULL = never expire

	paired, err := s.IsPaired(ctx, sender, "telegram")
	if err != nil || !paired {
		t.Fatalf("NULL-expiry device not admitted: paired=%v err=%v", paired, err)
	}
	ns := readExpiry(t, db, sender, "telegram", store.MasterTenantID)
	if ns.Valid {
		t.Errorf("never-expire (NULL) device got renewed: %v", ns.String)
	}
}

// TestSQLitePairingStore_ConfigurableTTLNever — FR-01: ttl=0 makes ApprovePairing
// write expires_at = NULL (never expire), and ListPaired reports ExpiresAt nil.
func TestSQLitePairingStore_ConfigurableTTLNever(t *testing.T) {
	s, ctx, db := newPairingTestStore(t, 0, 0) // never expire

	code, err := s.RequestPairing(ctx, "s-approve", "telegram", "chat", "default", nil)
	if err != nil {
		t.Fatalf("RequestPairing: %v", err)
	}
	if _, err := s.ApprovePairing(ctx, code, "tester"); err != nil {
		t.Fatalf("ApprovePairing: %v", err)
	}
	ns := readExpiry(t, db, "s-approve", "telegram", store.MasterTenantID)
	if ns.Valid {
		t.Errorf("ttl=0 approve should write NULL expires_at, got %v", ns.String)
	}
	for _, p := range s.ListPaired(ctx) {
		if p.SenderID == "s-approve" && p.ExpiresAt != nil {
			t.Errorf("ListPaired ExpiresAt should be nil for never-expire, got %v", p.ExpiresAt)
		}
	}
}

// TestSQLitePairingStore_ConfigurableTTLFinite — FR-01/FR-02: a finite ttl sets a
// finite expires_at on approve, and ListPaired surfaces ExpiresAt.
func TestSQLitePairingStore_ConfigurableTTLFinite(t *testing.T) {
	s, ctx, db := newPairingTestStore(t, time.Hour, 0) // 1h ttl, auto window 15m

	code, err := s.RequestPairing(ctx, "s-ttl", "telegram", "chat", "default", nil)
	if err != nil {
		t.Fatalf("RequestPairing: %v", err)
	}
	if _, err := s.ApprovePairing(ctx, code, "tester"); err != nil {
		t.Fatalf("ApprovePairing: %v", err)
	}
	ns := readExpiry(t, db, "s-ttl", "telegram", store.MasterTenantID)
	if !ns.Valid {
		t.Fatal("expected finite expires_at for ttl=1h")
	}
	var surfaced bool
	for _, p := range s.ListPaired(ctx) {
		if p.SenderID == "s-ttl" {
			surfaced = true
			if p.ExpiresAt == nil {
				t.Error("ListPaired ExpiresAt nil for finite-expiry device")
			}
		}
	}
	if !surfaced {
		t.Error("approved device not present in ListPaired")
	}
}

// TestSQLitePairingStore_RenewalTenantIsolation — FR-04: a renewal hit scoped to
// tenant A does not admit or renew tenant B's device.
func TestSQLitePairingStore_RenewalTenantIsolation(t *testing.T) {
	s, ctx, db := newPairingTestStore(t, 10*time.Minute, 5*time.Minute)
	other := uuid.Must(uuid.NewV7())
	if _, err := db.Exec("INSERT INTO tenants (id, name, slug, status) VALUES (?, ?, ?, ?)",
		other, "Other", "other", "active"); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	insertPaired(t, db, other, "s-other", "telegram", time.Now().Add(2*time.Minute).Round(0)) // within window
	before := expiryMillis(t, readExpiry(t, db, "s-other", "telegram", other))

	paired, err := s.IsPaired(ctx, "s-other", "telegram") // ctx = master tenant
	if err != nil {
		t.Fatalf("IsPaired err: %v", err)
	}
	if paired {
		t.Error("cross-tenant device reported as paired")
	}
	after := expiryMillis(t, readExpiry(t, db, "s-other", "telegram", other))
	if after != before {
		t.Errorf("cross-tenant device renewed: before=%d after=%d", before, after)
	}
}
