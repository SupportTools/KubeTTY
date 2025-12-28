package leaderelection

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/supporttools/KubeTTY/server/internal/shared/metrics"
	"k8s.io/client-go/kubernetes/fake"
)

// TestLeaderElector_StateTransitions tests the atomic state tracking mechanisms.
func TestLeaderElector_StateTransitions(t *testing.T) {
	cfg := Config{
		LeaseName:      "test-lease",
		LeaseNamespace: "test-ns",
		LeaseDuration:  15 * time.Second,
		RenewDeadline:  10 * time.Second,
		RetryPeriod:    2 * time.Second,
		Identity:       "test-pod-1",
	}

	clientset := fake.NewSimpleClientset()
	le := NewWithClient(cfg, Callbacks{}, clientset)

	t.Run("initial state is not leader", func(t *testing.T) {
		if le.IsLeader() {
			t.Error("expected IsLeader to be false initially")
		}
		if le.GetCurrentLeader() != "" {
			t.Errorf("expected GetCurrentLeader to be empty initially, got '%s'", le.GetCurrentLeader())
		}
	})

	t.Run("isLeader atomic operations work correctly", func(t *testing.T) {
		// Simulate becoming leader
		le.isLeader.Store(true)
		if !le.IsLeader() {
			t.Error("expected IsLeader to be true after Store(true)")
		}

		// Simulate losing leadership
		le.isLeader.Store(false)
		if le.IsLeader() {
			t.Error("expected IsLeader to be false after Store(false)")
		}
	})

	t.Run("currentLeader atomic operations work correctly", func(t *testing.T) {
		// Store new leader
		le.currentLeader.Store("leader-pod-1")
		if le.GetCurrentLeader() != "leader-pod-1" {
			t.Errorf("expected GetCurrentLeader to be 'leader-pod-1', got '%s'", le.GetCurrentLeader())
		}

		// Change leader
		le.currentLeader.Store("leader-pod-2")
		if le.GetCurrentLeader() != "leader-pod-2" {
			t.Errorf("expected GetCurrentLeader to be 'leader-pod-2', got '%s'", le.GetCurrentLeader())
		}

		// Clear leader
		le.currentLeader.Store("")
		if le.GetCurrentLeader() != "" {
			t.Errorf("expected GetCurrentLeader to be empty, got '%s'", le.GetCurrentLeader())
		}
	})
}

// TestLeaderElector_UpdateLeaderIdentityMetric tests the metric update function.
func TestLeaderElector_UpdateLeaderIdentityMetric(t *testing.T) {
	cfg := Config{
		Identity: "test-pod-1",
	}
	clientset := fake.NewSimpleClientset()
	le := NewWithClient(cfg, Callbacks{}, clientset)

	// Reset metrics before test
	metrics.LeaderIdentity.Reset()

	t.Run("updates metric when self is leader", func(t *testing.T) {
		le.updateLeaderIdentityMetric("test-pod-1", true)
		// The metric should be set without panicking
		// We can't easily verify the value without a metric collection mechanism
	})

	t.Run("updates metric when other is leader", func(t *testing.T) {
		le.updateLeaderIdentityMetric("other-pod", false)
		// The metric should be set without panicking
	})

	t.Run("handles empty identity", func(t *testing.T) {
		le.updateLeaderIdentityMetric("", false)
		// Should not panic
	})

	t.Run("handles special characters in identity", func(t *testing.T) {
		le.updateLeaderIdentityMetric("pod-name-with-dashes", true)
		le.updateLeaderIdentityMetric("pod_name_with_underscores", false)
		le.updateLeaderIdentityMetric("pod.name.with.dots", true)
		// Should not panic with various identity formats
	})
}

