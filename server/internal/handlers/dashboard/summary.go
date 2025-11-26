package dashboard

import (
	"net/http"

	log "github.com/sirupsen/logrus"

	"github.com/supporttools/KubeTTY/server/internal/projects"
	"github.com/supporttools/KubeTTY/server/internal/shared/util"
)

// GetSummary handles GET /api/admin/dashboard/summary
// Returns overview statistics for the dashboard.
func (h *Handlers) GetSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get project status counts
	projectCounts := ProjectCounts{}
	if h.projectStore != nil {
		statusCounts, err := h.projectStore.GetStatusCounts(ctx)
		if err != nil {
			log.WithError(err).Warn("dashboard: failed to get project status counts")
		} else {
			projectCounts.Running = statusCounts[projects.StatusRunning]
			projectCounts.Failed = statusCounts[projects.StatusFailed]
			for _, count := range statusCounts {
				projectCounts.Total += count
			}
		}
	}

	// Get tab counts
	tabCounts := TabCounts{}
	if h.tabStore != nil {
		statusCounts, err := h.tabStore.GetStatusCounts(ctx)
		if err != nil {
			log.WithError(err).Warn("dashboard: failed to get tab status counts")
		} else {
			// Count active tabs (connected or connecting)
			tabCounts.Active = statusCounts["connected"] + statusCounts["connecting"] + statusCounts["reconnecting"]
			for _, count := range statusCounts {
				tabCounts.Total += count
			}
		}
	}

	// Get metrics from collector
	var activeConnections int
	var last24h Last24hMetrics

	if h.metrics != nil {
		activeConnections = h.metrics.GetActiveConnections()

		totalConnections := h.metrics.GetTotalConnections()
		totalDisconnects := h.metrics.GetTotalDisconnects()
		totalErrors := h.metrics.GetTotalErrors()

		last24h = Last24hMetrics{
			Connections: totalConnections,
			Disconnects: totalDisconnects,
			Errors:      totalErrors,
		}

		// Calculate error rate as percentage
		if totalConnections > 0 {
			last24h.ErrorRate = float64(totalErrors) / float64(totalConnections) * 100
		}
	}

	response := SummaryResponse{
		ActiveConnections: activeConnections,
		Projects:          projectCounts,
		Tabs:              tabCounts,
		Last24h:           last24h,
	}

	util.WriteJSON(w, http.StatusOK, response)
}
