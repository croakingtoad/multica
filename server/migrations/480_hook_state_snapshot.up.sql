-- Lifecycle Hooks (LOCO-114): per-runtime hook state SNAPSHOT.
--
-- This table is a NON-AUTHORITATIVE cache. The host is the source of truth
-- for what hooks exist; this table only records what we last saw. It was
-- approved by ruling DP-LOCO-114-01 under five invariants. The table exists
-- because of them; if one stops holding, the table is re-decidable (or
-- droppable).
--
-- 1. Never read while the runtime is online. An online read always
--    re-initiates the host read (local-skills template) and refreshes this
--    snapshot from the result. The snapshot answers exactly one question:
--    what did we last see, and when.
-- 2. Never written by a user action. Only a host report writes it. A hook
--    edit goes host-first through the narrow writer; the snapshot updates
--    only from the subsequent read-back.
-- 3. Never rendered without its observed_at. No screen shows cached hook
--    state undated; "view last known hooks" is a stale-cache affordance.
-- 4. Droppable at zero cost beyond a refetch. Nothing may foreign-key to
--    this table, and no authoritative state may be derived from it.
-- 5. Carries source-file identity -- source_path plus content_hash --
--    alongside observed_at, so a refresh distinguishes "changed since"
--    (hash differs), "checked and unchanged" (hash equal), and "absent"
--    (both NULL).
--
-- Why not agent_runtime.metadata: registration overwrites that blob
-- wholesale by design (metadata = EXCLUDED.metadata, queries/runtime.sql),
-- and SetAgentRuntimeOfflineWithReason depends on the overwrite -- user hook
-- config there is destroyed on every daemon reconnect. Rejected with
-- reasons in DP-LOCO-114-01.
--
-- Why fire history is a separate table (hook_fire_history, 481): the two
-- stores have different truth status. This snapshot is droppable
-- (invariant 4); the fire log is the server's ONLY copy of its data
-- (Claude's debug log rotates on the host, Codex writes none). One table
-- would either make the log droppable or the cache undeletable.
--
-- No precedence/override columns: both providers MERGE hooks across layers
-- and nothing is ever shadowed (LOCO-114 verified fact 1). A winner column
-- would encode a falsehood.
--
-- Locks: CREATE TABLE plus inline unique index on a new empty relation; no
-- existing table is scanned or rewritten. Expected duration at production
-- scale: <1 second excluding lock-queue time.
--
-- Reversibility: fully reversible by 480_hook_state_snapshot.down.sql. The
-- down destroys only cache; a host refetch rebuilds it at zero cost.

CREATE TABLE hook_state_snapshot (
    -- Runtime relationships and dependent cleanup are enforced explicitly by
    -- the application inside the transaction that deletes agent_runtime.
    runtime_id UUID NOT NULL,
    -- Approved hook-capable providers. A third is a new migration that
    -- widens the check, never a data fix.
    provider TEXT NOT NULL CHECK (provider IN ('claude', 'codex')),
    -- Claude: user / project / local. Codex layers are a subset
    -- (user / project); 'local' is a superset value it never uses.
    scope TEXT NOT NULL CHECK (scope IN ('user', 'project', 'local')),
    -- Source format is identity, not decoration. Codex merges hooks.json
    -- with inline [hooks] in config.toml within one scope and neither wins,
    -- so format distinguishes two coexisting source files. Claude rows always
    -- use 'json'.
    format TEXT NOT NULL CHECK (format IN ('json', 'toml')),
    -- Provider-native hook entries as observed for this scope. Shape is
    -- provider-specific (object for Claude's "hooks" key); stage 3 owns the
    -- parse, so no deeper shape check here.
    hooks JSONB NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(hooks) IN ('object', 'array')),
    -- Claude's "_disabledHooks" sidecar (Park), or Codex's reserved "state"
    -- sub-table lifted out of "hooks". Hooks contains only event keys for
    -- both providers.
    disabled_hooks JSONB NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(disabled_hooks) IN ('object', 'array')),
    -- Source-file identity (invariant 5). Both NULL = "checked and absent".
    -- content_hash is sha256 (hex) of the WHOLE source file: a cheap
    -- refresh prefilter, not a claim about which key changed.
    source_path TEXT,
    content_hash TEXT CHECK (content_hash IS NULL OR content_hash ~ '^[0-9a-f]{64}$'),
    -- Host-reported observation time (stage 2's report carries it). Store as
    -- reported -- do not substitute now(); invariant 3 renders on this.
    observed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- The widened lookup key gives each source file its own row. Its
    -- (runtime_id, provider, scope) leading prefix still lets one index serve
    -- offline-view lookup and the refresh-upsert arbiter. The disjoint-index
    -- rationale for splitting state from fire
    -- history therefore survives unchanged.
    CONSTRAINT hook_state_snapshot_runtime_id_provider_scope_format_key
        UNIQUE (runtime_id, provider, scope, format),
    CONSTRAINT hook_state_snapshot_source_identity_check
        CHECK ((source_path IS NULL) = (content_hash IS NULL))
);
