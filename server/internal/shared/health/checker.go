// Package health provides a flexible health check system for KubeTTY server components.
//
// This package implements an extensible health check framework that validates database
// connectivity and allows custom health checkers to be plugged in. It powers the /api/healthz
// endpoint used by Kubernetes liveness and readiness probes.
//
// Key features:
//   - Database connectivity verification via Ping()
//   - Pluggable custom health checkers via Checker interface
//   - JSON health status responses with component-level details
//   - 5-second timeout for health check operations
//   - HTTP 503 Service Unavailable on failure, 200 OK on success
//
// The health check handler is designed to work with CloudNativePG database pools,
// Kubernetes pod lifecycle management, and custom application-level health validation.
package health

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/supporttools/KubeTTY/server/internal/shared/util"
)

// Checker is an interface for components that can report their health status.
type Checker interface {
	Check(ctx context.Context) (healthy bool, message string)
}

// Pinger is an interface for database connectivity checks.
type Pinger interface {
	Ping(ctx context.Context) error
}

// NewHandler creates an HTTP handler that checks database connectivity
// and optionally runs additional health checks provided via the Checker interface.
func NewHandler(db Pinger, checkers ...Checker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		status := map[string]any{
			"status": "healthy",
		}

		// Check database connectivity
		if db != nil {
			if err := db.Ping(ctx); err != nil {
				status["status"] = "unhealthy"
				status["database"] = "unavailable"
				status["error"] = err.Error()
				util.WriteJSON(w, http.StatusServiceUnavailable, status)
				return
			}
			status["database"] = "connected"
		}

		// Run additional health checks
		for i, checker := range checkers {
			healthy, message := checker.Check(ctx)
			if !healthy {
				status["status"] = "unhealthy"
				status[message] = "unavailable"
				_ = util.WriteJSON(w, http.StatusServiceUnavailable, status)
				return
			}
			// Store checker result with generic key if no specific message
			if message != "" {
				status[message] = "ok"
			} else {
				status[fmt.Sprintf("checker_%d", i)] = "ok"
			}
		}

		_ = util.WriteJSON(w, http.StatusOK, status)
	}
}
