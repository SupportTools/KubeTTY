package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestNewMockStore verifies the mock store constructor creates valid maps.
func TestNewMockStore(t *testing.T) {
	store := NewMockStore()

	require.NotNil(t, store)
	require.NotNil(t, store.users)
	require.NotNil(t, store.usersByName)
	require.NotNil(t, store.refreshTokens)
	require.Len(t, store.users, 0)
	require.Len(t, store.usersByName, 0)
	require.Len(t, store.refreshTokens, 0)
}

// TestMockStore_SetError verifies error injection and clearing.
func TestMockStore_SetError(t *testing.T) {
	ctx := context.Background()
	store := NewMockStore()
	testErr := errors.New("test error")

	// Set error
	store.SetError(testErr)

	// First call should return error
	_, err := store.GetUser(ctx, uuid.New())
	require.Equal(t, testErr, err)

	// Second call should work (error cleared)
	_, err = store.GetUser(ctx, uuid.New())
	require.Equal(t, ErrUserNotFound, err)
}

// TestMockStore_AddUser verifies the AddUser helper method.
func TestMockStore_AddUser(t *testing.T) {
	ctx := context.Background()
	store := NewMockStore()

	userID := uuid.New()
	user := &User{
		ID:       userID,
		Username: "testuser",
		IsActive: true,
	}

	store.AddUser(user)

	// Verify user can be retrieved by ID
	retrieved, err := store.GetUser(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, user.Username, retrieved.Username)

	// Verify user can be retrieved by username
	retrieved, err = store.GetUserByUsername(ctx, "testuser")
	require.NoError(t, err)
	require.Equal(t, userID, retrieved.ID)
}

// TestMockStore_AddRefreshToken verifies the AddRefreshToken helper method.
func TestMockStore_AddRefreshToken(t *testing.T) {
	ctx := context.Background()
	store := NewMockStore()

	tokenID := uuid.New()
	userID := uuid.New()
	token := &RefreshToken{
		ID:        uuid.New(),
		TokenID:   tokenID,
		UserID:    userID,
		TokenHash: []byte("hash"),
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	store.AddRefreshToken(token)

	// Verify token can be retrieved
	retrieved, err := store.GetRefreshToken(ctx, tokenID)
	require.NoError(t, err)
	require.Equal(t, userID, retrieved.UserID)
	require.Equal(t, tokenID, retrieved.TokenID)
}

// TestMockStore_GetUser verifies GetUser with various scenarios.
func TestMockStore_GetUser(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*MockStore) uuid.UUID
		wantErr error
	}{
		{
			name: "user found",
			setup: func(store *MockStore) uuid.UUID {
				userID := uuid.New()
				store.AddUser(&User{ID: userID, Username: "test"})
				return userID
			},
			wantErr: nil,
		},
		{
			name: "user not found",
			setup: func(store *MockStore) uuid.UUID {
				return uuid.New()
			},
			wantErr: ErrUserNotFound,
		},
		{
			name: "store error",
			setup: func(store *MockStore) uuid.UUID {
				store.SetError(errors.New("db error"))
				return uuid.New()
			},
			wantErr: errors.New("db error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			userID := tt.setup(store)

			user, err := store.GetUser(ctx, userID)

			if tt.wantErr != nil {
				require.Error(t, err)
				if errors.Is(tt.wantErr, ErrUserNotFound) {
					require.ErrorIs(t, err, ErrUserNotFound)
				}
				require.Nil(t, user)
			} else {
				require.NoError(t, err)
				require.NotNil(t, user)
			}
		})
	}
}

// TestMockStore_GetUserByUsername verifies GetUserByUsername with various scenarios.
func TestMockStore_GetUserByUsername(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		username string
		setup    func(*MockStore)
		wantErr  error
	}{
		{
			name:     "user found",
			username: "testuser",
			setup: func(store *MockStore) {
				store.AddUser(&User{ID: uuid.New(), Username: "testuser"})
			},
			wantErr: nil,
		},
		{
			name:     "user not found",
			username: "nonexistent",
			setup:    func(store *MockStore) {},
			wantErr:  ErrUserNotFound,
		},
		{
			name:     "store error",
			username: "testuser",
			setup: func(store *MockStore) {
				store.SetError(errors.New("db error"))
			},
			wantErr: errors.New("db error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			tt.setup(store)

			user, err := store.GetUserByUsername(ctx, tt.username)

			if tt.wantErr != nil {
				require.Error(t, err)
				require.Nil(t, user)
			} else {
				require.NoError(t, err)
				require.NotNil(t, user)
				require.Equal(t, tt.username, user.Username)
			}
		})
	}
}

