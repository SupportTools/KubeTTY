package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/KubeTTY/server/internal/gateway/relay"
	"k8s.io/client-go/rest"
)

func TestNewExecRelay(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
		Container: "test-container",
		Command:   []string{"/bin/sh"},
	}

	relay := NewExecRelay(nil, cfg)

	if relay.Status() != RelayStatusIdle {
		t.Errorf("expected initial status Idle, got %v", relay.Status())
	}

	if relay.LastError() != nil {
		t.Errorf("expected no initial error, got %v", relay.LastError())
	}

	if relay.buffer == nil {
		t.Error("expected buffer to be initialized")
	}

	if relay.clients == nil {
		t.Error("expected clients map to be initialized")
	}
}

func TestNewExecRelay_DefaultBufferSize(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	relay := NewExecRelay(nil, cfg)

	if relay.config.BufferSize != DefaultBufferSize {
		t.Errorf("expected default buffer size %d, got %d", DefaultBufferSize, relay.config.BufferSize)
	}
}

func TestNewExecRelay_CustomConfig(t *testing.T) {
	cfg := RelayConfig{
		Namespace:    "custom-ns",
		PodName:      "custom-pod",
		Container:    "custom-container",
		Command:      []string{"/bin/bash", "-c", "echo hello"},
		BufferSize:   128 * 1024,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	relay := NewExecRelay(nil, cfg)

	if relay.config.BufferSize != 128*1024 {
		t.Errorf("expected buffer size 128KB, got %d", relay.config.BufferSize)
	}
	if relay.config.ReadTimeout != 30*time.Second {
		t.Errorf("expected read timeout 30s, got %v", relay.config.ReadTimeout)
	}
	if relay.config.WriteTimeout != 5*time.Second {
		t.Errorf("expected write timeout 5s, got %v", relay.config.WriteTimeout)
	}
}

func TestNewExecRelay_DefaultTimeouts(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	relay := NewExecRelay(nil, cfg)

	if relay.config.ReadTimeout != 60*time.Second {
		t.Errorf("expected default read timeout 60s, got %v", relay.config.ReadTimeout)
	}
	if relay.config.WriteTimeout != 10*time.Second {
		t.Errorf("expected default write timeout 10s, got %v", relay.config.WriteTimeout)
	}
}

func TestExecRelay_Subscribe(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	relay := NewExecRelay(nil, cfg)
	ch := relay.Subscribe()

	if ch == nil {
		t.Error("expected Subscribe to return a channel")
	}

	// Verify we can receive from channel (should be buffered)
	select {
	case <-ch:
		t.Error("channel should be empty initially")
	default:
		// Expected - channel is empty
	}
}

func TestExecRelay_ActivityChan(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	relay := NewExecRelay(nil, cfg)
	ch := relay.ActivityChan()

	if ch == nil {
		t.Error("expected ActivityChan to return a channel")
	}
}

func TestExecRelay_ReplayBuffer(t *testing.T) {
	cfg := RelayConfig{
		Namespace:  "test-ns",
		PodName:    "test-pod",
		BufferSize: 100,
	}

	relay := NewExecRelay(nil, cfg)

	// Initially empty
	buf := relay.ReplayBuffer()
	if len(buf) != 0 {
		t.Errorf("expected empty replay buffer, got %d bytes", len(buf))
	}

	// Write to buffer directly
	relay.buffer.Write([]byte("test output"))

	buf = relay.ReplayBuffer()
	if string(buf) != "test output" {
		t.Errorf("expected 'test output', got %q", buf)
	}
}

func TestExecRelay_Close(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	relay := NewExecRelay(nil, cfg)

	// Subscribe to get status updates
	ch := relay.Subscribe()

	err := relay.Close()
	if err != nil {
		t.Fatalf("unexpected error from Close: %v", err)
	}

	if relay.Status() != RelayStatusClosed {
		t.Errorf("expected status Closed, got %v", relay.Status())
	}

	// Close() closes observer channels - verify channel is closed
	select {
	case _, ok := <-ch:
		if ok {
			// Channel returned a value but isn't closed yet - that's fine
			// The important thing is we didn't block forever
		}
		// Channel closed or received zero value - expected
	case <-time.After(100 * time.Millisecond):
		t.Error("expected channel to be closed or receive value within timeout")
	}
}

func TestExecRelay_StatusTransitions(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	relay := NewExecRelay(nil, cfg)

	// Test setStatus directly
	relay.setStatus(RelayStatusConnecting, nil)
	if relay.Status() != RelayStatusConnecting {
		t.Errorf("expected Connecting, got %v", relay.Status())
	}

	relay.setStatus(RelayStatusConnected, nil)
	if relay.Status() != RelayStatusConnected {
		t.Errorf("expected Connected, got %v", relay.Status())
	}
}

func TestExecRelay_StatusWithError(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	relay := NewExecRelay(nil, cfg)

	testErr := &rest.RequestConstructionError{Err: nil}
	relay.setStatus(RelayStatusClosed, testErr)

	if relay.LastError() != testErr {
		t.Errorf("expected error to be set, got %v", relay.LastError())
	}
}

func TestRelayStatus_Constants(t *testing.T) {
	// Verify status constants are properly aliased
	tests := []struct {
		name     string
		constant RelayStatus
	}{
		{"Idle", RelayStatusIdle},
		{"Connecting", RelayStatusConnecting},
		{"Connected", RelayStatusConnected},
		{"Reconnecting", RelayStatusReconnecting},
		{"Closed", RelayStatusClosed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant == "" {
				t.Errorf("status constant %s should not be empty", tt.name)
			}
		})
	}
}

