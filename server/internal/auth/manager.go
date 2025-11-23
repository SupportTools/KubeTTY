package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// RefreshTokenDelimiter separates the token ID and secret.
const RefreshTokenDelimiter = "."

// Errors returned by Manager operations.
var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrTokenMalformed     = errors.New("token malformed")
	ErrTokenExpired       = errors.New("token expired")
	ErrTokenRevoked       = errors.New("token revoked")
	ErrWeakPassword       = errors.New("password must be at least 8 characters")
)

// TokenPair captures newly issued JWT + refresh token values.
type TokenPair struct {
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshTokenID   uuid.UUID
	RefreshExpiresAt time.Time
	AccessIssuedAt   time.Time
	RefreshIssuedAt  time.Time
}

// TokenMetadata stores optional metadata for refresh tokens.
type TokenMetadata struct {
	CreatedBy string
	UserAgent string
	ClientIP  string
}

// AccessClaims defines our JWT payload.
type AccessClaims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// Manager handles password and token workflows.
type Manager struct {
	store      Store
	secret     []byte
	issuer     string
	accessTTL  time.Duration
	refreshTTL time.Duration
	now        func() time.Time
}

// NewManager configures an auth manager.
func NewManager(store Store, secret string, issuer string, accessTTL, refreshTTL time.Duration) (*Manager, error) {
	key, err := decodeSecret(secret)
	if err != nil {
		return nil, err
	}
	return &Manager{
		store:      store,
		secret:     key,
		issuer:     issuer,
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		now:        time.Now,
	}, nil
}

// Authenticate verifies username/password and ensures the user is active.
func (m *Manager) Authenticate(ctx context.Context, username, password string) (*User, error) {
	user, err := m.store.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if !user.IsActive {
		return nil, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword(user.PasswordHash, []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	return user, nil
}

// ChangePassword verifies the current password and updates to the new password.
// It also revokes all existing refresh tokens for security.
func (m *Manager) ChangePassword(ctx context.Context, userID uuid.UUID, currentPassword, newPassword string) error {
	// Validate new password strength
	if len(newPassword) < 8 {
		return ErrWeakPassword
	}

	// Get the user
	user, err := m.store.GetUser(ctx, userID)
	if err != nil {
		return err
	}

	// Verify current password
	if err := bcrypt.CompareHashAndPassword(user.PasswordHash, []byte(currentPassword)); err != nil {
		return ErrInvalidCredentials
	}

	// Hash new password
	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash new password: %w", err)
	}

	// Update password in store
	if err := m.store.UpdateUserPassword(ctx, userID, newHash); err != nil {
		return err
	}

	// Revoke all existing refresh tokens for security
	_, err = m.store.RevokeRefreshTokensByUser(ctx, userID, m.now())
	if err != nil {
		// Log the error but don't fail the password change
		// The password was successfully changed
		return nil
	}

	return nil
}

// IssueTokenPair creates signed access/refresh tokens for the user.
func (m *Manager) IssueTokenPair(ctx context.Context, user *User, meta TokenMetadata) (*TokenPair, error) {
	now := m.now()
	accessExp := now.Add(m.accessTTL)
	accessToken, err := m.signAccessToken(user, now, accessExp)
	if err != nil {
		return nil, err
	}
	refresh, err := m.createRefreshToken(ctx, user.ID, now, meta)
	if err != nil {
		return nil, err
	}
	return &TokenPair{
		AccessToken:      accessToken,
		AccessExpiresAt:  accessExp,
		AccessIssuedAt:   now,
		RefreshToken:     refresh.plain,
		RefreshTokenID:   refresh.record.TokenID,
		RefreshExpiresAt: refresh.record.ExpiresAt,
		RefreshIssuedAt:  refresh.record.IssuedAt,
	}, nil
}

// Refresh validates the refresh token and issues a new access token.
// The refresh token is reused (no rotation) to avoid race conditions
// when multiple refresh requests occur close together.
func (m *Manager) Refresh(ctx context.Context, token string, meta TokenMetadata) (*TokenPair, error) {
	tokenID, secret, err := ParseRefreshToken(token)
	if err != nil {
		return nil, err
	}
	record, err := m.store.GetRefreshToken(ctx, tokenID)
	if err != nil {
		return nil, err
	}
	now := m.now()
	if record.RevokedAt != nil {
		return nil, ErrTokenRevoked
	}
	if now.After(record.ExpiresAt) {
		return nil, ErrTokenExpired
	}
	if !hmac.Equal(record.TokenHash, hashSecret(secret)) {
		return nil, ErrTokenMalformed
	}
	user, err := m.store.GetUser(ctx, record.UserID)
	if err != nil {
		return nil, err
	}
	if !user.IsActive {
		return nil, ErrInvalidCredentials
	}

	// Issue new access token only (no rotation - reuse existing refresh token)
	accessExp := now.Add(m.accessTTL)
	accessToken, err := m.signAccessToken(user, now, accessExp)
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:      accessToken,
		AccessExpiresAt:  accessExp,
		AccessIssuedAt:   now,
		RefreshToken:     token, // Return the same refresh token
		RefreshTokenID:   tokenID,
		RefreshExpiresAt: record.ExpiresAt, // Original expiry
		RefreshIssuedAt:  record.IssuedAt,  // Original issue time
	}, nil
}