// TestMockStore_ListUsers verifies ListUsers returns all users.
func TestMockStore_ListUsers(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		setup     func(*MockStore)
		wantCount int
		wantErr   bool
	}{
		{
			name:      "empty store",
			setup:     func(store *MockStore) {},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name: "single user",
			setup: func(store *MockStore) {
				store.AddUser(&User{ID: uuid.New(), Username: "user1"})
			},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name: "multiple users",
			setup: func(store *MockStore) {
				store.AddUser(&User{ID: uuid.New(), Username: "user1"})
				store.AddUser(&User{ID: uuid.New(), Username: "user2"})
				store.AddUser(&User{ID: uuid.New(), Username: "user3"})
			},
			wantCount: 3,
			wantErr:   false,
		},
		{
			name: "store error",
			setup: func(store *MockStore) {
				store.AddUser(&User{ID: uuid.New(), Username: "user1"})
				store.SetError(errors.New("db error"))
			},
			wantCount: 0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			tt.setup(store)

			users, err := store.ListUsers(ctx)

			if tt.wantErr {
				require.Error(t, err)
				require.Nil(t, users)
			} else {
				require.NoError(t, err)
				require.Len(t, users, tt.wantCount)
			}
		})
	}
}

// TestMockStore_CreateUser verifies CreateUser with various scenarios.
func TestMockStore_CreateUser(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*MockStore)
		user    User
		wantErr error
	}{
		{
			name:  "create new user",
			setup: func(store *MockStore) {},
			user: User{
				ID:       uuid.New(),
				Username: "newuser",
				IsActive: true,
			},
			wantErr: nil,
		},
		{
			name: "duplicate username",
			setup: func(store *MockStore) {
				store.AddUser(&User{ID: uuid.New(), Username: "existinguser"})
			},
			user: User{
				ID:       uuid.New(),
				Username: "existinguser",
				IsActive: true,
			},
			wantErr: ErrDuplicateUsername,
		},
		{
			name: "store error",
			setup: func(store *MockStore) {
				store.SetError(errors.New("db error"))
			},
			user: User{
				ID:       uuid.New(),
				Username: "newuser",
				IsActive: true,
			},
			wantErr: errors.New("db error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			tt.setup(store)

			err := store.CreateUser(ctx, tt.user)

			if tt.wantErr != nil {
				require.Error(t, err)
				if errors.Is(tt.wantErr, ErrDuplicateUsername) {
					require.ErrorIs(t, err, ErrDuplicateUsername)
				}
			} else {
				require.NoError(t, err)
				// Verify user was created
				retrieved, err := store.GetUser(ctx, tt.user.ID)
				require.NoError(t, err)
				require.Equal(t, tt.user.Username, retrieved.Username)
			}
		})
	}
}

// TestMockStore_UpdateUserPassword verifies password updates.
func TestMockStore_UpdateUserPassword(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*MockStore) uuid.UUID
		newHash []byte
		wantErr error
	}{
		{
			name: "successful update",
			setup: func(store *MockStore) uuid.UUID {
				userID := uuid.New()
				store.AddUser(&User{
					ID:           userID,
					Username:     "testuser",
					PasswordHash: []byte("oldhash"),
				})
				return userID
			},
			newHash: []byte("newhash"),
			wantErr: nil,
		},
		{
			name: "user not found",
			setup: func(store *MockStore) uuid.UUID {
				return uuid.New()
			},
			newHash: []byte("newhash"),
			wantErr: ErrUserNotFound,
		},
		{
			name: "store error",
			setup: func(store *MockStore) uuid.UUID {
				userID := uuid.New()
				store.AddUser(&User{ID: userID, Username: "testuser"})
				store.SetError(errors.New("db error"))
				return userID
			},
			newHash: []byte("newhash"),
			wantErr: errors.New("db error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			userID := tt.setup(store)

			err := store.UpdateUserPassword(ctx, userID, tt.newHash)

			if tt.wantErr != nil {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				// Verify password was updated
				user, err := store.GetUser(ctx, userID)
				require.NoError(t, err)
				require.Equal(t, tt.newHash, user.PasswordHash)
			}
		})
	}
}

