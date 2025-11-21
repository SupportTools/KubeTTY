package auth

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

const testConnString = "postgres://postgres:postgres@localhost:5432/kubetty_test?sslmode=disable"

// newTestStore creates a PGStore connected to the test database.
// Skips the test if the database is not available.
func newTestStore(t *testing.T) *PGStore {
	t.Helper()
	ctx := context.Background()
	config, err := pgxpool.ParseConfig(testConnString)
	if err != nil {
		t.Skipf("Skipping database test: failed to parse connection string: %v", err)
	}

	store, err := NewStore(ctx, config)
	if err != nil {
		t.Skipf("Skipping database test: database not available: %v", err)
	}
	if store == nil {
		t.Skip("Skipping database test: store is nil")
	}

	return store
}

// cleanupUsers removes all users from the test database.
func cleanupUsers(t *testing.T, ctx context.Context, store *PGStore) {
	t.Helper()
	_, err := store.pool.Exec(ctx, "DELETE FROM kubetty_users")
	require.NoError(t, err)
}

// cleanupRefreshTokens removes all refresh tokens from the test database.
func cleanupRefreshTokens(t *testing.T, ctx context.Context, store *PGStore) {
	t.Helper()
	_, err := store.pool.Exec(ctx, "DELETE FROM kubetty_refresh_tokens")
	require.NoError(t, err)
}

// cleanupAll removes all test data from the database.
func cleanupAll(t *testing.T, ctx context.Context, store *PGStore) {
	t.Helper()
	cleanupRefreshTokens(t, ctx, store)
	cleanupUsers(t, ctx, store)
}

func TestPGStore_CreateUser(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()
	defer cleanupAll(t, ctx, store)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	require.NoError(t, err)

	t.Run("create user with all fields", func(t *testing.T) {
		cleanupUsers(t, ctx, store)

		userID := uuid.New()
		now := time.Now()
		user := User{
			ID:           userID,
			Username:     "testuser1",
			PasswordHash: passwordHash,
			IsActive:     true,
			CreatedAt:    now,
			UpdatedAt:    now,
			LastLoginAt:  &now,
		}

		err := store.CreateUser(ctx, user)
		require.NoError(t, err)

		// Verify user was created
		retrieved, err := store.GetUser(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, user.ID, retrieved.ID)
		require.Equal(t, user.Username, retrieved.Username)
		require.Equal(t, user.PasswordHash, retrieved.PasswordHash)
		require.Equal(t, user.IsActive, retrieved.IsActive)
		require.NotNil(t, retrieved.LastLoginAt)
	})

	t.Run("create user with auto-generated ID", func(t *testing.T) {
		cleanupUsers(t, ctx, store)

		user := User{
			Username:     "testuser2",
			PasswordHash: passwordHash,
			IsActive:     true,
		}

		err := store.CreateUser(ctx, user)
		require.NoError(t, err)

		// Verify by username lookup
		retrieved, err := store.GetUserByUsername(ctx, "testuser2")
		require.NoError(t, err)
		require.NotEqual(t, uuid.Nil, retrieved.ID)
		require.Equal(t, "testuser2", retrieved.Username)
	})

	t.Run("duplicate username", func(t *testing.T) {
		cleanupUsers(t, ctx, store)

		user1 := User{
			ID:           uuid.New(),
			Username:     "duplicate",
			PasswordHash: passwordHash,
			IsActive:     true,
		}
		err := store.CreateUser(ctx, user1)
		require.NoError(t, err)

		// Try to create another user with same username
		user2 := User{
			ID:           uuid.New(),
			Username:     "duplicate",
			PasswordHash: passwordHash,
			IsActive:     true,
		}
		err = store.CreateUser(ctx, user2)
		require.Error(t, err)
		// PostgreSQL unique constraint violation
		require.Contains(t, err.Error(), "create user")
	})
}

