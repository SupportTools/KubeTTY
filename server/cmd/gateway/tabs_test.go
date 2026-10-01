package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gatewayconfig "github.com/supporttools/KubeTTY/server/internal/gateway/config"
	"github.com/supporttools/KubeTTY/server/internal/gateway/tabs"
)

const testClientCookie = "test-client"
const testOwner = "client:" + testClientCookie

func tabsRequest(method, path, body string) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	req.Host = "gateway.example"
	req.AddCookie(&http.Cookie{Name: clientCookieName, Value: testClientCookie})
	return req
}

type errorBody struct {
	Status  int    `json:"status"`
	Error   string `json:"error"`
	Message string `json:"message"`
	Details string `json:"details"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var eb errorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &eb), rec.Body.String())
	return eb
}

type createTabResponse struct {
	Tab     tabs.Tab `json:"tab"`
	WSURL   string   `json:"wsUrl"`
	GUIMode bool     `json:"guiMode"`
}

func decodeCreate(t *testing.T, rec *httptest.ResponseRecorder) createTabResponse {
	t.Helper()
	var resp createTabResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp), rec.Body.String())
	return resp
}

func TestHandleTabs_CreateValidation(t *testing.T) {
	limited := testProject("limited")
	limited.Limits.MaxTabsPerClient = 1
	total := testProject("total")
	total.Limits.MaxTabsTotal = 1
	noGUI := testProject("nogui")

	tests := []struct {
		name        string
		body        string
		setup       func(env *testEnv)
		wantStatus  int
		wantMessage string
	}{
		{name: "invalid json", body: `{"projectId":`, wantStatus: http.StatusBadRequest, wantMessage: "invalid request body"},
		{name: "empty body", body: ``, wantStatus: http.StatusBadRequest, wantMessage: "invalid request body"},
		{name: "wrong type", body: `{"projectId": 42}`, wantStatus: http.StatusBadRequest, wantMessage: "invalid request body"},
		{name: "missing projectId", body: `{}`, wantStatus: http.StatusBadRequest, wantMessage: "projectId is required"},
		{name: "empty projectId", body: `{"projectId":""}`, wantStatus: http.StatusBadRequest, wantMessage: "projectId is required"},
		{name: "bad openAction", body: `{"projectId":"p","openAction":"steal"}`, wantStatus: http.StatusBadRequest, wantMessage: "openAction must be one of: create_new, attach_recent, attach_specific"},
		{name: "attach_specific without existingTabId", body: `{"projectId":"p","openAction":"attach_specific"}`, wantStatus: http.StatusBadRequest, wantMessage: "existingTabId is required for attach_specific"},
		{name: "attach_specific unknown tab", body: `{"projectId":"p","openAction":"attach_specific","existingTabId":"nope"}`, wantStatus: http.StatusNotFound, wantMessage: "tab not found"},
		{
			name: "attach_specific store error",
			body: `{"projectId":"p","openAction":"attach_specific","existingTabId":"t1"}`,
			setup: func(env *testEnv) {
				env.store.getErr = errBoom
			},
			wantStatus: http.StatusInternalServerError, wantMessage: "internal error",
		},
		{
			name: "attach_specific other owner",
			body: `{"projectId":"p","openAction":"attach_specific","existingTabId":"t1"}`,
			setup: func(env *testEnv) {
				env.store.put(tabs.Tab{TabID: "t1", ProjectID: "p", ClientID: "client:someone-else"})
			},
			wantStatus: http.StatusForbidden, wantMessage: "forbidden",
		},
		{
			name: "attach_specific project mismatch",
			body: `{"projectId":"p","openAction":"attach_specific","existingTabId":"t1"}`,
			setup: func(env *testEnv) {
				env.store.put(tabs.Tab{TabID: "t1", ProjectID: "other", ClientID: testOwner})
			},
			wantStatus: http.StatusForbidden, wantMessage: "forbidden",
		},
		{name: "unknown project", body: `{"projectId":"does-not-exist"}`, wantStatus: http.StatusNotFound, wantMessage: "project not found"},
		{name: "unknown project gui", body: `{"projectId":"does-not-exist","guiMode":true}`, wantStatus: http.StatusNotFound, wantMessage: "project not found"},
		{name: "gui not enabled", body: `{"projectId":"nogui","guiMode":true}`, wantStatus: http.StatusBadRequest, wantMessage: "GUI mode not enabled for this project"},
		{
			name: "per-client limit",
			body: `{"projectId":"limited"}`,
			setup: func(env *testEnv) {
				rec := serve(http.HandlerFunc(env.srv.handleTabs), tabsRequest(http.MethodPost, "/api/tabs", `{"projectId":"limited"}`))
				require.Equal(t, http.StatusCreated, rec.Code)
			},
			wantStatus: http.StatusTooManyRequests, wantMessage: "maximum tabs per client exceeded",
		},
		{
			name: "total limit",
			body: `{"projectId":"total"}`,
			setup: func(env *testEnv) {
				req := tabsRequest(http.MethodPost, "/api/tabs", `{"projectId":"total"}`)
				req.Header.Del("Cookie")
				req.AddCookie(&http.Cookie{Name: clientCookieName, Value: "another-client"})
				rec := serve(http.HandlerFunc(env.srv.handleTabs), req)
				require.Equal(t, http.StatusCreated, rec.Code)
			},
			wantStatus: http.StatusTooManyRequests, wantMessage: "maximum tabs total exceeded",
		},
		{
			name: "store create failure",
			body: `{"projectId":"limited"}`,
			setup: func(env *testEnv) {
				env.store.createErr = errBoom
			},
			wantStatus: http.StatusInternalServerError, wantMessage: "internal error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestServer(t, testServerOpts{projects: []gatewayconfig.Project{limited, total, noGUI}})
			if tt.setup != nil {
				tt.setup(env)
			}
			rec := serve(http.HandlerFunc(env.srv.handleTabs), tabsRequest(http.MethodPost, "/api/tabs", tt.body))
			require.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
			eb := decodeError(t, rec)
			assert.Equal(t, tt.wantStatus, eb.Status)
			assert.Equal(t, tt.wantMessage, eb.Message)
		})
	}
}

func TestHandleTabs_LimitDetails(t *testing.T) {
	p := testProject("limited")
	p.Limits.MaxTabsPerClient = 1
	env := newTestServer(t, testServerOpts{projects: []gatewayconfig.Project{p}})
	h := http.HandlerFunc(env.srv.handleTabs)

	require.Equal(t, http.StatusCreated, serve(h, tabsRequest(http.MethodPost, "/api/tabs", `{"projectId":"limited"}`)).Code)
	rec := serve(h, tabsRequest(http.MethodPost, "/api/tabs", `{"projectId":"limited"}`))
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "limit: 1 tabs per client for project limited", decodeError(t, rec).Details)
}

func TestHandleTabs_CreateSuccess(t *testing.T) {
	gui := testProject("desktop")
	gui.GUIEnabled = true

	tests := []struct {
		name       string
		body       string
		forwarded  string
		wantPrefix string
		wantGUI    bool
	}{
		{name: "terminal", body: `{"projectId":"term"}`, wantPrefix: "ws://gateway.example/ws?tab="},
		{name: "explicit create_new", body: `{"projectId":"term","openAction":"create_new"}`, wantPrefix: "ws://gateway.example/ws?tab="},
		{name: "terminal behind TLS proxy", body: `{"projectId":"term"}`, forwarded: "https", wantPrefix: "wss://gateway.example/ws?tab="},
		{name: "gui", body: `{"projectId":"desktop","guiMode":true}`, wantPrefix: "ws://gateway.example/vnc?tab=", wantGUI: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestServer(t, testServerOpts{projects: []gatewayconfig.Project{testProject("term"), gui}})
			sub := env.srv.subscribeTabEvents(testOwner)
			defer env.srv.unsubscribeTabEvents(testOwner, sub)

			req := tabsRequest(http.MethodPost, "/api/tabs", tt.body)
			if tt.forwarded != "" {
				req.Header.Set("X-Forwarded-Proto", tt.forwarded)
			}
			rec := serve(http.HandlerFunc(env.srv.handleTabs), req)
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

			resp := decodeCreate(t, rec)
			assert.NotEmpty(t, resp.Tab.TabID)
			assert.Equal(t, testOwner, resp.Tab.ClientID)
			assert.Equal(t, tt.wantPrefix+resp.Tab.TabID, resp.WSURL)
			assert.Equal(t, tt.wantGUI, resp.GUIMode)
			assert.True(t, env.store.has(resp.Tab.TabID), "tab must be persisted")

			// A snapshot event including the new tab is pushed to subscribers.
			select {
			case msg := <-sub:
				var ev struct {
					Type string     `json:"type"`
					Tabs []tabs.Tab `json:"tabs"`
				}
				require.NoError(t, json.Unmarshal(msg, &ev))
				assert.Equal(t, "snapshot", ev.Type)
				require.Len(t, ev.Tabs, 1)
				assert.Equal(t, resp.Tab.TabID, ev.Tabs[0].TabID)
			case <-time.After(time.Second):
				t.Fatal("expected snapshot event after tab creation")
			}
		})
	}
}

func TestHandleTabs_AttachRecent(t *testing.T) {
	env := newTestServer(t, testServerOpts{projects: []gatewayconfig.Project{testProject("p")}})
	h := http.HandlerFunc(env.srv.handleTabs)

	env.store.put(tabs.Tab{TabID: "old", ProjectID: "p", ClientID: testOwner})
	env.store.put(tabs.Tab{TabID: "other-project", ProjectID: "q", ClientID: testOwner})
	env.store.put(tabs.Tab{TabID: "newest", ProjectID: "p", ClientID: testOwner})
	env.store.put(tabs.Tab{TabID: "someone-elses", ProjectID: "p", ClientID: "client:other"})

	t.Run("returns most recent tab for project", func(t *testing.T) {
		rec := serve(h, tabsRequest(http.MethodPost, "/api/tabs", `{"projectId":"p","openAction":"attach_recent"}`))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		resp := decodeCreate(t, rec)
		assert.Equal(t, "newest", resp.Tab.TabID)
		assert.Equal(t, "ws://gateway.example/ws?tab=newest", resp.WSURL)
	})

	t.Run("gui mode uses vnc endpoint", func(t *testing.T) {
		rec := serve(h, tabsRequest(http.MethodPost, "/api/tabs", `{"projectId":"p","openAction":"attach_recent","guiMode":true}`))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, "ws://gateway.example/vnc?tab=newest", decodeCreate(t, rec).WSURL)
	})

	t.Run("falls back to creating a tab when none exist", func(t *testing.T) {
		env := newTestServer(t, testServerOpts{projects: []gatewayconfig.Project{testProject("p")}})
		rec := serve(http.HandlerFunc(env.srv.handleTabs), tabsRequest(http.MethodPost, "/api/tabs", `{"projectId":"p","openAction":"attach_recent"}`))
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	})

	t.Run("falls back to creating a tab when listing fails", func(t *testing.T) {
		env := newTestServer(t, testServerOpts{projects: []gatewayconfig.Project{testProject("p")}})
		env.store.listErr = errBoom
		rec := serve(http.HandlerFunc(env.srv.handleTabs), tabsRequest(http.MethodPost, "/api/tabs", `{"projectId":"p","openAction":"attach_recent"}`))
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	})
}

func TestHandleTabs_AttachSpecific(t *testing.T) {
	env := newTestServer(t, testServerOpts{})
	env.store.put(tabs.Tab{TabID: "t1", ProjectID: "p", ClientID: testOwner})
	h := http.HandlerFunc(env.srv.handleTabs)

	rec := serve(h, tabsRequest(http.MethodPost, "/api/tabs", `{"projectId":"p","openAction":"attach_specific","existingTabId":"t1"}`))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	resp := decodeCreate(t, rec)
	assert.Equal(t, "t1", resp.Tab.TabID)
	assert.Equal(t, "ws://gateway.example/ws?tab=t1", resp.WSURL)

	rec = serve(h, tabsRequest(http.MethodPost, "/api/tabs", `{"projectId":"p","openAction":"attach_specific","existingTabId":"t1","guiMode":true}`))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "ws://gateway.example/vnc?tab=t1", decodeCreate(t, rec).WSURL)
	assert.True(t, decodeCreate(t, rec).GUIMode)
}

func TestHandleTabs_List(t *testing.T) {
	env := newTestServer(t, testServerOpts{})
	env.store.put(tabs.Tab{TabID: "mine", ProjectID: "p", ClientID: testOwner})
	env.store.put(tabs.Tab{TabID: "theirs", ProjectID: "p", ClientID: "client:other"})
	h := http.HandlerFunc(env.srv.handleTabs)

	rec := serve(h, tabsRequest(http.MethodGet, "/api/tabs", ""))
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Tabs []tabs.Tab `json:"tabs"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Tabs, 1)
	assert.Equal(t, "mine", body.Tabs[0].TabID)

	env.store.listErr = errBoom
	rec = serve(h, tabsRequest(http.MethodGet, "/api/tabs", ""))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "internal error", decodeError(t, rec).Message)
}

