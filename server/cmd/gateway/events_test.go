package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gatewaymetrics "github.com/supporttools/KubeTTY/server/internal/gateway/metrics"
	"github.com/supporttools/KubeTTY/server/internal/gateway/tabs"
)

type tabEvent struct {
	Type    string                     `json:"type"`
	Tabs    []tabs.Tab                 `json:"tabs"`
	Tab     *tabs.Tab                  `json:"tab"`
	TabID   string                     `json:"tabId"`
	Metrics *gatewaymetrics.TabMetrics `json:"metrics"`
}

// readSSEEvent reads the next "data: ..." event from an SSE stream.
func readSSEEvent(t *testing.T, r *bufio.Reader) tabEvent {
	t.Helper()
	type result struct {
		ev  tabEvent
		err error
	}
	ch := make(chan result, 1)
	go func() {
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				ch <- result{err: err}
				return
			}
			line = strings.TrimRight(line, "\n")
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var ev tabEvent
			err = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev)
			ch <- result{ev: ev, err: err}
			return
		}
	}()
	select {
	case res := <-ch:
		require.NoError(t, res.err)
		return res.ev
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for SSE event")
		return tabEvent{}
	}
}

// waitForSubscriber blocks until clientID has at least one event subscriber.
func waitForSubscriber(t *testing.T, srv *server, clientID string) {
	t.Helper()
	require.Eventually(t, func() bool {
		srv.tabSubsMu.Lock()
		defer srv.tabSubsMu.Unlock()
		return len(srv.tabSubs[clientID]) > 0
	}, 5*time.Second, 5*time.Millisecond)
}

func TestTabEventsSSE_ThroughFullHandlerChain(t *testing.T) {
	env := newTestServer(t, testServerOpts{})
	env.store.put(tabs.Tab{TabID: "t1", ProjectID: "p", ClientID: testOwner})
	ts := httptest.NewServer(env.srv.routes(nil))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/tabs/events", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: clientCookieName, Value: testClientCookie})

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Regression: the logging middleware's ResponseWriter did not implement
	// http.Flusher, so SSE always failed with 500 "streaming unsupported".
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	assert.Equal(t, "no-cache", resp.Header.Get("Cache-Control"))

	reader := bufio.NewReader(resp.Body)

	// First event is a snapshot of the client's tabs.
	ev := readSSEEvent(t, reader)
	assert.Equal(t, "snapshot", ev.Type)
	require.Len(t, ev.Tabs, 1)
	assert.Equal(t, "t1", ev.Tabs[0].TabID)

	// Status updates are delivered to the owning client.
	env.srv.handleTabStatusUpdate(tabs.Tab{TabID: "t1", ProjectID: "p", ClientID: testOwner, Status: tabs.StatusConnected})
	ev = readSSEEvent(t, reader)
	assert.Equal(t, "update", ev.Type)
	require.NotNil(t, ev.Tab)
	assert.Equal(t, tabs.StatusConnected, ev.Tab.Status)

	// Metrics updates are routed via the tab's owner.
	env.srv.handleTabMetricsUpdate("t1", gatewaymetrics.TabMetrics{})
	ev = readSSEEvent(t, reader)
	assert.Equal(t, "metrics", ev.Type)
	assert.Equal(t, "t1", ev.TabID)

	env.srv.broadcastTabDelete(testOwner, "t1")
	ev = readSSEEvent(t, reader)
	assert.Equal(t, "delete", ev.Type)
	assert.Equal(t, "t1", ev.TabID)

	// Disconnecting unsubscribes the client.
	cancel()
	require.Eventually(t, func() bool {
		env.srv.tabSubsMu.Lock()
		defer env.srv.tabSubsMu.Unlock()
		return len(env.srv.tabSubs[testOwner]) == 0
	}, 5*time.Second, 5*time.Millisecond)
}

func TestTabEventsSSE_IsolatedPerClient(t *testing.T) {
	env := newTestServer(t, testServerOpts{})
	ts := httptest.NewServer(env.srv.routes(nil))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/tabs/events", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: clientCookieName, Value: testClientCookie})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	assert.Equal(t, "snapshot", readSSEEvent(t, reader).Type)

	// An event for another client must not leak; the next event we see is ours.
	env.srv.broadcastTabDelete("client:someone-else", "theirs")
	env.srv.broadcastTabDelete(testOwner, "ours")
	ev := readSSEEvent(t, reader)
	assert.Equal(t, "ours", ev.TabID)
}

