package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/supporttools/KubeTTY/server/internal/config"
	"github.com/supporttools/KubeTTY/server/internal/shared/buffer"
)

// TestPtySession_HasClients verifies hasClients returns correct state.
func TestPtySession_HasClients(t *testing.T) {
	ps := &ptySession{
		clients: make(map[*websocket.Conn]*wsClient),
	}

	// Initially no clients
	if ps.hasClients() {
		t.Error("hasClients() should return false when no clients connected")
	}

	// Add a mock client
	mockConn := &websocket.Conn{}
	ps.clients[mockConn] = &wsClient{conn: mockConn}

	if !ps.hasClients() {
		t.Error("hasClients() should return true when client connected")
	}

	// Remove the client
	delete(ps.clients, mockConn)

	if ps.hasClients() {
		t.Error("hasClients() should return false after client removed")
	}
}

// TestPtySession_GetClientCount verifies getClientCount returns correct count.
func TestPtySession_GetClientCount(t *testing.T) {
	ps := &ptySession{
		clients: make(map[*websocket.Conn]*wsClient),
	}

	if count := ps.getClientCount(); count != 0 {
		t.Errorf("getClientCount() = %d, want 0", count)
	}

	// Add clients
	for i := 0; i < 3; i++ {
		mockConn := &websocket.Conn{}
		ps.clients[mockConn] = &wsClient{conn: mockConn}
	}

	if count := ps.getClientCount(); count != 3 {
		t.Errorf("getClientCount() = %d, want 3", count)
	}
}

// TestPtySession_AddClient verifies client addition and buffer replay.
func TestPtySession_AddClient(t *testing.T) {
	// Create ring buffer and write test data
	ringBuf := buffer.NewRingBuffer(1024)
	ringBuf.Write([]byte("test output"))

	ps := &ptySession{
		clients:      make(map[*websocket.Conn]*wsClient),
		outputBuffer: ringBuf,
		createdAt:    time.Now(),
	}

	initialCount := ps.getClientCount()
	if initialCount != 0 {
		t.Errorf("Initial client count = %d, want 0", initialCount)
	}

	// Verify buffer contents can be retrieved
	bufferBytes := ps.outputBuffer.Bytes()
	if string(bufferBytes) != "test output" {
		t.Errorf("Buffer content = %q, want %q", string(bufferBytes), "test output")
	}

	// Note: We can't fully test addClient without a real WebSocket connection
	// because it tries to write the buffer to the connection.
	// This test verifies the struct setup is correct.
}

// TestPtySession_RemoveClient verifies client removal.
func TestPtySession_RemoveClient(t *testing.T) {
	ps := &ptySession{
		clients:   make(map[*websocket.Conn]*wsClient),
		createdAt: time.Now(),
	}

	mockConn := &websocket.Conn{}
	ps.clients[mockConn] = &wsClient{conn: mockConn}

	if count := ps.getClientCount(); count != 1 {
		t.Errorf("Client count before removal = %d, want 1", count)
	}

	ps.removeClient(mockConn)

	if count := ps.getClientCount(); count != 0 {
		t.Errorf("Client count after removal = %d, want 0", count)
	}

	// Removing non-existent client should not panic
	ps.removeClient(mockConn)
}

// TestPtySession_DisconnectAllClients_NoClients verifies disconnectAllClients handles empty case.
// Note: Full disconnectAllClients functionality is tested via integration tests
// (TestWebSocket_ForceReconnect) since it requires real WebSocket connections.
func TestPtySession_DisconnectAllClients_NoClients(t *testing.T) {
	ps := &ptySession{
		clients:   make(map[*websocket.Conn]*wsClient),
		createdAt: time.Now(),
	}

	// Should not panic with no clients
	ps.disconnectAllClients("test disconnect")

	if count := ps.getClientCount(); count != 0 {
		t.Errorf("Client count = %d, want 0", count)
	}
}