func TestPGStore_GetUser(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()
	defer cleanupAll(t, ctx, store)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	require.NoError(t, err)

	userID := uuid.New()
	user := User{
		ID:           userID,
		Username:     "getuser",
		PasswordHash: passwordHash,
		IsActive:     true,
	}
	err = store.CreateUser(ctx, user)
	require.NoError(t, err)

	t.Run("get existing user", func(t *testing.T) {
		retrieved, err := store.GetUser(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, userID, retrieved.ID)
		require.Equal(t, "getuser", retrieved.Username)
		require.True(t, retrieved.IsActive)
	})

	t.Run("get non-existent user", func(t *testing.T) {
		_, err := store.GetUser(ctx, uuid.New())
		require.ErrorIs(t, err, ErrUserNotFound)
	})
}

func TestPGStore_GetUserByUsername(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()
	defer cleanupAll(t, ctx, store)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	require.NoError(t, err)

	user := User{
		ID:           uuid.New(),
		Username:     "findme",
		PasswordHash: passwordHash,
		IsActive:     false,
	}
	err = store.CreateUser(ctx, user)
	require.NoError(t, err)

	t.Run("get existing user by username", func(t *testing.T) {
		retrieved, err := store.GetUserByUsername(ctx, "findme")
		require.NoError(t, err)
		require.Equal(t, user.ID, retrieved.ID)
		require.Equal(t, "findme", retrieved.Username)
		require.False(t, retrieved.IsActive)
	})

	t.Run("get non-existent user by username", func(t *testing.T) {
		_, err := store.GetUserByUsername(ctx, "nonexistent")
		require.ErrorIs(t, err, ErrUserNotFound)
	})
}

func TestPGStore_ListUsers(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()
	defer cleanupAll(t, ctx, store)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	require.NoError(t, err)

	t.Run("list empty", func(t *testing.T) {
		cleanupUsers(t, ctx, store)

		users, err := store.ListUsers(ctx)
		require.NoError(t, err)
		require.Empty(t, users)
	})

	t.Run("list multiple users", func(t *testing.T) {
		cleanupUsers(t, ctx, store)

		for i := 1; i <= 3; i++ {
			user := User{
				ID:           uuid.New(),
				Username:     "user" + string(rune('0'+i)),
				PasswordHash: passwordHash,
				IsActive:     i%2 == 0,
			}
			err := store.CreateUser(ctx, user)
			require.NoError(t, err)
		}

		users, err := store.ListUsers(ctx)
		require.NoError(t, err)
		require.Len(t, users, 3)
	})
}

func TestPGStore_UpdateUserPassword(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()
	defer cleanupAll(t, ctx, store)

	oldHash, err := bcrypt.GenerateFromPassword([]byte("oldpassword"), bcrypt.MinCost)
	require.NoError(t, err)

	userID := uuid.New()
	user := User{
		ID:           userID,
		Username:     "changepass",
		PasswordHash: oldHash,
		IsActive:     true,
	}
	err = store.CreateUser(ctx, user)
	require.NoError(t, err)

	t.Run("update password success", func(t *testing.T) {
		newHash, err := bcrypt.GenerateFromPassword([]byte("newpassword"), bcrypt.MinCost)
		require.NoError(t, err)

		err = store.UpdateUserPassword(ctx, userID, newHash)
		require.NoError(t, err)

		// Verify password was updated
		retrieved, err := store.GetUser(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, newHash, retrieved.PasswordHash)
	})

	t.Run("update password for non-existent user", func(t *testing.T) {
		newHash, err := bcrypt.GenerateFromPassword([]byte("newpassword"), bcrypt.MinCost)
		require.NoError(t, err)

		err = store.UpdateUserPassword(ctx, uuid.New(), newHash)
		require.ErrorIs(t, err, ErrUserNotFound)
	})
}

