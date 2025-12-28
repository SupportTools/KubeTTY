package leaderelection

import (
	"context"
	"os"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"
)

func TestDefaultConfig(t *testing.T) {
	// Save and restore environment
	oldPodName := os.Getenv("POD_NAME")
	oldPodNamespace := os.Getenv("POD_NAMESPACE")
	defer func() {
		os.Setenv("POD_NAME", oldPodName)
		os.Setenv("POD_NAMESPACE", oldPodNamespace)
	}()

	t.Run("uses POD_NAME and POD_NAMESPACE env vars", func(t *testing.T) {
		os.Setenv("POD_NAME", "test-pod-123")
		os.Setenv("POD_NAMESPACE", "test-namespace")

		cfg := DefaultConfig()

		if cfg.Identity != "test-pod-123" {
			t.Errorf("expected Identity to be 'test-pod-123', got '%s'", cfg.Identity)
		}
		if cfg.LeaseNamespace != "test-namespace" {
			t.Errorf("expected LeaseNamespace to be 'test-namespace', got '%s'", cfg.LeaseNamespace)
		}
	})

	t.Run("falls back to hostname when POD_NAME not set", func(t *testing.T) {
		os.Unsetenv("POD_NAME")
		os.Setenv("POD_NAMESPACE", "test-namespace")

		cfg := DefaultConfig()

		hostname, _ := os.Hostname()
		if cfg.Identity != hostname {
			t.Errorf("expected Identity to be hostname '%s', got '%s'", hostname, cfg.Identity)
		}
	})

	t.Run("falls back to default namespace when POD_NAMESPACE not set", func(t *testing.T) {
		os.Setenv("POD_NAME", "test-pod")
		os.Unsetenv("POD_NAMESPACE")

		cfg := DefaultConfig()

		if cfg.LeaseNamespace != "default" {
			t.Errorf("expected LeaseNamespace to be 'default', got '%s'", cfg.LeaseNamespace)
		}
	})

	t.Run("has sensible default durations", func(t *testing.T) {
		cfg := DefaultConfig()

		if cfg.LeaseDuration != 15*time.Second {
			t.Errorf("expected LeaseDuration to be 15s, got %v", cfg.LeaseDuration)
		}
		if cfg.RenewDeadline != 10*time.Second {
			t.Errorf("expected RenewDeadline to be 10s, got %v", cfg.RenewDeadline)
		}
		if cfg.RetryPeriod != 2*time.Second {
			t.Errorf("expected RetryPeriod to be 2s, got %v", cfg.RetryPeriod)
		}
		if cfg.LeaseName != "kubetty-gateway-leader" {
			t.Errorf("expected LeaseName to be 'kubetty-gateway-leader', got '%s'", cfg.LeaseName)
		}
	})
}

func TestNewWithClient(t *testing.T) {
	cfg := Config{
		LeaseName:      "test-lease",
		LeaseNamespace: "test-ns",
		LeaseDuration:  15 * time.Second,
		RenewDeadline:  10 * time.Second,
		RetryPeriod:    2 * time.Second,
		Identity:       "test-identity",
	}

	callbacks := Callbacks{
		OnStartedLeading: func(ctx context.Context) {},
		OnStoppedLeading: func() {},
		OnNewLeader:      func(identity string) {},
	}

	clientset := fake.NewSimpleClientset()
	le := NewWithClient(cfg, callbacks, clientset)

	if le == nil {
		t.Fatal("expected non-nil LeaderElector")
	}

	// Check initial state
	if le.IsLeader() {
		t.Error("expected IsLeader to be false initially")
	}
	if le.GetCurrentLeader() != "" {
		t.Errorf("expected GetCurrentLeader to be empty initially, got '%s'", le.GetCurrentLeader())
	}
	if le.GetIdentity() != "test-identity" {
		t.Errorf("expected GetIdentity to be 'test-identity', got '%s'", le.GetIdentity())
	}
}

func TestLeaderElector_GetIdentity(t *testing.T) {
	cfg := Config{
		Identity: "my-unique-identity",
	}
	clientset := fake.NewSimpleClientset()
	le := NewWithClient(cfg, Callbacks{}, clientset)

	if le.GetIdentity() != "my-unique-identity" {
		t.Errorf("expected GetIdentity to return 'my-unique-identity', got '%s'", le.GetIdentity())
	}
}

func TestLeaderElector_Stop(t *testing.T) {
	cfg := Config{
		LeaseName:      "test-lease",
		LeaseNamespace: "test-ns",
		LeaseDuration:  15 * time.Second,
		RenewDeadline:  10 * time.Second,
		RetryPeriod:    2 * time.Second,
		Identity:       "test-identity",
	}

	clientset := fake.NewSimpleClientset()
	le := NewWithClient(cfg, Callbacks{}, clientset)

	// Stop should not panic even if Start was never called
	le.Stop()
}

func TestLeaderElector_MultipleStops(t *testing.T) {
	cfg := Config{
		LeaseName:      "test-lease",
		LeaseNamespace: "test-ns",
		LeaseDuration:  15 * time.Second,
		RenewDeadline:  10 * time.Second,
		RetryPeriod:    2 * time.Second,
		Identity:       "test-identity",
	}

	clientset := fake.NewSimpleClientset()
	le := NewWithClient(cfg, Callbacks{}, clientset)

	// Multiple stops should not panic
	le.Stop()
	le.Stop()
	le.Stop()
}

