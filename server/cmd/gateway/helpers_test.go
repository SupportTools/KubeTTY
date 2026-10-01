package main

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/supporttools/KubeTTY/server/internal/auth"
	"github.com/supporttools/KubeTTY/server/internal/config"
	gatewayconfig "github.com/supporttools/KubeTTY/server/internal/gateway/config"
	"github.com/supporttools/KubeTTY/server/internal/gateway/manager"
	"github.com/supporttools/KubeTTY/server/internal/gateway/tabs"
	"github.com/supporttools/KubeTTY/server/internal/shared/metrics"
)

// testAppMetrics is shared because NewAppMetrics registers collectors with the
// global Prometheus registry and panics if called twice.
var (
	testAppMetricsOnce sync.Once
	testAppMetrics     *metrics.AppMetrics
)

func sharedAppMetrics() *metrics.AppMetrics {
	testAppMetricsOnce.Do(func() { testAppMetrics = metrics.NewAppMetrics() })
	return testAppMetrics
}

const testJWTSecret = "test-secret-key-that-is-at-least-32-bytes-long"

// fakeTabStore is an in-memory, concurrency-safe tabs.Store for handler tests.
type fakeTabStore struct {
	mu    sync.Mutex
	order []string
	tabs  map[string]tabs.Tab

	listErr      error
	getErr       error
	createErr    error
	deleteErr    error
	positionsErr error

	listClients    []string
	positionsCalls [][]string
}

var _ tabs.Store = (*fakeTabStore)(nil)

func newFakeTabStore() *fakeTabStore {
	return &fakeTabStore{tabs: make(map[string]tabs.Tab)}
}

func (f *fakeTabStore) put(tab tabs.Tab) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.tabs[tab.TabID]; !ok {
		f.order = append(f.order, tab.TabID)
	}
	f.tabs[tab.TabID] = tab
}

func (f *fakeTabStore) lastListClient() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.listClients) == 0 {
		return ""
	}
	return f.listClients[len(f.listClients)-1]
}

func (f *fakeTabStore) has(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.tabs[id]
	return ok
}

func (f *fakeTabStore) Create(_ context.Context, tab tabs.Tab) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.put(tab)
	return nil
}

func (f *fakeTabStore) UpdateStatus(_ context.Context, tabID string, status tabs.Status, lastError *string, downstreamURI *string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.tabs[tabID]; ok {
		t.Status = status
		t.LastError = lastError
		t.DownstreamURI = downstreamURI
		f.tabs[tabID] = t
	}
	return nil
}

func (f *fakeTabStore) UpdateClientID(_ context.Context, tabID, clientID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.tabs[tabID]; ok {
		t.ClientID = clientID
		f.tabs[tabID] = t
	}
	return nil
}

func (f *fakeTabStore) UpdatePositions(_ context.Context, _ string, tabIDs []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.positionsCalls = append(f.positionsCalls, append([]string(nil), tabIDs...))
	return f.positionsErr
}

func (f *fakeTabStore) Delete(_ context.Context, tabID string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.tabs, tabID)
	for i, id := range f.order {
		if id == tabID {
			f.order = append(f.order[:i], f.order[i+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeTabStore) Get(_ context.Context, tabID string) (*tabs.Tab, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tabs[tabID]
	if !ok {
		return nil, tabs.ErrNotFound
	}
	return &t, nil
}

func (f *fakeTabStore) ListByClient(_ context.Context, clientID string, limit int) ([]tabs.Tab, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listClients = append(f.listClients, clientID)
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := []tabs.Tab{}
	for _, id := range f.order {
		if t := f.tabs[id]; t.ClientID == clientID {
			out = append(out, t)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

func (f *fakeTabStore) ListAll(context.Context) ([]tabs.Tab, error) { return nil, nil }
func (f *fakeTabStore) CountByClientAndProject(context.Context, string, string) (int, error) {
	return 0, nil
}
func (f *fakeTabStore) GetNextPosition(context.Context, string) (int, error) { return 0, nil }
func (f *fakeTabStore) GetStatusCounts(context.Context) (map[string]int, error) {
	return map[string]int{}, nil
}
func (f *fakeTabStore) GetRecentErrors(context.Context, int) ([]tabs.Tab, error) { return nil, nil }
func (f *fakeTabStore) GetActiveCountByProject(context.Context) (map[string]int, error) {
	return map[string]int{}, nil
}
func (f *fakeTabStore) CleanOrphanedTabs(context.Context, time.Duration) (int64, error) {
	return 0, nil
}

// testServerOpts configures newTestServer.
type testServerOpts struct {
	authLocal     bool
	projects      []gatewayconfig.Project
	noTabManager  bool
	noTabStore    bool
	customUIFiles fstest.MapFS
}

// testEnv bundles a server with its fakes.
type testEnv struct {
	srv   *server
	store *fakeTabStore
	mgr   *manager.Manager
}

func defaultUIFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>KubeTTY</title>")},
		"assets/app.js": {Data: []byte("console.info('app')")},
	}
}

// newTestServer builds a server wired the same way main() does, but backed by
// in-memory fakes instead of PostgreSQL and Kubernetes.
func newTestServer(t *testing.T, opts testServerOpts) *testEnv {
	t.Helper()

	cfg := config.GatewayConfig{}
	cfg.AuthMode = "disabled"
	var authMgr *auth.Manager
	var authStore auth.Store
	if opts.authLocal {
		cfg.AuthMode = "local"
		ms := auth.NewMockStore()
		mgr, err := auth.NewManager(ms, testJWTSecret, "kubetty-test", 15*time.Minute, time.Hour)
		require.NoError(t, err)
		authMgr = mgr
		authStore = ms
	}

	store := newFakeTabStore()
	var mgr *manager.Manager
	if !opts.noTabManager {
		mgr = manager.NewWithConfig(gatewayconfig.Catalog{Projects: opts.projects}, store, manager.ManagerConfig{
			IdleTimeout: time.Hour,
		})
		t.Cleanup(mgr.Stop)
	}

	uiFS := opts.customUIFiles
	if uiFS == nil {
		uiFS = defaultUIFS()
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	srv := &server{
		cfg:         cfg,
		authStore:   authStore,
		authMgr:     authMgr,
		upgrader:    websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
		uiFS:        uiFS,
		appMetrics:  sharedAppMetrics(),
		tabManager:  mgr,
		tabSubs:     make(map[string]map[chan []byte]struct{}),
		shutdownCtx: ctx,
	}
	if !opts.noTabStore {
		srv.tabStore = store
	}
	return &testEnv{srv: srv, store: store, mgr: mgr}
}

// issueToken returns a valid access token for a fresh user.
func issueToken(t *testing.T, srv *server) (string, uuid.UUID) {
	t.Helper()
	require.NotNil(t, srv.authMgr, "auth must be enabled")
	user := &auth.User{ID: uuid.New(), Username: "alice", IsActive: true}
	pair, err := srv.authMgr.IssueTokenPair(context.Background(), user, auth.TokenMetadata{})
	require.NoError(t, err)
	return pair.AccessToken, user.ID
}

func testProject(id string) gatewayconfig.Project {
	return gatewayconfig.Project{ID: id, DisplayName: id, Namespace: "ns", Service: "svc-" + id, Port: 8080}
}

var errBoom = errors.New("boom")
