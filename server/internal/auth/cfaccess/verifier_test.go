package cfaccess

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testAUD = "aud-kubetty-dev"

// jwksServer serves a mutable key set the way Cloudflare's certs endpoint does.
type jwksServer struct {
	*httptest.Server
	mu    sync.Mutex
	keys  map[string]*rsa.PublicKey
	hits  atomic.Int32
	issue string
}

func newJWKSServer(t *testing.T) *jwksServer {
	t.Helper()
	s := &jwksServer{keys: map[string]*rsa.PublicKey{}}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cdn-cgi/access/certs" {
			http.NotFound(w, r)
			return
		}
		s.hits.Add(1)
		s.mu.Lock()
		defer s.mu.Unlock()
		out := struct {
			Keys []map[string]string `json:"keys"`
		}{}
		for kid, pub := range s.keys {
			out.Keys = append(out.Keys, map[string]string{
				"kid": kid, "kty": "RSA", "alg": "RS256", "use": "sig",
				"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(s.Close)
	s.issue = s.URL
	return s
}

func (s *jwksServer) setKeys(keys map[string]*rsa.PrivateKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = map[string]*rsa.PublicKey{}
	for kid, k := range keys {
		s.keys[kid] = &k.PublicKey
	}
}

func genKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func sign(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func goodClaims(iss string, now time.Time) jwt.MapClaims {
	return jwt.MapClaims{
		"iss":   iss,
		"aud":   []string{testAUD},
		"email": "MMattox@MattoxEngineering.com",
		"sub":   "user-uuid",
		"type":  "app",
		"iat":   now.Unix(),
		"nbf":   now.Unix(),
		"exp":   now.Add(8 * time.Hour).Unix(),
	}
}

func newVerifier(t *testing.T, s *jwksServer, now func() time.Time) *Verifier {
	t.Helper()
	v, err := New(Config{
		TeamDomain: s.URL,
		Audiences:  []string{testAUD},
		HTTPClient: s.Client(),
		Now:        now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestVerify(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	s := newJWKSServer(t)
	k1 := genKey(t)
	other := genKey(t)
	s.setKeys(map[string]*rsa.PrivateKey{"k1": k1})
	v := newVerifier(t, s, clock)

	mod := func(f func(jwt.MapClaims)) jwt.MapClaims {
		c := goodClaims(s.URL, now)
		f(c)
		return c
	}

	cases := []struct {
		name    string
		token   string
		wantErr error
	}{
		{"valid", sign(t, k1, "k1", goodClaims(s.URL, now)), nil},
		{"missing", "", ErrMissingToken},
		{"garbage", "not.a.jwt", ErrInvalidToken},
		{"bad signature (other key, same kid)", sign(t, other, "k1", goodClaims(s.URL, now)), ErrInvalidToken},
		{"tampered payload", tamper(t, sign(t, k1, "k1", goodClaims(s.URL, now))), ErrInvalidToken},
		{"wrong aud", sign(t, k1, "k1", mod(func(c jwt.MapClaims) { c["aud"] = []string{"aud-kubetty-prd"} })), ErrInvalidToken},
		{"aud string form", sign(t, k1, "k1", mod(func(c jwt.MapClaims) { c["aud"] = testAUD })), nil},
		{"no aud", sign(t, k1, "k1", mod(func(c jwt.MapClaims) { delete(c, "aud") })), ErrInvalidToken},
		{"expired", sign(t, k1, "k1", mod(func(c jwt.MapClaims) { c["exp"] = now.Add(-time.Minute).Unix() })), ErrInvalidToken},
		{"no exp", sign(t, k1, "k1", mod(func(c jwt.MapClaims) { delete(c, "exp") })), ErrInvalidToken},
		{"nbf in future", sign(t, k1, "k1", mod(func(c jwt.MapClaims) { c["nbf"] = now.Add(10 * time.Minute).Unix() })), ErrInvalidToken},
		{"iat in future", sign(t, k1, "k1", mod(func(c jwt.MapClaims) { c["iat"] = now.Add(10 * time.Minute).Unix() })), ErrInvalidToken},
		{"wrong issuer", sign(t, k1, "k1", mod(func(c jwt.MapClaims) { c["iss"] = "https://evil.cloudflareaccess.com" })), ErrInvalidToken},
		{"no email (service token)", sign(t, k1, "k1", mod(func(c jwt.MapClaims) { delete(c, "email"); c["common_name"] = "svc" })), ErrNoEmail},
		{"unknown kid", sign(t, k1, "nope", goodClaims(s.URL, now)), ErrInvalidToken},
		{"alg none", noneToken(t, goodClaims(s.URL, now)), ErrInvalidToken},
		{"alg HS256 with public key as secret", hsToken(t, k1, goodClaims(s.URL, now)), ErrInvalidToken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := v.Verify(context.Background(), tc.token)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if c.Email != "mmattox@mattoxengineering.com" {
					t.Fatalf("email not normalised: %q", c.Email)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v (claims %+v)", tc.wantErr, err, c)
			}
		})
	}
}

func TestExpiryWithinLeewayAccepted(t *testing.T) {
	now := time.Now()
	s := newJWKSServer(t)
	k1 := genKey(t)
	s.setKeys(map[string]*rsa.PrivateKey{"k1": k1})
	v := newVerifier(t, s, func() time.Time { return now })
	c := goodClaims(s.URL, now)
	c["exp"] = now.Add(-10 * time.Second).Unix()
	if _, err := v.Verify(context.Background(), sign(t, k1, "k1", c)); err != nil {
		t.Fatalf("10s past exp should be inside the 30s leeway: %v", err)
	}
}

func TestKeyRotationRefetchesOnUnknownKid(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	s := newJWKSServer(t)
	k1, k2 := genKey(t), genKey(t)
	s.setKeys(map[string]*rsa.PrivateKey{"k1": k1})
	v := newVerifier(t, s, clock)
	if err := v.Prefetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Cloudflare rotates: k2 appears. A token signed with k2 arrives.
	s.setKeys(map[string]*rsa.PrivateKey{"k1": k1, "k2": k2})
	now = now.Add(time.Minute) // past the refetch rate limit
	if _, err := v.Verify(context.Background(), sign(t, k2, "k2", goodClaims(s.URL, now))); err != nil {
		t.Fatalf("rotated key not picked up: %v", err)
	}
	if got := s.hits.Load(); got != 2 {
		t.Fatalf("want 2 cert fetches (prefetch + rotation), got %d", got)
	}
	// Cached: more verifications do not refetch.
	for i := 0; i < 5; i++ {
		if _, err := v.Verify(context.Background(), sign(t, k1, "k1", goodClaims(s.URL, now))); err != nil {
			t.Fatal(err)
		}
	}
	if got := s.hits.Load(); got != 2 {
		t.Fatalf("cache not used: %d fetches", got)
	}
}

func TestUnknownKidRefetchIsRateLimited(t *testing.T) {
	now := time.Now()
	s := newJWKSServer(t)
	k1 := genKey(t)
	s.setKeys(map[string]*rsa.PrivateKey{"k1": k1})
	v := newVerifier(t, s, func() time.Time { return now })
	if err := v.Prefetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		_, _ = v.Verify(context.Background(), sign(t, k1, "bogus", goodClaims(s.URL, now)))
	}
	if got := s.hits.Load(); got != 1 {
		t.Fatalf("unknown-kid floods must not hammer the certs endpoint: %d fetches", got)
	}
}

func TestStaleCacheRefreshesAndSurvivesOutage(t *testing.T) {
	now := time.Now()
	s := newJWKSServer(t)
	k1 := genKey(t)
	s.setKeys(map[string]*rsa.PrivateKey{"k1": k1})
	v := newVerifier(t, s, func() time.Time { return now })
	if err := v.Prefetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.Close() // Cloudflare certs endpoint unreachable
	now = now.Add(2 * time.Hour)
	if _, err := v.Verify(context.Background(), sign(t, k1, "k1", goodClaims(s.issue, now))); err != nil {
		t.Fatalf("known kid must still verify from a stale cache during an outage: %v", err)
	}
}

func TestKeysUnavailable(t *testing.T) {
	s := newJWKSServer(t)
	k1 := genKey(t)
	v := newVerifier(t, s, time.Now)
	s.Close()
	_, err := v.Verify(context.Background(), sign(t, k1, "k1", goodClaims(s.issue, time.Now())))
	if !errors.Is(err, ErrKeysUnusable) {
		t.Fatalf("want ErrKeysUnusable, got %v", err)
	}
}

func TestNewValidation(t *testing.T) {
	bad := []Config{
		{TeamDomain: "", Audiences: []string{"a"}},
		{TeamDomain: "http://x.cloudflareaccess.com", Audiences: []string{"a"}},
		{TeamDomain: "https://x.cloudflareaccess.com/path", Audiences: []string{"a"}},
		{TeamDomain: "https://x.cloudflareaccess.com", Audiences: nil},
		{TeamDomain: "https://x.cloudflareaccess.com", Audiences: []string{" "}},
	}
	for i, c := range bad {
		if _, err := New(c); err == nil {
			t.Errorf("case %d: expected error for %+v", i, c)
		}
	}
	v, err := New(Config{TeamDomain: "https://supporttools.cloudflareaccess.com/", Audiences: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	if v.CertsURL() != "https://supporttools.cloudflareaccess.com/cdn-cgi/access/certs" {
		t.Fatalf("certs url %q", v.CertsURL())
	}
}

func TestVerifyRequestReadsOnlyTheAssertionHeader(t *testing.T) {
	now := time.Now()
	s := newJWKSServer(t)
	k1 := genKey(t)
	s.setKeys(map[string]*rsa.PrivateKey{"k1": k1})
	v := newVerifier(t, s, func() time.Time { return now })
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Cf-Access-Authenticated-User-Email", "mmattox@mattoxengineering.com")
	if _, err := v.VerifyRequest(r); !errors.Is(err, ErrMissingToken) {
		t.Fatalf("the unsigned email header must not authenticate: %v", err)
	}
	r.Header.Set(HeaderName, sign(t, k1, "k1", goodClaims(s.URL, now)))
	if _, err := v.VerifyRequest(r); err != nil {
		t.Fatal(err)
	}
}

func tamper(t *testing.T, tok string) string {
	t.Helper()
	parts := strings.Split(tok, ".")
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	changed := strings.Replace(string(payload), "MattoxEngineering.com", "evil.example.com", 1)
	if changed == string(payload) {
		t.Fatal("tamper did not change the payload")
	}
	payload = []byte(changed)
	parts[1] = base64.RawURLEncoding.EncodeToString(payload)
	return strings.Join(parts, ".")
}

func noneToken(t *testing.T, c jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, c)
	tok.Header["kid"] = "k1"
	s, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func hsToken(t *testing.T, k *rsa.PrivateKey, c jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	tok.Header["kid"] = "k1"
	s, err := tok.SignedString(k.PublicKey.N.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return s
}
