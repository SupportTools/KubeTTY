package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/supporttools/KubeTTY/server/internal/auth"
	"github.com/supporttools/KubeTTY/server/internal/auth/cfaccess"
)

const accessTestAUD = "aud-test"

type accessFixture struct {
	key      *rsa.PrivateKey
	issuer   string
	verifier *cfaccess.Verifier
	store    *auth.MockStore
	mapper   *AccessUserMapper
	existing *auth.User
}

func newAccessFixture(t *testing.T) *accessFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kid": "k1", "kty": "RSA", "alg": "RS256", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	t.Cleanup(srv.Close)
	v, err := cfaccess.New(cfaccess.Config{TeamDomain: srv.URL, Audiences: []string{accessTestAUD}, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	store := auth.NewMockStore()
	hash, _ := bcrypt.GenerateFromPassword([]byte("old-local-password"), bcrypt.MinCost)
	existing := &auth.User{ID: uuid.New(), Username: "mmattox", PasswordHash: hash, IsActive: true}
	store.AddUser(existing)
	store.AddUser(&auth.User{ID: uuid.New(), Username: "disabled", PasswordHash: hash, IsActive: false})
	users, err := ParseAccessUsers("MMattox@MattoxEngineering.com=mmattox, new@example.com=newuser, off@example.com=disabled")
	if err != nil {
		t.Fatal(err)
	}
	return &accessFixture{key: key, issuer: srv.URL, verifier: v, store: store, mapper: NewAccessUserMapper(store, users), existing: existing}
}

func (f *accessFixture) token(t *testing.T, email, aud string) string {
	t.Helper()
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": f.issuer, "aud": []string{aud}, "email": email, "sub": "s",
		"iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = "k1"
	s, err := tok.SignedString(f.key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (f *accessFixture) serve(r *http.Request) (*httptest.ResponseRecorder, *User) {
	var got *User
	h := RequireAccess(f.verifier, f.mapper)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec, got
}

func TestRequireAccess(t *testing.T) {
	f := newAccessFixture(t)

	t.Run("no token is 401", func(t *testing.T) {
		rec, u := f.serve(httptest.NewRequest(http.MethodGet, "/api/tabs", nil))
		if rec.Code != http.StatusUnauthorized || u != nil {
			t.Fatalf("got %d user=%v", rec.Code, u)
		}
		if !strings.Contains(rec.Body.String(), "cloudflare-access") {
			t.Fatalf("body should name the auth mode: %s", rec.Body.String())
		}
	})

	t.Run("unsigned email header alone is 401", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/tabs", nil)
		r.Header.Set("Cf-Access-Authenticated-User-Email", "mmattox@mattoxengineering.com")
		if rec, u := f.serve(r); rec.Code != http.StatusUnauthorized || u != nil {
			t.Fatalf("got %d user=%v", rec.Code, u)
		}
	})

	t.Run("wrong aud is 401", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/tabs", nil)
		r.Header.Set(cfaccess.HeaderName, f.token(t, "mmattox@mattoxengineering.com", "aud-other-app"))
		if rec, u := f.serve(r); rec.Code != http.StatusUnauthorized || u != nil {
			t.Fatalf("got %d user=%v", rec.Code, u)
		}
	})

	t.Run("valid token maps to the existing account (keeps its tabs)", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/tabs", nil)
		r.Header.Set(cfaccess.HeaderName, f.token(t, "mmattox@mattoxengineering.com", accessTestAUD))
		rec, u := f.serve(r)
		if rec.Code != http.StatusOK || u == nil || u.ID != f.existing.ID || u.Username != "mmattox" {
			t.Fatalf("got %d user=%+v", rec.Code, u)
		}
	})

	t.Run("allowlisted email without an account gets an SSO-only account", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/tabs", nil)
		r.Header.Set(cfaccess.HeaderName, f.token(t, "New@Example.com", accessTestAUD))
		rec, u := f.serve(r)
		if rec.Code != http.StatusOK || u == nil || u.Username != "newuser" {
			t.Fatalf("got %d user=%+v", rec.Code, u)
		}
		created, err := f.store.GetUserByUsername(r.Context(), "newuser")
		if err != nil {
			t.Fatal(err)
		}
		if bcrypt.CompareHashAndPassword(created.PasswordHash, []byte("")) == nil {
			t.Fatal("SSO account must not accept any password")
		}
	})

	t.Run("valid token for an email not on the allowlist is 403", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/tabs", nil)
		r.Header.Set(cfaccess.HeaderName, f.token(t, "stranger@mattoxengineering.com", accessTestAUD))
		if rec, u := f.serve(r); rec.Code != http.StatusForbidden || u != nil {
			t.Fatalf("got %d user=%v", rec.Code, u)
		}
		if _, err := f.store.GetUserByUsername(r.Context(), "stranger"); err == nil {
			t.Fatal("no account may be created for a non-allowlisted email")
		}
	})

	t.Run("inactive account is 403", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/tabs", nil)
		r.Header.Set(cfaccess.HeaderName, f.token(t, "off@example.com", accessTestAUD))
		if rec, _ := f.serve(r); rec.Code != http.StatusForbidden {
			t.Fatalf("got %d", rec.Code)
		}
	})
}