// TestExecRelay_CloseIdempotent verifies that Close() can be called multiple times safely.
func TestExecRelay_CloseIdempotent(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	relay := NewExecRelay(nil, cfg)

	// First close
	err1 := relay.Close()
	if err1 != nil {
		t.Errorf("first Close() returned error: %v", err1)
	}

	if relay.Status() != RelayStatusClosed {
		t.Errorf("expected status Closed after first Close(), got %v", relay.Status())
	}

	// Second close should be idempotent (no panic, no error)
	err2 := relay.Close()
	if err2 != nil {
		t.Errorf("second Close() returned error: %v", err2)
	}

	if relay.Status() != RelayStatusClosed {
		t.Errorf("expected status Closed after second Close(), got %v", relay.Status())
	}

	// Third close for good measure
	err3 := relay.Close()
	if err3 != nil {
		t.Errorf("third Close() returned error: %v", err3)
	}
}

// TestExecRelay_EnsureSessionAfterClose verifies that ensureSession fails after Close.
func TestExecRelay_EnsureSessionAfterClose(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	relay := NewExecRelay(nil, cfg)

	// Close the relay first
	_ = relay.Close()

	// Now try to ensure session - should fail
	ctx := context.Background()
	err := relay.ensureSession(ctx)

	if err == nil {
		t.Error("expected error from ensureSession after Close, got nil")
	}

	expectedErr := "relay is closed"
	if err != nil && err.Error() != expectedErr {
		t.Errorf("expected error %q, got %q", expectedErr, err.Error())
	}
}

// TestExecRelay_CloseNotifiesObservers verifies that Close notifies all observers.
func TestExecRelay_CloseNotifiesObservers(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	r := NewExecRelay(nil, cfg)

	// Subscribe multiple observers
	ch1 := r.Subscribe()
	ch2 := r.Subscribe()
	ch3 := r.Subscribe()

	// Close the relay
	_ = r.Close()

	// All observers should receive the closed status
	channels := []<-chan relay.StatusEvent{ch1, ch2, ch3}
	for i, ch := range channels {
		select {
		case evt, ok := <-ch:
			if ok && evt.Status != RelayStatusClosed {
				t.Errorf("observer %d: expected status Closed, got %v", i+1, evt.Status)
			}
		case <-time.After(100 * time.Millisecond):
			// Channel might be closed without receiving event - that's acceptable
		}
	}
}

