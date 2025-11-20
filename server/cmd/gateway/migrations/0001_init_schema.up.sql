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
