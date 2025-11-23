-- Migration: Single-Namespace Schema Changes
-- Task #3850: Create database migration for single-namespace schema changes
--
-- This migration supports the single-namespace project controller model where
-- all projects deploy to a shared namespace (kubetty-projects-dev / kubetty-projects-prd)
-- instead of individual namespaces per project.

-- Add service_name column for explicit service naming
-- Service names follow pattern: kubetty-project-{name}
ALTER TABLE kubetty_projects
ADD COLUMN IF NOT EXISTS service_name VARCHAR(63);

-- Remove unique constraint on target_namespace since multiple projects
-- will now share the same namespace (e.g., kubetty-projects-dev)
ALTER TABLE kubetty_projects
DROP CONSTRAINT IF EXISTS kubetty_projects_target_namespace_key;

-- Backfill existing projects with computed service names BEFORE adding constraints
-- Uses the pattern: kubetty-project-{name}, truncated to 63 chars max (K8s limit)
-- Prefix "kubetty-project-" = 16 chars, leaving 47 chars for name portion
UPDATE kubetty_projects
SET service_name = SUBSTRING('kubetty-project-' || name FROM 1 FOR 63)
WHERE service_name IS NULL AND deleted_at IS NULL;

-- Add unique constraint on service_name for Kubernetes service uniqueness
-- This must be done AFTER backfill to ensure existing data complies
ALTER TABLE kubetty_projects
ADD CONSTRAINT kubetty_projects_service_name_key UNIQUE (service_name);

-- Add validation constraint to ensure service names follow Kubernetes DNS-1123 rules
ALTER TABLE kubetty_projects
ADD CONSTRAINT valid_service_name
CHECK (service_name ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$' AND length(service_name) <= 63);

-- Add index on service_name for efficient lookups (partial index for non-deleted)
CREATE INDEX IF NOT EXISTS idx_projects_service_name
ON kubetty_projects(service_name) WHERE deleted_at IS NULL;
