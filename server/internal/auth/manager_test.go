package auth

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// fixedTime returns a fixed time for deterministic testing.
// Use current time to prevent JWT expiration issues during validation.
var fixedTime = time.Now()

// newTestManager creates a Manager with a MockStore and fixed time function for testing.
func newTestManager(t *testing.T, store *MockStore) *Manager {
	t.Helper()
	// Use a simple 32-byte secret for testing
	secret := base64.StdEncoding.EncodeToString([]byte("test-secret-must-be-at-least-32-bytes-long!"))
	mgr, err := NewManager(store, secret, "kubetty-test", 15*time.Minute, 7*24*time.Hour)
	require.NoError(t, err)
	// Override now() for deterministic testing
	mgr.now = func() time.Time { return fixedTime }
	return mgr
}

// hashPassword is a test helper to hash passwords with minimum cost for speed.
func hashPassword(t *testing.T, password string) []byte {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)
	return hash
}

func TestNewManager(t *testing.T) {
	store := NewMockStore()

	tests := []struct {
		name      string
		secret    string
		issuer    string
		wantErr   bool
		errString string
	}{
		{
			name:    "valid base64 secret",
			secret:  base64.StdEncoding.EncodeToString([]byte("test-secret-must-be-at-least-32-bytes-long!")),
			issuer:  "kubetty",
			wantErr: false,
		},
		{
			name:    "valid plain text secret (>=32 bytes)",
			secret:  "my secret key is exactly 32 char",
			issuer:  "kubetty",
			wantErr: false,
		},
		{
			name:      "empty secret",
			secret:    "",
			issuer:    "kubetty",
			wantErr:   true,
			errString: "jwt secret required",
		},
		{
			name:      "short secret",
			secret:    "short",
			issuer:    "kubetty",
			wantErr:   true,
			errString: "JWT secret must be base64-encoded or >=32 bytes",
		},
		{
			name:      "short decoded secret",
			secret:    base64.StdEncoding.EncodeToString([]byte("short")),
			issuer:    "kubetty",
			wantErr:   true,
			errString: "decoded JWT secret must be at least 32 bytes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, err := NewManager(store, tt.secret, tt.issuer, 15*time.Minute, 7*24*time.Hour)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errString != "" {
					require.Contains(t, err.Error(), tt.errString)
				}
				require.Nil(t, mgr)
			} else {
				require.NoError(t, err)
				require.NotNil(t, mgr)
				require.Equal(t, tt.issuer, mgr.issuer)
				require.Equal(t, 15*time.Minute, mgr.accessTTL)
				require.Equal(t, 7*24*time.Hour, mgr.refreshTTL)
			}
		})
	}
}

func TestAuthenticate(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		username string
		password string
		setup    func(*MockStore)
		wantErr  error
	}{
		{
			name:     "valid credentials",
			username: "testuser",
			password: "password123",
			setup: func(store *MockStore) {
				user := &User{
					ID:           uuid.New(),
					Username:     "testuser",
					PasswordHash: hashPassword(t, "password123"),
					IsActive:     true,
					CreatedAt:    fixedTime,
					UpdatedAt:    fixedTime,
				}
				store.AddUser(user)
			},
			wantErr: nil,
		},
		{
			name:     "invalid password",
			username: "testuser",
			password: "wrongpassword",
			setup: func(store *MockStore) {
				user := &User{
					ID:           uuid.New(),
					Username:     "testuser",
					PasswordHash: hashPassword(t, "password123"),
					IsActive:     true,
					CreatedAt:    fixedTime,
					UpdatedAt:    fixedTime,
				}
				store.AddUser(user)
			},
			wantErr: ErrInvalidCredentials,
		},
		{
			name:     "user not found",
			username: "nonexistent",
			password: "password123",
			setup:    func(store *MockStore) {},
			wantErr:  ErrInvalidCredentials,
		},
		{
			name:     "inactive user",
			username: "inactiveuser",
			password: "password123",
			setup: func(store *MockStore) {
				user := &User{
					ID:           uuid.New(),
					Username:     "inactiveuser",
					PasswordHash: hashPassword(t, "password123"),
					IsActive:     false,
					CreatedAt:    fixedTime,
					UpdatedAt:    fixedTime,
				}
				store.AddUser(user)
			},
			wantErr: ErrInvalidCredentials,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			tt.setup(store)
			mgr := newTestManager(t, store)

			user, err := mgr.Authenticate(ctx, tt.username, tt.password)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Nil(t, user)
			} else {
				require.NoError(t, err)
				require.NotNil(t, user)
				require.Equal(t, tt.username, user.Username)
				require.True(t, user.IsActive)
			}
		})
	}
}