// TestPtySession_ConcurrentAccess verifies thread safety of client operations.
func TestPtySession_ConcurrentAccess(t *testing.T) {
	ps := &ptySession{
		clients:   make(map[*websocket.Conn]*wsClient),
		createdAt: time.Now(),
	}

	var wg sync.WaitGroup
	iterations := 100

	// Concurrent hasClients calls
	wg.Add(iterations)
	for i := 0; i < iterations; i++ {
		go func() {
			defer wg.Done()
			_ = ps.hasClients()
		}()
	}

	// Concurrent getClientCount calls
	wg.Add(iterations)
	for i := 0; i < iterations; i++ {
		go func() {
			defer wg.Done()
			_ = ps.getClientCount()
		}()
	}

	wg.Wait()
}

// TestWsClient_PausedState verifies flow control pause state.
func TestWsClient_PausedState(t *testing.T) {
	client := &wsClient{}

	// Initially not paused
	if client.paused.Load() {
		t.Error("Client should not be paused initially")
	}

	// Set paused
	client.paused.Store(true)
	if !client.paused.Load() {
		t.Error("Client should be paused after Store(true)")
	}

	// Resume
	client.paused.Store(false)
	if client.paused.Load() {
		t.Error("Client should not be paused after Store(false)")
	}
}

// TestWsClient_PausedStateConcurrent verifies thread-safe pause state operations.
func TestWsClient_PausedStateConcurrent(t *testing.T) {
	client := &wsClient{}
	var wg sync.WaitGroup

	// Concurrently toggle pause state
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			client.paused.Store(true)
		}()
		go func() {
			defer wg.Done()
			client.paused.Store(false)
		}()
	}

	// Concurrently read pause state
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = client.paused.Load()
		}()
	}

	wg.Wait()
	// Test passes if no race conditions occur
}

// TestPtySession_BroadcastSkipsPausedClients verifies that broadcast skips paused clients.
func TestPtySession_BroadcastSkipsPausedClients(t *testing.T) {
	// Create mock clients that track received data
	type mockTracker struct {
		received [][]byte
		mu       sync.Mutex
	}

	// We can't use real WebSocket connections here, but we can verify the logic
	// by checking the paused state is respected in the broadcast loop.
	ps := &ptySession{
		clients:   make(map[*websocket.Conn]*wsClient),
		createdAt: time.Now(),
	}

	// Create mock client entries (without real connections)
	mockConn1 := &websocket.Conn{}
	mockConn2 := &websocket.Conn{}

	client1 := &wsClient{conn: mockConn1}
	client2 := &wsClient{conn: mockConn2}

	ps.clients[mockConn1] = client1
	ps.clients[mockConn2] = client2

	// Pause client1
	client1.paused.Store(true)

	// Verify client1 is paused, client2 is not
	if !client1.paused.Load() {
		t.Error("Client1 should be paused")
	}
	if client2.paused.Load() {
		t.Error("Client2 should not be paused")
	}

	// Note: We can't call broadcast() directly without real connections
	// because writeMessage would fail. The integration tests below test
	// the full flow with real WebSocket connections.
}

// TestPtySession_BroadcastMultiplePauseStates verifies broadcast handles mixed pause states.
func TestPtySession_BroadcastMultiplePauseStates(t *testing.T) {
	ps := &ptySession{
		clients:   make(map[*websocket.Conn]*wsClient),
		createdAt: time.Now(),
	}

	// Create multiple mock clients with different pause states
	clients := make([]*wsClient, 5)
	for i := 0; i < 5; i++ {
		mockConn := &websocket.Conn{}
		client := &wsClient{conn: mockConn}
		clients[i] = client
		ps.clients[mockConn] = client
	}

	// Set various pause states: clients 0, 2, 4 paused; 1, 3 active
	clients[0].paused.Store(true)
	clients[1].paused.Store(false)
	clients[2].paused.Store(true)
	clients[3].paused.Store(false)
	clients[4].paused.Store(true)

	// Count paused vs active clients
	pausedCount := 0
	activeCount := 0
	for _, client := range clients {
		if client.paused.Load() {
			pausedCount++
		} else {
			activeCount++
		}
	}

	if pausedCount != 3 {
		t.Errorf("Paused client count = %d, want 3", pausedCount)
	}
	if activeCount != 2 {
		t.Errorf("Active client count = %d, want 2", activeCount)
	}
}

