package auth

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDefaultAdmissionIsAdmin(t *testing.T) {
	store := NewMockStore()
	user := &User{ID: uuid.New(), Username: "alice", IsActive: true}
	store.AddUser(user)
	mgr := newTestManager(t, store)

	pair, err := mgr.IssueTokenPair(context.Background(), user, TokenMetadata{})
	require.NoError(t, err)
	claims, err := mgr.ValidateAccessToken(pair.AccessToken)
	require.NoError(t, err)
	require.Equal(t, RoleAdmin, claims.Role)
}

func TestAdmissionSetsRoleAndGitHubLogin(t *testing.T) {
	store := NewMockStore()
	user := &User{ID: uuid.New(), Username: "octo", IsActive: true, GitHubID: 42, GitHubLogin: "octo"}
	store.AddUser(user)
	mgr := newTestManager(t, store)
	mgr.SetAdmission(func(context.Context, *User) (string, error) { return RoleUser, nil })

	pair, err := mgr.IssueTokenPair(context.Background(), user, TokenMetadata{})
	require.NoError(t, err)
	claims, err := mgr.ValidateAccessToken(pair.AccessToken)
	require.NoError(t, err)
	require.Equal(t, RoleUser, claims.Role)
	require.Equal(t, "octo", claims.GitHubLogin)
}

func TestAdmissionRefusesIssue(t *testing.T) {
	store := NewMockStore()
	user := &User{ID: uuid.New(), Username: "old-local", IsActive: true}
	store.AddUser(user)
	mgr := newTestManager(t, store)
	mgr.SetAdmission(func(context.Context, *User) (string, error) { return "", ErrNotAdmitted })

	_, err := mgr.IssueTokenPair(context.Background(), user, TokenMetadata{})
	require.ErrorIs(t, err, ErrNotAdmitted)
}

func TestRefreshRechecksAdmissionAndRevokes(t *testing.T) {
	store := NewMockStore()
	user := &User{ID: uuid.New(), Username: "octo", IsActive: true, GitHubID: 42, GitHubLogin: "octo"}
	store.AddUser(user)
	mgr := newTestManager(t, store)
	role := RoleAdmin
	admitted := true
	mgr.SetAdmission(func(context.Context, *User) (string, error) {
		if !admitted {
			return "", ErrNotAdmitted
		}
		return role, nil
	})
	ctx := context.Background()

	pair, err := mgr.IssueTokenPair(ctx, user, TokenMetadata{})
	require.NoError(t, err)

	role = RoleUser
	refreshed, err := mgr.Refresh(ctx, pair.RefreshToken, TokenMetadata{})
	require.NoError(t, err)
	claims, err := mgr.ValidateAccessToken(refreshed.AccessToken)
	require.NoError(t, err)
	require.Equal(t, RoleUser, claims.Role, "refresh must pick up a role change")

	admitted = false
	_, err = mgr.Refresh(ctx, pair.RefreshToken, TokenMetadata{})
	require.ErrorIs(t, err, ErrNotAdmitted)

	admitted = true
	_, err = mgr.Refresh(ctx, pair.RefreshToken, TokenMetadata{})
	require.ErrorIs(t, err, ErrTokenRevoked, "a refused refresh must revoke the refresh token")
}
