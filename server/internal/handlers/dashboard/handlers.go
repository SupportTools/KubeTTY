// Package dashboard provides HTTP handlers for the admin dashboard API.
package dashboard

import (
	"context"
	"time"

	"github.com/supporttools/KubeTTY/server/internal/gateway/tabs"
	"github.com/supporttools/KubeTTY/server/internal/projects"
)

// ProjectStore defines the interface for project database operations needed by dashboard.
type ProjectStore interface {
	List(ctx context.Context, filter projects.ListFilter) ([]projects.Project, error)
	GetStatusCounts(ctx context.Context) (map[projects.ProjectStatus]int, error)
	GetRecentlyFailed(ctx context.Context, since time.Time, limit int) ([]projects.Project, error)
}

// TabStore defines the interface for tab database operations needed by dashboard.
type TabStore interface {
	GetStatusCounts(ctx context.Context) (map[string]int, error)
	GetRecentErrors(ctx context.Context, limit int) ([]tabs.Tab, error)
	GetActiveCountByProject(ctx context.Context) (map[string]int, error)
}

// MetricsCollector defines the interface for Prometheus metrics collection.
type MetricsCollector interface {
	GetActiveConnections() int
	GetTotalConnections() int64
	GetTotalDisconnects() int64
	GetDisconnectsByReason() map[string]int64
	GetTotalErrors() int64
	GetFlowControlPauses() int64
	GetWriteErrors() int64
}

// Handlers provides HTTP handlers for dashboard API endpoints.
type Handlers struct {
	projectStore ProjectStore
	tabStore     TabStore
	metrics      MetricsCollector
}

// New creates a new Handlers instance.
func New(projectStore ProjectStore, tabStore TabStore, metrics MetricsCollector) *Handlers {
	return &Handlers{
		projectStore: projectStore,
		tabStore:     tabStore,
		metrics:      metrics,
	}
}