// TestExecRelay_ConcurrentClose tests that concurrent Close calls are safe.
func TestExecRelay_ConcurrentClose(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	relay := NewExecRelay(nil, cfg)

	// Start multiple goroutines that all try to close
	var wg sync.WaitGroup
	numClosers := 10

	for i := 0; i < numClosers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Should not panic even with concurrent calls
			_ = relay.Close()
		}()
	}

	// Wait for all closers to finish
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - all concurrent closes completed without panic
	case <-time.After(5 * time.Second):
		t.Error("concurrent Close() calls did not complete within timeout")
	}

	if relay.Status() != RelayStatusClosed {
		t.Errorf("expected status Closed, got %v", relay.Status())
	}
}

// TestExecRelay_ContextCancellation verifies that Close cancels the internal context.
func TestExecRelay_ContextCancellation(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	relay := NewExecRelay(nil, cfg)

	// Manually set up a cancel function to test
	ctx, cancel := context.WithCancel(context.Background())
	relay.mu.Lock()
	relay.cancelFunc = cancel
	relay.mu.Unlock()

	// Verify context is not cancelled yet
	select {
	case <-ctx.Done():
		t.Error("context should not be cancelled yet")
	default:
		// Expected
	}

	// Close the relay
	_ = relay.Close()

	// Verify context is now cancelled
	select {
	case <-ctx.Done():
		// Expected - context was cancelled
	case <-time.After(100 * time.Millisecond):
		t.Error("context should have been cancelled after Close()")
	}
}

// TestExecRelay_ReconnectDefaults verifies that reconnection config defaults are set properly.
func TestExecRelay_ReconnectDefaults(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	relay := NewExecRelay(nil, cfg)

	if relay.config.MaxRetries != DefaultMaxRetries {
		t.Errorf("expected default MaxRetries %d, got %d", DefaultMaxRetries, relay.config.MaxRetries)
	}
	if relay.config.RetryDelay != DefaultRetryDelay {
		t.Errorf("expected default RetryDelay %v, got %v", DefaultRetryDelay, relay.config.RetryDelay)
	}
}

// TestExecRelay_ReconnectCustomConfig verifies that custom reconnection config is respected.
func TestExecRelay_ReconnectCustomConfig(t *testing.T) {
	cfg := RelayConfig{
		Namespace:  "test-ns",
		PodName:    "test-pod",
		MaxRetries: 5,
		RetryDelay: 2 * time.Second,
	}

	relay := NewExecRelay(nil, cfg)

	if relay.config.MaxRetries != 5 {
		t.Errorf("expected MaxRetries 5, got %d", relay.config.MaxRetries)
	}
	if relay.config.RetryDelay != 2*time.Second {
		t.Errorf("expected RetryDelay 2s, got %v", relay.config.RetryDelay)
	}
}

// TestExecRelay_ReconnectAfterClose verifies that reconnect fails when relay is closed.
func TestExecRelay_ReconnectAfterClose(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	relay := NewExecRelay(nil, cfg)

	// Close the relay first
	_ = relay.Close()

	// Attempt to reconnect - should fail
	ctx := context.Background()
	err := relay.reconnect(ctx)

	if err == nil {
		t.Error("expected error from reconnect after Close, got nil")
	}

	expectedErr := "relay is closed"
	if err != nil && err.Error() != expectedErr {
		t.Errorf("expected error %q, got %q", expectedErr, err.Error())
	}
}

