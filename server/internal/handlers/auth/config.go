package auth

import (
	"net/http"

	"github.com/supporttools/KubeTTY/server/internal/shared/util"
)

// AuthModeGitHub is the AUTH_MODE value for GitHub sign-in.
const AuthModeGitHub = "github"

// GitHubLoginPath starts the GitHub sign-in.
const GitHubLoginPath = "/api/auth/github/login"

// NewAuthConfigHandler is the public /api/auth/config endpoint the UI reads
// to choose its sign-in screen.
func NewAuthConfigHandler(mode string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{"authMode": mode}
		if mode == AuthModeGitHub {
			body["githubLoginUrl"] = GitHubLoginPath
		}
		_ = util.WriteJSON(w, http.StatusOK, body)
	}
}
