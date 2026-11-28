-- cluster-services-1024: record drift between a project's DB row and its
-- running Deployment. The controller's periodic drift check writes these; it
-- never rolls a pod. NULL pending_changes means no drift (or not checked yet).
ALTER TABLE kubetty_projects
ADD COLUMN IF NOT EXISTS pending_changes JSONB;

ALTER TABLE kubetty_projects
ADD COLUMN IF NOT EXISTS drift_checked_at TIMESTAMPTZ;