func TestHandleTabs_NilStoreIsGatewayDisabled(t *testing.T) {
	env := newTestServer(t, testServerOpts{noTabStore: true})
	rec := serve(http.HandlerFunc(env.srv.handleTabs), tabsRequest(http.MethodGet, "/api/tabs", ""))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleTabsReorder(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		setup      func(env *testEnv)
		wantStatus int
		wantMsg    string
	}{
		{name: "invalid json", body: `nope`, wantStatus: http.StatusBadRequest, wantMsg: "invalid request body"},
		{name: "missing tabIds", body: `{}`, wantStatus: http.StatusBadRequest, wantMsg: "tabIds is required"},
		{name: "empty tabIds", body: `{"tabIds":[]}`, wantStatus: http.StatusBadRequest, wantMsg: "tabIds is required"},
		{
			name: "store update failure", body: `{"tabIds":["a"]}`,
			setup:      func(env *testEnv) { env.store.positionsErr = errBoom },
			wantStatus: http.StatusInternalServerError, wantMsg: "failed to reorder tabs",
		},
		{
			name: "list after reorder failure", body: `{"tabIds":["a"]}`,
			setup:      func(env *testEnv) { env.store.listErr = errBoom },
			wantStatus: http.StatusInternalServerError, wantMsg: "internal error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestServer(t, testServerOpts{})
			if tt.setup != nil {
				tt.setup(env)
			}
			rec := serve(http.HandlerFunc(env.srv.handleTabsReorder), tabsRequest(http.MethodPut, "/api/tabs/reorder", tt.body))
			require.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
			assert.Equal(t, tt.wantMsg, decodeError(t, rec).Message)
		})
	}

	t.Run("success", func(t *testing.T) {
		env := newTestServer(t, testServerOpts{})
		env.store.put(tabs.Tab{TabID: "a", ClientID: testOwner})
		env.store.put(tabs.Tab{TabID: "b", ClientID: testOwner})
		sub := env.srv.subscribeTabEvents(testOwner)
		defer env.srv.unsubscribeTabEvents(testOwner, sub)

		rec := serve(http.HandlerFunc(env.srv.handleTabsReorder), tabsRequest(http.MethodPut, "/api/tabs/reorder", `{"tabIds":["b","a"]}`))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, [][]string{{"b", "a"}}, env.store.positionsCalls)
		assert.Contains(t, rec.Body.String(), `"tabs"`)
		select {
		case msg := <-sub:
			assert.Contains(t, string(msg), `"type":"snapshot"`)
		case <-time.After(time.Second):
			t.Fatal("expected snapshot event after reorder")
		}
	})
}

