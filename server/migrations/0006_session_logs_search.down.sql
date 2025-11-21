-- Rollback: Remove session log search indexes
-- Task #3780: Add session log search functionality

DROP INDEX IF EXISTS session_logs_direction_idx;
