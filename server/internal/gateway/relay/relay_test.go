package relay

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ---- Status constants tests ----

func TestStatusConstants(t *testing.T) {
	tests := []struct {
		status   Status
		expected string
	}{
		{StatusIdle, "idle"},
		{StatusConnecting, "connecting"},
		{StatusConnected, "connected"},
		{StatusReconnecting, "reconnecting"},
		{StatusClosed, "closed"},
	}

	for _, tt := range tests {
		if string(tt.status) != tt.expected {
			t.Errorf("Status %v = %q, want %q", tt.status, string(tt.status), tt.expected)
		}
	}
}

func TestStatusValuesDistinct(t *testing.T) {
	statuses := []Status{
		StatusIdle,
		StatusConnecting,
		StatusConnected,
		StatusReconnecting,
		StatusClosed,
	}

	seen := make(map[Status]bool)
	for _, s := range statuses {
		if seen[s] {
			t.Errorf("duplicate status value: %v", s)
		}
		seen[s] = true
	}
}

// ---- CircuitBreaker tests ----

func TestNewCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker(5, 30*time.Second)
	if cb == nil {
		t.Fatal("NewCircuitBreaker returned nil")
	}
	if cb.threshold != 5 {
		t.Errorf("threshold = %d, want 5", cb.threshold)
	}
	if cb.resetAfter != 30*time.Second {
		t.Errorf("resetAfter = %v, want 30s", cb.resetAfter)
	}
	if cb.Failures() != 0 {
		t.Errorf("initial failures = %d, want 0", cb.Failures())
	}
	if cb.IsOpen() {
		t.Error("circuit breaker should not be open initially")
	}
}

func TestCircuitBreaker_RecordFailure(t *testing.T) {
	cb := NewCircuitBreaker(5, 30*time.Second)

	cb.RecordFailure()
	if cb.Failures() != 1 {
		t.Errorf("failures after 1 RecordFailure = %d, want 1", cb.Failures())
	}

	cb.RecordFailure()
	cb.RecordFailure()
	if cb.Failures() != 3 {
		t.Errorf("failures after 3 RecordFailure = %d, want 3", cb.Failures())
	}
}

func TestCircuitBreaker_RecordSuccess(t *testing.T) {
	cb := NewCircuitBreaker(5, 30*time.Second)

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.Failures() != 3 {
		t.Errorf("failures before reset = %d, want 3", cb.Failures())
	}

	cb.RecordSuccess()
	if cb.Failures() != 0 {
		t.Errorf("failures after RecordSuccess = %d, want 0", cb.Failures())
	}
}

func TestCircuitBreaker_IsOpen_BelowThreshold(t *testing.T) {
	cb := NewCircuitBreaker(5, 30*time.Second)

	for i := 0; i < 4; i++ {
		cb.RecordFailure()
		if cb.IsOpen() {
			t.Errorf("circuit breaker should not be open at %d failures (threshold is 5)", i+1)
		}
	}
}

func TestCircuitBreaker_IsOpen_AtThreshold(t *testing.T) {
	cb := NewCircuitBreaker(5, 30*time.Second)

	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}
	if !cb.IsOpen() {
		t.Error("circuit breaker should be open at threshold")
	}
}

func TestCircuitBreaker_IsOpen_ResetsAfterTime(t *testing.T) {
	cb := NewCircuitBreaker(5, 50*time.Millisecond)

	// Trigger circuit breaker
	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}
	if !cb.IsOpen() {
		t.Error("circuit breaker should be open after failures")
	}

	// Wait for reset period
	time.Sleep(60 * time.Millisecond)

	// Circuit breaker should allow attempts (half-open)
	if cb.IsOpen() {
		t.Error("circuit breaker should be half-open after reset period")
	}
}

func TestCircuitBreaker_Failures(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Second)

	for i := 0; i < 10; i++ {
		cb.RecordFailure()
		expected := i + 1
		if cb.Failures() != expected {
			t.Errorf("failures after %d RecordFailure = %d, want %d", i+1, cb.Failures(), expected)
		}
	}
}

// ---- StatusEvent tests ----

