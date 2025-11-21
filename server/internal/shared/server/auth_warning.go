package server

import "net/http"

// AuthWarningMiddleware adds an X-Auth-Warning header to all responses when
// authentication is disabled. This helps developers and security auditors
// quickly identify that the gateway is running without authentication.
//
// The header is only added when authMode is not "local" (i.e., auth is disabled).
func AuthWarningMiddleware(authMode string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		// If auth is enabled, pass through without modification
		if authMode == "local" {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Add warning header to indicate authentication is disabled
			w.Header().Set("X-Auth-Warning", "Authentication is disabled")
			next.ServeHTTP(w, r)
		})
	}
}
