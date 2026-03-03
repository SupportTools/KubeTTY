-- Global settings table for KubeTTY system-wide configuration
CREATE TABLE IF NOT EXISTS kubetty_settings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Setting identification
    category VARCHAR(64) NOT NULL,
    key VARCHAR(128) NOT NULL,

    -- Value storage with type metadata
    value JSONB NOT NULL,
    value_type VARCHAR(16) NOT NULL DEFAULT 'string',

    -- Metadata
    display_name VARCHAR(255) NOT NULL,
    description TEXT,
    is_sensitive BOOLEAN NOT NULL DEFAULT false,
    is_readonly BOOLEAN NOT NULL DEFAULT false,

    -- Validation (optional JSON schema or simple constraints)
    validation JSONB,

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Constraints
    CONSTRAINT unique_category_key UNIQUE (category, key),
    CONSTRAINT valid_category CHECK (category IN ('project_defaults', 'auth', 'features', 'ui', 'controller', 'notifications', 'secrets')),
    CONSTRAINT valid_value_type CHECK (value_type IN ('string', 'int', 'bool', 'json'))
);

-- Indexes for common queries
CREATE INDEX IF NOT EXISTS idx_settings_category ON kubetty_settings(category);
CREATE INDEX IF NOT EXISTS idx_settings_key ON kubetty_settings(key);

-- Settings history table for audit trail
CREATE TABLE IF NOT EXISTS kubetty_settings_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Reference to setting (nullable for deleted settings)
    setting_id UUID REFERENCES kubetty_settings(id) ON DELETE SET NULL,
    category VARCHAR(64) NOT NULL,
    key VARCHAR(128) NOT NULL,

    -- Change details
    old_value JSONB,
    new_value JSONB,
    change_type VARCHAR(16) NOT NULL,

    -- Who/when/how
    changed_by VARCHAR(64),
    changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    change_source VARCHAR(32) NOT NULL DEFAULT 'api',
    change_reason TEXT,

    -- Client context
    client_ip VARCHAR(45),
    user_agent TEXT,

    -- Constraints
    CONSTRAINT valid_change_type CHECK (change_type IN ('insert', 'update', 'delete'))
);

-- Indexes for querying history
CREATE INDEX IF NOT EXISTS idx_settings_history_setting ON kubetty_settings_history(setting_id);
CREATE INDEX IF NOT EXISTS idx_settings_history_category_key ON kubetty_settings_history(category, key);
CREATE INDEX IF NOT EXISTS idx_settings_history_changed_at ON kubetty_settings_history(changed_at DESC);

-- Trigger function to automatically record history
CREATE OR REPLACE FUNCTION record_settings_history()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        INSERT INTO kubetty_settings_history (
            setting_id, category, key, old_value, new_value,
            change_type, changed_by, change_source
        ) VALUES (
            NEW.id, NEW.category, NEW.key, NULL, NEW.value,
            'insert', COALESCE(current_setting('kubetty.changed_by', true), 'system'),
            COALESCE(current_setting('kubetty.change_source', true), 'system')
        );
        RETURN NEW;
    ELSIF TG_OP = 'UPDATE' THEN
        -- Only record if value actually changed
        IF OLD.value IS DISTINCT FROM NEW.value THEN
            INSERT INTO kubetty_settings_history (
                setting_id, category, key, old_value, new_value,
                change_type, changed_by, change_source
            ) VALUES (
                NEW.id, NEW.category, NEW.key, OLD.value, NEW.value,
                'update', COALESCE(current_setting('kubetty.changed_by', true), 'system'),
                COALESCE(current_setting('kubetty.change_source', true), 'api')
            );
        END IF;
        -- Update timestamp
        NEW.updated_at = NOW();
        RETURN NEW;
    ELSIF TG_OP = 'DELETE' THEN
        INSERT INTO kubetty_settings_history (
            setting_id, category, key, old_value, new_value,
            change_type, changed_by, change_source
        ) VALUES (
            OLD.id, OLD.category, OLD.key, OLD.value, NULL,
            'delete', COALESCE(current_setting('kubetty.changed_by', true), 'system'),
            COALESCE(current_setting('kubetty.change_source', true), 'api')
        );
        RETURN OLD;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

