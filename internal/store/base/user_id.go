package base

import (
	"context"
	"database/sql"
	"strings"

	"github.com/google/uuid"
)

// NormalizeUserID resolves email strings to user UUIDs.
// If userID is already a valid UUID, returns it as-is.
// If userID contains '@', looks up users table by email and returns the UUID.
// Otherwise returns userID unchanged.
func NormalizeUserID(ctx context.Context, db *sql.DB, userID string) (string, error) {
	if _, err := uuid.Parse(userID); err == nil {
		return userID, nil
	}
	if strings.Contains(userID, "@") {
		var normalized string
		err := db.QueryRowContext(ctx, `SELECT id::text FROM users WHERE email = $1`, userID).Scan(&normalized)
		if err == nil {
			return normalized, nil
		}
	}
	return userID, nil
}
