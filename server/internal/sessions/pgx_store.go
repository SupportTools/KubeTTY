package sessions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGXStore persists session metadata to CNPG via pgxpool.
type PGXStore struct {
	pool *pgxpool.Pool
}

// NewPGXStore creates the connection pool using a secure, structured configuration.
// Schema migrations are run separately.
//
// The config parameter should be created using sharedconfig.BuildPostgresConfig() or
// the CommonConfig.ConnConfig() method, which provide injection-proof configuration.
//
// Example:
//
//	cfg, err := config.LoadGatewayConfig()
//	if err != nil {
//	    return err
//	}
//	poolConfig, err := cfg.ConnConfig()
//	if err != nil {
//	    return fmt.Errorf("build pool config: %w", err)
//	}
//	store, err := sessions.NewPGXStore(ctx, poolConfig)
func NewPGXStore(ctx context.Context, config *pgxpool.Config) (*PGXStore, error) {
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connect pgx: %w", err)
	}
	return &PGXStore{pool: pool}, nil
}

// Close releases the pool resources.
func (s *PGXStore) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// Ping checks database connectivity for health checks.
func (s *PGXStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// GetSession retrieves a session row.
func (s *PGXStore) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	const stmt = `
SELECT s.session_uuid, s.deployment_id, s.shell_pid, s.created_at, s.updated_at, s.forked_from, s.attached_to, s.attached_at, s.state,
       stats.last_log_at, COALESCE(stats.log_count,0)
FROM sessions s
LEFT JOIN (
  SELECT session_uuid, MAX(created_at) AS last_log_at, COUNT(*) AS log_count
  FROM session_logs
  GROUP BY session_uuid
) stats ON stats.session_uuid = s.session_uuid
WHERE s.session_uuid=$1`
	row := s.pool.QueryRow(ctx, stmt, sessionID)
	var sess Session
	if err := row.Scan(&sess.SessionID, &sess.DeploymentID, &sess.ShellPID, &sess.CreatedAt, &sess.UpdatedAt, &sess.ForkedFrom, &sess.AttachedTo, &sess.AttachedAt, &sess.State, &sess.LastLogAt, &sess.LogCount); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get session: %w", err)
	}
	return &sess, nil
}

// ListSessions lists sessions by deployment ordered by most recent update.
func (s *PGXStore) ListSessions(ctx context.Context, deploymentID string) ([]Session, error) {
	const stmt = `
SELECT s.session_uuid, s.deployment_id, s.shell_pid, s.created_at, s.updated_at, s.forked_from, s.attached_to, s.attached_at, s.state,
       stats.last_log_at, COALESCE(stats.log_count,0)
FROM sessions s
LEFT JOIN (
  SELECT session_uuid, MAX(created_at) AS last_log_at, COUNT(*) AS log_count
  FROM session_logs
  GROUP BY session_uuid
) stats ON stats.session_uuid = s.session_uuid
WHERE s.deployment_id=$1
ORDER BY s.updated_at DESC`
	rows, err := s.pool.Query(ctx, stmt, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	var result []Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.SessionID, &sess.DeploymentID, &sess.ShellPID, &sess.CreatedAt, &sess.UpdatedAt, &sess.ForkedFrom, &sess.AttachedTo, &sess.AttachedAt, &sess.State, &sess.LastLogAt, &sess.LogCount); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		result = append(result, sess)
	}
	return result, rows.Err()
}

// UpsertSession inserts or updates session metadata.
func (s *PGXStore) UpsertSession(ctx context.Context, sess Session) error {
	const stmt = `
INSERT INTO sessions (session_uuid, deployment_id, shell_pid, created_at, updated_at, forked_from, attached_to, attached_at, state)
VALUES ($1,$2,$3,COALESCE($4,NOW()),COALESCE($5,NOW()),$6,$7,$8,$9)
ON CONFLICT (session_uuid) DO UPDATE
SET shell_pid=EXCLUDED.shell_pid,
    updated_at=COALESCE(EXCLUDED.updated_at,NOW()),
    forked_from=EXCLUDED.forked_from,
    attached_to=EXCLUDED.attached_to,
    attached_at=EXCLUDED.attached_at,
    state=EXCLUDED.state;
`
	var createdAt, updatedAt *time.Time
	if !sess.CreatedAt.IsZero() {
		createdAt = &sess.CreatedAt
	}
	if !sess.UpdatedAt.IsZero() {
		updatedAt = &sess.UpdatedAt
	}
	if _, err := s.pool.Exec(ctx, stmt, sess.SessionID, sess.DeploymentID, sess.ShellPID, createdAt, updatedAt, sess.ForkedFrom, sess.AttachedTo, sess.AttachedAt, sess.State); err != nil {
		return fmt.Errorf("upsert session: %w", err)
	}
	return nil
}