func TestChangePassword(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	tests := []struct {
		name            string
		userID          uuid.UUID
		currentPassword string
		newPassword     string
		setup           func(*MockStore)
		wantErr         error
	}{
		{
			name:            "successful password change",
			userID:          userID,
			currentPassword: "oldpassword",
			newPassword:     "newpassword123",
			setup: func(store *MockStore) {
				user := &User{
					ID:           userID,
					Username:     "testuser",
					PasswordHash: hashPassword(t, "oldpassword"),
					IsActive:     true,
					CreatedAt:    fixedTime,
					UpdatedAt:    fixedTime,
				}
				store.AddUser(user)
			},
			wantErr: nil,
		},
		{
			name:            "weak new password",
			userID:          userID,
			currentPassword: "oldpassword",
			newPassword:     "short",
			setup: func(store *MockStore) {
				user := &User{
					ID:           userID,
					Username:     "testuser",
					PasswordHash: hashPassword(t, "oldpassword"),
					IsActive:     true,
					CreatedAt:    fixedTime,
					UpdatedAt:    fixedTime,
				}
				store.AddUser(user)
			},
			wantErr: ErrWeakPassword,
		},
		{
			name:            "wrong current password",
			userID:          userID,
			currentPassword: "wrongpassword",
			newPassword:     "newpassword123",
			setup: func(store *MockStore) {
				user := &User{
					ID:           userID,
					Username:     "testuser",
					PasswordHash: hashPassword(t, "oldpassword"),
					IsActive:     true,
					CreatedAt:    fixedTime,
					UpdatedAt:    fixedTime,
				}
				store.AddUser(user)
			},
			wantErr: ErrInvalidCredentials,
		},
		{
			name:            "user not found",
			userID:          uuid.New(),
			currentPassword: "oldpassword",
			newPassword:     "newpassword123",
			setup:           func(store *MockStore) {},
			wantErr:         ErrUserNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			tt.setup(store)
			mgr := newTestManager(t, store)

			err := mgr.ChangePassword(ctx, tt.userID, tt.currentPassword, tt.newPassword)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				// Verify password was actually changed
				user, err := store.GetUser(ctx, tt.userID)
				require.NoError(t, err)
				err = bcrypt.CompareHashAndPassword(user.PasswordHash, []byte(tt.newPassword))
				require.NoError(t, err)
			}
		})
	}
}

func TestIssueTokenPair(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	user := &User{
		ID:           userID,
		Username:     "testuser",
		PasswordHash: hashPassword(t, "password123"),
		IsActive:     true,
		CreatedAt:    fixedTime,
		UpdatedAt:    fixedTime,
	}

	tests := []struct {
		name    string
		user    *User
		meta    TokenMetadata
		wantErr bool
	}{
		{
			name: "successful token issuance",
			user: user,
			meta: TokenMetadata{
				CreatedBy: "test-client",
				UserAgent: "Mozilla/5.0",
				ClientIP:  "192.168.1.1",
			},
			wantErr: false,
		},
		{
			name: "token issuance with minimal metadata",
			user: user,
			meta: TokenMetadata{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			store.AddUser(tt.user)
			mgr := newTestManager(t, store)

			pair, err := mgr.IssueTokenPair(ctx, tt.user, tt.meta)

			if tt.wantErr {
				require.Error(t, err)
				require.Nil(t, pair)
			} else {
				require.NoError(t, err)
				require.NotNil(t, pair)
				require.NotEmpty(t, pair.AccessToken)
				require.NotEmpty(t, pair.RefreshToken)
				require.Equal(t, fixedTime, pair.AccessIssuedAt)
				require.Equal(t, fixedTime, pair.RefreshIssuedAt)
				require.Equal(t, fixedTime.Add(15*time.Minute), pair.AccessExpiresAt)
				require.Equal(t, fixedTime.Add(7*24*time.Hour), pair.RefreshExpiresAt)

				// Verify access token can be validated
				claims, err := mgr.ValidateAccessToken(pair.AccessToken)
				require.NoError(t, err)
				require.Equal(t, tt.user.Username, claims.Username)
				require.Equal(t, tt.user.ID.String(), claims.Subject)
			}
		})
	}
}

