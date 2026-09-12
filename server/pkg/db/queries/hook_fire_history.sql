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
