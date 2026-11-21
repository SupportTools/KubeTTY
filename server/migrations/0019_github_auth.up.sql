ALTER TABLE kubetty_users ADD COLUMN IF NOT EXISTS github_id BIGINT;
ALTER TABLE kubetty_users ADD COLUMN IF NOT EXISTS github_login TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS kubetty_users_github_id_key ON kubetty_users(github_id) WHERE github_id IS NOT NULL;

ALTER TABLE kubetty_projects ADD COLUMN IF NOT EXISTS github_repo VARCHAR(140);