func TestValidateAccessToken(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	user := &User{
		ID:           userID,
		Username:     "testuser",
		PasswordHash: hashPassword(t, "password123"),
		IsActive:     true,
		CreatedAt:    fixedTime,
		UpdatedAt:    fixedTime,
	}

	store := NewMockStore()
	store.AddUser(user)
	mgr := newTestManager(t, store)

	// Issue a valid token pair
	pair, err := mgr.IssueTokenPair(ctx, user, TokenMetadata{})
	require.NoError(t, err)

	tests := []struct {
		name      string
		token     string
		wantErr   error
		checkTime bool
	}{
		{
			name:    "valid token",
			token:   pair.AccessToken,
			wantErr: nil,
		},
		{
			name:    "malformed token",
			token:   "not.a.valid.jwt",
			wantErr: nil, // Will wrap the error, not ErrTokenExpired
		},
		{
			name:    "empty token",
			token:   "",
			wantErr: nil, // Will wrap the error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := mgr.ValidateAccessToken(tt.token)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Nil(t, claims)
			} else if tt.name == "valid token" {
				require.NoError(t, err)
				require.NotNil(t, claims)
				require.Equal(t, user.Username, claims.Username)
				require.Equal(t, user.ID.String(), claims.Subject)
				require.Equal(t, "kubetty-test", claims.Issuer)
			} else {
				// Malformed tokens should error
				require.Error(t, err)
				require.Nil(t, claims)
			}
		})
	}
}

func TestValidateAccessToken_Expired(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	user := &User{
		ID:           userID,
		Username:     "testuser",
		PasswordHash: hashPassword(t, "password123"),
		IsActive:     true,
		CreatedAt:    fixedTime,
		UpdatedAt:    fixedTime,
	}

	store := NewMockStore()
	store.AddUser(user)

	// Create a manager with a very short TTL (1 nanosecond)
	secret := base64.StdEncoding.EncodeToString([]byte("test-secret-must-be-at-least-32-bytes-long!"))
	mgr, err := NewManager(store, secret, "kubetty-test", 1*time.Nanosecond, 7*24*time.Hour)
	require.NoError(t, err)
	mgr.now = func() time.Time { return fixedTime }

	// Issue a token with 1ns TTL
	pair, err := mgr.IssueTokenPair(ctx, user, TokenMetadata{})
	require.NoError(t, err)

	// Sleep briefly to ensure expiration
	time.Sleep(10 * time.Millisecond)

	// Validate - should be expired
	claims, err := mgr.ValidateAccessToken(pair.AccessToken)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrTokenExpired)
	require.Nil(t, claims)
}

