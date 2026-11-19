package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/supporttools/KubeTTY/server/internal/auth"
	"github.com/supporttools/KubeTTY/server/internal/auth/cfaccess"
	"github.com/supporttools/KubeTTY/server/internal/shared/util"

	log "github.com/sirupsen/logrus"
)

// AuthModeCloudflareAccess is the AUTH_MODE value for Cloudflare Access SSO
// (cluster-services-1299): the gateway trusts the identity in the verified
// Cf-Access-Jwt-Assertion JWT and has no local-password path.
const AuthModeCloudflareAccess = "cloudflare-access"

// AccessLogoutURL is Cloudflare's per-app logout endpoint (same origin).
const AccessLogoutURL = "/cdn-cgi/access/logout"

// ErrAccessUserNotAllowed is returned for a valid Access identity that is not
// mapped to a KubeTTY user.
var ErrAccessUserNotAllowed = errors.New("access identity not allowed")

// ssoPasswordHash marks accounts created for SSO. It is not a bcrypt hash, so
// bcrypt comparison always fails and the account can never log in with a
// password, even if the gateway is rolled back to AUTH_MODE=local.
var ssoPasswordHash = []byte("!sso-only:cloudflare-access")

// ParseAccessUsers parses AUTH_ACCESS_USERS: comma-separated email=username
// pairs. Emails are matched case-insensitively.
func ParseAccessUsers(spec string) (map[string]string, error) {
	out := map[string]string{}
	for _, item := range strings.Split(spec, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		email, username, ok := strings.Cut(item, "=")
		email = strings.ToLower(strings.TrimSpace(email))
		username = strings.TrimSpace(username)
		if !ok || email == "" || !strings.Contains(email, "@") || username == "" {
			return nil, fmt.Errorf("AUTH_ACCESS_USERS entry %q must be email=username", item)
		}
		if len(username) > MaxUsernameLength || !UsernameRegex.MatchString(username) {
			return nil, fmt.Errorf("AUTH_ACCESS_USERS entry %q: invalid username", item)
		}
		if _, dup := out[email]; dup {
			return nil, fmt.Errorf("AUTH_ACCESS_USERS lists %q twice", email)
		}
		out[email] = username
	}
	if len(out) == 0 {
		return nil, errors.New("AUTH_ACCESS_USERS must map at least one email to a username")
	}
	return out, nil
}

// AccessUserMapper maps a verified Access email to a KubeTTY user. Only
// allowlisted emails map; an allowlisted email whose username does not exist
// yet gets an SSO-only account on first login. Existing accounts (with their
// tab ownership) are reused by username.
type AccessUserMapper struct {
	store    auth.Store
	users    map[string]string
	services map[string]ServicePrincipal
	ttl      time.Duration
	now      func() time.Time
	mu       sync.Mutex
	cached   map[string]accessCacheEntry
}

type accessCacheEntry struct {
	user *User
	at   time.Time
}

// NewAccessUserMapper builds a mapper over the auth store.
func NewAccessUserMapper(store auth.Store, users map[string]string) *AccessUserMapper {
	return &AccessUserMapper{store: store, users: users, ttl: time.Minute, now: time.Now, cached: map[string]accessCacheEntry{}}
}

// ServicePrincipal is what an allowlisted Access service token maps to: a
// dedicated KubeTTY user that is always Restricted to one project.
type ServicePrincipal struct {
	Username string
	Project  string
}

// ParseAccessServiceTokens parses AUTH_ACCESS_SERVICE_TOKENS: comma-separated
// <client_id>=<username>:<project> entries (cluster-services-1534). An empty
// spec means no service token is accepted. A username listed in
// AUTH_ACCESS_USERS (a human account) is refused, so a service token can never
// act as a person.
func ParseAccessServiceTokens(spec string, humans map[string]string) (map[string]ServicePrincipal, error) {
	out := map[string]ServicePrincipal{}
	humanNames := map[string]bool{}
	for _, u := range humans {
		humanNames[strings.ToLower(u)] = true
	}
	seenUser := map[string]bool{}
	for _, item := range strings.Split(spec, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		clientID, rest, ok := strings.Cut(item, "=")
		clientID = strings.TrimSpace(clientID)
		username, project, ok2 := strings.Cut(rest, ":")
		username = strings.TrimSpace(username)
		project = strings.TrimSpace(project)
		if !ok || !ok2 || !strings.HasSuffix(clientID, ".access") || len(clientID) <= len(".access") || username == "" || project == "" {
			return nil, fmt.Errorf("AUTH_ACCESS_SERVICE_TOKENS entry %q must be <client_id>.access=<username>:<project>", item)
		}
		if len(username) > MaxUsernameLength || !UsernameRegex.MatchString(username) {
			return nil, fmt.Errorf("AUTH_ACCESS_SERVICE_TOKENS entry %q: invalid username", item)
		}
		if humanNames[strings.ToLower(username)] {
			return nil, fmt.Errorf("AUTH_ACCESS_SERVICE_TOKENS entry %q: %q is a human account in AUTH_ACCESS_USERS", item, username)
		}
		if _, dup := out[clientID]; dup {
			return nil, fmt.Errorf("AUTH_ACCESS_SERVICE_TOKENS lists %q twice", clientID)
		}
		if seenUser[strings.ToLower(username)] {
			return nil, fmt.Errorf("AUTH_ACCESS_SERVICE_TOKENS maps two tokens to %q", username)
		}
		seenUser[strings.ToLower(username)] = true
		out[clientID] = ServicePrincipal{Username: username, Project: project}
	}
	return out, nil
}

