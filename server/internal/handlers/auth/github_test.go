package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/supporttools/KubeTTY/server/internal/auth"
	gh "github.com/supporttools/KubeTTY/server/internal/auth/github"
)

type fakeGitHubClient struct {
	identity     gh.Identity
	err          error
	gotVerifier  string
	gotChallenge string
}

func (f *fakeGitHubClient) AuthorizeURL(redirectURI, state, challenge string) string {
	f.gotChallenge = challenge
	return "https://github.example/login/oauth/authorize?state=" + url.QueryEscape(state) + "&redirect_uri=" + url.QueryEscape(redirectURI)
}

func (f *fakeGitHubClient) SignIn(_ context.Context, code, verifier, _ string) (gh.Identity, error) {
	f.gotVerifier = verifier
	if code != "good" {
		return gh.Identity{}, errors.New("bad code")
	}
	return f.identity, f.err
}

type fakeAuthorizer struct {
	admitted map[string]bool
	admins   map[string]bool
	err      error
}

func (f *fakeAuthorizer) Admitted(_ context.Context, login string) (bool, error) {
	return f.admitted[login], f.err
}

func (f *fakeAuthorizer) IsAdmin(_ context.Context, login string) (bool, error) {
	return f.admins[login], f.err
}

type ghFixture struct {
	signIn *GitHubSignIn
	store  *auth.MockStore
	client *fakeGitHubClient
	authz  *fakeAuthorizer
	mgr    *auth.Manager
}