// ValidateAccessToken parses and verifies a JWT access token.
func (m *Manager) ValidateAccessToken(token string) (*AccessClaims, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(m.issuer),
	)
	claims := &AccessClaims{}
	_, err := parser.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		return m.secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, fmt.Errorf("parse access token: %w", err)
	}
	return claims, nil
}

type refreshResult struct {
	plain  string
	record RefreshToken
}

func (m *Manager) signAccessToken(user *User, issuedAt, expiresAt time.Time) (string, error) {
	claims := AccessClaims{
		Username: user.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID.String(),
			Issuer:    m.issuer,
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			NotBefore: jwt.NewNumericDate(issuedAt),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}
	return signed, nil
}

func (m *Manager) createRefreshToken(ctx context.Context, userID uuid.UUID, now time.Time, meta TokenMetadata) (*refreshResult, error) {
	tokenID := uuid.New()
	secretBytes, err := randomBytes(32)
	if err != nil {
		return nil, err
	}
	secretEncoded := base64.RawURLEncoding.EncodeToString(secretBytes)
	plain := fmt.Sprintf("%s%s%s", tokenID.String(), RefreshTokenDelimiter, secretEncoded)
	rec := RefreshToken{
		TokenID:   tokenID,
		UserID:    userID,
		TokenHash: hashSecret(secretBytes),
		IssuedAt:  now,
		ExpiresAt: now.Add(m.refreshTTL),
		CreatedBy: meta.CreatedBy,
		UserAgent: meta.UserAgent,
		ClientIP:  meta.ClientIP,
	}
	if err := m.store.InsertRefreshToken(ctx, rec); err != nil {
		return nil, err
	}
	return &refreshResult{plain: plain, record: rec}, nil
}

func decodeSecret(secret string) ([]byte, error) {
	if secret == "" {
		return nil, fmt.Errorf("jwt secret required")
	}
	if decoded, err := base64.StdEncoding.DecodeString(secret); err == nil {
		if len(decoded) < 32 {
			return nil, fmt.Errorf("decoded JWT secret must be at least 32 bytes")
		}
		return decoded, nil
	}
	if len(secret) < 32 {
		return nil, fmt.Errorf("JWT secret must be base64-encoded or >=32 bytes")
	}
	return []byte(secret), nil
}

// ParseRefreshToken splits the encoded refresh token string.
func ParseRefreshToken(token string) (uuid.UUID, []byte, error) {
	parts := strings.Split(token, RefreshTokenDelimiter)
	if len(parts) != 2 {
		return uuid.Nil, nil, ErrTokenMalformed
	}
	tokenID, err := uuid.Parse(parts[0])
	if err != nil {
		return uuid.Nil, nil, ErrTokenMalformed
	}
	secretBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return uuid.Nil, nil, ErrTokenMalformed
	}
	return tokenID, secretBytes, nil
}

func hashSecret(secret []byte) []byte {
	sum := sha256.Sum256(secret)
	return sum[:]
}

func randomBytes(n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("rand read: %w", err)
	}
	return buf, nil
}