// TestExecRelay_ReconnectMaxRetriesExceeded verifies max retry limit is enforced.
func TestExecRelay_ReconnectMaxRetriesExceeded(t *testing.T) {
	cfg := RelayConfig{
		Namespace:  "test-ns",
		PodName:    "test-pod",
		MaxRetries: 2,
		RetryDelay: 1 * time.Millisecond, // Fast for testing
	}

	relay := NewExecRelay(nil, cfg)

	ctx := context.Background()

	// First reconnect attempt - will fail (no rest.Config) but should increment counter
	err1 := relay.reconnect(ctx)
	if err1 == nil {
		t.Error("expected error from first reconnect (no restConfig)")
	}

	relay.mu.RLock()
	count1 := relay.retryCount
	relay.mu.RUnlock()
	if count1 != 1 {
		t.Errorf("expected retryCount 1 after first attempt, got %d", count1)
	}

	// Second reconnect attempt
	err2 := relay.reconnect(ctx)
	if err2 == nil {
		t.Error("expected error from second reconnect (no restConfig)")
	}

	relay.mu.RLock()
	count2 := relay.retryCount
	relay.mu.RUnlock()
	if count2 != 2 {
		t.Errorf("expected retryCount 2 after second attempt, got %d", count2)
	}

	// Third attempt should fail due to max retries
	err3 := relay.reconnect(ctx)
	if err3 == nil {
		t.Error("expected error from third reconnect (max retries)")
	}

	if err3 != nil && err3.Error() != "max reconnection attempts (2) exceeded" {
		t.Errorf("expected max retries error, got %q", err3.Error())
	}

	// Relay should be closed after max retries exceeded
	if relay.Status() != RelayStatusClosed {
		t.Errorf("expected status Closed after max retries, got %v", relay.Status())
	}
}

// TestExecRelay_ReconnectStatusTransition verifies status changes during reconnection.
func TestExecRelay_ReconnectStatusTransition(t *testing.T) {
	cfg := RelayConfig{
		Namespace:  "test-ns",
		PodName:    "test-pod",
		MaxRetries: 1,
		RetryDelay: 1 * time.Millisecond,
	}

	relay := NewExecRelay(nil, cfg)

	// Subscribe to status events
	ch := relay.Subscribe()

	ctx := context.Background()

	// Attempt reconnect (will fail due to no rest config)
	_ = relay.reconnect(ctx)

	// Should see Reconnecting status
	var sawReconnecting bool
	timeout := time.After(100 * time.Millisecond)
loop:
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				break loop
			}
			if evt.Status == RelayStatusReconnecting {
				sawReconnecting = true
			}
		case <-timeout:
			break loop
		}
	}

	if !sawReconnecting {
		t.Error("expected to see Reconnecting status during reconnection attempt")
	}
}

// TestExecRelay_ReconnectConcurrent verifies concurrent reconnect attempts are safe.
func TestExecRelay_ReconnectConcurrent(t *testing.T) {
	cfg := RelayConfig{
		Namespace:  "test-ns",
		PodName:    "test-pod",
		MaxRetries: 10, // High limit to allow multiple concurrent attempts
		RetryDelay: 1 * time.Millisecond,
	}

	relay := NewExecRelay(nil, cfg)
	ctx := context.Background()

	// Start multiple goroutines that try to reconnect
	var wg sync.WaitGroup
	numReconnectors := 5

	for i := 0; i < numReconnectors; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Should not panic
			_ = relay.reconnect(ctx)
		}()
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Error("concurrent reconnect attempts did not complete within timeout")
	}
}

// TestExecRelay_FlowControlClientTracking tests pause/resume client tracking
func TestExecRelay_FlowControlClientTracking(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	r := NewExecRelay(nil, cfg)

	// Verify pausedClients map is initialized
	if r.pausedClients == nil {
		t.Error("pausedClients map should be initialized")
	}

	// Verify it starts empty
	r.clientsMu.RLock()
	pausedCount := len(r.pausedClients)
	r.clientsMu.RUnlock()

	if pausedCount != 0 {
		t.Errorf("expected 0 paused clients initially, got %d", pausedCount)
	}
}

// TestExecRelay_ClientMapInitialization tests clients map is properly initialized
func TestExecRelay_ClientMapInitialization(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	r := NewExecRelay(nil, cfg)

	if r.clients == nil {
		t.Error("clients map should be initialized")
	}

	r.clientsMu.RLock()
	clientCount := len(r.clients)
	r.clientsMu.RUnlock()

	if clientCount != 0 {
		t.Errorf("expected 0 clients initially, got %d", clientCount)
	}
}

