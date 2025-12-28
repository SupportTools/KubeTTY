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

	// WebSocket connection metrics
	wsConnectionsTotal  prometheus.Counter
	wsConnectionsActive prometheus.Gauge
	wsDisconnectsTotal  *prometheus.CounterVec
	wsWriteErrorsTotal  prometheus.Counter
	wsFlowControlPauses prometheus.Counter

	// Flow control metrics
	wsFlowControlResumes       prometheus.Counter
	wsFlowControlBufferedBytes prometheus.Counter
	wsFlowControlBufferDrops   prometheus.Counter

	// Output buffer metrics (ring buffer for PTY output replay)
	outputBufferUsageBytes    prometheus.Gauge
	outputBufferCapacityBytes prometheus.Gauge
	outputBufferTotalWritten  prometheus.Counter
	replayBytesTotal          prometheus.Counter
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

		// WebSocket connection metrics
		wsConnectionsTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "kubetty_websocket_connections_total",
				Help: "Total number of WebSocket connection attempts",
			},
		),
		wsConnectionsActive: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "kubetty_websocket_connections_active",
				Help: "Number of currently active WebSocket connections",
			},
		),
		wsDisconnectsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "kubetty_websocket_disconnects_total",
				Help: "Total number of WebSocket disconnections by reason",
			},
			[]string{"reason"},
		),
		wsWriteErrorsTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "kubetty_websocket_write_errors_total",
				Help: "Total number of WebSocket write errors",
			},
		),
		wsFlowControlPauses: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "kubetty_websocket_flow_control_pauses_total",
				Help: "Total number of flow control pause events",
			},
		),

		// Flow control metrics
		wsFlowControlResumes: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "kubetty_websocket_flow_control_resumes_total",
				Help: "Total number of flow control resume events",
			},
		),
		wsFlowControlBufferedBytes: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "kubetty_websocket_flow_control_buffered_bytes_total",
				Help: "Total bytes buffered during flow control pauses",
			},
		),
		wsFlowControlBufferDrops: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "kubetty_websocket_flow_control_buffer_drops_total",
				Help: "Total number of messages dropped due to full flow control buffer",
			},
		),

		// Output buffer metrics
		outputBufferUsageBytes: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "kubetty_project_output_buffer_usage_bytes",
				Help: "Current bytes stored in the PTY output ring buffer",
			},
		),
		outputBufferCapacityBytes: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "kubetty_project_output_buffer_capacity_bytes",
				Help: "Total capacity of the PTY output ring buffer",
			},
		),
		outputBufferTotalWritten: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "kubetty_project_output_buffer_total_written_bytes",
				Help: "Total bytes ever written to the PTY output buffer (monotonic)",
			},
		),
		replayBytesTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "kubetty_project_replay_bytes_total",
				Help: "Total bytes sent during buffer replay to new clients",
			},
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

// WebSocket connection metrics observers

func (m *appMetrics) observeWSConnectionAttempt() {
	if m == nil {
		return
	}
	m.wsConnectionsTotal.Inc()
}

func (m *appMetrics) observeWSConnectionOpened() {
	if m == nil {
		return
	}
	m.wsConnectionsActive.Inc()
}

func (m *appMetrics) observeWSConnectionClosed() {
	if m == nil {
		return
	}
	m.wsConnectionsActive.Dec()
}

func (m *appMetrics) observeWSDisconnect(reason string) {
	if m == nil {
		return
	}
	m.wsDisconnectsTotal.WithLabelValues(reason).Inc()
}

func (m *appMetrics) observeWSWriteError() {
	if m == nil {
		return
	}
	m.wsWriteErrorsTotal.Inc()
}

func (m *appMetrics) observeWSFlowControlPause() {
	if m == nil {
		return
	}
	m.wsFlowControlPauses.Inc()
}

func (m *appMetrics) observeFlowControlResume(bytesFlushed int) {
	if m == nil {
		return
	}
	m.wsFlowControlResumes.Inc()
	m.wsFlowControlBufferedBytes.Add(float64(bytesFlushed))
}

func (m *appMetrics) observeFlowControlBufferDrop() {
	if m == nil {
		return
	}
	m.wsFlowControlBufferDrops.Inc()
}

// Output buffer metrics observers

func (m *appMetrics) setOutputBufferCapacity(capacity int) {
	if m == nil {
		return
	}
	m.outputBufferCapacityBytes.Set(float64(capacity))
}

func (m *appMetrics) setOutputBufferUsage(usage int) {
	if m == nil {
		return
	}
	m.outputBufferUsageBytes.Set(float64(usage))
}

func (m *appMetrics) observeOutputBufferWrite(bytesWritten int) {
	if m == nil {
		return
	}
	m.outputBufferTotalWritten.Add(float64(bytesWritten))
}

func (m *appMetrics) observeReplayBytes(bytesSent int) {
	if m == nil {
		return
	}
	m.replayBytesTotal.Add(float64(bytesSent))
}
