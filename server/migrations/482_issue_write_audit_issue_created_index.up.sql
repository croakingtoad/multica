-- Supports reconstruction of one issue's write history. CONCURRENTLY avoids
-- blocking audit inserts; duration is proportional to retained rows. Reversible.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_write_audit_issue_created
    ON issue_write_audit (workspace_id, issue_id, created_at DESC);
