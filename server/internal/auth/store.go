package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Errors returned by the auth store.
var (
	ErrUserNotFound         = errors.New("auth user not found")
	ErrRefreshTokenNotFound = errors.New("refresh token not found")
	ErrDuplicateUsername    = errors.New("username already exists")
)

// User represents a KubeTTY local user account.
type User struct {
	ID           uuid.UUID
	Username     string
	PasswordHash []byte
	IsActive     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastLoginAt  *time.Time
}

// RefreshToken represents a persisted refresh token entry.
type RefreshToken struct {
	ID        uuid.UUID
	TokenID   uuid.UUID
	UserID    uuid.UUID
	TokenHash []byte
	IssuedAt  time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedBy string
	UserAgent string
	ClientIP  string
}

// Store exposes auth persistence operations.
type Store interface {
	GetUser(ctx context.Context, id uuid.UUID) (*User, error)
	GetUserByUsername(ctx context.Context, username string) (*User, error)
	ListUsers(ctx context.Context) ([]User, error)
	CreateUser(ctx context.Context, user User) error
	UpdateUserPassword(ctx context.Context, id uuid.UUID, passwordHash []byte) error
	SetUserActive(ctx context.Context, id uuid.UUID, active bool) error
	UpdateLastLogin(ctx context.Context, id uuid.UUID, ts time.Time) error

	InsertRefreshToken(ctx context.Context, token RefreshToken) error
	GetRefreshToken(ctx context.Context, tokenID uuid.UUID) (*RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, tokenID uuid.UUID, revokedAt time.Time) error
	RevokeRefreshTokensByUser(ctx context.Context, userID uuid.UUID, revokedAt time.Time) (int64, error)
	DeleteExpiredRefreshTokens(ctx context.Context, cutoff time.Time) (int64, error)
}

// PGStore is a pgx-backed Store implementation.
type PGStore struct {
	pool *pgxpool.Pool
}

// NewStore creates a new store using its own connection pool with secure, structured configuration.
//
// The config parameter should be created using sharedconfig.BuildPostgresConfig() or
// the CommonConfig.ConnConfig() method, which provide injection-proof configuration.
//
// Example:
//
//	cfg, err := config.LoadGatewayConfig()
//	if err != nil {
//	    return err
//	}
//	poolConfig, err := cfg.ConnConfig()
//	if err != nil {
//	    return fmt.Errorf("build pool config: %w", err)
//	}
//	store, err := auth.NewStore(ctx, poolConfig)
func NewStore(ctx context.Context, config *pgxpool.Config) (*PGStore, error) {
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("auth store connect: %w", err)
	}
	return &PGStore{pool: pool}, nil
}

// Close releases the connection pool.
func (s *PGStore) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// NewStoreFromPool reuses an existing pool.
func NewStoreFromPool(pool *pgxpool.Pool) *PGStore {
	return &PGStore{pool: pool}
}

func (s *PGStore) GetUser(ctx context.Context, id uuid.UUID) (*User, error) {
	const stmt = `
SELECT id, username, password_hash, is_active, created_at, updated_at, last_login_at
FROM kubetty_users
WHERE id=$1`
	row := s.pool.QueryRow(ctx, stmt, id)
	return scanUser(row)
}

func (s *PGStore) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	const stmt = `
SELECT id, username, password_hash, is_active, created_at, updated_at, last_login_at
FROM kubetty_users
WHERE username=$1`
	row := s.pool.QueryRow(ctx, stmt, username)
	return scanUser(row)
}

func (s *PGStore) ListUsers(ctx context.Context) ([]User, error) {
	const stmt = `
SELECT id, username, password_hash, is_active, created_at, updated_at, last_login_at
FROM kubetty_users
ORDER BY username`
	rows, err := s.pool.Query(ctx, stmt)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *user)
	}
	return users, rows.Err()
}

func (s *PGStore) CreateUser(ctx context.Context, user User) error {
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}
	const stmt = `
INSERT INTO kubetty_users (id, username, password_hash, is_active, created_at, updated_at, last_login_at)
VALUES ($1, $2, $3, COALESCE($4, TRUE), COALESCE($5, NOW()), COALESCE($6, NOW()), $7)`
	if _, err := s.pool.Exec(ctx, stmt, user.ID, user.Username, user.PasswordHash, user.IsActive, nullableTime(user.CreatedAt), nullableTime(user.UpdatedAt), user.LastLoginAt); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

