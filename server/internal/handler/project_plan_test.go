package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/projectplan"
	"github.com/multica-ai/multica/server/internal/testutil"
)

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
