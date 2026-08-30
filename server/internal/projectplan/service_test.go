package projectplan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

type planTestFixture struct {
	service     *Service
	pool        *pgxpool.Pool
	workspaceID pgtype.UUID
	projectID   pgtype.UUID
	userID      pgtype.UUID
}

func newPlanTestFixture(t *testing.T) *planTestFixture {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v", err)
	}
	var schemaReady bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('project_plan') IS NOT NULL`).Scan(&schemaReady); err != nil || !schemaReady {
		pool.Close()
		t.Skip("project plan migrations are not applied")
	}
	t.Cleanup(pool.Close)

	suffix := time.Now().UnixNano()
	var userID, workspaceID, projectID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ('Plan Writer', $1) RETURNING id`,
		fmt.Sprintf("plan-writer-%d@invalid.test", suffix),
	).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspace (name, slug) VALUES ('Plan Test', $1) RETURNING id`,
		fmt.Sprintf("plan-test-%d", suffix),
	).Scan(&workspaceID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		workspaceID, userID,
	); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO project (workspace_id, title, description, status, priority)
		 VALUES ($1, 'Plan Project', '', 'planned', 'none') RETURNING id`, workspaceID,
	).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		cleanup := func(name, query string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(cleanupCtx, query, args...); err != nil {
				t.Errorf("cleanup %s: %v", name, err)
			}
		}
		cleanup("project plan dependencies", `DELETE FROM project_plan_dependency WHERE project_plan_id IN (SELECT id FROM project_plan WHERE workspace_id = $1)`, workspaceID)
		cleanup("project plan issue links", `DELETE FROM project_plan_part_issue WHERE project_plan_id IN (SELECT id FROM project_plan WHERE workspace_id = $1)`, workspaceID)
		cleanup("project plan parts", `DELETE FROM project_plan_part WHERE project_plan_id IN (SELECT id FROM project_plan WHERE workspace_id = $1)`, workspaceID)
		cleanup("project plan phases", `DELETE FROM project_plan_phase WHERE project_plan_id IN (SELECT id FROM project_plan WHERE workspace_id = $1)`, workspaceID)
		cleanup("project plans", `DELETE FROM project_plan WHERE workspace_id = $1`, workspaceID)
		cleanup("issues", `DELETE FROM issue WHERE workspace_id = $1`, workspaceID)
		cleanup("project", `DELETE FROM project WHERE id = $1`, projectID)
		cleanup("members", `DELETE FROM member WHERE workspace_id = $1`, workspaceID)
		cleanup("workspace", `DELETE FROM workspace WHERE id = $1`, workspaceID)
		cleanup("user", `DELETE FROM "user" WHERE id = $1`, userID)
	})

	provider := featureflag.NewStaticProvider()
	provider.Set(featureflags.ProjectPlans, featureflag.Rule{Default: true})
	flags := featureflag.NewService(provider)
	workspaceUUID := parsePlanTestUUID(t, workspaceID)
	projectUUID := parsePlanTestUUID(t, projectID)
	userUUID := parsePlanTestUUID(t, userID)
	return &planTestFixture{
		service:     NewService(NewRepository(pool), pool, flags),
		pool:        pool,
		workspaceID: workspaceUUID,
		projectID:   projectUUID,
		userID:      userUUID,
	}
}

func (f *planTestFixture) actor() Actor {
	return Actor{Type: "member", ID: f.userID}
}

