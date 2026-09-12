package projectplan

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestReaderRollupsMidPlanGapAndCoverageStates(t *testing.T) {
	fixture := newPlanTestFixture(t)
	planID := fixture.createManual(t, "Overview plan")
	foundationsID := fixture.addPhase(t, planID, "Foundations", 0)
	gapPhaseID := fixture.addPhase(t, planID, "Marketplace gap", 1)
	fixture.addPhase(t, planID, "Empty manual phase", 2)
	laterPhaseID := fixture.addPhase(t, planID, "Rollout", 3)

	completePartID := fixture.addPart(t, planID, foundationsID, "Complete", 0)
	gapPartID := fixture.addPart(t, planID, gapPhaseID, "No tasks yet", 0)
	inProgressPartID := fixture.addPart(t, planID, laterPhaseID, "In progress", 0)
	notStartedPartID := fixture.addPart(t, planID, laterPhaseID, "Not started", 1)
	inactivePartID := fixture.addPart(t, planID, laterPhaseID, "Covered, no active tasks", 2)

	completeIssueID := fixture.issue(t, "Complete issue", "")
	inProgressDoneID := fixture.issue(t, "Completed slice", "")
	inProgressOpenID := fixture.issue(t, "Active slice", "")
	notStartedIssueID := fixture.issue(t, "Queued slice", "")
	cancelledIssueID := fixture.issue(t, "Cancelled slice", "")
	deletedIssueID := fixture.issue(t, "Deleted slice", "")

	setIssueStatus(t, fixture, completeIssueID, "done")
	setIssueStatus(t, fixture, inProgressDoneID, "done")
	setIssueStatus(t, fixture, inProgressOpenID, "in_progress")
	setIssueStatus(t, fixture, cancelledIssueID, "cancelled")

	linkIssue(t, fixture, planID, completePartID, completeIssueID)
	linkIssue(t, fixture, planID, inProgressPartID, inProgressDoneID)
	linkIssue(t, fixture, planID, inProgressPartID, inProgressOpenID)
	linkIssue(t, fixture, planID, notStartedPartID, notStartedIssueID)
	linkIssue(t, fixture, planID, inactivePartID, cancelledIssueID)
	linkIssue(t, fixture, planID, inactivePartID, deletedIssueID)
	if _, err := fixture.pool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, deletedIssueID); err != nil {
		t.Fatalf("delete linked issue: %v", err)
	}

	queries := db.New(fixture.pool)
	if _, err := queries.CreateProjectPlanDependency(context.Background(), db.CreateProjectPlanDependencyParams{
		ProjectPlanID: planID, BlockedPhaseID: laterPhaseID, BlockingPartID: completePartID,
	}); err != nil {
		t.Fatalf("create phase-to-part dependency: %v", err)
	}
	if _, err := queries.CreateProjectPlanDependency(context.Background(), db.CreateProjectPlanDependencyParams{
		ProjectPlanID: planID, BlockedPartID: inProgressPartID, BlockingPhaseID: gapPhaseID,
	}); err != nil {
		t.Fatalf("create part-to-phase dependency: %v", err)
	}

	overview, err := NewReader(queries).ReadActive(
		context.Background(), fixture.workspaceID, fixture.projectID,
	)
	if err != nil {
		t.Fatalf("ReadActive: %v", err)
	}

	if got, want := overview.Rollup, (Rollup{
		TasksDone: 2, TasksTotal: 4, Percent: 50,
		PartsCovered: 4, PartsTotal: 5, PartsWithoutTasks: 1,
	}); got != want {
		t.Fatalf("plan rollup = %+v, want %+v", got, want)
	}
	if len(overview.Phases) != 4 {
		t.Fatalf("phases = %d, want 4 (including the empty phase)", len(overview.Phases))
	}
	if got := overview.Phases[1].Rollup; got != (TaskRollup{}) {
		t.Fatalf("mid-plan gap phase rollup = %+v, want zero", got)
	}
	if got := overview.Phases[3].Rollup; got != (TaskRollup{TasksDone: 1, TasksTotal: 3, Percent: 33}) {
		t.Fatalf("later phase rollup = %+v, want 1/3 (33%%)", got)
	}

	states := make(map[string]string)
	for _, phase := range overview.Phases {
		for _, part := range phase.Parts {
			states[part.ID] = part.CoverageState
		}
	}
	for partID, want := range map[string]string{
		uuidString(completePartID):   CoverageComplete,
		uuidString(gapPartID):        CoverageNoTasksYet,
		uuidString(inProgressPartID): CoverageInProgress,
		uuidString(notStartedPartID): CoverageNotStarted,
		uuidString(inactivePartID):   CoverageCoveredNoActiveTasks,
	} {
		if got := states[partID]; got != want {
			t.Errorf("part %s coverage = %q, want %q", partID, got, want)
		}
	}

	if len(overview.UncoveredParts) != 1 || overview.UncoveredParts[0].ID != uuidString(gapPartID) {
		t.Fatalf("uncovered parts = %+v, want only the mid-plan gap", overview.UncoveredParts)
	}
	if len(overview.Dependencies) != 2 {
		t.Fatalf("dependencies = %d, want 2", len(overview.Dependencies))
	}
	if overview.Dependencies[0].Blocked.Title == "" || overview.Dependencies[0].Blocking.Title == "" ||
		overview.Dependencies[1].Blocked.Title == "" || overview.Dependencies[1].Blocking.Title == "" {
		t.Fatalf("dependency endpoint labels are incomplete: %+v", overview.Dependencies)
	}

	inactivePart := findPart(t, overview, inactivePartID)
	if inactivePart.Rollup != (TaskRollup{}) || len(inactivePart.Issues) != 2 {
		t.Fatalf("inactive part = %+v, want zero rollup and two historical rows", inactivePart)
	}
	deletedRows := 0
	for _, issue := range inactivePart.Issues {
		if issue.Deleted {
			deletedRows++
			if issue.ID != nil || issue.Status != "deleted" || issue.StatusCategory != "deleted" {
				t.Errorf("deleted issue detail = %+v", issue)
			}
		}
	}
	if deletedRows != 1 {
		t.Fatalf("deleted issue rows = %d, want 1", deletedRows)
	}
}

