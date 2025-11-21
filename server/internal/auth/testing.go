package auth

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MockStore implements Store interface for unit tests.
type MockStore struct {
	mu            sync.RWMutex
	users         map[uuid.UUID]*User
	usersByName   map[string]*User
	refreshTokens map[uuid.UUID]*RefreshToken
	err           error // For simulating errors
}

// NewMockStore creates a new mock store for testing.
func NewMockStore() *MockStore {
	return &MockStore{
		users:         make(map[uuid.UUID]*User),
		usersByName:   make(map[string]*User),
		refreshTokens: make(map[uuid.UUID]*RefreshToken),
	}
}

// SetError configures the mock to return an error on next operation.
func (m *MockStore) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

// AddUser adds a user to the mock store (test helper).
func (m *MockStore) AddUser(user *User) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[user.ID] = user
	m.usersByName[user.Username] = user
}

// AddRefreshToken adds a refresh token to the mock store (test helper).
func (m *MockStore) AddRefreshToken(token *RefreshToken) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshTokens[token.TokenID] = token
}

// GetUser retrieves a user by ID.
func (m *MockStore) GetUser(ctx context.Context, id uuid.UUID) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.err != nil {
		err := m.err
		m.err = nil
		return nil, err
	}
	user, ok := m.users[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	return user, nil
}

// GetUserByUsername retrieves a user by username.
func (m *MockStore) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.err != nil {
		err := m.err
		m.err = nil
		return nil, err
	}
	user, ok := m.usersByName[username]
	if !ok {
		return nil, ErrUserNotFound
	}
	return user, nil
}

// ListUsers returns all users.
func (m *MockStore) ListUsers(ctx context.Context) ([]User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.err != nil {
		err := m.err
		m.err = nil
		return nil, err
	}
	users := make([]User, 0, len(m.users))
	for _, user := range m.users {
		users = append(users, *user)
	}
	return users, nil
}

// CreateUser creates a new user.
func (m *MockStore) CreateUser(ctx context.Context, user User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		err := m.err
		m.err = nil
		return err
	}
	// Check for duplicate username
	if _, exists := m.usersByName[user.Username]; exists {
		return ErrDuplicateUsername
	}
	m.users[user.ID] = &user
	m.usersByName[user.Username] = &user
	return nil
}

// UpdateUserPassword updates a user's password hash.
func (m *MockStore) UpdateUserPassword(ctx context.Context, id uuid.UUID, passwordHash []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		err := m.err
		m.err = nil
		return err
	}
	user, ok := m.users[id]
	if !ok {
		return ErrUserNotFound
	}
	user.PasswordHash = passwordHash
	user.UpdatedAt = time.Now()
	return nil
}

// SetUserActive sets a user's active status.
func (m *MockStore) SetUserActive(ctx context.Context, id uuid.UUID, active bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		err := m.err
		m.err = nil
		return err
	}
	user, ok := m.users[id]
	if !ok {
		return ErrUserNotFound
	}
	user.IsActive = active
	user.UpdatedAt = time.Now()
	return nil
}

// UpdateLastLogin updates a user's last login timestamp.
func (m *MockStore) UpdateLastLogin(ctx context.Context, id uuid.UUID, ts time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		err := m.err
		m.err = nil
		return err
	}
	user, ok := m.users[id]
	if !ok {
		return ErrUserNotFound
	}
	user.LastLoginAt = &ts
	user.UpdatedAt = time.Now()
	return nil
}

// InsertRefreshToken inserts a new refresh token.
func (m *MockStore) InsertRefreshToken(ctx context.Context, token RefreshToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		err := m.err
		m.err = nil
		return err
	}
	m.refreshTokens[token.TokenID] = &token
	return nil
}

// GetRefreshToken retrieves a refresh token by token ID.
func (m *MockStore) GetRefreshToken(ctx context.Context, tokenID uuid.UUID) (*RefreshToken, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.err != nil {
		err := m.err
		m.err = nil
		return nil, err
	}
	token, ok := m.refreshTokens[tokenID]
	if !ok {
		return nil, ErrRefreshTokenNotFound
	}
	return token, nil
}

// RevokeRefreshToken revokes a single refresh token.
func (m *MockStore) RevokeRefreshToken(ctx context.Context, tokenID uuid.UUID, revokedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		err := m.err
		m.err = nil
		return err
	}
	token, ok := m.refreshTokens[tokenID]
	if !ok {
		return ErrRefreshTokenNotFound
	}
	token.RevokedAt = &revokedAt
	return nil
}

// RevokeRefreshTokensByUser revokes all refresh tokens for a user.
func (m *MockStore) RevokeRefreshTokensByUser(ctx context.Context, userID uuid.UUID, revokedAt time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		err := m.err
		m.err = nil
		return 0, err
	}
	count := int64(0)
	for _, token := range m.refreshTokens {
		if token.UserID == userID && token.RevokedAt == nil {
			token.RevokedAt = &revokedAt
			count++
		}
	}
	return count, nil
}

// DeleteExpiredRefreshTokens deletes expired refresh tokens.
func (m *MockStore) DeleteExpiredRefreshTokens(ctx context.Context, cutoff time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		err := m.err
		m.err = nil
		return 0, err
	}
	count := int64(0)
	for tokenID, token := range m.refreshTokens {
		if token.ExpiresAt.Before(cutoff) {
			delete(m.refreshTokens, tokenID)
			count++
		}
	}
	return count, nil
}
