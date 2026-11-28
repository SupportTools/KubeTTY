package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/supporttools/KubeTTY/server/internal/auth"
)

func TestParseRestrictedUsers(t *testing.T) {
	got, err := ParseRestrictedUsers(" kubetty-smoke : test-project ,")
	if err != nil || len(got) != 1 || got["kubetty-smoke"] != "test-project" {
		t.Fatalf("got %v %v", got, err)
	}
	if got, err := ParseRestrictedUsers(""); err != nil || len(got) != 0 {
		t.Fatalf("empty spec must mean no restricted users: %v %v", got, err)
	}
	for _, bad := range []string{"kubetty-smoke", "kubetty-smoke:", ":test-project", "bad name:test-project", "a:p,a:q"} {
		if _, err := ParseRestrictedUsers(bad); err == nil {
			t.Errorf("spec %q must be rejected", bad)
		}
	}
}

func serveWithUser(mw func(http.Handler) http.Handler, r *http.Request, user *User) (*httptest.ResponseRecorder, *User) {
	var seen *User
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = UserFromContext(r.Context()) })
	if user != nil {
		r = r.WithContext(ContextWithUser(r.Context(), user))
	}
	rec := httptest.NewRecorder()
	mw(inner).ServeHTTP(rec, r)
	return rec, seen
}

func TestRestrictUsers(t *testing.T) {
	mw := RestrictUsers(map[string]string{"kubetty-smoke": "test-project"})
	smoke := &User{ID: uuid.New(), Username: "kubetty-smoke", Role: auth.RoleAdmin}

	rec, u := serveWithUser(mw, httptest.NewRequest(http.MethodGet, "/api/tabs", nil), smoke)
	if rec.Code != http.StatusOK || u == nil || !u.Restricted || u.AllowedProject != "test-project" || u.Role != auth.RoleUser || u.IsAdmin() {
		t.Fatalf("got %d %+v", rec.Code, u)
	}
	if smoke.Restricted {
		t.Fatal("the caller's user must not be modified")
	}
	if rec, _ := serveWithUser(mw, httptest.NewRequest(http.MethodGet, "/api/admin/projects", nil), smoke); rec.Code != http.StatusForbidden {
		t.Fatalf("admin API for the smoke user: %d", rec.Code)
	}
	if rec, _ := serveWithUser(mw, httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil), smoke); rec.Code != http.StatusOK {
		t.Fatalf("logout for the smoke user: %d", rec.Code)
	}
	gh := &User{Username: "kubetty-smoke", GitHubLogin: "kubetty-smoke", Role: auth.RoleUser}
	if rec, u := serveWithUser(mw, httptest.NewRequest(http.MethodGet, "/api/admin/projects", nil), gh); rec.Code != http.StatusOK || u.Restricted {
		t.Fatalf("a GitHub account is never a restricted local user: %d %+v", rec.Code, u)
	}
	if rec, u := serveWithUser(mw, httptest.NewRequest(http.MethodGet, "/api/admin/projects", nil), nil); rec.Code != http.StatusOK || u != nil {
		t.Fatalf("auth disabled: %d %+v", rec.Code, u)
	}
}

func TestRequireAdmin(t *testing.T) {
	on := RequireAdmin(true)
	for name, tc := range map[string]struct {
		user *User
		want int
	}{
		"admin":      {&User{Role: auth.RoleAdmin}, http.StatusOK},
		"user":       {&User{Role: auth.RoleUser}, http.StatusForbidden},
		"no role":    {&User{}, http.StatusForbidden},
		"restricted": {&User{Role: auth.RoleAdmin, Restricted: true}, http.StatusForbidden},
		"nobody":     {nil, http.StatusForbidden},
	} {
		if rec, _ := serveWithUser(on, httptest.NewRequest(http.MethodGet, "/api/admin/projects", nil), tc.user); rec.Code != tc.want {
			t.Errorf("%s: got %d want %d", name, rec.Code, tc.want)
		}
	}
	off := RequireAdmin(false)
	if rec, _ := serveWithUser(off, httptest.NewRequest(http.MethodGet, "/api/admin/projects", nil), nil); rec.Code != http.StatusOK {
		t.Fatalf("auth disabled: %d", rec.Code)
	}
}

func TestCanUseProject(t *testing.T) {
	writers := map[string]bool{"SupportTools/KubeTTY/octo": true}
	check := func(_ context.Context, repo, login string) (bool, error) { return writers[repo+"/"+login], nil }
	ctx := context.Background()
	cases := []struct {
		name    string
		user    *User
		project string
		repo    string
		want    bool
	}{
		{"nobody", nil, "kubetty", "SupportTools/KubeTTY", false},
		{"admin, no repo", &User{Role: auth.RoleAdmin}, "kubetty", "", true},
		{"writer", &User{Role: auth.RoleUser, GitHubLogin: "octo"}, "kubetty", "SupportTools/KubeTTY", true},
		{"reader", &User{Role: auth.RoleUser, GitHubLogin: "reader"}, "kubetty", "SupportTools/KubeTTY", false},
		{"no repo means admins only", &User{Role: auth.RoleUser, GitHubLogin: "octo"}, "kubetty", "", false},
		{"local non-admin", &User{Role: auth.RoleUser}, "kubetty", "SupportTools/KubeTTY", false},
		{"restricted, own project", &User{Restricted: true, AllowedProject: "test-project"}, "test-project", "", true},
		{"restricted, other project", &User{Restricted: true, AllowedProject: "test-project", Role: auth.RoleAdmin}, "kubetty", "", false},
	}
	for _, tc := range cases {
		if got, err := CanUseProject(ctx, tc.user, tc.project, tc.repo, check); err != nil || got != tc.want {
			t.Errorf("%s: got %v %v want %v", tc.name, got, err, tc.want)
		}
	}
	boom := func(context.Context, string, string) (bool, error) { return false, errors.New("github down") }
	if ok, err := CanUseProject(ctx, &User{Role: auth.RoleUser, GitHubLogin: "octo"}, "kubetty", "SupportTools/KubeTTY", boom); ok || err == nil {
		t.Fatalf("an outage must deny and report: %v %v", ok, err)
	}
}
