-- Rollback: Remove 'syncing' status from valid_status constraint
-- WARNING: This will fail if any projects are in 'syncing' status

ALTER TABLE kubetty_projects DROP CONSTRAINT IF EXISTS valid_status;
ALTER TABLE kubetty_projects ADD CONSTRAINT valid_status
    CHECK (status IN ('pending', 'creating', 'running', 'updating', 'failed', 'deleting', 'deleted'));
