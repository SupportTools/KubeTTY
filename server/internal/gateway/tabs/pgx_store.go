package tabs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGXStore implements Store backed by CNPG via pgxpool.
type PGXStore struct {
	pool *pgxpool.Pool
}

// NewPGXStore creates a new Store using the provided pool.
func NewPGXStore(pool *pgxpool.Pool) *PGXStore {
	return &PGXStore{pool: pool}
}

// Create inserts a new tab row.
func (s *PGXStore) Create(ctx context.Context, tab Tab) error {
	const stmt = `
INSERT INTO gateway_tabs (tab_id, project_id, client_id, status, position, created_at, updated_at, last_error, downstream_uri)
VALUES ($1,$2,$3,$4,$5,COALESCE($6,NOW()),COALESCE($7,NOW()),$8,$9)
ON CONFLICT (tab_id) DO UPDATE SET
  project_id=EXCLUDED.project_id,
  client_id=EXCLUDED.client_id,
  status=EXCLUDED.status,
  position=EXCLUDED.position,
  created_at=COALESCE(EXCLUDED.created_at,gateway_tabs.created_at),
  updated_at=COALESCE(EXCLUDED.updated_at,NOW()),
  last_error=EXCLUDED.last_error,
  downstream_uri=EXCLUDED.downstream_uri
`
	var createdAt, updatedAt interface{}
	if !tab.CreatedAt.IsZero() {
		createdAt = tab.CreatedAt
	}
	if !tab.UpdatedAt.IsZero() {
		updatedAt = tab.UpdatedAt
	}
	if _, err := s.pool.Exec(ctx, stmt, tab.TabID, tab.ProjectID, tab.ClientID, tab.Status, tab.Position, createdAt, updatedAt, tab.LastError, tab.DownstreamURI); err != nil {
		return fmt.Errorf("create tab: %w", err)
	}
	return nil
}

