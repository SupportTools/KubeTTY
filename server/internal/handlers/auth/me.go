package auth

import (
	"net/http"

	"github.com/supporttools/KubeTTY/server/internal/shared/util"
)

// MeResponse represents the current user information response.
type MeResponse struct {
	User map[string]any `json:"user"` // User object with id and username
}

// NewAuthMeHandler creates an HTTP handler for retrieving the current authenticated user.
//
// Endpoint: GET /api/auth/me
// Authentication: Required (via access token)
//
// Response (200 OK):
//   {
//     "user": {
//       "id": string,      // User UUID
//       "username": string // Username
//     }
//   }
//
// Response (401 Unauthorized):
//   - "unauthorized" - No authenticated user found in request context
//
// This handler must be used with the RequireAuth middleware to ensure
// the user is authenticated. It extracts the user from the request context
// (populated by the middleware) and returns the user information.
func NewAuthMeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		_ = util.WriteJSON(w, http.StatusOK, MeResponse{
			User: map[string]any{
				"id":       user.ID.String(),
				"username": user.Username,
			},
		})
	}
}