func TestPGStore_SetUserActive(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()
	defer cleanupAll(t, ctx, store)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	require.NoError(t, err)

	userID := uuid.New()
	user := User{
		ID:           userID,
		Username:     "activetest",
		PasswordHash: passwordHash,
		IsActive:     true,
	}
	err = store.CreateUser(ctx, user)
	require.NoError(t, err)

	t.Run("deactivate user", func(t *testing.T) {
		err := store.SetUserActive(ctx, userID, false)
		require.NoError(t, err)

		retrieved, err := store.GetUser(ctx, userID)
		require.NoError(t, err)
		require.False(t, retrieved.IsActive)
	})

	t.Run("activate user", func(t *testing.T) {
		err := store.SetUserActive(ctx, userID, true)
		require.NoError(t, err)

		retrieved, err := store.GetUser(ctx, userID)
		require.NoError(t, err)
		require.True(t, retrieved.IsActive)
	})

	t.Run("set active for non-existent user", func(t *testing.T) {
		err := store.SetUserActive(ctx, uuid.New(), true)
		require.ErrorIs(t, err, ErrUserNotFound)
	})
}

func TestPGStore_UpdateLastLogin(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()
	defer cleanupAll(t, ctx, store)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	require.NoError(t, err)

	userID := uuid.New()
	user := User{
		ID:           userID,
		Username:     "logintest",
		PasswordHash: passwordHash,
		IsActive:     true,
	}
	err = store.CreateUser(ctx, user)
	require.NoError(t, err)

	t.Run("update last login", func(t *testing.T) {
		loginTime := time.Now().Add(-1 * time.Hour)
		err := store.UpdateLastLogin(ctx, userID, loginTime)
		require.NoError(t, err)

		retrieved, err := store.GetUser(ctx, userID)
		require.NoError(t, err)
		require.NotNil(t, retrieved.LastLoginAt)
		// Allow small time difference due to database precision
		require.WithinDuration(t, loginTime, *retrieved.LastLoginAt, time.Second)
	})

	t.Run("update last login for non-existent user", func(t *testing.T) {
		err := store.UpdateLastLogin(ctx, uuid.New(), time.Now())
		require.ErrorIs(t, err, ErrUserNotFound)
	})
}

func TestPGStore_RefreshTokens(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()
	defer cleanupAll(t, ctx, store)

	// Create a user first
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	require.NoError(t, err)
	userID := uuid.New()
	user := User{
		ID:           userID,
		Username:     "tokenuser",
		PasswordHash: passwordHash,
		IsActive:     true,
	}
	err = store.CreateUser(ctx, user)
	require.NoError(t, err)

	t.Run("insert and get refresh token", func(t *testing.T) {
		cleanupRefreshTokens(t, ctx, store)

		tokenID := uuid.New()
		now := time.Now()
		token := RefreshToken{
			TokenID:   tokenID,
			UserID:    userID,
			TokenHash: []byte("hash123"),
			IssuedAt:  now,
			ExpiresAt: now.Add(7 * 24 * time.Hour),
			CreatedBy: "test-client",
			UserAgent: "Mozilla/5.0",
			ClientIP:  "192.168.1.1",
		}

		err := store.InsertRefreshToken(ctx, token)
		require.NoError(t, err)

		// Retrieve token
		retrieved, err := store.GetRefreshToken(ctx, tokenID)
		require.NoError(t, err)
		require.Equal(t, tokenID, retrieved.TokenID)
		require.Equal(t, userID, retrieved.UserID)
		require.Equal(t, []byte("hash123"), retrieved.TokenHash)
		require.Equal(t, "test-client", retrieved.CreatedBy)
		require.Nil(t, retrieved.RevokedAt)
	})

	t.Run("get non-existent token", func(t *testing.T) {
		_, err := store.GetRefreshToken(ctx, uuid.New())
		require.ErrorIs(t, err, ErrRefreshTokenNotFound)
	})

	t.Run("revoke refresh token", func(t *testing.T) {
		cleanupRefreshTokens(t, ctx, store)

		tokenID := uuid.New()
		token := RefreshToken{
			TokenID:   tokenID,
			UserID:    userID,
			TokenHash: []byte("hash456"),
			IssuedAt:  time.Now(),
			ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		}
		err := store.InsertRefreshToken(ctx, token)
		require.NoError(t, err)

		// Revoke the token
		revokedAt := time.Now()
		err = store.RevokeRefreshToken(ctx, tokenID, revokedAt)
		require.NoError(t, err)

		// Verify token is revoked
		retrieved, err := store.GetRefreshToken(ctx, tokenID)
		require.NoError(t, err)
		require.NotNil(t, retrieved.RevokedAt)
		require.WithinDuration(t, revokedAt, *retrieved.RevokedAt, time.Second)
	})

	t.Run("revoke non-existent token", func(t *testing.T) {
		err := store.RevokeRefreshToken(ctx, uuid.New(), time.Now())
		require.ErrorIs(t, err, ErrRefreshTokenNotFound)
	})

	t.Run("revoke already revoked token", func(t *testing.T) {
		cleanupRefreshTokens(t, ctx, store)

		tokenID := uuid.New()
		revokedAt := time.Now()
		token := RefreshToken{
			TokenID:   tokenID,
			UserID:    userID,
			TokenHash: []byte("hash789"),
			IssuedAt:  time.Now(),
			ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
			RevokedAt: &revokedAt,
		}
		err := store.InsertRefreshToken(ctx, token)
		require.NoError(t, err)

		// Try to revoke again
		err = store.RevokeRefreshToken(ctx, tokenID, time.Now())
		require.ErrorIs(t, err, ErrRefreshTokenNotFound)
	})
}

