package vnc

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supporttools/KubeTTY/server/internal/gateway/relay"
)

// mockVNCServer creates a simple TCP server that echoes data back.
// It simulates basic VNC server behavior for testing purposes.
type mockVNCServer struct {
	listener net.Listener
	addr     string
	mu       sync.Mutex
	conns    []net.Conn
	received [][]byte
	toSend   [][]byte // Data to send to clients
}

func newMockVNCServer(t *testing.T) *mockVNCServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	m := &mockVNCServer{
		listener: listener,
		addr:     listener.Addr().String(),
	}

	go m.accept(t)
	return m
}

func (m *mockVNCServer) accept(t *testing.T) {
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			return // Listener closed
		}

		m.mu.Lock()
		m.conns = append(m.conns, conn)
		toSend := m.toSend
		m.mu.Unlock()

		// Send any queued data
		for _, data := range toSend {
			conn.Write(data)
		}

		// Start echo handler
		go m.handleConn(t, conn)
	}
}

func (m *mockVNCServer) handleConn(t *testing.T, conn net.Conn) {
	defer conn.Close()

	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}

		// Record received data
		m.mu.Lock()
		data := make([]byte, n)
		copy(data, buf[:n])
		m.received = append(m.received, data)
		m.mu.Unlock()

		// Echo back with "ECHO:" prefix for verification
		response := append([]byte("ECHO:"), buf[:n]...)
		conn.Write(response)
	}
}

// QueueData queues data to be sent to the next connecting client.
func (m *mockVNCServer) QueueData(data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toSend = append(m.toSend, data)
}

// GetReceived returns all data received by the server.
func (m *mockVNCServer) GetReceived() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([][]byte, len(m.received))
	copy(result, m.received)
	return result
}

func (m *mockVNCServer) Close() {
	m.listener.Close()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, conn := range m.conns {
		conn.Close()
	}
}

// WebSocket test helper
func createTestWebSocketServer(t *testing.T, handler func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("WebSocket upgrade failed: %v", err)
			return
		}
		defer conn.Close()
		handler(conn)
	}))

	return server
}

func dialTestWebSocket(t *testing.T, serverURL string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	return conn
}

func TestNewRelay(t *testing.T) {
	t.Parallel()

	target := "localhost:5901"
	r := NewRelay(target)

	assert.Equal(t, target, r.config.Target)
	assert.Equal(t, DefaultDialTimeout, r.config.DialTimeout)
	assert.Equal(t, DefaultReadBufferSize, r.config.ReadBufferSize)
	assert.Equal(t, DefaultWriteBufferSize, r.config.WriteBufferSize)
	assert.Equal(t, RelayStatusIdle, r.Status())
}

func TestNewRelayWithOptions(t *testing.T) {
	t.Parallel()

	target := "localhost:5901"
	r := NewRelay(target,
		WithDialTimeout(5*time.Second),
		WithReadBufferSize(1024),
		WithWriteBufferSize(512),
		WithMaxRetries(5),
		WithRetryDelay(1*time.Second),
	)

	assert.Equal(t, target, r.config.Target)
	assert.Equal(t, 5*time.Second, r.config.DialTimeout)
	assert.Equal(t, 1024, r.config.ReadBufferSize)
	assert.Equal(t, 512, r.config.WriteBufferSize)
	assert.Equal(t, 5, r.config.MaxRetries)
	assert.Equal(t, 1*time.Second, r.config.RetryDelay)
}

func TestRelay_Connect(t *testing.T) {
	t.Parallel()

	// Start mock VNC server
	mock := newMockVNCServer(t)
	defer mock.Close()

	r := NewRelay(mock.addr, WithDialTimeout(2*time.Second))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test connection
	err := r.ensureConnection(ctx)
	require.NoError(t, err)
	assert.Equal(t, RelayStatusConnected, r.Status())

	// Cleanup
	err = r.Close()
	require.NoError(t, err)
	assert.Equal(t, RelayStatusClosed, r.Status())
}

