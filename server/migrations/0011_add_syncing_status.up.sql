-- Migration: Add 'syncing' status to project lifecycle
-- This status is used during the template PVC sync Job phase before deployment creation

-- Drop and recreate the valid_status constraint to include 'syncing'
ALTER TABLE kubetty_projects DROP CONSTRAINT IF EXISTS valid_status;
ALTER TABLE kubetty_projects ADD CONSTRAINT valid_status
    CHECK (status IN ('pending', 'syncing', 'creating', 'running', 'updating', 'failed', 'deleting', 'deleted'));