func TestPGStore_RevokeRefreshTokensByUser(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()
	defer cleanupAll(t, ctx, store)

	// Create a user
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	require.NoError(t, err)
	userID := uuid.New()
	user := User{
		ID:           userID,
		Username:     "bulkrevoke",
		PasswordHash: passwordHash,
		IsActive:     true,
	}
	err = store.CreateUser(ctx, user)
	require.NoError(t, err)

	t.Run("revoke multiple tokens for user", func(t *testing.T) {
		cleanupRefreshTokens(t, ctx, store)

		// Create 3 tokens for the user
		for i := 0; i < 3; i++ {
			token := RefreshToken{
				TokenID:   uuid.New(),
				UserID:    userID,
				TokenHash: []byte("hash" + string(rune('0'+i))),
				IssuedAt:  time.Now(),
				ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
			}
			err := store.InsertRefreshToken(ctx, token)
			require.NoError(t, err)
		}

		// Revoke all tokens for user
		revokedAt := time.Now()
		count, err := store.RevokeRefreshTokensByUser(ctx, userID, revokedAt)
		require.NoError(t, err)
		require.Equal(t, int64(3), count)
	})

	t.Run("revoke for user with no tokens", func(t *testing.T) {
		cleanupRefreshTokens(t, ctx, store)

		count, err := store.RevokeRefreshTokensByUser(ctx, userID, time.Now())
		require.NoError(t, err)
		require.Equal(t, int64(0), count)
	})

	t.Run("revoke only non-revoked tokens", func(t *testing.T) {
		cleanupRefreshTokens(t, ctx, store)

		// Create 2 active tokens
		for i := 0; i < 2; i++ {
			token := RefreshToken{
				TokenID:   uuid.New(),
				UserID:    userID,
				TokenHash: []byte("active" + string(rune('0'+i))),
				IssuedAt:  time.Now(),
				ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
			}
			err := store.InsertRefreshToken(ctx, token)
			require.NoError(t, err)
		}

		// Create 1 already revoked token
		revokedTime := time.Now().Add(-1 * time.Hour)
		revokedToken := RefreshToken{
			TokenID:   uuid.New(),
			UserID:    userID,
			TokenHash: []byte("alreadyrevoked"),
			IssuedAt:  time.Now(),
			ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
			RevokedAt: &revokedTime,
		}
		err := store.InsertRefreshToken(ctx, revokedToken)
		require.NoError(t, err)

		// Revoke - should only affect the 2 active tokens
		count, err := store.RevokeRefreshTokensByUser(ctx, userID, time.Now())
		require.NoError(t, err)
		require.Equal(t, int64(2), count)
	})
}

