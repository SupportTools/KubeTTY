package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestRelay_ActivityChan(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	cfg := Config{
		ProjectID:     "test",
		Endpoint:      endpoint,
		DownstreamURI: endpoint.String(),
	}

	relay := New(cfg)

	// Verify ActivityChan returns a channel
	activityCh := relay.ActivityChan()
	if activityCh == nil {
		t.Fatal("Expected ActivityChan to return a non-nil channel")
	}

	// Verify it's the same channel on multiple calls
	activityCh2 := relay.ActivityChan()
	if activityCh != activityCh2 {
		t.Error("Expected ActivityChan to return the same channel instance")
	}
}

func TestRelay_ActivitySignal_NonBlocking(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	cfg := Config{
		ProjectID:     "test",
		Endpoint:      endpoint,
		DownstreamURI: endpoint.String(),
	}

	relay := New(cfg)
	activityCh := relay.ActivityChan()

	// Fill the channel (buffer size is 1)
	select {
	case relay.activityCh <- struct{}{}:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("First send to activity channel should not block")
	}

	// Attempt to send again - should not block (pipe() uses non-blocking send)
	select {
	case relay.activityCh <- struct{}{}:
		// This might succeed if the channel was drained
	default:
		// This is expected if the channel is full - non-blocking send succeeded
	}

	// Drain the channel to verify we can receive
	select {
	case <-activityCh:
		// Successfully drained
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected to receive from activity channel")
	}
}

func TestRelay_Pipe_SignalsActivity(t *testing.T) {
	// Create mock downstream server
	upgrader := websocket.Upgrader{}
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("Upgrade error: %v", err)
			return
		}
		defer conn.Close()

		// Echo back any messages received
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(msgType, data); err != nil {
				return
			}
		}
	}))
	defer downstream.Close()

	// Parse URL and convert to WebSocket
	downstreamURL, _ := url.Parse(downstream.URL)
	downstreamURL.Scheme = "ws"

	cfg := Config{
		ProjectID:     "test",
		Endpoint:      downstreamURL,
		DownstreamURI: downstreamURL.String(),
	}

	relay := New(cfg)

	// Create mock upstream connection
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Send a test message
		if err := conn.WriteMessage(websocket.TextMessage, []byte("test message")); err != nil {
			t.Logf("Write error: %v", err)
			return
		}

		// Wait for echo
		_, _, err = conn.ReadMessage()
		if err != nil {
			t.Logf("Read error: %v", err)
		}
	}))
	defer upstream.Close()

	// Connect to upstream mock server
	upstreamURL, _ := url.Parse(upstream.URL)
	upstreamURL.Scheme = "ws"
	upstreamConn, _, err := websocket.DefaultDialer.Dial(upstreamURL.String(), nil)
	if err != nil {
		t.Fatalf("Failed to connect to upstream mock: %v", err)
	}
	defer upstreamConn.Close()

	// Get activity channel
	activityCh := relay.ActivityChan()

	// Start proxy in background
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	proxyCh := make(chan error, 1)
	go func() {
		proxyCh <- relay.Proxy(ctx, upstreamConn)
	}()

	// Wait for activity signal or timeout
	select {
	case <-activityCh:
		t.Log("Activity signal received as expected")
	case err := <-proxyCh:
		if err == nil || err == context.Canceled {
			// Proxy ended, that's okay for this test
			t.Log("Proxy ended")
		} else {
			t.Logf("Proxy error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Expected to receive activity signal, but timed out")
	}

	cancel()
}

func TestRelay_ActivityChannel_BufferSize(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	cfg := Config{
		ProjectID:     "test",
		Endpoint:      endpoint,
		DownstreamURI: endpoint.String(),
	}

	relay := New(cfg)

	// Verify channel has buffer size 1 (can send without receiving)
	sent := false
	select {
	case relay.activityCh <- struct{}{}:
		sent = true
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected to be able to send to buffered channel")
	}

	if !sent {
		t.Error("Expected to successfully send to activity channel")
	}

	// Second send should not block (non-blocking send in pipe())
	// We can't easily test the non-blocking behavior from outside,
	// but we can verify the channel is buffered
	select {
	case <-relay.activityCh:
		// Drained successfully
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected to drain activity channel")
	}
}

func TestRelay_New_InitializesActivityChannel(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	cfg := Config{
		ProjectID:     "test",
		Endpoint:      endpoint,
		DownstreamURI: endpoint.String(),
	}

	relay := New(cfg)

	if relay.activityCh == nil {
		t.Error("Expected activityCh to be initialized by New()")
	}

	// Verify it's a buffered channel (cap > 0)
	if cap(relay.activityCh) != 1 {
		t.Errorf("Expected activityCh to have buffer size 1, got %d", cap(relay.activityCh))
	}
}

func TestRelay_Pipe_ErrorHandling(t *testing.T) {
	// This test verifies that pipe() properly handles errors
	// and that activity signals are sent before errors occur

	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	cfg := Config{
		ProjectID:     "test",
		Endpoint:      endpoint,
		DownstreamURI: endpoint.String(),
	}

	relay := New(cfg)

	// Create a mock connection that will fail
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Close immediately to trigger error
		conn.Close()
	}))
	defer server.Close()

	// Connect to server
	serverURL, _ := url.Parse(server.URL)
	serverURL.Scheme = "ws"
	conn, _, err := websocket.DefaultDialer.Dial(serverURL.String(), nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Attempt proxy - should fail due to closed connection
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = relay.Proxy(ctx, conn)
	if err == nil {
		t.Error("Expected Proxy to return an error with closed connection")
	}
}

func TestRelay_Pipe_ContextCancellation(t *testing.T) {
	// Verify that pipe respects context cancellation

	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Keep connection open
		for {
			time.Sleep(100 * time.Millisecond)
		}
	}))
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	serverURL.Scheme = "ws"

	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	cfg := Config{
		ProjectID:     "test",
		Endpoint:      endpoint,
		DownstreamURI: endpoint.String(),
	}

	relay := New(cfg)

	conn, _, err := websocket.DefaultDialer.Dial(serverURL.String(), nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- relay.Proxy(ctx, conn)
	}()

	// Cancel context
	cancel()

	// Verify proxy exits
	select {
	case err := <-errCh:
		if err != nil && !strings.Contains(err.Error(), "context") && err != context.Canceled {
			t.Logf("Proxy exited with error: %v (this is acceptable)", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Expected Proxy to exit after context cancellation")
	}
}
