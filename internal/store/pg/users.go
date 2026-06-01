package pg

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

// PGUserStore implements store.UserStore backed by Postgres.
type PGUserStore struct {
	db *sql.DB
}

// NewPGUserStore creates a new PGUserStore.
func NewPGUserStore(db *sql.DB) *PGUserStore {
	return &PGUserStore{db: db}
}

// --- User CRUD ---

func (s *PGUserStore) Create(ctx context.Context, user *store.UserData) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, email, display_name, avatar_url, tenant_id, auth_provider, password_hash, status, last_login_at, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		user.ID, user.Email, user.DisplayName,
		sql.NullString{String: derefStr(user.AvatarURL), Valid: user.AvatarURL != nil && *user.AvatarURL != ""},
		user.TenantID, user.AuthProvider,
		sql.NullString{String: derefStr(user.PasswordHash), Valid: user.PasswordHash != nil && *user.PasswordHash != ""},
		user.Status,
		sql.NullTime{Time: func() time.Time {
			if user.LastLoginAt != nil {
				return *user.LastLoginAt
			}
			return time.Time{}
		}(), Valid: user.LastLoginAt != nil && !user.LastLoginAt.IsZero()},
		user.CreatedAt, user.UpdatedAt,
	)
	return err
}

func (s *PGUserStore) GetByID(ctx context.Context, id uuid.UUID) (*store.UserData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, email, display_name, avatar_url, tenant_id, auth_provider, password_hash, status, last_login_at, created_at, updated_at
		 FROM users WHERE id = $1`, id)
	u, err := scanUserRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return u, nil
}

func (s *PGUserStore) GetByEmail(ctx context.Context, tenantID uuid.UUID, email string) (*store.UserData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, email, display_name, avatar_url, tenant_id, auth_provider, password_hash, status, last_login_at, created_at, updated_at
		 FROM users WHERE tenant_id = $1 AND email = $2`, tenantID, email)
	u, err := scanUserRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return u, nil
}

func (s *PGUserStore) GetByEmailAnyTenant(ctx context.Context, email string) (*store.UserData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, email, display_name, avatar_url, tenant_id, auth_provider, password_hash, status, last_login_at, created_at, updated_at
		 FROM users WHERE email = $1 LIMIT 1`, email)
	u, err := scanUserRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return u, nil
}

func (s *PGUserStore) Update(ctx context.Context, user *store.UserData) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET display_name = $1, avatar_url = $2, updated_at = NOW() WHERE id = $3`,
		user.DisplayName,
		sql.NullString{String: derefStr(user.AvatarURL), Valid: user.AvatarURL != nil && *user.AvatarURL != ""},
		user.ID,
	)
	return err
}

func (s *PGUserStore) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET status = $1, updated_at = NOW() WHERE id = $2`,
		status, id,
	)
	return err
}

func (s *PGUserStore) UpdateLastLogin(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET last_login_at = NOW() WHERE id = $1`, id,
	)
	return err
}

func (s *PGUserStore) List(ctx context.Context, tenantID uuid.UUID, params store.UserListParams) (*store.UserListResult, error) {
	var conditions []string
	var args []any
	idx := 1

	conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", idx))
	args = append(args, tenantID)
	idx++

	if params.Search != "" {
		conditions = append(conditions, fmt.Sprintf("(email ILIKE $%d OR display_name ILIKE $%d)", idx, idx))
		args = append(args, "%"+params.Search+"%")
		idx++
	}

	if params.Status != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", idx))
		args = append(args, params.Status)
		idx++
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := max(params.Offset, 0)

	where := "WHERE " + strings.Join(conditions, " AND ")

	query := fmt.Sprintf(
		`SELECT id, email, display_name, avatar_url, tenant_id, auth_provider, password_hash, status, last_login_at, created_at, updated_at,
		        COUNT(*) OVER() AS total_count
		 FROM users %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		where, idx, idx+1,
	)
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []store.UserData
	var total int
	for rows.Next() {
		var u store.UserData
		var avatarURL, passwordHash sql.NullString
		var lastLoginAt sql.NullTime
		if err := rows.Scan(
			&u.ID, &u.Email, &u.DisplayName, &avatarURL, &u.TenantID, &u.AuthProvider, &passwordHash,
			&u.Status, &lastLoginAt, &u.CreatedAt, &u.UpdatedAt, &total,
		); err != nil {
			return nil, err
		}
		u.AvatarURL = nilStr(avatarURL.String)
		if passwordHash.Valid {
			u.PasswordHash = nilStr(passwordHash.String)
		}
		if lastLoginAt.Valid {
			t := lastLoginAt.Time
			u.LastLoginAt = &t
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if users == nil {
		users = []store.UserData{}
	}

	return &store.UserListResult{
		Users:  users,
		Total:  total,
		Offset: offset,
		Limit:  limit,
	}, nil
}

func (s *PGUserStore) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id)
	return err
}

// --- Identity management ---

func (s *PGUserStore) CreateIdentity(ctx context.Context, identity *store.UserIdentity) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_identities (id, user_id, provider, provider_subject, provider_tenant, email, linked_at, last_used_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		identity.ID, identity.UserID, identity.Provider, identity.ProviderSubject,
		sql.NullString{String: derefStr(identity.ProviderTenant), Valid: identity.ProviderTenant != nil && *identity.ProviderTenant != ""},
		identity.Email, identity.LinkedAt,
		sql.NullTime{Time: func() time.Time {
			if identity.LastUsedAt != nil {
				return *identity.LastUsedAt
			}
			return time.Time{}
		}(), Valid: identity.LastUsedAt != nil && !identity.LastUsedAt.IsZero()},
	)
	return err
}

func (s *PGUserStore) GetIdentityByProviderSubject(ctx context.Context, provider, subject string) (*store.UserIdentity, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, provider, provider_subject, provider_tenant, email, linked_at, last_used_at
		 FROM user_identities WHERE provider = $1 AND provider_subject = $2`, provider, subject)
	ident, err := scanIdentityRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return ident, nil
}