func TestRelay_ConnectFailure(t *testing.T) {
	t.Parallel()

	// Use an invalid address
	r := NewRelay("127.0.0.1:1", WithDialTimeout(100*time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := r.ensureConnection(ctx)
	require.Error(t, err)
	assert.Equal(t, RelayStatusClosed, r.Status())
}

func TestRelay_Subscribe(t *testing.T) {
	t.Parallel()

	mock := newMockVNCServer(t)
	defer mock.Close()

	r := NewRelay(mock.addr)

	// Subscribe to status updates
	statusCh := r.Subscribe()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect and expect status updates
	err := r.ensureConnection(ctx)
	require.NoError(t, err)

	// Should receive connecting and connected events
	var events []relay.StatusEvent
	timeout := time.After(1 * time.Second)
	for {
		select {
		case evt := <-statusCh:
			events = append(events, evt)
			if evt.Status == RelayStatusConnected {
				goto done
			}
		case <-timeout:
			goto done
		}
	}
done:

	assert.True(t, len(events) >= 1, "expected at least one status event")

	// Find the connected event
	var foundConnected bool
	for _, evt := range events {
		if evt.Status == RelayStatusConnected {
			foundConnected = true
			break
		}
	}
	assert.True(t, foundConnected, "expected to find connected status")

	r.Close()
}

func TestRelay_ActivityChan(t *testing.T) {
	t.Parallel()

	r := NewRelay("localhost:5901")
	activityCh := r.ActivityChan()

	assert.NotNil(t, activityCh)

	// Should be able to send non-blocking
	select {
	case r.activityCh <- struct{}{}:
	default:
		t.Error("activity channel should accept at least one value")
	}

	// Second send should not block (drops if full)
	select {
	case r.activityCh <- struct{}{}:
	default:
		// This is expected behavior
	}

	r.Close()
}

func TestRelay_Close(t *testing.T) {
	t.Parallel()

	mock := newMockVNCServer(t)
	defer mock.Close()

	r := NewRelay(mock.addr)

	// Subscribe before connecting
	statusCh := r.Subscribe()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := r.ensureConnection(ctx)
	require.NoError(t, err)

	// Close the relay
	err = r.Close()
	require.NoError(t, err)
	assert.Equal(t, RelayStatusClosed, r.Status())

	// Should receive closed event
	timeout := time.After(1 * time.Second)
	var gotClosed bool
	for {
		select {
		case evt, ok := <-statusCh:
			if !ok {
				// Channel closed
				gotClosed = true
				goto done
			}
			if evt.Status == RelayStatusClosed {
				gotClosed = true
				goto done
			}
		case <-timeout:
			goto done
		}
	}
done:
	assert.True(t, gotClosed, "expected closed status or channel close")

	// Double close should be safe
	err = r.Close()
	require.NoError(t, err)
}

func TestRelay_Proxy_BidirectionalData(t *testing.T) {
	t.Parallel()

	// Start mock VNC server
	mock := newMockVNCServer(t)
	defer mock.Close()

	r := NewRelay(mock.addr,
		WithDialTimeout(2*time.Second),
		WithReadTimeout(5*time.Second),
		WithWriteTimeout(5*time.Second),
	)
	defer r.Close()

	// Track data received by test client
	var clientReceived [][]byte
	var clientReceivedMu sync.Mutex
	proxyStarted := make(chan struct{})
	proxyDone := make(chan error, 1)

	// Create WebSocket server that runs the VNC proxy
	// When a client connects, the server passes the connection to r.Proxy()
	wsServer := createTestWebSocketServer(t, func(serverConn *websocket.Conn) {
		close(proxyStarted)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		proxyDone <- r.Proxy(ctx, serverConn)
	})
	defer wsServer.Close()

	// Connect to the WebSocket server as a client
	clientConn := dialTestWebSocket(t, wsServer.URL)
	defer clientConn.Close()

	// Wait for proxy to start
	select {
	case <-proxyStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("proxy did not start in time")
	}

	// Wait for VNC connection to be established
	time.Sleep(300 * time.Millisecond)
	require.Equal(t, RelayStatusConnected, r.Status())

	// Start goroutine to read responses from server
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			_, data, err := clientConn.ReadMessage()
			if err != nil {
				return
			}
			clientReceivedMu.Lock()
			clientReceived = append(clientReceived, data)
			clientReceivedMu.Unlock()
		}
	}()

	// Send data from client -> VNC server
	testData := []byte("Hello VNC")
	err := clientConn.WriteMessage(websocket.BinaryMessage, testData)
	require.NoError(t, err)

	// Wait for echo response
	time.Sleep(500 * time.Millisecond)

	// Verify data was received by VNC server
	received := mock.GetReceived()
	require.NotEmpty(t, received, "VNC server should have received data")
	assert.Equal(t, testData, received[0])

	// Verify client received echo response
	clientReceivedMu.Lock()
	clientData := clientReceived
	clientReceivedMu.Unlock()

	require.NotEmpty(t, clientData, "Client should have received echo response")
	// Echo format is "ECHO:" + original data
	expected := append([]byte("ECHO:"), testData...)
	assert.Equal(t, expected, clientData[0])

	// Cleanup
	clientConn.Close()
}

