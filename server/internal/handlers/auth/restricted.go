package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/supporttools/KubeTTY/server/internal/auth"
	"github.com/supporttools/KubeTTY/server/internal/shared/util"

	log "github.com/sirupsen/logrus"
)

// ParseRestrictedUsers parses AUTH_RESTRICTED_USERS: comma-separated
// <username>:<project> entries for local accounts such as the CI smoke user.
func ParseRestrictedUsers(spec string) (map[string]string, error) {
	out := map[string]string{}
	for _, item := range strings.Split(spec, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		username, project, ok := strings.Cut(item, ":")
		username = strings.TrimSpace(username)
		project = strings.TrimSpace(project)
		if !ok || username == "" || project == "" {
			return nil, fmt.Errorf("AUTH_RESTRICTED_USERS entry %q must be <username>:<project>", item)
		}
		if len(username) > MaxUsernameLength || !UsernameRegex.MatchString(username) {
			return nil, fmt.Errorf("AUTH_RESTRICTED_USERS entry %q: invalid username", item)
		}
		if _, dup := out[username]; dup {
			return nil, fmt.Errorf("AUTH_RESTRICTED_USERS lists %q twice", username)
		}
		out[username] = project
	}
	return out, nil
}

// RestrictUsers must run after RequireAuth. It marks a listed local user
// Restricted and refuses every route outside RestrictedRouteAllowed with 403.
func RestrictUsers(projects map[string]string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if next == nil || len(projects) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if user == nil || user.GitHubLogin != "" {
				next.ServeHTTP(w, r)
				return
			}
			project, ok := projects[user.Username]
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			if !RestrictedRouteAllowed(r) {
				log.WithFields(log.Fields{
					"path":     r.URL.Path,
					"method":   r.Method,
					"username": user.Username,
				}).Warn("auth/restricted: route refused for restricted user")
				_ = util.WriteJSON(w, http.StatusForbidden, map[string]any{"error": "this user may not use this endpoint"})
				return
			}
			restricted := *user
			restricted.Role = auth.RoleUser
			restricted.Restricted = true
			restricted.AllowedProject = project
			next.ServeHTTP(w, r.WithContext(ContextWithUser(r.Context(), &restricted)))
		})
	}
}

// RequireAdmin must run after RequireAuth. With authentication enabled it
// refuses non-admins with 403.
func RequireAdmin(enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if next == nil || !enabled {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !UserFromContext(r.Context()).IsAdmin() {
				_ = util.WriteJSON(w, http.StatusForbidden, map[string]any{"error": "admin role required"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RestrictedRouteAllowed is everything a restricted user may do: read who it
// is, log out, list/open its own terminal tabs, attach to one, and close one.
// The handlers still enforce tab ownership and the allowed project.
func RestrictedRouteAllowed(r *http.Request) bool {
	p := r.URL.Path
	switch {
	case r.Method == http.MethodGet && p == "/api/auth/me":
		return true
	case r.Method == http.MethodPost && p == "/api/auth/logout":
		return true
	case p == "/api/tabs" && (r.Method == http.MethodGet || r.Method == http.MethodPost):
		return true
	case r.Method == http.MethodDelete && strings.HasPrefix(p, "/api/tabs/"):
		id := strings.TrimPrefix(p, "/api/tabs/")
		return id != "" && !strings.Contains(id, "/") && id != "reorder" && id != "events"
	case r.Method == http.MethodGet && p == "/ws":
		return r.URL.Query().Get("force") == ""
	}
	return false
}

// RestrictedTabRequestAllowed checks a POST /api/tabs body for a restricted
// user: a new terminal (not GUI) tab on its one project.
func RestrictedTabRequestAllowed(u *User, projectID string, guiMode bool, openAction string) bool {
	if u == nil || !u.Restricted {
		return true
	}
	return u.AllowedProject != "" && projectID == u.AllowedProject && !guiMode && (openAction == "" || openAction == "create_new")
}

// RepoCheck reports whether login has write access to repo.
type RepoCheck func(ctx context.Context, repo, login string) (bool, error)

// CanUseProject decides whether user may open or attach terminals on a
// project. repo is the project's GitHub repo ("" if none). Admins may use
// every project, restricted users only their own, GitHub users the projects
// whose repo they can write to.
func CanUseProject(ctx context.Context, user *User, projectID, repo string, canWrite RepoCheck) (bool, error) {
	switch {
	case user == nil:
		return false, nil
	case user.Restricted:
		return user.AllowedProject != "" && user.AllowedProject == projectID, nil
	case user.IsAdmin():
		return true, nil
	case repo == "" || user.GitHubLogin == "" || canWrite == nil:
		return false, nil
	}
	return canWrite(ctx, repo, user.GitHubLogin)
}