// UpdateStatus updates status/metadata for an existing tab.
func (s *PGXStore) UpdateStatus(ctx context.Context, tabID string, status Status, lastError *string, downstreamURI *string) error {
	const stmt = `
UPDATE gateway_tabs
SET status=$2,
    updated_at=NOW(),
    last_error=$3,
    downstream_uri=$4
WHERE tab_id=$1`
	cmd, err := s.pool.Exec(ctx, stmt, tabID, status, lastError, downstreamURI)
	if err != nil {
		return fmt.Errorf("update tab status: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateClientID updates the client ID for a tab (used for force takeover).
func (s *PGXStore) UpdateClientID(ctx context.Context, tabID, clientID string) error {
	const stmt = `
UPDATE gateway_tabs
SET client_id=$2,
    updated_at=NOW()
WHERE tab_id=$1`
	cmd, err := s.pool.Exec(ctx, stmt, tabID, clientID)
	if err != nil {
		return fmt.Errorf("update tab client_id: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a tab row permanently.
func (s *PGXStore) Delete(ctx context.Context, tabID string) error {
	const stmt = `DELETE FROM gateway_tabs WHERE tab_id=$1`
	if _, err := s.pool.Exec(ctx, stmt, tabID); err != nil {
		return fmt.Errorf("delete tab: %w", err)
	}
	return nil
}

// Get retrieves a tab by ID.
func (s *PGXStore) Get(ctx context.Context, tabID string) (*Tab, error) {
	const stmt = `
SELECT tab_id, project_id, client_id, status, position, created_at, updated_at, last_error, downstream_uri
FROM gateway_tabs
WHERE tab_id=$1`
	row := s.pool.QueryRow(ctx, stmt, tabID)
	var tab Tab
	if err := row.Scan(&tab.TabID, &tab.ProjectID, &tab.ClientID, &tab.Status, &tab.Position, &tab.CreatedAt, &tab.UpdatedAt, &tab.LastError, &tab.DownstreamURI); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get tab: %w", err)
	}
	return &tab, nil
}

// ListByClient returns tabs for a client ordered by position.
func (s *PGXStore) ListByClient(ctx context.Context, clientID string, limit int) ([]Tab, error) {
	if limit <= 0 {
		limit = 50
	}
	const stmt = `
SELECT tab_id, project_id, client_id, status, position, created_at, updated_at, last_error, downstream_uri
FROM gateway_tabs
WHERE client_id=$1
ORDER BY position ASC, created_at ASC
LIMIT $2`
	rows, err := s.pool.Query(ctx, stmt, clientID, limit)
	if err != nil {
		return nil, fmt.Errorf("list tabs: %w", err)
	}
	defer rows.Close()
	var result []Tab
	for rows.Next() {
		var tab Tab
		if err := rows.Scan(&tab.TabID, &tab.ProjectID, &tab.ClientID, &tab.Status, &tab.Position, &tab.CreatedAt, &tab.UpdatedAt, &tab.LastError, &tab.DownstreamURI); err != nil {
			return nil, fmt.Errorf("scan tab: %w", err)
		}
		result = append(result, tab)
	}
	return result, rows.Err()
}

// ListAll returns all tabs for restoration purposes.
func (s *PGXStore) ListAll(ctx context.Context) ([]Tab, error) {
	const stmt = `
SELECT tab_id, project_id, client_id, status, position, created_at, updated_at, last_error, downstream_uri
FROM gateway_tabs`
	rows, err := s.pool.Query(ctx, stmt)
	if err != nil {
		return nil, fmt.Errorf("list all tabs: %w", err)
	}
	defer rows.Close()
	var result []Tab
	for rows.Next() {
		var tab Tab
		if err := rows.Scan(&tab.TabID, &tab.ProjectID, &tab.ClientID, &tab.Status, &tab.Position, &tab.CreatedAt, &tab.UpdatedAt, &tab.LastError, &tab.DownstreamURI); err != nil {
			return nil, fmt.Errorf("scan tab: %w", err)
		}
		result = append(result, tab)
	}
	return result, rows.Err()
}

// CountByClientAndProject returns the number of active tabs for a specific client and project.
// Note: Closed tabs are deleted from the database (via CloseTab/closeIdleTab calling store.Delete),
// so this naturally counts only active tabs without needing status filtering.
func (s *PGXStore) CountByClientAndProject(ctx context.Context, clientID, projectID string) (int, error) {
	const stmt = `
SELECT COUNT(*)
FROM gateway_tabs
WHERE client_id = $1 AND project_id = $2`
	var count int
	if err := s.pool.QueryRow(ctx, stmt, clientID, projectID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count tabs: %w", err)
	}
	return count, nil
}

// GetStatusCounts returns a count of tabs grouped by status.
func (s *PGXStore) GetStatusCounts(ctx context.Context) (map[string]int, error) {
	const stmt = `
SELECT status, COUNT(*) as count
FROM gateway_tabs
GROUP BY status`

	rows, err := s.pool.Query(ctx, stmt)
	if err != nil {
		return nil, fmt.Errorf("get status counts: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("scan status count: %w", err)
		}
		counts[status] = count
	}

	return counts, rows.Err()
}

// GetRecentErrors returns tabs with errors, ordered by most recent.
func (s *PGXStore) GetRecentErrors(ctx context.Context, limit int) ([]Tab, error) {
	if limit <= 0 {
		limit = 50
	}

	const stmt = `
SELECT tab_id, project_id, client_id, status, position, created_at, updated_at, last_error, downstream_uri
FROM gateway_tabs
WHERE last_error IS NOT NULL AND last_error != ''
ORDER BY updated_at DESC
LIMIT $1`

	rows, err := s.pool.Query(ctx, stmt, limit)
	if err != nil {
		return nil, fmt.Errorf("get recent errors: %w", err)
	}
	defer rows.Close()

	var result []Tab
	for rows.Next() {
		var tab Tab
		if err := rows.Scan(&tab.TabID, &tab.ProjectID, &tab.ClientID, &tab.Status, &tab.Position, &tab.CreatedAt, &tab.UpdatedAt, &tab.LastError, &tab.DownstreamURI); err != nil {
			return nil, fmt.Errorf("scan tab: %w", err)
		}
		result = append(result, tab)
	}
	return result, rows.Err()
}

// GetActiveCountByProject returns the count of active tabs per project.
func (s *PGXStore) GetActiveCountByProject(ctx context.Context) (map[string]int, error) {
	const stmt = `
SELECT project_id, COUNT(*) as count
FROM gateway_tabs
WHERE status IN ('connecting', 'connected', 'reconnecting')
GROUP BY project_id`

	rows, err := s.pool.Query(ctx, stmt)
	if err != nil {
		return nil, fmt.Errorf("get active count by project: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var projectID string
		var count int
		if err := rows.Scan(&projectID, &count); err != nil {
			return nil, fmt.Errorf("scan project count: %w", err)
		}
		counts[projectID] = count
	}

	return counts, rows.Err()
}

// UpdatePositions updates the position of multiple tabs in the given order.
// tabIDs should be in the desired order (first element = position 0).
func (s *PGXStore) UpdatePositions(ctx context.Context, clientID string, tabIDs []string) error {
	if len(tabIDs) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	const stmt = `UPDATE gateway_tabs SET position=$1, updated_at=NOW() WHERE tab_id=$2 AND client_id=$3`
	for i, tabID := range tabIDs {
		cmd, err := tx.Exec(ctx, stmt, i, tabID, clientID)
		if err != nil {
			return fmt.Errorf("update position for tab %s: %w", tabID, err)
		}
		if cmd.RowsAffected() == 0 {
			return fmt.Errorf("tab %s not found or does not belong to client", tabID)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// GetNextPosition returns the next available position for a new tab.
func (s *PGXStore) GetNextPosition(ctx context.Context, clientID string) (int, error) {
	const stmt = `SELECT COALESCE(MAX(position), -1) + 1 FROM gateway_tabs WHERE client_id=$1`
	var pos int
	if err := s.pool.QueryRow(ctx, stmt, clientID).Scan(&pos); err != nil {
		return 0, fmt.Errorf("get next position: %w", err)
	}
	return pos, nil
}

// CleanOrphanedTabs deletes tabs that have been in 'closed' or 'reconnecting' status
// for longer than maxAge. This helps clean up database rows that weren't properly
// removed when their in-memory counterparts were closed (e.g., during gateway restart).
// Returns the number of rows deleted.
func (s *PGXStore) CleanOrphanedTabs(ctx context.Context, maxAge time.Duration) (int64, error) {
	const stmt = `
DELETE FROM gateway_tabs
WHERE updated_at < $1
  AND status IN ('closed', 'reconnecting')`

	cutoff := time.Now().Add(-maxAge)
	result, err := s.pool.Exec(ctx, stmt, cutoff)
	if err != nil {
		return 0, fmt.Errorf("clean orphaned tabs: %w", err)
	}
	return result.RowsAffected(), nil
}
