//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteUserStore implements store.UserStore backed by SQLite.
type SQLiteUserStore struct {
	db *sql.DB
}

// NewSQLiteUserStore creates a new SQLite-backed user store.
func NewSQLiteUserStore(db *sql.DB) *SQLiteUserStore {
	return &SQLiteUserStore{db: db}
}

// --- User CRUD ---

func (s *SQLiteUserStore) Create(ctx context.Context, user *store.UserData) error {
	if user.ID == uuid.Nil {
		user.ID = store.GenNewID()
	}
	now := time.Now().UTC()
	user.CreatedAt = now
	user.UpdatedAt = now

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, email, display_name, avatar_url, tenant_id, auth_provider, password_hash, status, last_login_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID.String(), user.Email, user.DisplayName, user.AvatarURL,
		user.TenantID.String(), user.AuthProvider, user.PasswordHash,
		user.Status, user.LastLoginAt, user.CreatedAt, user.UpdatedAt,
	)
	return err
}

const userCols = `id, email, display_name, avatar_url, tenant_id, auth_provider, password_hash, status, last_login_at, created_at, updated_at`

func scanUser(row interface{ Scan(dest ...any) error }) (*store.UserData, error) {
	var u store.UserData
	var id, tenantID string
	var avatarURL, passwordHash *string
	var lastLoginAt nullSqliteTime
	createdAt, updatedAt := scanTimePair()

	err := row.Scan(
		&id, &u.Email, &u.DisplayName, &avatarURL,
		&tenantID, &u.AuthProvider, &passwordHash,
		&u.Status, &lastLoginAt, createdAt, updatedAt,
	)
	if err != nil {
		return nil, err
	}

	u.ID, err = uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("parse user id: %w", err)
	}
	u.TenantID, err = uuid.Parse(tenantID)
	if err != nil {
		return nil, fmt.Errorf("parse tenant_id: %w", err)
	}
	u.AvatarURL = avatarURL
	u.PasswordHash = passwordHash
	if lastLoginAt.Valid {
		u.LastLoginAt = &lastLoginAt.Time
	}
	u.CreatedAt = createdAt.Time
	u.UpdatedAt = updatedAt.Time

	return &u, nil
}

func (s *SQLiteUserStore) GetByID(ctx context.Context, id uuid.UUID) (*store.UserData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+userCols+` FROM users WHERE id = ?`,
		id.String(),
	)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return u, nil
}

func (s *SQLiteUserStore) GetByEmail(ctx context.Context, tenantID uuid.UUID, email string) (*store.UserData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+userCols+` FROM users WHERE tenant_id = ? AND email = ?`,
		tenantID.String(), email,
	)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return u, nil
}

func (s *SQLiteUserStore) Update(ctx context.Context, user *store.UserData) error {
	now := time.Now().UTC()
	user.UpdatedAt = now

	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET display_name = ?, avatar_url = ?, updated_at = ? WHERE id = ?`,
		user.DisplayName, user.AvatarURL, now, user.ID.String(),
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLiteUserStore) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET status = ?, updated_at = ? WHERE id = ?`,
		status, now, id.String(),
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLiteUserStore) UpdateLastLogin(ctx context.Context, id uuid.UUID) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET last_login_at = ? WHERE id = ?`,
		now, id.String(),
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLiteUserStore) List(ctx context.Context, tenantID uuid.UUID, params store.UserListParams) (*store.UserListResult, error) {
	var conditions []string
	var args []any

	conditions = append(conditions, "tenant_id = ?")
	args = append(args, tenantID.String())

	if params.Search != "" {
		escaped := strings.NewReplacer("%", "\\%", "_", "\\_").Replace(params.Search)
		pattern := "%" + escaped + "%"
		conditions = append(conditions, "(display_name LIKE ? ESCAPE '\\' OR email LIKE ? ESCAPE '\\')")
		args = append(args, pattern, pattern)
	}
	if params.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, params.Status)
	}

	where := " WHERE " + strings.Join(conditions, " AND ")

	// Count total.
	var total int
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users`+where,
		countArgs...,
	).Scan(&total)
	if err != nil {
		return nil, err
	}

	// Fetch page.
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT ` + userCols + ` FROM users` + where +
		` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, params.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []store.UserData
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &store.UserListResult{
		Users:  users,
		Total:  total,
		Offset: params.Offset,
		Limit:  limit,
	}, nil
}

