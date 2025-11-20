package main

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/supporttools/KubeTTY/server/internal/shared/metrics"
)

// cleanupMetrics tracks log retention cleanup operations
type cleanupMetrics struct {
	logsDeleted *prometheus.CounterVec
	logRetentionDuration *prometheus.HistogramVec
}

func newCleanupMetrics() *cleanupMetrics {
	return &cleanupMetrics{
		logsDeleted: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "kubetty_logs_deleted_total",
				Help: "Total number of log entries deleted by retention cleanup",
			},
			[]string{"session_id"},
		),
		logRetentionDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "kubetty_log_retention_duration_seconds",
				Help:    "Duration of log retention cleanup operations",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"operation"},
		),
	}
}

func (m *cleanupMetrics) observeLogsDeleted(sessionID string, count int) {
	if m == nil || count <= 0 {
		return
	}
	m.logsDeleted.WithLabelValues(sessionID).Add(float64(count))
}

func (m *cleanupMetrics) recordRunStart() {
	// Placeholder - can add run start timestamp tracking if needed
}

func (m *cleanupMetrics) recordError(err error) {
	// Placeholder - can add error counter if needed
	// Log the error for now
	if err != nil {
		// Could add error counter metric here if needed
	}
}

func (m *cleanupMetrics) addPruned(count int64) {
	// Delegate to observeLogsDeleted with "pruned" session ID
	m.observeLogsDeleted("pruned", int(count))
}

func (m *cleanupMetrics) addTrimmed(count int64) {
	// Delegate to observeLogsDeleted with "trimmed" session ID
	m.observeLogsDeleted("trimmed", int(count))
}

// Project-specific appMetrics additions (not in shared package)
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

// Delegate methods for shared metrics (with lowercase names for compatibility)
func (m *appMetrics) observeWSBytes(typ string, n int) {
	if m == nil {
		return
	}
	m.ObserveWSBytes(typ, n)
}

func (m *appMetrics) observeStore(op string, dur time.Duration, err error) {
	if m == nil {
		return
	}
	m.ObserveStore(op, dur, err)
}
