// Package leaderelection provides Kubernetes Lease-based leader election for the gateway controller.
// This ensures only one gateway replica runs the controller reconciliation loops at a time,
// preventing race conditions when managing project resources.
package leaderelection

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/supporttools/KubeTTY/server/internal/shared/metrics"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

var log = logrus.WithField("component", "leaderelection")

// Config holds leader election configuration.
type Config struct {
	// LeaseName is the name of the Lease resource.
	LeaseName string

	// LeaseNamespace is the namespace for the Lease resource.
	// Defaults to POD_NAMESPACE environment variable.
	LeaseNamespace string

	// LeaseDuration is the duration that non-leader candidates will wait
	// before attempting to acquire leadership.
	LeaseDuration time.Duration

	// RenewDeadline is the duration that the leader will retry refreshing
	// leadership before giving up.
	RenewDeadline time.Duration

	// RetryPeriod is the duration between attempts to acquire/renew leadership.
	RetryPeriod time.Duration

	// Identity is the unique identifier for this candidate.
	// Defaults to POD_NAME environment variable or hostname.
	Identity string

	// RetryOnLostLeadership controls whether to automatically retry leader
	// election after losing leadership (e.g., due to network issues).
	// Default: true
	RetryOnLostLeadership bool

	// RetryBackoffInitial is the initial backoff duration when retrying
	// after losing leadership. Default: 1 second.
	RetryBackoffInitial time.Duration

	// RetryBackoffMax is the maximum backoff duration between retry attempts.
	// Default: 30 seconds.
	RetryBackoffMax time.Duration
}

// DefaultConfig returns sensible defaults for leader election.
func DefaultConfig() Config {
	identity := os.Getenv("POD_NAME")
	if identity == "" {
		identity, _ = os.Hostname()
	}

	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		namespace = "default"
	}

	return Config{
		LeaseName:             "kubetty-gateway-leader",
		LeaseNamespace:        namespace,
		LeaseDuration:         15 * time.Second,
		RenewDeadline:         10 * time.Second,
		RetryPeriod:           2 * time.Second,
		Identity:              identity,
		RetryOnLostLeadership: true,
		RetryBackoffInitial:   1 * time.Second,
		RetryBackoffMax:       30 * time.Second,
	}
}

// Callbacks contains the callback functions for leader election events.
type Callbacks struct {
	// OnStartedLeading is called when this instance becomes the leader.
	OnStartedLeading func(ctx context.Context)

	// OnStoppedLeading is called when this instance loses leadership.
	OnStoppedLeading func()

	// OnNewLeader is called when a new leader is elected (including self).
	OnNewLeader func(identity string)
}

// LeaderElector manages leader election for the gateway controller.
type LeaderElector struct {
	cfg       Config
	callbacks Callbacks
	clientset kubernetes.Interface

	// isLeader tracks whether this instance is currently the leader.
	isLeader atomic.Bool

	// currentLeader stores the identity of the current leader.
	currentLeader atomic.Value

	// cancel cancels the leader election context.
	cancel context.CancelFunc
}

// New creates a new LeaderElector instance.
func New(cfg Config, callbacks Callbacks) (*LeaderElector, error) {
	// Create in-cluster Kubernetes client
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return NewWithClient(cfg, callbacks, clientset), nil
}

// NewWithClient creates a LeaderElector with an existing Kubernetes client (useful for testing).
func NewWithClient(cfg Config, callbacks Callbacks, clientset kubernetes.Interface) *LeaderElector {
	le := &LeaderElector{
		cfg:       cfg,
		callbacks: callbacks,
		clientset: clientset,
	}
	le.currentLeader.Store("")
	return le
}

