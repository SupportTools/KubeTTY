-- Add composite index on (client_id, project_id) for efficient tab limit queries
CREATE INDEX IF NOT EXISTS gateway_tabs_client_project_idx ON gateway_tabs(client_id, project_id);