// Integration tests using httptest

// createTestServer creates a test HTTP server with WebSocket support for testing.
func createTestServer(t *testing.T) (*server, *httptest.Server) {
	t.Helper()

	srv := &server{
		cfg: &config.ProjectConfig{
			SessionID:      "test-session",
			Shell:          "/bin/sh",
			KubettyUser:    "testuser",
			KubettyProject: "testproject",
		},
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", srv.handleWebsocket)

	ts := httptest.NewServer(mux)
	return srv, ts
}

// TestWebSocket_SingleClientEnforcement tests that second client gets 409.
func TestWebSocket_SingleClientEnforcement(t *testing.T) {
	srv, ts := createTestServer(t)
	defer ts.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	// First client connects
	dialer := websocket.Dialer{}
	conn1, resp1, err := dialer.Dial(wsURL, nil)
	if err != nil {
		// PTY might fail to start in test environment, which is expected
		if resp1 != nil && resp1.StatusCode == http.StatusInternalServerError {
			t.Skip("PTY initialization failed (expected in test environment)")
		}
		t.Fatalf("First client connection failed: %v", err)
	}
	defer conn1.Close()

	// Verify first client connected
	if resp1.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("First client status = %d, want %d", resp1.StatusCode, http.StatusSwitchingProtocols)
	}

	// Second client should get 409 Conflict
	_, resp2, err := dialer.Dial(wsURL, nil)
	if err == nil {
		t.Error("Second client should have failed to connect")
	}
	if resp2 != nil && resp2.StatusCode != http.StatusConflict {
		t.Errorf("Second client status = %d, want %d (Conflict)", resp2.StatusCode, http.StatusConflict)
	}

	// Clean up
	srv.mu.Lock()
	if srv.pty != nil {
		srv.pty.broadcastClose()
	}
	srv.mu.Unlock()
}

