package dashboard

import (
	"net/http"

	"github.com/supporttools/KubeTTY/server/internal/shared/util"
)

// GetMetrics handles GET /api/admin/dashboard/metrics
// Query params: ?period=1h|24h|7d (default: 1h)
// Returns connection metrics and time series data.
func (h *Handlers) GetMetrics(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "1h"
	}

	// Validate period
	validPeriods := map[string]bool{"1h": true, "24h": true, "7d": true}
	if !validPeriods[period] {
		http.Error(w, "invalid period: must be 1h, 24h, or 7d", http.StatusBadRequest)
		return
	}

	// Get metrics from collector
	var disconnectsByReason map[string]int64
	var flowControlPauses int64
	var writeErrors int64

	if h.metrics != nil {
		disconnectsByReason = h.metrics.GetDisconnectsByReason()
		flowControlPauses = h.metrics.GetFlowControlPauses()
		writeErrors = h.metrics.GetWriteErrors()
	} else {
		disconnectsByReason = make(map[string]int64)
	}

	// Note: Time series data would require either:
	// 1. Querying Prometheus directly for historical data
	// 2. Storing snapshots in the database
	// For now, we return current metrics without time series
	response := MetricsResponse{
		Period:               period,
		ConnectionTimeseries: []ConnectionDataPoint{}, // Would need Prometheus query
		DisconnectsByReason:  disconnectsByReason,
		FlowControlPauses:    flowControlPauses,
		WriteErrors:          writeErrors,
	}

	util.WriteJSON(w, http.StatusOK, response)
}
