package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supporttools/KubeTTY/server/internal/auth"
)

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRoutes_PublicEndpoints(t *testing.T) {
	for _, authLocal := range []bool{false, true} {
		name := "auth_disabled"
		if authLocal {
			name = "auth_local"
		}
		t.Run(name, func(t *testing.T) {
			env := newTestServer(t, testServerOpts{authLocal: authLocal})
			h := env.srv.routes(nil)

			t.Run("healthz", func(t *testing.T) {
				rec := serve(h, httptest.NewRequest(http.MethodGet, "/api/healthz", nil))
				assert.Equal(t, http.StatusOK, rec.Code)
				var body map[string]any
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
				assert.Equal(t, "healthy", body["status"])
			})

			t.Run("version", func(t *testing.T) {
				rec := serve(h, httptest.NewRequest(http.MethodGet, "/api/version", nil))
				assert.Equal(t, http.StatusOK, rec.Code)
				assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
				var body map[string]string
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
				assert.Equal(t, version, body["version"])
				assert.Contains(t, body, "gitCommit")
				assert.Contains(t, body, "buildTime")
			})

			t.Run("metrics", func(t *testing.T) {
				rec := serve(h, httptest.NewRequest(http.MethodGet, "/metrics", nil))
				assert.Equal(t, http.StatusOK, rec.Code)
			})

			t.Run("leader status without elector", func(t *testing.T) {
				// Regression: a nil *LeaderElector was passed as a non-nil
				// interface and the handler panicked.
				rec := serve(h, httptest.NewRequest(http.MethodGet, "/api/healthz/leader", nil))
				assert.Equal(t, http.StatusOK, rec.Code)
				var body map[string]any
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
				assert.Equal(t, "disabled", body["status"])
				assert.Equal(t, true, body["isLeader"])
			})

			t.Run("static index", func(t *testing.T) {
				rec := serve(h, httptest.NewRequest(http.MethodGet, "/", nil))
				assert.Equal(t, http.StatusOK, rec.Code)
				assert.Contains(t, rec.Body.String(), "<title>KubeTTY</title>")
			})

			t.Run("static asset", func(t *testing.T) {
				rec := serve(h, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
				assert.Equal(t, http.StatusOK, rec.Code)
				assert.Contains(t, rec.Body.String(), "console.info")
			})

			t.Run("unknown path falls back to SPA index", func(t *testing.T) {
				rec := serve(h, httptest.NewRequest(http.MethodGet, "/projects/foo/settings", nil))
				assert.Equal(t, http.StatusOK, rec.Code)
				assert.Contains(t, rec.Body.String(), "<title>KubeTTY</title>")
			})

			t.Run("auth warning header", func(t *testing.T) {
				rec := serve(h, httptest.NewRequest(http.MethodGet, "/api/version", nil))
				if authLocal {
					assert.Empty(t, rec.Header().Get("X-Auth-Warning"))
				} else {
					assert.Equal(t, "Authentication is disabled", rec.Header().Get("X-Auth-Warning"))
				}
			})
		})
	}
}

// protectedRoutes lists every route that main() wraps with requireAuth when
// AUTH_MODE=local.
var protectedRoutes = []struct {
	method string
	path   string
}{
	{http.MethodGet, "/api/projects"},
	{http.MethodGet, "/api/tabs"},
	{http.MethodPost, "/api/tabs"},
	{http.MethodPut, "/api/tabs/reorder"},
	{http.MethodGet, "/api/tabs/events"},
	{http.MethodDelete, "/api/tabs/some-tab"},
	{http.MethodGet, "/api/tabs/some-tab/health"},
	{http.MethodGet, "/ws?tab=x"},
	{http.MethodGet, "/vnc?tab=x"},
	{http.MethodGet, "/session/logs"},
	{http.MethodGet, "/api/auth/me"},
	{http.MethodPost, "/api/auth/logout"},
	{http.MethodPost, "/api/auth/password"},
	{http.MethodGet, "/api/admin/dashboard/summary"},
	{http.MethodGet, "/api/admin/dashboard/metrics"},
	{http.MethodGet, "/api/admin/dashboard/errors"},
	{http.MethodGet, "/api/admin/dashboard/usage"},
	{http.MethodGet, "/api/admin/settings"},
	{http.MethodGet, "/api/admin/settings/categories"},
	{http.MethodGet, "/api/admin/settings/history"},
	{http.MethodGet, "/api/admin/settings/general"},
	{http.MethodGet, "/api/admin/settings/general/key"},
	{http.MethodPut, "/api/admin/settings/general/key"},
	{http.MethodPost, "/api/admin/settings"},
	{http.MethodDelete, "/api/admin/settings/general/key"},
	{http.MethodGet, "/api/admin/settings/general/key/history"},
}

func TestRoutes_AuthLocal_ProtectedRoutesRequireToken(t *testing.T) {
	env := newTestServer(t, testServerOpts{authLocal: true})
	h := env.srv.routes(nil)

	for _, rt := range protectedRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			rec := serve(h, httptest.NewRequest(rt.method, rt.path, nil))
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.Equal(t, `Bearer realm="kubetty"`, rec.Header().Get("WWW-Authenticate"))
			var body map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, "authentication required", body["error"])
		})
	}
}

