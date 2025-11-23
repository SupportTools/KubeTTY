-- Rollback projects table migration
DROP TRIGGER IF EXISTS kubetty_projects_updated_at ON kubetty_projects;
DROP FUNCTION IF EXISTS update_kubetty_projects_updated_at();
DROP TABLE IF EXISTS kubetty_projects;