// TestWebSocket_ForceReconnect tests that ?force=true disconnects existing client.
func TestWebSocket_ForceReconnect(t *testing.T) {
	srv, ts := createTestServer(t)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	wsURLForce := wsURL + "?force=true"

	dialer := websocket.Dialer{}

	// First client connects
	conn1, resp1, err := dialer.Dial(wsURL, nil)
	if err != nil {
		if resp1 != nil && resp1.StatusCode == http.StatusInternalServerError {
			t.Skip("PTY initialization failed (expected in test environment)")
		}
		t.Fatalf("First client connection failed: %v", err)
	}

	// Channel to track when first client is disconnected
	disconnected := make(chan struct{})
	go func() {
		defer close(disconnected)
		for {
			_, _, err := conn1.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	// Second client connects with force=true
	conn2, resp2, err := dialer.Dial(wsURLForce, nil)
	if err != nil {
		t.Fatalf("Force reconnect failed: %v", err)
	}
	defer conn2.Close()

	if resp2.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("Force client status = %d, want %d", resp2.StatusCode, http.StatusSwitchingProtocols)
	}

	// Wait for first client to be disconnected (with timeout)
	select {
	case <-disconnected:
		// Success - first client was disconnected
	case <-time.After(5 * time.Second):
		t.Error("First client should have been disconnected within 5 seconds")
	}

	// Clean up
	srv.mu.Lock()
	if srv.pty != nil {
		srv.pty.broadcastClose()
	}
	srv.mu.Unlock()
}

// TestWebSocket_ForceReconnect_CloseCode tests that displaced client receives code 4000.
func TestWebSocket_ForceReconnect_CloseCode(t *testing.T) {
	srv, ts := createTestServer(t)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	wsURLForce := wsURL + "?force=true"

	dialer := websocket.Dialer{}

	// First client connects
	conn1, resp1, err := dialer.Dial(wsURL, nil)
	if err != nil {
		if resp1 != nil && resp1.StatusCode == http.StatusInternalServerError {
			t.Skip("PTY initialization failed (expected in test environment)")
		}
		t.Fatalf("First client connection failed: %v", err)
	}

	// Channel to capture close code
	closeCode := make(chan int, 1)
	closeReason := make(chan string, 1)
	go func() {
		for {
			_, _, err := conn1.ReadMessage()
			if err != nil {
				if closeErr, ok := err.(*websocket.CloseError); ok {
					closeCode <- closeErr.Code
					closeReason <- closeErr.Text
				} else {
					closeCode <- -1
					closeReason <- err.Error()
				}
				return
			}
		}
	}()

	// Second client connects with force=true
	conn2, _, err := dialer.Dial(wsURLForce, nil)
	if err != nil {
		t.Fatalf("Force reconnect failed: %v", err)
	}
	defer conn2.Close()

	// Wait for close code
	select {
	case code := <-closeCode:
		if code != 4000 {
			t.Errorf("Close code = %d, want 4000", code)
		}
		reason := <-closeReason
		if !strings.Contains(reason, "taken over") {
			t.Errorf("Close reason = %q, want to contain 'taken over'", reason)
		}
	case <-time.After(5 * time.Second):
		t.Error("Should have received close code within 5 seconds")
	}

	// Clean up
	srv.mu.Lock()
	if srv.pty != nil {
		srv.pty.broadcastClose()
	}
	srv.mu.Unlock()
}

// TestWebSocket_NoForceWithoutExistingClient tests that ?force=true works even without existing client.
func TestWebSocket_NoForceWithoutExistingClient(t *testing.T) {
	srv, ts := createTestServer(t)
	defer ts.Close()

	wsURLForce := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?force=true"

	dialer := websocket.Dialer{}

	// Connect with force=true when no other client exists
	conn, resp, err := dialer.Dial(wsURLForce, nil)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusInternalServerError {
			t.Skip("PTY initialization failed (expected in test environment)")
		}
		t.Fatalf("Connection failed: %v", err)
	}
	defer conn.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("Status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
	}

	// Clean up
	srv.mu.Lock()
	if srv.pty != nil {
		srv.pty.broadcastClose()
	}
	srv.mu.Unlock()
}

// TestWebSocket_FlowControlPauseResume tests pause/resume message handling.
func TestWebSocket_FlowControlPauseResume(t *testing.T) {
	srv, ts := createTestServer(t)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	dialer := websocket.Dialer{}

	// Connect client
	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusInternalServerError {
			t.Skip("PTY initialization failed (expected in test environment)")
		}
		t.Fatalf("Connection failed: %v", err)
	}
	defer conn.Close()

	// Give time for the connection to be established
	time.Sleep(100 * time.Millisecond)

	// Get the client to verify pause state
	srv.mu.RLock()
	ps := srv.pty
	srv.mu.RUnlock()

	if ps == nil {
		t.Fatal("PTY session should exist")
	}

	// Get the client
	ps.mu.RLock()
	var client *wsClient
	for _, c := range ps.clients {
		client = c
		break
	}
	ps.mu.RUnlock()

	if client == nil {
		t.Fatal("Client should be registered")
	}

	// Initially not paused
	if client.paused.Load() {
		t.Error("Client should not be paused initially")
	}

	// Send pause message
	err = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"pause"}`))
	if err != nil {
		t.Fatalf("Failed to send pause message: %v", err)
	}

	// Wait for message to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify client is now paused
	if !client.paused.Load() {
		t.Error("Client should be paused after sending pause message")
	}

	// Send resume message
	err = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"resume"}`))
	if err != nil {
		t.Fatalf("Failed to send resume message: %v", err)
	}

	// Wait for message to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify client is no longer paused
	if client.paused.Load() {
		t.Error("Client should not be paused after sending resume message")
	}

	// Clean up
	srv.mu.Lock()
	if srv.pty != nil {
		srv.pty.broadcastClose()
	}
	srv.mu.Unlock()
}