// nonFlushingWriter is a ResponseWriter without http.Flusher.
type nonFlushingWriter struct {
	header http.Header
	status int
	body   strings.Builder
}

func (w *nonFlushingWriter) Header() http.Header         { return w.header }
func (w *nonFlushingWriter) Write(b []byte) (int, error) { return w.body.Write(b) }
func (w *nonFlushingWriter) WriteHeader(code int)        { w.status = code }

func TestTabEventsSSE_StreamingUnsupported(t *testing.T) {
	env := newTestServer(t, testServerOpts{})
	w := &nonFlushingWriter{header: http.Header{}}
	env.srv.handleTabEvents(w, tabsRequest(http.MethodGet, "/api/tabs/events", ""))
	assert.Equal(t, http.StatusInternalServerError, w.status)
	assert.Contains(t, w.body.String(), "streaming unsupported")
}

func wsURL(ts *httptest.Server, path string) string {
	return "ws" + strings.TrimPrefix(ts.URL, "http") + path
}

func TestTabEventsWebSocket(t *testing.T) {
	env := newTestServer(t, testServerOpts{})
	env.store.put(tabs.Tab{TabID: "t1", ProjectID: "p", ClientID: testOwner})
	ts := httptest.NewServer(env.srv.routes(nil))
	defer ts.Close()

	header := http.Header{}
	header.Set("Cookie", clientCookieName+"="+testClientCookie)
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL(ts, "/api/tabs/events"), header)
	require.NoError(t, err)
	defer conn.Close()
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	var ev tabEvent
	require.NoError(t, conn.ReadJSON(&ev))
	assert.Equal(t, "snapshot", ev.Type)
	require.Len(t, ev.Tabs, 1)

	env.srv.broadcastTabDelete(testOwner, "t1")
	require.NoError(t, conn.ReadJSON(&ev))
	assert.Equal(t, "delete", ev.Type)

	// Closing the client connection unsubscribes it.
	require.NoError(t, conn.Close())
	require.Eventually(t, func() bool {
		env.srv.tabSubsMu.Lock()
		defer env.srv.tabSubsMu.Unlock()
		return len(env.srv.tabSubs[testOwner]) == 0
	}, 5*time.Second, 5*time.Millisecond)
}