func TestRelay_ContextCancellation(t *testing.T) {
	t.Parallel()

	mock := newMockVNCServer(t)
	defer mock.Close()

	r := NewRelay(mock.addr)
	defer r.Close()

	// Create a context that we'll cancel
	ctx, cancel := context.WithCancel(context.Background())
	proxyStarted := make(chan struct{})
	proxyDone := make(chan error, 1)

	// Create WebSocket server that runs the VNC proxy
	wsServer := createTestWebSocketServer(t, func(serverConn *websocket.Conn) {
		close(proxyStarted)
		proxyDone <- r.Proxy(ctx, serverConn)
	})
	defer wsServer.Close()

	// Connect to the WebSocket server as a client
	clientConn := dialTestWebSocket(t, wsServer.URL)
	defer clientConn.Close()

	// Wait for proxy to start
	select {
	case <-proxyStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("proxy did not start in time")
	}

	// Wait for connection
	time.Sleep(200 * time.Millisecond)

	// Cancel context
	cancel()

	// Should exit with context error
	select {
	case err := <-proxyDone:
		assert.True(t, err == nil || err == context.Canceled || strings.Contains(err.Error(), "context canceled"),
			"expected context cancellation error, got: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("proxy did not exit after context cancellation")
	}
}

func TestRelay_Target(t *testing.T) {
	t.Parallel()

	target := "vnc-service.namespace.svc:5901"
	r := NewRelay(target)

	assert.Equal(t, target, r.Target())
	r.Close()
}

func TestConfig_Defaults(t *testing.T) {
	t.Parallel()

	cfg := NewConfig("localhost:5901")

	assert.Equal(t, "localhost:5901", cfg.Target)
	assert.Equal(t, DefaultDialTimeout, cfg.DialTimeout)
	assert.Equal(t, DefaultReadBufferSize, cfg.ReadBufferSize)
	assert.Equal(t, DefaultWriteBufferSize, cfg.WriteBufferSize)
	assert.Equal(t, DefaultReadTimeout, cfg.ReadTimeout)
	assert.Equal(t, DefaultWriteTimeout, cfg.WriteTimeout)
	assert.Equal(t, DefaultMaxRetries, cfg.MaxRetries)
	assert.Equal(t, DefaultRetryDelay, cfg.RetryDelay)
}

func TestConfig_WithOptions(t *testing.T) {
	t.Parallel()

	cfg := NewConfig("localhost:5901",
		WithDialTimeout(5*time.Second),
		WithReadBufferSize(1024),
		WithWriteBufferSize(512),
		WithReadTimeout(30*time.Second),
		WithWriteTimeout(5*time.Second),
		WithMaxRetries(10),
		WithRetryDelay(2*time.Second),
	)

	assert.Equal(t, 5*time.Second, cfg.DialTimeout)
	assert.Equal(t, 1024, cfg.ReadBufferSize)
	assert.Equal(t, 512, cfg.WriteBufferSize)
	assert.Equal(t, 30*time.Second, cfg.ReadTimeout)
	assert.Equal(t, 5*time.Second, cfg.WriteTimeout)
	assert.Equal(t, 10, cfg.MaxRetries)
	assert.Equal(t, 2*time.Second, cfg.RetryDelay)
}

func TestConfig_InvalidOptions(t *testing.T) {
	t.Parallel()

	// Invalid values should be ignored
	cfg := NewConfig("localhost:5901",
		WithDialTimeout(-1*time.Second),
		WithReadBufferSize(-1),
		WithWriteBufferSize(0),
		WithMaxRetries(-5),
		WithRetryDelay(0),
	)

	// Should use defaults when invalid
	assert.Equal(t, DefaultDialTimeout, cfg.DialTimeout)
	assert.Equal(t, DefaultReadBufferSize, cfg.ReadBufferSize)
	assert.Equal(t, DefaultWriteBufferSize, cfg.WriteBufferSize)
	assert.Equal(t, DefaultMaxRetries, cfg.MaxRetries)
	assert.Equal(t, DefaultRetryDelay, cfg.RetryDelay)
}

// TestRelay_ReconnectOnFailure tests the reconnection logic.
func TestRelay_ReconnectOnFailure(t *testing.T) {
	t.Parallel()

	r := NewRelay("127.0.0.1:1", // Invalid port
		WithDialTimeout(100*time.Millisecond),
		WithMaxRetries(2),
		WithRetryDelay(50*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Should fail to connect and exhaust retries
	err := r.reconnect(ctx)
	require.Error(t, err)

	// Should still have retries left
	err = r.reconnect(ctx)
	require.Error(t, err)

	// Third attempt should fail with max retries exceeded
	err = r.reconnect(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max reconnection attempts")

	r.Close()
}

// TestRelay_LastError tests the LastError method.
func TestRelay_LastError(t *testing.T) {
	t.Parallel()

	r := NewRelay("127.0.0.1:1", WithDialTimeout(100*time.Millisecond))

	// Initially no error
	assert.Nil(t, r.LastError())

	// After failed connection attempt, should have error
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = r.ensureConnection(ctx)

	err := r.LastError()
	assert.NotNil(t, err)
	r.Close()
}

// TestRelay_EnsureConnectionAlreadyConnected tests that ensureConnection returns early if already connected.
func TestRelay_EnsureConnectionAlreadyConnected(t *testing.T) {
	t.Parallel()

	mock := newMockVNCServer(t)
	defer mock.Close()

	r := NewRelay(mock.addr)
	defer r.Close()

	ctx := context.Background()

	// First connection
	err := r.ensureConnection(ctx)
	require.NoError(t, err)
	assert.Equal(t, RelayStatusConnected, r.Status())

	// Second call should return early (already connected)
	err = r.ensureConnection(ctx)
	require.NoError(t, err)
	assert.Equal(t, RelayStatusConnected, r.Status())
}

// TestRelay_EnsureConnectionAfterClose tests that ensureConnection fails after close.
func TestRelay_EnsureConnectionAfterClose(t *testing.T) {
	t.Parallel()

	r := NewRelay("localhost:5901")
	r.Close()

	ctx := context.Background()
	err := r.ensureConnection(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

// TestRelay_ReconnectAfterClose tests that reconnect fails after close.
func TestRelay_ReconnectAfterClose(t *testing.T) {
	t.Parallel()

	r := NewRelay("localhost:5901")
	r.Close()

	ctx := context.Background()
	err := r.reconnect(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

// TestRelay_CloseIdempotent tests that Close can be called multiple times.
func TestRelay_CloseIdempotent(t *testing.T) {
	t.Parallel()

	mock := newMockVNCServer(t)
	defer mock.Close()

	r := NewRelay(mock.addr)

	ctx := context.Background()
	_ = r.ensureConnection(ctx)

	// First close
	err := r.Close()
	require.NoError(t, err)

	// Second close should be safe
	err = r.Close()
	require.NoError(t, err)

	// Third close
	err = r.Close()
	require.NoError(t, err)
}

// TestRelay_CloseWithObservers tests that Close notifies observers.
func TestRelay_CloseWithObservers(t *testing.T) {
	t.Parallel()

	r := NewRelay("localhost:5901")

	// Subscribe multiple observers
	ch1 := r.Subscribe()
	ch2 := r.Subscribe()

	r.Close()

	// Both should receive close notification or channel close
	for i, ch := range []<-chan relay.StatusEvent{ch1, ch2} {
		select {
		case evt, ok := <-ch:
			if ok && evt.Status != RelayStatusClosed {
				t.Errorf("observer %d: expected closed status, got %v", i+1, evt.Status)
			}
		case <-time.After(100 * time.Millisecond):
			// Channel might be closed
		}
	}
}

// BenchmarkRelay_DataTransfer benchmarks the data transfer performance.
func BenchmarkRelay_DataTransfer(b *testing.B) {
	// Start mock VNC server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer listener.Close()

	// Simple echo server
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go io.Copy(conn, conn)
		}
	}()

	r := NewRelay(listener.Addr().String())
	defer r.Close()

	ctx := context.Background()
	err = r.ensureConnection(ctx)
	if err != nil {
		b.Fatal(err)
	}

	// Benchmark would go here - simplified for now
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.mu.RLock()
		conn := r.vncConn
		r.mu.RUnlock()

		if conn != nil {
			conn.Write([]byte("benchmark data"))
			buf := make([]byte, 64)
			conn.Read(buf)
		}
	}
}
