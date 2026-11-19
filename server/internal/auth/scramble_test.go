package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

func TestScramblePasswords(t *testing.T) {
	ctx := context.Background()
	store := NewMockStore()
	oldHash, _ := bcrypt.GenerateFromPassword([]byte("old-local-password"), bcrypt.MinCost)
	alice := &User{ID: uuid.New(), Username: "mmattox", PasswordHash: oldHash, IsActive: true}
	bob := &User{ID: uuid.New(), Username: "testuser", PasswordHash: oldHash, IsActive: true}
	sso := &User{ID: uuid.New(), Username: "ssouser", PasswordHash: []byte("!sso-only:cloudflare-access"), IsActive: true}
	for _, u := range []*User{alice, bob, sso} {
		store.AddUser(u)
	}
	live := &RefreshToken{TokenID: uuid.New(), UserID: alice.ID, ExpiresAt: time.Now().Add(time.Hour)}
	store.AddRefreshToken(live)

	now := time.Now()
	res, err := ScramblePasswords(ctx, store, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Users != 3 || res.RevokedRefresh != 1 {
		t.Fatalf("result = %+v", res)
	}
	seen := map[string]bool{}
	for _, u := range []*User{alice, bob, sso} {
		got, err := store.GetUser(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Username != u.Username || !got.IsActive {
			t.Fatalf("account identity changed: %+v", got)
		}
		if bcrypt.CompareHashAndPassword(got.PasswordHash, []byte("old-local-password")) == nil {
			t.Fatalf("%s: old password still works", u.Username)
		}
		if _, err := bcrypt.Cost(got.PasswordHash); err != nil {
			t.Fatalf("%s: not a bcrypt hash: %v", u.Username, err)
		}
		if seen[string(got.PasswordHash)] {
			t.Fatal("two accounts got the same hash: randomness is not per account")
		}
		seen[string(got.PasswordHash)] = true
	}
	tok, err := store.GetRefreshToken(ctx, live.TokenID)
	if err != nil {
		t.Fatal(err)
	}
	if tok.RevokedAt == nil {
		t.Fatal("refresh token must be revoked")
	}
}

func TestUnusablePasswordHashUsesTheRandomBytes(t *testing.T) {
	secret := bytes.Repeat([]byte{7}, 64)
	h, err := UnusablePasswordHash(bytes.NewReader(append([]byte(nil), secret...)))
	if err != nil {
		t.Fatal(err)
	}
	// Proves the hash is of the 64 random bytes (and that only they would match).
	if bcrypt.CompareHashAndPassword(h, secret) != nil {
		t.Fatal("hash is not bcrypt(random bytes)")
	}
	if bcrypt.CompareHashAndPassword(h, []byte("")) == nil {
		t.Fatal("empty password matches")
	}
	if _, err := UnusablePasswordHash(bytes.NewReader(make([]byte, 10))); err == nil {
		t.Fatal("short randomness must fail, never hash a short secret")
	}
	if _, err := UnusablePasswordHash(rand.Reader); err != nil {
		t.Fatal(err)
	}
}

func TestScramblePasswordsStopsOnStoreError(t *testing.T) {
	store := NewMockStore()
	store.SetError(errors.New("db down"))
	if _, err := ScramblePasswords(context.Background(), store, nil, time.Now()); err == nil {
		t.Fatal("expected error")
	}
}
