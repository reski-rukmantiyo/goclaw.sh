//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func testMasterDSN() string {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = defaultTestDSN
	}
	return dsn
}

func dropTestTenantDB(t *testing.T, db *sql.DB, dbName string) {
	t.Helper()
	_, _ = db.Exec(fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid()", dbName))
	_, _ = db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS \"%s\"", dbName))
}

func dropTestTenantRole(t *testing.T, db *sql.DB, username string) {
	t.Helper()
	_, _ = db.Exec(fmt.Sprintf("DROP ROLE IF EXISTS \"%s\"", username))
}

func TestProvisionTenantDB(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	tenantID := uuid.New()
	slug := "test-provision-" + tenantID.String()[:8]

	t.Cleanup(func() {
		dbName := fmt.Sprintf("goclaw_tenant_%s_%s", slug, tenantID.String()[:8])
		username := fmt.Sprintf("goclaw_%s", slug)
		dropTestTenantDB(t, db, dbName)
		dropTestTenantRole(t, db, username)
	})

	dbName := fmt.Sprintf("goclaw_tenant_%s_%s", slug, tenantID.String()[:8])
	username := fmt.Sprintf("goclaw_%s", slug)
	dropTestTenantDB(t, db, dbName)
	dropTestTenantRole(t, db, username)

	conn, err := pg.ProvisionTenantDB(ctx, db, tenantID, slug, nil, "", "disable", testMasterDSN())
	if err != nil {
		t.Fatalf("ProvisionTenantDB failed: %v", err)
	}

	if conn.TenantID != tenantID {
		t.Errorf("tenant_id mismatch: got %v, want %v", conn.TenantID, tenantID)
	}
	if conn.DatabaseName != dbName {
		t.Errorf("dbname mismatch: got %q, want %q", conn.DatabaseName, dbName)
	}
	if conn.Username != username {
		t.Errorf("username mismatch: got %q, want %q", conn.Username, username)
	}
	if conn.SSLMode != "disable" {
		t.Errorf("sslmode mismatch: got %q, want %q", conn.SSLMode, "disable")
	}

	tenantDB, err := pg.OpenDB(conn.DSN())
	if err != nil {
		t.Fatalf("open tenant db failed: %v", err)
	}
	defer tenantDB.Close()

	for _, table := range []string{"agents", "sessions", "tenants", "tenant_users", "llm_providers"} {
		var exists bool
		err := tenantDB.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name = $1)", table,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("check table %s existence: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s does not exist in tenant db", table)
		}
	}

	for _, ext := range []string{"uuid-ossp", "pgcrypto", "vector"} {
		var exists bool
		err := tenantDB.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = $1)", ext,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("check extension %s: %v", ext, err)
		}
		if !exists {
			t.Errorf("extension %s not installed in tenant db", ext)
		}
	}
}

func TestTenantNameUpdate(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	s := pg.NewPGTenantStore(db)

	tenant := &store.TenantData{
		ID:     store.GenNewID(),
		Name:   "Original Name",
		Slug:   "test-update-" + store.GenNewID().String()[:8],
		Status: store.TenantStatusActive,
	}

	if err := s.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	fetched, err := s.GetTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("get tenant: %v", err)
	}
	if fetched.Name != "Original Name" {
		t.Errorf("initial name mismatch: got %q, want %q", fetched.Name, "Original Name")
	}

	if err := s.UpdateTenant(ctx, tenant.ID, map[string]any{"name": "Updated Name"}); err != nil {
		t.Fatalf("update tenant name: %v", err)
	}

	updated, err := s.GetTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("get tenant after update: %v", err)
	}
	if updated.Name != "Updated Name" {
		t.Errorf("updated name mismatch: got %q, want %q", updated.Name, "Updated Name")
	}

	if err := s.UpdateTenant(ctx, tenant.ID, map[string]any{"slug": "new-slug"}); err == nil {
		t.Error("expected error when updating slug, got nil")
	}
}

func TestTenantDeletion(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	s := pg.NewPGTenantStore(db)

	tenant := &store.TenantData{
		ID:     store.GenNewID(),
		Name:   "Tenant To Delete",
		Slug:   "test-delete-" + store.GenNewID().String()[:8],
		Status: store.TenantStatusActive,
	}

	if err := s.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	if err := s.AddUser(ctx, tenant.ID, "test-user-1", store.TenantRoleMember); err != nil {
		t.Fatalf("add user: %v", err)
	}

	users, err := s.ListUsers(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}

	if err := s.DeleteTenant(ctx, tenant.ID); err != nil {
		t.Fatalf("delete tenant: %v", err)
	}

	_, err = s.GetTenant(ctx, tenant.ID)
	if err == nil {
		t.Error("expected error getting deleted tenant, got nil")
	}

	users, err = s.ListUsers(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("list users after delete: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("expected 0 users after delete, got %d", len(users))
	}
}