func (s *PGStore) UpdateUserPassword(ctx context.Context, id uuid.UUID, passwordHash []byte) error {
	const stmt = `
UPDATE kubetty_users
SET password_hash=$2,
    updated_at=NOW()
WHERE id=$1`
	tag, err := s.pool.Exec(ctx, stmt, id, passwordHash)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *PGStore) SetUserActive(ctx context.Context, id uuid.UUID, active bool) error {
	const stmt = `
UPDATE kubetty_users
SET is_active=$2,
    updated_at=NOW()
WHERE id=$1`
	tag, err := s.pool.Exec(ctx, stmt, id, active)
	if err != nil {
		return fmt.Errorf("set user active: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *PGStore) UpdateLastLogin(ctx context.Context, id uuid.UUID, ts time.Time) error {
	const stmt = `
UPDATE kubetty_users
SET last_login_at=$2,
    updated_at=NOW()
WHERE id=$1`
	tag, err := s.pool.Exec(ctx, stmt, id, ts)
	if err != nil {
		return fmt.Errorf("update last login: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *PGStore) InsertRefreshToken(ctx context.Context, token RefreshToken) error {
	if token.ID == uuid.Nil {
		token.ID = uuid.New()
	}
	const stmt = `
INSERT INTO kubetty_refresh_tokens (id, token_id, user_id, token_hash, issued_at, expires_at, revoked_at, created_by, user_agent, client_ip)
VALUES ($1,$2,$3,$4,COALESCE($5,NOW()),$6,$7,$8,$9,$10)`
	if _, err := s.pool.Exec(ctx, stmt, token.ID, token.TokenID, token.UserID, token.TokenHash, nullableTime(token.IssuedAt), token.ExpiresAt, token.RevokedAt, nullIfEmpty(token.CreatedBy), nullIfEmpty(token.UserAgent), nullIfEmpty(token.ClientIP)); err != nil {
		return fmt.Errorf("insert refresh token: %w", err)
	}
	return nil
}

func (s *PGStore) GetRefreshToken(ctx context.Context, tokenID uuid.UUID) (*RefreshToken, error) {
	const stmt = `
SELECT id, token_id, user_id, token_hash, issued_at, expires_at, revoked_at, COALESCE(created_by,''), COALESCE(user_agent,''), COALESCE(client_ip,'')
FROM kubetty_refresh_tokens
WHERE token_id=$1`
	row := s.pool.QueryRow(ctx, stmt, tokenID)
	var token RefreshToken
	var createdBy, userAgent, clientIP string
	if err := row.Scan(&token.ID, &token.TokenID, &token.UserID, &token.TokenHash, &token.IssuedAt, &token.ExpiresAt, &token.RevokedAt, &createdBy, &userAgent, &clientIP); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRefreshTokenNotFound
		}
		return nil, fmt.Errorf("get refresh token: %w", err)
	}
	token.CreatedBy = createdBy
	token.UserAgent = userAgent
	token.ClientIP = clientIP
	return &token, nil
}

func (s *PGStore) RevokeRefreshToken(ctx context.Context, tokenID uuid.UUID, revokedAt time.Time) error {
	const stmt = `
UPDATE kubetty_refresh_tokens
SET revoked_at=$2
WHERE token_id=$1 AND revoked_at IS NULL`
	tag, err := s.pool.Exec(ctx, stmt, tokenID, revokedAt)
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRefreshTokenNotFound
	}
	return nil
}

func (s *PGStore) RevokeRefreshTokensByUser(ctx context.Context, userID uuid.UUID, revokedAt time.Time) (int64, error) {
	const stmt = `
UPDATE kubetty_refresh_tokens
SET revoked_at=$2
WHERE user_id=$1 AND revoked_at IS NULL`
	tag, err := s.pool.Exec(ctx, stmt, userID, revokedAt)
	if err != nil {
		return 0, fmt.Errorf("revoke refresh tokens by user: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *PGStore) DeleteExpiredRefreshTokens(ctx context.Context, cutoff time.Time) (int64, error) {
	const stmt = `
DELETE FROM kubetty_refresh_tokens
WHERE expires_at < $1 OR (revoked_at IS NOT NULL AND revoked_at < $1)`
	tag, err := s.pool.Exec(ctx, stmt, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete expired refresh tokens: %w", err)
	}
	return tag.RowsAffected(), nil
}

func scanUser(row pgx.Row) (*User, error) {
	var user User
	if err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.IsActive, &user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("scan user: %w", err)
	}
	return &user, nil
}

func nullableTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t
}

func nullIfEmpty(val string) interface{} {
	if val == "" {
		return nil
	}
	return val
}
