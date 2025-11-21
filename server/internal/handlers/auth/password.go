package auth

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/supporttools/KubeTTY/server/internal/auth"
	apierrors "github.com/supporttools/KubeTTY/server/internal/shared/errors"
	"github.com/supporttools/KubeTTY/server/internal/shared/util"
)

// PasswordChangeRequest represents the password change request body.
type PasswordChangeRequest struct {
	CurrentPassword string `json:"currentPassword"` // User's current password
	NewPassword     string `json:"newPassword"`     // Desired new password
}

// PasswordChangeResponse represents a successful password change response.
type PasswordChangeResponse struct {
	Message string `json:"message"` // Success message
}

// NewAuthPasswordChangeHandler creates an HTTP handler for changing user passwords.
//
// Endpoint: POST /api/auth/password
// Content-Type: application/json
// Authentication: Required (via access token)
//
// Request Body:
//
//	{
//	  "currentPassword": string,  // User's current password
//	  "newPassword": string       // Desired new password
//	}
//
// Response (200 OK):
//
//	{
//	  "message": "password changed successfully"
//	}
//
// Response (400 Bad Request):
//   - "invalid request body" - Request body is not valid JSON
//   - "current and new password are required" - Missing required fields
//   - "<error>" - Password validation error (e.g., weak password)
//
// Response (401 Unauthorized):
//   - "unauthorized" - No authenticated user found in request context
//   - "current password is incorrect" - Current password does not match
//
// Response (405 Method Not Allowed):
//   - "method not allowed" - HTTP method is not POST
//
// Response (500 Internal Server Error):
//   - "failed to change password" - Password change service error
//
// The handler validates the current password, changes it to the new password,
// and revokes all refresh tokens for the user. The access and refresh token
// cookies are cleared, requiring the user to log in again.
//
// This handler must be used with the RequireAuth middleware to ensure
// the user is authenticated.
func NewAuthPasswordChangeHandler(cfg AuthConfig, authMgr *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			_ = apierrors.WriteError(w, apierrors.ErrorResponse{
				Status:  http.StatusMethodNotAllowed,
				Error:   "method_not_allowed",
				Message: "method not allowed",
			})
			return
		}

		user := UserFromContext(r.Context())
		if user == nil {
			_ = apierrors.WriteError(w, apierrors.Unauthorized("unauthorized", ""))
			return
		}

		var req PasswordChangeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// Don't expose JSON parsing details to client for security
			_ = apierrors.WriteError(w, apierrors.BadRequest("invalid request body", ""))
			return
		}

		if req.CurrentPassword == "" || req.NewPassword == "" {
			_ = apierrors.WriteError(w, apierrors.BadRequest("current and new password are required", ""))
			return
		}

		err := authMgr.ChangePassword(r.Context(), user.ID, req.CurrentPassword, req.NewPassword)
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrInvalidCredentials):
				_ = apierrors.WriteError(w, apierrors.Unauthorized("current password is incorrect", ""))
			case errors.Is(err, auth.ErrWeakPassword):
				// Don't expose validation details to client for security
				_ = apierrors.WriteError(w, apierrors.BadRequest("password requirements not met", ""))
			default:
				log.Printf("password change error: %v", err)
				_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to change password", ""))
			}
			return
		}

		// Clear auth cookies since refresh tokens were revoked
		ClearAccessCookie(w, cfg)
		ClearRefreshCookie(w, cfg)

		_ = util.WriteJSON(w, http.StatusOK, PasswordChangeResponse{
			Message: "password changed successfully",
		})
	}
}