func TestReaderSupersededPlanUsesLiveIssueStatus(t *testing.T) {
	fixture := newPlanTestFixture(t)
	oldPlanID := fixture.createManual(t, "Version one")
	phaseID := fixture.addPhase(t, oldPlanID, "Build", 0)
	partID := fixture.addPart(t, oldPlanID, phaseID, "Read path", 0)
	issueID := fixture.issue(t, "Read API", "")
	linkIssue(t, fixture, oldPlanID, partID, issueID)

	if _, err := fixture.service.Supersede(context.Background(), SupersedeParams{
		WorkspaceID: fixture.workspaceID, PlanID: oldPlanID, CreatedBy: fixture.actor(),
	}); err != nil {
		t.Fatalf("Supersede: %v", err)
	}
	setIssueStatus(t, fixture, issueID, "done")

	overview, err := NewReader(db.New(fixture.pool)).Read(
		context.Background(), fixture.workspaceID, fixture.projectID, oldPlanID,
	)
	if err != nil {
		t.Fatalf("Read superseded plan: %v", err)
	}
	if !overview.Plan.Superseded {
		t.Fatal("old plan is not marked superseded")
	}
	if got := overview.Rollup; got.TasksDone != 1 || got.TasksTotal != 1 || got.Percent != 100 {
		t.Fatalf("superseded plan rollup = %+v, want live 1/1", got)
	}
	issue := findPart(t, overview, partID).Issues[0]
	if issue.Status != "done" || issue.StatusCategory != "done" {
		t.Fatalf("superseded plan issue = %+v, want current done status", issue)
	}
}

func linkIssue(t *testing.T, fixture *planTestFixture, planID, partID, issueID pgtype.UUID) {
	t.Helper()
	if _, err := fixture.service.LinkIssue(
		context.Background(), fixture.workspaceID, planID, partID, issueID,
	); err != nil {
		t.Fatalf("LinkIssue: %v", err)
	}
}

func setIssueStatus(t *testing.T, fixture *planTestFixture, issueID pgtype.UUID, status string) {
	t.Helper()
	if _, err := fixture.pool.Exec(
		context.Background(), `UPDATE issue SET status = $2 WHERE id = $1`, issueID, status,
	); err != nil {
		t.Fatalf("set issue status to %q: %v", status, err)
	}
}

func setIssueParent(t *testing.T, fixture *planTestFixture, issueID, parentID pgtype.UUID) {
	t.Helper()
	if _, err := fixture.pool.Exec(
		context.Background(), `UPDATE issue SET parent_issue_id = $2 WHERE id = $1`, issueID, parentID,
	); err != nil {
		t.Fatalf("set parent of issue %s: %v", uuidString(issueID), err)
	}
}

func findPart(t *testing.T, overview Overview, partID pgtype.UUID) Part {
	t.Helper()
	wantID := uuidString(partID)
	for _, phase := range overview.Phases {
		for _, part := range phase.Parts {
			if part.ID == wantID {
				return part
			}
		}
	}
	t.Fatalf("part %s not found", wantID)
	return Part{}
}