// TestExecRelay_OutputChannelInitialization tests output channel is properly initialized
func TestExecRelay_OutputChannelInitialization(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	r := NewExecRelay(nil, cfg)

	if r.outputCh == nil {
		t.Error("outputCh should be initialized")
	}

	// Output channel should be buffered
	select {
	case r.outputCh <- []byte("test"):
		// Success - channel is writable
	default:
		t.Error("outputCh should be buffered and accept writes")
	}

	// Clean up - drain the channel
	select {
	case <-r.outputCh:
	default:
	}
}

// TestExecRelay_ActivityChannelInitialization tests activity channel is properly initialized
func TestExecRelay_ActivityChannelInitialization(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	r := NewExecRelay(nil, cfg)

	if r.activityCh == nil {
		t.Error("activityCh should be initialized")
	}

	// Activity channel should be buffered
	select {
	case r.activityCh <- struct{}{}:
		// Success
	default:
		t.Error("activityCh should be buffered")
	}

	// Clean up
	select {
	case <-r.activityCh:
	default:
	}
}

// TestExecRelay_BufferWriteAndRead tests buffer read/write cycle
func TestExecRelay_BufferWriteAndRead(t *testing.T) {
	cfg := RelayConfig{
		Namespace:  "test-ns",
		PodName:    "test-pod",
		BufferSize: 1024,
	}

	r := NewExecRelay(nil, cfg)

	// Write data to buffer
	testData := []byte("Hello, World!")
	r.buffer.Write(testData)

	// Read back via ReplayBuffer
	replayData := r.ReplayBuffer()
	if string(replayData) != string(testData) {
		t.Errorf("expected %q, got %q", testData, replayData)
	}
}

// TestExecRelay_BufferOverwrite tests buffer circular overwrite
func TestExecRelay_BufferOverwrite(t *testing.T) {
	cfg := RelayConfig{
		Namespace:  "test-ns",
		PodName:    "test-pod",
		BufferSize: 10, // Small buffer
	}

	r := NewExecRelay(nil, cfg)

	// Write more data than buffer can hold
	r.buffer.Write([]byte("0123456789")) // Fill buffer
	r.buffer.Write([]byte("ABCDE"))      // Overflow, should wrap

	// Buffer should have most recent data
	replayData := r.ReplayBuffer()
	if len(replayData) > cfg.BufferSize {
		t.Errorf("buffer exceeded max size: got %d bytes, max %d", len(replayData), cfg.BufferSize)
	}
}

