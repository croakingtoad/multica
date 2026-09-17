package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type issueWriteAuditProbe struct {
	ActorType           string
	ActorID             pgtype.UUID
	SourceTaskID        pgtype.UUID
	ClientPlatform      string
	Endpoint            string
	RequestedFields     []string
	ChangedFields       []string
	OldTitleBytes       pgtype.Int8
	NewTitleBytes       pgtype.Int8
	OldTitleHash        pgtype.Text
	NewTitleHash        pgtype.Text
	OldDescriptionBytes pgtype.Int8
	NewDescriptionBytes pgtype.Int8
	OldDescriptionHash  pgtype.Text
	NewDescriptionHash  pgtype.Text
	ExpectedRevision    pgtype.Int8
	RevisionBefore      int64
	RevisionAfter       int64
	RevisionConflict    bool
	Outcome             string
}

type countingIssueWriteTxStarter struct {
	inner txStarter
	calls int
}

func (s *countingIssueWriteTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	s.calls++
	return s.inner.Begin(ctx)
}

func issueTextHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func issueWriteAuditFor(t *testing.T, issueID string) issueWriteAuditProbe {
	t.Helper()
	var got issueWriteAuditProbe
	dbfx.QueryRow(t, `
		SELECT actor_type, actor_id, source_task_id, client_platform, endpoint, requested_fields, changed_fields,
		       old_title_bytes, new_title_bytes, old_title_hash, new_title_hash,
		       old_description_bytes, new_description_bytes,
		       old_description_hash, new_description_hash, expected_revision,
		       revision_before, revision_after, revision_conflict, outcome
		FROM issue_write_audit
		WHERE issue_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, issueID).Scan(
		&got.ActorType, &got.ActorID, &got.SourceTaskID, &got.ClientPlatform, &got.Endpoint, &got.RequestedFields, &got.ChangedFields,
		&got.OldTitleBytes, &got.NewTitleBytes, &got.OldTitleHash, &got.NewTitleHash,
		&got.OldDescriptionBytes, &got.NewDescriptionBytes,
		&got.OldDescriptionHash, &got.NewDescriptionHash, &got.ExpectedRevision,
		&got.RevisionBefore, &got.RevisionAfter, &got.RevisionConflict, &got.Outcome,
	)
	return got
}

func auditUpdateRequest(method, path string, body any) *http.Request {
	return testutil.WithHeaders(testutil.JSONRequest(method, path, body),
		"X-User-ID", testUserID,
		"X-Workspace-ID", testWorkspaceID,
	)
}

func assertDescriptionAudit(t *testing.T, got issueWriteAuditProbe, oldDescription, newDescription string, revisionBefore, revisionAfter int64) {
	t.Helper()
	if !reflect.DeepEqual(got.RequestedFields, []string{"description"}) {
		t.Errorf("requested_fields = %v, want [description]", got.RequestedFields)
	}
	if !reflect.DeepEqual(got.ChangedFields, []string{"description"}) {
		t.Errorf("changed_fields = %v, want [description]", got.ChangedFields)
	}
	if !got.OldDescriptionBytes.Valid || !got.NewDescriptionBytes.Valid ||
		got.OldDescriptionBytes.Int64 != int64(len([]byte(oldDescription))) || got.NewDescriptionBytes.Int64 != int64(len([]byte(newDescription))) {
		t.Errorf("description bytes = (%d, %d), want (%d, %d)",
			got.OldDescriptionBytes.Int64, got.NewDescriptionBytes.Int64, len([]byte(oldDescription)), len([]byte(newDescription)))
	}
	if !got.OldDescriptionHash.Valid || !got.NewDescriptionHash.Valid ||
		got.OldDescriptionHash.String != issueTextHash(oldDescription) || got.NewDescriptionHash.String != issueTextHash(newDescription) {
		t.Errorf("description hashes = (%q, %q), want SHA-256 of before/after bodies", got.OldDescriptionHash.String, got.NewDescriptionHash.String)
	}
	if got.RevisionBefore != revisionBefore || got.RevisionAfter != revisionAfter {
		t.Errorf("revisions = (%d, %d), want (%d, %d)", got.RevisionBefore, got.RevisionAfter, revisionBefore, revisionAfter)
	}
	if got.RevisionConflict || got.Outcome != "applied" {
		t.Errorf("result = (conflict %t, outcome %q), want applied", got.RevisionConflict, got.Outcome)
	}
	if got.ActorType != "member" || !got.ActorID.Valid || got.ActorID != parseUUID(testUserID) || got.SourceTaskID.Valid {
		t.Errorf("actor = (%q, %v, task %v), want test member without source task", got.ActorType, got.ActorID, got.SourceTaskID)
	}
}

func TestIssueDescriptionWritesAreAuditedAcrossUpdatePaths(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	tests := []struct {
		name             string
		oldDescription   string
		newDescription   string
		clientPlatform   string
		endpoint         string
		expectedRevision bool
		call             func(t *testing.T, issueID, description string)
	}{
		{
			name:             "CLI-style update with expected revision",
			oldDescription:   "cli old",
			newDescription:   "cli new Ω",
			clientPlatform:   "cli",
			endpoint:         issueUpdateEndpoint,
			expectedRevision: true,
			call: func(t *testing.T, issueID, description string) {
				req := auditUpdateRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{
					"description": description, "expected_revision": 1,
				})
				req.Header.Set("X-Client-Platform", "cli")
				testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(req, "id", issueID)).Want(http.StatusOK)
			},
		},
		{
			name:             "raw API update without expected revision",
			oldDescription:   "raw old",
			newDescription:   "raw new",
			clientPlatform:   "unknown",
			endpoint:         issueUpdateEndpoint,
			expectedRevision: false,
			call: func(t *testing.T, issueID, description string) {
				req := auditUpdateRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{"description": description})
				testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(req, "id", issueID)).Want(http.StatusOK)
			},
		},
		{
			name:             "batch update",
			oldDescription:   "batch old",
			newDescription:   "batch new",
			clientPlatform:   "unknown",
			endpoint:         issueBatchUpdateEndpoint,
			expectedRevision: false,
			call: func(t *testing.T, issueID, description string) {
				req := auditUpdateRequest(http.MethodPost, "/api/issues/batch-update", map[string]any{
					"issue_ids": []string{issueID}, "updates": map[string]any{"description": description},
				})
				testutil.Call(t, testHandler.BatchUpdateIssues, req).Want(http.StatusOK)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			issueID := dbfx.Issue(t, "audit path", testutil.Cols{"description": tc.oldDescription})
			dbfx.Cleanup(t, `DELETE FROM issue_write_audit WHERE issue_id = $1`, issueID)
			tc.call(t, issueID, tc.newDescription)

			got := issueWriteAuditFor(t, issueID)
			assertDescriptionAudit(t, got, tc.oldDescription, tc.newDescription, 1, 2)
			if got.ClientPlatform != tc.clientPlatform || got.Endpoint != tc.endpoint {
				t.Errorf("source = (%q, %q), want (%q, %q)", got.ClientPlatform, got.Endpoint, tc.clientPlatform, tc.endpoint)
			}
			if tc.expectedRevision {
				if !got.ExpectedRevision.Valid || got.ExpectedRevision.Int64 != 1 {
					t.Errorf("expected_revision = %+v, want 1", got.ExpectedRevision)
				}
			} else if got.ExpectedRevision.Valid {
				t.Errorf("expected_revision = %+v, want NULL", got.ExpectedRevision)
			}
		})
	}

	t.Run("batch title-only update", func(t *testing.T) {
		const oldTitle = "batch title old"
		const newTitle = "batch title new Ω"
		issueID := dbfx.Issue(t, oldTitle, testutil.Cols{"description": "untouched description"})
		dbfx.Cleanup(t, `DELETE FROM issue_write_audit WHERE issue_id = $1`, issueID)

		req := auditUpdateRequest(http.MethodPost, "/api/issues/batch-update", map[string]any{
			"issue_ids": []string{issueID}, "updates": map[string]any{"title": newTitle},
		})
		txStarter := &countingIssueWriteTxStarter{inner: testHandler.TxStarter}
		h := *testHandler
		h.TxStarter = txStarter
		testutil.Call(t, h.BatchUpdateIssues, req).Want(http.StatusOK)
		if txStarter.calls == 0 {
			t.Error("title-only batch update started no transaction; want locked atomic write path")
		}

		got := issueWriteAuditFor(t, issueID)
		if !reflect.DeepEqual(got.RequestedFields, []string{"title"}) || !reflect.DeepEqual(got.ChangedFields, []string{"title"}) {
			t.Errorf("fields = (requested %v, changed %v), want title only", got.RequestedFields, got.ChangedFields)
		}
		if !got.OldTitleBytes.Valid || !got.NewTitleBytes.Valid ||
			got.OldTitleBytes.Int64 != int64(len([]byte(oldTitle))) || got.NewTitleBytes.Int64 != int64(len([]byte(newTitle))) {
			t.Errorf("title bytes = (%d, %d), want (%d, %d)",
				got.OldTitleBytes.Int64, got.NewTitleBytes.Int64, len([]byte(oldTitle)), len([]byte(newTitle)))
		}
		if !got.OldTitleHash.Valid || !got.NewTitleHash.Valid ||
			got.OldTitleHash.String != issueTextHash(oldTitle) || got.NewTitleHash.String != issueTextHash(newTitle) {
			t.Errorf("title hashes = (%q, %q), want SHA-256 of before/after titles", got.OldTitleHash.String, got.NewTitleHash.String)
		}
		if got.OldDescriptionBytes.Valid || got.NewDescriptionBytes.Valid || got.OldDescriptionHash.Valid || got.NewDescriptionHash.Valid {
			t.Errorf("description audit = (%+v, %+v, %+v, %+v), want all NULL",
				got.OldDescriptionBytes, got.NewDescriptionBytes, got.OldDescriptionHash, got.NewDescriptionHash)
		}
		if got.RevisionBefore != 1 || got.RevisionAfter != 2 {
			t.Errorf("revisions = (%d, %d), want (1, 2)", got.RevisionBefore, got.RevisionAfter)
		}
	})
}

func TestIssueDescriptionRevisionConflictIsAudited(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	issueID := dbfx.Issue(t, "audit conflict", testutil.Cols{"description": "original"})
	dbfx.Cleanup(t, `DELETE FROM issue_write_audit WHERE issue_id = $1`, issueID)

	first := auditUpdateRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{
		"description": "current", "expected_revision": 1,
	})
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(first, "id", issueID)).Want(http.StatusOK)
	stale := auditUpdateRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{
		"description": "stale replacement", "expected_revision": 1,
	})
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(stale, "id", issueID)).Want(http.StatusConflict)

	got := issueWriteAuditFor(t, issueID)
	if got.Outcome != "revision_conflict" || !got.RevisionConflict {
		t.Fatalf("result = (conflict %t, outcome %q), want revision conflict", got.RevisionConflict, got.Outcome)
	}
	if got.RevisionBefore != 2 || got.RevisionAfter != 2 {
		t.Errorf("conflict revisions = (%d, %d), want (2, 2)", got.RevisionBefore, got.RevisionAfter)
	}
	if !got.ExpectedRevision.Valid || got.ExpectedRevision.Int64 != 1 {
		t.Errorf("expected_revision = %+v, want stale revision 1", got.ExpectedRevision)
	}
	if len(got.ChangedFields) != 0 {
		t.Errorf("changed_fields = %v, want none for rejected write", got.ChangedFields)
	}
	if !got.OldDescriptionBytes.Valid || !got.NewDescriptionBytes.Valid ||
		got.OldDescriptionBytes.Int64 != int64(len("current")) || got.NewDescriptionBytes.Int64 != int64(len("stale replacement")) {
		t.Errorf("conflict description bytes = (%d, %d), want current/attempted lengths", got.OldDescriptionBytes.Int64, got.NewDescriptionBytes.Int64)
	}
}

func TestAgentIssueWriteAuditRetainsSourceTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	issueID := dbfx.Issue(t, "agent audit", testutil.Cols{"description": "before agent"})
	agentID := dbfx.Agent(t, "Audit agent", testRuntimeID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": testRuntimeID})
	dbfx.Cleanup(t, `DELETE FROM issue_write_audit WHERE issue_id = $1`, issueID)

	req := auditUpdateRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{"description": "after agent"})
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(req, "id", issueID)).Want(http.StatusOK)

	got := issueWriteAuditFor(t, issueID)
	if got.ActorType != "agent" || got.ActorID != parseUUID(agentID) || got.SourceTaskID != parseUUID(taskID) {
		t.Fatalf("agent audit = (%q, %v, task %v), want agent %s task %s", got.ActorType, got.ActorID, got.SourceTaskID, agentID, taskID)
	}
}

func TestChannelMediaDescriptionMaterializationIsAudited(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	const oldDescription = "channel base"
	const newDescription = "channel base with media"
	issueID := dbfx.Issue(t, "audit materialization", testutil.Cols{"description": oldDescription})
	dbfx.Cleanup(t, `DELETE FROM issue_write_audit WHERE issue_id = $1`, issueID)

	row, err := db.New(testPool).MaterializeIssueChannelMediaMarkdown(t.Context(), db.MaterializeIssueChannelMediaMarkdownParams{
		ID:              parseUUID(issueID),
		WorkspaceID:     parseUUID(testWorkspaceID),
		BaseDescription: pgtype.Text{String: oldDescription, Valid: true},
		Description:     newDescription,
		Markdown:        pgtype.Text{String: "unused fallback", Valid: true},
	})
	if err != nil {
		t.Fatalf("materialize description: %v", err)
	}
	if row.Description.String != newDescription {
		t.Fatalf("materialized description = %q, want %q", row.Description.String, newDescription)
	}

	got := issueWriteAuditFor(t, issueID)
	if got.ActorType != "system" || got.ActorID.Valid || got.SourceTaskID.Valid {
		t.Errorf("materialization actor = (%q, %v, task %v), want system", got.ActorType, got.ActorID, got.SourceTaskID)
	}
	if got.ClientPlatform != "internal" || got.Endpoint != "channel_media_materialization" {
		t.Errorf("materialization source = (%q, %q), want internal channel media", got.ClientPlatform, got.Endpoint)
	}
	if !got.OldDescriptionBytes.Valid || !got.NewDescriptionBytes.Valid ||
		got.OldDescriptionBytes.Int64 != int64(len(oldDescription)) || got.NewDescriptionBytes.Int64 != int64(len(newDescription)) {
		t.Errorf("materialization lengths = (%d, %d), want (%d, %d)", got.OldDescriptionBytes.Int64, got.NewDescriptionBytes.Int64, len(oldDescription), len(newDescription))
	}
}
