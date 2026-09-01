-- LOCO-812: rename the project-plan migration ledger entries after moving
-- migrations 441-463 to 446-468. This is a one-off operator script, not a
-- normal migration. Run it once, before starting a build containing the
-- renumbered files.
--
-- Locks: the migration advisory lock and schema_migrations with SHARE ROW
-- EXCLUSIVE MODE. Expected duration: milliseconds for up to 23 ledger rows.
-- Reversible: only before a subsequent migration run; use the paired rollback
-- script while schema_migrations still contains the remapped entries.
--
-- Run with: psql "$DATABASE_URL" -X -v ON_ERROR_STOP=1 -f \
--   server/scripts/remap_project_plan_migration_versions_loco812.sql

BEGIN;

-- Match cmd/migrate's session-level advisory lock so a running migrator cannot
-- observe the ledger halfway through the two-phase rename.
SELECT pg_advisory_lock(7244554146635925501);
LOCK TABLE public.schema_migrations IN SHARE ROW EXCLUSIVE MODE;

CREATE TEMP TABLE loco812_project_plan_remap (

	old_version TEXT PRIMARY KEY,
	new_version TEXT NOT NULL UNIQUE
) ON COMMIT DROP;

INSERT INTO loco812_project_plan_remap (old_version, new_version) VALUES
	('441_project_plan', '446_project_plan'),
	('442_project_plan_project_version_key', '447_project_plan_project_version_key'),
	('443_project_plan_project_id_active_idx', '448_project_plan_project_id_active_idx'),
	('444_project_plan_workspace_id_idx', '449_project_plan_workspace_id_idx'),
	('445_project_plan_source_issue_id_idx', '450_project_plan_source_issue_id_idx'),
	('446_project_plan_kind_idx', '451_project_plan_kind_idx'),
	('447_project_plan_phase_plan_position_key', '452_project_plan_phase_plan_position_key'),
	('448_project_plan_part_phase_position_key', '453_project_plan_part_phase_position_key'),
	('449_project_plan_part_plan_phase_idx', '454_project_plan_part_plan_phase_idx'),
	('450_project_plan_part_issue_plan_issue_key', '455_project_plan_part_issue_plan_issue_key'),
	('451_project_plan_part_issue_plan_part_issue_idx', '456_project_plan_part_issue_plan_part_issue_idx'),
	('452_project_plan_part_issue_issue_id_idx', '457_project_plan_part_issue_issue_id_idx'),
	('453_project_plan_dependency_edge_key', '458_project_plan_dependency_edge_key'),
	('454_project_plan_dependency_blocked_phase_idx', '459_project_plan_dependency_blocked_phase_idx'),
	('455_project_plan_dependency_blocked_part_idx', '460_project_plan_dependency_blocked_part_idx'),
	('456_project_plan_dependency_blocking_phase_idx', '461_project_plan_dependency_blocking_phase_idx'),
	('457_project_plan_dependency_blocking_part_idx', '462_project_plan_dependency_blocking_part_idx'),
	('458_project_plan_kind_key_index', '463_project_plan_kind_key_index'),
	('459_project_plan_id_index', '464_project_plan_id_index'),
	('460_project_plan_phase_id_index', '465_project_plan_phase_id_index'),
	('461_project_plan_part_id_index', '466_project_plan_part_id_index'),
	('462_project_plan_part_issue_id_index', '467_project_plan_part_issue_id_index'),
	('463_project_plan_dependency_id_index', '468_project_plan_dependency_id_index');

DO $$
DECLARE
	expected_count INTEGER;
	changed_count INTEGER;
BEGIN
	SELECT count(*)
	INTO expected_count
	FROM public.schema_migrations AS sm
	JOIN loco812_project_plan_remap AS remap ON remap.old_version = sm.version;

	IF expected_count = 0 THEN
		RAISE EXCEPTION 'LOCO-812 remap found no legacy project-plan migration entries; refusing a no-op';
	END IF;

	IF EXISTS (
		SELECT 1
		FROM public.schema_migrations AS sm
		JOIN loco812_project_plan_remap AS remap ON remap.new_version = sm.version
	) THEN
		RAISE EXCEPTION 'LOCO-812 remap found already-renumbered project-plan entries; refusing to mix ledger states';
	END IF;

	IF EXISTS (
		SELECT 1
		FROM public.schema_migrations
		WHERE strpos(version, '__loco812_project_plan_remap__') = 1
	) THEN
		RAISE EXCEPTION 'LOCO-812 remap found temporary ledger entries from an interrupted manual operation';
	END IF;

	-- Stage first because the old 446-463 values are also source rows.
	UPDATE public.schema_migrations AS sm
	SET version = '__loco812_project_plan_remap__' || remap.old_version
	FROM loco812_project_plan_remap AS remap
	WHERE sm.version = remap.old_version;
	GET DIAGNOSTICS changed_count = ROW_COUNT;
	IF changed_count <> expected_count THEN
		RAISE EXCEPTION 'LOCO-812 remap staged % rows; expected %', changed_count, expected_count;
	END IF;

	UPDATE public.schema_migrations AS sm
	SET version = remap.new_version
	FROM loco812_project_plan_remap AS remap
	WHERE sm.version = '__loco812_project_plan_remap__' || remap.old_version;
	GET DIAGNOSTICS changed_count = ROW_COUNT;
	IF changed_count <> expected_count THEN
		RAISE EXCEPTION 'LOCO-812 remap finalized % rows; expected %', changed_count, expected_count;
	END IF;
END
$$;

COMMIT;
SELECT pg_advisory_unlock(7244554146635925501);
