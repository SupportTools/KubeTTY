package sessions

import (
	"context"
	"errors"
	"time"
)

// Store defines operations required to persist session metadata in CNPG.
type Store interface {
	GetSession(ctx context.Context, sessionID string) (*Session, error)
	ListSessions(ctx context.Context, deploymentID string) ([]Session, error)
	UpsertSession(ctx context.Context, s Session) error
	DeleteSession(ctx context.Context, sessionID string) error
	ClearAttachments(ctx context.Context, deploymentID string) error
	SetAttachment(ctx context.Context, sessionID, clientID string, attached bool) error
	AppendLog(ctx context.Context, entry LogEntry) error
	ListLogs(ctx context.Context, sessionID string, limit int) ([]LogEntry, error)
	PruneLogs(ctx context.Context, cutoff time.Time) (int64, error)
	TrimLogs(ctx context.Context, maxEntries int) (int64, error)
}

// Session represents metadata persisted in CNPG.
type Session struct {
	SessionID    string     `json:"sessionId"`
	DeploymentID string     `json:"deploymentId"`
	ShellPID     int        `json:"shellPid"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	ForkedFrom   *string    `json:"forkedFrom,omitempty"`
	AttachedTo   *string    `json:"attachedTo,omitempty"`
	AttachedAt   *time.Time `json:"attachedAt,omitempty"`
	State        []byte     `json:"state,omitempty"`
	LastLogAt    *time.Time `json:"lastLogAt,omitempty"`
	LogCount     int        `json:"logCount,omitempty"`
}

// LogEntry represents PTY transcript entries.
type LogEntry struct {
	SessionID string    `json:"sessionId"`
	Direction string    `json:"direction"`
	Data      []byte    `json:"data"`
	CreatedAt time.Time `json:"createdAt"`
}

// ErrNotFound is returned when a session does not exist.
var ErrNotFound = errors.New("session not found")
