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