func TestRequireAccessWebSocketOrigin(t *testing.T) {
	f := newAccessFixture(t)
	ws := func(origin string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "http://kubetty.support.tools/ws?tab=x", nil)
		r.Host = "kubetty.support.tools"
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		r.Header.Set(cfaccess.HeaderName, f.token(t, "mmattox@mattoxengineering.com", accessTestAUD))
		return r
	}
	if rec, _ := f.serve(ws("https://kubetty.support.tools")); rec.Code != http.StatusOK {
		t.Fatalf("same-origin websocket: %d", rec.Code)
	}
	if rec, _ := f.serve(ws("")); rec.Code != http.StatusOK {
		t.Fatalf("no-Origin (non-browser) websocket: %d", rec.Code)
	}
	if rec, _ := f.serve(ws("https://evil.example.com")); rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin websocket: %d", rec.Code)
	}
	noJWT := ws("https://kubetty.support.tools")
	noJWT.Header.Del(cfaccess.HeaderName)
	if rec, _ := f.serve(noJWT); rec.Code != http.StatusUnauthorized {
		t.Fatalf("websocket without JWT: %d", rec.Code)
	}
}

func TestParseAccessUsers(t *testing.T) {
	m, err := ParseAccessUsers(" A@B.com = alice ,c@d.com=bob")
	if err != nil || m["a@b.com"] != "alice" || m["c@d.com"] != "bob" {
		t.Fatalf("%v %v", m, err)
	}
	for _, bad := range []string{"", " , ", "a@b.com", "=alice", "notanemail=alice", "a@b.com=bad name", "a@b.com=x,A@b.com=y"} {
		if _, err := ParseAccessUsers(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestAccessSessionAndRetiredHandlers(t *testing.T) {
	f := newAccessFixture(t)
	r := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	r.Header.Set(cfaccess.HeaderName, f.token(t, "mmattox@mattoxengineering.com", accessTestAUD))
	rec := httptest.NewRecorder()
	RequireAccess(f.verifier, f.mapper)(NewAccessSessionHandler()).ServeHTTP(rec, r)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"username":"mmattox"`) {
		t.Fatalf("refresh: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	NewPasswordsRetiredHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"mmattox","password":"old-local-password"}`)))
	if rec.Code != http.StatusGone {
		t.Fatalf("login must be retired: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	NewAuthConfigHandler(AuthModeCloudflareAccess).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/config", nil))
	if !strings.Contains(rec.Body.String(), `"authMode":"cloudflare-access"`) || !strings.Contains(rec.Body.String(), AccessLogoutURL) {
		t.Fatalf("config: %s", rec.Body.String())
	}
}
