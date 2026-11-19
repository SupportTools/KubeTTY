package auth

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ScrambleResult reports what ScramblePasswords changed.
type ScrambleResult struct {
	Users          int   // accounts whose password hash was replaced
	RevokedRefresh int64 // refresh tokens revoked
}

// ScramblePasswords retires every local password (cluster-services-1534,
// operator decision 2026-11-19 "Scramble them"). Each account's hash is
// replaced with bcrypt of 64 fresh random bytes that are never stored or
// printed, so no password can match it, and every live refresh token is
// revoked. A rollback of the gateway to AUTH_MODE=local therefore cannot
// reopen password login or resume an old local session. Accounts, their IDs
// and tab ownership are untouched (SSO keeps mapping onto them by username).
//
// rnd is the randomness source (crypto/rand when nil); now stamps revocation.
func ScramblePasswords(ctx context.Context, store Store, rnd io.Reader, now time.Time) (ScrambleResult, error) {
	if rnd == nil {
		rnd = rand.Reader
	}
	var res ScrambleResult
	users, err := store.ListUsers(ctx)
	if err != nil {
		return res, fmt.Errorf("list users: %w", err)
	}
	for _, u := range users {
		hash, err := UnusablePasswordHash(rnd)
		if err != nil {
			return res, fmt.Errorf("scramble %q: %w", u.Username, err)
		}
		if err := store.UpdateUserPassword(ctx, u.ID, hash); err != nil {
			return res, fmt.Errorf("scramble %q: %w", u.Username, err)
		}
		res.Users++
		n, err := store.RevokeRefreshTokensByUser(ctx, u.ID, now)
		if err != nil {
			return res, fmt.Errorf("revoke refresh tokens of %q: %w", u.Username, err)
		}
		res.RevokedRefresh += n
	}
	return res, nil
}

// UnusablePasswordHash returns bcrypt(64 random bytes) and discards the input.
func UnusablePasswordHash(rnd io.Reader) ([]byte, error) {
	secret := make([]byte, 64)
	if _, err := io.ReadFull(rnd, secret); err != nil {
		return nil, fmt.Errorf("read random bytes: %w", err)
	}
	defer func() {
		for i := range secret {
			secret[i] = 0
		}
	}()
	return bcrypt.GenerateFromPassword(secret, bcrypt.DefaultCost)
}
