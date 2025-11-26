package dashboard

import (
	"net/http"

	log "github.com/sirupsen/logrus"

	"github.com/supporttools/KubeTTY/server/internal/projects"
	"github.com/supporttools/KubeTTY/server/internal/shared/util"
)

// GetUsage handles GET /api/admin/dashboard/usage
// Query params: ?period=24h|7d|30d (default: 24h)
// Returns usage analytics and trends.
func (h *Handlers) GetUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "24h"
	}

	// Validate period
	validPeriods := map[string]bool{"24h": true, "7d": true, "30d": true}
	if !validPeriods[period] {
		http.Error(w, "invalid period: must be 24h, 7d, or 30d", http.StatusBadRequest)
		return
	}

	// Get project connection counts (based on active tabs)
	var topProjects []ProjectUsage
	if h.tabStore != nil && h.projectStore != nil {
		// Get active tab counts by project
		tabCounts, err := h.tabStore.GetActiveCountByProject(ctx)
		if err != nil {
			log.WithError(err).Warn("dashboard: failed to get tab counts by project")
		} else {
			// Get project details for display names
			projectList, err := h.projectStore.List(ctx, projects.ListFilter{})
			if err != nil {
				log.WithError(err).Warn("dashboard: failed to get project list")
			}

			// Create map for quick lookup
			projectMap := make(map[string]projects.Project)
			for _, p := range projectList {
				projectMap[p.Name] = p
			}

			// Build top projects list
			for projectID, count := range tabCounts {
				displayName := projectID
				if p, ok := projectMap[projectID]; ok {
					displayName = p.DisplayName
				}

				topProjects = append(topProjects, ProjectUsage{
					ProjectID:   projectID,
					Name:        projectID,
					DisplayName: displayName,
					Connections: int64(count),
				})
			}

			// Sort by connection count (descending)
			for i := 0; i < len(topProjects); i++ {
				for j := i + 1; j < len(topProjects); j++ {
					if topProjects[j].Connections > topProjects[i].Connections {
						topProjects[i], topProjects[j] = topProjects[j], topProjects[i]
					}
				}
			}

			// Limit to top 10
			if len(topProjects) > 10 {
				topProjects = topProjects[:10]
			}
		}
	}

	response := UsageResponse{
		Period:             period,
		HourlyConnections:  []HourlyCount{}, // Would need time-series data from Prometheus or DB
		TopProjects:        topProjects,
		PeakHour:           "", // Would need historical analysis
		AvgSessionDuration: 0,  // Would need session duration tracking
	}

	util.WriteJSON(w, http.StatusOK, response)
}
