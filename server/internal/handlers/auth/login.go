package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/supporttools/KubeTTY/server/internal/auth"
	apierrors "github.com/supporttools/KubeTTY/server/internal/shared/errors"
	"github.com/supporttools/KubeTTY/server/internal/shared/util"
)

// LoginRequest represents the login request body.
type LoginRequest struct {
	Username string `json:"username"` // Username (max 64 chars, alphanumeric + underscore/dash)
	Password string `json:"password"` // User password
}

// LoginResponse represents a successful login response.
type LoginResponse struct {
	User             map[string]any `json:"user"`             // User object with id and username
	AccessToken      string         `json:"accessToken"`      // JWT access token
	AccessExpiresAt  time.Time      `json:"accessExpiresAt"`  // Access token expiration timestamp
	RefreshExpiresAt time.Time      `json:"refreshExpiresAt"` // Refresh token expiration timestamp
}

// NewAuthLoginHandler creates an HTTP handler for user authentication.
//
// Endpoint: POST /api/auth/login
// Content-Type: application/json
//
// Request Body:
//
//	{
//	  "username": string,  // Username (max 64 chars, alphanumeric + underscore/dash)
//	  "password": string   // User password
//	}
//
// Response (200 OK):
//
//	{
//	  "user": {
//	    "id": string,      // User UUID
//	    "username": string // Username
//	  },
//	  "accessToken": string,       // JWT access token
//	  "accessExpiresAt": string,   // ISO 8601 timestamp
//	  "refreshExpiresAt": string   // ISO 8601 timestamp
//	}
//
// Response (400 Bad Request):
//   - "invalid JSON" - Request body is not valid JSON
//   - "username and password required" - Missing credentials
//   - "username must be 64 characters or less" - Username too long
//   - "username must contain only letters, numbers, underscores, and dashes" - Invalid characters
//
// Response (401 Unauthorized):
//   - "invalid credentials" - Username or password is incorrect
//
// Response (500 Internal Server Error):
//   - "authenticate: <error>" - Authentication service error
//   - "issue tokens: <error>" - Token generation error
//
// Response (501 Not Implemented):
//   - "auth disabled" - Authentication is not enabled
//
// The handler validates credentials, issues JWT tokens, sets HTTP-only
// cookies, and updates the user's last login timestamp.
func NewAuthLoginHandler(cfg AuthConfig, authMgr *auth.Manager, authStore auth.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !AuthEnabled(cfg, authMgr) {
			_ = apierrors.WriteError(w, apierrors.ServiceUnavailable("authentication disabled", ""))
			return
		}

		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// Don't expose JSON parsing details to client for security
			_ = apierrors.WriteError(w, apierrors.BadRequest("invalid JSON", ""))
			return
		}

		req.Username = strings.TrimSpace(req.Username)
		if req.Username == "" || req.Password == "" {
			_ = apierrors.WriteError(w, apierrors.BadRequest("username and password required", ""))
			return
		}

		if len(req.Username) > MaxUsernameLength {
			_ = apierrors.WriteError(w, apierrors.BadRequest(
				fmt.Sprintf("username must be %d characters or less", MaxUsernameLength), ""))
			return
		}

		if !UsernameRegex.MatchString(req.Username) {
			_ = apierrors.WriteError(w, apierrors.BadRequest(
				"username must contain only letters, numbers, underscores, and dashes", ""))
			return
		}

		user, err := authMgr.Authenticate(r.Context(), req.Username, req.Password)
		if err != nil {
			if errors.Is(err, auth.ErrInvalidCredentials) {
				_ = apierrors.WriteError(w, apierrors.Unauthorized("invalid credentials", ""))
				return
			}
			_ = apierrors.WriteError(w, apierrors.InternalServerError("authentication failed", ""))
			return
		}

		meta := TokenMetadataFromRequest(r)
		tokens, err := authMgr.IssueTokenPair(r.Context(), user, meta)
		if err != nil {
			_ = apierrors.WriteError(w, apierrors.InternalServerError("token generation failed", ""))
			return
		}

		if authStore != nil {
			if err := authStore.UpdateLastLogin(r.Context(), user.ID, time.Now()); err != nil {
				log.Printf("warn: update last login: %v", err)
			}
		}

		SetAuthCookies(w, tokens, cfg)
		_ = util.WriteJSON(w, http.StatusOK, LoginResponse{
			User: map[string]any{
				"id":       user.ID.String(),
				"username": user.Username,
			},
			AccessToken:      tokens.AccessToken,
			AccessExpiresAt:  tokens.AccessExpiresAt,
			RefreshExpiresAt: tokens.RefreshExpiresAt,
		})
	}
}
