-- Remove position column and index
DROP INDEX IF EXISTS gateway_tabs_client_position_idx;
ALTER TABLE gateway_tabs DROP COLUMN IF EXISTS position;
