package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/projectplan"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestIssueDeletionRetainsPlanMembershipSnapshotsAndCoverage(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database unavailable")
	}
	ctx := context.Background()
	withFeatureFlag(t, testHandler, featureflags.ProjectPlans, true)

	projectID := dbfx.Project(t, "Plan deletion coverage")
	planID := dbfx.Insert(t, "project_plan", testutil.Cols{
		"workspace_id": testWorkspaceID, "project_id": projectID, "version": 1,
		"kind": "prd", "origin": "manual", "title": "Deletion coverage",
		"created_by_type": "member", "created_by_id": testUserID,
	})
	phaseID := dbfx.Insert(t, "project_plan_phase", testutil.Cols{
		"project_plan_id": planID, "title": "Phase", "position": 0,
	})
	part := func(title string, position int) string {
		return dbfx.Insert(t, "project_plan_part", testutil.Cols{
			"project_plan_id": planID, "project_plan_phase_id": phaseID,
			"title": title, "position": position,
		})
	}
	completePart := part("Complete", 0)
	inProgressPart := part("In progress", 1)
	startedZeroPart := part("Started at zero", 2)
	notStartedPart := part("Not started", 3)
	part("No tasks", 4)
	cancelledPart := part("Cancelled", 5)
	deletedPart := part("Deleted", 6)
	mixedPart := part("Mixed", 7)

	issue := func(title, status string) string {
		return dbfx.Issue(t, title, testutil.Cols{"project_id": projectID, "status": status})
	}
	completeIssue := issue("Complete issue", "done")
	inProgressDone := issue("In-progress completed issue", "done")
	inProgressOpen := issue("In-progress active issue", "in_progress")
	startedZeroIssue := issue("Started zero issue", "in_progress")
	notStartedIssue := issue("Not started issue", "todo")
	cancelledFirst := issue("Cancelled first issue", "cancelled")
	cancelledSecond := issue("Cancelled second issue", "cancelled")
	deletedSingle := issue("Deleted single issue", "todo")
	deletedBatchFirst := issue("Deleted batch first issue", "todo")
	deletedBatchSecond := issue("Deleted batch second issue", "todo")
	mixedLive := issue("Mixed live issue", "todo")
	mixedDeleted := issue("Mixed deleted issue", "todo")

	link := func(partID, issueID string, number int, title string) string {
		return dbfx.Insert(t, "project_plan_part_issue", testutil.Cols{
			"project_plan_id": planID, "project_plan_part_id": partID, "issue_id": issueID,
			"issue_number_snapshot": number, "issue_title_snapshot": title,
		})
	}
	link(completePart, completeIssue, 101, "Complete snapshot")
	link(inProgressPart, inProgressDone, 102, "In-progress done snapshot")
	link(inProgressPart, inProgressOpen, 103, "In-progress active snapshot")
	link(startedZeroPart, startedZeroIssue, 104, "Started zero snapshot")
	link(notStartedPart, notStartedIssue, 105, "Not started snapshot")
	link(cancelledPart, cancelledFirst, 106, "Cancelled first snapshot")
	link(cancelledPart, cancelledSecond, 107, "Cancelled second snapshot")
	singleLinkID := link(deletedPart, deletedSingle, 108, "Deleted single snapshot")
	link(deletedPart, deletedBatchFirst, 109, "Deleted batch first snapshot")
	link(deletedPart, deletedBatchSecond, 110, "Deleted batch second snapshot")
	link(mixedPart, mixedLive, 111, "Mixed live snapshot")
	link(mixedPart, mixedDeleted, 112, "Mixed deleted snapshot")

	deleteRequest := withURLParam(
		newRequest(http.MethodDelete, "/api/issues/"+deletedSingle, nil), "id", deletedSingle,
	)
	testutil.Call(t, testHandler.DeleteIssue, deleteRequest).Want(http.StatusNoContent)

	var singleLive bool
	var singleNumber int
	var singleTitle string
	if err := testPool.QueryRow(ctx, `
		SELECT issue_id IS NOT NULL, issue_number_snapshot, issue_title_snapshot
		FROM project_plan_part_issue WHERE id = $1`, singleLinkID,
	).Scan(&singleLive, &singleNumber, &singleTitle); err != nil {
		t.Fatalf("load single-deleted membership: %v", err)
	}
	if singleLive || singleNumber != 108 || singleTitle != "Deleted single snapshot" {
		t.Fatalf("single-deleted membership = live:%t number:%d title:%q, want false/108/snapshot",
			singleLive, singleNumber, singleTitle)
	}

	batchRequest := newRequest(http.MethodDelete, "/api/issues/batch", map[string]any{
		"issue_ids": []string{deletedBatchFirst, deletedBatchSecond, mixedDeleted},
	})
	testutil.Call(t, testHandler.BatchDeleteIssues, batchRequest).Want(http.StatusOK)

	var retainedLiveLinks int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM project_plan_part_issue
		WHERE issue_id IN ($1, $2, $3, $4)`,
		deletedSingle, deletedBatchFirst, deletedBatchSecond, mixedDeleted,
	).Scan(&retainedLiveLinks); err != nil {
		t.Fatalf("count dangling plan memberships: %v", err)
	}
	if retainedLiveLinks != 0 {
		t.Fatalf("live plan memberships for deleted issues = %d, want 0", retainedLiveLinks)
	}

	request := withURLParam(
		newRequest(http.MethodGet, "/api/projects/"+projectID+"/plan", nil), "id", projectID,
	)
	var overview projectplan.Overview
	testutil.Call(t, testHandler.GetActiveProjectPlan, request).Want(http.StatusOK).JSON(&overview)
	parts := make(map[string]projectplan.Part)
	for _, phase := range overview.Phases {
		for _, planPart := range phase.Parts {
			parts[planPart.Title] = planPart
		}
	}
	assertPart := func(title, state string, rollup projectplan.TaskRollup) projectplan.Part {
		planPart, ok := parts[title]
		if !ok {
			t.Fatalf("missing part %q in overview", title)
		}
		if planPart.CoverageState != state || planPart.Rollup != rollup {
			t.Fatalf("part %q = state:%q rollup:%+v, want %q %+v",
				title, planPart.CoverageState, planPart.Rollup, state, rollup)
		}
		return planPart
	}
	assertPart("Complete", projectplan.CoverageComplete, projectplan.TaskRollup{TasksDone: 1, TasksTotal: 1, Percent: 100})
	assertPart("In progress", projectplan.CoverageInProgress, projectplan.TaskRollup{TasksDone: 1, TasksTotal: 2, Percent: 50})
	assertPart("Started at zero", projectplan.CoverageInProgress, projectplan.TaskRollup{TasksDone: 0, TasksTotal: 1, Percent: 0})
	assertPart("Not started", projectplan.CoverageNotStarted, projectplan.TaskRollup{TasksDone: 0, TasksTotal: 1, Percent: 0})
	assertPart("No tasks", projectplan.CoverageNoTasksYet, projectplan.TaskRollup{})
	assertPart("Cancelled", projectplan.CoverageCoveredNoActiveTasks, projectplan.TaskRollup{})
	deleted := assertPart("Deleted", projectplan.CoverageCoveredNoActiveTasks, projectplan.TaskRollup{})
	mixed := assertPart("Mixed", projectplan.CoverageNotStarted, projectplan.TaskRollup{TasksDone: 0, TasksTotal: 1, Percent: 0})

	var foundDeletedSnapshot bool
	for _, row := range deleted.Issues {
		if row.Deleted && row.Title == "Deleted single snapshot" && row.Number == 108 && row.ID == nil {
			foundDeletedSnapshot = true
		}
	}
	if !foundDeletedSnapshot {
		t.Fatalf("deleted plan rows did not render the retained snapshot: %+v", deleted.Issues)
	}
	if len(mixed.Issues) != 2 || mixed.Issues[0].Deleted == mixed.Issues[1].Deleted {
		t.Fatalf("mixed plan rows = %+v, want one live and one deleted row", mixed.Issues)
	}
}

func TestGetActiveProjectPlanFlagAndResponse(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database unavailable")
	}
	projectID := dbfx.Project(t, "Plan read handler")
	planID := dbfx.Insert(t, "project_plan", testutil.Cols{
		"workspace_id": testWorkspaceID, "project_id": projectID, "version": 1,
		"kind": "prd", "origin": "manual", "title": "Handler overview",
		"created_by_type": "member", "created_by_id": testUserID,
	})
	phaseID := dbfx.Insert(t, "project_plan_phase", testutil.Cols{
		"project_plan_id": planID, "title": "Phase", "position": 0,
	})
	partID := dbfx.Insert(t, "project_plan_part", testutil.Cols{
		"project_plan_id": planID, "project_plan_phase_id": phaseID,
		"title": "Gap", "position": 0,
	})

	request := func() *http.Request {
		return withURLParam(
			newRequest(http.MethodGet, "/api/projects/"+projectID+"/plan", nil),
			"id", projectID,
		)
	}

	withFeatureFlag(t, testHandler, featureflags.ProjectPlans, false)
	testutil.Call(t, testHandler.GetActiveProjectPlan, request()).Want(http.StatusNotFound)

	withFeatureFlag(t, testHandler, featureflags.ProjectPlans, true)
	var overview projectplan.Overview
	testutil.Call(t, testHandler.GetActiveProjectPlan, request()).Want(http.StatusOK).JSON(&overview)
	if overview.Plan.ID != planID || overview.Plan.ProjectID != projectID {
		t.Fatalf("plan = %+v, want plan %s in project %s", overview.Plan, planID, projectID)
	}
	if overview.Rollup.PartsTotal != 1 || overview.Rollup.PartsWithoutTasks != 1 {
		t.Fatalf("rollup = %+v, want one uncovered part", overview.Rollup)
	}
	if len(overview.UncoveredParts) != 1 || overview.UncoveredParts[0].ID != partID {
		t.Fatalf("uncovered_parts = %+v, want %s", overview.UncoveredParts, partID)
	}
}

func TestGetProjectPlanScopesRetainedVersionToProject(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database unavailable")
	}
	projectID := dbfx.Project(t, "Plan read owner")
	otherProjectID := dbfx.Project(t, "Other plan owner")
	planID := dbfx.Insert(t, "project_plan", testutil.Cols{
		"workspace_id": testWorkspaceID, "project_id": projectID, "version": 1,
		"kind": "prd", "origin": "manual", "title": "Retained overview",
		"created_by_type": "member", "created_by_id": testUserID,
		"superseded_at": testutil.Raw("now()"),
	})
	withFeatureFlag(t, testHandler, featureflags.ProjectPlans, true)

	request := func(targetProjectID, targetPlanID string) *http.Request {
		req := newRequest(http.MethodGet,
			"/api/projects/"+targetProjectID+"/plans/"+targetPlanID, nil)
		return withURLParams(req, "id", targetProjectID, "planId", targetPlanID)
	}

	var overview projectplan.Overview
	testutil.Call(t, testHandler.GetProjectPlan, request(projectID, planID)).Want(http.StatusOK).JSON(&overview)
	if overview.Plan.ID != planID || !overview.Plan.Superseded {
		t.Fatalf("retained plan = %+v", overview.Plan)
	}
	testutil.Call(t, testHandler.GetProjectPlan, request(otherProjectID, planID)).Want(http.StatusNotFound)
	testutil.Call(t, testHandler.GetProjectPlan, request(projectID, "not-a-uuid")).Want(http.StatusBadRequest)
}