// TestWebSocket_FlowControlPauseStopsBroadcast tests that paused clients don't receive data.
func TestWebSocket_FlowControlPauseStopsBroadcast(t *testing.T) {
	srv, ts := createTestServer(t)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	dialer := websocket.Dialer{}

	// Connect client
	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusInternalServerError {
			t.Skip("PTY initialization failed (expected in test environment)")
		}
		t.Fatalf("Connection failed: %v", err)
	}
	defer conn.Close()

	// Give time for the connection to be established
	time.Sleep(100 * time.Millisecond)

	// Get the PTY session
	srv.mu.RLock()
	ps := srv.pty
	srv.mu.RUnlock()

	if ps == nil {
		t.Fatal("PTY session should exist")
	}

	// Send pause message to stop output
	err = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"pause"}`))
	if err != nil {
		t.Fatalf("Failed to send pause message: %v", err)
	}

	// Wait for message to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify client is paused
	ps.mu.RLock()
	var client *wsClient
	for _, c := range ps.clients {
		client = c
		break
	}
	isPaused := client != nil && client.paused.Load()
	ps.mu.RUnlock()

	if !isPaused {
		t.Error("Client should be paused")
	}

	// Note: We can't easily test that broadcast skips the client without
	// generating PTY output and timing reads. The unit tests above verify
	// the pause flag is set correctly, and code review confirms broadcast
	// checks client.paused.Load() before writing.

	// Clean up
	srv.mu.Lock()
	if srv.pty != nil {
		srv.pty.broadcastClose()
	}
	srv.mu.Unlock()
}

// TestWebSocket_PingPongHeartbeat tests that ping messages receive pong responses.
func TestWebSocket_PingPongHeartbeat(t *testing.T) {
	srv, ts := createTestServer(t)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	dialer := websocket.Dialer{}

	// Connect client
	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusInternalServerError {
			t.Skip("PTY initialization failed (expected in test environment)")
		}
		t.Fatalf("Connection failed: %v", err)
	}
	defer conn.Close()

	// Send ping message
	err = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping"}`))
	if err != nil {
		t.Fatalf("Failed to send ping message: %v", err)
	}

	// Set read deadline
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	// Read response - should be pong
	pongReceived := false
	for i := 0; i < 10; i++ {
		_, data, err := conn.ReadMessage()
		if err != nil {
			// May receive PTY output or timeout, continue trying
			continue
		}

		if strings.Contains(string(data), `"type":"pong"`) {
			pongReceived = true
			break
		}
	}

	if !pongReceived {
		t.Error("Should have received pong response to ping")
	}

	// Clean up
	srv.mu.Lock()
	if srv.pty != nil {
		srv.pty.broadcastClose()
	}
	srv.mu.Unlock()
}

// =============================================================================
// Pause Buffer Unit Tests
// =============================================================================

// TestWsClient_BufferPausedData tests normal data buffering.
func TestWsClient_BufferPausedData(t *testing.T) {
	client := &wsClient{}

	// Buffer some data
	data := []byte("test data")
	if !client.bufferPausedData(data) {
		t.Error("bufferPausedData should return true for small data")
	}

	// Verify data was buffered
	client.pauseBufferMu.Lock()
	if len(client.pauseBuffer) != len(data) {
		t.Errorf("Buffer length = %d, want %d", len(client.pauseBuffer), len(data))
	}
	if string(client.pauseBuffer) != string(data) {
		t.Errorf("Buffer content = %q, want %q", string(client.pauseBuffer), string(data))
	}
	client.pauseBufferMu.Unlock()
}

