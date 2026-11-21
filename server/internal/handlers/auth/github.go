package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/supporttools/KubeTTY/server/internal/auth"
	gh "github.com/supporttools/KubeTTY/server/internal/auth/github"

	log "github.com/sirupsen/logrus"
)

const githubStateCookie = "kubetty_github_oauth"

// githubPasswordHash is not a bcrypt hash, so a GitHub account can never sign in with a password.
var githubPasswordHash = []byte("!github")

// GitHubClient is the part of the GitHub client the sign-in flow uses.
type GitHubClient interface {
	AuthorizeURL(redirectURI, state, codeChallenge string) string
	SignIn(ctx context.Context, code, codeVerifier, redirectURI string) (gh.Identity, error)
}

// GitHubAuthorizer answers who may sign in and who is an admin.
type GitHubAuthorizer interface {
	Admitted(ctx context.Context, login string) (bool, error)
	IsAdmin(ctx context.Context, login string) (bool, error)
}

// GitHubSignIn serves /api/auth/github/login and /api/auth/github/callback.
type GitHubSignIn struct {
	Config      AuthConfig
	Manager     *auth.Manager
	Store       auth.Store
	Client      GitHubClient
	Authorizer  GitHubAuthorizer
	CallbackURL string
	// Reserved local usernames a GitHub account must never take over
	// (the break-glass admin and restricted users).
	Reserved map[string]bool
}

// GitHubAdmission is the token admission rule for AUTH_MODE=github: GitHub
// accounts need a matching allow rule (admins from the admin teams); the only
// local accounts allowed are the break-glass admin and restricted users.
func GitHubAdmission(authz GitHubAuthorizer, breakGlassUser string, restricted map[string]string) auth.Admission {
	return func(ctx context.Context, u *auth.User) (string, error) {
		if u.GitHubID == 0 {
			switch {
			case breakGlassUser != "" && strings.EqualFold(u.Username, breakGlassUser):
				return auth.RoleAdmin, nil
			case restricted[u.Username] != "":
				return auth.RoleUser, nil
			}
			return "", auth.ErrNotAdmitted
		}
		ok, err := authz.Admitted(ctx, u.GitHubLogin)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", auth.ErrNotAdmitted
		}
		admin, err := authz.IsAdmin(ctx, u.GitHubLogin)
		if err != nil {
			return "", err
		}
		if admin {
			return auth.RoleAdmin, nil
		}
		return auth.RoleUser, nil
	}
}

// LoginHandler sends the browser to GitHub with a fresh state and PKCE challenge.
func (g *GitHubSignIn) LoginHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state, err1 := randomURLString()
		verifier, err2 := randomURLString()
		if err1 != nil || err2 != nil {
			http.Error(w, "cannot start sign-in", http.StatusInternalServerError)
			return
		}
		sum := sha256.Sum256([]byte(verifier))
		http.SetCookie(w, &http.Cookie{
			Name:     githubStateCookie,
			Value:    state + "." + verifier,
			Path:     "/api/auth/github/",
			MaxAge:   600,
			HttpOnly: true,
			Secure:   g.Config.GetAuthCookieSecure(),
			SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, g.Client.AuthorizeURL(g.CallbackURL, state, base64.RawURLEncoding.EncodeToString(sum[:])), http.StatusFound)
	}
}

