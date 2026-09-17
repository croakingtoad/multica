-- Retains issue text-write attribution without retaining title or description
-- bodies. This takes only the brief catalog lock required to create an empty
-- table; duration is independent of issue count. Reversible while callers no
-- longer depend on the audit history.
CREATE TABLE issue_write_audit (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('member', 'agent', 'plugin', 'system')),
    actor_id UUID,
    source_task_id UUID,
    client_platform TEXT NOT NULL,
    client_version TEXT,
    client_os TEXT,
    endpoint TEXT NOT NULL,
    requested_fields TEXT[] NOT NULL,
    changed_fields TEXT[] NOT NULL,
    old_title_bytes BIGINT,
    new_title_bytes BIGINT,
    old_title_hash TEXT,
    new_title_hash TEXT,
    old_description_bytes BIGINT,
    new_description_bytes BIGINT,
    old_description_hash TEXT,
    new_description_hash TEXT,
    expected_revision BIGINT,
    revision_before BIGINT NOT NULL,
    revision_after BIGINT NOT NULL,
    revision_conflict BOOLEAN NOT NULL,
    outcome TEXT NOT NULL CHECK (outcome IN ('applied', 'no_change', 'revision_conflict')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (actor_type = 'system' OR actor_id IS NOT NULL),
    CHECK (source_task_id IS NULL OR actor_type = 'agent'),
    CHECK (old_title_hash IS NULL OR old_title_hash ~ '^[0-9a-f]{64}$'),
    CHECK (new_title_hash IS NULL OR new_title_hash ~ '^[0-9a-f]{64}$'),
    CHECK (old_description_hash IS NULL OR old_description_hash ~ '^[0-9a-f]{64}$'),
    CHECK (new_description_hash IS NULL OR new_description_hash ~ '^[0-9a-f]{64}$'),
    CHECK (revision_conflict = (outcome = 'revision_conflict'))
);