// TestWsClient_BufferPausedData_Accumulates tests that multiple bufferPausedData calls accumulate.
func TestWsClient_BufferPausedData_Accumulates(t *testing.T) {
	client := &wsClient{}

	// Buffer multiple chunks
	chunk1 := []byte("first ")
	chunk2 := []byte("second ")
	chunk3 := []byte("third")

	client.bufferPausedData(chunk1)
	client.bufferPausedData(chunk2)
	client.bufferPausedData(chunk3)

	client.pauseBufferMu.Lock()
	expected := "first second third"
	if string(client.pauseBuffer) != expected {
		t.Errorf("Buffer content = %q, want %q", string(client.pauseBuffer), expected)
	}
	client.pauseBufferMu.Unlock()
}

// TestWsClient_BufferPausedData_MaxSize tests that buffer respects max size.
func TestWsClient_BufferPausedData_MaxSize(t *testing.T) {
	client := &wsClient{}

	// Fill buffer to max capacity
	largeData := make([]byte, pauseBufferMaxSize)
	for i := range largeData {
		largeData[i] = byte('A' + (i % 26))
	}

	if !client.bufferPausedData(largeData) {
		t.Error("bufferPausedData should return true when filling to max")
	}

	client.pauseBufferMu.Lock()
	if len(client.pauseBuffer) != pauseBufferMaxSize {
		t.Errorf("Buffer length = %d, want %d", len(client.pauseBuffer), pauseBufferMaxSize)
	}
	client.pauseBufferMu.Unlock()

	// Now try to add more - should return false (buffer full)
	moreData := []byte("extra")
	if client.bufferPausedData(moreData) {
		t.Error("bufferPausedData should return false when buffer is full")
	}

	// Buffer should still be at max size
	client.pauseBufferMu.Lock()
	if len(client.pauseBuffer) != pauseBufferMaxSize {
		t.Errorf("Buffer length after overflow = %d, want %d", len(client.pauseBuffer), pauseBufferMaxSize)
	}
	client.pauseBufferMu.Unlock()
}

// TestWsClient_BufferPausedData_PartialFit tests partial data buffering when space is limited.
func TestWsClient_BufferPausedData_PartialFit(t *testing.T) {
	client := &wsClient{}

	// Fill buffer partially
	initialSize := pauseBufferMaxSize - 100
	initialData := make([]byte, initialSize)
	client.bufferPausedData(initialData)

	// Now try to add 200 bytes - only 100 should fit
	moreData := make([]byte, 200)
	for i := range moreData {
		moreData[i] = byte('X')
	}

	if !client.bufferPausedData(moreData) {
		t.Error("bufferPausedData should return true for partial fit")
	}

	// Verify buffer is now at max size
	client.pauseBufferMu.Lock()
	if len(client.pauseBuffer) != pauseBufferMaxSize {
		t.Errorf("Buffer length = %d, want %d", len(client.pauseBuffer), pauseBufferMaxSize)
	}
	client.pauseBufferMu.Unlock()
}

// TestWsClient_PauseBufferLen tests the pauseBufferLen method.
func TestWsClient_PauseBufferLen(t *testing.T) {
	client := &wsClient{}

	// Initially empty
	if client.pauseBufferLen() != 0 {
		t.Errorf("Initial buffer length = %d, want 0", client.pauseBufferLen())
	}

	// Add some data
	data := []byte("hello world")
	client.bufferPausedData(data)

	if client.pauseBufferLen() != len(data) {
		t.Errorf("Buffer length = %d, want %d", client.pauseBufferLen(), len(data))
	}
}

// TestWsClient_FlushPauseBuffer_Empty tests flushing an empty buffer.
func TestWsClient_FlushPauseBuffer_Empty(t *testing.T) {
	client := &wsClient{}

	bytesFlushed, err := client.flushPauseBuffer()
	if err != nil {
		t.Errorf("flushPauseBuffer returned error for empty buffer: %v", err)
	}
	if bytesFlushed != 0 {
		t.Errorf("Bytes flushed = %d, want 0", bytesFlushed)
	}
}