func TestStatusEvent_Fields(t *testing.T) {
	now := time.Now()
	err := errors.New("test error")

	event := StatusEvent{
		Status: StatusConnected,
		Err:    err,
		When:   now,
	}

	if event.Status != StatusConnected {
		t.Errorf("Status = %v, want %v", event.Status, StatusConnected)
	}
	if event.Err != err {
		t.Errorf("Err = %v, want %v", event.Err, err)
	}
	if event.When != now {
		t.Errorf("When = %v, want %v", event.When, now)
	}
}

func TestStatusEvent_NilError(t *testing.T) {
	event := StatusEvent{
		Status: StatusIdle,
		Err:    nil,
		When:   time.Now(),
	}

	if event.Err != nil {
		t.Errorf("Err should be nil, got %v", event.Err)
	}
}

// ---- Config tests ----

func TestConfig_Fields(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	headers := http.Header{}
	headers.Set("Authorization", "Bearer token")
	dialer := websocket.DefaultDialer

	cfg := Config{
		ProjectID:     "test-project",
		Endpoint:      endpoint,
		Headers:       headers,
		Dialer:        dialer,
		DownstreamURI: "ws://localhost:8080/ws",
	}

	if cfg.ProjectID != "test-project" {
		t.Errorf("ProjectID = %q, want %q", cfg.ProjectID, "test-project")
	}
	if cfg.Endpoint.String() != "ws://localhost:8080/ws" {
		t.Errorf("Endpoint = %q, want %q", cfg.Endpoint.String(), "ws://localhost:8080/ws")
	}
	if cfg.Headers.Get("Authorization") != "Bearer token" {
		t.Errorf("Headers Authorization = %q, want %q", cfg.Headers.Get("Authorization"), "Bearer token")
	}
	if cfg.Dialer != dialer {
		t.Error("Dialer mismatch")
	}
	if cfg.DownstreamURI != "ws://localhost:8080/ws" {
		t.Errorf("DownstreamURI = %q, want %q", cfg.DownstreamURI, "ws://localhost:8080/ws")
	}
}

// ---- Relay New() tests ----

func TestNew(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	cfg := Config{
		ProjectID:     "test",
		Endpoint:      endpoint,
		DownstreamURI: endpoint.String(),
	}

	relay := New(cfg)

	if relay == nil {
		t.Fatal("New returned nil")
	}
	if relay.Status() != StatusIdle {
		t.Errorf("initial status = %v, want %v", relay.Status(), StatusIdle)
	}
	if relay.cfg.ProjectID != "test" {
		t.Errorf("cfg.ProjectID = %q, want %q", relay.cfg.ProjectID, "test")
	}
}

func TestNew_DefaultsDialer(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	cfg := Config{
		ProjectID: "test",
		Endpoint:  endpoint,
		Dialer:    nil, // Should be defaulted
	}

	relay := New(cfg)

	if relay.cfg.Dialer == nil {
		t.Error("Dialer should be defaulted to websocket.DefaultDialer")
	}
}

func TestNew_PreservesDialer(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	customDialer := &websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	cfg := Config{
		ProjectID: "test",
		Endpoint:  endpoint,
		Dialer:    customDialer,
	}

	relay := New(cfg)

	if relay.cfg.Dialer != customDialer {
		t.Error("custom Dialer should be preserved")
	}
}

// ---- Relay Status() and LastError() tests ----

func TestRelay_Status_Initial(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	relay := New(Config{ProjectID: "test", Endpoint: endpoint})

	if relay.Status() != StatusIdle {
		t.Errorf("Status() = %v, want %v", relay.Status(), StatusIdle)
	}
}

func TestRelay_LastError_Initial(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	relay := New(Config{ProjectID: "test", Endpoint: endpoint})

	if relay.LastError() != nil {
		t.Errorf("LastError() = %v, want nil", relay.LastError())
	}
}

// ---- Subscribe tests ----