// CallbackHandler finishes the sign-in and redirects to the UI. Failures go
// to /?auth_error=<reason>.
func (g *GitHubSignIn) CallbackHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fail := func(reason string, err error, fields log.Fields) {
			entry := log.WithFields(fields).WithField("reason", reason)
			if err != nil {
				entry = entry.WithError(err)
			}
			entry.Warn("auth/github: sign-in refused")
			http.Redirect(w, r, "/?auth_error="+reason, http.StatusFound)
		}

		q := r.URL.Query()
		cookie, cerr := r.Cookie(githubStateCookie)
		http.SetCookie(w, &http.Cookie{Name: githubStateCookie, Value: "", Path: "/api/auth/github/", MaxAge: -1, HttpOnly: true, Secure: g.Config.GetAuthCookieSecure(), SameSite: http.SameSiteLaxMode})
		if q.Get("error") != "" {
			fail("github_denied", nil, log.Fields{"github_error": q.Get("error")})
			return
		}
		var state, verifier string
		if cerr == nil {
			state, verifier, _ = strings.Cut(cookie.Value, ".")
		}
		if state == "" || verifier == "" || q.Get("code") == "" || subtle.ConstantTimeCompare([]byte(state), []byte(q.Get("state"))) != 1 {
			fail("state", nil, nil)
			return
		}

		ctx := r.Context()
		id, err := g.Client.SignIn(ctx, q.Get("code"), verifier, g.CallbackURL)
		if err != nil {
			fail("github_failed", err, nil)
			return
		}
		fields := log.Fields{"github_login": id.Login, "github_id": id.ID}
		ok, err := g.Authorizer.Admitted(ctx, id.Login)
		if err != nil {
			fail("github_failed", err, fields)
			return
		}
		if !ok {
			fail("not_allowed", nil, fields)
			return
		}
		user, err := g.account(ctx, id)
		if err != nil {
			fail("github_failed", err, fields)
			return
		}
		if !user.IsActive {
			fail("not_allowed", nil, fields)
			return
		}
		tokens, err := g.Manager.IssueTokenPair(ctx, user, TokenMetadataFromRequest(r))
		if errors.Is(err, auth.ErrNotAdmitted) {
			fail("not_allowed", nil, fields)
			return
		}
		if err != nil {
			fail("github_failed", err, fields)
			return
		}
		if err := g.Store.UpdateLastLogin(ctx, user.ID, time.Now()); err != nil {
			log.WithError(err).Warn("auth/github: failed to update last login")
		}
		log.WithFields(fields).WithField("username", user.Username).Info("auth/github: signed in")
		SetAuthCookies(w, tokens, g.Config)
		http.Redirect(w, r, "/", http.StatusFound)
	}
}

// account returns the KubeTTY account for a GitHub identity. It links an
// existing active local account with the same name unless that name is
// reserved; otherwise it creates one.
func (g *GitHubSignIn) account(ctx context.Context, id gh.Identity) (*auth.User, error) {
	u, err := g.Store.GetUserByGitHubID(ctx, id.ID)
	if err == nil {
		if u.GitHubLogin != id.Login {
			if err := g.Store.LinkGitHub(ctx, u.ID, id.ID, id.Login, u.PasswordHash); err != nil {
				return nil, err
			}
			u.GitHubLogin = id.Login
		}
		return u, nil
	}
	if !errors.Is(err, auth.ErrUserNotFound) {
		return nil, err
	}

	username := id.Login
	existing, err := g.Store.GetUserByUsername(ctx, id.Login)
	switch {
	case err == nil && existing.GitHubID == 0 && existing.IsActive && !g.reserved(existing.Username):
		if err := g.Store.LinkGitHub(ctx, existing.ID, id.ID, id.Login, githubPasswordHash); err != nil {
			return nil, err
		}
		log.WithFields(log.Fields{"username": existing.Username, "github_id": id.ID}).Info("auth/github: linked existing local account")
		return g.Store.GetUserByGitHubID(ctx, id.ID)
	case err == nil || g.reserved(id.Login):
		username = fmt.Sprintf("%s-gh%d", id.Login, id.ID)
	case !errors.Is(err, auth.ErrUserNotFound):
		return nil, err
	}

	cerr := g.Store.CreateUser(ctx, auth.User{ID: uuid.New(), Username: username, PasswordHash: githubPasswordHash, IsActive: true, GitHubID: id.ID, GitHubLogin: id.Login})
	u, err = g.Store.GetUserByGitHubID(ctx, id.ID)
	if err != nil && cerr != nil {
		return nil, fmt.Errorf("create github account %q: %w", username, cerr)
	}
	if err == nil && cerr == nil {
		log.WithFields(log.Fields{"username": username, "github_id": id.ID}).Info("auth/github: created account on first sign-in")
	}
	return u, err
}

func (g *GitHubSignIn) reserved(username string) bool {
	for name := range g.Reserved {
		if strings.EqualFold(name, username) {
			return true
		}
	}
	return false
}

func randomURLString() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
