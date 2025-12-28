-- Rollback: Remove 'paused' field from projects

ALTER TABLE kubetty_projects DROP COLUMN IF EXISTS paused;
