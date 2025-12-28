package main

import (
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// testMetrics is a shared metrics instance to avoid duplicate registration
var (
	testMetrics     *appMetrics
	testMetricsOnce sync.Once
)

func getTestMetrics() *appMetrics {
	testMetricsOnce.Do(func() {
		testMetrics = newAppMetrics()
	})
	return testMetrics
}

// TestNewAppMetrics verifies the metrics constructor creates all metrics
func TestNewAppMetrics(t *testing.T) {
	m := getTestMetrics()

	if m == nil {
		t.Fatal("newAppMetrics() returned nil")
	}

	if m.AppMetrics == nil {
		t.Error("AppMetrics embedded struct is nil")
	}

	if m.sessionAttached == nil {
		t.Error("sessionAttached counter is nil")
	}
	if m.sessionDetached == nil {
		t.Error("sessionDetached counter is nil")
	}
	if m.ptyExit == nil {
		t.Error("ptyExit counter vec is nil")
	}
	if m.wsConnectionsTotal == nil {
		t.Error("wsConnectionsTotal counter is nil")
	}
	if m.wsConnectionsActive == nil {
		t.Error("wsConnectionsActive gauge is nil")
	}
	if m.wsDisconnectsTotal == nil {
		t.Error("wsDisconnectsTotal counter vec is nil")
	}
	if m.wsWriteErrorsTotal == nil {
		t.Error("wsWriteErrorsTotal counter is nil")
	}
	if m.wsFlowControlPauses == nil {
		t.Error("wsFlowControlPauses counter is nil")
	}
	if m.wsFlowControlResumes == nil {
		t.Error("wsFlowControlResumes counter is nil")
	}
	if m.wsFlowControlBufferedBytes == nil {
		t.Error("wsFlowControlBufferedBytes counter is nil")
	}
	if m.wsFlowControlBufferDrops == nil {
		t.Error("wsFlowControlBufferDrops counter is nil")
	}
}

// TestObserveSessionAttached tests the session attached observer
func TestObserveSessionAttached(t *testing.T) {
	m := getTestMetrics()

	before := testutil.ToFloat64(m.sessionAttached)
	m.observeSessionAttached()
	after := testutil.ToFloat64(m.sessionAttached)

	if after != before+1 {
		t.Errorf("sessionAttached increment failed: before=%f, after=%f", before, after)
	}
}

// TestObserveSessionAttached_Nil tests nil receiver handling
func TestObserveSessionAttached_Nil(t *testing.T) {
	var m *appMetrics
	// Should not panic
	m.observeSessionAttached()
}

// TestObserveSessionDetached tests the session detached observer
func TestObserveSessionDetached(t *testing.T) {
	m := getTestMetrics()

	before := testutil.ToFloat64(m.sessionDetached)
	m.observeSessionDetached()
	after := testutil.ToFloat64(m.sessionDetached)

	if after != before+1 {
		t.Errorf("sessionDetached increment failed: before=%f, after=%f", before, after)
	}
}

// TestObserveSessionDetached_Nil tests nil receiver handling
func TestObserveSessionDetached_Nil(t *testing.T) {
	var m *appMetrics
	m.observeSessionDetached()
}

// TestObservePtyExit tests the PTY exit observer
func TestObservePtyExit(t *testing.T) {
	m := getTestMetrics()

	// Test with a unique exit code to avoid interference from other tests
	exitCode := 42
	before := testutil.ToFloat64(m.ptyExit.WithLabelValues("42"))
	m.observePtyExit(exitCode)
	after := testutil.ToFloat64(m.ptyExit.WithLabelValues("42"))

	if after != before+1 {
		t.Errorf("ptyExit{exit_code=\"42\"} increment failed: before=%f, after=%f", before, after)
	}
}

// TestObservePtyExit_Nil tests nil receiver handling
func TestObservePtyExit_Nil(t *testing.T) {
	var m *appMetrics
	m.observePtyExit(0)
}

// TestObserveWSBytes tests the WebSocket bytes observer
func TestObserveWSBytes(t *testing.T) {
	m := getTestMetrics()

	// Test that calling the method doesn't panic
	m.observeWSBytes("in", 100)
	m.observeWSBytes("out", 200)
}

// TestObserveWSBytes_Nil tests nil receiver handling
func TestObserveWSBytes_Nil(t *testing.T) {
	var m *appMetrics
	m.observeWSBytes("in", 100)
}

// TestObserveWSConnectionAttempt tests the connection attempt observer
func TestObserveWSConnectionAttempt(t *testing.T) {
	m := getTestMetrics()

	before := testutil.ToFloat64(m.wsConnectionsTotal)
	m.observeWSConnectionAttempt()
	after := testutil.ToFloat64(m.wsConnectionsTotal)

	if after != before+1 {
		t.Errorf("wsConnectionsTotal increment failed: before=%f, after=%f", before, after)
	}
}

// TestObserveWSConnectionAttempt_Nil tests nil receiver handling
func TestObserveWSConnectionAttempt_Nil(t *testing.T) {
	var m *appMetrics
	m.observeWSConnectionAttempt()
}

// TestObserveWSConnectionOpened tests the connection opened observer
func TestObserveWSConnectionOpened(t *testing.T) {
	m := getTestMetrics()

	before := testutil.ToFloat64(m.wsConnectionsActive)
	m.observeWSConnectionOpened()
	after := testutil.ToFloat64(m.wsConnectionsActive)

	if after != before+1 {
		t.Errorf("wsConnectionsActive increment failed: before=%f, after=%f", before, after)
	}

	// Clean up by closing the connection
	m.observeWSConnectionClosed()
}

// TestObserveWSConnectionOpened_Nil tests nil receiver handling
func TestObserveWSConnectionOpened_Nil(t *testing.T) {
	var m *appMetrics
	m.observeWSConnectionOpened()
}

// TestObserveWSConnectionClosed tests the connection closed observer
func TestObserveWSConnectionClosed(t *testing.T) {
	m := getTestMetrics()

	// First open a connection
	m.observeWSConnectionOpened()
	before := testutil.ToFloat64(m.wsConnectionsActive)
	m.observeWSConnectionClosed()
	after := testutil.ToFloat64(m.wsConnectionsActive)

	if after != before-1 {
		t.Errorf("wsConnectionsActive decrement failed: before=%f, after=%f", before, after)
	}
}

// TestObserveWSConnectionClosed_Nil tests nil receiver handling
func TestObserveWSConnectionClosed_Nil(t *testing.T) {
	var m *appMetrics
	m.observeWSConnectionClosed()
}

// TestObserveWSDisconnect tests the disconnect observer with reasons
func TestObserveWSDisconnect(t *testing.T) {
	m := getTestMetrics()

	// Use unique reason to avoid interference
	reason := "test_disconnect_reason"
	before := testutil.ToFloat64(m.wsDisconnectsTotal.WithLabelValues(reason))
	m.observeWSDisconnect(reason)
	after := testutil.ToFloat64(m.wsDisconnectsTotal.WithLabelValues(reason))

	if after != before+1 {
		t.Errorf("wsDisconnectsTotal{reason=%q} increment failed: before=%f, after=%f", reason, before, after)
	}
}

// TestObserveWSDisconnect_Nil tests nil receiver handling
func TestObserveWSDisconnect_Nil(t *testing.T) {
	var m *appMetrics
	m.observeWSDisconnect("error")
}

// TestObserveWSWriteError tests the write error observer
func TestObserveWSWriteError(t *testing.T) {
	m := getTestMetrics()

	before := testutil.ToFloat64(m.wsWriteErrorsTotal)
	m.observeWSWriteError()
	after := testutil.ToFloat64(m.wsWriteErrorsTotal)

	if after != before+1 {
		t.Errorf("wsWriteErrorsTotal increment failed: before=%f, after=%f", before, after)
	}
}

// TestObserveWSWriteError_Nil tests nil receiver handling
func TestObserveWSWriteError_Nil(t *testing.T) {
	var m *appMetrics
	m.observeWSWriteError()
}

// TestObserveWSFlowControlPause tests the flow control pause observer
func TestObserveWSFlowControlPause(t *testing.T) {
	m := getTestMetrics()

	before := testutil.ToFloat64(m.wsFlowControlPauses)
	m.observeWSFlowControlPause()
	after := testutil.ToFloat64(m.wsFlowControlPauses)

	if after != before+1 {
		t.Errorf("wsFlowControlPauses increment failed: before=%f, after=%f", before, after)
	}
}

// TestObserveWSFlowControlPause_Nil tests nil receiver handling
func TestObserveWSFlowControlPause_Nil(t *testing.T) {
	var m *appMetrics
	m.observeWSFlowControlPause()
}

// TestObserveFlowControlResume tests the flow control resume observer
func TestObserveFlowControlResume(t *testing.T) {
	m := getTestMetrics()

	beforeResumes := testutil.ToFloat64(m.wsFlowControlResumes)
	beforeBytes := testutil.ToFloat64(m.wsFlowControlBufferedBytes)

	bytesToFlush := 1234
	m.observeFlowControlResume(bytesToFlush)

	afterResumes := testutil.ToFloat64(m.wsFlowControlResumes)
	afterBytes := testutil.ToFloat64(m.wsFlowControlBufferedBytes)

	if afterResumes != beforeResumes+1 {
		t.Errorf("wsFlowControlResumes increment failed: before=%f, after=%f", beforeResumes, afterResumes)
	}
	if afterBytes != beforeBytes+float64(bytesToFlush) {
		t.Errorf("wsFlowControlBufferedBytes increment failed: before=%f, after=%f, expected=%f",
			beforeBytes, afterBytes, beforeBytes+float64(bytesToFlush))
	}
}

// TestObserveFlowControlResume_Nil tests nil receiver handling
func TestObserveFlowControlResume_Nil(t *testing.T) {
	var m *appMetrics
	m.observeFlowControlResume(1000)
}

// TestObserveFlowControlBufferDrop tests the buffer drop observer
func TestObserveFlowControlBufferDrop(t *testing.T) {
	m := getTestMetrics()

	before := testutil.ToFloat64(m.wsFlowControlBufferDrops)
	m.observeFlowControlBufferDrop()
	after := testutil.ToFloat64(m.wsFlowControlBufferDrops)

	if after != before+1 {
		t.Errorf("wsFlowControlBufferDrops increment failed: before=%f, after=%f", before, after)
	}
}

// TestObserveFlowControlBufferDrop_Nil tests nil receiver handling
func TestObserveFlowControlBufferDrop_Nil(t *testing.T) {
	var m *appMetrics
	m.observeFlowControlBufferDrop()
}

// TestMetricsIntegration tests a realistic sequence of metric observations
func TestMetricsIntegration(t *testing.T) {
	m := getTestMetrics()

	// Record starting values
	startActive := testutil.ToFloat64(m.wsConnectionsActive)

	// Simulate a session lifecycle
	m.observeWSConnectionAttempt()
	m.observeWSConnectionOpened()
	m.observeSessionAttached()

	// Verify connection is active
	if testutil.ToFloat64(m.wsConnectionsActive) != startActive+1 {
		t.Error("Expected active connections to increment")
	}

	// Simulate some data flow
	m.observeWSBytes("in", 100)
	m.observeWSBytes("out", 5000)

	// Simulate flow control
	m.observeWSFlowControlPause()
	m.observeFlowControlResume(2048)

	// Simulate disconnect
	m.observeSessionDetached()
	m.observeWSConnectionClosed()
	m.observeWSDisconnect("integration_test")

	// Verify connection was closed
	if testutil.ToFloat64(m.wsConnectionsActive) != startActive {
		t.Error("Expected active connections to return to start value")
	}
}

// TestMetricNames verifies metrics have the expected names
func TestMetricNames(t *testing.T) {
	m := getTestMetrics()

	// Collect all metrics and verify names contain expected prefixes
	metrics := []prometheus.Collector{
		m.sessionAttached,
		m.sessionDetached,
		m.ptyExit,
		m.wsConnectionsTotal,
		m.wsConnectionsActive,
		m.wsDisconnectsTotal,
		m.wsWriteErrorsTotal,
		m.wsFlowControlPauses,
		m.wsFlowControlResumes,
		m.wsFlowControlBufferedBytes,
		m.wsFlowControlBufferDrops,
	}

	for _, metric := range metrics {
		desc := make(chan *prometheus.Desc, 1)
		metric.Describe(desc)
		d := <-desc
		if d == nil {
			t.Error("Metric has nil descriptor")
			continue
		}
		// Verify the metric has a proper description
		str := d.String()
		if !strings.Contains(str, "kubetty") {
			t.Errorf("Metric name should contain 'kubetty': %s", str)
		}
	}
}

// TestObserverNilSafety runs all observer methods on nil receiver to ensure no panics
func TestObserverNilSafety(t *testing.T) {
	var m *appMetrics

	// All these should be no-ops without panic
	m.observeSessionAttached()
	m.observeSessionDetached()
	m.observePtyExit(0)
	m.observeWSBytes("in", 100)
	m.observeWSConnectionAttempt()
	m.observeWSConnectionOpened()
	m.observeWSConnectionClosed()
	m.observeWSDisconnect("test")
	m.observeWSWriteError()
	m.observeWSFlowControlPause()
	m.observeFlowControlResume(100)
	m.observeFlowControlBufferDrop()

	// Output buffer metrics (new)
	m.setOutputBufferCapacity(8 * 1024 * 1024)
	m.setOutputBufferUsage(1024)
	m.observeOutputBufferWrite(512)
	m.observeReplayBytes(65536)
}
