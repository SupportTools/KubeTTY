package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ---- ActivityChan edge case tests ----

func TestRelay_ActivityChan_BufferCapacity(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	relay := New(Config{ProjectID: "test", Endpoint: endpoint})

	ch := relay.ActivityChan()
	// Should have buffer size 1
	if cap(ch) != 1 {
		t.Errorf("ActivityChan buffer capacity = %d, want 1", cap(ch))
	}
}

func TestRelay_ActivityChan_ReturnsSameChannel(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	relay := New(Config{ProjectID: "test", Endpoint: endpoint})

	ch1 := relay.ActivityChan()
	ch2 := relay.ActivityChan()

	// Should return same channel
	if ch1 != ch2 {
		t.Error("ActivityChan() should return the same channel on multiple calls")
	}
}

// ---- setStatus observer tests ----

func TestRelay_setStatus_NotifiesAllObservers(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	relay := New(Config{ProjectID: "test", Endpoint: endpoint})

	// Subscribe multiple observers
	ch1 := relay.Subscribe()
	ch2 := relay.Subscribe()
	ch3 := relay.Subscribe()

	// Trigger status change
	relay.setStatus(StatusConnecting, nil)

	// All should receive
	for i, ch := range []<-chan StatusEvent{ch1, ch2, ch3} {
		select {
		case evt := <-ch:
			if evt.Status != StatusConnecting {
				t.Errorf("observer %d: status = %v, want %v", i+1, evt.Status, StatusConnecting)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("observer %d: did not receive event", i+1)
		}
	}
}

func TestRelay_setStatus_WithError(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	relay := New(Config{ProjectID: "test", Endpoint: endpoint})

	ch := relay.Subscribe()

	testErr := context.DeadlineExceeded
	relay.setStatus(StatusReconnecting, testErr)

	select {
	case evt := <-ch:
		if evt.Status != StatusReconnecting {
			t.Errorf("status = %v, want %v", evt.Status, StatusReconnecting)
		}
		if evt.Err != testErr {
			t.Errorf("err = %v, want %v", evt.Err, testErr)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("did not receive event")
	}
}

func TestRelay_setStatus_DropsIfChannelFull(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	relay := New(Config{ProjectID: "test", Endpoint: endpoint})

	ch := relay.Subscribe()

	// Fill the channel buffer (cap is 4)
	for i := 0; i < 10; i++ {
		relay.setStatus(StatusConnecting, nil)
	}

	// Should have received at most 4 events (buffer size)
	count := 0
	timeout := time.After(50 * time.Millisecond)
drainLoop:
	for {
		select {
		case <-ch:
			count++
		case <-timeout:
			break drainLoop
		}
	}

	if count != 4 {
		t.Errorf("received %d events, expected 4 (buffer size)", count)
	}
}

// ---- Connect with closed status tests ----

func TestRelay_Connect_ExitsIfClosed(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	relay := New(Config{ProjectID: "test", Endpoint: endpoint})

	// Close the relay first
	_ = relay.Close()

	ctx := context.Background()
	_, err := relay.Connect(ctx, FixedBackoff{Delay: 10 * time.Millisecond})

	if err == nil {
		t.Error("Connect should return error when relay is closed")
	}
	if err.Error() != "relay closed" {
		t.Errorf("error = %q, want %q", err.Error(), "relay closed")
	}
}

func TestRelay_Connect_ReuseExisting(t *testing.T) {
	// Start downstream server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, _ := upgrader.Upgrade(w, req, nil)
		defer conn.Close()
		// Keep connection open
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	endpoint, _ := url.Parse("ws" + strings.TrimPrefix(srv.URL, "http"))
	relay := New(Config{ProjectID: "test", Endpoint: endpoint})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// First connect
	conn1, err := relay.Connect(ctx, DefaultBackoff())
	if err != nil {
		t.Fatalf("first Connect failed: %v", err)
	}

	// Second connect should reuse
	conn2, err := relay.Connect(ctx, DefaultBackoff())
	if err != nil {
		t.Fatalf("second Connect failed: %v", err)
	}

	if conn1 != conn2 {
		t.Error("second Connect should reuse existing connection")
	}

	relay.Close()
}

// ---- Close with active connection tests ----

func TestRelay_Close_WithActiveConnection(t *testing.T) {
	// Start downstream server
	connClosed := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, _ := upgrader.Upgrade(w, req, nil)
		defer func() {
			conn.Close()
			close(connClosed)
		}()
		// Wait for close signal
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	endpoint, _ := url.Parse("ws" + strings.TrimPrefix(srv.URL, "http"))
	relay := New(Config{ProjectID: "test", Endpoint: endpoint})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := relay.Connect(ctx, DefaultBackoff())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// Close should disconnect downstream
	err = relay.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}

	// Verify downstream detected close
	select {
	case <-connClosed:
		// Success
	case <-time.After(time.Second):
		t.Error("downstream did not detect close")
	}
}

// ---- CircuitBreaker edge case tests ----

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	cb := NewCircuitBreaker(100, time.Second)

	var wg sync.WaitGroup
	iterations := 100

	// Multiple goroutines recording failures
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				cb.RecordFailure()
			}
		}()
	}

	// Multiple goroutines checking state
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = cb.IsOpen()
				_ = cb.Failures()
			}
		}()
	}

	wg.Wait()

	// Should have recorded all failures
	if cb.Failures() != 1000 {
		t.Errorf("failures = %d, want 1000", cb.Failures())
	}
}