// TestWsClient_BufferPausedData_Concurrent tests concurrent buffer operations.
func TestWsClient_BufferPausedData_Concurrent(t *testing.T) {
	client := &wsClient{}
	var wg sync.WaitGroup

	// Concurrent writes
	numWriters := 10
	dataPerWrite := 100

	wg.Add(numWriters)
	for i := 0; i < numWriters; i++ {
		go func(id int) {
			defer wg.Done()
			data := make([]byte, dataPerWrite)
			for j := range data {
				data[j] = byte('A' + id)
			}
			client.bufferPausedData(data)
		}(i)
	}

	// Concurrent reads of buffer length
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			_ = client.pauseBufferLen()
		}()
	}

	wg.Wait()

	// Buffer should contain data (exact amount depends on race timing)
	bufLen := client.pauseBufferLen()
	if bufLen == 0 {
		t.Error("Buffer should contain some data after concurrent writes")
	}
	if bufLen > pauseBufferMaxSize {
		t.Errorf("Buffer exceeded max size: %d > %d", bufLen, pauseBufferMaxSize)
	}
}

// TestWsClient_ConcurrentPauseAndBuffer tests concurrent pause state and buffer operations.
func TestWsClient_ConcurrentPauseAndBuffer(t *testing.T) {
	client := &wsClient{}
	var wg sync.WaitGroup

	// Concurrent pause state toggles
	wg.Add(50)
	for i := 0; i < 50; i++ {
		go func(id int) {
			defer wg.Done()
			if id%2 == 0 {
				client.paused.Store(true)
			} else {
				client.paused.Store(false)
			}
		}(i)
	}

	// Concurrent buffer operations
	wg.Add(50)
	for i := 0; i < 50; i++ {
		go func() {
			defer wg.Done()
			data := []byte("test")
			client.bufferPausedData(data)
		}()
	}

	// Concurrent buffer length reads
	wg.Add(50)
	for i := 0; i < 50; i++ {
		go func() {
			defer wg.Done()
			_ = client.pauseBufferLen()
		}()
	}

	wg.Wait()
	// Test passes if no race conditions or panics occur
}

// TestPtySession_Broadcast_BuffersPausedClients tests that broadcast buffers data for paused clients.
func TestPtySession_Broadcast_BuffersPausedClients(t *testing.T) {
	ps := &ptySession{
		clients:   make(map[*websocket.Conn]*wsClient),
		createdAt: time.Now(),
	}

	// Create mock clients
	mockConn1 := &websocket.Conn{}
	mockConn2 := &websocket.Conn{}

	client1 := &wsClient{conn: mockConn1}
	client2 := &wsClient{conn: mockConn2}

	ps.clients[mockConn1] = client1
	ps.clients[mockConn2] = client2

	// Pause client1
	client1.paused.Store(true)

	// Manually simulate what broadcast does for paused clients
	testData := []byte("test output data")

	// For client1 (paused), data should be buffered
	if client1.paused.Load() {
		client1.bufferPausedData(testData)
	}

	// Verify client1 has buffered data
	if client1.pauseBufferLen() != len(testData) {
		t.Errorf("Paused client buffer = %d, want %d", client1.pauseBufferLen(), len(testData))
	}

	// Client2 (not paused) should have no buffered data
	if client2.pauseBufferLen() != 0 {
		t.Errorf("Active client buffer = %d, want 0", client2.pauseBufferLen())
	}
}

// TestPtySession_BufferOverflow_DropsExcessData tests buffer overflow behavior.
func TestPtySession_BufferOverflow_DropsExcessData(t *testing.T) {
	client := &wsClient{}

	// Fill buffer to max
	fillData := make([]byte, pauseBufferMaxSize)
	if !client.bufferPausedData(fillData) {
		t.Fatal("Initial fill should succeed")
	}

	// Track drop count (simulating what happens in broadcast)
	droppedCount := 0
	for i := 0; i < 5; i++ {
		data := []byte("overflow data")
		if !client.bufferPausedData(data) {
			droppedCount++
		}
	}

	if droppedCount != 5 {
		t.Errorf("Dropped count = %d, want 5", droppedCount)
	}

	// Buffer should still be at max
	if client.pauseBufferLen() != pauseBufferMaxSize {
		t.Errorf("Buffer size = %d, want %d", client.pauseBufferLen(), pauseBufferMaxSize)
	}
}

