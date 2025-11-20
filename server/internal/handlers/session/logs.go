package session

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

// LogsResponse represents the session logs response.
type LogsResponse struct {
	SessionID string              `json:"sessionId"` // Session UUID
	Logs      []sessions.LogEntry `json:"logs"`      // Log entries for the session
}

// NewSessionLogsHandler creates an HTTP handler for retrieving session logs.
//
// Endpoint: GET /session/logs
// Query Parameters:
//   - session (required): The session ID to retrieve logs for
//   - limit (optional): Maximum number of logs to return (default: 200, max: 2000)
//
// Response (200 OK):
//
//	{
//	  "sessionId": string,
//	  "logs": [
//	    {
//	      "sessionId": string,
//	      "direction": string,  // "input" or "output"
//	      "data": string,       // Base64-encoded data
//	      "createdAt": string   // ISO 8601 timestamp
//	    }
//	  ]
//	}
//
// Response (400 Bad Request):
//   - "missing session parameter" - No session ID provided
//
// Response (500 Internal Server Error):
//   - "list logs: <error>" - Database query error
//   - "encode response: <error>" - JSON encoding error
//
// The handler queries the sessions store with configurable pagination and
// returns logs as JSON. The observer parameter can be nil if metrics
// tracking is not needed.
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
			// Log error server-side but don't expose details to client
			// TODO: Add structured logging here
			_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to retrieve session logs", ""))
			return
		}

		// Ensure non-nil slice for JSON response
		if logs == nil {
			logs = []sessions.LogEntry{}
		}

		// Build and send response
		resp := LogsResponse{
			SessionID: sessionID,
			Logs:      logs,
		}

		if err := util.WriteJSON(w, http.StatusOK, resp); err != nil {
			// Log error server-side but don't expose details to client
			// TODO: Add structured logging here
			_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to encode response", ""))
		}
	}
}
