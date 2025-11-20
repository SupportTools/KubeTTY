package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/supporttools/KubeTTY/server/internal/sessions"
	apierrors "github.com/supporttools/KubeTTY/server/internal/shared/errors"
	"github.com/supporttools/KubeTTY/server/internal/shared/util"
)

// StoreMetricsObserver defines an interface for observing store operations.
// Implementations can use this to track metrics (e.g., Prometheus counters).
type StoreMetricsObserver interface {
	ObserveStore(operation string, start time.Time, err error)
}

// NewSessionLogsHandler creates an HTTP handler for retrieving session logs.
// It queries the sessions store with configurable pagination and returns logs as JSON.
//
// Query parameters:
//   - session (required): The session ID to retrieve logs for
//   - limit (optional): Maximum number of logs to return (default: 200, max: 2000)
//
// The observer parameter can be nil if metrics tracking is not needed.
func NewSessionLogsHandler(store sessions.Store, observer StoreMetricsObserver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sessionID := r.URL.Query().Get("session")
		if sessionID == "" {
			_ = apierrors.WriteError(w, apierrors.BadRequest("missing session parameter", ""))
			return
		}

		// Parse pagination limit with defaults and bounds
		limit := 200
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil {
				switch {
				case parsed <= 0:
					// Keep default
				case parsed > 2000:
					limit = 2000
				default:
					limit = parsed
				}
			}
		}

		// Query logs from store
		start := time.Now()
		logs, err := store.ListLogs(ctx, sessionID, limit)

		// Observe metrics if observer is provided
		if observer != nil {
			observer.ObserveStore("ListLogs", start, err)
		}

		if err != nil {
			_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to retrieve session logs", fmt.Sprintf("%v", err)))
			return
		}

		// Ensure non-nil slice for JSON response
		if logs == nil {
			logs = []sessions.LogEntry{}
		}

		// Build and send response
		resp := map[string]any{
			"sessionId": sessionID,
			"logs":      logs,
		}

		if err := util.WriteJSON(w, http.StatusOK, resp); err != nil {
			_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to encode response", fmt.Sprintf("%v", err)))
		}
	}
}