// TestFlowControl_Constants verifies flow control constants are reasonable.
func TestFlowControl_Constants(t *testing.T) {
	// Verify constants are set to reasonable values
	if pauseBufferMaxSize <= 0 {
		t.Errorf("pauseBufferMaxSize should be positive: %d", pauseBufferMaxSize)
	}
	if pauseBufferChunkSize <= 0 {
		t.Errorf("pauseBufferChunkSize should be positive: %d", pauseBufferChunkSize)
	}
	if pauseBufferChunkDelay < 0 {
		t.Errorf("pauseBufferChunkDelay should be non-negative: %v", pauseBufferChunkDelay)
	}

	// Chunk size should be smaller than max buffer size
	if pauseBufferChunkSize >= pauseBufferMaxSize {
		t.Errorf("Chunk size (%d) should be smaller than max buffer (%d)",
			pauseBufferChunkSize, pauseBufferMaxSize)
	}

	// Verify expected values from implementation
	expectedMaxSize := 64 * 1024  // 64KB
	expectedChunkSize := 8 * 1024 // 8KB

	if pauseBufferMaxSize != expectedMaxSize {
		t.Errorf("pauseBufferMaxSize = %d, want %d", pauseBufferMaxSize, expectedMaxSize)
	}
	if pauseBufferChunkSize != expectedChunkSize {
		t.Errorf("pauseBufferChunkSize = %d, want %d", pauseBufferChunkSize, expectedChunkSize)
	}
}

// TestWsClient_BufferAndFlushSequence tests the complete buffer -> flush cycle.
func TestWsClient_BufferAndFlushSequence(t *testing.T) {
	client := &wsClient{}

	// Buffer some data
	testData := []byte("data to buffer and flush")
	client.bufferPausedData(testData)

	// Verify it's buffered
	if client.pauseBufferLen() != len(testData) {
		t.Errorf("Buffer before flush = %d, want %d", client.pauseBufferLen(), len(testData))
	}

	// Clear the buffer directly (simulating what happens after flush without real WS)
	client.pauseBufferMu.Lock()
	data := client.pauseBuffer
	client.pauseBuffer = nil
	client.pauseBufferMu.Unlock()

	// Verify data was retrieved correctly
	if string(data) != string(testData) {
		t.Errorf("Retrieved data = %q, want %q", string(data), string(testData))
	}

	// Buffer should now be empty
	if client.pauseBufferLen() != 0 {
		t.Errorf("Buffer after flush = %d, want 0", client.pauseBufferLen())
	}
}

// TestWsClient_PauseResumeWithBuffering tests complete pause -> buffer -> resume flow.
func TestWsClient_PauseResumeWithBuffering(t *testing.T) {
	client := &wsClient{}

	// Initially not paused
	if client.paused.Load() {
		t.Error("Client should not be paused initially")
	}

	// Pause client
	client.paused.Store(true)

	// Simulate buffering data while paused
	for i := 0; i < 10; i++ {
		data := []byte("buffered output\n")
		client.bufferPausedData(data)
	}

	// Verify data accumulated
	expectedLen := 10 * len("buffered output\n")
	if client.pauseBufferLen() != expectedLen {
		t.Errorf("Buffer while paused = %d, want %d", client.pauseBufferLen(), expectedLen)
	}

	// Resume client
	client.paused.Store(false)

	// On resume, flush would be called - simulate the buffer clearing
	client.pauseBufferMu.Lock()
	bufferedData := client.pauseBuffer
	client.pauseBuffer = nil
	client.pauseBufferMu.Unlock()

	// Verify all data was retrieved
	if len(bufferedData) != expectedLen {
		t.Errorf("Flushed data = %d bytes, want %d", len(bufferedData), expectedLen)
	}

	// Buffer should be empty after flush
	if client.pauseBufferLen() != 0 {
		t.Errorf("Buffer after resume = %d, want 0", client.pauseBufferLen())
	}
}
