ALTER TABLE kubetty_projects
DROP CONSTRAINT IF EXISTS valid_session_mode;

ALTER TABLE kubetty_projects
DROP COLUMN IF EXISTS session_mode;