// WithServiceTokens enables the given Access service tokens (client ID ->
// principal). Without it every service token is refused (403).
func (m *AccessUserMapper) WithServiceTokens(services map[string]ServicePrincipal) *AccessUserMapper {
	m.services = services
	return m
}

// LookupServiceToken returns the restricted KubeTTY user for an allowlisted
// service token client ID. The account is created on first use with a
// password hash bcrypt can never match.
func (m *AccessUserMapper) LookupServiceToken(ctx context.Context, clientID string) (*User, error) {
	p, ok := m.services[strings.TrimSpace(clientID)]
	if !ok {
		return nil, ErrAccessUserNotAllowed
	}
	u, err := m.loadOrCreate(ctx, "svc:"+clientID, p.Username)
	if err != nil {
		return nil, err
	}
	return &User{ID: u.ID, Username: u.Username, Restricted: true, AllowedProject: p.Project}, nil
}

// Lookup returns the KubeTTY user for email.
func (m *AccessUserMapper) Lookup(ctx context.Context, email string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	username, ok := m.users[email]
	if !ok {
		return nil, ErrAccessUserNotAllowed
	}
	return m.loadOrCreate(ctx, email, username)
}

// loadOrCreate returns the (cached) active account for username, creating an
// SSO-only one if it does not exist. key identifies the identity in the cache.
func (m *AccessUserMapper) loadOrCreate(ctx context.Context, key, username string) (*User, error) {
	m.mu.Lock()
	if e, ok := m.cached[key]; ok && m.now().Sub(e.at) < m.ttl {
		m.mu.Unlock()
		return e.user, nil
	}
	m.mu.Unlock()

	u, err := m.store.GetUserByUsername(ctx, username)
	if errors.Is(err, auth.ErrUserNotFound) {
		nu := auth.User{ID: uuid.New(), Username: username, PasswordHash: ssoPasswordHash, IsActive: true}
		cerr := m.store.CreateUser(ctx, nu)
		if cerr == nil {
			log.WithFields(log.Fields{"username": username, "identity": key}).Info("auth/access: created SSO-only user on first login")
		}
		// Re-read either way: a concurrent first login may have created it.
		u, err = m.store.GetUserByUsername(ctx, username)
		if err != nil && cerr != nil {
			return nil, fmt.Errorf("create sso user %q: %w", username, cerr)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("load user %q: %w", username, err)
	}
	if !u.IsActive {
		return nil, ErrAccessUserNotAllowed
	}
	_ = m.store.UpdateLastLogin(ctx, u.ID, m.now())
	out := &User{ID: u.ID, Username: u.Username}
	m.mu.Lock()
	m.cached[key] = accessCacheEntry{user: out, at: m.now()}
	m.mu.Unlock()
	return out, nil
}

// RequireAccess authenticates every request with the Cloudflare Access JWT.
// No JWT, or an invalid one, is 401; a valid identity that is not allowlisted
// is 403. WebSocket upgrades must also be same-origin when the browser sends
// an Origin header (cross-site WebSocket hijacking guard).
func RequireAccess(v *cfaccess.Verifier, m *AccessUserMapper) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, status, err := AuthenticateAccess(r, v, m)
			if err != nil {
				log.WithFields(log.Fields{
					"path":      r.URL.Path,
					"method":    r.Method,
					"client_ip": util.ClientIPFromRequest(r),
					"status":    status,
					"error":     err.Error(),
				}).Warn("auth/access: request refused")
				WriteAccessFailure(w, status)
				return
			}
			if user.Restricted && !RestrictedRouteAllowed(r) {
				log.WithFields(log.Fields{
					"path":     r.URL.Path,
					"method":   r.Method,
					"username": user.Username,
				}).Warn("auth/access: route refused for restricted service principal")
				_ = util.WriteJSON(w, http.StatusForbidden, map[string]any{"error": "this service principal may not use this endpoint"})
				return
			}
			if isWebSocketUpgrade(r) && !sameOrigin(r) {
				log.WithFields(log.Fields{
					"path":   r.URL.Path,
					"origin": r.Header.Get("Origin"),
					"host":   r.Host,
				}).Warn("auth/access: cross-origin websocket refused")
				_ = util.WriteJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin websocket refused"})
				return
			}
			next.ServeHTTP(w, r.WithContext(ContextWithUser(r.Context(), user)))
		})
	}
}