func TestPGStore_DeleteExpiredRefreshTokens(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()
	defer cleanupAll(t, ctx, store)

	// Create a user
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	require.NoError(t, err)
	userID := uuid.New()
	user := User{
		ID:           userID,
		Username:     "deleteexpired",
		PasswordHash: passwordHash,
		IsActive:     true,
	}
	err = store.CreateUser(ctx, user)
	require.NoError(t, err)

	t.Run("delete expired tokens", func(t *testing.T) {
		cleanupRefreshTokens(t, ctx, store)

		now := time.Now()

		// Create 2 expired tokens
		for i := 0; i < 2; i++ {
			token := RefreshToken{
				TokenID:   uuid.New(),
				UserID:    userID,
				TokenHash: []byte("expired" + string(rune('0'+i))),
				IssuedAt:  now.Add(-8 * 24 * time.Hour),
				ExpiresAt: now.Add(-1 * time.Hour),
			}
			err := store.InsertRefreshToken(ctx, token)
			require.NoError(t, err)
		}

		// Create 1 valid token
		validToken := RefreshToken{
			TokenID:   uuid.New(),
			UserID:    userID,
			TokenHash: []byte("valid"),
			IssuedAt:  now,
			ExpiresAt: now.Add(7 * 24 * time.Hour),
		}
		err := store.InsertRefreshToken(ctx, validToken)
		require.NoError(t, err)

		// Delete expired tokens
		count, err := store.DeleteExpiredRefreshTokens(ctx, now)
		require.NoError(t, err)
		require.Equal(t, int64(2), count)

		// Verify valid token still exists
		retrieved, err := store.GetRefreshToken(ctx, validToken.TokenID)
		require.NoError(t, err)
		require.NotNil(t, retrieved)
	})

	t.Run("delete old revoked tokens", func(t *testing.T) {
		cleanupRefreshTokens(t, ctx, store)

		now := time.Now()

		// Create old revoked token
		oldRevokedTime := now.Add(-8 * 24 * time.Hour)
		oldToken := RefreshToken{
			TokenID:   uuid.New(),
			UserID:    userID,
			TokenHash: []byte("oldrevoked"),
			IssuedAt:  now,
			ExpiresAt: now.Add(7 * 24 * time.Hour),
			RevokedAt: &oldRevokedTime,
		}
		err := store.InsertRefreshToken(ctx, oldToken)
		require.NoError(t, err)

		// Create recently revoked token
		recentRevokedTime := now.Add(-1 * time.Hour)
		recentToken := RefreshToken{
			TokenID:   uuid.New(),
			UserID:    userID,
			TokenHash: []byte("recentrevoked"),
			IssuedAt:  now,
			ExpiresAt: now.Add(7 * 24 * time.Hour),
			RevokedAt: &recentRevokedTime,
		}
		err = store.InsertRefreshToken(ctx, recentToken)
		require.NoError(t, err)

		// Delete tokens revoked before 2 hours ago
		cutoff := now.Add(-2 * time.Hour)
		count, err := store.DeleteExpiredRefreshTokens(ctx, cutoff)
		require.NoError(t, err)
		require.Equal(t, int64(1), count)

		// Recent token should still exist
		retrieved, err := store.GetRefreshToken(ctx, recentToken.TokenID)
		require.NoError(t, err)
		require.NotNil(t, retrieved)

		// Old token should be gone
		_, err = store.GetRefreshToken(ctx, oldToken.TokenID)
		require.ErrorIs(t, err, ErrRefreshTokenNotFound)
	})
}