-- Create trigger on settings table
CREATE TRIGGER settings_history_trigger
    AFTER INSERT OR UPDATE OR DELETE ON kubetty_settings
    FOR EACH ROW EXECUTE FUNCTION record_settings_history();

-- Seed default settings
INSERT INTO kubetty_settings (category, key, value, value_type, display_name, description) VALUES
    -- Project Defaults
    ('project_defaults', 'cpu_request', '"500m"', 'string', 'Default CPU Request', 'Default CPU request for new projects'),
    ('project_defaults', 'cpu_limit', '"4000m"', 'string', 'Default CPU Limit', 'Default CPU limit for new projects'),
    ('project_defaults', 'memory_request', '"2Gi"', 'string', 'Default Memory Request', 'Default memory request for new projects'),
    ('project_defaults', 'memory_limit', '"8Gi"', 'string', 'Default Memory Limit', 'Default memory limit for new projects'),
    ('project_defaults', 'storage_size', '"50Gi"', 'string', 'Default Storage Size', 'Default PVC storage size for new projects'),
    ('project_defaults', 'storage_class', '"longhorn"', 'string', 'Default Storage Class', 'Default Kubernetes storage class for new projects'),
    ('project_defaults', 'max_tabs_per_client', '3', 'int', 'Max Tabs Per Client', 'Maximum terminal tabs per client connection'),
    ('project_defaults', 'max_tabs_total', '10', 'int', 'Max Tabs Total', 'Maximum total terminal tabs per project'),
    ('project_defaults', 'session_mode', '"exclusive_takeover"', 'string', 'Session Mode', 'Client attachment policy: exclusive_takeover, shared_concurrent, independent_shells'),
    ('project_defaults', 'image_repository', '"harbor.support.tools/kubetty/kubetty"', 'string', 'Default Image Repository', 'Default container image repository for projects'),
    ('project_defaults', 'image_tag', '"latest"', 'string', 'Default Image Tag', 'Default container image tag for new projects'),
    ('project_defaults', 'dind_enabled', 'true', 'bool', 'Docker-in-Docker Enabled', 'Enable Docker-in-Docker by default for new projects'),

    -- Auth
    ('auth', 'access_token_ttl', '"24h"', 'string', 'Access Token TTL', 'JWT access token time-to-live duration'),
    ('auth', 'refresh_token_ttl', '"720h"', 'string', 'Refresh Token TTL', 'Refresh token time-to-live duration (30 days)'),
    ('auth', 'session_idle_timeout', '"2h"', 'string', 'Session Idle Timeout', 'Timeout for idle sessions'),
    ('auth', 'max_failed_logins', '5', 'int', 'Max Failed Logins', 'Maximum failed login attempts before lockout'),
    ('auth', 'lockout_duration', '"15m"', 'string', 'Lockout Duration', 'Duration of account lockout after max failed logins'),

    -- Features
    ('features', 'session_logging_enabled', 'true', 'bool', 'Session Logging', 'Enable terminal session logging for replay'),
    ('features', 'session_log_retention_hours', '720', 'int', 'Log Retention Hours', 'Hours to retain session logs (default 30 days)'),
    ('features', 'session_log_max_entries', '5000', 'int', 'Max Log Entries', 'Maximum log entries per session'),
    ('features', 'motd_enabled', 'true', 'bool', 'MOTD Enabled', 'Show Message of the Day on terminal connect'),
    ('features', 'metrics_enabled', 'true', 'bool', 'Metrics Collection', 'Enable Prometheus metrics collection'),
    ('features', 'health_check_interval', '"30s"', 'string', 'Health Check Interval', 'Interval between project health checks'),
    ('features', 'template_sync_enabled', 'true', 'bool', 'Template Sync', 'Enable template PVC sync for new projects'),
    ('features', 'rate_limit_enabled', 'true', 'bool', 'Rate Limiting', 'Enable API rate limiting'),
    ('features', 'rate_limit_requests_per_minute', '60', 'int', 'Requests Per Minute', 'Max API requests per minute per client'),
    ('features', 'rate_limit_burst', '10', 'int', 'Rate Limit Burst', 'Burst allowance for rate limiting'),
    ('features', 'connection_throttle_enabled', 'false', 'bool', 'Connection Throttling', 'Enable WebSocket connection throttling'),
    ('features', 'max_connections_per_ip', '10', 'int', 'Max Connections Per IP', 'Maximum concurrent connections per IP'),

    -- UI
    ('ui', 'terminal_font_family', '"Monaco, Menlo, monospace"', 'string', 'Terminal Font', 'Font family for terminal display'),
    ('ui', 'terminal_font_size', '14', 'int', 'Terminal Font Size', 'Font size in pixels for terminal'),
    ('ui', 'terminal_theme', '"dark"', 'string', 'Terminal Theme', 'Terminal color theme (dark/light)'),
    ('ui', 'show_resource_metrics', 'true', 'bool', 'Show Resource Metrics', 'Display CPU/memory metrics in tab headers'),
    ('ui', 'default_rows', '24', 'int', 'Default Terminal Rows', 'Default number of rows for terminal'),
    ('ui', 'default_cols', '80', 'int', 'Default Terminal Columns', 'Default number of columns for terminal'),
    ('ui', 'max_scrollback', '10000', 'int', 'Max Scrollback Lines', 'Maximum scrollback buffer size'),
    ('ui', 'motd_content', '"Welcome to KubeTTY!\nType `help` for available commands."', 'string', 'MOTD Content', 'Message of the Day text shown on terminal connect'),

    -- Controller
    ('controller', 'template_pvc_name', '"kubetty-template"', 'string', 'Template PVC Name', 'Name of the template PVC to copy files from'),
    ('controller', 'sync_image', '"harbor.support.tools/kubetty/kubetty:latest"', 'string', 'Sync Job Image', 'Container image for template sync jobs'),
    ('controller', 'resource_prefix', '"kubetty-project-"', 'string', 'Resource Prefix', 'Prefix for all project Kubernetes resources'),
    ('controller', 'reconcile_interval', '"30s"', 'string', 'Reconcile Interval', 'Controller reconciliation loop interval'),
    ('controller', 'health_check_timeout', '"10s"', 'string', 'Health Check Timeout', 'Timeout for project health checks'),
    ('controller', 'max_concurrent_syncs', '3', 'int', 'Max Concurrent Syncs', 'Maximum template sync jobs running simultaneously'),

    -- Notifications
    ('notifications', 'webhook_url', '""', 'string', 'Webhook URL', 'URL for sending notification webhooks'),
    ('notifications', 'webhook_enabled', 'false', 'bool', 'Webhooks Enabled', 'Enable webhook notifications'),
    ('notifications', 'email_smtp_host', '""', 'string', 'SMTP Host', 'SMTP server hostname for email notifications'),
    ('notifications', 'email_smtp_port', '587', 'int', 'SMTP Port', 'SMTP server port'),
    ('notifications', 'email_from', '""', 'string', 'Email From', 'From address for notification emails'),
    ('notifications', 'email_enabled', 'false', 'bool', 'Email Enabled', 'Enable email notifications'),
    ('notifications', 'notify_on_project_failure', 'true', 'bool', 'Notify on Failure', 'Send notification when project enters failed state'),
    ('notifications', 'notify_on_project_create', 'false', 'bool', 'Notify on Create', 'Send notification when project is created'),

    -- Secrets (sensitive, can be overridden)
    ('secrets', 'github_token', '""', 'string', 'GitHub Token', 'GitHub personal access token for repository access'),
    ('secrets', 'harbor_password', '""', 'string', 'Harbor Password', 'Harbor registry password'),
    ('secrets', 'slack_webhook_url', '""', 'string', 'Slack Webhook', 'Slack incoming webhook URL for notifications'),
    ('secrets', 'custom_env_vars', '{}', 'json', 'Custom Env Vars', 'JSON object of custom environment variables for all projects')
ON CONFLICT (category, key) DO NOTHING;

-- Mark sensitive settings
UPDATE kubetty_settings SET is_sensitive = true WHERE category = 'secrets';
