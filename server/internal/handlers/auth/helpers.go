package auth

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/supporttools/KubeTTY/server/internal/auth"
	"github.com/supporttools/KubeTTY/server/internal/shared/util"
)

// Cookie names used for authentication
const (
	ClientCookieName       = "kubetty_client"
	AccessTokenCookieName  = "kubetty_access"
	RefreshTokenCookieName = "kubetty_refresh"
)

// Input validation limits
const (
	MaxUsernameLength = 64
)

// UsernameRegex allows only alphanumeric characters, underscores, and dashes
var UsernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Common auth errors
var (
	ErrAuthMissingToken = errors.New("authentication token missing")
	ErrAuthDisabled     = errors.New("authentication disabled")
)

// AuthConfig defines the configuration interface needed for auth handlers.
// Both config.Config and config.GatewayConfig implement this interface.
type AuthConfig interface {
	GetAuthMode() string
	GetAuthCookieDomain() string
	GetAuthCookieSecure() bool
}

// contextKey is used for storing values in request context
type contextKey string

const authUserContextKey contextKey = "kubettyAuthUser"

// User represents an authenticated user in the request context
type User struct {
	ID       uuid.UUID
	Username string
}

// StoreMetricsObserver defines an interface for observing store operations.
// Implementations can use this to track metrics (e.g., Prometheus counters).
type StoreMetricsObserver interface {
	ObserveStore(operation string, start time.Time, err error)
}

// UserFromContext extracts the authenticated user from the request context.
// Returns nil if no user is found in the context.
func UserFromContext(ctx context.Context) *User {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(authUserContextKey).(*User); ok {
		return v
	}
	return nil
}

// ContextWithUser returns a new context with the user embedded.
func ContextWithUser(ctx context.Context, user *User) context.Context {
	return context.WithValue(ctx, authUserContextKey, user)
}

// AccessTokenFromRequest extracts the access token from the HTTP request.
// It checks the Authorization header first (Bearer token), then falls back
// to the access token cookie.
func AccessTokenFromRequest(r *http.Request) string {
	authz := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return strings.TrimSpace(authz[7:])
	}
	if c, err := r.Cookie(AccessTokenCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	return ""
}

// RefreshTokenFromRequest extracts the refresh token from the HTTP request.
// It first checks if a token was provided directly, then falls back to the
// refresh token cookie.
func RefreshTokenFromRequest(r *http.Request, provided string) string {
	if provided != "" {
		return provided
	}
	if c, err := r.Cookie(RefreshTokenCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	return ""
}

// TokenMetadataFromRequest extracts metadata from the HTTP request for token creation.
func TokenMetadataFromRequest(r *http.Request) auth.TokenMetadata {
	return auth.TokenMetadata{
		CreatedBy: r.Header.Get("X-Requested-By"),
		UserAgent: r.UserAgent(),
		ClientIP:  util.ClientIPFromRequest(r),
	}
}

// SetAuthCookies sets both access and refresh token cookies in the response.
func SetAuthCookies(w http.ResponseWriter, pair *auth.TokenPair, cfg AuthConfig) {
	if pair == nil {
		return
	}
	http.SetCookie(w, cookieTemplate(AccessTokenCookieName, pair.AccessToken, pair.AccessExpiresAt, cfg))
	http.SetCookie(w, cookieTemplate(RefreshTokenCookieName, pair.RefreshToken, pair.RefreshExpiresAt, cfg))
}

// ClearAuthCookies clears both access and refresh token cookies.
func ClearAuthCookies(w http.ResponseWriter, cfg AuthConfig) {
	ClearAccessCookie(w, cfg)
	ClearRefreshCookie(w, cfg)
}

// ClearAccessCookie clears the access token cookie.
func ClearAccessCookie(w http.ResponseWriter, cfg AuthConfig) {
	c := cookieTemplate(AccessTokenCookieName, "", time.Time{}, cfg)
	c.MaxAge = -1
	c.Expires = time.Unix(0, 0)
	http.SetCookie(w, c)
}

// ClearRefreshCookie clears the refresh token cookie.
func ClearRefreshCookie(w http.ResponseWriter, cfg AuthConfig) {
	c := cookieTemplate(RefreshTokenCookieName, "", time.Time{}, cfg)
	c.MaxAge = -1
	c.Expires = time.Unix(0, 0)
	http.SetCookie(w, c)
}

// cookieTemplate creates an HTTP cookie with standard security settings.
func cookieTemplate(name, value string, expires time.Time, cfg AuthConfig) *http.Cookie {
	c := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   cfg.GetAuthCookieSecure(),
		SameSite: http.SameSiteLaxMode,
	}
	if cfg.GetAuthCookieDomain() != "" {
		c.Domain = cfg.GetAuthCookieDomain()
	}
	if !expires.IsZero() {
		c.Expires = expires
		maxAge := int(time.Until(expires).Seconds())
		if maxAge < 0 {
			maxAge = 0
		}
		c.MaxAge = maxAge
	}
	return c
}

// AuthEnabled checks if authentication is enabled based on configuration.
func AuthEnabled(cfg AuthConfig, authMgr *auth.Manager) bool {
	return cfg.GetAuthMode() == "local" && authMgr != nil
}

// AuthenticateRequest validates the access token from the request and returns
// the authenticated user. Returns an error if authentication fails.
func AuthenticateRequest(r *http.Request, authMgr *auth.Manager) (*User, error) {
	token := AccessTokenFromRequest(r)
	if token == "" {
		return nil, ErrAuthMissingToken
	}
	claims, err := authMgr.ValidateAccessToken(token)
	if err != nil {
		return nil, err
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, auth.ErrTokenMalformed
	}
	return &User{ID: userID, Username: claims.Username}, nil
}

// HandleAuthFailure sends a standardized authentication failure response.
// It clears the access cookie and sets appropriate error messages based on
// the error type.
func HandleAuthFailure(w http.ResponseWriter, err error, cfg AuthConfig) {
	msg := "unauthorized"
	status := http.StatusUnauthorized
	switch {
	case errors.Is(err, ErrAuthMissingToken):
		msg = "authentication required"
	case errors.Is(err, auth.ErrTokenExpired):
		msg = "token expired"
	case errors.Is(err, auth.ErrTokenMalformed):
		msg = "token malformed"
	case errors.Is(err, ErrAuthDisabled):
		msg = "auth disabled"
	}
	ClearAccessCookie(w, cfg)
	w.Header().Set("WWW-Authenticate", `Bearer realm="kubetty"`)
	_ = util.WriteJSON(w, status, map[string]any{"error": msg})
}