func TestGatewayWebsocket(t *testing.T) {
	for _, authLocal := range []bool{false, true} {
		name := "auth_disabled"
		if authLocal {
			name = "auth_local"
		}
		t.Run(name, func(t *testing.T) {
			env := newTestServer(t, testServerOpts{authLocal: authLocal})
			ts := httptest.NewServer(env.srv.routes(nil))
			defer ts.Close()

			header := http.Header{}
			header.Set("Cookie", clientCookieName+"="+testClientCookie)
			if authLocal {
				token, _ := issueToken(t, env.srv)
				header.Set("Authorization", "Bearer "+token)
			}

			t.Run("missing tab parameter", func(t *testing.T) {
				_, resp, err := websocket.DefaultDialer.Dial(wsURL(ts, "/ws"), header)
				require.Error(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
			})

			t.Run("upgrade succeeds and unknown tab is closed with error", func(t *testing.T) {
				// Regression (auth disabled): /ws is wrapped by the metrics
				// instrumentation whose ResponseWriter could not be hijacked,
				// so every terminal WebSocket upgrade failed.
				conn, resp, err := websocket.DefaultDialer.Dial(wsURL(ts, "/ws?tab=unknown"), header)
				require.NoError(t, err)
				defer conn.Close()
				assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

				require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
				_, _, err = conn.ReadMessage()
				var closeErr *websocket.CloseError
				require.ErrorAs(t, err, &closeErr)
				assert.Equal(t, websocket.CloseInternalServerErr, closeErr.Code)
				assert.Equal(t, tabs.ErrNotFound.Error(), closeErr.Text)
			})

			t.Run("vnc missing tab parameter", func(t *testing.T) {
				_, resp, err := websocket.DefaultDialer.Dial(wsURL(ts, "/vnc"), header)
				require.Error(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
			})

			t.Run("vnc tab without gui support", func(t *testing.T) {
				_, resp, err := websocket.DefaultDialer.Dial(wsURL(ts, "/vnc?tab=unknown"), header)
				require.Error(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
			})
		})
	}
}

func TestSubscribeUnsubscribeTabEvents(t *testing.T) {
	srv := &server{}
	ch1 := srv.subscribeTabEvents("c1")
	ch2 := srv.subscribeTabEvents("c1")
	ch3 := srv.subscribeTabEvents("c2")
	assert.Len(t, srv.tabSubs["c1"], 2)
	assert.Len(t, srv.tabSubs["c2"], 1)

	srv.sendTabEvent("c1", map[string]string{"type": "x"})
	for _, ch := range []chan []byte{ch1, ch2} {
		select {
		case msg := <-ch:
			assert.JSONEq(t, `{"type":"x"}`, string(msg))
		default:
			t.Fatal("expected event on c1 subscriber")
		}
	}
	select {
	case <-ch3:
		t.Fatal("c2 must not receive c1's event")
	default:
	}

	srv.unsubscribeTabEvents("c1", ch1)
	assert.Len(t, srv.tabSubs["c1"], 1)
	_, open := <-ch1
	assert.False(t, open, "unsubscribe closes the channel")

	srv.unsubscribeTabEvents("c1", ch2)
	_, exists := srv.tabSubs["c1"]
	assert.False(t, exists, "empty client entry is removed")
	srv.unsubscribeTabEvents("c2", ch3)
	assert.Empty(t, srv.tabSubs)
}

func TestSendTabEvent_DropsWhenSubscriberBufferFull(t *testing.T) {
	srv := &server{}
	ch := srv.subscribeTabEvents("c")
	defer srv.unsubscribeTabEvents("c", ch)

	done := make(chan struct{})
	go func() {
		for i := 0; i < cap(ch)+5; i++ {
			srv.sendTabEvent("c", map[string]int{"i": i})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sendTabEvent blocked on a slow subscriber")
	}
	assert.Len(t, ch, cap(ch))
}

func TestSendTabEvent_UnmarshalablePayloadIgnored(t *testing.T) {
	srv := &server{}
	ch := srv.subscribeTabEvents("c")
	defer srv.unsubscribeTabEvents("c", ch)
	srv.sendTabEvent("c", map[string]any{"bad": make(chan int)})
	assert.Empty(t, ch)
}

// TestSendTabEvent_ConcurrentWithSubscribe guards against the race where
// sendTabEvent iterated a subscriber map (and sent on its channels) after
// releasing the lock, racing with subscribe/unsubscribe. Run with -race.
func TestSendTabEvent_ConcurrentWithSubscribe(t *testing.T) {
	srv := &server{}
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				srv.sendTabEvent("c", map[string]string{"type": "x"})
			}
		}
	}()
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				ch := srv.subscribeTabEvents("c")
				srv.unsubscribeTabEvents("c", ch)
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}

func TestBroadcastHelpers_NilStore(t *testing.T) {
	srv := &server{}
	ch := srv.subscribeTabEvents("c")
	defer srv.unsubscribeTabEvents("c", ch)

	// Without a tab store, snapshot and metrics broadcasts are no-ops.
	srv.broadcastTabSnapshot("c")
	srv.broadcastMetricsUpdate("t1", gatewaymetrics.TabMetrics{})
	assert.Empty(t, ch)
}

func TestBroadcastHelpers_StoreErrors(t *testing.T) {
	env := newTestServer(t, testServerOpts{})
	ch := env.srv.subscribeTabEvents(testOwner)
	defer env.srv.unsubscribeTabEvents(testOwner, ch)

	env.store.listErr = errBoom
	env.srv.broadcastTabSnapshot(testOwner)
	// Unknown tab: metrics cannot be routed to an owner.
	env.srv.broadcastMetricsUpdate("missing", gatewaymetrics.TabMetrics{})
	assert.Empty(t, ch)
}
