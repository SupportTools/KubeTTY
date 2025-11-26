package exec

import (
	"testing"
	"time"

	"k8s.io/client-go/tools/remotecommand"
)

// ---- terminalSizeQueue tests ----

func TestNewTerminalSizeQueue(t *testing.T) {
	q := newTerminalSizeQueue()
	if q == nil {
		t.Fatal("newTerminalSizeQueue returned nil")
	}
	if q.ch == nil {
		t.Error("channel should not be nil")
	}
	if q.current != nil {
		t.Error("current should be nil initially")
	}
}

func TestTerminalSizeQueue_Resize(t *testing.T) {
	q := newTerminalSizeQueue()

	// Resize should not block
	done := make(chan struct{})
	go func() {
		q.Resize(80, 24)
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Fatal("Resize blocked unexpectedly")
	}
}

func TestTerminalSizeQueue_ResizeMultiple(t *testing.T) {
	q := newTerminalSizeQueue()

	// Queue multiple resize events - should not block
	done := make(chan struct{})
	go func() {
		q.Resize(80, 24)
		q.Resize(100, 30)
		q.Resize(120, 40)
		close(done)
	}()

	select {
	case <-done:
		// Success - all resizes completed without blocking
	case <-time.After(time.Second):
		t.Fatal("Multiple Resize calls blocked unexpectedly")
	}
}

func TestTerminalSizeQueue_Current_Initial(t *testing.T) {
	q := newTerminalSizeQueue()

	// Current should be nil before any resize
	current := q.Current()
	if current != nil {
		t.Errorf("Current() should be nil initially, got %+v", current)
	}
}

func TestTerminalSizeQueue_Next(t *testing.T) {
	q := newTerminalSizeQueue()

	// Send a size
	go func() {
		q.Resize(80, 24)
	}()

	// Next should return the size
	done := make(chan *remotecommand.TerminalSize)
	go func() {
		done <- q.Next()
	}()

	select {
	case size := <-done:
		if size == nil {
			t.Fatal("Next() returned nil")
		}
		if size.Width != 80 {
			t.Errorf("Width = %d, want 80", size.Width)
		}
		if size.Height != 24 {
			t.Errorf("Height = %d, want 24", size.Height)
		}
	case <-time.After(time.Second):
		t.Fatal("Next() blocked longer than expected")
	}
}

func TestTerminalSizeQueue_Next_UpdatesCurrent(t *testing.T) {
	q := newTerminalSizeQueue()

	// Current should be nil initially
	if q.Current() != nil {
		t.Error("Current() should be nil initially")
	}

	// Send a size
	go q.Resize(100, 50)

	// Read it with Next
	done := make(chan struct{})
	go func() {
		q.Next()
		close(done)
	}()

	select {
	case <-done:
		// Now Current should be set
		current := q.Current()
		if current == nil {
			t.Fatal("Current() should not be nil after Next()")
		}
		if current.Width != 100 {
			t.Errorf("Current().Width = %d, want 100", current.Width)
		}
		if current.Height != 50 {
			t.Errorf("Current().Height = %d, want 50", current.Height)
		}
	case <-time.After(time.Second):
		t.Fatal("Next() blocked longer than expected")
	}
}

// ---- SessionConfig tests ----

func TestSessionConfig_Defaults(t *testing.T) {
	cfg := SessionConfig{
		Namespace: "default",
		PodName:   "test-pod",
	}

	// Command should be empty initially
	if len(cfg.Command) != 0 {
		t.Errorf("Command should be empty initially, got %v", cfg.Command)
	}

	// Container is optional
	if cfg.Container != "" {
		t.Errorf("Container should be empty by default, got %q", cfg.Container)
	}
}

// ---- NewSession tests ----

func TestNewSession_MissingNamespace(t *testing.T) {
	cfg := SessionConfig{
		Namespace: "",
		PodName:   "test-pod",
	}

	_, err := NewSession(nil, cfg)
	if err == nil {
		t.Fatal("expected error for missing namespace")
	}
	if err.Error() != "namespace is required" {
		t.Errorf("error message = %q, want %q", err.Error(), "namespace is required")
	}
}

func TestNewSession_MissingPodName(t *testing.T) {
	cfg := SessionConfig{
		Namespace: "default",
		PodName:   "",
	}

	_, err := NewSession(nil, cfg)
	if err == nil {
		t.Fatal("expected error for missing pod name")
	}
	if err.Error() != "pod name is required" {
		t.Errorf("error message = %q, want %q", err.Error(), "pod name is required")
	}
}

// Note: Testing NewSession with nil restConfig is skipped because the Kubernetes
// clientset panics on nil config rather than returning an error. The input
// validation for namespace and podName is tested above.

// ---- Session state tests (without actual K8s connection) ----

