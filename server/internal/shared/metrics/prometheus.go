// Package metrics provides Prometheus instrumentation for KubeTTY server components.
//
// This package centralizes all Prometheus metrics collection for WebSocket connections,
// database operations, and HTTP request tracking. It powers the /metrics endpoint used
// by Prometheus scraping and Grafana dashboards for KubeTTY observability.
//
// Collected metrics include:
//   - kubetty_websocket_bytes_total: WebSocket byte transmission (rx/tx)
//   - kubetty_store_duration_seconds: Database operation latency histograms
//   - kubetty_store_errors_total: Database operation error counts
//   - kubetty_http_duration_seconds: HTTP request latency histograms
//   - kubetty_http_requests_total: HTTP request counts by route/method/status
//   - kubetty_pvc_usage_bytes: Current PVC usage in bytes
//   - kubetty_pvc_limit_bytes: PVC capacity limit in bytes
//   - kubetty_pvc_usage_percent: PVC usage as percentage (0-100)
//   - kubetty_pvc_expansions_total: Total number of PVC expansions triggered
//   - kubetty_pvc_expansion_failed_total: Total number of failed PVC expansions
//   - kubetty_pvc_current_size_bytes: Current PVC requested size in bytes
//
// All metrics are automatically registered with Prometheus default registry via promauto
// and follow Prometheus naming conventions for consistency with Kubernetes ecosystem monitoring.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Leader election metrics (package-level for access from gateway)
var (
	// LeaderStatus indicates whether this instance is the leader (1) or not (0).
	LeaderStatus = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "kubetty_gateway_leader_status",
			Help: "Whether this gateway instance is the leader (1) or not (0)",
		},
	)

	// LeaderTransitionsTotal counts total leader transitions.
	LeaderTransitionsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubetty_gateway_leader_transitions_total",
			Help: "Total number of leader transitions",
		},
		[]string{"type"}, // "acquired" or "lost"
	)

	// LeaderIdentity stores the current leader identity as an info metric.
	LeaderIdentity = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kubetty_gateway_leader_identity",
			Help: "Current leader identity (value is always 1, labels contain identity info)",
		},
		[]string{"identity", "is_self"},
	)

	// LeaderElectionRetriesTotal counts the number of leader election retry attempts
	// after losing leadership due to transient failures.
	LeaderElectionRetriesTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "kubetty_gateway_leader_election_retries_total",
			Help: "Total number of leader election retry attempts after losing leadership",
		},
	)
)

// PVC storage monitoring metrics (package-level for access from controller)
var (
	// PVCUsageBytes tracks current PVC disk usage in bytes per project.
	PVCUsageBytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kubetty_pvc_usage_bytes",
			Help: "Current PVC disk usage in bytes",
		},
		[]string{"project", "pvc"},
	)

	// PVCLimitBytes tracks PVC capacity limit in bytes per project.
	PVCLimitBytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kubetty_pvc_limit_bytes",
			Help: "PVC capacity limit in bytes",
		},
		[]string{"project", "pvc"},
	)

	// PVCUsagePercent tracks PVC usage as percentage (0-100) per project.
	PVCUsagePercent = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kubetty_pvc_usage_percent",
			Help: "PVC usage as percentage (0-100)",
		},
		[]string{"project", "pvc"},
	)

	// PVCExpansionsTotal counts total PVC expansions by status (success/failed).
	PVCExpansionsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubetty_pvc_expansions_total",
			Help: "Total number of PVC expansions triggered",
		},
		[]string{"project", "pvc", "status"},
	)

	// PVCExpansionFailedTotal counts failed PVC expansions by reason (for alerting).
	PVCExpansionFailedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubetty_pvc_expansion_failed_total",
			Help: "Total number of failed PVC expansions (for alerting)",
		},
		[]string{"project", "pvc", "reason"},
	)

	// PVCCurrentSizeBytes tracks current PVC requested size in bytes.
	PVCCurrentSizeBytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kubetty_pvc_current_size_bytes",
			Help: "Current PVC requested size in bytes",
		},
		[]string{"project", "pvc"},
	)
)

// AppMetrics holds all Prometheus metrics for the application.
type AppMetrics struct {
	wsBytes       *prometheus.CounterVec
	storeDuration *prometheus.HistogramVec
	storeErrors   *prometheus.CounterVec
	httpDuration  *prometheus.HistogramVec
	httpRequests  *prometheus.CounterVec
}

// NewAppMetrics creates and registers all Prometheus metrics.
func NewAppMetrics() *AppMetrics {
	return &AppMetrics{
		wsBytes: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "kubetty_websocket_bytes_total",
				Help: "Total bytes transmitted over WebSocket connections",
			},
			[]string{"type"},
		),
		storeDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "kubetty_store_duration_seconds",
				Help:    "Duration of store operations",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"operation"},
		),
		storeErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "kubetty_store_errors_total",
				Help: "Total number of store errors",
			},
			[]string{"operation"},
		),
		httpDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "kubetty_http_duration_seconds",
				Help:    "Duration of HTTP requests",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"route", "method", "status"},
		),
		httpRequests: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "kubetty_http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"route", "method", "status"},
		),
	}
}

// ObserveWSBytes records WebSocket byte transmission.
func (m *AppMetrics) ObserveWSBytes(typ string, n int) {
	m.wsBytes.WithLabelValues(typ).Add(float64(n))
}

// ObserveStore records store operation duration and errors.
func (m *AppMetrics) ObserveStore(operation string, dur time.Duration, err error) {
	m.storeDuration.WithLabelValues(operation).Observe(dur.Seconds())
	if err != nil {
		m.storeErrors.WithLabelValues(operation).Inc()
	}
}

// InstrumentHandler wraps an HTTP handler with Prometheus metrics instrumentation.
func (m *AppMetrics) InstrumentHandler(route string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		h.ServeHTTP(rec, r)

		dur := time.Since(start)
		status := strconv.Itoa(rec.status)
		m.httpDuration.WithLabelValues(route, r.Method, status).Observe(dur.Seconds())
		m.httpRequests.WithLabelValues(route, r.Method, status).Inc()
	})
}