// TestMockStore_SetUserActive verifies active status updates.
func TestMockStore_SetUserActive(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*MockStore) uuid.UUID
		active  bool
		wantErr error
	}{
		{
			name: "deactivate user",
			setup: func(store *MockStore) uuid.UUID {
				userID := uuid.New()
				store.AddUser(&User{ID: userID, Username: "testuser", IsActive: true})
				return userID
			},
			active:  false,
			wantErr: nil,
		},
		{
			name: "activate user",
			setup: func(store *MockStore) uuid.UUID {
				userID := uuid.New()
				store.AddUser(&User{ID: userID, Username: "testuser", IsActive: false})
				return userID
			},
			active:  true,
			wantErr: nil,
		},
		{
			name: "user not found",
			setup: func(store *MockStore) uuid.UUID {
				return uuid.New()
			},
			active:  true,
			wantErr: ErrUserNotFound,
		},
		{
			name: "store error",
			setup: func(store *MockStore) uuid.UUID {
				userID := uuid.New()
				store.AddUser(&User{ID: userID, Username: "testuser"})
				store.SetError(errors.New("db error"))
				return userID
			},
			active:  false,
			wantErr: errors.New("db error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			userID := tt.setup(store)

			err := store.SetUserActive(ctx, userID, tt.active)

			if tt.wantErr != nil {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				// Verify active status was updated
				user, err := store.GetUser(ctx, userID)
				require.NoError(t, err)
				require.Equal(t, tt.active, user.IsActive)
			}
		})
	}
}

// TestMockStore_UpdateLastLogin verifies last login timestamp updates.
func TestMockStore_UpdateLastLogin(t *testing.T) {
	ctx := context.Background()
	loginTime := time.Now()

	tests := []struct {
		name    string
		setup   func(*MockStore) uuid.UUID
		wantErr error
	}{
		{
			name: "successful update",
			setup: func(store *MockStore) uuid.UUID {
				userID := uuid.New()
				store.AddUser(&User{ID: userID, Username: "testuser"})
				return userID
			},
			wantErr: nil,
		},
		{
			name: "user not found",
			setup: func(store *MockStore) uuid.UUID {
				return uuid.New()
			},
			wantErr: ErrUserNotFound,
		},
		{
			name: "store error",
			setup: func(store *MockStore) uuid.UUID {
				userID := uuid.New()
				store.AddUser(&User{ID: userID, Username: "testuser"})
				store.SetError(errors.New("db error"))
				return userID
			},
			wantErr: errors.New("db error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			userID := tt.setup(store)

			err := store.UpdateLastLogin(ctx, userID, loginTime)

			if tt.wantErr != nil {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				// Verify last login was updated
				user, err := store.GetUser(ctx, userID)
				require.NoError(t, err)
				require.NotNil(t, user.LastLoginAt)
				require.Equal(t, loginTime, *user.LastLoginAt)
			}
		})
	}
}

// TestMockStore_InsertRefreshToken verifies token insertion.
func TestMockStore_InsertRefreshToken(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*MockStore)
		token   RefreshToken
		wantErr bool
	}{
		{
			name:  "successful insert",
			setup: func(store *MockStore) {},
			token: RefreshToken{
				ID:        uuid.New(),
				TokenID:   uuid.New(),
				UserID:    uuid.New(),
				TokenHash: []byte("hash"),
				IssuedAt:  time.Now(),
				ExpiresAt: time.Now().Add(24 * time.Hour),
			},
			wantErr: false,
		},
		{
			name: "store error",
			setup: func(store *MockStore) {
				store.SetError(errors.New("db error"))
			},
			token: RefreshToken{
				ID:        uuid.New(),
				TokenID:   uuid.New(),
				UserID:    uuid.New(),
				TokenHash: []byte("hash"),
				IssuedAt:  time.Now(),
				ExpiresAt: time.Now().Add(24 * time.Hour),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			tt.setup(store)

			err := store.InsertRefreshToken(ctx, tt.token)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				// Verify token was inserted
				retrieved, err := store.GetRefreshToken(ctx, tt.token.TokenID)
				require.NoError(t, err)
				require.Equal(t, tt.token.UserID, retrieved.UserID)
			}
		})
	}
}

// TestMockStore_GetRefreshToken verifies token retrieval.
func TestMockStore_GetRefreshToken(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*MockStore) uuid.UUID
		wantErr error
	}{
		{
			name: "token found",
			setup: func(store *MockStore) uuid.UUID {
				tokenID := uuid.New()
				store.AddRefreshToken(&RefreshToken{
					TokenID:   tokenID,
					UserID:    uuid.New(),
					ExpiresAt: time.Now().Add(24 * time.Hour),
				})
				return tokenID
			},
			wantErr: nil,
		},
		{
			name: "token not found",
			setup: func(store *MockStore) uuid.UUID {
				return uuid.New()
			},
			wantErr: ErrRefreshTokenNotFound,
		},
		{
			name: "store error",
			setup: func(store *MockStore) uuid.UUID {
				tokenID := uuid.New()
				store.AddRefreshToken(&RefreshToken{TokenID: tokenID, UserID: uuid.New(), ExpiresAt: time.Now()})
				store.SetError(errors.New("db error"))
				return tokenID
			},
			wantErr: errors.New("db error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			tokenID := tt.setup(store)

			token, err := store.GetRefreshToken(ctx, tokenID)

			if tt.wantErr != nil {
				require.Error(t, err)
				require.Nil(t, token)
			} else {
				require.NoError(t, err)
				require.NotNil(t, token)
				require.Equal(t, tokenID, token.TokenID)
			}
		})
	}
}

