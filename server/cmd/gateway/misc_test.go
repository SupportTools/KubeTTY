package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supporttools/KubeTTY/server/internal/auth"
	"github.com/supporttools/KubeTTY/server/internal/config"
	gatewayconfig "github.com/supporttools/KubeTTY/server/internal/gateway/config"
	handlers_auth "github.com/supporttools/KubeTTY/server/internal/handlers/auth"
	"github.com/supporttools/KubeTTY/server/internal/projects"
)

func TestCloneURL(t *testing.T) {
	t.Run("nil returns root", func(t *testing.T) {
		got := cloneURL(nil)
		require.NotNil(t, got)
		assert.Equal(t, "/", got.Path)
	})

	t.Run("copy is independent", func(t *testing.T) {
		orig := &url.URL{Scheme: "http", Host: "h", Path: "/a", RawQuery: "x=1"}
		clone := cloneURL(orig)
		assert.Equal(t, orig.String(), clone.String())
		clone.Path = "/changed"
		clone.RawQuery = ""
		assert.Equal(t, "/a", orig.Path)
		assert.Equal(t, "x=1", orig.RawQuery)
	})
}

func TestAuthEnabled(t *testing.T) {
	mgr, err := auth.NewManager(auth.NewMockStore(), testJWTSecret, "i", time.Minute, time.Hour)
	require.NoError(t, err)

	local := config.GatewayConfig{AuthMode: "local"}
	disabled := config.GatewayConfig{AuthMode: "disabled"}

	tests := []struct {
		name string
		srv  *server
		want bool
	}{
		{"nil server", nil, false},
		{"disabled without manager", &server{cfg: disabled}, false},
		{"disabled with manager", &server{cfg: disabled, authMgr: mgr}, false},
		{"local without manager", &server{cfg: local}, false},
		{"local with manager", &server{cfg: local, authMgr: mgr}, true},
		{"empty mode", &server{authMgr: mgr}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.srv.authEnabled())
		})
	}
}

func TestTabOwnerID(t *testing.T) {
	mgr, err := auth.NewManager(auth.NewMockStore(), testJWTSecret, "i", time.Minute, time.Hour)
	require.NoError(t, err)
	userID := uuid.New()
	withUser := func(r *http.Request) *http.Request {
		return r.WithContext(handlers_auth.ContextWithUser(r.Context(), &handlers_auth.User{ID: userID, Username: "u"}))
	}
	withCookie := func(r *http.Request) *http.Request {
		r.AddCookie(&http.Cookie{Name: clientCookieName, Value: "abc"})
		return r
	}

	authSrv := &server{cfg: config.GatewayConfig{AuthMode: "local"}, authMgr: mgr}
	noAuthSrv := &server{cfg: config.GatewayConfig{AuthMode: "disabled"}}

	tests := []struct {
		name       string
		srv        *server
		prep       func(*http.Request) *http.Request
		want       string
		wantCookie bool
	}{
		{"auth user is user-scoped", authSrv, withUser, "user:" + userID.String(), false},
		{"auth user wins over cookie", authSrv, func(r *http.Request) *http.Request { return withUser(withCookie(r)) }, "user:" + userID.String(), false},
		{"auth enabled but no user falls back to cookie", authSrv, withCookie, "client:abc", false},
		{"no auth uses existing cookie", noAuthSrv, withCookie, "client:abc", false},
		{"no auth ignores context user", noAuthSrv, func(r *http.Request) *http.Request { return withUser(withCookie(r)) }, "client:abc", false},
		{"no auth without cookie issues one", noAuthSrv, func(r *http.Request) *http.Request { return r }, "", true},
		{"empty cookie value issues a new one", noAuthSrv, func(r *http.Request) *http.Request {
			r.AddCookie(&http.Cookie{Name: clientCookieName, Value: ""})
			return r
		}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := tt.prep(httptest.NewRequest(http.MethodGet, "/", nil))
			got := tt.srv.tabOwnerID(rec, req)
			cookies := rec.Result().Cookies()
			if !tt.wantCookie {
				assert.Equal(t, tt.want, got)
				assert.Empty(t, cookies)
				return
			}
			require.Len(t, cookies, 1)
			c := cookies[0]
			assert.Equal(t, "client:"+c.Value, got)
			_, err := uuid.Parse(c.Value)
			assert.NoError(t, err, "client id should be a UUID")
			assert.Equal(t, "/", c.Path)
			assert.True(t, c.HttpOnly)
			assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
			assert.Equal(t, 365*24*60*60, c.MaxAge)
			assert.False(t, c.Secure, "plain HTTP request")
		})
	}

	t.Run("cookie is Secure over TLS", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "https://example/", nil)
		req.TLS = &tls.ConnectionState{}
		noAuthSrv.ensureClientID(rec, req)
		require.Len(t, rec.Result().Cookies(), 1)
		assert.True(t, rec.Result().Cookies()[0].Secure)
	})
}

