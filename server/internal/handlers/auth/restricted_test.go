package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestParseRestrictedUsers(t *testing.T) {
	got, err := ParseRestrictedUsers(" kubetty-smoke : test-project ,")
	if err != nil || len(got) != 1 || got["kubetty-smoke"] != "test-project" {
		t.Fatalf("got %v %v", got, err)
	}
	if got, err := ParseRestrictedUsers(""); err != nil || len(got) != 0 {
		t.Fatalf("empty spec must mean no restricted users: %v %v", got, err)
	}
	for _, bad := range []string{
		"kubetty-smoke",
		"kubetty-smoke:",
		":test-project",
		"bad name:test-project",
		"a:p,a:q",
	} {
		if _, err := ParseRestrictedUsers(bad); err == nil {
			t.Errorf("spec %q must be rejected", bad)
		}
	}
}

func serveRestricted(t *testing.T, r *http.Request, user *User) (*httptest.ResponseRecorder, *User) {
	t.Helper()
	var seen *User
	h := RestrictUsers(map[string]string{"kubetty-smoke": "test-project"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = UserFromContext(r.Context())
	}))
	if user != nil {
		r = r.WithContext(ContextWithUser(r.Context(), user))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec, seen
}

func TestRestrictUsersMarksListedUser(t *testing.T) {
	smoke := &User{ID: uuid.New(), Username: "kubetty-smoke"}
	rec, u := serveRestricted(t, httptest.NewRequest(http.MethodGet, "/api/tabs", nil), smoke)
	if rec.Code != http.StatusOK || u == nil || !u.Restricted || u.AllowedProject != "test-project" || u.ID != smoke.ID {
		t.Fatalf("got %d %+v", rec.Code, u)
	}
	if smoke.Restricted {
		t.Fatal("the caller's user must not be modified")
	}

	rec, u = serveRestricted(t, httptest.NewRequest(http.MethodGet, "/api/admin/projects", nil), &User{Username: "mmattox"})
	if rec.Code != http.StatusOK || u == nil || u.Restricted {
		t.Fatalf("unlisted user: %d %+v", rec.Code, u)
	}

	rec, u = serveRestricted(t, httptest.NewRequest(http.MethodGet, "/api/admin/projects", nil), nil)
	if rec.Code != http.StatusOK || u != nil {
		t.Fatalf("no user (auth disabled): %d %+v", rec.Code, u)
	}
}

func TestRestrictUsersWithoutListPassesThrough(t *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	if got := RestrictUsers(nil)(next); got == nil {
		t.Fatal("handler must be returned")
	}
}

func TestRestrictedRoutes(t *testing.T) {
	cases := []struct {
		method, target string
		want           int
	}{
		{http.MethodGet, "/api/auth/me", http.StatusOK},
		{http.MethodPost, "/api/auth/logout", http.StatusOK},
		{http.MethodGet, "/api/tabs", http.StatusOK},
		{http.MethodPost, "/api/tabs", http.StatusOK},
		{http.MethodDelete, "/api/tabs/3f1a6d68-56f4-40c4-ae03-5268c9997184", http.StatusOK},
		{http.MethodGet, "/ws?tab=abc", http.StatusOK},
		{http.MethodGet, "/ws?tab=abc&force=true", http.StatusForbidden},
		{http.MethodPost, "/api/auth/password", http.StatusForbidden},
		{http.MethodGet, "/vnc?tab=abc", http.StatusForbidden},
		{http.MethodGet, "/api/projects", http.StatusForbidden},
		{http.MethodGet, "/api/admin/projects", http.StatusForbidden},
		{http.MethodPost, "/api/admin/projects", http.StatusForbidden},
		{http.MethodDelete, "/api/admin/projects/test-project", http.StatusForbidden},
		{http.MethodGet, "/api/admin/projects/test-project/secrets", http.StatusForbidden},
		{http.MethodPut, "/api/admin/settings/x/y", http.StatusForbidden},
		{http.MethodGet, "/api/admin/dashboard/summary", http.StatusForbidden},
		{http.MethodGet, "/api/tabs/events", http.StatusForbidden},
		{http.MethodPost, "/api/tabs/reorder", http.StatusForbidden},
		{http.MethodDelete, "/api/tabs/reorder", http.StatusForbidden},
		{http.MethodGet, "/api/tabs/abc/health", http.StatusForbidden},
		{http.MethodDelete, "/api/tabs/", http.StatusForbidden},
		{http.MethodPost, "/api/auth/me", http.StatusForbidden},
		{http.MethodGet, "/session/logs", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.target, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.target, nil)
			if rec, _ := serveRestricted(t, r, &User{Username: "kubetty-smoke"}); rec.Code != tc.want {
				t.Fatalf("got %d want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestRestrictedTabRequestAllowed(t *testing.T) {
	smoke := &User{Username: "kubetty-smoke", Restricted: true, AllowedProject: "test-project"}
	if !RestrictedTabRequestAllowed(smoke, "test-project", false, "") || !RestrictedTabRequestAllowed(smoke, "test-project", false, "create_new") {
		t.Fatal("a new terminal tab on the allowed project must be allowed")
	}
	for name, ok := range map[string]bool{
		"other project":   RestrictedTabRequestAllowed(smoke, "kubetty", false, ""),
		"gui tab":         RestrictedTabRequestAllowed(smoke, "test-project", true, ""),
		"attach_specific": RestrictedTabRequestAllowed(smoke, "test-project", false, "attach_specific"),
		"attach_recent":   RestrictedTabRequestAllowed(smoke, "test-project", false, "attach_recent"),
		"no project set":  RestrictedTabRequestAllowed(&User{Restricted: true}, "", false, ""),
	} {
		if ok {
			t.Errorf("%s must be refused", name)
		}
	}
	if !RestrictedTabRequestAllowed(&User{Username: "mmattox"}, "anything", true, "attach_specific") || !RestrictedTabRequestAllowed(nil, "x", false, "") {
		t.Fatal("unrestricted users are not limited by this check")
	}
}
