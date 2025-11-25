package auth

import (
	"net/http"

	"github.com/supporttools/KubeTTY/server/internal/auth"

	log "github.com/sirupsen/logrus"
)

// RequireAuth returns a middleware that enforces authentication.
// If authentication is disabled or the authMgr is nil, the middleware
// passes through without modification. Otherwise, it validates the
// access token and adds the authenticated user to the request context.
//
// If authentication fails, it responds with an appropriate error and
// does not call the next handler.
func RequireAuth(cfg AuthConfig, authMgr *auth.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if next == nil || !AuthEnabled(cfg, authMgr) {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.WithFields(log.Fields{
				"path":       r.URL.Path,
				"method":     r.Method,
				"client_ip":  r.RemoteAddr,
				"user_agent": r.UserAgent(),
				"has_cookie": r.Header.Get("Cookie") != "",
				"has_authz":  r.Header.Get("Authorization") != "",
			}).Debug("auth/middleware: authenticating request")

			user, err := AuthenticateRequest(r, authMgr)
			if err != nil {
				log.WithFields(log.Fields{
					"path":      r.URL.Path,
					"method":    r.Method,
					"client_ip": r.RemoteAddr,
					"error":     err.Error(),
				}).Warn("auth/middleware: authentication failed")
				HandleAuthFailure(w, err, cfg)
				return
			}

			log.WithFields(log.Fields{
				"path":      r.URL.Path,
				"method":    r.Method,
				"client_ip": r.RemoteAddr,
				"user_id":   user.ID.String(),
				"username":  user.Username,
			}).Debug("auth/middleware: authentication successful")

			ctx := ContextWithUser(r.Context(), user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
