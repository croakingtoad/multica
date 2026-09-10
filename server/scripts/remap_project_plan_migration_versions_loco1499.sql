-- LOCO-1499: rename the project-plan migration ledger entries after moving
-- migrations 451-473 to 457-479. Upstream 9fab6da9 now occupies 451-456; the
-- fork's project-plan block shifted by +6 to clear it. This is a one-off
-- operator script, not a normal migration. Run it once, before starting a
-- build containing the renumbered files.
--
-- Intended for instances that already applied 451-473 (the pre-LOCO-1499
-- numbering), such as the self-hosted instance of record (LOCO-894 /
-- LOCO-1012), whose ledger top row is 473_project_plan_dependency_id_index.
-- Fresh installs that ran the renumbered files need no remap: the script
-- refuses a no-op.
--
-- Locks: the migration advisory lock and schema_migrations with SHARE ROW
-- EXCLUSIVE MODE. Expected duration: milliseconds for up to 23 ledger rows.
-- Reversible: only before a subsequent migration run; use the paired rollback
-- script while schema_migrations still contains the remapped entries.
--
-- Run with: psql "$DATABASE_URL" -X -v ON_ERROR_STOP=1 -f \
--   server/scripts/remap_project_plan_migration_versions_loco1499.sql

BEGIN;

-- Match cmd/migrate's session-level advisory lock so a running migrator cannot
-- observe the ledger halfway through the two-phase rename.
SELECT pg_advisory_lock(7244554146635925501);
LOCK TABLE public.schema_migrations IN SHARE ROW EXCLUSIVE MODE;

CREATE TEMP TABLE loco1499_project_plan_remap (

	old_version TEXT PRIMARY KEY,
	new_version TEXT NOT NULL UNIQUE
) ON COMMIT DROP;

INSERT INTO loco1499_project_plan_remap (old_version, new_version) VALUES
	('451_project_plan', '457_project_plan'),
	('452_project_plan_project_version_key', '458_project_plan_project_version_key'),
	('453_project_plan_project_id_active_idx', '459_project_plan_project_id_active_idx'),
	('454_project_plan_workspace_id_idx', '460_project_plan_workspace_id_idx'),
	('455_project_plan_source_issue_id_idx', '461_project_plan_source_issue_id_idx'),
	('456_project_plan_kind_idx', '462_project_plan_kind_idx'),
	('457_project_plan_phase_plan_position_key', '463_project_plan_phase_plan_position_key'),
	('458_project_plan_part_phase_position_key', '464_project_plan_part_phase_position_key'),
	('459_project_plan_part_plan_phase_idx', '465_project_plan_part_plan_phase_idx'),
	('460_project_plan_part_issue_plan_issue_key', '466_project_plan_part_issue_plan_issue_key'),
	('461_project_plan_part_issue_plan_part_issue_idx', '467_project_plan_part_issue_plan_part_issue_idx'),
	('462_project_plan_part_issue_issue_id_idx', '468_project_plan_part_issue_issue_id_idx'),
	('463_project_plan_dependency_edge_key', '469_project_plan_dependency_edge_key'),
	('464_project_plan_dependency_blocked_phase_idx', '470_project_plan_dependency_blocked_phase_idx'),
	('465_project_plan_dependency_blocked_part_idx', '471_project_plan_dependency_blocked_part_idx'),
	('466_project_plan_dependency_blocking_phase_idx', '472_project_plan_dependency_blocking_phase_idx'),
	('467_project_plan_dependency_blocking_part_idx', '473_project_plan_dependency_blocking_part_idx'),
	('468_project_plan_kind_key_index', '474_project_plan_kind_key_index'),
	('469_project_plan_id_index', '475_project_plan_id_index'),
	('470_project_plan_phase_id_index', '476_project_plan_phase_id_index'),
	('471_project_plan_part_id_index', '477_project_plan_part_id_index'),
	('472_project_plan_part_issue_id_index', '478_project_plan_part_issue_id_index'),
	('473_project_plan_dependency_id_index', '479_project_plan_dependency_id_index');

DO $$
DECLARE
	expected_count INTEGER;
	changed_count INTEGER;
BEGIN
	SELECT count(*)
	INTO expected_count
	FROM public.schema_migrations AS sm
	JOIN loco1499_project_plan_remap AS remap ON remap.old_version = sm.version;

	IF expected_count = 0 THEN
		RAISE EXCEPTION 'LOCO-1499 remap found no legacy project-plan migration entries; refusing a no-op';
	END IF;

	IF EXISTS (
		SELECT 1
		FROM public.schema_migrations AS sm
		JOIN loco1499_project_plan_remap AS remap ON remap.new_version = sm.version
	) THEN
		RAISE EXCEPTION 'LOCO-1499 remap found already-renumbered project-plan entries; refusing to mix ledger states';
	END IF;

	IF EXISTS (
		SELECT 1
		FROM public.schema_migrations
		WHERE strpos(version, '__loco1499_project_plan_remap__') = 1
	) THEN
		RAISE EXCEPTION 'LOCO-1499 remap found temporary ledger entries from an interrupted manual operation';
	END IF;

	-- Stage first because the old 451-473 values overlap the new 457-479 range
	-- (457-473 exist on both sides), so a single-pass rename cannot be atomic.
	UPDATE public.schema_migrations AS sm
	SET version = '__loco1499_project_plan_remap__' || remap.old_version
	FROM loco1499_project_plan_remap AS remap
	WHERE sm.version = remap.old_version;
	GET DIAGNOSTICS changed_count = ROW_COUNT;
	IF changed_count <> expected_count THEN
		RAISE EXCEPTION 'LOCO-1499 remap staged % rows; expected %', changed_count, expected_count;
	END IF;

	UPDATE public.schema_migrations AS sm
	SET version = remap.new_version
	FROM loco1499_project_plan_remap AS remap
	WHERE sm.version = '__loco1499_project_plan_remap__' || remap.old_version;
	GET DIAGNOSTICS changed_count = ROW_COUNT;
	IF changed_count <> expected_count THEN
		RAISE EXCEPTION 'LOCO-1499 remap finalized % rows; expected %', changed_count, expected_count;
	END IF;
END
$$;

COMMIT;
SELECT pg_advisory_unlock(7244554146635925501);