func TestReaderRollupsSubtreeSurvivesParentCycle(t *testing.T) {
	fixture := newPlanTestFixture(t)
	planID := fixture.createManual(t, "Cycle plan")
	phaseID := fixture.addPhase(t, planID, "Cycle", 0)
	partID := fixture.addPart(t, planID, phaseID, "Cyclic", 0)

	aID := fixture.issue(t, "Cycle A", "")
	bID := fixture.issue(t, "Cycle B", "")
	cID := fixture.issue(t, "Self loop", "")
	// Corrupt the hierarchy on purpose: A <-> B is a two-node cycle and C is
	// its own parent. The rollup must terminate and count each issue once.
	setIssueParent(t, fixture, bID, aID)
	setIssueParent(t, fixture, aID, bID)
	setIssueParent(t, fixture, cID, cID)
	linkIssue(t, fixture, planID, partID, aID)
	linkIssue(t, fixture, planID, partID, cID)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	queries := db.New(fixture.pool)
	rows, err := queries.ListProjectPlanRollups(ctx, db.ListProjectPlanRollupsParams{
		ProjectPlanID: planID, WorkspaceID: fixture.workspaceID, ProjectID: fixture.projectID,
	})
	if err != nil {
		t.Fatalf("ListProjectPlanRollups with cyclic parents: %v", err)
	}
	var total, done, started, membership int64
	for _, row := range rows {
		if row.PartID.Valid && uuidString(row.PartID) == uuidString(partID) {
			total, done, started, membership = row.PartTasksTotal, row.PartTasksDone, row.PartTasksStarted, row.PartMembershipRows
		}
	}
	if membership != 2 || total != 3 || done != 0 || started != 0 {
		t.Fatalf("cyclic part = membership:%d total:%d done:%d started:%d, want 2 3 0 0",
			membership, total, done, started)
	}
}

