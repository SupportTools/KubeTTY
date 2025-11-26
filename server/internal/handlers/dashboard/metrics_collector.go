package dashboard

// NullMetricsCollector implements MetricsCollector with zero values.
// This is used when WebSocket metrics are not available (e.g., in gateway mode).
// Gateway mode tracks tabs/projects in the database, while WebSocket metrics
// are tracked per-project in the project pods.
type NullMetricsCollector struct{}

// NewNullMetricsCollector creates a metrics collector that returns zero values.
func NewNullMetricsCollector() *NullMetricsCollector {
	return &NullMetricsCollector{}
}

func (c *NullMetricsCollector) GetActiveConnections() int {
	return 0
}

func (c *NullMetricsCollector) GetTotalConnections() int64 {
	return 0
}

func (c *NullMetricsCollector) GetTotalDisconnects() int64 {
	return 0
}

func (c *NullMetricsCollector) GetDisconnectsByReason() map[string]int64 {
	return make(map[string]int64)
}

func (c *NullMetricsCollector) GetTotalErrors() int64 {
	return 0
}

func (c *NullMetricsCollector) GetFlowControlPauses() int64 {
	return 0
}

func (c *NullMetricsCollector) GetWriteErrors() int64 {
	return 0
}
