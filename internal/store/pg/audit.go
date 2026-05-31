package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Compile-time interface check.
var _ store.AuditStore = (*PGAuditStore)(nil)

// PGAuditStore implements store.AuditStore backed by Postgres.
type PGAuditStore struct {
	db *sql.DB
}

// NewPGAuditStore creates a new PGAuditStore.
func NewPGAuditStore(db *sql.DB) *PGAuditStore {
	return &PGAuditStore{db: db}
}

func (s *PGAuditStore) dbFor(ctx context.Context) *sql.DB {
	if db := store.TenantDBFromContext(ctx); db != nil {
		return db
	}
	return s.db
}

func (s *PGAuditStore) Log(ctx context.Context, entry *store.AuditLogEntry) error {
	if entry.ID == uuid.Nil {
		entry.ID = store.GenNewID()
	}

	var detail any
	if entry.Detail != nil {
		b, err := json.Marshal(entry.Detail)
		if err != nil {
			return fmt.Errorf("marshal audit detail: %w", err)
		}
		detail = b
	}

	_, err := s.dbFor(ctx).ExecContext(ctx,
		`INSERT INTO audit_log (id, tenant_id, actor_id, action, resource_type, resource_id, group_id, detail, ip_address, user_agent, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		entry.ID, entry.TenantID, nilUUID(entry.ActorID), entry.Action,
		entry.ResourceType, entry.ResourceID,
		nilUUID(entry.GroupID), detail,
		nilStr(derefStrPtr(entry.IPAddress)),
		nilStr(derefStrPtr(entry.UserAgent)),
		entry.CreatedAt,
	)
	return err
}

func (s *PGAuditStore) List(ctx context.Context, tenantID uuid.UUID, params store.AuditListParams) ([]store.AuditLogEntry, int, error) {
	var conditions []string
	var args []any
	idx := 1

	conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", idx))
	args = append(args, tenantID)
	idx++

	if params.Action != "" {
		conditions = append(conditions, fmt.Sprintf("action = $%d", idx))
		args = append(args, params.Action)
		idx++
	}

	if params.ResourceType != "" {
		conditions = append(conditions, fmt.Sprintf("resource_type = $%d", idx))
		args = append(args, params.ResourceType)
		idx++
	}

	if params.ResourceID != nil {
		conditions = append(conditions, fmt.Sprintf("resource_id = $%d", idx))
		args = append(args, *params.ResourceID)
		idx++
	}

	if params.GroupID != nil {
		conditions = append(conditions, fmt.Sprintf("group_id = $%d", idx))
		args = append(args, *params.GroupID)
		idx++
	}

	if params.ActorID != nil {
		conditions = append(conditions, fmt.Sprintf("actor_id = $%d", idx))
		args = append(args, *params.ActorID)
		idx++
	}

	if params.FromTime != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", idx))
		args = append(args, *params.FromTime)
		idx++
	}

	if params.ToTime != nil {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", idx))
		args = append(args, *params.ToTime)
		idx++
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := max(params.Offset, 0)

	where := "WHERE " + strings.Join(conditions, " AND ")

	query := fmt.Sprintf(
		`SELECT id, tenant_id, actor_id, action, resource_type, resource_id, group_id, detail, ip_address, user_agent, created_at,
		        COUNT(*) OVER() AS total_count
		 FROM audit_log %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		where, idx, idx+1,
	)
	args = append(args, limit, offset)

	rows, err := s.dbFor(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []store.AuditLogEntry
	var total int
	for rows.Next() {
		var e store.AuditLogEntry
		var actorID uuid.NullUUID
		var groupID sql.NullString
		var detailJSON []byte
		var ipAddress sql.NullString
		var userAgent sql.NullString
		if err := rows.Scan(
			&e.ID, &e.TenantID, &actorID, &e.Action,
			&e.ResourceType, &e.ResourceID, &groupID, &detailJSON,
			&ipAddress, &userAgent, &e.CreatedAt, &total,
		); err != nil {
			return nil, 0, err
		}
		if actorID.Valid {
			e.ActorID = &actorID.UUID
		}
		if groupID.Valid {
			id, err := uuid.Parse(groupID.String)
			if err == nil {
				e.GroupID = &id
			}
		}
		if detailJSON != nil {
			e.Detail = json.RawMessage(detailJSON)
		}
		if ipAddress.Valid {
			e.IPAddress = nilStr(ipAddress.String)
		}
		if userAgent.Valid {
			e.UserAgent = nilStr(userAgent.String)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if entries == nil {
		entries = []store.AuditLogEntry{}
	}

	return entries, total, nil
}
