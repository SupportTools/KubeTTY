ALTER TABLE kubetty_projects
ADD COLUMN IF NOT EXISTS session_mode VARCHAR(32) NOT NULL DEFAULT 'exclusive_takeover';

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'valid_session_mode'
      AND conrelid = 'kubetty_projects'::regclass
  ) THEN
    ALTER TABLE kubetty_projects
    ADD CONSTRAINT valid_session_mode
    CHECK (session_mode IN ('exclusive_takeover', 'shared_concurrent', 'independent_shells'));
  END IF;
END $$;