func TestRefresh(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	user := &User{
		ID:           userID,
		Username:     "testuser",
		PasswordHash: hashPassword(t, "password123"),
		IsActive:     true,
		CreatedAt:    fixedTime,
		UpdatedAt:    fixedTime,
	}

	tests := []struct {
		name    string
		setup   func(*MockStore) string
		meta    TokenMetadata
		wantErr error
	}{
		{
			name: "successful refresh",
			setup: func(store *MockStore) string {
				store.AddUser(user)
				mgr := newTestManager(t, store)
				pair, err := mgr.IssueTokenPair(ctx, user, TokenMetadata{})
				require.NoError(t, err)
				return pair.RefreshToken
			},
			meta:    TokenMetadata{CreatedBy: "refresh-client"},
			wantErr: nil,
		},
		{
			name: "revoked token",
			setup: func(store *MockStore) string {
				store.AddUser(user)
				mgr := newTestManager(t, store)
				pair, err := mgr.IssueTokenPair(ctx, user, TokenMetadata{})
				require.NoError(t, err)
				// Revoke the token
				revokedAt := fixedTime
				err = store.RevokeRefreshToken(ctx, pair.RefreshTokenID, revokedAt)
				require.NoError(t, err)
				return pair.RefreshToken
			},
			meta:    TokenMetadata{},
			wantErr: ErrTokenRevoked,
		},
		{
			name: "malformed token",
			setup: func(store *MockStore) string {
				return "not-a-valid-token"
			},
			meta:    TokenMetadata{},
			wantErr: ErrTokenMalformed,
		},
		{
			name: "token not found",
			setup: func(store *MockStore) string {
				store.AddUser(user)
				// Create a valid-looking token that doesn't exist in store
				tokenID := uuid.New()
				secret := base64.RawURLEncoding.EncodeToString([]byte("fake-secret-that-is-32-bytes!!"))
				return tokenID.String() + RefreshTokenDelimiter + secret
			},
			meta:    TokenMetadata{},
			wantErr: ErrRefreshTokenNotFound,
		},
		{
			name: "inactive user",
			setup: func(store *MockStore) string {
				store.AddUser(user)
				mgr := newTestManager(t, store)
				pair, err := mgr.IssueTokenPair(ctx, user, TokenMetadata{})
				require.NoError(t, err)
				// Deactivate user
				err = store.SetUserActive(ctx, userID, false)
				require.NoError(t, err)
				return pair.RefreshToken
			},
			meta:    TokenMetadata{},
			wantErr: ErrInvalidCredentials,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			token := tt.setup(store)
			mgr := newTestManager(t, store)

			pair, err := mgr.Refresh(ctx, token, tt.meta)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Nil(t, pair)
			} else {
				require.NoError(t, err)
				require.NotNil(t, pair)
				require.NotEmpty(t, pair.AccessToken)
				require.NotEmpty(t, pair.RefreshToken)
				// Old token should be revoked
				tokenID, _, err := ParseRefreshToken(token)
				require.NoError(t, err)
				oldToken, err := store.GetRefreshToken(ctx, tokenID)
				require.NoError(t, err)
				require.NotNil(t, oldToken.RevokedAt)
			}
		})
	}
}

func TestParseRefreshToken(t *testing.T) {
	validTokenID := uuid.New()
	validSecret := base64.RawURLEncoding.EncodeToString([]byte("test-secret-32-bytes-length!"))
	validToken := validTokenID.String() + RefreshTokenDelimiter + validSecret

	tests := []struct {
		name    string
		token   string
		wantErr error
	}{
		{
			name:    "valid token",
			token:   validToken,
			wantErr: nil,
		},
		{
			name:    "missing delimiter",
			token:   "no-delimiter-token",
			wantErr: ErrTokenMalformed,
		},
		{
			name:    "too many parts",
			token:   "part1.part2.part3",
			wantErr: ErrTokenMalformed,
		},
		{
			name:    "invalid uuid",
			token:   "not-a-uuid.secret",
			wantErr: ErrTokenMalformed,
		},
		{
			name:    "invalid base64 secret",
			token:   validTokenID.String() + ".invalid-base64!!!",
			wantErr: ErrTokenMalformed,
		},
		{
			name:    "empty token",
			token:   "",
			wantErr: ErrTokenMalformed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenID, secret, err := ParseRefreshToken(tt.token)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Equal(t, uuid.Nil, tokenID)
				require.Nil(t, secret)
			} else {
				require.NoError(t, err)
				require.NotEqual(t, uuid.Nil, tokenID)
				require.NotNil(t, secret)
				require.Greater(t, len(secret), 0)
			}
		})
	}
}