func TestReaderRollupsCountLinkedSubtree(t *testing.T) {
	fixture := newPlanTestFixture(t)
	planID := fixture.createManual(t, "Subtree plan")
	phaseID := fixture.addPhase(t, planID, "Build", 0)

	epicPartID := fixture.addPart(t, planID, phaseID, "Epic", 0)
	grandchildPartID := fixture.addPart(t, planID, phaseID, "Grandchildren", 1)
	cancelledPartID := fixture.addPart(t, planID, phaseID, "Cancelled excluded", 2)
	dedupePartID := fixture.addPart(t, planID, phaseID, "Parent and child", 3)
	otherProjectPartID := fixture.addPart(t, planID, phaseID, "Other project", 4)
	flipPartID := fixture.addPart(t, planID, phaseID, "Flip", 5)
	unlinkedPartID := fixture.addPart(t, planID, phaseID, "Unlinked", 6)

	// Epic + 12 done sub-issues, epic still open: 12/13, in_progress.
	epicID := fixture.issue(t, "Epic", "")
	for i := 1; i <= 12; i++ {
		childID := fixture.issue(t, fmt.Sprintf("Slice %d", i), "")
		setIssueParent(t, fixture, childID, epicID)
		setIssueStatus(t, fixture, childID, "done")
	}
	linkIssue(t, fixture, planID, epicPartID, epicID)

	// Three levels deep: the grandchild counts toward the rollup.
	chainEpicID := fixture.issue(t, "Chain epic", "")
	chainChildID := fixture.issue(t, "Chain child", "")
	chainGrandchildID := fixture.issue(t, "Chain grandchild", "")
	setIssueParent(t, fixture, chainChildID, chainEpicID)
	setIssueParent(t, fixture, chainGrandchildID, chainChildID)
	setIssueStatus(t, fixture, chainEpicID, "in_progress")
	setIssueStatus(t, fixture, chainChildID, "done")
	setIssueStatus(t, fixture, chainGrandchildID, "done")
	linkIssue(t, fixture, planID, grandchildPartID, chainEpicID)

	// Cancelled descendants drop out of the total at every depth, but their
	// live descendants are still traversed and counted.
	cancelEpicID := fixture.issue(t, "Cancel epic", "")
	cancelDoneID := fixture.issue(t, "Cancel live child", "")
	cancelCancelledID := fixture.issue(t, "Cancel cancelled child", "")
	cancelGrandchildID := fixture.issue(t, "Cancel grandchild under cancelled child", "")
	setIssueParent(t, fixture, cancelDoneID, cancelEpicID)
	setIssueParent(t, fixture, cancelCancelledID, cancelEpicID)
	setIssueParent(t, fixture, cancelGrandchildID, cancelCancelledID)
	setIssueStatus(t, fixture, cancelDoneID, "done")
	setIssueStatus(t, fixture, cancelCancelledID, "cancelled")
	setIssueStatus(t, fixture, cancelGrandchildID, "done")
	linkIssue(t, fixture, planID, cancelledPartID, cancelEpicID)

	// Linking both an issue and its own child counts the child once.
	dedupeEpicID := fixture.issue(t, "Dedupe epic", "")
	dedupeChildID := fixture.issue(t, "Dedupe child", "")
	setIssueParent(t, fixture, dedupeChildID, dedupeEpicID)
	setIssueStatus(t, fixture, dedupeEpicID, "done")
	setIssueStatus(t, fixture, dedupeChildID, "done")
	linkIssue(t, fixture, planID, dedupePartID, dedupeEpicID)
	linkIssue(t, fixture, planID, dedupePartID, dedupeChildID)

	// A child in another project stays out of the rollup.
	var otherProjectID string
	if err := fixture.pool.QueryRow(context.Background(),
		`INSERT INTO project (workspace_id, title, description, status, priority)
		 VALUES ($1, 'Other Project', '', 'planned', 'none') RETURNING id`,
		fixture.workspaceID,
	).Scan(&otherProjectID); err != nil {
		t.Fatalf("seed second project: %v", err)
	}
	isoEpicID := fixture.issue(t, "Isolation epic", "")
	isoChildID := fixture.issue(t, "Isolation child", "")
	setIssueParent(t, fixture, isoChildID, isoEpicID)
	setIssueStatus(t, fixture, isoChildID, "done")
	if _, err := fixture.pool.Exec(context.Background(),
		`UPDATE issue SET project_id = $2 WHERE id = $1`, isoChildID, otherProjectID,
	); err != nil {
		t.Fatalf("move child to other project: %v", err)
	}
	linkIssue(t, fixture, planID, otherProjectPartID, isoEpicID)

	// Epic still todo, but its in-flight child flips the part to in_progress.
	flipEpicID := fixture.issue(t, "Flip epic", "")
	flipActiveID := fixture.issue(t, "Flip active child", "")
	flipBacklogID := fixture.issue(t, "Flip backlog child", "")
	setIssueParent(t, fixture, flipActiveID, flipEpicID)
	setIssueParent(t, fixture, flipBacklogID, flipEpicID)
	setIssueStatus(t, fixture, flipActiveID, "in_progress")
	setIssueStatus(t, fixture, flipBacklogID, "backlog")
	linkIssue(t, fixture, planID, flipPartID, flipEpicID)

	overview, err := NewReader(db.New(fixture.pool)).ReadActive(
		context.Background(), fixture.workspaceID, fixture.projectID,
	)
	if err != nil {
		t.Fatalf("ReadActive: %v", err)
	}

	if got, want := overview.Rollup, (Rollup{
		TasksDone: 18, TasksTotal: 25, Percent: 72,
		PartsCovered: 6, PartsTotal: 7, PartsWithoutTasks: 1,
	}); got != want {
		t.Fatalf("plan rollup = %+v, want %+v", got, want)
	}
	if got := overview.Phases[0].Rollup; got != (TaskRollup{TasksDone: 18, TasksTotal: 25, Percent: 72}) {
		t.Fatalf("phase rollup = %+v, want task-weighted 18/25", got)
	}

	wantRollups := map[string]TaskRollup{
		uuidString(epicPartID):         {TasksDone: 12, TasksTotal: 13, Percent: 92},
		uuidString(grandchildPartID):   {TasksDone: 2, TasksTotal: 3, Percent: 67},
		uuidString(cancelledPartID):    {TasksDone: 2, TasksTotal: 3, Percent: 67},
		uuidString(dedupePartID):       {TasksDone: 2, TasksTotal: 2, Percent: 100},
		uuidString(otherProjectPartID): {TasksDone: 0, TasksTotal: 1, Percent: 0},
		uuidString(flipPartID):         {TasksDone: 0, TasksTotal: 3, Percent: 0},
		uuidString(unlinkedPartID):     TaskRollup{},
	}
	wantStates := map[string]string{
		uuidString(epicPartID):         CoverageInProgress,
		uuidString(grandchildPartID):   CoverageInProgress,
		uuidString(cancelledPartID):    CoverageInProgress,
		uuidString(dedupePartID):       CoverageComplete,
		uuidString(otherProjectPartID): CoverageNotStarted,
		uuidString(flipPartID):         CoverageInProgress,
		uuidString(unlinkedPartID):     CoverageNoTasksYet,
	}
	for _, phase := range overview.Phases {
		for _, part := range phase.Parts {
			if want, ok := wantRollups[part.ID]; ok && part.Rollup != want {
				t.Errorf("part %q rollup = %+v, want %+v", part.Title, part.Rollup, want)
			}
			if want, ok := wantStates[part.ID]; ok && part.CoverageState != want {
				t.Errorf("part %q coverage = %q, want %q", part.Title, part.CoverageState, want)
			}
		}
	}
}
