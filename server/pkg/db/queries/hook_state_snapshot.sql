-- name: UpsertHookStateSnapshot :exec
INSERT INTO hook_state_snapshot (
    runtime_id, provider, scope, format, hooks, disabled_hooks,
    source_path, content_hash, observed_at
) VALUES (
    @runtime_id, @provider, @scope, @format, @hooks, @disabled_hooks,
    @source_path, @content_hash, @observed_at
)
ON CONFLICT (runtime_id, provider, scope, format) DO UPDATE SET
    hooks = EXCLUDED.hooks,
    disabled_hooks = EXCLUDED.disabled_hooks,
    source_path = EXCLUDED.source_path,
    content_hash = EXCLUDED.content_hash,
    observed_at = EXCLUDED.observed_at,
    updated_at = now();

-- name: DeleteHookStateSnapshotSource :exec
DELETE FROM hook_state_snapshot
WHERE runtime_id = @runtime_id
  AND provider = @provider
  AND scope = @scope
  AND format = @format;

-- name: ListHookStateSnapshot :many
SELECT runtime_id, provider, scope, format, hooks, disabled_hooks,
       source_path, content_hash, observed_at, created_at, updated_at
FROM hook_state_snapshot
WHERE runtime_id = @runtime_id
  AND provider = @provider
ORDER BY scope, format;