// TestLeaderElector_ConcurrentAccess tests thread-safety of state access.
func TestLeaderElector_ConcurrentAccess(t *testing.T) {
	cfg := Config{
		LeaseName:      "test-lease",
		LeaseNamespace: "test-ns",
		Identity:       "test-pod",
	}
	clientset := fake.NewSimpleClientset()
	le := NewWithClient(cfg, Callbacks{}, clientset)

	done := make(chan bool)
	iterations := 1000

	// Writer goroutine for isLeader
	go func() {
		for i := 0; i < iterations; i++ {
			le.isLeader.Store(i%2 == 0)
		}
		done <- true
	}()

	// Reader goroutine for isLeader
	go func() {
		for i := 0; i < iterations; i++ {
			_ = le.IsLeader()
		}
		done <- true
	}()

	// Writer goroutine for currentLeader
	go func() {
		for i := 0; i < iterations; i++ {
			if i%2 == 0 {
				le.currentLeader.Store("leader-a")
			} else {
				le.currentLeader.Store("leader-b")
			}
		}
		done <- true
	}()

	// Reader goroutine for currentLeader
	go func() {
		for i := 0; i < iterations; i++ {
			_ = le.GetCurrentLeader()
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 4; i++ {
		<-done
	}
}

// TestLeaderElector_GetCurrentLeaderNilValue tests handling of nil atomic value.
func TestLeaderElector_GetCurrentLeaderNilValue(t *testing.T) {
	cfg := Config{
		Identity: "test-pod",
	}
	clientset := fake.NewSimpleClientset()

	// Create leader elector manually to test nil case
	le := &LeaderElector{
		cfg:       cfg,
		clientset: clientset,
	}
	// Don't initialize currentLeader atomic.Value

	// This should handle nil gracefully
	leader := le.GetCurrentLeader()
	if leader != "" {
		t.Errorf("expected empty string for nil currentLeader, got '%s'", leader)
	}
}

// TestLeaderElector_StopIdempotent tests that Stop can be called multiple times safely.
func TestLeaderElector_StopIdempotent(t *testing.T) {
	cfg := Config{
		LeaseName:      "test-lease",
		LeaseNamespace: "test-ns",
		Identity:       "test-pod",
	}
	clientset := fake.NewSimpleClientset()
	le := NewWithClient(cfg, Callbacks{}, clientset)

	// Stop should be safe to call multiple times
	for i := 0; i < 10; i++ {
		le.Stop() // Should not panic
	}
}

// TestLeaderElector_ConfigFields tests that all config fields are accessible.
func TestLeaderElector_ConfigFields(t *testing.T) {
	cfg := Config{
		LeaseName:      "my-lease",
		LeaseNamespace: "my-namespace",
		LeaseDuration:  30 * time.Second,
		RenewDeadline:  20 * time.Second,
		RetryPeriod:    5 * time.Second,
		Identity:       "my-identity",
	}

	clientset := fake.NewSimpleClientset()
	le := NewWithClient(cfg, Callbacks{}, clientset)

	// Verify the config is stored correctly
	if le.cfg.LeaseName != "my-lease" {
		t.Errorf("expected LeaseName 'my-lease', got '%s'", le.cfg.LeaseName)
	}
	if le.cfg.LeaseNamespace != "my-namespace" {
		t.Errorf("expected LeaseNamespace 'my-namespace', got '%s'", le.cfg.LeaseNamespace)
	}
	if le.cfg.LeaseDuration != 30*time.Second {
		t.Errorf("expected LeaseDuration 30s, got %v", le.cfg.LeaseDuration)
	}
	if le.cfg.RenewDeadline != 20*time.Second {
		t.Errorf("expected RenewDeadline 20s, got %v", le.cfg.RenewDeadline)
	}
	if le.cfg.RetryPeriod != 5*time.Second {
		t.Errorf("expected RetryPeriod 5s, got %v", le.cfg.RetryPeriod)
	}
	if le.GetIdentity() != "my-identity" {
		t.Errorf("expected Identity 'my-identity', got '%s'", le.GetIdentity())
	}
}

// TestLeaderElector_CallbacksStored tests that callbacks are stored correctly.
func TestLeaderElector_CallbacksStored(t *testing.T) {
	startedCalled := false
	stoppedCalled := false
	newLeaderIdentity := ""

	callbacks := Callbacks{
		OnStartedLeading: func(ctx context.Context) {
			startedCalled = true
		},
		OnStoppedLeading: func() {
			stoppedCalled = true
		},
		OnNewLeader: func(identity string) {
			newLeaderIdentity = identity
		},
	}

	cfg := Config{Identity: "test"}
	clientset := fake.NewSimpleClientset()
	le := NewWithClient(cfg, callbacks, clientset)

	// Verify callbacks are stored (we can't easily invoke them without Start)
	if le.callbacks.OnStartedLeading == nil {
		t.Error("expected OnStartedLeading to be set")
	}
	if le.callbacks.OnStoppedLeading == nil {
		t.Error("expected OnStoppedLeading to be set")
	}
	if le.callbacks.OnNewLeader == nil {
		t.Error("expected OnNewLeader to be set")
	}

	// Test that callbacks work when invoked directly
	if le.callbacks.OnNewLeader != nil {
		le.callbacks.OnNewLeader("new-leader")
	}
	if newLeaderIdentity != "new-leader" {
		t.Errorf("expected newLeaderIdentity to be 'new-leader', got '%s'", newLeaderIdentity)
	}

	if le.callbacks.OnStoppedLeading != nil {
		le.callbacks.OnStoppedLeading()
	}
	if !stoppedCalled {
		t.Error("expected stoppedCalled to be true")
	}

	// Test OnStartedLeading callback
	if le.callbacks.OnStartedLeading != nil {
		le.callbacks.OnStartedLeading(context.Background())
	}
	if !startedCalled {
		t.Error("expected startedCalled to be true")
	}
}

// TestDefaultConfig_AllFields verifies all fields in DefaultConfig are set.
func TestDefaultConfig_AllFields(t *testing.T) {
	cfg := DefaultConfig()

	// All fields should have non-zero values
	if cfg.LeaseName == "" {
		t.Error("expected LeaseName to be non-empty")
	}
	if cfg.LeaseNamespace == "" {
		t.Error("expected LeaseNamespace to be non-empty")
	}
	if cfg.LeaseDuration == 0 {
		t.Error("expected LeaseDuration to be non-zero")
	}
	if cfg.RenewDeadline == 0 {
		t.Error("expected RenewDeadline to be non-zero")
	}
	if cfg.RetryPeriod == 0 {
		t.Error("expected RetryPeriod to be non-zero")
	}
	if cfg.Identity == "" {
		t.Error("expected Identity to be non-empty")
	}

	// Verify timing constraints: RenewDeadline < LeaseDuration
	if cfg.RenewDeadline >= cfg.LeaseDuration {
		t.Errorf("expected RenewDeadline (%v) < LeaseDuration (%v)", cfg.RenewDeadline, cfg.LeaseDuration)
	}

	// Verify timing constraints: RetryPeriod < RenewDeadline
	if cfg.RetryPeriod >= cfg.RenewDeadline {
		t.Errorf("expected RetryPeriod (%v) < RenewDeadline (%v)", cfg.RetryPeriod, cfg.RenewDeadline)
	}
}

// TestLeaderElector_AtomicValueInitialization tests the currentLeader is properly initialized.
func TestLeaderElector_AtomicValueInitialization(t *testing.T) {
	cfg := Config{Identity: "test"}
	clientset := fake.NewSimpleClientset()
	le := NewWithClient(cfg, Callbacks{}, clientset)

	// The atomic.Value should be initialized with empty string
	// This tests line 122-123 in NewWithClient
	leader := le.GetCurrentLeader()
	if leader != "" {
		t.Errorf("expected initialized currentLeader to be empty string, got '%s'", leader)
	}
}

// TestLeaderElector_IsLeaderAtomicBool tests the atomic.Bool behavior.
func TestLeaderElector_IsLeaderAtomicBool(t *testing.T) {
	var isLeader atomic.Bool

	// Default value should be false
	if isLeader.Load() {
		t.Error("expected default atomic.Bool to be false")
	}

	// Store and verify
	isLeader.Store(true)
	if !isLeader.Load() {
		t.Error("expected atomic.Bool to be true after Store(true)")
	}

	isLeader.Store(false)
	if isLeader.Load() {
		t.Error("expected atomic.Bool to be false after Store(false)")
	}
}