func TestRelay_Subscribe(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	relay := New(Config{ProjectID: "test", Endpoint: endpoint})

	ch := relay.Subscribe()
	if ch == nil {
		t.Fatal("Subscribe() returned nil channel")
	}

	// Channel should have buffer
	if cap(ch) != 4 {
		t.Errorf("Subscribe() channel cap = %d, want 4", cap(ch))
	}
}

func TestRelay_Subscribe_Multiple(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	relay := New(Config{ProjectID: "test", Endpoint: endpoint})

	ch1 := relay.Subscribe()
	ch2 := relay.Subscribe()

	if ch1 == ch2 {
		t.Error("Subscribe() should return different channels for each call")
	}
}

// ---- Close tests ----

func TestRelay_Close_Idle(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	relay := New(Config{ProjectID: "test", Endpoint: endpoint})

	err := relay.Close()
	if err != nil {
		t.Errorf("Close() on idle relay returned error: %v", err)
	}
	if relay.Status() != StatusClosed {
		t.Errorf("Status after Close() = %v, want %v", relay.Status(), StatusClosed)
	}
}

func TestRelay_Close_NotifiesObservers(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:8080/ws")
	relay := New(Config{ProjectID: "test", Endpoint: endpoint})

	ch := relay.Subscribe()
	_ = relay.Close()

	select {
	case evt := <-ch:
		if evt.Status != StatusClosed {
			t.Errorf("event Status = %v, want %v", evt.Status, StatusClosed)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected to receive close event")
	}
}

// ---- pipeError tests ----

func TestPipeError_Error(t *testing.T) {
	baseErr := errors.New("connection closed")
	pErr := &pipeError{
		err:       baseErr,
		direction: "up->down",
		isRead:    true,
	}

	if pErr.Error() != "connection closed" {
		t.Errorf("Error() = %q, want %q", pErr.Error(), "connection closed")
	}
}

func TestPipeError_Unwrap(t *testing.T) {
	baseErr := errors.New("connection closed")
	pErr := &pipeError{
		err:       baseErr,
		direction: "up->down",
		isRead:    true,
	}

	if pErr.Unwrap() != baseErr {
		t.Errorf("Unwrap() = %v, want %v", pErr.Unwrap(), baseErr)
	}
}

func TestPipeError_ErrorsAs(t *testing.T) {
	baseErr := errors.New("connection closed")
	pErr := &pipeError{
		err:       baseErr,
		direction: "down->up",
		isRead:    false,
	}

	var target *pipeError
	if !errors.As(pErr, &target) {
		t.Error("errors.As should match pipeError")
	}
	if target.direction != "down->up" {
		t.Errorf("direction = %q, want %q", target.direction, "down->up")
	}
	if target.isRead != false {
		t.Error("isRead should be false")
	}
}

// ---- Backoff tests ----

func TestDefaultBackoff(t *testing.T) {
	b := DefaultBackoff()
	got := []time.Duration{}
	for i := 1; i <= 5; i++ {
		got = append(got, b.Next(i))
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attempt %d: got %s want %s", i+1, got[i], want[i])
		}
	}
}

func TestDefaultBackoff_ZeroAttempt(t *testing.T) {
	b := DefaultBackoff()
	// Attempt 0 should be treated as attempt 1
	got := b.Next(0)
	if got != time.Second {
		t.Errorf("Next(0) = %v, want %v", got, time.Second)
	}
}

func TestDefaultBackoff_MaxCap(t *testing.T) {
	b := DefaultBackoff()
	// Attempts beyond 6 should be capped at 32s (1 << 5 = 32)
	got6 := b.Next(6)
	got7 := b.Next(7)
	got100 := b.Next(100)

	if got6 != 32*time.Second {
		t.Errorf("Next(6) = %v, want %v", got6, 32*time.Second)
	}
	if got7 != 32*time.Second {
		t.Errorf("Next(7) = %v, want %v (should be capped)", got7, 32*time.Second)
	}
	if got100 != 32*time.Second {
		t.Errorf("Next(100) = %v, want %v (should be capped)", got100, 32*time.Second)
	}
}

