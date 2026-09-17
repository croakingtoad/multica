-- Remove stable audit-record identity without blocking audit inserts.
DROP INDEX CONCURRENTLY IF EXISTS idx_issue_write_audit_id;