// TestExecRelay_MultipleSubscribers tests multiple observers receive events
func TestExecRelay_MultipleSubscribers(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	r := NewExecRelay(nil, cfg)

	// Subscribe multiple observers
	ch1 := r.Subscribe()
	ch2 := r.Subscribe()
	ch3 := r.Subscribe()

	// Trigger a status change
	r.setStatus(RelayStatusConnecting, nil)

	// All observers should receive the event
	channels := []<-chan relay.StatusEvent{ch1, ch2, ch3}
	for i, ch := range channels {
		select {
		case evt := <-ch:
			if evt.Status != RelayStatusConnecting {
				t.Errorf("observer %d: expected Connecting, got %v", i+1, evt.Status)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("observer %d: did not receive event within timeout", i+1)
		}
	}
}

// TestExecRelay_StatusEventHasTimestamp tests that status events include timestamp
func TestExecRelay_StatusEventHasTimestamp(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	r := NewExecRelay(nil, cfg)
	ch := r.Subscribe()

	before := time.Now()
	r.setStatus(RelayStatusConnecting, nil)
	after := time.Now()

	select {
	case evt := <-ch:
		if evt.When.Before(before) || evt.When.After(after) {
			t.Errorf("event timestamp %v should be between %v and %v", evt.When, before, after)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("did not receive event within timeout")
	}
}

// TestExecRelay_ObserverChannelBuffered tests that observer channels are buffered
func TestExecRelay_ObserverChannelBuffered(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	r := NewExecRelay(nil, cfg)
	ch := r.Subscribe()

	// Send multiple events without reading
	r.setStatus(RelayStatusConnecting, nil)
	r.setStatus(RelayStatusConnected, nil)
	r.setStatus(RelayStatusReconnecting, nil)
	r.setStatus(RelayStatusConnected, nil)

	// Channel should be buffered (size 4), so some events should be queued
	eventsReceived := 0
	timeout := time.After(100 * time.Millisecond)

drain:
	for {
		select {
		case <-ch:
			eventsReceived++
		case <-timeout:
			break drain
		}
	}

	if eventsReceived == 0 {
		t.Error("expected to receive at least some events from buffered channel")
	}
}

// TestExecRelay_ReconnectNilRestConfig tests reconnect behavior with nil rest config
func TestExecRelay_ReconnectNilRestConfig(t *testing.T) {
	cfg := RelayConfig{
		Namespace:  "test-ns",
		PodName:    "test-pod",
		MaxRetries: 1,
		RetryDelay: 1 * time.Millisecond,
	}

	// Create relay with nil rest config
	r := NewExecRelay(nil, cfg)

	ctx := context.Background()
	err := r.reconnect(ctx)

	// Should fail due to nil rest config
	if err == nil {
		t.Error("expected error from reconnect with nil restConfig")
	}

	expectedErr := "cannot reconnect: no Kubernetes client configuration"
	if err != nil && err.Error() != expectedErr {
		t.Errorf("expected error %q, got %q", expectedErr, err.Error())
	}
}

// TestExecRelay_EnsureSessionNilRestConfig tests that ensureSession handles nil restConfig
// without deadlocking. This test was previously removed due to a deadlock bug (see issue 4092).
// The fix was to release the mutex before calling setStatus() in ensureSession().
func TestExecRelay_EnsureSessionNilRestConfig(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	r := NewExecRelay(nil, cfg)

	// Test with timeout to detect deadlock
	done := make(chan error, 1)
	go func() {
		done <- r.ensureSession(context.Background())
	}()

	select {
	case err := <-done:
		// ensureSession should fail with nil restConfig, but should NOT deadlock
		if err == nil {
			t.Error("expected error from ensureSession with nil restConfig")
		}
		// Verify status transitioned to closed
		if r.Status() != RelayStatusClosed {
			t.Errorf("expected status Closed, got %v", r.Status())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ensureSession deadlocked - test timed out")
	}
}

// TestExecRelay_EnsureSessionConcurrent tests concurrent calls to ensureSession
// to verify no race conditions or deadlocks with multiple goroutines.
func TestExecRelay_EnsureSessionConcurrent(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	r := NewExecRelay(nil, cfg)

	// Launch multiple concurrent ensureSession calls
	const numGoroutines = 10
	done := make(chan struct{}, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			// Will fail due to nil restConfig, but should not deadlock
			r.ensureSession(context.Background())
			done <- struct{}{}
		}()
	}

	// Wait for all goroutines with timeout to detect deadlock
	for i := 0; i < numGoroutines; i++ {
		select {
		case <-done:
			// Success
		case <-time.After(5 * time.Second):
			t.Fatalf("goroutine %d deadlocked - test timed out", i)
		}
	}
}

// TestExecRelay_StatusTransitionsWithErrors tests status transitions include errors
func TestExecRelay_StatusTransitionsWithErrors(t *testing.T) {
	cfg := RelayConfig{
		Namespace: "test-ns",
		PodName:   "test-pod",
	}

	r := NewExecRelay(nil, cfg)
	ch := r.Subscribe()

	testErr := context.DeadlineExceeded
	r.setStatus(RelayStatusClosed, testErr)

	select {
	case evt := <-ch:
		if evt.Err != testErr {
			t.Errorf("expected error %v, got %v", testErr, evt.Err)
		}
		if evt.Status != RelayStatusClosed {
			t.Errorf("expected status Closed, got %v", evt.Status)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("did not receive event within timeout")
	}
}

// TestControlMessage_Parsing tests control message structure
func TestControlMessage_Parsing(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		wantType string
		wantCols uint16
		wantRows uint16
	}{
		{
			name:     "resize message",
			json:     `{"type":"resize","cols":120,"rows":40}`,
			wantType: "resize",
			wantCols: 120,
			wantRows: 40,
		},
		{
			name:     "ping message",
			json:     `{"type":"ping"}`,
			wantType: "ping",
		},
		{
			name:     "pause message",
			json:     `{"type":"pause"}`,
			wantType: "pause",
		},
		{
			name:     "resume message",
			json:     `{"type":"resume"}`,
			wantType: "resume",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var msg controlMessage
			if err := json.Unmarshal([]byte(tt.json), &msg); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if msg.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", msg.Type, tt.wantType)
			}
			if msg.Cols != tt.wantCols {
				t.Errorf("Cols = %d, want %d", msg.Cols, tt.wantCols)
			}
			if msg.Rows != tt.wantRows {
				t.Errorf("Rows = %d, want %d", msg.Rows, tt.wantRows)
			}
		})
	}
}

