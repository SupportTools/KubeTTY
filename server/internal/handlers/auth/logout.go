package auth

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/supporttools/KubeTTY/server/internal/auth"
	apierrors "github.com/supporttools/KubeTTY/server/internal/shared/errors"

	log "github.com/sirupsen/logrus"
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
//
//	{
//	  "refreshToken": string  // Refresh token to revoke (if not provided via cookie)
//	}
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
func NewAuthLogoutHandler(cfg AuthConfig, authMgr *auth.Manager, authStore auth.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.WithFields(log.Fields{
			"client_ip":  r.RemoteAddr,
			"user_agent": r.UserAgent(),
			"has_cookie": r.Header.Get("Cookie") != "",
		}).Debug("auth/logout: request received")

		if !AuthEnabled(cfg, authMgr) {
			log.Debug("auth/logout: authentication disabled, returning 204")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		var req LogoutRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			log.WithFields(log.Fields{
				"error":     err.Error(),
				"client_ip": r.RemoteAddr,
			}).Warn("auth/logout: invalid JSON in request body")
			_ = apierrors.WriteError(w, apierrors.BadRequest("invalid JSON", ""))
			return
		}

		token := RefreshTokenFromRequest(r, strings.TrimSpace(req.RefreshToken))
		tokenIDStr := "none"

		if token != "" && authStore != nil {
			if tokenID, _, err := auth.ParseRefreshToken(token); err == nil {
				tokenIDStr = tokenID.String()
				log.WithFields(log.Fields{
					"token_id":  tokenIDStr,
					"client_ip": r.RemoteAddr,
				}).Debug("auth/logout: attempting to revoke refresh token")

				if err := authStore.RevokeRefreshToken(r.Context(), tokenID, time.Now()); err != nil && !errors.Is(err, auth.ErrRefreshTokenNotFound) {
					log.WithFields(log.Fields{
						"token_id":  tokenIDStr,
						"error":     err.Error(),
						"client_ip": r.RemoteAddr,
					}).Warn("auth/logout: failed to revoke refresh token")
				} else {
					log.WithFields(log.Fields{
						"token_id":  tokenIDStr,
						"client_ip": r.RemoteAddr,
					}).Info("auth/logout: refresh token revoked successfully")
				}
			} else {
				log.WithFields(log.Fields{
					"error":     err.Error(),
					"client_ip": r.RemoteAddr,
				}).Warn("auth/logout: failed to parse refresh token")
			}
		} else {
			log.WithFields(log.Fields{
				"has_token": token != "",
				"has_store": authStore != nil,
				"client_ip": r.RemoteAddr,
			}).Debug("auth/logout: no refresh token to revoke")
		}

		log.WithFields(log.Fields{
			"token_id":  tokenIDStr,
			"client_ip": r.RemoteAddr,
		}).Info("auth/logout: clearing auth cookies and completing logout")

		ClearAuthCookies(w, cfg)
		w.WriteHeader(http.StatusNoContent)
	}
}
