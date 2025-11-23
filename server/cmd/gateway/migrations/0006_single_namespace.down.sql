-- Rollback: Single-Namespace Schema Changes
-- Task #3850: Create database migration for single-namespace schema changes
--
-- ⚠️  IMPORTANT: This migration becomes IRREVERSIBLE once projects share namespaces!
--
-- After adopting single-namespace mode, multiple projects will have identical
-- target_namespace values (e.g., "kubetty-projects-dev"). The final statement
-- restoring the unique constraint will FAIL in that case.
--
-- If rollback is needed after shared namespace adoption:
-- 1. Manually update target_namespace to unique values for each project
-- 2. OR skip the unique constraint restoration entirely
-- 3. Contact operations team for guidance

-- Drop the service_name index
DROP INDEX IF EXISTS idx_projects_service_name;

-- Drop the service_name validation constraint
ALTER TABLE kubetty_projects
DROP CONSTRAINT IF EXISTS valid_service_name;

-- Drop the service_name unique constraint
ALTER TABLE kubetty_projects
DROP CONSTRAINT IF EXISTS kubetty_projects_service_name_key;

-- Remove service_name column
ALTER TABLE kubetty_projects
DROP COLUMN IF EXISTS service_name;

-- Restore unique constraint on target_namespace
-- NOTE: This will fail if duplicate target_namespace values exist
ALTER TABLE kubetty_projects
ADD CONSTRAINT kubetty_projects_target_namespace_key UNIQUE (target_namespace);
