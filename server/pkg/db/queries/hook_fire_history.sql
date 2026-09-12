-- Hook fire rows are append-only in their observed identity and result. The
-- one sanctioned UPDATE in this query set is MergeRuntimeHookData in
-- runtime.sql: it may re-point runtime_id during a legacy-runtime merge while
-- leaving provider, event, execution_id, hook_spec, fired_at, provenance, outcome,
-- and detail byte-for-byte unchanged. That preserves the append-only history
-- intent while attaching the same observations to the surviving runtime.
-- The one sanctioned INSERT in this query set is InsertHookFireHistory below:
-- it appends observed fire rows without changing existing rows.
-- The one sanctioned retention DELETE in this query set is PruneHookFireHistory
-- below: it applies the age and row-cap retention policy to one runtime's history.
-- The two sanctioned cascade DELETEs in this query set are DeleteRuntimeHookData
-- in runtime.sql and DeleteWorkspaceRuntimeHookData in workspace_delete.sql.

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
-- name: ListRuntimeHookFires :many
-- The feed read path (LOCO-135), authorised by DP-LOCO-114-03 under four
-- binding conditions. Three of them are properties of this statement:
--
--  1. Read-only. This is the only SELECT in the hook_fire_history query set;
--     the mutating set stays exactly InsertHookFireHistory, the two cascade
--     deletes (runtime.sql / workspace_delete.sql), MergeRuntimeHookData's
--     runtime_id-only UPDATE, and retention's delete-by-predicate.
--  2. No provenance computation at read time. `provenance` is projected
--     verbatim. This statement must never derive, upgrade, COALESCE or CASE
--     it -- round 3 of this effort shipped a greedy join that assigned one
--     execution's timestamp to another and stamped the result 'debug_log',
--     fabricating provenance. The capture path is now guarded against that by
--     four audits; a read path that recomputes provenance reintroduces the
--     same defect in a layer nobody is watching.
--  3. No join on execution_id as configured-hook identity. execution_id is
--     Claude's per-execution reference, fresh on every fire (migration 481,
--     DP-LOCO-114-02 item 3). Nothing here may GROUP BY, JOIN, DISTINCT or
--     dedupe on it. This is a flat time-ordered projection of single rows, so
--     no grouping exists to get wrong.
--
-- Ordering and predicate are exactly the mandated feed index
-- hook_fire_history_runtime_id_fired_at_idx (runtime_id, fired_at DESC).
--
-- fired_at is host-reported for 'debug_log' rows and the daemon's receipt time
-- for 'inferred' rows. That difference is the whole point of the two-tier
-- design and the reader must be told it per row, so provenance travels beside
-- fired_at on the wire and is never collapsed into a per-provider claim.
SELECT
    hook_fire_history.id,
    hook_fire_history.provider,
    hook_fire_history.event,
    hook_fire_history.execution_id,
    hook_fire_history.hook_spec,
    hook_fire_history.fired_at,
    hook_fire_history.provenance,
    hook_fire_history.outcome,
    hook_fire_history.detail
FROM hook_fire_history
WHERE hook_fire_history.runtime_id = @runtime_id
ORDER BY hook_fire_history.fired_at DESC
LIMIT @row_limit::int;
