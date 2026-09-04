-- LOCO-896 rollback for remap_project_plan_migration_versions_loco896.sql.
-- Run only before any subsequent migration run; it restores the old ledger
-- names and does not undo any schema change.

BEGIN;
SELECT pg_advisory_lock(7244554146635925501);
LOCK TABLE public.schema_migrations IN SHARE ROW EXCLUSIVE MODE;

CREATE TEMP TABLE loco896_project_plan_rollback (
	new_version TEXT PRIMARY KEY,
	old_version TEXT NOT NULL UNIQUE
) ON COMMIT DROP;

INSERT INTO loco896_project_plan_rollback (new_version, old_version) VALUES
	('451_project_plan', '446_project_plan'),
	('452_project_plan_project_version_key', '447_project_plan_project_version_key'),
	('453_project_plan_project_id_active_idx', '448_project_plan_project_id_active_idx'),
	('454_project_plan_workspace_id_idx', '449_project_plan_workspace_id_idx'),
	('455_project_plan_source_issue_id_idx', '450_project_plan_source_issue_id_idx'),
	('456_project_plan_kind_idx', '451_project_plan_kind_idx'),
	('457_project_plan_phase_plan_position_key', '452_project_plan_phase_plan_position_key'),
	('458_project_plan_part_phase_position_key', '453_project_plan_part_phase_position_key'),
	('459_project_plan_part_plan_phase_idx', '454_project_plan_part_plan_phase_idx'),
	('460_project_plan_part_issue_plan_issue_key', '455_project_plan_part_issue_plan_issue_key'),
	('461_project_plan_part_issue_plan_part_issue_idx', '456_project_plan_part_issue_plan_part_issue_idx'),
	('462_project_plan_part_issue_issue_id_idx', '457_project_plan_part_issue_issue_id_idx'),
	('463_project_plan_dependency_edge_key', '458_project_plan_dependency_edge_key'),
	('464_project_plan_dependency_blocked_phase_idx', '459_project_plan_dependency_blocked_phase_idx'),
	('465_project_plan_dependency_blocked_part_idx', '460_project_plan_dependency_blocked_part_idx'),
	('466_project_plan_dependency_blocking_phase_idx', '461_project_plan_dependency_blocking_phase_idx'),
	('467_project_plan_dependency_blocking_part_idx', '462_project_plan_dependency_blocking_part_idx'),
	('468_project_plan_kind_key_index', '463_project_plan_kind_key_index'),
	('469_project_plan_id_index', '464_project_plan_id_index'),
	('470_project_plan_phase_id_index', '465_project_plan_phase_id_index'),
	('471_project_plan_part_id_index', '466_project_plan_part_id_index'),
	('472_project_plan_part_issue_id_index', '467_project_plan_part_issue_id_index'),
	('473_project_plan_dependency_id_index', '468_project_plan_dependency_id_index');

DO $$
DECLARE
	expected_count INTEGER;
	changed_count INTEGER;
BEGIN
	SELECT count(*)
	INTO expected_count
	FROM public.schema_migrations AS sm
	JOIN loco896_project_plan_rollback AS rollback ON rollback.new_version = sm.version;

	IF expected_count = 0 THEN
		RAISE EXCEPTION 'LOCO-896 rollback found no remapped project-plan migration entries; refusing a no-op';
	END IF;

	IF EXISTS (
		SELECT 1
		FROM public.schema_migrations AS sm
		JOIN loco896_project_plan_rollback AS rollback ON rollback.old_version = sm.version
	) THEN
		RAISE EXCEPTION 'LOCO-896 rollback found legacy project-plan entries; refusing to mix ledger states';
	END IF;

	IF EXISTS (
		SELECT 1
		FROM public.schema_migrations
		WHERE strpos(version, '__loco896_project_plan_rollback__') = 1
	) THEN
		RAISE EXCEPTION 'LOCO-896 rollback found temporary ledger entries from an interrupted manual operation';
	END IF;

	UPDATE public.schema_migrations AS sm
	SET version = '__loco896_project_plan_rollback__' || rollback.new_version
	FROM loco896_project_plan_rollback AS rollback
	WHERE sm.version = rollback.new_version;
	GET DIAGNOSTICS changed_count = ROW_COUNT;
	IF changed_count <> expected_count THEN
		RAISE EXCEPTION 'LOCO-896 rollback staged % rows; expected %', changed_count, expected_count;
	END IF;

	UPDATE public.schema_migrations AS sm
	SET version = rollback.old_version
	FROM loco896_project_plan_rollback AS rollback
	WHERE sm.version = '__loco896_project_plan_rollback__' || rollback.new_version;
	GET DIAGNOSTICS changed_count = ROW_COUNT;
	IF changed_count <> expected_count THEN
		RAISE EXCEPTION 'LOCO-896 rollback finalized % rows; expected %', changed_count, expected_count;
	END IF;
END
$$;

COMMIT;
SELECT pg_advisory_unlock(7244554146635925501);
