-- Projects table for KubeTTY project lifecycle management
CREATE TABLE IF NOT EXISTS kubetty_projects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Identity
    name VARCHAR(63) NOT NULL UNIQUE,  -- K8s naming constraints (DNS-1123 subdomain)
    display_name VARCHAR(255) NOT NULL,
    description TEXT,
    icon VARCHAR(64),  -- Icon identifier for UI

    -- Target configuration
    target_namespace VARCHAR(63) NOT NULL UNIQUE,  -- Generated: kubetty-{name}
    session_id UUID NOT NULL UNIQUE,  -- Unique session identifier
    user_name VARCHAR(64) NOT NULL,

    -- Resource configuration
    cpu_request VARCHAR(16) NOT NULL DEFAULT '500m',
    cpu_limit VARCHAR(16) NOT NULL DEFAULT '4000m',
    memory_request VARCHAR(16) NOT NULL DEFAULT '2Gi',
    memory_limit VARCHAR(16) NOT NULL DEFAULT '8Gi',
    storage_size VARCHAR(16) NOT NULL DEFAULT '50Gi',
    storage_class VARCHAR(63) NOT NULL DEFAULT 'longhorn',

    -- RBAC configuration (JSON arrays of namespace names)
    admin_namespaces JSONB NOT NULL DEFAULT '[]',
    read_namespaces JSONB NOT NULL DEFAULT '[]',

    -- Tab limits
    max_tabs_per_client INT NOT NULL DEFAULT 3,
    max_tabs_total INT NOT NULL DEFAULT 10,

    -- Feature flags
    dind_enabled BOOLEAN NOT NULL DEFAULT true,

    -- Environment variables (JSON object)
    env_vars JSONB NOT NULL DEFAULT '{}',

    -- Image configuration
    image_repository VARCHAR(255) NOT NULL DEFAULT 'harbor.support.tools/kubetty/kubetty',
    image_tag VARCHAR(128) NOT NULL DEFAULT 'latest',

    -- Status tracking
    status VARCHAR(32) NOT NULL DEFAULT 'pending',  -- pending, creating, running, updating, failed, deleting, deleted
    status_message TEXT,
    last_health_check TIMESTAMPTZ,
    pod_ip VARCHAR(45),  -- IPv4 or IPv6

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,  -- Soft delete

    -- Constraints
    CONSTRAINT valid_status CHECK (status IN ('pending', 'creating', 'running', 'updating', 'failed', 'deleting', 'deleted')),
    CONSTRAINT valid_name CHECK (name ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$')
);

-- Indexes for common queries
CREATE INDEX IF NOT EXISTS idx_projects_status ON kubetty_projects(status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_projects_name ON kubetty_projects(name) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_projects_user ON kubetty_projects(user_name) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_projects_namespace ON kubetty_projects(target_namespace) WHERE deleted_at IS NULL;

-- Trigger to update updated_at on modification
CREATE OR REPLACE FUNCTION update_kubetty_projects_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER kubetty_projects_updated_at
    BEFORE UPDATE ON kubetty_projects
    FOR EACH ROW
    EXECUTE FUNCTION update_kubetty_projects_updated_at();
