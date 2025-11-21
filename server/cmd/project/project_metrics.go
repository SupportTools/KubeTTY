package main

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/supporttools/KubeTTY/server/internal/shared/metrics"
)

// Project-specific appMetrics (stateless PTY mode - no database metrics)
type appMetrics struct {
	*metrics.AppMetrics
	sessionAttached prometheus.Counter
	sessionDetached prometheus.Counter
	ptyExit         *prometheus.CounterVec
}

func newAppMetrics() *appMetrics {
	return &appMetrics{
		AppMetrics: metrics.NewAppMetrics(),
		sessionAttached: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "kubetty_session_attached_total",
				Help: "Total number of sessions attached",
			},
		),
		sessionDetached: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "kubetty_session_detached_total",
				Help: "Total number of sessions detached",
			},
		),
		ptyExit: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "kubetty_pty_exit_total",
				Help: "Total number of PTY exits by exit code",
			},
			[]string{"exit_code"},
		),
	}
}

func (m *appMetrics) observeSessionAttached() {
	if m == nil {
		return
	}
	m.sessionAttached.Inc()
}

func (m *appMetrics) observeSessionDetached() {
	if m == nil {
		return
	}
	m.sessionDetached.Inc()
}

func (m *appMetrics) observePtyExit(exitCode int) {
	if m == nil {
		return
	}
	m.ptyExit.WithLabelValues(fmt.Sprintf("%d", exitCode)).Inc()
}

// Delegate method for shared metrics
func (m *appMetrics) observeWSBytes(typ string, n int) {
	if m == nil {
		return
	}
	m.ObserveWSBytes(typ, n)
}
