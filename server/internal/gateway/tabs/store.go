package tabs

import (
	"context"
	"errors"
	"time"

	"github.com/supporttools/KubeTTY/server/internal/gateway/metrics"
)

// Status indicates the lifecycle state of a tab relay.
type Status string

const (
	StatusConnecting   Status = "connecting"
	StatusConnected    Status = "connected"
	StatusReconnecting Status = "reconnecting"
	StatusClosed       Status = "closed"
)

// Tab models a gateway-side view of an open browser tab.
type Tab struct {
	TabID         string              `json:"tabId"`
	ProjectID     string              `json:"projectId"`
	ClientID      string              `json:"clientId"`
	Status        Status              `json:"status"`
	Position      int                 `json:"position"`
	CreatedAt     time.Time           `json:"createdAt"`
	UpdatedAt     time.Time           `json:"updatedAt"`
	LastError     *string             `json:"lastError,omitempty"`
	DownstreamURI *string             `json:"downstreamUri,omitempty"`
	Metrics       *metrics.TabMetrics `json:"metrics,omitempty"`
}

// Store persists tab metadata for reconnects and auditing.
type Store interface {
	Create(ctx context.Context, tab Tab) error
	UpdateStatus(ctx context.Context, tabID string, status Status, lastError *string, downstreamURI *string) error
	UpdateClientID(ctx context.Context, tabID, clientID string) error
	UpdatePositions(ctx context.Context, clientID string, tabIDs []string) error
	Delete(ctx context.Context, tabID string) error
	Get(ctx context.Context, tabID string) (*Tab, error)
	ListByClient(ctx context.Context, clientID string, limit int) ([]Tab, error)
	ListAll(ctx context.Context) ([]Tab, error)
	CountByClientAndProject(ctx context.Context, clientID, projectID string) (int, error)
	GetNextPosition(ctx context.Context, clientID string) (int, error)

	// Dashboard methods
	GetStatusCounts(ctx context.Context) (map[string]int, error)
	GetRecentErrors(ctx context.Context, limit int) ([]Tab, error)
	GetActiveCountByProject(ctx context.Context) (map[string]int, error)

	// Cleanup methods
	// CleanOrphanedTabs deletes tabs that have been in 'closed' or 'reconnecting' status
	// for longer than maxAge. Returns the number of rows deleted.
	CleanOrphanedTabs(ctx context.Context, maxAge time.Duration) (int64, error)
}

// ErrNotFound is returned when a tab does not exist.
var ErrNotFound = errors.New("tab not found")
