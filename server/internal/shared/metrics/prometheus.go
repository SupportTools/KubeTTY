package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
