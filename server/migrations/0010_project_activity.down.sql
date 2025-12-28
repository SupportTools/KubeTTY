-- Rollback last_activity column
ALTER TABLE kubetty_projects
DROP COLUMN IF EXISTS last_activity;