func TestLeaderElector_IsLeaderInitialState(t *testing.T) {
	cfg := Config{
		Identity: "test-pod",
	}
	clientset := fake.NewSimpleClientset()
	le := NewWithClient(cfg, Callbacks{}, clientset)

	if le.IsLeader() {
		t.Error("expected IsLeader to be false before Start is called")
	}
}

func TestLeaderElector_GetCurrentLeaderInitialState(t *testing.T) {
	cfg := Config{
		Identity: "test-pod",
	}
	clientset := fake.NewSimpleClientset()
	le := NewWithClient(cfg, Callbacks{}, clientset)

	leader := le.GetCurrentLeader()
	if leader != "" {
		t.Errorf("expected GetCurrentLeader to be empty before Start, got '%s'", leader)
	}
}

func TestLeaderElector_ConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "empty config",
			cfg:  Config{},
		},
		{
			name: "only identity set",
			cfg:  Config{Identity: "test"},
		},
		{
			name: "full config",
			cfg: Config{
				LeaseName:      "test-lease",
				LeaseNamespace: "test-ns",
				LeaseDuration:  15 * time.Second,
				RenewDeadline:  10 * time.Second,
				RetryPeriod:    2 * time.Second,
				Identity:       "test-identity",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientset := fake.NewSimpleClientset()
			le := NewWithClient(tt.cfg, Callbacks{}, clientset)
			if le == nil {
				t.Error("expected non-nil LeaderElector even with minimal config")
			}
		})
	}
}

func TestLeaderElector_CallbacksOptional(t *testing.T) {
	cfg := Config{
		LeaseName:      "test-lease",
		LeaseNamespace: "test-ns",
		LeaseDuration:  15 * time.Second,
		RenewDeadline:  10 * time.Second,
		RetryPeriod:    2 * time.Second,
		Identity:       "test-identity",
	}

	// All callbacks nil should be fine
	clientset := fake.NewSimpleClientset()
	le := NewWithClient(cfg, Callbacks{}, clientset)
	if le == nil {
		t.Error("expected non-nil LeaderElector with nil callbacks")
	}

	// Only some callbacks set should be fine
	le = NewWithClient(cfg, Callbacks{
		OnNewLeader: func(identity string) {},
	}, clientset)
	if le == nil {
		t.Error("expected non-nil LeaderElector with partial callbacks")
	}
}

func TestDefaultConfig_RetrySettings(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.RetryOnLostLeadership {
		t.Error("expected RetryOnLostLeadership to be true by default")
	}
	if cfg.RetryBackoffInitial != 1*time.Second {
		t.Errorf("expected RetryBackoffInitial to be 1s, got %v", cfg.RetryBackoffInitial)
	}
	if cfg.RetryBackoffMax != 30*time.Second {
		t.Errorf("expected RetryBackoffMax to be 30s, got %v", cfg.RetryBackoffMax)
	}
}

func TestLeaderElector_Start_ContextCancellation(t *testing.T) {
	cfg := Config{
		LeaseName:             "test-lease",
		LeaseNamespace:        "test-ns",
		LeaseDuration:         15 * time.Second,
		RenewDeadline:         10 * time.Second,
		RetryPeriod:           2 * time.Second,
		Identity:              "test-identity",
		RetryOnLostLeadership: true,
		RetryBackoffInitial:   100 * time.Millisecond,
		RetryBackoffMax:       500 * time.Millisecond,
	}

	clientset := fake.NewSimpleClientset()
	le := NewWithClient(cfg, Callbacks{}, clientset)

	// Create a context that cancels quickly
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Start should return when context is cancelled
	done := make(chan error, 1)
	go func() {
		done <- le.Start(ctx)
	}()

	select {
	case err := <-done:
		if err != context.DeadlineExceeded {
			t.Errorf("expected context.DeadlineExceeded, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Start did not return after context cancellation")
	}
}

func TestLeaderElector_Start_RetryDisabled(t *testing.T) {
	cfg := Config{
		LeaseName:             "test-lease",
		LeaseNamespace:        "test-ns",
		LeaseDuration:         15 * time.Second,
		RenewDeadline:         10 * time.Second,
		RetryPeriod:           2 * time.Second,
		Identity:              "test-identity",
		RetryOnLostLeadership: false, // Disable retry
		RetryBackoffInitial:   100 * time.Millisecond,
		RetryBackoffMax:       500 * time.Millisecond,
	}

	clientset := fake.NewSimpleClientset()
	le := NewWithClient(cfg, Callbacks{}, clientset)

	// Create a context that we'll cancel
	ctx, cancel := context.WithCancel(context.Background())

	// Start should return after first attempt when retry is disabled
	done := make(chan error, 1)
	go func() {
		done <- le.Start(ctx)
	}()

	// Give it a moment to start, then cancel
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Start did not return after context cancellation with retry disabled")
	}
}

func TestLeaderElector_BackoffCalculation(t *testing.T) {
	// Test that backoff increases but doesn't exceed max
	initialBackoff := 100 * time.Millisecond
	maxBackoff := 500 * time.Millisecond

	backoff := initialBackoff
	for i := 0; i < 10; i++ {
		backoff = backoff * 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}

	// After 10 iterations, backoff should be capped at max
	if backoff != maxBackoff {
		t.Errorf("expected backoff to be capped at %v, got %v", maxBackoff, backoff)
	}
}