func (s *PGUserStore) GetIdentities(ctx context.Context, userID uuid.UUID) ([]store.UserIdentity, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, provider, provider_subject, provider_tenant, email, linked_at, last_used_at
		 FROM user_identities WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.UserIdentity
	for rows.Next() {
		ident, err := scanIdentityRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *ident)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		result = []store.UserIdentity{}
	}
	return result, nil
}

func (s *PGUserStore) UpdateIdentityLastUsed(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE user_identities SET last_used_at = NOW() WHERE id = $1`, id,
	)
	return err
}

// --- Refresh token management ---

func (s *PGUserStore) StoreRefreshToken(ctx context.Context, userID uuid.UUID, tokenHash string, deviceInfo string, expiresAt time.Time) error {
	id := store.GenNewID()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, device_info, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, NOW())`,
		id, userID, tokenHash, deviceInfo, expiresAt,
	)
	return err
}

func (s *PGUserStore) ValidateRefreshToken(ctx context.Context, tokenHash string) (uuid.UUID, error) {
	var userID uuid.UUID
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id FROM refresh_tokens WHERE token_hash = $1 AND expires_at > NOW()`, tokenHash,
	).Scan(&userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, nil
		}
		return uuid.Nil, err
	}
	return userID, nil
}

func (s *PGUserStore) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM refresh_tokens WHERE token_hash = $1`, tokenHash,
	)
	return err
}

func (s *PGUserStore) RevokeAllRefreshTokens(ctx context.Context, userID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM refresh_tokens WHERE user_id = $1`, userID,
	)
	return err
}

// PruneExpiredRefreshTokens removes all expired refresh tokens.
func (s *PGUserStore) PruneExpiredRefreshTokens(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE expires_at <= NOW()`)
	return err
}

// --- Scan helpers ---

func scanUserRow(row *sql.Row) (*store.UserData, error) {
	var u store.UserData
	var avatarURL, passwordHash sql.NullString
	var lastLoginAt sql.NullTime
	err := row.Scan(
		&u.ID, &u.Email, &u.DisplayName, &avatarURL, &u.TenantID, &u.AuthProvider, &passwordHash,
		&u.Status, &lastLoginAt, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	u.AvatarURL = nilStr(avatarURL.String)
	if passwordHash.Valid {
		u.PasswordHash = nilStr(passwordHash.String)
	}
	if lastLoginAt.Valid {
		t := lastLoginAt.Time
		u.LastLoginAt = &t
	}
	return &u, nil
}

func scanIdentityRow(row *sql.Row) (*store.UserIdentity, error) {
	var ident store.UserIdentity
	var providerTenant sql.NullString
	var lastUsedAt sql.NullTime
	err := row.Scan(
		&ident.ID, &ident.UserID, &ident.Provider, &ident.ProviderSubject,
		&providerTenant, &ident.Email, &ident.LinkedAt, &lastUsedAt,
	)
	if err != nil {
		return nil, err
	}
	ident.ProviderTenant = nilStr(providerTenant.String)
	if lastUsedAt.Valid {
		t := lastUsedAt.Time
		ident.LastUsedAt = &t
	}
	return &ident, nil
}

func scanIdentityRows(rows *sql.Rows) (*store.UserIdentity, error) {
	var ident store.UserIdentity
	var providerTenant sql.NullString
	var lastUsedAt sql.NullTime
	err := rows.Scan(
		&ident.ID, &ident.UserID, &ident.Provider, &ident.ProviderSubject,
		&providerTenant, &ident.Email, &ident.LinkedAt, &lastUsedAt,
	)
	if err != nil {
		return nil, err
	}
	ident.ProviderTenant = nilStr(providerTenant.String)
	if lastUsedAt.Valid {
		t := lastUsedAt.Time
		ident.LastUsedAt = &t
	}
	return &ident, nil
}