func (f *planTestFixture) createManual(t *testing.T, title string) pgtype.UUID {
	t.Helper()
	plan, err := f.service.CreateManual(context.Background(), CreateManualParams{
		WorkspaceID: f.workspaceID, ProjectID: f.projectID, Kind: "prd",
		Title: title, Description: "Plan body", CreatedBy: f.actor(),
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	return plan.ID
}

func (f *planTestFixture) addPhase(t *testing.T, planID pgtype.UUID, title string, position int32) pgtype.UUID {
	t.Helper()
	phase, err := f.service.AddPhase(context.Background(), CreatePhaseParams{
		WorkspaceID: f.workspaceID, PlanID: planID, Title: title, Position: position,
	})
	if err != nil {
		t.Fatalf("AddPhase: %v", err)
	}
	return phase.ID
}

func (f *planTestFixture) addPart(
	t *testing.T,
	planID pgtype.UUID,
	phaseID pgtype.UUID,
	title string,
	position int32,
) pgtype.UUID {
	t.Helper()
	part, err := f.service.AddPart(context.Background(), CreatePartParams{
		WorkspaceID: f.workspaceID, PlanID: planID, PhaseID: phaseID,
		Title: title, AcceptanceCriteria: "It works", Position: position,
	})
	if err != nil {
		t.Fatalf("AddPart: %v", err)
	}
	return part.ID
}

func (f *planTestFixture) issue(t *testing.T, title, description string) pgtype.UUID {
	t.Helper()
	var issueID string
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO issue (
			workspace_id, project_id, title, description, status, priority,
			creator_type, creator_id, position, number
		) VALUES (
			$1, $2, $3, $4, 'todo', 'none', 'member', $5, 0,
			(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1)
		) RETURNING id`,
		f.workspaceID, f.projectID, title, description, f.userID,
	).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	return parsePlanTestUUID(t, issueID)
}

func TestServiceDefaultsOff(t *testing.T) {
	service := &Service{}
	_, err := service.CreateManual(context.Background(), CreateManualParams{})
	if got := planErrorKind(t, err); got != ErrorDisabled {
		t.Fatalf("error kind = %q, want %q", got, ErrorDisabled)
	}
}

func TestCreateFromIssueCapturesDescriptionProvenance(t *testing.T) {
	fixture := newPlanTestFixture(t)
	issueID := fixture.issue(t, "Source PRD", "Exact issue description")

	plan, err := fixture.service.CreateFromIssue(context.Background(), CreateFromIssueParams{
		WorkspaceID: fixture.workspaceID, ProjectID: fixture.projectID,
		SourceIssueID: issueID, Kind: "prd", CreatedBy: fixture.actor(),
	})
	if err != nil {
		t.Fatalf("CreateFromIssue: %v", err)
	}
	wantDigest := sha256.Sum256([]byte("Exact issue description"))
	if plan.Origin != "issue" || plan.Title != "Source PRD" || plan.Description != "Exact issue description" {
		t.Fatalf("ingested plan = %+v", plan)
	}
	if plan.SourceIssueID != issueID || !plan.SourceIssueRevision.Valid || plan.SourceIssueRevision.Int64 < 1 {
		t.Fatalf("source identity/revision not captured: %+v", plan)
	}
	if plan.SourceDescriptionSnapshot.String != "Exact issue description" ||
		plan.SourceContentSha256.String != hex.EncodeToString(wantDigest[:]) {
		t.Fatalf("source snapshot/digest not captured: %+v", plan)
	}
}

func TestCreateManualUsesNullProvenance(t *testing.T) {
	fixture := newPlanTestFixture(t)
	planID := fixture.createManual(t, "Hand-authored PRD")

	var origin string
	var sourceIssueID, revision, snapshot, digest any
	if err := fixture.pool.QueryRow(context.Background(),
		`SELECT origin, source_issue_id, source_issue_revision,
		        source_description_snapshot, source_content_sha256
		 FROM project_plan WHERE id = $1`, planID,
	).Scan(&origin, &sourceIssueID, &revision, &snapshot, &digest); err != nil {
		t.Fatalf("load manual provenance: %v", err)
	}
	if origin != "manual" || sourceIssueID != nil || revision != nil || snapshot != nil || digest != nil {
		t.Fatalf("manual provenance = (%q, %v, %v, %v, %v), want all source values null",
			origin, sourceIssueID, revision, snapshot, digest)
	}
}

func TestSecondActivePlanReturnsTypedError(t *testing.T) {
	fixture := newPlanTestFixture(t)
	fixture.createManual(t, "First")

	_, err := fixture.service.CreateManual(context.Background(), CreateManualParams{
		WorkspaceID: fixture.workspaceID, ProjectID: fixture.projectID, Kind: "prd",
		Title: "Second", CreatedBy: fixture.actor(),
	})
	if got := planErrorKind(t, err); got != ErrorActivePlanExists {
		t.Fatalf("error kind = %q, want %q", got, ErrorActivePlanExists)
	}
}

func TestSupersedeClonesStructureAndKeepsPriorMappings(t *testing.T) {
	fixture := newPlanTestFixture(t)
	oldPlanID := fixture.createManual(t, "Version one")
	phaseID := fixture.addPhase(t, oldPlanID, "Build", 0)
	partID := fixture.addPart(t, oldPlanID, phaseID, "Write path", 0)
	issueID := fixture.issue(t, "Implement write path", "Ship it")
	if _, err := fixture.service.LinkIssue(context.Background(), fixture.workspaceID, oldPlanID, partID, issueID); err != nil {
		t.Fatalf("LinkIssue: %v", err)
	}

	newTitle := "Version two"
	newPlan, err := fixture.service.Supersede(context.Background(), SupersedeParams{
		WorkspaceID: fixture.workspaceID, PlanID: oldPlanID,
		Patch: PlanPatch{Title: &newTitle}, CreatedBy: fixture.actor(),
	})
	if err != nil {
		t.Fatalf("Supersede: %v", err)
	}
	if newPlan.Version != 2 || newPlan.Title != newTitle || newPlan.ID == oldPlanID {
		t.Fatalf("new plan = %+v", newPlan)
	}
	var oldSuperseded bool
	if err := fixture.pool.QueryRow(context.Background(),
		`SELECT superseded_at IS NOT NULL FROM project_plan WHERE id = $1`, oldPlanID,
	).Scan(&oldSuperseded); err != nil {
		t.Fatalf("load old plan: %v", err)
	}
	if !oldSuperseded {
		t.Fatal("old plan is still active")
	}
	for _, planID := range []pgtype.UUID{oldPlanID, newPlan.ID} {
		var links int
		if err := fixture.pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM project_plan_part_issue WHERE project_plan_id = $1 AND issue_id = $2`,
			planID, issueID,
		).Scan(&links); err != nil {
			t.Fatalf("count links: %v", err)
		}
		if links != 1 {
			t.Fatalf("plan %v has %d links, want 1", planID, links)
		}
	}
	if _, err := fixture.service.UpdatePlan(context.Background(), fixture.workspaceID, oldPlanID, PlanPatch{Title: &newTitle}); planErrorKind(t, err) != ErrorNotActive {
		t.Fatalf("editing superseded plan error = %v", err)
	}
}