func TestCircuitBreaker_ResetAfterSuccess(t *testing.T) {
	cb := NewCircuitBreaker(5, time.Second)

	// Record failures up to threshold
	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}
	if !cb.IsOpen() {
		t.Error("circuit breaker should be open")
	}

	// Record success resets
	cb.RecordSuccess()
	if cb.IsOpen() {
		t.Error("circuit breaker should be closed after success")
	}
	if cb.Failures() != 0 {
		t.Errorf("failures after success = %d, want 0", cb.Failures())
	}
}

// ---- Proxy circuit breaker tests ----

func TestRelay_Proxy_CircuitBreakerOpens(t *testing.T) {
	// Use endpoint that won't connect
	endpoint, _ := url.Parse("ws://localhost:1/ws")
	relay := New(Config{ProjectID: "test", Endpoint: endpoint})

	// Pre-open the circuit breaker
	for i := 0; i < 5; i++ {
		relay.circuitBreaker.RecordFailure()
	}

	// Start upstream server
	upstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		upstream, err := upgrader.Upgrade(w, req, nil)
		if err != nil {
			return
		}
		defer upstream.Close()

		ctx := context.Background()
		err = relay.Proxy(ctx, upstream)
		if err == nil {
			t.Error("Proxy should return error when circuit breaker is open")
		}
	}))
	defer upstreamSrv.Close()

	// Connect client
	upstreamURL, _ := url.Parse("ws" + strings.TrimPrefix(upstreamSrv.URL, "http"))
	client, _, err := websocket.DefaultDialer.Dial(upstreamURL.String(), nil)
	if err != nil {
		t.Fatalf("dial upstream: %v", err)
	}
	defer client.Close()

	// Wait for handler to complete
	time.Sleep(200 * time.Millisecond)
}

// ---- runPipes timeout test ----

func TestRelay_runPipes_ContextCancellation(t *testing.T) {
	// Create downstream that keeps connection open
	downstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, _ := upgrader.Upgrade(w, req, nil)
		defer conn.Close()
		// Keep alive
		time.Sleep(10 * time.Second)
	}))
	defer downstreamSrv.Close()

	endpoint, _ := url.Parse("ws" + strings.TrimPrefix(downstreamSrv.URL, "http"))
	relay := New(Config{ProjectID: "test", Endpoint: endpoint})

	// Create upstream that keeps connection open
	upstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		upstream, _ := upgrader.Upgrade(w, req, nil)
		defer upstream.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		downstream, _ := relay.Connect(ctx, DefaultBackoff())
		if downstream == nil {
			return
		}

		// runPipes should exit when context is cancelled
		err := relay.runPipes(ctx, upstream, downstream)
		if err != context.DeadlineExceeded {
			t.Errorf("runPipes error = %v, want %v", err, context.DeadlineExceeded)
		}
	}))
	defer upstreamSrv.Close()

	// Connect client
	upstreamURL, _ := url.Parse("ws" + strings.TrimPrefix(upstreamSrv.URL, "http"))
	client, _, err := websocket.DefaultDialer.Dial(upstreamURL.String(), nil)
	if err != nil {
		t.Fatalf("dial upstream: %v", err)
	}
	defer client.Close()

	// Wait for test to complete
	time.Sleep(500 * time.Millisecond)
	relay.Close()
}

// ---- Additional status transition tests ----