func TestHandleTabByID_Delete(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		setup      func(env *testEnv)
		wantStatus int
		wantMsg    string
	}{
		{name: "missing id", path: "/api/tabs/", wantStatus: http.StatusBadRequest, wantMsg: "missing tab id"},
		{name: "not found", path: "/api/tabs/nope", wantStatus: http.StatusNotFound, wantMsg: "tab not found"},
		{
			name: "store error", path: "/api/tabs/t1",
			setup:      func(env *testEnv) { env.store.getErr = errBoom },
			wantStatus: http.StatusInternalServerError, wantMsg: "internal error",
		},
		{
			name: "other owner", path: "/api/tabs/t1",
			setup: func(env *testEnv) {
				env.store.put(tabs.Tab{TabID: "t1", ClientID: "client:other"})
			},
			wantStatus: http.StatusForbidden, wantMsg: "forbidden",
		},
		{
			name: "close failure", path: "/api/tabs/t1",
			setup: func(env *testEnv) {
				env.store.put(tabs.Tab{TabID: "t1", ClientID: testOwner})
				env.store.deleteErr = errBoom
			},
			wantStatus: http.StatusInternalServerError, wantMsg: "internal error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestServer(t, testServerOpts{})
			if tt.setup != nil {
				tt.setup(env)
			}
			rec := serve(http.HandlerFunc(env.srv.routeTabByID), tabsRequest(http.MethodDelete, tt.path, ""))
			require.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
			assert.Equal(t, tt.wantMsg, decodeError(t, rec).Message)
		})
	}

	t.Run("success emits delete event", func(t *testing.T) {
		env := newTestServer(t, testServerOpts{projects: []gatewayconfig.Project{testProject("p")}})
		h := env.srv.routes(nil)

		rec := serve(h, tabsRequest(http.MethodPost, "/api/tabs", `{"projectId":"p"}`))
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		tabID := decodeCreate(t, rec).Tab.TabID

		sub := env.srv.subscribeTabEvents(testOwner)
		defer env.srv.unsubscribeTabEvents(testOwner, sub)

		rec = serve(h, tabsRequest(http.MethodDelete, "/api/tabs/"+tabID, ""))
		require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
		assert.False(t, env.store.has(tabID))

		// The manager's status callback isn't wired in tests, so the only
		// event is the handler's explicit delete broadcast.
		select {
		case msg := <-sub:
			var ev map[string]any
			require.NoError(t, json.Unmarshal(msg, &ev))
			assert.Equal(t, "delete", ev["type"])
			assert.Equal(t, tabID, ev["tabId"])
		case <-time.After(time.Second):
			t.Fatal("expected delete event")
		}
	})
}

