package main

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type appMetrics struct {
	activeSessions prometheus.Gauge
	forceAttach    prometheus.Counter
	ptyExits       *prometheus.CounterVec
	wsBytes        *prometheus.CounterVec
	storeDuration  *prometheus.HistogramVec
	storeErrors    *prometheus.CounterVec
	httpDuration   *prometheus.HistogramVec
	httpRequests   *prometheus.CounterVec
}

func newAppMetrics() *appMetrics {
	m := &appMetrics{
		activeSessions: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kubetty_sessions_active",
			Help: "Number of PTY sessions currently attached.",
		}),
		forceAttach: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "kubetty_sessions_force_attach_total",
			Help: "Number of times a force attach replaced an existing client.",
		}),
		ptyExits: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubetty_pty_exits_total",
			Help: "Counts PTY process exits labeled by result.",
		}, []string{"result"}),
		wsBytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubetty_ws_bytes_total",
			Help: "Bytes relayed over WebSocket in each direction.",
		}, []string{"direction"}),
		storeDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "kubetty_store_operation_seconds",
			Help:    "Time spent performing CNPG store operations.",
			Buckets: prometheus.DefBuckets,
		}, []string{"operation"}),
		storeErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubetty_store_errors_total",
			Help: "Counts store operations that returned an error.",
		}, []string{"operation"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "kubetty_http_request_seconds",
			Help:    "Duration of HTTP handlers.",
			Buckets: prometheus.DefBuckets,
		}, []string{"handler", "method"}),
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubetty_http_requests_total",
			Help: "HTTP requests handled labeled by handler/method/status.",
		}, []string{"handler", "method", "status"}),
	}
	prometheus.MustRegister(
		m.activeSessions,
		m.forceAttach,
		m.ptyExits,
		m.wsBytes,
		m.storeDuration,
		m.storeErrors,
		m.httpDuration,
		m.httpRequests,
	)
	return m
}

func (m *appMetrics) sessionAttached(force bool) {
	if m == nil {
		return
	}
	m.activeSessions.Inc()
	if force {
		m.forceAttach.Inc()
	}
}

func (m *appMetrics) sessionDetached() {
	if m == nil {
		return
	}
	m.activeSessions.Dec()
}

func (m *appMetrics) observePtyExit(err error) {
	if m == nil {
		return
	}
	result := "success"
	if err != nil {
		result = "error"
	}
	m.ptyExits.WithLabelValues(result).Inc()
}

func (m *appMetrics) observeWSBytes(direction string, n int) {
	if m == nil || n <= 0 {
		return
	}
	m.wsBytes.WithLabelValues(direction).Add(float64(n))
}

func (m *appMetrics) observeStore(op string, duration time.Duration, err error) {
	if m == nil {
		return
	}
	m.storeDuration.WithLabelValues(op).Observe(duration.Seconds())
	if err != nil {
		m.storeErrors.WithLabelValues(op).Inc()
	}
}

func (m *appMetrics) instrumentHandler(name string, handler http.Handler) http.Handler {
	if m == nil {
		return handler
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		handler.ServeHTTP(rec, r)
		elapsed := time.Since(start)
		status := strconv.Itoa(rec.status)
		m.httpRequests.WithLabelValues(name, r.Method, status).Inc()
		m.httpDuration.WithLabelValues(name, r.Method).Observe(elapsed.Seconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  atomic.Bool
}

func (sr *statusRecorder) WriteHeader(code int) {
	if sr.wrote.CompareAndSwap(false, true) {
		sr.status = code
		sr.ResponseWriter.WriteHeader(code)
		return
	}
	sr.ResponseWriter.WriteHeader(code)
}

func (sr *statusRecorder) Write(b []byte) (int, error) {
	if sr.wrote.CompareAndSwap(false, true) {
		sr.status = http.StatusOK
	}
	return sr.ResponseWriter.Write(b)
}

func (sr *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := sr.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("statusRecorder: underlying writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func (sr *statusRecorder) Flush() {
	if flusher, ok := sr.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (sr *statusRecorder) CloseNotify() <-chan bool {
	if notifier, ok := sr.ResponseWriter.(http.CloseNotifier); ok {
		return notifier.CloseNotify()
	}
	return nil
}

func (sr *statusRecorder) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := sr.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}
