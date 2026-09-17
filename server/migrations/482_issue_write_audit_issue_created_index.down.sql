-- Remove the issue-history lookup index without blocking audit inserts.
DROP INDEX CONCURRENTLY IF EXISTS idx_issue_write_audit_issue_created;