func TestDeleteImpactAndDeleteLeaveIssuesIntact(t *testing.T) {
	fixture := newPlanTestFixture(t)
	planID := fixture.createManual(t, "Disposable plan")
	phaseID := fixture.addPhase(t, planID, "Phase", 0)
	partID := fixture.addPart(t, planID, phaseID, "Part", 0)
	issueID := fixture.issue(t, "Surviving issue", "Must remain")
	if _, err := fixture.service.LinkIssue(context.Background(), fixture.workspaceID, planID, partID, issueID); err != nil {
		t.Fatalf("LinkIssue: %v", err)
	}

	impact, err := fixture.service.DeleteImpact(context.Background(), fixture.workspaceID, planID)
	if err != nil {
		t.Fatalf("DeleteImpact: %v", err)
	}
	if impact.MembershipRows != 1 || impact.LiveIssuesUnlinked != 1 {
		t.Fatalf("impact = %+v, want 1/1", impact)
	}
	deletedImpact, err := fixture.service.DeletePlan(context.Background(), fixture.workspaceID, planID)
	if err != nil {
		t.Fatalf("DeletePlan: %v", err)
	}
	if deletedImpact != impact {
		t.Fatalf("delete impact = %+v, preview = %+v", deletedImpact, impact)
	}

	var issueCount, planRows int
	if err := fixture.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM issue WHERE id = $1`, issueID).Scan(&issueCount); err != nil {
		t.Fatalf("count issue: %v", err)
	}
	if issueCount != 1 {
		t.Fatalf("issue count = %d, want 1", issueCount)
	}
	if err := fixture.pool.QueryRow(context.Background(),
		`SELECT
			(SELECT COUNT(*) FROM project_plan WHERE id = $1) +
			(SELECT COUNT(*) FROM project_plan_phase WHERE project_plan_id = $1) +
			(SELECT COUNT(*) FROM project_plan_part WHERE project_plan_id = $1) +
			(SELECT COUNT(*) FROM project_plan_part_issue WHERE project_plan_id = $1) +
			(SELECT COUNT(*) FROM project_plan_dependency WHERE project_plan_id = $1)`, planID,
	).Scan(&planRows); err != nil {
		t.Fatalf("count plan structure: %v", err)
	}
	if planRows != 0 {
		t.Fatalf("plan structure retained %d rows", planRows)
	}
}

func TestFineGrainedEditsReorderAndUnlink(t *testing.T) {
	fixture := newPlanTestFixture(t)
	planID := fixture.createManual(t, "Editable")
	firstPhase := fixture.addPhase(t, planID, "First", 0)
	secondPhase := fixture.addPhase(t, planID, "Second", 1)
	firstPart := fixture.addPart(t, planID, firstPhase, "First part", 0)
	secondPart := fixture.addPart(t, planID, firstPhase, "Second part", 1)

	planDescription := "Edited plan description"
	if _, err := fixture.service.UpdatePlan(context.Background(), fixture.workspaceID, planID,
		PlanPatch{Description: &planDescription}); err != nil {
		t.Fatalf("UpdatePlan: %v", err)
	}
	phaseDescription := "Edited phase description"
	if _, err := fixture.service.UpdatePhase(context.Background(), fixture.workspaceID, planID, firstPhase,
		PhasePatch{Description: &phaseDescription}); err != nil {
		t.Fatalf("UpdatePhase: %v", err)
	}
	acceptance := "Exact acceptance"
	if _, err := fixture.service.UpdatePart(context.Background(), fixture.workspaceID, planID, firstPart,
		PartPatch{AcceptanceCriteria: &acceptance}); err != nil {
		t.Fatalf("UpdatePart: %v", err)
	}
	if err := fixture.service.ReorderPhases(context.Background(), fixture.workspaceID, planID,
		[]pgtype.UUID{secondPhase, firstPhase}); err != nil {
		t.Fatalf("ReorderPhases: %v", err)
	}
	if err := fixture.service.ReorderParts(context.Background(), fixture.workspaceID, planID, firstPhase,
		[]pgtype.UUID{secondPart, firstPart}); err != nil {
		t.Fatalf("ReorderParts: %v", err)
	}

	issueID := fixture.issue(t, "Temporary link", "")
	if _, err := fixture.service.LinkIssue(context.Background(), fixture.workspaceID, planID, firstPart, issueID); err != nil {
		t.Fatalf("LinkIssue: %v", err)
	}
	if err := fixture.service.UnlinkIssue(context.Background(), fixture.workspaceID, planID, firstPart, issueID); err != nil {
		t.Fatalf("UnlinkIssue: %v", err)
	}
	if impact, err := fixture.service.DeleteImpact(context.Background(), fixture.workspaceID, planID); err != nil {
		t.Fatalf("DeleteImpact: %v", err)
	} else if impact.LiveIssuesUnlinked != 0 {
		t.Fatalf("live issues after unlink = %d, want 0", impact.LiveIssuesUnlinked)
	}

	var phase0, phase1, part0, part1 int32
	if err := fixture.pool.QueryRow(context.Background(), `SELECT position FROM project_plan_phase WHERE id = $1`, secondPhase).Scan(&phase0); err != nil {
		t.Fatalf("load second phase: %v", err)
	}
	if err := fixture.pool.QueryRow(context.Background(), `SELECT position FROM project_plan_phase WHERE id = $1`, firstPhase).Scan(&phase1); err != nil {
		t.Fatalf("load first phase: %v", err)
	}
	if err := fixture.pool.QueryRow(context.Background(), `SELECT position FROM project_plan_part WHERE id = $1`, secondPart).Scan(&part0); err != nil {
		t.Fatalf("load second part: %v", err)
	}
	if err := fixture.pool.QueryRow(context.Background(), `SELECT position FROM project_plan_part WHERE id = $1`, firstPart).Scan(&part1); err != nil {
		t.Fatalf("load first part: %v", err)
	}
	if phase0 != 0 || phase1 != 1 || part0 != 0 || part1 != 1 {
		t.Fatalf("positions = phases(%d,%d) parts(%d,%d), want (0,1) and (0,1)", phase0, phase1, part0, part1)
	}
}

func TestDeletePhaseAndPartPerformApplicationCascades(t *testing.T) {
	fixture := newPlanTestFixture(t)
	planID := fixture.createManual(t, "Cascade plan")
	phaseID := fixture.addPhase(t, planID, "Delete me", 0)
	keptPhaseID := fixture.addPhase(t, planID, "Keep me", 1)
	partID := fixture.addPart(t, planID, phaseID, "Delete with phase", 0)
	keptPartID := fixture.addPart(t, planID, keptPhaseID, "Delete directly", 0)
	issueID := fixture.issue(t, "Cascade survivor", "")
	directIssueID := fixture.issue(t, "Direct cascade survivor", "")
	if _, err := fixture.service.LinkIssue(context.Background(), fixture.workspaceID, planID, partID, issueID); err != nil {
		t.Fatalf("LinkIssue: %v", err)
	}
	if _, err := fixture.service.LinkIssue(context.Background(), fixture.workspaceID, planID, keptPartID, directIssueID); err != nil {
		t.Fatalf("LinkIssue direct part: %v", err)
	}
	if _, err := fixture.pool.Exec(context.Background(),
		`INSERT INTO project_plan_dependency (
			project_plan_id, blocked_part_id, blocking_phase_id
		) VALUES ($1, $2, $3)`, planID, partID, keptPhaseID,
	); err != nil {
		t.Fatalf("seed dependency: %v", err)
	}

	if err := fixture.service.DeletePhase(context.Background(), fixture.workspaceID, planID, phaseID); err != nil {
		t.Fatalf("DeletePhase: %v", err)
	}
	var removedRows int
	if err := fixture.pool.QueryRow(context.Background(),
		`SELECT
			(SELECT COUNT(*) FROM project_plan_phase WHERE id = $1) +
			(SELECT COUNT(*) FROM project_plan_part WHERE id = $2) +
			(SELECT COUNT(*) FROM project_plan_part_issue WHERE project_plan_part_id = $2) +
			(SELECT COUNT(*) FROM project_plan_dependency WHERE project_plan_id = $3)`,
		phaseID, partID, planID,
	).Scan(&removedRows); err != nil {
		t.Fatalf("count phase cascade: %v", err)
	}
	if removedRows != 0 {
		t.Fatalf("phase cascade retained %d rows", removedRows)
	}
	if _, err := fixture.pool.Exec(context.Background(),
		`INSERT INTO project_plan_dependency (
			project_plan_id, blocked_part_id, blocking_phase_id
		) VALUES ($1, $2, $3)`, planID, keptPartID, keptPhaseID,
	); err != nil {
		t.Fatalf("seed direct-part dependency: %v", err)
	}
	if err := fixture.service.DeletePart(context.Background(), fixture.workspaceID, planID, keptPartID); err != nil {
		t.Fatalf("DeletePart: %v", err)
	}
	var issueCount, removedPartRows int
	if err := fixture.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM issue WHERE id = $1 OR id = $2`, issueID, directIssueID).Scan(&issueCount); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	if err := fixture.pool.QueryRow(context.Background(),
		`SELECT
			(SELECT COUNT(*) FROM project_plan_part WHERE id = $1) +
			(SELECT COUNT(*) FROM project_plan_part_issue WHERE project_plan_part_id = $1) +
			(SELECT COUNT(*) FROM project_plan_dependency WHERE blocked_part_id = $1 OR blocking_part_id = $1)`,
		keptPartID,
	).Scan(&removedPartRows); err != nil {
		t.Fatalf("count part cascade: %v", err)
	}
	if issueCount != 2 || removedPartRows != 0 {
		t.Fatalf("surviving issues/removed part rows = %d/%d, want 2/0", issueCount, removedPartRows)
	}
}

func parsePlanTestUUID(t *testing.T, value string) pgtype.UUID {
	t.Helper()
	parsed, err := util.ParseUUID(value)
	if err != nil {
		t.Fatalf("parse UUID %q: %v", value, err)
	}
	return parsed
}

func planErrorKind(t *testing.T, err error) ErrorKind {
	t.Helper()
	if err == nil {
		t.Fatal("expected project plan error")
	}
	var planErr *Error
	if !errors.As(err, &planErr) {
		t.Fatalf("error type = %T, want *projectplan.Error: %v", err, err)
	}
	return planErr.Kind
}
