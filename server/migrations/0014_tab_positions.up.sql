-- Add position column for user-controlled tab ordering
ALTER TABLE gateway_tabs ADD COLUMN IF NOT EXISTS position INT NOT NULL DEFAULT 0;

-- Create index for efficient ordering by position
CREATE INDEX IF NOT EXISTS gateway_tabs_client_position_idx ON gateway_tabs(client_id, position);