// DeleteSession removes the session row and cascades logs.
func (s *PGXStore) DeleteSession(ctx context.Context, sessionID string) error {
	const stmt = `DELETE FROM sessions WHERE session_uuid=$1`
	if _, err := s.pool.Exec(ctx, stmt, sessionID); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// ClearAttachments detaches any sessions for the deployment, used at startup.
func (s *PGXStore) ClearAttachments(ctx context.Context, deploymentID string) error {
	const stmt = `
UPDATE sessions
SET attached_to=NULL,
    attached_at=NULL,
    updated_at=NOW()
WHERE deployment_id=$1 AND attached_to IS NOT NULL`
	if _, err := s.pool.Exec(ctx, stmt, deploymentID); err != nil {
		return fmt.Errorf("clear attachments: %w", err)
	}
	return nil
}

// SetAttachment marks whether a client is attached to the session.
func (s *PGXStore) SetAttachment(ctx context.Context, sessionID, clientID string, attached bool) error {
	if attached {
		const attachStmt = `UPDATE sessions SET attached_to=$2, attached_at=NOW(), updated_at=NOW() WHERE session_uuid=$1`
		if _, err := s.pool.Exec(ctx, attachStmt, sessionID, clientID); err != nil {
			return fmt.Errorf("set attachment attach: %w", err)
		}
		return nil
	}
	const detachStmt = `UPDATE sessions SET attached_to=NULL, attached_at=NULL, updated_at=NOW() WHERE session_uuid=$1 AND ($2='' OR attached_to=$2)`
	if _, err := s.pool.Exec(ctx, detachStmt, sessionID, clientID); err != nil {
		return fmt.Errorf("set attachment detach: %w", err)
	}
	return nil
}

// AppendLog stores a PTY transcript entry.
func (s *PGXStore) AppendLog(ctx context.Context, entry LogEntry) error {
	const stmt = `
INSERT INTO session_logs (session_uuid, direction, payload, created_at)
VALUES ($1,$2,$3,COALESCE($4,NOW()))`
	var createdAt interface{}
	if !entry.CreatedAt.IsZero() {
		createdAt = entry.CreatedAt
	}
	if _, err := s.pool.Exec(ctx, stmt, entry.SessionID, entry.Direction, entry.Data, createdAt); err != nil {
		return fmt.Errorf("append log: %w", err)
	}
	return nil
}

// ListLogs returns log entries for a session with optional filtering.
// The filter parameter can be nil for unfiltered results.
func (s *PGXStore) ListLogs(ctx context.Context, sessionID string, limit int, filter *LogFilter) (LogsResult, error) {
	if limit <= 0 {
		limit = 200
	}

	// Build dynamic query based on filter
	args := []interface{}{sessionID}
	argIdx := 2

	whereClause := "WHERE session_uuid=$1"

	// Add direction filter if specified
	if filter != nil && filter.Direction != "" {
		whereClause += fmt.Sprintf(" AND direction=$%d", argIdx)
		args = append(args, filter.Direction)
		argIdx++
	}

	// Add search filter if specified (case-insensitive search on decoded payload)
	if filter != nil && filter.Search != "" {
		// Use convert_from with error handling - skip non-UTF8 rows
		whereClause += fmt.Sprintf(" AND convert_from(payload, 'UTF8') ILIKE $%d", argIdx)
		args = append(args, "%"+filter.Search+"%")
		argIdx++
	}

	// First, get the total count matching the filter
	countStmt := fmt.Sprintf(`SELECT COUNT(*) FROM session_logs %s`, whereClause)
	var matchCount int
	if err := s.pool.QueryRow(ctx, countStmt, args...).Scan(&matchCount); err != nil {
		return LogsResult{}, fmt.Errorf("count logs: %w", err)
	}

	// Then fetch the actual logs with limit
	args = append(args, limit)
	stmt := fmt.Sprintf(`
SELECT session_uuid, direction, payload, created_at
FROM session_logs
%s
ORDER BY created_at ASC
LIMIT $%d`, whereClause, argIdx)

	rows, err := s.pool.Query(ctx, stmt, args...)
	if err != nil {
		return LogsResult{}, fmt.Errorf("list logs: %w", err)
	}
	defer rows.Close()

	var logs []LogEntry
	for rows.Next() {
		var entry LogEntry
		if err := rows.Scan(&entry.SessionID, &entry.Direction, &entry.Data, &entry.CreatedAt); err != nil {
			return LogsResult{}, fmt.Errorf("scan log: %w", err)
		}
		logs = append(logs, entry)
	}
	if err := rows.Err(); err != nil {
		return LogsResult{}, err
	}

	return LogsResult{
		Logs:       logs,
		MatchCount: matchCount,
	}, nil
}

// PruneLogs deletes entries older than the cutoff.
func (s *PGXStore) PruneLogs(ctx context.Context, cutoff time.Time) (int64, error) {
	if cutoff.IsZero() {
		return 0, nil
	}
	const stmt = `DELETE FROM session_logs WHERE created_at < $1`
	tag, err := s.pool.Exec(ctx, stmt, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune logs: %w", err)
	}
	return tag.RowsAffected(), nil
}

// TrimLogs removes entries exceeding maxEntries per session, keeping the newest rows.
func (s *PGXStore) TrimLogs(ctx context.Context, maxEntries int) (int64, error) {
	if maxEntries <= 0 {
		return 0, nil
	}
	const stmt = `
WITH ranked AS (
  SELECT id,
         ROW_NUMBER() OVER (PARTITION BY session_uuid ORDER BY created_at DESC, id DESC) AS rn
  FROM session_logs
),
to_delete AS (
  SELECT id FROM ranked WHERE rn > $1
)
DELETE FROM session_logs
WHERE id IN (SELECT id FROM to_delete)
`
	tag, err := s.pool.Exec(ctx, stmt, maxEntries)
	if err != nil {
		return 0, fmt.Errorf("trim logs: %w", err)
	}
	return tag.RowsAffected(), nil
}
