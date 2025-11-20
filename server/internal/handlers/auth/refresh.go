package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/supporttools/KubeTTY/server/internal/auth"
	"github.com/supporttools/KubeTTY/server/internal/config"
	"github.com/supporttools/KubeTTY/server/internal/shared/util"
)

// RefreshRequest represents the refresh token request body.
type RefreshRequest struct {
	RefreshToken string `json:"refreshToken"` // Optional: refresh token (can also be provided via cookie)
}

// RefreshResponse represents a successful refresh response.
type RefreshResponse struct {
	User              map[string]any `json:"user"`              // User object with id and username
	AccessToken       string         `json:"accessToken"`       // New JWT access token
	AccessExpiresAt   time.Time      `json:"accessExpiresAt"`   // Access token expiration timestamp
	RefreshExpiresAt  time.Time      `json:"refreshExpiresAt"`  // Refresh token expiration timestamp
}

// NewAuthRefreshHandler creates an HTTP handler for refreshing authentication tokens.
//
// Endpoint: POST /api/auth/refresh
// Content-Type: application/json
//
// Request Body (optional):
//   {
//     "refreshToken": string  // Refresh token (if not provided via cookie)
//   }
//
// The refresh token can be provided either in the request body or via the
// "kubetty_refresh" HTTP-only cookie. The cookie takes precedence.
//
// Response (200 OK):
//   {
//     "user": {
//       "id": string,      // User UUID
//       "username": string // Username
//     },
//     "accessToken": string,       // New JWT access token
//     "accessExpiresAt": string,   // ISO 8601 timestamp
//     "refreshExpiresAt": string   // ISO 8601 timestamp
//   }
//
// Response (400 Bad Request):
//   - "invalid JSON" - Request body is not valid JSON
//
// Response (401 Unauthorized):
//   - "refresh token required" - No refresh token provided
//   - "refresh token expired" - Token has expired
//   - "refresh token revoked" - Token has been revoked
//   - "account disabled" - User account is disabled
//
// Response (500 Internal Server Error):
//   - "refresh: <error>" - Token refresh service error
//   - "invalid access token" - Generated access token is invalid
//
// Response (501 Not Implemented):
//   - "auth disabled" - Authentication is not enabled
//
// The handler validates the refresh token, issues a new token pair,
// and sets updated HTTP-only cookies.
func NewAuthRefreshHandler(cfg config.Config, authMgr *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !AuthEnabled(cfg, authMgr) {
			http.Error(w, "auth disabled", http.StatusNotImplemented)
			return
		}

		var req RefreshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		token := RefreshTokenFromRequest(r, strings.TrimSpace(req.RefreshToken))
		if token == "" {
			http.Error(w, "refresh token required", http.StatusUnauthorized)
			return
		}

		meta := TokenMetadataFromRequest(r)
		pair, err := authMgr.Refresh(r.Context(), token, meta)
		if err != nil {
			status := http.StatusUnauthorized
			switch {
			case errors.Is(err, auth.ErrTokenExpired):
				http.Error(w, "refresh token expired", status)
			case errors.Is(err, auth.ErrTokenRevoked):
				http.Error(w, "refresh token revoked", status)
			case errors.Is(err, auth.ErrInvalidCredentials):
				http.Error(w, "account disabled", status)
			default:
				http.Error(w, fmt.Sprintf("refresh: %v", err), http.StatusInternalServerError)
			}
			return
		}

		SetAuthCookies(w, pair, cfg)

		claims, err := authMgr.ValidateAccessToken(pair.AccessToken)
		if err != nil {
			http.Error(w, "invalid access token", http.StatusInternalServerError)
			return
		}

		_ = util.WriteJSON(w, http.StatusOK, RefreshResponse{
			User: map[string]any{
				"id":       claims.Subject,
				"username": claims.Username,
			},
			AccessToken:      pair.AccessToken,
			AccessExpiresAt:  pair.AccessExpiresAt,
			RefreshExpiresAt: pair.RefreshExpiresAt,
		})
	}
}
