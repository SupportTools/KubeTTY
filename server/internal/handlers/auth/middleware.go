package auth

import (
	"net/http"

	"github.com/supporttools/KubeTTY/server/internal/auth"
	"github.com/supporttools/KubeTTY/server/internal/config"
)

// RequireAuth returns a middleware that enforces authentication.
// If authentication is disabled or the authMgr is nil, the middleware
// passes through without modification. Otherwise, it validates the
// access token and adds the authenticated user to the request context.
//
// If authentication fails, it responds with an appropriate error and
// does not call the next handler.
func RequireAuth(cfg config.Config, authMgr *auth.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if next == nil || !AuthEnabled(cfg, authMgr) {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, err := AuthenticateRequest(r, authMgr)
			if err != nil {
				HandleAuthFailure(w, err, cfg)
				return
			}
			ctx := ContextWithUser(r.Context(), user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