func (s *SQLiteUserStore) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM users WHERE id = ?`,
		id.String(),
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// --- Identity management ---

const identityCols = `id, user_id, provider, provider_subject, provider_tenant, email, linked_at, last_used_at`

func scanIdentity(row interface{ Scan(dest ...any) error }) (*store.UserIdentity, error) {
	var id store.UserIdentity
	var idStr, userIDStr string
	var providerTenant *string
	var lastUsedAt nullSqliteTime
	linkedAt := &sqliteTime{}

	err := row.Scan(
		&idStr, &userIDStr, &id.Provider, &id.ProviderSubject,
		&providerTenant, &id.Email, linkedAt, &lastUsedAt,
	)
	if err != nil {
		return nil, err
	}

	id.ID, err = uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("parse identity id: %w", err)
	}
	id.UserID, err = uuid.Parse(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("parse user_id: %w", err)
	}
	id.ProviderTenant = providerTenant
	id.LinkedAt = linkedAt.Time
	if lastUsedAt.Valid {
		id.LastUsedAt = &lastUsedAt.Time
	}

	return &id, nil
}

func (s *SQLiteUserStore) CreateIdentity(ctx context.Context, identity *store.UserIdentity) error {
	if identity.ID == uuid.Nil {
		identity.ID = store.GenNewID()
	}
	if identity.LinkedAt.IsZero() {
		identity.LinkedAt = time.Now().UTC()
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_identities (id, user_id, provider, provider_subject, provider_tenant, email, linked_at, last_used_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		identity.ID.String(), identity.UserID.String(),
		identity.Provider, identity.ProviderSubject,
		identity.ProviderTenant, identity.Email,
		identity.LinkedAt, identity.LastUsedAt,
	)
	return err
}

func (s *SQLiteUserStore) GetIdentityByProviderSubject(ctx context.Context, provider, subject string) (*store.UserIdentity, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+identityCols+` FROM user_identities WHERE provider = ? AND provider_subject = ?`,
		provider, subject,
	)
	id, err := scanIdentity(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return id, nil
}

func (s *SQLiteUserStore) GetIdentities(ctx context.Context, userID uuid.UUID) ([]store.UserIdentity, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+identityCols+` FROM user_identities WHERE user_id = ? ORDER BY linked_at`,
		userID.String(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var identities []store.UserIdentity
	for rows.Next() {
		id, err := scanIdentity(rows)
		if err != nil {
			return nil, err
		}
		identities = append(identities, *id)
	}
	return identities, rows.Err()
}

func (s *SQLiteUserStore) UpdateIdentityLastUsed(ctx context.Context, id uuid.UUID) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`UPDATE user_identities SET last_used_at = ? WHERE id = ?`,
		now, id.String(),
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// --- Refresh token management ---

func (s *SQLiteUserStore) StoreRefreshToken(ctx context.Context, userID uuid.UUID, tokenHash string, deviceInfo string, expiresAt time.Time) error {
	id := store.GenNewID()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, device_info, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id.String(), userID.String(), tokenHash,
		deviceInfo, expiresAt.UTC(), time.Now().UTC(),
	)
	return err
}

func (s *SQLiteUserStore) ValidateRefreshToken(ctx context.Context, tokenHash string) (uuid.UUID, error) {
	var userIDStr string
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id FROM refresh_tokens WHERE token_hash = ? AND expires_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
		tokenHash,
	).Scan(&userIDStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, nil
		}
		return uuid.Nil, err
	}
	return uuid.Parse(userIDStr)
}

func (s *SQLiteUserStore) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM refresh_tokens WHERE token_hash = ?`,
		tokenHash,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLiteUserStore) RevokeAllRefreshTokens(ctx context.Context, userID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM refresh_tokens WHERE user_id = ?`,
		userID.String(),
	)
	return err
}

// PruneExpiredRefreshTokens removes all expired refresh tokens.
func (s *SQLiteUserStore) PruneExpiredRefreshTokens(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM refresh_tokens WHERE expires_at <= strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
	)
	return err
}

// Ensure interface satisfaction at compile time.
var _ store.UserStore = (*SQLiteUserStore)(nil)
