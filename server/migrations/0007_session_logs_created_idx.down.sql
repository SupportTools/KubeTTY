-- Rollback: 0007_session_logs_created_idx
-- Remove the created_at index for log retention queries

DROP INDEX IF EXISTS session_logs_created_idx;