func TestDefaultBackoff_ExponentialGrowth(t *testing.T) {
	b := DefaultBackoff()
	// Verify exponential growth pattern
	prev := time.Duration(0)
	for i := 2; i <= 6; i++ {
		curr := b.Next(i)
		expected := time.Duration(1<<uint(i-1)) * time.Second
		if curr != expected {
			t.Errorf("Next(%d) = %v, want %v", i, curr, expected)
		}
		if prev > 0 && curr != 2*prev {
			t.Errorf("Next(%d) should be 2x Next(%d): got %v, expected %v", i, i-1, curr, 2*prev)
		}
		prev = curr
	}
}

func TestFixedBackoff(t *testing.T) {
	fb := FixedBackoff{Delay: 3 * time.Second}
	if d := fb.Next(10); d != 3*time.Second {
		t.Fatalf("expected 3s backoff, got %s", d)
	}
}

func TestFixedBackoff_ZeroDelay(t *testing.T) {
	fb := FixedBackoff{Delay: 0}
	// Zero delay should default to 1 second
	if d := fb.Next(1); d != time.Second {
		t.Errorf("zero delay backoff = %v, want %v", d, time.Second)
	}
}

func TestFixedBackoff_NegativeDelay(t *testing.T) {
	fb := FixedBackoff{Delay: -5 * time.Second}
	// Negative delay should default to 1 second
	if d := fb.Next(1); d != time.Second {
		t.Errorf("negative delay backoff = %v, want %v", d, time.Second)
	}
}

func TestFixedBackoff_IgnoresAttempt(t *testing.T) {
	fb := FixedBackoff{Delay: 500 * time.Millisecond}
	// All attempts should return same delay
	for i := 0; i <= 100; i++ {
		d := fb.Next(i)
		if d != 500*time.Millisecond {
			t.Errorf("Next(%d) = %v, want 500ms", i, d)
		}
	}
}

func TestFixedBackoff_DefaultStruct(t *testing.T) {
	// Default struct (zero value) should use 1 second
	var fb FixedBackoff
	if d := fb.Next(1); d != time.Second {
		t.Errorf("default FixedBackoff.Next(1) = %v, want %v", d, time.Second)
	}
}

func TestRelayConnectFailure(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:0/ws")
	r := New(Config{ProjectID: "test", Endpoint: endpoint})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := r.Connect(ctx, FixedBackoff{Delay: 10 * time.Millisecond}); err == nil {
		t.Fatalf("expected connect error, got nil")
	}
}

func TestRelayProxyEcho(t *testing.T) {
	// Start downstream echo server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, req, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			_ = conn.WriteMessage(mt, data)
		}
	}))
	defer srv.Close()

	u := ""
	if raw, err := url.Parse("ws" + strings.TrimPrefix(srv.URL, "http")); err == nil {
		u = raw.String()
	} else {
		t.Fatalf("parse downstream url: %v", err)
	}
	endpoint, _ := url.Parse(u)
	r := New(Config{ProjectID: "test", Endpoint: endpoint})

	// Start upstream WS
	upstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		upstream, err := upgrader.Upgrade(w, req, nil)
		if err != nil {
			t.Errorf("upgrade upstream: %v", err)
			return
		}
		defer upstream.Close()

		ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
		defer cancel()
		// Proxy error is expected when client disconnects - just log it
		_ = r.Proxy(ctx, upstream)
	}))
	defer upstreamSrv.Close()

	// Client writes through upstream, expect echo
	uUpstream := ""
	if raw, err := url.Parse("ws" + strings.TrimPrefix(upstreamSrv.URL, "http")); err == nil {
		uUpstream = raw.String()
	} else {
		t.Fatalf("parse upstream url: %v", err)
	}
	c, _, err := websocket.DefaultDialer.Dial(uUpstream, nil)
	if err != nil {
		t.Fatalf("dial upstream: %v", err)
	}
	defer c.Close()

	msg := []byte("hello")
	if err := c.WriteMessage(websocket.BinaryMessage, msg); err != nil {
		t.Fatalf("write upstream: %v", err)
	}
	_, data, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("read upstream: %v", err)
	}
	if string(data) != string(msg) {
		t.Fatalf("expected %q, got %q", msg, data)
	}
}
