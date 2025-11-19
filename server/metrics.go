package main

import (
	"expvar"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type cleanupMetrics struct {
	runCount       atomic.Int64
	errorCount     atomic.Int64
	prunedRows     atomic.Int64
	trimmedRows    atomic.Int64
	lastRunUnix    atomic.Int64
	lastErrorUnix  atomic.Int64
	lastErrorMsg   atomic.Value
	runCounter     prometheus.Counter
	errorCounter   prometheus.Counter
	prunedCounter  prometheus.Counter
	trimmedCounter prometheus.Counter
	lastRunGauge   prometheus.Gauge
	lastErrorGauge prometheus.Gauge
}

func newCleanupMetrics() *cleanupMetrics {
	cm := &cleanupMetrics{
		runCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "kubetty_log_cleanup_runs_total",
			Help: "Total number of log-retention runs executed.",
		}),
		errorCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "kubetty_log_cleanup_errors_total",
			Help: "Total number of log-retention errors encountered.",
		}),
		prunedCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "kubetty_log_cleanup_rows_pruned_total",
			Help: "Total session log rows pruned due to age.",
		}),
		trimmedCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "kubetty_log_cleanup_rows_trimmed_total",
			Help: "Total session log rows trimmed to enforce caps.",
		}),
		lastRunGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kubetty_log_cleanup_last_run_timestamp_seconds",
			Help: "Unix timestamp of the last completed log cleanup run.",
		}),
		lastErrorGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kubetty_log_cleanup_last_error_timestamp_seconds",
			Help: "Unix timestamp of the last log cleanup error (0 if none).",
		}),
	}
	prometheus.MustRegister(
		cm.runCounter,
		cm.errorCounter,
		cm.prunedCounter,
		cm.trimmedCounter,
		cm.lastRunGauge,
		cm.lastErrorGauge,
	)
	expvar.Publish("log_cleanup", expvar.Func(func() any {
		return cm.snapshot()
	}))
	return cm
}

func (m *cleanupMetrics) recordRunStart() {
	now := time.Now().Unix()
	m.lastRunUnix.Store(now)
	m.runCount.Add(1)
	m.runCounter.Inc()
	m.lastRunGauge.Set(float64(now))
}

func (m *cleanupMetrics) recordError(err error) {
	if err == nil {
		return
	}
	now := time.Now().Unix()
	m.errorCount.Add(1)
	m.lastErrorUnix.Store(now)
	m.lastErrorMsg.Store(err.Error())
	m.errorCounter.Inc()
	m.lastErrorGauge.Set(float64(now))
}

func (m *cleanupMetrics) addPruned(rows int64) {
	if rows > 0 {
		m.prunedRows.Add(rows)
		m.prunedCounter.Add(float64(rows))
	}
}

func (m *cleanupMetrics) addTrimmed(rows int64) {
	if rows > 0 {
		m.trimmedRows.Add(rows)
		m.trimmedCounter.Add(float64(rows))
	}
}

func (m *cleanupMetrics) snapshot() map[string]any {
	lastRun := m.lastRunUnix.Load()
	lastErrUnix := m.lastErrorUnix.Load()
	lastErrVal, _ := m.lastErrorMsg.Load().(string)
	return map[string]any{
		"runs":          m.runCount.Load(),
		"errors":        m.errorCount.Load(),
		"prunedRows":    m.prunedRows.Load(),
		"trimmedRows":   m.trimmedRows.Load(),
		"lastRunUnix":   lastRun,
		"lastErrorUnix": lastErrUnix,
		"lastError":     lastErrVal,
	}
}
