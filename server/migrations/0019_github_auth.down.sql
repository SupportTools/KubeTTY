ALTER TABLE kubetty_projects DROP COLUMN IF EXISTS github_repo;

DROP INDEX IF EXISTS kubetty_users_github_id_key;
ALTER TABLE kubetty_users DROP COLUMN IF EXISTS github_login;
ALTER TABLE kubetty_users DROP COLUMN IF EXISTS github_id;
