//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ClearSessionsByPattern resets or deletes all sessions matching a SQL LIKE pattern.
func (s *SQLiteSessionStore) ClearSessionsByPattern(ctx context.Context, pattern string, action string) (int, error) {
	tid := tenantIDForInsert(ctx)

	rows, err := s.db.QueryContext(ctx,
		"SELECT session_key FROM sessions WHERE session_key LIKE ? AND tenant_id = ?",
		pattern, tid.String())
	if err != nil {
		return 0, err
	}
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return 0, err
		}
		keys = append(keys, k)
	}
	rows.Close()

	if len(keys) == 0 {
		return 0, nil
	}
	return s.clearKeys(ctx, keys, action, tid.String())
}

// ClearSessionsByKeys resets or deletes sessions by explicit key list.
func (s *SQLiteSessionStore) ClearSessionsByKeys(ctx context.Context, keys []string, action string) (int, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	tid := tenantIDForInsert(ctx)
	return s.clearKeys(ctx, keys, action, tid.String())
}

func (s *SQLiteSessionStore) clearKeys(ctx context.Context, keys []string, action string, tid string) (int, error) {
	s.evictFromCache(keys, tid)

	switch action {
	case "reset":
		return s.bulkReset(ctx, keys, tid)
	case "delete":
		return s.bulkDelete(ctx, keys, tid)
	default:
		slog.Warn("session_clear.unknown_action", "action", action)
		return 0, nil
	}
}

func (s *SQLiteSessionStore) bulkReset(ctx context.Context, keys []string, tid string) (int, error) {
	ph := make([]string, len(keys))
	args := make([]any, 0, len(keys)+2)
	args = append(args, time.Now())
	for i, k := range keys {
		ph[i] = "?"
		args = append(args, k)
	}
	args = append(args, tid)

	query := `UPDATE sessions SET messages = '[]', summary = '', updated_at = ?
			  WHERE session_key IN (` + strings.Join(ph, ",") + `)
			  AND tenant_id = ?`

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLiteSessionStore) bulkDelete(ctx context.Context, keys []string, tid string) (int, error) {
	if s.OnDelete != nil {
		for _, k := range keys {
			s.OnDelete(k)
		}
	}

	ph := make([]string, len(keys))
	args := make([]any, 0, len(keys)+1)
	for i, k := range keys {
		ph[i] = "?"
		args = append(args, k)
	}
	args = append(args, tid)

	query := "DELETE FROM sessions WHERE session_key IN (" + strings.Join(ph, ",") + ") AND tenant_id = ?"

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLiteSessionStore) evictFromCache(keys []string, tid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prefix := tid + ":"
	for _, k := range keys {
		delete(s.cache, prefix+k)
	}
}

// QuerySessionKeys returns session keys matching a SQL LIKE pattern.
func (s *SQLiteSessionStore) QuerySessionKeys(ctx context.Context, pattern string) ([]string, error) {
	tid := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx,
		"SELECT session_key FROM sessions WHERE session_key LIKE ? AND tenant_id = ?",
		pattern, tid.String())
	if err != nil {
		return nil, err
	}
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return keys, err
		}
		keys = append(keys, k)
	}
	rows.Close()
	return keys, nil
}

var _ store.SessionBulkStore = (*SQLiteSessionStore)(nil)
