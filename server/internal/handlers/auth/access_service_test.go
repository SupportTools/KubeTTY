package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/supporttools/KubeTTY/server/internal/auth/cfaccess"
)

// cluster-services-1534: the KubeTTY CI smoke uses a Cloudflare Access service
// token on dev only. It maps to a dedicated restricted user, never a person.

const smokeClientID = "74db6234cffb8c621b7e6616192f810d.access"

func (f *accessFixture) serviceToken(t *testing.T, clientID string) string {
	t.Helper()
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"type": "app", "iss": f.issuer, "aud": []string{accessTestAUD}, "sub": "",
		"common_name": clientID, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = "k1"
	s, err := tok.SignedString(f.key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newServiceFixture(t *testing.T) *accessFixture {
	t.Helper()
	f := newAccessFixture(t)
	services, err := ParseAccessServiceTokens(smokeClientID+"=kubetty-smoke:test-project", map[string]string{"mmattox@mattoxengineering.com": "mmattox"})
	if err != nil {
		t.Fatal(err)
	}
	f.mapper.WithServiceTokens(services)
	return f
}

func TestParseAccessServiceTokens(t *testing.T) {
	humans := map[string]string{"mmattox@mattoxengineering.com": "mmattox"}
	got, err := ParseAccessServiceTokens(" "+smokeClientID+" = kubetty-smoke : test-project ,", humans)
	if err != nil || got[smokeClientID] != (ServicePrincipal{Username: "kubetty-smoke", Project: "test-project"}) || len(got) != 1 {
		t.Fatalf("got %v %v", got, err)
	}
	if got, err := ParseAccessServiceTokens("", humans); err != nil || len(got) != 0 {
		t.Fatalf("empty spec must mean no tokens: %v %v", got, err)
	}
	for _, bad := range []string{
		"abc=kubetty-smoke:test-project",                 // not a client id
		".access=kubetty-smoke:test-project",             // empty client id
		smokeClientID + "=kubetty-smoke",                 // no project
		smokeClientID + "=:test-project",                 // no username
		smokeClientID + "=bad name:test-project",         // invalid username
		smokeClientID + "=MMattox:test-project",          // a human account
		smokeClientID + "=a:p," + smokeClientID + "=b:p", // duplicate client id
		smokeClientID + "=a:p,ffff.access=a:p",           // two tokens, one user
	} {
		if _, err := ParseAccessServiceTokens(bad, humans); err == nil {
			t.Errorf("spec %q must be rejected", bad)
		}
	}
}

func TestServiceTokenMapsToRestrictedUser(t *testing.T) {
	f := newServiceFixture(t)

	r := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	r.Header.Set(cfaccess.HeaderName, f.serviceToken(t, smokeClientID))
	rec, u := f.serve(r)
	if rec.Code != http.StatusOK || u == nil {
		t.Fatalf("got %d user=%v body=%s", rec.Code, u, rec.Body.String())
	}
	if u.Username != "kubetty-smoke" || !u.Restricted || u.AllowedProject != "test-project" || u.ID == f.existing.ID {
		t.Fatalf("service token must map to the restricted smoke user, got %+v", u)
	}
	created, err := f.store.GetUserByUsername(r.Context(), "kubetty-smoke")
	if err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword(created.PasswordHash, []byte("")) == nil {
		t.Fatal("the smoke account must not accept any password")
	}

	// Second request (cached) is still restricted.
	r2 := httptest.NewRequest(http.MethodGet, "/api/tabs", nil)
	r2.Header.Set(cfaccess.HeaderName, f.serviceToken(t, smokeClientID))
	if rec, u := f.serve(r2); rec.Code != http.StatusOK || u == nil || !u.Restricted {
		t.Fatalf("cached lookup: %d %+v", rec.Code, u)
	}

	// A human's token is never restricted.
	r3 := httptest.NewRequest(http.MethodGet, "/api/admin/projects", nil)
	r3.Header.Set(cfaccess.HeaderName, f.token(t, "mmattox@mattoxengineering.com", accessTestAUD))
	if rec, u := f.serve(r3); rec.Code != http.StatusOK || u == nil || u.Restricted {
		t.Fatalf("human: %d %+v", rec.Code, u)
	}
}

func TestUnknownServiceTokenIs403(t *testing.T) {
	f := newServiceFixture(t)
	r := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	r.Header.Set(cfaccess.HeaderName, f.serviceToken(t, "ffffffffffffffffffffffffffffffff.access"))
	if rec, u := f.serve(r); rec.Code != http.StatusForbidden || u != nil {
		t.Fatalf("got %d user=%v", rec.Code, u)
	}

	// Without WithServiceTokens (prod) every service token is refused.
	g := newAccessFixture(t)
	r = httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	r.Header.Set(cfaccess.HeaderName, g.serviceToken(t, smokeClientID))
	if rec, u := g.serve(r); rec.Code != http.StatusForbidden || u != nil {
		t.Fatalf("no service tokens configured: got %d user=%v", rec.Code, u)
	}
}

func TestRestrictedRoutes(t *testing.T) {
	f := newServiceFixture(t)
	cases := []struct {
		method, target string
		want           int
	}{
		{http.MethodGet, "/api/auth/me", http.StatusOK},
		{http.MethodGet, "/api/auth/refresh", http.StatusOK},
		{http.MethodGet, "/api/tabs", http.StatusOK},
		{http.MethodPost, "/api/tabs", http.StatusOK},
		{http.MethodDelete, "/api/tabs/3f1a6d68-56f4-40c4-ae03-5268c9997184", http.StatusOK},
		{http.MethodGet, "/ws?tab=abc", http.StatusOK},
		// Everything else is refused before any handler runs.
		{http.MethodGet, "/ws?tab=abc&force=true", http.StatusForbidden},
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
			r.Header.Set(cfaccess.HeaderName, f.serviceToken(t, smokeClientID))
			if rec, _ := f.serve(r); rec.Code != tc.want {
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
