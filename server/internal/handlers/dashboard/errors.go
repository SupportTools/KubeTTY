package dashboard

import (
	"net/http"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/supporttools/KubeTTY/server/internal/shared/util"
)

// GetErrors handles GET /api/admin/dashboard/errors
// Query params: ?limit=50&project=<id>
// Returns recent errors for troubleshooting.
func (h *Handlers) GetErrors(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse limit parameter (default: 50, max: 200)
	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}

	// Parse optional project filter
	_ = r.URL.Query().Get("project") // projectFilter - for future use

	var errors []DashboardError

	// Get recently failed projects
	if h.projectStore != nil {
		// Get projects that failed in the last 24 hours
		since := time.Now().Add(-24 * time.Hour)
		failedProjects, err := h.projectStore.GetRecentlyFailed(ctx, since, limit/2)
		if err != nil {
			log.WithError(err).Warn("dashboard: failed to get recently failed projects")
		} else {
			for _, p := range failedProjects {
				errors = append(errors, DashboardError{
					Type:        "project_failed",
					Reason:      string(p.Status),
					ProjectID:   p.ID.String(),
					ProjectName: p.DisplayName,
					Timestamp:   p.UpdatedAt,
					Details:     p.StatusMessage,
				})
			}
		}
	}

	// Get recent tab errors
	if h.tabStore != nil {
		tabErrors, err := h.tabStore.GetRecentErrors(ctx, limit/2)
		if err != nil {
			log.WithError(err).Warn("dashboard: failed to get recent tab errors")
		} else {
			for _, t := range tabErrors {
				var lastErr string
				if t.LastError != nil {
					lastErr = *t.LastError
				}
				errors = append(errors, DashboardError{
					Type:        "tab_error",
					Reason:      string(t.Status),
					ProjectID:   t.ProjectID,
					ProjectName: t.ProjectID, // We don't have display name in tabs
					TabID:       t.TabID,
					Timestamp:   t.UpdatedAt,
					Details:     lastErr,
				})
			}
		}
	}

	// Sort errors by timestamp (most recent first)
	// Simple bubble sort since we have at most 100 items
	for i := 0; i < len(errors); i++ {
		for j := i + 1; j < len(errors); j++ {
			if errors[j].Timestamp.After(errors[i].Timestamp) {
				errors[i], errors[j] = errors[j], errors[i]
			}
		}
	}

	// Trim to limit
	if len(errors) > limit {
		errors = errors[:limit]
	}

	response := ErrorsResponse{
		Errors: errors,
		Total:  len(errors),
	}

	util.WriteJSON(w, http.StatusOK, response)
}