// TestMockStore_RevokeRefreshToken verifies token revocation.
func TestMockStore_RevokeRefreshToken(t *testing.T) {
	ctx := context.Background()
	revokedAt := time.Now()

	tests := []struct {
		name    string
		setup   func(*MockStore) uuid.UUID
		wantErr error
	}{
		{
			name: "successful revocation",
			setup: func(store *MockStore) uuid.UUID {
				tokenID := uuid.New()
				store.AddRefreshToken(&RefreshToken{
					TokenID:   tokenID,
					UserID:    uuid.New(),
					ExpiresAt: time.Now().Add(24 * time.Hour),
				})
				return tokenID
			},
			wantErr: nil,
		},
		{
			name: "token not found",
			setup: func(store *MockStore) uuid.UUID {
				return uuid.New()
			},
			wantErr: ErrRefreshTokenNotFound,
		},
		{
			name: "store error",
			setup: func(store *MockStore) uuid.UUID {
				tokenID := uuid.New()
				store.AddRefreshToken(&RefreshToken{TokenID: tokenID, UserID: uuid.New(), ExpiresAt: time.Now()})
				store.SetError(errors.New("db error"))
				return tokenID
			},
			wantErr: errors.New("db error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			tokenID := tt.setup(store)

			err := store.RevokeRefreshToken(ctx, tokenID, revokedAt)

			if tt.wantErr != nil {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				// Verify token was revoked
				token, err := store.GetRefreshToken(ctx, tokenID)
				require.NoError(t, err)
				require.NotNil(t, token.RevokedAt)
				require.Equal(t, revokedAt, *token.RevokedAt)
			}
		})
	}
}

// TestMockStore_RevokeRefreshTokensByUser verifies bulk user token revocation.
func TestMockStore_RevokeRefreshTokensByUser(t *testing.T) {
	ctx := context.Background()
	revokedAt := time.Now()

	tests := []struct {
		name      string
		setup     func(*MockStore) uuid.UUID
		wantCount int64
		wantErr   bool
	}{
		{
			name: "revoke multiple tokens",
			setup: func(store *MockStore) uuid.UUID {
				userID := uuid.New()
				store.AddRefreshToken(&RefreshToken{TokenID: uuid.New(), UserID: userID, ExpiresAt: time.Now().Add(24 * time.Hour)})
				store.AddRefreshToken(&RefreshToken{TokenID: uuid.New(), UserID: userID, ExpiresAt: time.Now().Add(24 * time.Hour)})
				store.AddRefreshToken(&RefreshToken{TokenID: uuid.New(), UserID: userID, ExpiresAt: time.Now().Add(24 * time.Hour)})
				return userID
			},
			wantCount: 3,
			wantErr:   false,
		},
		{
			name: "no tokens for user",
			setup: func(store *MockStore) uuid.UUID {
				return uuid.New()
			},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name: "skip already revoked tokens",
			setup: func(store *MockStore) uuid.UUID {
				userID := uuid.New()
				oldRevoked := time.Now().Add(-1 * time.Hour)
				store.AddRefreshToken(&RefreshToken{TokenID: uuid.New(), UserID: userID, ExpiresAt: time.Now().Add(24 * time.Hour), RevokedAt: &oldRevoked})
				store.AddRefreshToken(&RefreshToken{TokenID: uuid.New(), UserID: userID, ExpiresAt: time.Now().Add(24 * time.Hour)})
				return userID
			},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name: "store error",
			setup: func(store *MockStore) uuid.UUID {
				userID := uuid.New()
				store.AddRefreshToken(&RefreshToken{TokenID: uuid.New(), UserID: userID, ExpiresAt: time.Now()})
				store.SetError(errors.New("db error"))
				return userID
			},
			wantCount: 0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			userID := tt.setup(store)

			count, err := store.RevokeRefreshTokensByUser(ctx, userID, revokedAt)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantCount, count)
			}
		})
	}
}

