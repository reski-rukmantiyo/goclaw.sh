//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Compile-time interface check.
var _ store.AuditStore = (*SQLiteAuditStore)(nil)

// SQLiteAuditStore implements store.AuditStore backed by SQLite.
type SQLiteAuditStore struct {
	db *sql.DB
}

// NewSQLiteAuditStore creates a new SQLite-backed audit store.
func NewSQLiteAuditStore(db *sql.DB) *SQLiteAuditStore {
	return &SQLiteAuditStore{db: db}
}

const auditCols = `id, tenant_id, actor_id, action, resource_type, resource_id, group_id, detail, ip_address, user_agent, created_at`

func scanAuditEntry(row interface{ Scan(dest ...any) error }) (*store.AuditLogEntry, error) {
	var e store.AuditLogEntry
	var groupID *string
	var detailStr *string
	createdAt := &sqliteTime{}
	if err := row.Scan(
		&e.ID, &e.TenantID, &e.ActorID, &e.Action,
		&e.ResourceType, &e.ResourceID, &groupID,
		&detailStr, &e.IPAddress, &e.UserAgent, createdAt,
	); err != nil {
		return nil, err
	}
	e.CreatedAt = createdAt.Time
	if groupID != nil {
		gid, err := uuid.Parse(*groupID)
		if err == nil {
			e.GroupID = &gid
		}
	}
	if detailStr != nil {
		// Detail stored as TEXT (JSON string). Unmarshal into any.
		var detail any
		if err := json.Unmarshal([]byte(*detailStr), &detail); err == nil {
			e.Detail = detail
		} else {
			// If not valid JSON, store as raw string.
			e.Detail = *detailStr
		}
	}
	return &e, nil
}

func (s *SQLiteAuditStore) Log(ctx context.Context, entry *store.AuditLogEntry) error {
	if entry.ID == uuid.Nil {
		entry.ID = store.GenNewID()
	}
	now := time.Now().UTC()
	entry.CreatedAt = now

	// Marshal Detail to JSON string for SQLite TEXT column.
	var detailStr *string
	if entry.Detail != nil {
		b, err := json.Marshal(entry.Detail)
		if err == nil {
			s := string(b)
			detailStr = &s
		}
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (id, tenant_id, actor_id, action, resource_type, resource_id, group_id, detail, ip_address, user_agent, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.TenantID, entry.ActorID, entry.Action,
		entry.ResourceType, entry.ResourceID, nilUUID(entry.GroupID),
		detailStr, entry.IPAddress, entry.UserAgent, now,
	)
	return err
}

func (s *SQLiteAuditStore) List(ctx context.Context, tenantID uuid.UUID, params store.AuditListParams) ([]store.AuditLogEntry, int, error) {
	// Build dynamic WHERE clause.
	var conditions []string
	var args []any

	conditions = append(conditions, "tenant_id = ?")
	args = append(args, tenantID)

	if params.Action != "" {
		conditions = append(conditions, "action = ?")
		args = append(args, params.Action)
	}
	if params.ResourceType != "" {
		conditions = append(conditions, "resource_type = ?")
		args = append(args, params.ResourceType)
	}
	if params.ResourceID != nil {
		conditions = append(conditions, "resource_id = ?")
		args = append(args, *params.ResourceID)
	}
	if params.GroupID != nil {
		conditions = append(conditions, "group_id = ?")
		args = append(args, *params.GroupID)
	}
	if params.ActorID != nil {
		conditions = append(conditions, "actor_id = ?")
		args = append(args, *params.ActorID)
	}
	if params.FromTime != nil {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, params.FromTime.UTC().Format(time.RFC3339Nano))
	}
	if params.ToTime != nil {
		conditions = append(conditions, "created_at <= ?")
		args = append(args, params.ToTime.UTC().Format(time.RFC3339Nano))
	}

	where := " WHERE " + strings.Join(conditions, " AND ")

	// Count query.
	var total int
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	countRow := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log`+where, countArgs...)
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, err
	}

	// Data query.
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	query := `SELECT ` + auditCols + ` FROM audit_log` + where + ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []store.AuditLogEntry
	for rows.Next() {
		e, err := scanAuditEntry(rows)
		if err != nil {
			return nil, 0, err
		}
		entries = append(entries, *e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return entries, total, nil
}
