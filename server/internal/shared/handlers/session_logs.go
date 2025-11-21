// Package handlers provides reusable HTTP request handlers for common KubeTTY API operations.
//
// This package centralizes shared handler implementations that are used across multiple
// KubeTTY components (gateway, project, legacy). It promotes code reuse and consistent
// API behavior for operations like session log retrieval.
//
// Key features:
//   - Session logs handler with configurable pagination (default: 200, max: 2000)
//   - Automatic metrics tracking via StoreMetricsObserver interface
//   - Standardized error responses using shared/errors package
//   - Input validation and bounds checking for query parameters
//
// All handlers follow the KubeTTY API handler standards defined in
// docs/development/api-handler-standards.md with proper context propagation,
// error handling, and metrics instrumentation.
package handlers

import (
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
// It queries the sessions store with configurable pagination, optional search,
// and direction filtering, then returns logs as JSON.
//
// Query parameters:
//   - session (required): The session ID to retrieve logs for
//   - limit (optional): Maximum number of logs to return (default: 200, max: 2000)
//   - search (optional): Case-insensitive substring search on log content
//   - direction (optional): Filter by direction ("in" for client input, "out" for session output)
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

		// Validate session ID length (UUIDs are 36 chars)
		if len(sessionID) > 64 {
			_ = apierrors.WriteError(w, apierrors.BadRequest("invalid session parameter", "session ID too long"))
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

		// Parse optional filter parameters
		var filter *sessions.LogFilter
		search := r.URL.Query().Get("search")
		direction := r.URL.Query().Get("direction")

		// Validate search term length to prevent DoS
		if len(search) > 500 {
			_ = apierrors.WriteError(w, apierrors.BadRequest("search term too long", "maximum 500 characters"))
			return
		}

		// Validate direction if provided
		if direction != "" && direction != "in" && direction != "out" {
			_ = apierrors.WriteError(w, apierrors.BadRequest("invalid direction parameter", "direction must be 'in' or 'out'"))
			return
		}

		// Build filter if any filter params are provided
		if search != "" || direction != "" {
			filter = &sessions.LogFilter{
				Search:    search,
				Direction: direction,
			}
		}

		// Query logs from store
		start := time.Now()
		result, err := store.ListLogs(ctx, sessionID, limit, filter)

		// Observe metrics if observer is provided
		if observer != nil {
			observer.ObserveStore("ListLogs", start, err)
		}

		if err != nil {
			// Log error server-side but don't expose details to client
			// TODO: Add structured logging here
			_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to retrieve session logs", ""))
			return
		}

		// Ensure non-nil slice for JSON response
		logs := result.Logs
		if logs == nil {
			logs = []sessions.LogEntry{}
		}

		// Build and send response
		resp := map[string]any{
			"sessionId":  sessionID,
			"logs":       logs,
			"matchCount": result.MatchCount,
		}

		if err := util.WriteJSON(w, http.StatusOK, resp); err != nil {
			// Log error server-side but don't expose details to client
			// TODO: Add structured logging here
			_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to encode response", ""))
		}
	}
}
