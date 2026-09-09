-- LOCO-1499 rollback: undo remap_project_plan_migration_versions_loco1499.sql
-- by renaming the project-plan ledger entries from 457-479 back to 451-473.
--
-- Only valid while schema_migrations still contains the remapped 457-479
-- entries and nothing newer has been applied on top. Same lock, same
-- two-phase staged rename, same refusal guards as the forward script.
--
-- Run with: psql "$DATABASE_URL" -X -v ON_ERROR_STOP=1 -f \
--   server/scripts/rollback_project_plan_migration_versions_loco1499.sql

BEGIN;

SELECT pg_advisory_lock(7244554146635925501);
LOCK TABLE public.schema_migrations IN SHARE ROW EXCLUSIVE MODE;

CREATE TEMP TABLE loco1499_project_plan_remap_rollback (

	old_version TEXT PRIMARY KEY,
	new_version TEXT NOT NULL UNIQUE
) ON COMMIT DROP;

INSERT INTO loco1499_project_plan_remap_rollback (old_version, new_version) VALUES
	('457_project_plan', '451_project_plan'),
	('458_project_plan_project_version_key', '452_project_plan_project_version_key'),
	('459_project_plan_project_id_active_idx', '453_project_plan_project_id_active_idx'),
	('460_project_plan_workspace_id_idx', '454_project_plan_workspace_id_idx'),
	('461_project_plan_source_issue_id_idx', '455_project_plan_source_issue_id_idx'),
	('462_project_plan_kind_idx', '456_project_plan_kind_idx'),
	('463_project_plan_phase_plan_position_key', '457_project_plan_phase_plan_position_key'),
	('464_project_plan_part_phase_position_key', '458_project_plan_part_phase_position_key'),
	('465_project_plan_part_plan_phase_idx', '459_project_plan_part_plan_phase_idx'),
	('466_project_plan_part_issue_plan_issue_key', '460_project_plan_part_issue_plan_issue_key'),
	('467_project_plan_part_issue_plan_part_issue_idx', '461_project_plan_part_issue_plan_part_issue_idx'),
	('468_project_plan_part_issue_issue_id_idx', '462_project_plan_part_issue_issue_id_idx'),
	('469_project_plan_dependency_edge_key', '463_project_plan_dependency_edge_key'),
	('470_project_plan_dependency_blocked_phase_idx', '464_project_plan_dependency_blocked_phase_idx'),
	('471_project_plan_dependency_blocked_part_idx', '465_project_plan_dependency_blocked_part_idx'),
	('472_project_plan_dependency_blocking_phase_idx', '466_project_plan_dependency_blocking_phase_idx'),
	('473_project_plan_dependency_blocking_part_idx', '467_project_plan_dependency_blocking_part_idx'),
	('474_project_plan_kind_key_index', '468_project_plan_kind_key_index'),
	('475_project_plan_id_index', '469_project_plan_id_index'),
	('476_project_plan_phase_id_index', '470_project_plan_phase_id_index'),
	('477_project_plan_part_id_index', '471_project_plan_part_id_index'),
	('478_project_plan_part_issue_id_index', '472_project_plan_part_issue_id_index'),
	('479_project_plan_dependency_id_index', '473_project_plan_dependency_id_index');

DO $$
DECLARE
	expected_count INTEGER;
	changed_count INTEGER;
BEGIN
	SELECT count(*)
	INTO expected_count
	FROM public.schema_migrations AS sm
	JOIN loco1499_project_plan_remap_rollback AS remap ON remap.old_version = sm.version;

	IF expected_count = 0 THEN
		RAISE EXCEPTION 'LOCO-1499 rollback found no remapped project-plan migration entries; refusing a no-op';
	END IF;

	IF EXISTS (
		SELECT 1
		FROM public.schema_migrations AS sm
		JOIN loco1499_project_plan_remap_rollback AS remap ON remap.new_version = sm.version
	) THEN
		RAISE EXCEPTION 'LOCO-1499 rollback found pre-remap project-plan entries; refusing to mix ledger states';
	END IF;

	IF EXISTS (
		SELECT 1
		FROM public.schema_migrations
		WHERE strpos(version, '__loco1499_rollback__') = 1
	) THEN
		RAISE EXCEPTION 'LOCO-1499 rollback found temporary ledger entries from an interrupted manual operation';
	END IF;

	UPDATE public.schema_migrations AS sm
	SET version = '__loco1499_rollback__' || remap.old_version
	FROM loco1499_project_plan_remap_rollback AS remap
	WHERE sm.version = remap.old_version;
	GET DIAGNOSTICS changed_count = ROW_COUNT;
	IF changed_count <> expected_count THEN
		RAISE EXCEPTION 'LOCO-1499 rollback staged % rows; expected %', changed_count, expected_count;
	END IF;

	UPDATE public.schema_migrations AS sm
	SET version = remap.new_version
	FROM loco1499_project_plan_remap_rollback AS remap
	WHERE sm.version = '__loco1499_rollback__' || remap.old_version;
	GET DIAGNOSTICS changed_count = ROW_COUNT;
	IF changed_count <> expected_count THEN
		RAISE EXCEPTION 'LOCO-1499 rollback finalized % rows; expected %', changed_count, expected_count;
	END IF;
END
$$;

COMMIT;
SELECT pg_advisory_unlock(7244554146635925501);
