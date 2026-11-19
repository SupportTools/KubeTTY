package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/supporttools/KubeTTY/server/internal/config"
	gatewayconfig "github.com/supporttools/KubeTTY/server/internal/gateway/config"
	"github.com/supporttools/KubeTTY/server/internal/gateway/manager"
	"github.com/supporttools/KubeTTY/server/internal/gateway/tabs"
)

// wsTestStore is the slice of tabs.Store the /ws ownership tests touch; any
// other call panics on the nil embedded interface, which flags an unexpected
// store write.
type wsTestStore struct {
	tabs.Store
	updateClientID atomic.Int32
}

func (s *wsTestStore) Create(context.Context, tabs.Tab) error               { return nil }
func (s *wsTestStore) GetNextPosition(context.Context, string) (int, error) { return 0, nil }
func (s *wsTestStore) UpdateStatus(context.Context, string, tabs.Status, *string, *string) error {
	return nil
}
func (s *wsTestStore) UpdateClientID(context.Context, string, string) error {
	s.updateClientID.Add(1)
	return nil
}

// newWSOwnershipServer builds a no-auth gateway server (owner ID is the
// kubetty_client cookie) with one tab owned by "client:alice".
func newWSOwnershipServer(t *testing.T) (*server, *wsTestStore, string) {
	t.Helper()
	cat := gatewayconfig.Catalog{Projects: []gatewayconfig.Project{{ID: "proj", Namespace: "ns", Service: "svc", Port: 8080, GUIEnabled: true, GUIVNCPort: 5901}}}
	store := &wsTestStore{}
	mgr := manager.New(cat, store, 2*time.Hour)
	tab, err := mgr.CreateTab(context.Background(), "proj", "client:alice")
	if err != nil {
		t.Fatalf("CreateTab: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := &server{
		cfg:         config.GatewayConfig{},
		upgrader:    websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
		tabManager:  mgr,
		tabStore:    store,
		tabSubs:     make(map[string]map[chan []byte]struct{}),
		shutdownCtx: ctx,
	}
	return s, store, tab.TabID
}

func wsRequest(path, tabID, clientCookie string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path+"?tab="+url.QueryEscape(tabID)+"&force=true", nil)
	r.AddCookie(&http.Cookie{Name: clientCookieName, Value: clientCookie})
	return r
}

// TestGatewayWS_ForceOtherOwnerForbidden: /ws?force=true for another
// owner's tab is a 403 before the upgrade, and the tab keeps its owner
// (cluster-services-1542).
func TestGatewayWS_ForceOtherOwnerForbidden(t *testing.T) {
	s, store, tabID := newWSOwnershipServer(t)

	for _, tc := range []struct {
		name    string
		path    string
		handler http.HandlerFunc
	}{
		{"terminal", "/ws", s.handleGatewayWebsocket},
		{"vnc", "/ws/vnc", s.handleVNCWebsocket},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.handler(rec, wsRequest(tc.path, tabID, "mallory"))
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
			}
			if err := s.tabManager.CheckAttach(tabID, "client:alice"); err != nil {
				t.Fatalf("tab no longer owned by client:alice: %v", err)
			}
		})
	}
	if n := store.updateClientID.Load(); n != 0 {
		t.Fatalf("UpdateClientID called %d times, want 0", n)
	}
}

// TestGatewayWS_ForceOwnOwnerUpgrades: the owner's own force reattach passes
// the ownership check and is upgraded to a WebSocket.
func TestGatewayWS_ForceOwnOwnerUpgrades(t *testing.T) {
	s, store, tabID := newWSOwnershipServer(t)
	ts := httptest.NewServer(http.HandlerFunc(s.handleGatewayWebsocket))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?tab=" + url.QueryEscape(tabID) + "&force=true"
	hdr := http.Header{}
	hdr.Set("Cookie", clientCookieName+"=alice")
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, resp, err := dialer.Dial(wsURL, hdr)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("owner force dial failed: %v (status %d)", err, status)
	}
	defer conn.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}
	if err := s.tabManager.CheckAttach(tabID, "client:alice"); err != nil {
		t.Fatalf("owner changed: %v", err)
	}
	if n := store.updateClientID.Load(); n != 0 {
		t.Fatalf("UpdateClientID called %d times, want 0", n)
	}
}
