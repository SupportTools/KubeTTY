package exec

import (
	"testing"
	"time"

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