func TestSession_IsRunning_BeforeStart(t *testing.T) {
	// Create a minimal session struct for testing state
	s := &Session{
		started: false,
		closed:  false,
		doneCh:  make(chan struct{}),
	}

	if s.IsRunning() {
		t.Error("IsRunning() should be false before start")
	}
}

func TestSession_IsRunning_AfterClose(t *testing.T) {
	s := &Session{
		started: true,
		closed:  true,
		doneCh:  make(chan struct{}),
	}

	if s.IsRunning() {
		t.Error("IsRunning() should be false after close")
	}
}

func TestSession_IsRunning_WhenRunning(t *testing.T) {
	s := &Session{
		started: true,
		closed:  false,
		doneCh:  make(chan struct{}),
	}

	if !s.IsRunning() {
		t.Error("IsRunning() should be true when started and not closed")
	}
}

func TestSession_Write_BeforeStart(t *testing.T) {
	s := &Session{
		started: false,
		closed:  false,
		doneCh:  make(chan struct{}),
	}

	_, err := s.Write([]byte("test"))
	if err == nil {
		t.Fatal("expected error when writing before start")
	}
	if err.Error() != "session not started" {
		t.Errorf("error = %q, want %q", err.Error(), "session not started")
	}
}

func TestSession_Write_AfterClose(t *testing.T) {
	s := &Session{
		started: true,
		closed:  true,
		doneCh:  make(chan struct{}),
	}

	_, err := s.Write([]byte("test"))
	if err == nil {
		t.Fatal("expected error when writing after close")
	}
	if err.Error() != "session is closed" {
		t.Errorf("error = %q, want %q", err.Error(), "session is closed")
	}
}

func TestSession_Read_BeforeStart(t *testing.T) {
	s := &Session{
		started: false,
		closed:  false,
		doneCh:  make(chan struct{}),
	}

	buf := make([]byte, 100)
	_, err := s.Read(buf)
	if err == nil {
		t.Fatal("expected error when reading before start")
	}
	if err.Error() != "session not started" {
		t.Errorf("error = %q, want %q", err.Error(), "session not started")
	}
}

func TestSession_ReadStderr_BeforeStart(t *testing.T) {
	s := &Session{
		started: false,
		closed:  false,
		doneCh:  make(chan struct{}),
	}

	buf := make([]byte, 100)
	_, err := s.ReadStderr(buf)
	if err == nil {
		t.Fatal("expected error when reading stderr before start")
	}
	if err.Error() != "session not started" {
		t.Errorf("error = %q, want %q", err.Error(), "session not started")
	}
}

func TestSession_Resize_BeforeStart(t *testing.T) {
	s := &Session{
		started:   false,
		closed:    false,
		doneCh:    make(chan struct{}),
		sizeQueue: newTerminalSizeQueue(),
	}

	err := s.Resize(80, 24)
	if err == nil {
		t.Fatal("expected error when resizing before start")
	}
	if err.Error() != "session not started" {
		t.Errorf("error = %q, want %q", err.Error(), "session not started")
	}
}

func TestSession_Resize_AfterClose(t *testing.T) {
	s := &Session{
		started:   true,
		closed:    true,
		doneCh:    make(chan struct{}),
		sizeQueue: newTerminalSizeQueue(),
	}

	err := s.Resize(80, 24)
	if err == nil {
		t.Fatal("expected error when resizing after close")
	}
	if err.Error() != "session is closed" {
		t.Errorf("error = %q, want %q", err.Error(), "session is closed")
	}
}

func TestSession_Done(t *testing.T) {
	doneCh := make(chan struct{})
	s := &Session{
		doneCh: doneCh,
	}

	// Done should return the channel
	if s.Done() != doneCh {
		t.Error("Done() should return the done channel")
	}
}

func TestSession_Close_AlreadyClosed(t *testing.T) {
	doneCh := make(chan struct{})
	close(doneCh)

	s := &Session{
		started: true,
		closed:  true,
		doneCh:  doneCh,
	}

	// Closing an already-closed session should not error
	err := s.Close()
	if err != nil {
		t.Errorf("Close() on already closed session should not error, got %v", err)
	}
}

// ---- Start validation tests ----

func TestSession_Start_AlreadyStarted(t *testing.T) {
	s := &Session{
		started: true,
		closed:  false,
		doneCh:  make(chan struct{}),
	}

	err := s.Start(nil)
	if err == nil {
		t.Fatal("expected error when starting already started session")
	}
	if err.Error() != "session already started" {
		t.Errorf("error = %q, want %q", err.Error(), "session already started")
	}
}

func TestSession_Start_Closed(t *testing.T) {
	s := &Session{
		started: false,
		closed:  true,
		doneCh:  make(chan struct{}),
	}

	err := s.Start(nil)
	if err == nil {
		t.Fatal("expected error when starting closed session")
	}
	if err.Error() != "session is closed" {
		t.Errorf("error = %q, want %q", err.Error(), "session is closed")
	}
}
