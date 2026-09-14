-- Lifecycle Hooks (LOCO-114): per-runtime hook FIRE HISTORY.
--
-- This table is SERVER-AUTHORITATIVE -- the only copy of this data. Claude's
-- debug log rotates on the host and Codex writes no per-hook log at all, so
-- if the server drops a row, it is gone. Do not make it droppable.
--
-- Why a separate table from hook_state_snapshot (480): different truth
-- status (that snapshot is droppable, invariant 4; this log is the only
-- copy), different growth (small, bounded, wholesale-replaced vs
-- append-only, retention-bounded), disjoint index needs, and the rule below.
--
-- This table must NOT reference hook_state_snapshot -- no FK, no key. A fire
-- may refer to a hook that has since been edited or deleted, and on Codex an
-- edited hook is a DIFFERENT hook by the provider's own trust-hash
-- reckoning. An FK would either cascade history away on an edit or block the
-- edit. This is a correctness requirement, not a normalization preference.
-- The identity observed at fire time is therefore denormalized here:
-- provider, event, execution_id, hook_spec.
--
-- execution_id has provider-specific semantics grounded in the available
-- source. For Claude it is hook_response.hook_id, a fresh identity for that
-- execution; it MUST NOT be presented as stable configured-hook identity.
-- For Codex the available identity remains its content-stable trust hash.
--
-- Append-only: ordinary writes only INSERT through InsertHookFireHistory. The one
-- sanctioned retention deletion is PruneHookFireHistory, and the two sanctioned
-- cascade deletions are DeleteRuntimeHookData in runtime.sql and
-- DeleteWorkspaceRuntimeHookData in workspace_delete.sql; after every accepted
-- fire batch, retention deletes rows older than 30 days and rows beyond the newest
-- 30,000 for that runtime. The cap is
-- the implementer's choice under LOCO-136, set at 30,000 as roughly 3x a
-- 10,000-row order-of-magnitude starting point given the measured 2.94x C3
-- volume; it is the hard bound when hook-heavy hosts fill 30 days early.
-- No trigger enforces immutability: a trigger would fight retention's DELETE,
-- and there is no ordinary UPDATE path in the query set, which is what keeps
-- rows immutable. Retention deletes directly by predicate in one statement;
-- it never exposes a SELECT/read path or touches hook_state_snapshot.
--
-- provenance is per row and rendered on screen. A Claude fire is debug_log
-- only when its stream response has a unique debug-record match; all other
-- Claude fires and Codex fires are inferred.
--
-- No updated_at (documented exception to the house timestamp rule): rows are
-- never updated. created_at is server insert time; fired_at is the
-- host-reported event time (store as reported, do not substitute now()).
--
-- Retention: the (runtime_id, fired_at DESC) index below is both the feed index
-- and the scan path for the per-runtime time prune and row cap. Enforcement
-- runs after each accepted batch while the runtime row lock serializes writers.
-- A runtime that sends no later batch cannot accumulate more rows, so there is
-- no separate global sweep or snapshot-table coupling.
-- Rows are pruned when their runtime next reports, so 30 days is the window a
-- reporting runtime retains; an idle runtime's rows persist past it until it
-- reports again, bounded only by the row cap.
--
-- Locks: CREATE TABLE plus one index on a new empty relation; no existing
-- table is scanned or rewritten. Expected duration at production scale: <1
-- second excluding lock-queue time.
--
-- Reversibility: schema-reversible by 481_hook_fire_history.down.sql. The
-- down destroys fire history, which is UNRECOVERABLE -- at stage 1 no rows
-- have been written, so real-data loss is zero; after stage 7 writes begin,
-- export before rolling back.

CREATE TABLE hook_fire_history (
    id UUID NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    -- The application deletes a runtime's history explicitly inside the
    -- transaction that deletes agent_runtime. The report path applies live
    -- runtime retention after every accepted batch.
    runtime_id UUID NOT NULL,
    -- Denormalized observed identity (see block above): self-contained so a
    -- later edit/delete of the hook cannot touch these rows.
    provider TEXT NOT NULL CHECK (provider IN ('claude', 'codex')),
    -- Hook event as reported (e.g. 'PreToolUse', 'Stop'). Free text on
    -- purpose: provider event catalogs (33 Claude / 12 Codex) evolve with
    -- provider releases; a CHECK enum would churn.
    event TEXT NOT NULL,
    -- Provider-derived execution reference at fire time (contract above).
    execution_id TEXT NOT NULL,
    -- The observed handler spec at fire time, so the feed can show what the
    -- hook was after it was edited or deleted.
    hook_spec JSONB NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(hook_spec) IN ('object', 'array')),
    -- Host-reported fire time; feeds ORDER BY fired_at DESC.
    fired_at TIMESTAMPTZ NOT NULL,
    -- Rendered on screen with its limits stated (block above).
    provenance TEXT NOT NULL CHECK (provenance IN ('debug_log', 'inferred')),
    -- Stage 7 owns the mapping; the CHECK is widened by a new migration if
    -- the final set differs. 'skipped' is the never-ran case; 'blocked' is a
    -- decision:block continuation (LOCO-114 verified fact 4). Claude also
    -- emits 'cancelled' for timeouts; it maps to 'unknown' because cancellation
    -- establishes neither whether the hook ran nor a success/failure outcome.
    outcome TEXT NOT NULL
        CHECK (outcome IN ('success', 'failure', 'blocked', 'skipped', 'unknown')),
    -- Stage 7's detail fields (debug outcome, message from a unique debug-log
    -- match, and exit code); shape settled there.
    detail JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- THE feed index (mandated): per-runtime, time-ordered. Also serves the
-- filtered feed (runtime + time-range spine) and retention scans (backward
-- scan covers fired_at < cutoff within a runtime). Runtime cleanup also uses
-- this leading prefix -- no separate index.
CREATE INDEX hook_fire_history_runtime_id_fired_at_idx
    ON hook_fire_history (runtime_id, fired_at DESC);
