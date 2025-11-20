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