// TestMockStore_DeleteExpiredRefreshTokens verifies expired token cleanup.
func TestMockStore_DeleteExpiredRefreshTokens(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	cutoff := now

	tests := []struct {
		name      string
		setup     func(*MockStore)
		wantCount int64
		wantErr   bool
	}{
		{
			name: "delete expired tokens",
			setup: func(store *MockStore) {
				// Expired token
				store.AddRefreshToken(&RefreshToken{
					TokenID:   uuid.New(),
					UserID:    uuid.New(),
					ExpiresAt: now.Add(-1 * time.Hour),
				})
				// Another expired token
				store.AddRefreshToken(&RefreshToken{
					TokenID:   uuid.New(),
					UserID:    uuid.New(),
					ExpiresAt: now.Add(-2 * time.Hour),
				})
				// Valid token (not expired)
				store.AddRefreshToken(&RefreshToken{
					TokenID:   uuid.New(),
					UserID:    uuid.New(),
					ExpiresAt: now.Add(24 * time.Hour),
				})
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name: "no expired tokens",
			setup: func(store *MockStore) {
				store.AddRefreshToken(&RefreshToken{
					TokenID:   uuid.New(),
					UserID:    uuid.New(),
					ExpiresAt: now.Add(24 * time.Hour),
				})
			},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "empty store",
			setup:     func(store *MockStore) {},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name: "store error",
			setup: func(store *MockStore) {
				store.AddRefreshToken(&RefreshToken{
					TokenID:   uuid.New(),
					UserID:    uuid.New(),
					ExpiresAt: now.Add(-1 * time.Hour),
				})
				store.SetError(errors.New("db error"))
			},
			wantCount: 0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			tt.setup(store)

			count, err := store.DeleteExpiredRefreshTokens(ctx, cutoff)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantCount, count)
			}
		})
	}
}

// TestMockStore_ConcurrentAccess verifies thread safety of the mock store.
func TestMockStore_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	store := NewMockStore()
	userID := uuid.New()
	store.AddUser(&User{ID: userID, Username: "testuser", IsActive: true})

	done := make(chan bool)

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func() {
			_, _ = store.GetUser(ctx, userID)
			_, _ = store.ListUsers(ctx)
			done <- true
		}()
	}

	// Concurrent writes
	for i := 0; i < 10; i++ {
		go func() {
			tokenID := uuid.New()
			_ = store.InsertRefreshToken(ctx, RefreshToken{
				TokenID:   tokenID,
				UserID:    userID,
				ExpiresAt: time.Now().Add(time.Hour),
			})
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 20; i++ {
		<-done
	}
}

// TestMockStore_UserFields verifies user fields are preserved.
func TestMockStore_UserFields(t *testing.T) {
	ctx := context.Background()
	store := NewMockStore()

	now := time.Now()
	lastLogin := now.Add(-24 * time.Hour)
	user := User{
		ID:           uuid.New(),
		Username:     "testuser",
		PasswordHash: []byte("hashedpassword"),
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
		LastLoginAt:  &lastLogin,
	}

	err := store.CreateUser(ctx, user)
	require.NoError(t, err)

	retrieved, err := store.GetUser(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, user.Username, retrieved.Username)
	require.Equal(t, user.PasswordHash, retrieved.PasswordHash)
	require.Equal(t, user.IsActive, retrieved.IsActive)
	require.NotNil(t, retrieved.LastLoginAt)
}

// TestMockStore_RefreshTokenFields verifies refresh token fields are preserved.
func TestMockStore_RefreshTokenFields(t *testing.T) {
	ctx := context.Background()
	store := NewMockStore()

	now := time.Now()
	token := RefreshToken{
		ID:        uuid.New(),
		TokenID:   uuid.New(),
		UserID:    uuid.New(),
		TokenHash: []byte("tokenhash"),
		IssuedAt:  now,
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedBy: "test-client",
		UserAgent: "Mozilla/5.0",
		ClientIP:  "192.168.1.1",
	}

	err := store.InsertRefreshToken(ctx, token)
	require.NoError(t, err)

	retrieved, err := store.GetRefreshToken(ctx, token.TokenID)
	require.NoError(t, err)
	require.Equal(t, token.UserID, retrieved.UserID)
	require.Equal(t, token.TokenHash, retrieved.TokenHash)
	require.Equal(t, token.CreatedBy, retrieved.CreatedBy)
	require.Equal(t, token.UserAgent, retrieved.UserAgent)
	require.Equal(t, token.ClientIP, retrieved.ClientIP)
}
