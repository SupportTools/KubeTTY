-- Migration: Add 'paused' field to projects
-- This allows pausing a project by scaling the deployment to 0 replicas

ALTER TABLE kubetty_projects ADD COLUMN IF NOT EXISTS paused BOOLEAN NOT NULL DEFAULT false;
