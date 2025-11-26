-- KubeTTY Test Database Initialization
-- Combined migrations for testing purposes
-- This file is auto-maintained based on server/migrations/*.up.sql

-- =============================================================================
-- Migration 0001: Initial Schema
-- =============================================================================
CREATE TABLE IF NOT EXISTS sessions (
  session_uuid UUID PRIMARY KEY,
  deployment_id TEXT NOT NULL,
  shell_pid INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  forked_from UUID NULL,
  attached_to TEXT NULL,
  state BYTEA NULL
);

CREATE TABLE IF NOT EXISTS session_logs (
  id BIGSERIAL PRIMARY KEY,
  session_uuid UUID NOT NULL REFERENCES sessions(session_uuid) ON DELETE CASCADE,
  direction TEXT NOT NULL,
  payload BYTEA NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS session_logs_session_created_idx ON session_logs(session_uuid, created_at);

-- =============================================================================
-- Migration 0002: Add attached_at column
-- =============================================================================
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS attached_at TIMESTAMPTZ NULL;

-- =============================================================================
-- Migration 0003: Gateway Tabs
-- =============================================================================
CREATE TABLE IF NOT EXISTS gateway_tabs (
  tab_id UUID PRIMARY KEY,
  project_id TEXT NOT NULL,
  client_id TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_error TEXT NULL,
  downstream_uri TEXT NULL
);

CREATE INDEX IF NOT EXISTS gateway_tabs_project_idx ON gateway_tabs(project_id);
CREATE INDEX IF NOT EXISTS gateway_tabs_client_idx ON gateway_tabs(client_id);

-- =============================================================================
-- Migration 0004: Auth Tables
-- =============================================================================
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE IF NOT EXISTS kubetty_users (
  id UUID PRIMARY KEY,
  username CITEXT NOT NULL UNIQUE,
  password_hash BYTEA NOT NULL,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_login_at TIMESTAMPTZ NULL
);

CREATE TABLE IF NOT EXISTS kubetty_refresh_tokens (
  id UUID PRIMARY KEY,
  token_id UUID NOT NULL UNIQUE,
  user_id UUID NOT NULL REFERENCES kubetty_users(id) ON DELETE CASCADE,
  token_hash BYTEA NOT NULL,
  issued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ NULL,
  created_by TEXT NULL,
  user_agent TEXT NULL,
  client_ip TEXT NULL
);

CREATE INDEX IF NOT EXISTS kubetty_refresh_tokens_user_idx ON kubetty_refresh_tokens(user_id);
CREATE INDEX IF NOT EXISTS kubetty_refresh_tokens_expiry_idx ON kubetty_refresh_tokens(expires_at);
CREATE INDEX IF NOT EXISTS kubetty_refresh_tokens_revoked_idx ON kubetty_refresh_tokens(revoked_at);

-- =============================================================================
-- Migration 0005: Gateway Tabs Composite Index
-- =============================================================================
CREATE INDEX IF NOT EXISTS gateway_tabs_client_project_idx ON gateway_tabs(client_id, project_id);

-- =============================================================================
-- Migration 0006: Session Logs Search Index
-- =============================================================================
CREATE INDEX IF NOT EXISTS session_logs_direction_idx ON session_logs(session_uuid, direction);

-- =============================================================================
-- Migration 0007: Session Logs Created Index
-- Note: CONCURRENTLY removed for init script compatibility
-- =============================================================================
CREATE INDEX IF NOT EXISTS session_logs_created_idx ON session_logs(created_at);

-- =============================================================================
-- Migration 0008: Projects Table
-- =============================================================================
CREATE TABLE IF NOT EXISTS kubetty_projects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Identity
    name VARCHAR(63) NOT NULL UNIQUE,
    display_name VARCHAR(255) NOT NULL,
    description TEXT,
    icon VARCHAR(64),

    -- Target configuration
    target_namespace VARCHAR(63) NOT NULL,
    session_id UUID NOT NULL UNIQUE,
    user_name VARCHAR(64) NOT NULL,

    -- Resource configuration
    cpu_request VARCHAR(16) NOT NULL DEFAULT '500m',
    cpu_limit VARCHAR(16) NOT NULL DEFAULT '4000m',
    memory_request VARCHAR(16) NOT NULL DEFAULT '2Gi',
    memory_limit VARCHAR(16) NOT NULL DEFAULT '8Gi',
    storage_size VARCHAR(16) NOT NULL DEFAULT '50Gi',
    storage_class VARCHAR(63) NOT NULL DEFAULT 'longhorn',

    -- RBAC configuration
    admin_namespaces JSONB NOT NULL DEFAULT '[]',
    read_namespaces JSONB NOT NULL DEFAULT '[]',

    -- Tab limits
    max_tabs_per_client INT NOT NULL DEFAULT 3,
    max_tabs_total INT NOT NULL DEFAULT 10,

    -- Feature flags
    dind_enabled BOOLEAN NOT NULL DEFAULT true,

    -- Environment variables
    env_vars JSONB NOT NULL DEFAULT '{}',

    -- Image configuration
    image_repository VARCHAR(255) NOT NULL DEFAULT 'harbor.support.tools/kubetty/kubetty',
    image_tag VARCHAR(128) NOT NULL DEFAULT 'latest',

    -- Status tracking
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    status_message TEXT,
    last_health_check TIMESTAMPTZ,
    pod_ip VARCHAR(45),

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,

    -- Constraints
    CONSTRAINT valid_status CHECK (status IN ('pending', 'creating', 'running', 'updating', 'failed', 'deleting', 'deleted')),
    CONSTRAINT valid_name CHECK (name ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$')
);

CREATE INDEX IF NOT EXISTS idx_projects_status ON kubetty_projects(status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_projects_name ON kubetty_projects(name) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_projects_user ON kubetty_projects(user_name) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_projects_namespace ON kubetty_projects(target_namespace) WHERE deleted_at IS NULL;

CREATE OR REPLACE FUNCTION update_kubetty_projects_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS kubetty_projects_updated_at ON kubetty_projects;
CREATE TRIGGER kubetty_projects_updated_at
    BEFORE UPDATE ON kubetty_projects
    FOR EACH ROW
    EXECUTE FUNCTION update_kubetty_projects_updated_at();

-- =============================================================================
-- Migration 0009a: Single Namespace Schema Changes
-- =============================================================================
ALTER TABLE kubetty_projects
ADD COLUMN IF NOT EXISTS service_name VARCHAR(63);

-- Remove unique constraint on target_namespace (multiple projects can share namespace)
ALTER TABLE kubetty_projects
DROP CONSTRAINT IF EXISTS kubetty_projects_target_namespace_key;

-- Backfill existing projects with computed service names
UPDATE kubetty_projects
SET service_name = SUBSTRING('kubetty-project-' || name FROM 1 FOR 63)
WHERE service_name IS NULL AND deleted_at IS NULL;

-- Add unique constraint on service_name
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'kubetty_projects_service_name_key'
    ) THEN
        ALTER TABLE kubetty_projects
        ADD CONSTRAINT kubetty_projects_service_name_key UNIQUE (service_name);
    END IF;
END $$;

-- Add validation constraint for service names
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'valid_service_name'
    ) THEN
        ALTER TABLE kubetty_projects
        ADD CONSTRAINT valid_service_name
        CHECK (service_name ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$' AND length(service_name) <= 63);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_projects_service_name
ON kubetty_projects(service_name) WHERE deleted_at IS NULL;

-- =============================================================================
-- Migration 0009b: Project Activity Tracking
-- =============================================================================
ALTER TABLE kubetty_projects
ADD COLUMN IF NOT EXISTS last_activity TIMESTAMP DEFAULT CURRENT_TIMESTAMP;

UPDATE kubetty_projects
SET last_activity = CURRENT_TIMESTAMP
WHERE last_activity IS NULL;