// Start begins the leader election process. This method blocks until the context is cancelled.
// If RetryOnLostLeadership is enabled (default), this will automatically retry leader election
// after losing leadership due to transient failures (e.g., network issues).
func (le *LeaderElector) Start(ctx context.Context) error {
	ctx, le.cancel = context.WithCancel(ctx)

	log.WithFields(logrus.Fields{
		"lease_name":               le.cfg.LeaseName,
		"lease_namespace":          le.cfg.LeaseNamespace,
		"identity":                 le.cfg.Identity,
		"lease_duration":           le.cfg.LeaseDuration,
		"renew_deadline":           le.cfg.RenewDeadline,
		"retry_period":             le.cfg.RetryPeriod,
		"retry_on_lost_leadership": le.cfg.RetryOnLostLeadership,
	}).Info("Starting leader election")

	// Retry loop - continues until context is cancelled
	attempt := 0
	backoff := le.cfg.RetryBackoffInitial
	if backoff == 0 {
		backoff = 1 * time.Second
	}
	maxBackoff := le.cfg.RetryBackoffMax
	if maxBackoff == 0 {
		maxBackoff = 30 * time.Second
	}

	for {
		// Check if context is cancelled before starting
		select {
		case <-ctx.Done():
			log.Info("Leader election stopped: context cancelled")
			return ctx.Err()
		default:
		}

		attempt++
		if attempt > 1 {
			log.WithFields(logrus.Fields{
				"attempt": attempt,
				"backoff": backoff,
			}).Info("Retrying leader election after leadership loss")
			metrics.LeaderElectionRetriesTotal.Inc()
		}

		// Run a single leader election attempt
		err := le.runLeaderElection(ctx)

		// If context was cancelled, exit cleanly
		if ctx.Err() != nil {
			log.Info("Leader election stopped: context cancelled")
			return ctx.Err()
		}

		// If retry is disabled, exit after first attempt
		if !le.cfg.RetryOnLostLeadership {
			if err != nil {
				return err
			}
			return nil
		}

		// Log the reason for retry
		if err != nil {
			log.WithError(err).Warn("Leader election ended with error, will retry")
		} else {
			log.Warn("Leader election ended (lost leadership), will retry")
		}

		// Wait with backoff before retrying
		select {
		case <-ctx.Done():
			log.Info("Leader election stopped: context cancelled during backoff")
			return ctx.Err()
		case <-time.After(backoff):
		}

		// Increase backoff for next attempt (exponential with cap)
		backoff = backoff * 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// runLeaderElection runs a single leader election attempt. It blocks until
// leadership is lost or context is cancelled.
func (le *LeaderElector) runLeaderElection(ctx context.Context) error {
	// Create the resource lock for leader election
	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      le.cfg.LeaseName,
			Namespace: le.cfg.LeaseNamespace,
		},
		Client: le.clientset.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: le.cfg.Identity,
		},
	}

	// Create and run the leader elector
	leaderElector, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   le.cfg.LeaseDuration,
		RenewDeadline:   le.cfg.RenewDeadline,
		RetryPeriod:     le.cfg.RetryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				le.isLeader.Store(true)
				le.currentLeader.Store(le.cfg.Identity)
				log.WithField("identity", le.cfg.Identity).Info("Started leading")

				// Update Prometheus metrics
				metrics.LeaderStatus.Set(1)
				metrics.LeaderTransitionsTotal.WithLabelValues("acquired").Inc()
				le.updateLeaderIdentityMetric(le.cfg.Identity, true)

				if le.callbacks.OnStartedLeading != nil {
					le.callbacks.OnStartedLeading(ctx)
				}
			},
			OnStoppedLeading: func() {
				le.isLeader.Store(false)
				log.WithField("identity", le.cfg.Identity).Warn("Stopped leading")

				// Update Prometheus metrics
				metrics.LeaderStatus.Set(0)
				metrics.LeaderTransitionsTotal.WithLabelValues("lost").Inc()

				if le.callbacks.OnStoppedLeading != nil {
					le.callbacks.OnStoppedLeading()
				}
			},
			OnNewLeader: func(identity string) {
				le.currentLeader.Store(identity)
				isSelf := identity == le.cfg.Identity
				if isSelf {
					log.WithField("identity", identity).Info("This instance is the new leader")
				} else {
					log.WithField("leader", identity).Info("New leader elected")
				}

				// Update leader identity metric
				le.updateLeaderIdentityMetric(identity, isSelf)

				if le.callbacks.OnNewLeader != nil {
					le.callbacks.OnNewLeader(identity)
				}
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create leader elector: %w", err)
	}

	// Run the leader election (blocks until context is cancelled or leadership lost)
	leaderElector.Run(ctx)
	return nil
}

// Stop stops the leader election process.
func (le *LeaderElector) Stop() {
	if le.cancel != nil {
		log.Info("Stopping leader election")
		le.cancel()
	}
}

// IsLeader returns true if this instance is currently the leader.
func (le *LeaderElector) IsLeader() bool {
	return le.isLeader.Load()
}

// GetCurrentLeader returns the identity of the current leader.
func (le *LeaderElector) GetCurrentLeader() string {
	if v := le.currentLeader.Load(); v != nil {
		return v.(string)
	}
	return ""
}

// GetIdentity returns this instance's identity.
func (le *LeaderElector) GetIdentity() string {
	return le.cfg.Identity
}

// updateLeaderIdentityMetric updates the leader identity gauge metric.
// It clears any previous identity labels and sets the current leader.
func (le *LeaderElector) updateLeaderIdentityMetric(identity string, isSelf bool) {
	// Reset the metric (clear old labels)
	metrics.LeaderIdentity.Reset()

	// Set the new leader identity
	isSelfStr := "false"
	if isSelf {
		isSelfStr = "true"
	}
	metrics.LeaderIdentity.WithLabelValues(identity, isSelfStr).Set(1)
}
