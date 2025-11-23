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
	"github.com/supporttools/KubeTTY/server/internal/shared/util"

	log "github.com/sirupsen/logrus"
)

// RefreshRequest represents the refresh token request body.
type RefreshRequest struct {
	RefreshToken string `json:"refreshToken"` // Optional: refresh token (can also be provided via cookie)
}

// RefreshResponse represents a successful refresh response.
type RefreshResponse struct {
	User             map[string]any `json:"user"`             // User object with id and username
	AccessToken      string         `json:"accessToken"`      // New JWT access token
	AccessExpiresAt  time.Time      `json:"accessExpiresAt"`  // Access token expiration timestamp
	RefreshExpiresAt time.Time      `json:"refreshExpiresAt"` // Refresh token expiration timestamp
}

// NewAuthRefreshHandler creates an HTTP handler for refreshing authentication tokens.
//
// Endpoint: POST /api/auth/refresh
// Content-Type: application/json
//
// Request Body (optional):
//
//	{
//	  "refreshToken": string  // Refresh token (if not provided via cookie)
//	}
//
// The refresh token can be provided either in the request body or via the
// "kubetty_refresh" HTTP-only cookie. The cookie takes precedence.
//
// Response (200 OK):
//
//	{
//	  "user": {
//	    "id": string,      // User UUID
//	    "username": string // Username
//	  },
//	  "accessToken": string,       // New JWT access token
//	  "accessExpiresAt": string,   // ISO 8601 timestamp
//	  "refreshExpiresAt": string   // ISO 8601 timestamp
//	}
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
func NewAuthRefreshHandler(cfg AuthConfig, authMgr *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !AuthEnabled(cfg, authMgr) {
			_ = apierrors.WriteError(w, apierrors.ServiceUnavailable("authentication disabled", ""))
			return
		}

		var req RefreshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			// Don't expose JSON parsing details to client for security
			_ = apierrors.WriteError(w, apierrors.BadRequest("invalid JSON", ""))
			return
		}

		token := RefreshTokenFromRequest(r, strings.TrimSpace(req.RefreshToken))
		if token == "" {
			log.WithFields(log.Fields{
				"has_cookie": r.Header.Get("Cookie") != "",
				"client_ip":  r.RemoteAddr,
			}).Warn("auth/refresh: no refresh token provided")
			_ = apierrors.WriteError(w, apierrors.Unauthorized("refresh token required", ""))
			return
		}

		// Parse token to get ID for logging (without exposing secret)
		tokenID, _, parseErr := auth.ParseRefreshToken(token)
		tokenIDStr := "parse_failed"
		if parseErr == nil {
			tokenIDStr = tokenID.String()
		}

		log.WithFields(log.Fields{
			"token_id":  tokenIDStr,
			"client_ip": r.RemoteAddr,
		}).Debug("auth/refresh: attempting refresh")

		meta := TokenMetadataFromRequest(r)
		pair, err := authMgr.Refresh(r.Context(), token, meta)
		if err != nil {
			log.WithFields(log.Fields{
				"token_id":  tokenIDStr,
				"error":     err.Error(),
				"client_ip": r.RemoteAddr,
			}).Warn("auth/refresh: refresh failed")

			switch {
			case errors.Is(err, auth.ErrTokenExpired):
				_ = apierrors.WriteError(w, apierrors.Unauthorized("refresh token expired", ""))
			case errors.Is(err, auth.ErrTokenRevoked):
				_ = apierrors.WriteError(w, apierrors.Unauthorized("refresh token revoked", ""))
			case errors.Is(err, auth.ErrInvalidCredentials):
				_ = apierrors.WriteError(w, apierrors.Unauthorized("account disabled", ""))
			default:
				_ = apierrors.WriteError(w, apierrors.InternalServerError("token refresh failed", ""))
			}
			return
		}

		log.WithFields(log.Fields{
			"token_id":  tokenIDStr,
			"client_ip": r.RemoteAddr,
		}).Info("auth/refresh: refresh successful")

		SetAuthCookies(w, pair, cfg)

		claims, err := authMgr.ValidateAccessToken(pair.AccessToken)
		if err != nil {
			_ = apierrors.WriteError(w, apierrors.InternalServerError("invalid access token", ""))
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