func TestRoutes_AuthLocal_InvalidTokenRejected(t *testing.T) {
	env := newTestServer(t, testServerOpts{authLocal: true})
	h := env.srv.routes(nil)

	tests := []struct {
		name  string
		setup func(r *http.Request)
	}{
		{"garbage bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer not-a-jwt") }},
		{"garbage cookie", func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: accessTokenCookieName, Value: "not-a-jwt"})
		}},
		{"token signed with other secret", func(r *http.Request) {
			otherMgr, err := auth.NewManager(auth.NewMockStore(), "another-secret-key-that-is-also-32-bytes-long!", "kubetty-test", time.Minute, time.Hour)
			require.NoError(t, err)
			pair, err := otherMgr.IssueTokenPair(context.Background(), &auth.User{ID: uuid.New(), Username: "mallory"}, auth.TokenMetadata{})
			require.NoError(t, err)
			r.Header.Set("Authorization", "Bearer "+pair.AccessToken)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/tabs", nil)
			tt.setup(req)
			rec := serve(h, req)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.Empty(t, env.store.lastListClient(), "handler must not run for rejected requests")
		})
	}
}

func TestRoutes_AuthLocal_ValidTokenScopesTabsToUser(t *testing.T) {
	env := newTestServer(t, testServerOpts{authLocal: true})
	h := env.srv.routes(nil)
	token, userID := issueToken(t, env.srv)

	t.Run("bearer header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/tabs", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := serve(h, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, "user:"+userID.String(), env.store.lastListClient())
		// User-scoped owners do not need the anonymous client cookie.
		assert.Empty(t, rec.Result().Cookies())
	})

	t.Run("access cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/tabs", nil)
		req.AddCookie(&http.Cookie{Name: accessTokenCookieName, Value: token})
		rec := serve(h, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, "user:"+userID.String(), env.store.lastListClient())
	})

	t.Run("auth me", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := serve(h, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), userID.String())
	})
}

func TestRoutes_AuthLocal_LoginIsPublic(t *testing.T) {
	env := newTestServer(t, testServerOpts{authLocal: true})
	h := env.srv.routes(nil)

	rec := serve(h, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{}`)))
	// Reaches the login handler (validation error) instead of the auth middleware.
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "username and password required")
}

func TestRoutes_AuthDisabled_NoTokenNeeded(t *testing.T) {
	env := newTestServer(t, testServerOpts{})
	h := env.srv.routes(nil)

	rec := serve(h, httptest.NewRequest(http.MethodGet, "/api/tabs", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	// Anonymous clients get a long-lived, HttpOnly client cookie and are
	// scoped by it.
	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, clientCookieName, cookies[0].Name)
	assert.True(t, cookies[0].HttpOnly)
	assert.Equal(t, "client:"+cookies[0].Value, env.store.lastListClient())

	// Auth endpoints are not registered when auth is disabled: they fall
	// through to the SPA handler rather than a JSON auth handler.
	rec = serve(h, httptest.NewRequest(http.MethodGet, "/api/auth/me", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "<title>KubeTTY</title>")
}

func TestRoutes_MethodHandling(t *testing.T) {
	env := newTestServer(t, testServerOpts{})
	h := env.srv.routes(nil)

	tests := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodPatch, "/api/tabs", http.StatusMethodNotAllowed},
		{http.MethodDelete, "/api/tabs", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/tabs/reorder", http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/tabs/reorder", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/tabs/abc", http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/tabs/abc", http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/tabs/abc/health", http.StatusMethodNotAllowed},
		{http.MethodDelete, "/api/tabs/abc/health", http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rec := serve(h, httptest.NewRequest(tt.method, tt.path, nil))
			assert.Equal(t, tt.want, rec.Code, rec.Body.String())
		})
	}

	t.Run("custom 405 body is JSON", func(t *testing.T) {
		rec := serve(h, httptest.NewRequest(http.MethodPatch, "/api/tabs", nil))
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "method_not_allowed", body["error"])
	})

	// Known quirk (documented, not changed here): the "/" SPA catch-all matches
	// every method, so a wrong method on a method-pattern admin route such as
	// "GET /api/admin/dashboard/summary" is served the SPA index (200 HTML)
	// instead of the ServeMux's 405. This pins the current behavior so a
	// future fix is a deliberate test change.
	for _, rt := range []struct{ method, path string }{
		{http.MethodDelete, "/api/admin/dashboard/summary"},
		{http.MethodPost, "/api/admin/settings/categories"},
		{http.MethodPatch, "/api/admin/settings/general/key"},
	} {
		t.Run("wrong method on admin route falls through to SPA "+rt.method+" "+rt.path, func(t *testing.T) {
			rec := serve(h, httptest.NewRequest(rt.method, rt.path, nil))
			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Contains(t, rec.Body.String(), "<title>KubeTTY</title>")
		})
	}
}

func TestRoutes_GatewayDisabledReturns404(t *testing.T) {
	env := newTestServer(t, testServerOpts{noTabManager: true})
	h := env.srv.routes(nil)

	for _, rt := range []struct{ method, path string }{
		{http.MethodGet, "/api/tabs"},
		{http.MethodPut, "/api/tabs/reorder"},
		{http.MethodGet, "/api/tabs/events"},
		{http.MethodDelete, "/api/tabs/abc"},
		{http.MethodGet, "/api/tabs/abc/health"},
		{http.MethodGet, "/ws?tab=abc"},
		{http.MethodGet, "/vnc?tab=abc"},
		{http.MethodGet, "/api/projects"},
	} {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			rec := serve(h, httptest.NewRequest(rt.method, rt.path, nil))
			assert.Equal(t, http.StatusNotFound, rec.Code)
			body, _ := io.ReadAll(rec.Body)
			assert.Contains(t, string(body), "gateway disabled")
		})
	}
}
