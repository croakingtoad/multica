-- Stable audit-record identity. CONCURRENTLY avoids blocking audit inserts;
-- duration is proportional to retained audit rows. Reversible.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_write_audit_id
    ON issue_write_audit (id);
