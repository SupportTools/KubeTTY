// Package cfaccess verifies Cloudflare Access application tokens.
//
// Cloudflare Access puts a signed JWT in the Cf-Access-Jwt-Assertion request
// header of every request it lets through to the origin. KubeTTY trusts that
// identity only after verifying the token itself (cluster-services-1299):
//
//   - RS256 signature against the team's published keys
//     (https://<team>.cloudflareaccess.com/cdn-cgi/access/certs), cached and
//     refetched on rotation (unknown kid) or when the cache is older than the
//     refresh interval;
//   - iss equals the team domain;
//   - aud contains the Access application's AUD tag for this environment;
//   - exp is present and in the future, nbf/iat are not in the future;
//   - an email claim is present, OR the token is an Access service-token token
//     (type "app", no email, a common_name = the token's client ID). Service
//     tokens come back with ServiceToken set and an empty Email; the caller
//     decides whether that client ID is allowed (cluster-services-1534).
//
// The Cf-Access-Authenticated-User-Email header is never trusted: it is not
// signed and any client that reaches the origin directly can set it.
package cfaccess

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// HeaderName is the request header Cloudflare Access adds for the origin.
const HeaderName = "Cf-Access-Jwt-Assertion"

// Errors returned by Verify. Callers map all of them to 401.
var (
	ErrMissingToken = errors.New("cloudflare access token missing")
	ErrInvalidToken = errors.New("cloudflare access token invalid")
	ErrNoEmail      = errors.New("cloudflare access token has no email claim")
	ErrKeysUnusable = errors.New("cloudflare access signing keys unavailable")
)

// Claims are the verified claims KubeTTY uses.
type Claims struct {
	Email     string
	Subject   string
	ExpiresAt time.Time
	// ServiceToken is true for a token Cloudflare issued to an Access service
	// token (non_identity policy). Email is then empty and CommonName holds the
	// service token's client ID ("<hex>.access").
	ServiceToken bool
	CommonName   string
}

// Config configures a Verifier.
type Config struct {
	// TeamDomain is the Access team domain, e.g. https://supporttools.cloudflareaccess.com.
	// It is the expected iss claim and the base of the certs URL.
	TeamDomain string
	// Audiences are the accepted application AUD tags (normally exactly one).
	Audiences []string
	// HTTPClient fetches the certs. Defaults to a client with a 10s timeout.
	HTTPClient *http.Client
	// RefreshInterval is the maximum age of the cached keys (default 1h).
	RefreshInterval time.Duration
	// MinRefetchInterval rate-limits refetches triggered by an unknown kid (default 30s).
	MinRefetchInterval time.Duration
	// Leeway tolerates clock skew on exp/nbf/iat (default 30s).
	Leeway time.Duration
	// Now overrides the clock (tests).
	Now func() time.Time
}

// Verifier verifies Cloudflare Access JWTs. It is safe for concurrent use.
type Verifier struct {
	issuer   string
	certsURL string
	auds     []string
	client   *http.Client
	refresh  time.Duration
	minRefet time.Duration
	leeway   time.Duration
	now      func() time.Time

	mu          sync.RWMutex
	keys        map[string]*rsa.PublicKey
	fetchedAt   time.Time
	lastAttempt time.Time
	fetchMu     sync.Mutex
}

// New validates cfg and returns a Verifier. It does not fetch keys; call
// Prefetch at startup to warm the cache.
func New(cfg Config) (*Verifier, error) {
	team := strings.TrimRight(strings.TrimSpace(cfg.TeamDomain), "/")
	u, err := url.Parse(team)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" {
		return nil, fmt.Errorf("cfaccess: team domain must be https://<team>.cloudflareaccess.com, got %q", cfg.TeamDomain)
	}
	var auds []string
	for _, a := range cfg.Audiences {
		if a = strings.TrimSpace(a); a != "" {
			auds = append(auds, a)
		}
	}
	if len(auds) == 0 {
		return nil, errors.New("cfaccess: at least one application AUD is required")
	}
	v := &Verifier{
		issuer:   team,
		certsURL: team + "/cdn-cgi/access/certs",
		auds:     auds,
		client:   cfg.HTTPClient,
		refresh:  cfg.RefreshInterval,
		minRefet: cfg.MinRefetchInterval,
		leeway:   cfg.Leeway,
		now:      cfg.Now,
		keys:     map[string]*rsa.PublicKey{},
	}
	if v.client == nil {
		v.client = &http.Client{Timeout: 10 * time.Second}
	}
	if v.refresh <= 0 {
		v.refresh = time.Hour
	}
	if v.minRefet <= 0 {
		v.minRefet = 30 * time.Second
	}
	if v.leeway <= 0 {
		v.leeway = 30 * time.Second
	}
	if v.now == nil {
		v.now = time.Now
	}
	return v, nil
}

// CertsURL returns the URL the keys are fetched from.
func (v *Verifier) CertsURL() string { return v.certsURL }

