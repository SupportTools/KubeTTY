package auth

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/supporttools/KubeTTY/server/internal/auth"
	"github.com/supporttools/KubeTTY/server/internal/config"
)

// LogoutRequest represents the logout request body.
type LogoutRequest struct {
	RefreshToken string `json:"refreshToken"` // Optional: refresh token to revoke (can also be provided via cookie)
}

// NewAuthLogoutHandler creates an HTTP handler for user logout.
//
// Endpoint: POST /api/auth/logout
// Content-Type: application/json
//
// Request Body (optional):
//   {
//     "refreshToken": string  // Refresh token to revoke (if not provided via cookie)
//   }
//
// The refresh token can be provided either in the request body or via the
// "kubetty_refresh" HTTP-only cookie.
//
// Response (204 No Content):
//   - Logout successful, cookies cleared, refresh token revoked
//
// Response (400 Bad Request):
//   - "invalid JSON" - Request body is not valid JSON
//
// The handler revokes the refresh token (if provided), clears all
// authentication cookies, and returns a 204 No Content response.
//
// If authentication is disabled, the handler returns 204 immediately.
// If the refresh token is not found or cannot be parsed, the handler
// logs a warning but still clears cookies and returns success.
func NewAuthLogoutHandler(cfg config.Config, authMgr *auth.Manager, authStore auth.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !AuthEnabled(cfg, authMgr) {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		var req LogoutRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		token := RefreshTokenFromRequest(r, strings.TrimSpace(req.RefreshToken))
		if token != "" && authStore != nil {
			if tokenID, _, err := auth.ParseRefreshToken(token); err == nil {
				if err := authStore.RevokeRefreshToken(r.Context(), tokenID, time.Now()); err != nil && !errors.Is(err, auth.ErrRefreshTokenNotFound) {
					log.Printf("warn: revoke refresh token: %v", err)
				}
			}
		}

		ClearAuthCookies(w, cfg)
		w.WriteHeader(http.StatusNoContent)
	}
}
