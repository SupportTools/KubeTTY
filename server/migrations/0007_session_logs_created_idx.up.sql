-- Migration: 0007_session_logs_created_idx
-- Purpose: Add index on created_at for log retention queries
-- The PruneLogs() function deletes logs by timestamp only,
-- which cannot efficiently use the existing composite index (session_uuid, created_at).
-- This dedicated index improves performance of retention cleanup queries.
-- Note: CONCURRENTLY allows the index to be created without locking the table.

CREATE INDEX CONCURRENTLY session_logs_created_idx ON session_logs(created_at);
