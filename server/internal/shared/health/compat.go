package health

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/supporttools/KubeTTY/server/internal/shared/util"
)

// NewCompatHandler creates a health check handler that maintains backward compatibility
// with the legacy response format used by gateway and project binaries.
//
// Legacy format:
//
//	{
//	  "status": "healthy",
//	  "components": {
//	    "database": "ok",
//	    "gateway": "enabled"
//	  }
//	}
//
// This wrapper uses the shared health infrastructure internally but transforms
// the response to match the exact format expected by monitoring systems.
func NewCompatHandler(db Pinger, checkers ...Checker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		status := "healthy"
		httpStatus := http.StatusOK
		components := make(map[string]string)

		// Check database connectivity
		if db != nil {
			if err := db.Ping(ctx); err != nil {
				status = "unhealthy"
				httpStatus = http.StatusServiceUnavailable
				components["database"] = fmt.Sprintf("error: %v", err)
			} else {
				components["database"] = "ok"
			}
		} else {
			components["database"] = "not_configured"
		}

		// Run additional checkers
		// Checkers return format: (healthy, "name:status")
		for _, checker := range checkers {
			healthy, message := checker.Check(ctx)
			if !healthy {
				// If a checker reports unhealthy, mark overall status
				// (though current implementation always returns healthy=true)
				status = "unhealthy"
				if httpStatus == http.StatusOK {
					httpStatus = http.StatusServiceUnavailable
				}
			}

			// Parse "name:status" format
			parts := strings.SplitN(message, ":", 2)
			if len(parts) == 2 {
				components[parts[0]] = parts[1]
			} else {
				// Fallback for checkers that return just component name
				components[message] = "ok"
			}
		}

		// Write response in legacy format
		_ = util.WriteJSON(w, httpStatus, map[string]any{
			"status":     status,
			"components": components,
		})
	}
}