func TestRelay_StatusTransitions(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	relay := New(Config{ProjectID: "test", Endpoint: endpoint})

	// Initial state
	if relay.Status() != StatusIdle {
		t.Errorf("initial status = %v, want %v", relay.Status(), StatusIdle)
	}

	// Manual status transitions
	relay.setStatus(StatusConnecting, nil)
	if relay.Status() != StatusConnecting {
		t.Errorf("after setStatus(Connecting) = %v, want %v", relay.Status(), StatusConnecting)
	}

	relay.setStatus(StatusConnected, nil)
	if relay.Status() != StatusConnected {
		t.Errorf("after setStatus(Connected) = %v, want %v", relay.Status(), StatusConnected)
	}

	relay.setStatus(StatusReconnecting, context.DeadlineExceeded)
	if relay.Status() != StatusReconnecting {
		t.Errorf("after setStatus(Reconnecting) = %v, want %v", relay.Status(), StatusReconnecting)
	}
	if relay.LastError() != context.DeadlineExceeded {
		t.Errorf("LastError = %v, want %v", relay.LastError(), context.DeadlineExceeded)
	}

	relay.setStatus(StatusClosed, nil)
	if relay.Status() != StatusClosed {
		t.Errorf("after setStatus(Closed) = %v, want %v", relay.Status(), StatusClosed)
	}
}

// ---- Config edge case tests ----

func TestConfig_NilFields(t *testing.T) {
	cfg := Config{
		ProjectID:     "test",
		Endpoint:      nil,
		Headers:       nil,
		Dialer:        nil,
		DownstreamURI: "",
	}

	relay := New(cfg)
	if relay == nil {
		t.Fatal("New should handle nil fields")
	}

	// Dialer should be defaulted
	if relay.cfg.Dialer == nil {
		t.Error("Dialer should be defaulted to websocket.DefaultDialer")
	}
}

// TestPipe_ContextCancellation_UnblocksReadMessage verifies that cancelling the
// context causes a goroutine blocked inside ReadMessage() to exit promptly.
//
// This is the regression test for the zombie-TCP-connection bug:
// gorilla/websocket.ReadMessage() blocks indefinitely and auto-responds to
// ping frames. Without the watcher goroutine in pipe(), the downstream TCP
// connection (and the project's single-client slot) could remain open for
// hours after the relay had logically shut down.
func TestPipe_ContextCancellation_UnblocksReadMessage(t *testing.T) {
	// Downstream server: accepts the upgrade then deliberately never sends any
	// data, so the up→down pipe goroutine will block indefinitely in
	// ReadMessage(). A short-lived context (200 ms) simulates the relay being
	// cancelled while a ReadMessage() is in flight.
	downstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, _ := upgrader.Upgrade(w, req, nil)
		defer conn.Close()
		// Hold open; the test controls shutdown via context cancellation.
		time.Sleep(5 * time.Second)
	}))
	defer downstreamSrv.Close()

	endpoint, _ := url.Parse("ws" + strings.TrimPrefix(downstreamSrv.URL, "http"))
	r := New(Config{ProjectID: "test-ctx-cancel", Endpoint: endpoint})

	// Upstream server: an ephemeral WS server that lets the test retrieve the
	// server-side *websocket.Conn for passing to runPipes directly.
	connCh := make(chan *websocket.Conn, 1)
	upstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, _ := upgrader.Upgrade(w, req, nil)
		connCh <- conn
		time.Sleep(5 * time.Second)
		conn.Close()
	}))
	defer upstreamSrv.Close()

	// Dial the upstream server to get a client-side conn.
	upURL := "ws" + strings.TrimPrefix(upstreamSrv.URL, "http")
	upConn, _, err := websocket.DefaultDialer.Dial(upURL, nil)
	if err != nil {
		t.Fatalf("upstream dial: %v", err)
	}
	defer upConn.Close()

	// Connect relay to the downstream server.
	connectCtx, connectCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer connectCancel()
	ds, err := r.Connect(connectCtx, DefaultBackoff())
	if err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}
	defer r.Close()

	// Use a short context that we cancel manually after pipes are running.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run runPipes in a goroutine; capture when it returns.
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		_ = r.runPipes(ctx, upConn, ds)
	}()

	// Give the pipe goroutines a moment to start and block in ReadMessage().
	time.Sleep(50 * time.Millisecond)

	// Cancel the context. The watcher goroutine inside pipe() should call
	// SetReadDeadline, unblocking ReadMessage() within ~100 ms.
	cancel()

	// runPipes must return well within the old 5-second goroutine-leak window.
	// We allow 1 second to give ample margin while keeping the test fast.
	select {
	case <-doneCh:
		// Pass: pipes exited promptly after context cancellation.
	case <-time.After(1 * time.Second):
		t.Fatal("pipe goroutine did not exit after context cancellation — goroutine leak detected")
	}
}