func TestHandleTabHealth(t *testing.T) {
	strptr := func(s string) *string { return &s }

	t.Run("validation", func(t *testing.T) {
		tests := []struct {
			name       string
			path       string
			setup      func(env *testEnv)
			wantStatus int
			wantMsg    string
		}{
			{name: "missing id", path: "/api/tabs//health", wantStatus: http.StatusBadRequest, wantMsg: "missing tab id"},
			{name: "not found", path: "/api/tabs/nope/health", wantStatus: http.StatusNotFound, wantMsg: "tab not found"},
			{
				name: "store error", path: "/api/tabs/t1/health",
				setup:      func(env *testEnv) { env.store.getErr = errBoom },
				wantStatus: http.StatusInternalServerError, wantMsg: "internal error",
			},
			{
				name: "other owner", path: "/api/tabs/t1/health",
				setup: func(env *testEnv) {
					env.store.put(tabs.Tab{TabID: "t1", ClientID: "client:other", DownstreamURI: strptr("ws://127.0.0.1:1/ws")})
				},
				wantStatus: http.StatusForbidden, wantMsg: "forbidden",
			},
			{
				name: "no downstream", path: "/api/tabs/t1/health",
				setup:      func(env *testEnv) { env.store.put(tabs.Tab{TabID: "t1", ClientID: testOwner}) },
				wantStatus: http.StatusServiceUnavailable, wantMsg: "tab not ready",
			},
			{
				name: "empty downstream", path: "/api/tabs/t1/health",
				setup: func(env *testEnv) {
					env.store.put(tabs.Tab{TabID: "t1", ClientID: testOwner, DownstreamURI: strptr("")})
				},
				wantStatus: http.StatusServiceUnavailable, wantMsg: "tab not ready",
			},
			{
				name: "malformed downstream", path: "/api/tabs/t1/health",
				setup: func(env *testEnv) {
					env.store.put(tabs.Tab{TabID: "t1", ClientID: testOwner, DownstreamURI: strptr("ws://[::1")})
				},
				wantStatus: http.StatusInternalServerError, wantMsg: "internal error",
			},
			{
				name: "downstream unreachable", path: "/api/tabs/t1/health",
				setup: func(env *testEnv) {
					// Port 1 on loopback refuses connections immediately.
					env.store.put(tabs.Tab{TabID: "t1", ClientID: testOwner, DownstreamURI: strptr("ws://127.0.0.1:1/ws")})
				},
				wantStatus: http.StatusServiceUnavailable, wantMsg: "health check failed",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				env := newTestServer(t, testServerOpts{})
				if tt.setup != nil {
					tt.setup(env)
				}
				rec := serve(http.HandlerFunc(env.srv.routeTabByID), tabsRequest(http.MethodGet, tt.path, ""))
				require.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
				assert.Equal(t, tt.wantMsg, decodeError(t, rec).Message)
			})
		}
	})

	t.Run("proxies downstream status, headers and body", func(t *testing.T) {
		var gotPath string
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Upstream", "project-pod")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"unhealthy"}`))
		}))
		defer upstream.Close()
		u, err := url.Parse(upstream.URL)
		require.NoError(t, err)

		env := newTestServer(t, testServerOpts{})
		env.store.put(tabs.Tab{TabID: "t1", ClientID: testOwner, DownstreamURI: strptr("ws://" + u.Host + "/ws")})

		rec := serve(http.HandlerFunc(env.srv.routeTabByID), tabsRequest(http.MethodGet, "/api/tabs/t1/health", ""))
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Equal(t, `{"status":"unhealthy"}`, rec.Body.String())
		assert.Equal(t, "/api/healthz", gotPath)
		// Regression: headers used to be copied after WriteHeader and were dropped.
		assert.Equal(t, "project-pod", rec.Header().Get("X-Upstream"))
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	})
}