// AuthenticateAccess verifies the request's Access JWT and maps it to a user.
// It returns the HTTP status to use on failure.
func AuthenticateAccess(r *http.Request, v *cfaccess.Verifier, m *AccessUserMapper) (*User, int, error) {
	claims, err := v.VerifyRequest(r)
	if err != nil {
		if errors.Is(err, cfaccess.ErrKeysUnusable) {
			return nil, http.StatusServiceUnavailable, err
		}
		return nil, http.StatusUnauthorized, err
	}
	var user *User
	ident := claims.Email
	if claims.ServiceToken {
		ident = "service token " + claims.CommonName
		user, err = m.LookupServiceToken(r.Context(), claims.CommonName)
	} else {
		user, err = m.Lookup(r.Context(), claims.Email)
	}
	if err != nil {
		if errors.Is(err, ErrAccessUserNotAllowed) {
			return nil, http.StatusForbidden, fmt.Errorf("%w: %s", err, ident)
		}
		return nil, http.StatusInternalServerError, err
	}
	return user, 0, nil
}

// RestrictedRouteAllowed is the complete list of what a restricted service
// principal (the CI smoke token, cluster-services-1534) may do: read who it
// is, list/open its own terminal tabs, attach to one, and close one. The
// handlers still enforce tab ownership and the allowed project; everything
// else (admin API, settings, project list, events, VNC, session logs,
// force-takeover) is refused here.
func RestrictedRouteAllowed(r *http.Request) bool {
	p := r.URL.Path
	switch {
	case r.Method == http.MethodGet && (p == "/api/auth/me" || p == "/api/auth/refresh"):
		return true
	case p == "/api/tabs" && (r.Method == http.MethodGet || r.Method == http.MethodPost):
		return true
	case r.Method == http.MethodDelete && strings.HasPrefix(p, "/api/tabs/"):
		id := strings.TrimPrefix(p, "/api/tabs/")
		return id != "" && !strings.Contains(id, "/") && id != "reorder" && id != "events"
	case r.Method == http.MethodGet && p == "/ws":
		return r.URL.Query().Get("force") == ""
	}
	return false
}

// RestrictedTabRequestAllowed checks a POST /api/tabs body for a restricted
// principal: a terminal (not GUI) tab on its one project, created new.
func RestrictedTabRequestAllowed(u *User, projectID string, guiMode bool, openAction string) bool {
	if u == nil || !u.Restricted {
		return true
	}
	return u.AllowedProject != "" && projectID == u.AllowedProject && !guiMode && (openAction == "" || openAction == "create_new")
}

// WriteAccessFailure writes the JSON error for a refused request.
func WriteAccessFailure(w http.ResponseWriter, status int) {
	msg := "authentication required: sign in through Cloudflare Access"
	switch status {
	case http.StatusForbidden:
		msg = "this Cloudflare Access identity is not allowed to use KubeTTY"
	case http.StatusServiceUnavailable:
		msg = "cannot verify Cloudflare Access identity right now"
	case http.StatusInternalServerError:
		msg = "internal error"
	}
	w.Header().Set("X-Kubetty-Auth-Mode", AuthModeCloudflareAccess)
	_ = util.WriteJSON(w, status, map[string]any{"error": msg, "authMode": AuthModeCloudflareAccess})
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// sameOrigin allows a missing Origin (non-browser clients cannot be driven
// cross-site) and otherwise requires Origin's host to equal the Host header.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// NewAccessSessionHandler serves /api/auth/me and /api/auth/refresh in SSO
// mode: the Access JWT is the session, so "refresh" just re-reports the user.
// Must be wrapped by RequireAccess.
func NewAccessSessionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil {
			WriteAccessFailure(w, http.StatusUnauthorized)
			return
		}
		_ = util.WriteJSON(w, http.StatusOK, map[string]any{
			"user":     map[string]any{"id": user.ID.String(), "username": user.Username},
			"authMode": AuthModeCloudflareAccess,
		})
	}
}

// NewAccessLogoutHandler tells the UI where Cloudflare's logout is.
func NewAccessLogoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = util.WriteJSON(w, http.StatusOK, map[string]any{"logoutUrl": AccessLogoutURL, "authMode": AuthModeCloudflareAccess})
	}
}

// NewPasswordsRetiredHandler answers the local-password endpoints in SSO mode.
func NewPasswordsRetiredHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = util.WriteJSON(w, http.StatusGone, map[string]any{
			"error":    "local passwords are retired; sign in through Cloudflare Access",
			"authMode": AuthModeCloudflareAccess,
		})
	}
}

// NewAuthConfigHandler is the public /api/auth/config endpoint the UI reads
// to choose between the password form and the Access sign-in screen.
func NewAuthConfigHandler(mode string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{"authMode": mode}
		if mode == AuthModeCloudflareAccess {
			body["logoutUrl"] = AccessLogoutURL
		}
		_ = util.WriteJSON(w, http.StatusOK, body)
	}
}