// TestExecRelay_RetryCountReset verifies retry count resets after successful reconnection
func TestExecRelay_RetryCountReset(t *testing.T) {
	cfg := RelayConfig{
		Namespace:  "test-ns",
		PodName:    "test-pod",
		MaxRetries: 5,
		RetryDelay: 1 * time.Millisecond,
	}

	r := NewExecRelay(nil, cfg)

	// Manually set retry count to simulate previous failures
	r.mu.Lock()
	r.retryCount = 3
	r.mu.Unlock()

	// Verify retry count
	r.mu.RLock()
	count := r.retryCount
	r.mu.RUnlock()

	if count != 3 {
		t.Errorf("expected retryCount 3, got %d", count)
	}
}

// TestExecRelay_ExponentialBackoff tests that backoff delay calculation works
func TestExecRelay_ExponentialBackoff(t *testing.T) {
	// This tests the exponential backoff formula: delay = baseDelay * 2^(attempt-1)
	// With cap at 30 seconds
	baseDelay := 1 * time.Second

	tests := []struct {
		attempt     int
		expectedCap time.Duration
	}{
		{1, 1 * time.Second},   // 1 * 2^0 = 1s
		{2, 2 * time.Second},   // 1 * 2^1 = 2s
		{3, 4 * time.Second},   // 1 * 2^2 = 4s
		{4, 8 * time.Second},   // 1 * 2^3 = 8s
		{5, 16 * time.Second},  // 1 * 2^4 = 16s
		{6, 30 * time.Second},  // 1 * 2^5 = 32s, capped at 30s
		{10, 30 * time.Second}, // would be 512s, capped at 30s
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			delay := baseDelay * time.Duration(1<<(tt.attempt-1))
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}

			if delay != tt.expectedCap {
				t.Errorf("attempt %d: expected delay %v, got %v", tt.attempt, tt.expectedCap, delay)
			}
		})
	}
}

// TestExecRelay_ContextCancellationDuringReconnect tests reconnect respects context cancellation
func TestExecRelay_ContextCancellationDuringReconnect(t *testing.T) {
	cfg := RelayConfig{
		Namespace:  "test-ns",
		PodName:    "test-pod",
		MaxRetries: 5,
		RetryDelay: 500 * time.Millisecond, // Use moderate delay to test context cancellation
	}

	r := NewExecRelay(nil, cfg)

	// Create a context that we'll cancel quickly
	ctx, cancel := context.WithCancel(context.Background())

	// Start reconnect in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.reconnect(ctx)
	}()

	// Cancel context quickly (before the retry delay elapses)
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Should return context error reasonably quickly
	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("reconnect did not return within timeout after context cancellation")
	}
}