func TestHandleVersion(t *testing.T) {
	origVersion, origCommit, origBuild := version, gitCommit, buildTime
	t.Cleanup(func() { version, gitCommit, buildTime = origVersion, origCommit, origBuild })
	version, gitCommit, buildTime = "v1.2.3", "abc123", "2026-01-01T00:00:00Z"

	rec := httptest.NewRecorder()
	handleVersion(rec, httptest.NewRequest(http.MethodGet, "/api/version", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"version":"v1.2.3","gitCommit":"abc123","buildTime":"2026-01-01T00:00:00Z"}`, rec.Body.String())
}

func TestStaticHandler(t *testing.T) {
	env := newTestServer(t, testServerOpts{})
	h := env.srv.staticHandler()

	tests := []struct {
		name     string
		path     string
		wantCode int
		contains string
	}{
		{"root", "/", http.StatusOK, "<title>KubeTTY</title>"},
		{"asset", "/assets/app.js", http.StatusOK, "console.info"},
		{"spa route", "/tabs/123", http.StatusOK, "<title>KubeTTY</title>"},
		{"missing asset falls back to index", "/assets/missing.js", http.StatusOK, "<title>KubeTTY</title>"},
		// FileServer redirects explicit index.html requests to the directory.
		{"index.html redirects", "/index.html", http.StatusMovedPermanently, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := serve(h, req)
			assert.Equal(t, tt.wantCode, rec.Code)
			if tt.contains != "" {
				assert.Contains(t, rec.Body.String(), tt.contains)
			}
			// The fallback must not mutate the caller's request URL.
			assert.Equal(t, tt.path, req.URL.Path)
		})
	}
}

func TestObserveStore_NilSafe(t *testing.T) {
	var nilSrv *server
	assert.NotPanics(t, func() { nilSrv.observeStore("op", time.Now(), nil) })
	assert.NotPanics(t, func() { (&server{}).ObserveStore("op", time.Now(), errors.New("x")) })
	assert.NotPanics(t, func() {
		(&server{appMetrics: sharedAppMetrics()}).ObserveStore("op", time.Now(), nil)
	})
}

type projectListResponse struct {
	Projects []ProjectResponse `json:"projects"`
}

func TestHandleListProjects_TabManagerFallback(t *testing.T) {
	env := newTestServer(t, testServerOpts{projects: []gatewayconfig.Project{testProject("alpha"), testProject("beta")}})

	rec := serve(http.HandlerFunc(env.srv.handleListProjects), httptest.NewRequest(http.MethodGet, "/api/projects", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Projects []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"projects"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	ids := []string{}
	for _, p := range body.Projects {
		ids = append(ids, p.ID)
		assert.Equal(t, "unknown", p.Status)
	}
	assert.ElementsMatch(t, []string{"alpha", "beta"}, ids)
}

var projectColumns = []string{
	"id", "name", "display_name", "description", "icon",
	"target_namespace", "service_name", "session_id", "user_name",
	"cpu_request", "cpu_limit", "memory_request", "memory_limit",
	"storage_size", "storage_class",
	"admin_namespaces", "read_namespaces",
	"max_tabs_per_client", "max_tabs_total", "session_mode",
	"dind_enabled", "gui_enabled", "gui_resolution", "gui_vnc_port",
	"env_vars",
	"image_repository", "image_tag",
	"status", "status_message", "last_health_check", "last_activity", "pod_ip", "paused",
	"created_at", "updated_at", "deleted_at",
}

type projectRow struct {
	name, serviceName, cpuLimit, memLimit string
	status                                projects.ProjectStatus
	paused, gui                           bool
	vncPort                               *int
	maxPerClient, maxTotal                int
}

func (p projectRow) values() []any {
	strp := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	now := time.Now()
	return []any{
		uuid.New(), p.name, "Display " + p.name, strp("desc " + p.name), strp("icon"),
		"target-ns", strp(p.serviceName), uuid.New(), "owner",
		"100m", p.cpuLimit, "128Mi", p.memLimit,
		"10Gi", "standard",
		[]byte(`[]`), []byte(`[]`),
		p.maxPerClient, p.maxTotal, projects.SessionMode("shared"),
		false, p.gui, (*string)(nil), p.vncPort,
		[]byte(`{}`),
		"repo", "tag",
		p.status, (*string)(nil), (*time.Time)(nil), (*time.Time)(nil), (*string)(nil), p.paused,
		now, now, (*time.Time)(nil),
	}
}

func TestHandleListProjects_FromDatabase(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	vnc := 5902
	rows := pgxmock.NewRows(projectColumns).
		AddRow(projectRow{name: "alpha", serviceName: "svc-alpha", cpuLimit: "1500m", memLimit: "1Gi", status: projects.StatusRunning, maxPerClient: 3, maxTotal: 10}.values()...).
		AddRow(projectRow{name: "beta", cpuLimit: "bogus", memLimit: "", status: projects.StatusPending, paused: true, gui: true, vncPort: &vnc}.values()...)
	mock.ExpectQuery(`SELECT id, name, display_name`).WillReturnRows(rows)

	env := newTestServer(t, testServerOpts{projects: []gatewayconfig.Project{testProject("alpha")}})
	env.srv.projectStore = projects.NewStoreWithPool(mock, "target-ns")

	rec := serve(http.HandlerFunc(env.srv.handleListProjects), httptest.NewRequest(http.MethodGet, "/api/projects", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, mock.ExpectationsWereMet())

	var body projectListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Projects, 2)

	alpha, beta := body.Projects[0], body.Projects[1]

	assert.Equal(t, "alpha", alpha.ID)
	assert.Equal(t, "Display alpha", alpha.DisplayName)
	assert.Equal(t, "target-ns", alpha.Namespace)
	assert.Equal(t, "svc-alpha", alpha.Service)
	assert.Equal(t, 8080, alpha.Port)
	assert.Equal(t, "running", alpha.LifecycleStatus)
	assert.Equal(t, int64(1500), alpha.Limits.CPUMillicores)
	assert.Equal(t, int64(1<<30), alpha.Limits.MemoryBytes)
	assert.Equal(t, 3, alpha.Limits.MaxTabsPerClient)
	assert.Equal(t, 10, alpha.Limits.MaxTabsTotal)
	assert.Equal(t, "unknown", alpha.HealthStatus, "registered with tabManager -> health attached")
	assert.Equal(t, "shared", alpha.SessionMode)

	assert.Equal(t, "beta", beta.ID)
	assert.Equal(t, projects.ComputeServiceName("beta"), beta.Service, "missing service name is computed")
	assert.Equal(t, "pending", beta.LifecycleStatus)
	assert.True(t, beta.Paused)
	assert.Zero(t, beta.Limits.CPUMillicores, "unparseable CPU limit is ignored")
	assert.Zero(t, beta.Limits.MemoryBytes)
	assert.Empty(t, beta.HealthStatus, "not registered with tabManager -> no health")
	assert.True(t, beta.GUIEnabled)
	assert.Equal(t, 5902, beta.GUIVNCPort)
}

func TestHandleListProjects_DatabaseError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()
	mock.ExpectQuery(`SELECT id, name, display_name`).WillReturnError(errBoom)

	env := newTestServer(t, testServerOpts{})
	env.srv.projectStore = projects.NewStoreWithPool(mock, "ns")

	rec := serve(http.HandlerFunc(env.srv.handleListProjects), httptest.NewRequest(http.MethodGet, "/api/projects", nil))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "failed to list projects", decodeError(t, rec).Message)
}

func TestHandleListProjects_DatabaseWithoutTabManager(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()
	mock.ExpectQuery(`SELECT id, name, display_name`).
		WillReturnRows(pgxmock.NewRows(projectColumns).
			AddRow(projectRow{name: "alpha", status: projects.StatusRunning}.values()...))

	env := newTestServer(t, testServerOpts{noTabManager: true})
	env.srv.projectStore = projects.NewStoreWithPool(mock, "ns")

	rec := serve(http.HandlerFunc(env.srv.handleListProjects), httptest.NewRequest(http.MethodGet, "/api/projects", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body projectListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Projects, 1)
	assert.Empty(t, body.Projects[0].HealthStatus)
}

func TestRunMigrations_InvalidConnString(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// An unreachable host fails fast at ping, before touching migrations.
	err := runMigrations(ctx, "postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ping db")
}