func newGHFixture(t *testing.T) *ghFixture {
	t.Helper()
	store := auth.NewMockStore()
	mgr, err := auth.NewManager(store, base64.StdEncoding.EncodeToString([]byte("test-secret-must-be-at-least-32-bytes-long!")), "kubetty-test", 15*time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeGitHubClient{identity: gh.Identity{ID: 42, Login: "octo"}}
	authz := &fakeAuthorizer{admitted: map[string]bool{"octo": true}, admins: map[string]bool{}}
	mgr.SetAdmission(GitHubAdmission(authz, "breakglass", map[string]string{"kubetty-smoke": "test-project"}))
	return &ghFixture{
		signIn: &GitHubSignIn{
			Config:      &mockAuthConfig{authMode: AuthModeGitHub, cookieSecure: true},
			Manager:     mgr,
			Store:       store,
			Client:      client,
			Authorizer:  authz,
			CallbackURL: "https://kubetty.example/api/auth/github/callback",
			Reserved:    map[string]bool{"breakglass": true, "kubetty-smoke": true},
		},
		store: store, client: client, authz: authz, mgr: mgr,
	}
}

// signIn runs the login redirect and the callback, returning the callback response.
func (f *ghFixture) run(t *testing.T, code string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	f.signIn.LoginHandler()(rec, httptest.NewRequest(http.MethodGet, "/api/auth/github/login", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("login: %d", rec.Code)
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	var stateCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == githubStateCookie {
			stateCookie = c
		}
	}
	if stateCookie == nil || !stateCookie.HttpOnly || !stateCookie.Secure {
		t.Fatalf("state cookie missing or not HttpOnly+Secure: %+v", stateCookie)
	}
	cb := httptest.NewRequest(http.MethodGet, "/api/auth/github/callback?code="+code+"&state="+url.QueryEscape(loc.Query().Get("state")), nil)
	cb.AddCookie(stateCookie)
	out := httptest.NewRecorder()
	f.signIn.CallbackHandler()(out, cb)
	return out
}

func redirectReason(rec *httptest.ResponseRecorder) string {
	loc := rec.Header().Get("Location")
	if loc == "/" {
		return "ok"
	}
	u, _ := url.Parse(loc)
	return u.Query().Get("auth_error")
}

func accessCookie(rec *httptest.ResponseRecorder) string {
	for _, c := range rec.Result().Cookies() {
		if c.Name == AccessTokenCookieName {
			return c.Value
		}
	}
	return ""
}

func TestGitHubSignInCreatesAccountAndPKCEMatches(t *testing.T) {
	f := newGHFixture(t)
	rec := f.run(t, "good")
	if redirectReason(rec) != "ok" {
		t.Fatalf("redirect %q", rec.Header().Get("Location"))
	}
	sum := sha256.Sum256([]byte(f.client.gotVerifier))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != f.client.gotChallenge {
		t.Fatal("the code verifier does not match the challenge sent to GitHub")
	}
	u, err := f.store.GetUserByGitHubID(context.Background(), 42)
	if err != nil || u.Username != "octo" || u.GitHubLogin != "octo" {
		t.Fatalf("account: %+v %v", u, err)
	}
	claims, err := f.mgr.ValidateAccessToken(accessCookie(rec))
	if err != nil || claims.Role != auth.RoleUser || claims.GitHubLogin != "octo" {
		t.Fatalf("claims: %+v %v", claims, err)
	}
}

func TestGitHubSignInAdminRole(t *testing.T) {
	f := newGHFixture(t)
	f.authz.admins["octo"] = true
	rec := f.run(t, "good")
	claims, err := f.mgr.ValidateAccessToken(accessCookie(rec))
	if err != nil || claims.Role != auth.RoleAdmin {
		t.Fatalf("claims: %+v %v", claims, err)
	}
}

func TestGitHubSignInRefusals(t *testing.T) {
	t.Run("not allowed", func(t *testing.T) {
		f := newGHFixture(t)
		f.authz.admitted = map[string]bool{}
		if r := redirectReason(f.run(t, "good")); r != "not_allowed" {
			t.Fatalf("got %q", r)
		}
		if _, err := f.store.GetUserByGitHubID(context.Background(), 42); err == nil {
			t.Fatal("no account may be created for a refused user")
		}
	})
	t.Run("bad code", func(t *testing.T) {
		f := newGHFixture(t)
		if r := redirectReason(f.run(t, "bad")); r != "github_failed" {
			t.Fatalf("got %q", r)
		}
	})
	t.Run("github outage", func(t *testing.T) {
		f := newGHFixture(t)
		f.authz.err = errors.New("timeout")
		if r := redirectReason(f.run(t, "good")); r != "github_failed" {
			t.Fatalf("got %q", r)
		}
	})
	t.Run("state mismatch", func(t *testing.T) {
		f := newGHFixture(t)
		cb := httptest.NewRequest(http.MethodGet, "/api/auth/github/callback?code=good&state=forged", nil)
		cb.AddCookie(&http.Cookie{Name: githubStateCookie, Value: "real.verifier"})
		rec := httptest.NewRecorder()
		f.signIn.CallbackHandler()(rec, cb)
		if r := redirectReason(rec); r != "state" {
			t.Fatalf("got %q", r)
		}
	})
	t.Run("no state cookie", func(t *testing.T) {
		f := newGHFixture(t)
		rec := httptest.NewRecorder()
		f.signIn.CallbackHandler()(rec, httptest.NewRequest(http.MethodGet, "/api/auth/github/callback?code=good&state=x", nil))
		if r := redirectReason(rec); r != "state" {
			t.Fatalf("got %q", r)
		}
	})
	t.Run("user cancelled on GitHub", func(t *testing.T) {
		f := newGHFixture(t)
		rec := httptest.NewRecorder()
		f.signIn.CallbackHandler()(rec, httptest.NewRequest(http.MethodGet, "/api/auth/github/callback?error=access_denied", nil))
		if r := redirectReason(rec); r != "github_denied" {
			t.Fatalf("got %q", r)
		}
	})
}

func TestGitHubSignInLinksExistingLocalAccount(t *testing.T) {
	f := newGHFixture(t)
	local := &auth.User{ID: uuid.New(), Username: "octo", PasswordHash: []byte("$2a$10$x"), IsActive: true}
	f.store.AddUser(local)
	if r := redirectReason(f.run(t, "good")); r != "ok" {
		t.Fatalf("got %q", r)
	}
	u, err := f.store.GetUserByGitHubID(context.Background(), 42)
	if err != nil || u.ID != local.ID {
		t.Fatalf("the existing account (and its tabs) must be reused: %+v %v", u, err)
	}
	if string(u.PasswordHash) != string(githubPasswordHash) {
		t.Fatal("a linked account must lose its password")
	}
}

func TestGitHubSignInNeverTakesReservedAccounts(t *testing.T) {
	for _, name := range []string{"breakglass", "kubetty-smoke"} {
		t.Run(name, func(t *testing.T) {
			f := newGHFixture(t)
			f.client.identity = gh.Identity{ID: 7, Login: name}
			f.authz.admitted[name] = true
			local := &auth.User{ID: uuid.New(), Username: name, PasswordHash: []byte("$2a$10$x"), IsActive: true}
			f.store.AddUser(local)
			if r := redirectReason(f.run(t, "good")); r != "ok" {
				t.Fatalf("got %q", r)
			}
			u, err := f.store.GetUserByGitHubID(context.Background(), 7)
			if err != nil || u.ID == local.ID || !strings.HasPrefix(u.Username, name+"-gh") {
				t.Fatalf("reserved account must stay local: %+v %v", u, err)
			}
			if string(local.PasswordHash) != "$2a$10$x" {
				t.Fatal("the reserved account's password must not change")
			}
		})
	}
}

func TestGitHubSignInUpdatesRenamedLogin(t *testing.T) {
	f := newGHFixture(t)
	f.store.AddUser(&auth.User{ID: uuid.New(), Username: "old-name", IsActive: true, GitHubID: 42, GitHubLogin: "old-name"})
	if r := redirectReason(f.run(t, "good")); r != "ok" {
		t.Fatalf("got %q", r)
	}
	u, _ := f.store.GetUserByGitHubID(context.Background(), 42)
	if u.GitHubLogin != "octo" || u.Username != "old-name" {
		t.Fatalf("got %+v", u)
	}
}

func TestGitHubAdmission(t *testing.T) {
	authz := &fakeAuthorizer{admitted: map[string]bool{"octo": true, "boss": true}, admins: map[string]bool{"boss": true}}
	admit := GitHubAdmission(authz, "breakglass", map[string]string{"kubetty-smoke": "test-project"})
	ctx := context.Background()
	cases := []struct {
		user *auth.User
		role string
		err  error
	}{
		{&auth.User{Username: "breakglass"}, auth.RoleAdmin, nil},
		{&auth.User{Username: "kubetty-smoke"}, auth.RoleUser, nil},
		{&auth.User{Username: "old-local"}, "", auth.ErrNotAdmitted},
		{&auth.User{Username: "octo", GitHubID: 1, GitHubLogin: "octo"}, auth.RoleUser, nil},
		{&auth.User{Username: "boss", GitHubID: 2, GitHubLogin: "boss"}, auth.RoleAdmin, nil},
		{&auth.User{Username: "gone", GitHubID: 3, GitHubLogin: "gone"}, "", auth.ErrNotAdmitted},
		{&auth.User{Username: "breakglass-gh4", GitHubID: 4, GitHubLogin: "breakglass"}, "", auth.ErrNotAdmitted},
	}
	for _, tc := range cases {
		role, err := admit(ctx, tc.user)
		if role != tc.role || !errors.Is(err, tc.err) {
			t.Errorf("%s: got %q %v, want %q %v", tc.user.Username, role, err, tc.role, tc.err)
		}
	}
	authz.err = errors.New("github down")
	if _, err := admit(ctx, &auth.User{Username: "octo", GitHubID: 1, GitHubLogin: "octo"}); err == nil || errors.Is(err, auth.ErrNotAdmitted) {
		t.Fatalf("an outage must be an error, not a refusal: %v", err)
	}
}
