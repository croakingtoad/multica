-- Hook fire rows are append-only in their observed identity and result. The
-- one sanctioned UPDATE in this query set is MergeRuntimeHookData in
-- runtime.sql: it may re-point runtime_id during a legacy-runtime merge while
-- leaving provider, event, execution_id, hook_spec, fired_at, provenance, outcome,
-- and detail byte-for-byte unchanged. That preserves the append-only history
-- intent while attaching the same observations to the surviving runtime.

-- name: InsertHookFireHistory :execrows
INSERT INTO hook_fire_history (
    id, runtime_id, provider, event, execution_id, hook_spec,
    fired_at, provenance, outcome, detail
) VALUES (
    @id, @runtime_id, @provider, @event, @execution_id, @hook_spec,
    @fired_at, @provenance, @outcome, @detail
)
ON CONFLICT (id) DO NOTHING;

-- name: PruneHookFireHistory :execrows
-- Retention is one DELETE by predicate, not a select-then-delete read path.
-- The subquery only identifies overflow rows inside the same statement. The
-- runtime row lock held by ReportHookFires serializes this with other batches.
DELETE FROM hook_fire_history AS fire
WHERE fire.runtime_id = @runtime_id
  AND (
    fire.fired_at < now() - INTERVAL '30 days'
    OR fire.id IN (
      SELECT overflow.id
      FROM hook_fire_history AS overflow
      WHERE overflow.runtime_id = @runtime_id
      ORDER BY overflow.fired_at DESC, overflow.id DESC
      OFFSET sqlc.arg(max_rows)::int
    )
  );
