package tabs

import (
	"context"
	"errors"
	"fmt"

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
INSERT INTO gateway_tabs (tab_id, project_id, client_id, status, created_at, updated_at, last_error, downstream_uri)
VALUES ($1,$2,$3,$4,COALESCE($5,NOW()),COALESCE($6,NOW()),$7,$8)
ON CONFLICT (tab_id) DO UPDATE SET
  project_id=EXCLUDED.project_id,
  client_id=EXCLUDED.client_id,
  status=EXCLUDED.status,
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
	if _, err := s.pool.Exec(ctx, stmt, tab.TabID, tab.ProjectID, tab.ClientID, tab.Status, createdAt, updatedAt, tab.LastError, tab.DownstreamURI); err != nil {
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
SELECT tab_id, project_id, client_id, status, created_at, updated_at, last_error, downstream_uri
FROM gateway_tabs
WHERE tab_id=$1`
	row := s.pool.QueryRow(ctx, stmt, tabID)
	var tab Tab
	if err := row.Scan(&tab.TabID, &tab.ProjectID, &tab.ClientID, &tab.Status, &tab.CreatedAt, &tab.UpdatedAt, &tab.LastError, &tab.DownstreamURI); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get tab: %w", err)
	}
	return &tab, nil
}

// ListByClient returns most recent tabs for a client.
func (s *PGXStore) ListByClient(ctx context.Context, clientID string, limit int) ([]Tab, error) {
	if limit <= 0 {
		limit = 50
	}
	const stmt = `
SELECT tab_id, project_id, client_id, status, created_at, updated_at, last_error, downstream_uri
FROM gateway_tabs
WHERE client_id=$1
ORDER BY updated_at DESC
LIMIT $2`
	rows, err := s.pool.Query(ctx, stmt, clientID, limit)
	if err != nil {
		return nil, fmt.Errorf("list tabs: %w", err)
	}
	defer rows.Close()
	var result []Tab
	for rows.Next() {
		var tab Tab
		if err := rows.Scan(&tab.TabID, &tab.ProjectID, &tab.ClientID, &tab.Status, &tab.CreatedAt, &tab.UpdatedAt, &tab.LastError, &tab.DownstreamURI); err != nil {
			return nil, fmt.Errorf("scan tab: %w", err)
		}
		result = append(result, tab)
	}
	return result, rows.Err()
}

// ListAll returns all tabs for restoration purposes.
func (s *PGXStore) ListAll(ctx context.Context) ([]Tab, error) {
	const stmt = `
SELECT tab_id, project_id, client_id, status, created_at, updated_at, last_error, downstream_uri
FROM gateway_tabs`
	rows, err := s.pool.Query(ctx, stmt)
	if err != nil {
		return nil, fmt.Errorf("list all tabs: %w", err)
	}
	defer rows.Close()
	var result []Tab
	for rows.Next() {
		var tab Tab
		if err := rows.Scan(&tab.TabID, &tab.ProjectID, &tab.ClientID, &tab.Status, &tab.CreatedAt, &tab.UpdatedAt, &tab.LastError, &tab.DownstreamURI); err != nil {
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
