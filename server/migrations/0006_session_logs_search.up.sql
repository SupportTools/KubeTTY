-- Migration: Add indexes for session log search functionality
-- Task #3780: Add session log search functionality

-- Index for direction filtering (useful for filtering client input vs session output)
CREATE INDEX IF NOT EXISTS session_logs_direction_idx ON session_logs(session_uuid, direction);

-- Note: Full-text search on payload is performed using convert_from(payload, 'UTF8')
-- at query time. We don't add a functional index here because:
-- 1. The payload column stores terminal data which may contain invalid UTF-8
-- 2. The existing (session_uuid, created_at) index handles the primary filtering
-- 3. LIMIT on results keeps query performance reasonable
-- If search performance becomes an issue, consider a GIN index on text content.
