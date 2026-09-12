-- Lifecycle Hooks (LOCO-114): per-runtime hook FIRE HISTORY.
--
-- This table is SERVER-AUTHORITATIVE -- the only copy of this data. Claude's
-- debug log rotates on the host and Codex writes no per-hook log at all, so
-- if the server drops a row, it is gone. Do not make it droppable.
--
-- Why a separate table from hook_state_snapshot (480): different truth
-- status (that snapshot is droppable, invariant 4; this log is the only
-- copy), different growth (small, bounded, wholesale-replaced vs
-- append-only, unbounded), disjoint index needs, and the rule below.
--
-- This table must NOT reference hook_state_snapshot -- no FK, no key. A fire
-- may refer to a hook that has since been edited or deleted, and on Codex an
-- edited hook is a DIFFERENT hook by the provider's own trust-hash
-- reckoning. An FK would either cascade history away on an edit or block the
-- edit. This is a correctness requirement, not a normalization preference.
-- The identity observed at fire time is therefore denormalized here:
-- provider, event, hook_id, hook_spec.
--
-- hook_id contract (host-side, stage 2/3): stable across fires of an
-- unedited hook, and changes when the hook's content changes. On Codex it is
-- the provider's own trust hash. The schema cannot enforce this; the reader
-- does.
--
-- Append-only: the only write is INSERT. The only sanctioned deletion is the
-- retention policy (stage 7; intended default 30 days plus a per-runtime row
-- cap). No trigger enforces immutability: a trigger would fight retention's
-- DELETE, and there is no UPDATE path in the query set, which is what keeps
-- rows immutable.
--
-- provenance is rendered on screen: the UI must state that Claude fires are
-- read from the debug log (observed) while Codex fires are inferred from
-- exit codes and statusMessage (not direct observation).
--
-- No updated_at (documented exception to the house timestamp rule): rows are
-- never updated. created_at is server insert time; fired_at is the
-- host-reported event time (store as reported, do not substitute now()).
--
-- Retention readiness: the (runtime_id, fired_at DESC) index below is both
-- the feed index and the retention scan path (per-runtime time prune and row
-- cap). A global prune loops per runtime (runtimes are bounded); if that
-- outgrows, add a (fired_at)-leading index in a later additive migration --
-- not now.
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
    -- transaction that deletes agent_runtime. Retention for live runtimes is
    -- stage 7's separate responsibility.
    runtime_id UUID NOT NULL,
    -- Denormalized observed identity (see block above): self-contained so a
    -- later edit/delete of the hook cannot touch these rows.
    provider TEXT NOT NULL CHECK (provider IN ('claude', 'codex')),
    -- Hook event as reported (e.g. 'PreToolUse', 'Stop'). Free text on
    -- purpose: provider event catalogs (33 Claude / 12 Codex) evolve with
    -- provider releases; a CHECK enum would churn.
    event TEXT NOT NULL,
    -- Provider-derived identity of the hook at fire time (contract above).
    hook_id TEXT NOT NULL,
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
    -- decision:block continuation (LOCO-114 verified fact 4).
    outcome TEXT NOT NULL
        CHECK (outcome IN ('success', 'failure', 'blocked', 'skipped', 'unknown')),
    -- Stage 7's raw material (exit code, stderr first line, duration,
    -- statusMessage, ...); shape settled there.
    detail JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- THE feed index (mandated): per-runtime, time-ordered. Also serves the
-- filtered feed (runtime + time-range spine) and retention scans (backward
-- scan covers fired_at < cutoff within a runtime). Runtime cleanup also uses
-- this leading prefix -- no separate index.
CREATE INDEX hook_fire_history_runtime_id_fired_at_idx
    ON hook_fire_history (runtime_id, fired_at DESC);
