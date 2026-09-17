package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ErrIssueRevisionConflict means the caller based an update on a stale issue
// revision. Transports map it to their stable conflict response.
var ErrIssueRevisionConflict = errors.New("issue revision conflict")

// IssueContentPatch is the first shared Public API write primitive. It is
// deliberately transport- and credential-agnostic: App, PAT, and Plugin
// entrypoints authorize independently, then call the same business operation.
type IssueContentPatch struct {
	Title            *string
	Description      *string
	ExpectedRevision *int64
	Audit            IssueWriteAudit
}

// IssueWriteAudit carries attribution from a transport boundary to the shared
// issue write. Bodies never enter this value; lengths and hashes are computed
// beside the UPDATE from the authoritative before/after rows.
type IssueWriteAudit struct {
	ActorType      string
	ActorID        pgtype.UUID
	SourceTaskID   pgtype.UUID
	ClientPlatform string
	ClientVersion  pgtype.Text
	ClientOS       pgtype.Text
	Endpoint       string
}

func applyIssueWriteAudit(params *db.UpdateIssueParams, audit IssueWriteAudit) {
	params.AuditActorType = audit.ActorType
	params.AuditActorID = audit.ActorID
	params.AuditSourceTaskID = audit.SourceTaskID
	params.AuditClientPlatform = audit.ClientPlatform
	params.AuditClientVersion = audit.ClientVersion
	params.AuditClientOs = audit.ClientOS
	params.AuditEndpoint = audit.Endpoint
}

func conflictText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

// UpdateContent updates only the low-risk issue content fields exposed in the
// first Public API slice. Assignment, status, project, and hierarchy changes
// remain separate operations because each has additional policy and side
// effects.
func (s *IssueService) UpdateContent(ctx context.Context, issue db.Issue, patch IssueContentPatch) (db.Issue, error) {
	if s.TxStarter == nil {
		return db.Issue{}, errors.New("issue content update requires transaction starter")
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.Issue{}, fmt.Errorf("begin issue content update: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)
	current, err := qtx.LockIssueForDescriptionUpdate(ctx, db.LockIssueForDescriptionUpdateParams{
		ID:          issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return db.Issue{}, fmt.Errorf("lock issue content: %w", err)
	}
	if patch.ExpectedRevision != nil && current.Revision != *patch.ExpectedRevision {
		if err := qtx.RecordIssueWriteConflict(ctx, db.RecordIssueWriteConflictParams{
			AuditActorType:      patch.Audit.ActorType,
			AuditActorID:        patch.Audit.ActorID,
			AuditSourceTaskID:   patch.Audit.SourceTaskID,
			AuditClientPlatform: patch.Audit.ClientPlatform,
			AuditClientVersion:  patch.Audit.ClientVersion,
			AuditClientOs:       patch.Audit.ClientOS,
			AuditEndpoint:       patch.Audit.Endpoint,
			Title:               conflictText(patch.Title),
			Description:         conflictText(patch.Description),
			ExpectedRevision:    *patch.ExpectedRevision,
			ID:                  current.ID,
			WorkspaceID:         current.WorkspaceID,
		}); err != nil {
			return db.Issue{}, fmt.Errorf("record issue revision conflict: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return db.Issue{}, fmt.Errorf("commit issue revision conflict audit: %w", err)
		}
		return db.Issue{}, ErrIssueRevisionConflict
	}

	params := db.UpdateIssueParams{
		ID:            current.ID,
		AssigneeType:  current.AssigneeType,
		AssigneeID:    current.AssigneeID,
		StartDate:     current.StartDate,
		DueDate:       current.DueDate,
		ParentIssueID: current.ParentIssueID,
		ProjectID:     current.ProjectID,
		Stage:         current.Stage,
	}
	applyIssueWriteAudit(&params, patch.Audit)
	if patch.ExpectedRevision != nil {
		params.ExpectedRevision = pgtype.Int8{Int64: *patch.ExpectedRevision, Valid: true}
	}
	if patch.Title != nil {
		params.Title = pgtype.Text{String: *patch.Title, Valid: true}
	}
	if patch.Description != nil {
		params.Description = pgtype.Text{String: *patch.Description, Valid: true}
	}

	updatedRow, err := qtx.UpdateIssue(ctx, params)
	if patch.ExpectedRevision != nil && errors.Is(err, pgx.ErrNoRows) {
		return db.Issue{}, ErrIssueRevisionConflict
	}
	if err != nil {
		return db.Issue{}, err
	}
	updated := updatedRow.Issue()
	if err := tx.Commit(ctx); err != nil {
		return db.Issue{}, fmt.Errorf("commit issue content update: %w", err)
	}
	return updated, nil
}
