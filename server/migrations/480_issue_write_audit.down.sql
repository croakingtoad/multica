-- Drops retained issue-write history. The table lock is brief; runtime is
-- proportional only to catalog cleanup. This rollback is destructive.
DROP TABLE IF EXISTS issue_write_audit;