// Prefetch loads the signing keys. Errors are returned for logging; Verify
// retries the fetch on demand.
func (v *Verifier) Prefetch(ctx context.Context) error {
	return v.fetch(ctx, true)
}

// VerifyRequest verifies the token in the Cf-Access-Jwt-Assertion header.
func (v *Verifier) VerifyRequest(r *http.Request) (*Claims, error) {
	return v.Verify(r.Context(), r.Header.Get(HeaderName))
}

// Verify checks a raw token and returns its claims.
func (v *Verifier) Verify(ctx context.Context, raw string) (*Claims, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrMissingToken
	}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(v.leeway),
		jwt.WithTimeFunc(v.now),
	)
	claims := jwt.MapClaims{}
	_, err := parser.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("token has no kid")
		}
		return v.key(ctx, kid)
	})
	if err != nil {
		if errors.Is(err, ErrKeysUnusable) {
			return nil, fmt.Errorf("%w: %v", ErrKeysUnusable, err)
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	aud, err := claims.GetAudience()
	if err != nil || !v.audienceOK(aud) {
		return nil, fmt.Errorf("%w: audience not accepted", ErrInvalidToken)
	}
	email, _ := claims["email"].(string)
	email = strings.ToLower(strings.TrimSpace(email))
	sub, _ := claims.GetSubject()
	exp, _ := claims.GetExpirationTime()
	out := &Claims{Email: email, Subject: sub}
	if email == "" {
		// Service-token tokens: type "app", no email, no subject, and the
		// client ID in common_name. Anything else without an email is refused.
		typ, _ := claims["type"].(string)
		cn, _ := claims["common_name"].(string)
		cn = strings.TrimSpace(cn)
		if typ != "app" || sub != "" || cn == "" {
			return nil, ErrNoEmail
		}
		out.ServiceToken = true
		out.CommonName = cn
	}
	if exp != nil {
		out.ExpiresAt = exp.Time
	}
	return out, nil
}

func (v *Verifier) audienceOK(got []string) bool {
	for _, g := range got {
		for _, want := range v.auds {
			if g == want {
				return true
			}
		}
	}
	return false
}

// key returns the public key for kid, refetching on a miss or stale cache.
func (v *Verifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	k, ok := v.keys[kid]
	stale := v.now().Sub(v.fetchedAt) > v.refresh
	v.mu.RUnlock()
	if ok && !stale {
		return k, nil
	}
	// Miss or stale: refetch (rate-limited). A stale cache still serves a
	// known kid if the refetch fails, so a short Cloudflare blip does not
	// log everyone out.
	ferr := v.fetch(ctx, false)
	v.mu.RLock()
	k, ok = v.keys[kid]
	v.mu.RUnlock()
	if ok {
		return k, nil
	}
	if ferr != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeysUnusable, ferr)
	}
	return nil, fmt.Errorf("unknown kid %q", kid)
}

type jwks struct {
	Keys []struct {
		Kid string `json:"kid"`
		Kty string `json:"kty"`
		Alg string `json:"alg"`
		Use string `json:"use"`
		N   string `json:"n"`
		E   string `json:"e"`
	} `json:"keys"`
}

func (v *Verifier) fetch(ctx context.Context, force bool) error {
	v.fetchMu.Lock()
	defer v.fetchMu.Unlock()
	now := v.now()
	if !force && now.Sub(v.lastAttempt) < v.minRefet {
		return nil // another caller fetched (or tried) moments ago
	}
	v.lastAttempt = now

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.certsURL, nil)
	if err != nil {
		return err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", v.certsURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch %s: HTTP %d", v.certsURL, resp.StatusCode)
	}
	var set jwks
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&set); err != nil {
		return fmt.Errorf("decode %s: %w", v.certsURL, err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" || k.Kid == "" || (k.Use != "" && k.Use != "sig") || (k.Alg != "" && k.Alg != "RS256") {
			continue
		}
		pub, err := rsaKey(k.N, k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return fmt.Errorf("%s returned no usable RS256 keys", v.certsURL)
	}
	v.mu.Lock()
	v.keys = keys
	v.fetchedAt = now
	v.mu.Unlock()
	return nil
}

func rsaKey(n64, e64 string) (*rsa.PublicKey, error) {
	nb, err := base64.RawURLEncoding.DecodeString(n64)
	if err != nil {
		return nil, err
	}
	eb, err := base64.RawURLEncoding.DecodeString(e64)
	if err != nil {
		return nil, err
	}
	e := new(big.Int).SetBytes(eb)
	if !e.IsInt64() || e.Int64() < 3 || e.Int64() > 1<<31 {
		return nil, errors.New("bad exponent")
	}
	n := new(big.Int).SetBytes(nb)
	if n.BitLen() < 2048 {
		return nil, errors.New("modulus too small")
	}
	return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
}
