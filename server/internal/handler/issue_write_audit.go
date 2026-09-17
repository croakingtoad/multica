package handler

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	issueUpdateEndpoint      = "PUT /api/issues/:id"
	issueBatchUpdateEndpoint = "POST /api/issues/batch-update"
	pluginIssuePatchEndpoint = "PATCH /api/plugin-bridge/v1/issues/:issue_ref"
)

func optionalText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

func (h *Handler) issueWriteAuditFromRequest(r *http.Request, actorType, actorID, endpoint string) service.IssueWriteAudit {
	platform, version, osName := middleware.ClientMetadataFromContext(r.Context())
	// Direct handler tests and trusted in-process callers do not traverse the
	// middleware, so preserve the same best-effort metadata from the headers.
	if platform == "" {
		platform = r.Header.Get(middleware.HeaderClientPlatform)
	}
	if version == "" {
		version = r.Header.Get(middleware.HeaderClientVersion)
	}
	if osName == "" {
		osName = r.Header.Get(middleware.HeaderClientOS)
	}
	if platform == "" {
		platform = "unknown"
	}

	audit := service.IssueWriteAudit{
		ActorType:      actorType,
		ClientPlatform: platform,
		ClientVersion:  optionalText(version),
		ClientOS:       optionalText(osName),
		Endpoint:       endpoint,
	}
	if parsed, err := util.ParseUUID(actorID); err == nil {
		audit.ActorID = parsed
	}
	if actorType == "agent" {
		audit.SourceTaskID = h.commentSourceTaskID(r)
	}
	return audit
}

func applyIssueWriteAudit(params *db.UpdateIssueParams, audit service.IssueWriteAudit) {
	params.AuditActorType = audit.ActorType
	params.AuditActorID = audit.ActorID
	params.AuditSourceTaskID = audit.SourceTaskID
	params.AuditClientPlatform = audit.ClientPlatform
	params.AuditClientVersion = audit.ClientVersion
	params.AuditClientOs = audit.ClientOS
	params.AuditEndpoint = audit.Endpoint
}

func auditText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

func (h *Handler) recordIssueRevisionConflict(ctx context.Context, issue db.Issue, req UpdateIssueRequest, expectedRevision int64, audit service.IssueWriteAudit) error {
	return h.Queries.RecordIssueWriteConflict(ctx, db.RecordIssueWriteConflictParams{
		AuditActorType:      audit.ActorType,
		AuditActorID:        audit.ActorID,
		AuditSourceTaskID:   audit.SourceTaskID,
		AuditClientPlatform: audit.ClientPlatform,
		AuditClientVersion:  audit.ClientVersion,
		AuditClientOs:       audit.ClientOS,
		AuditEndpoint:       audit.Endpoint,
		Title:               auditText(req.Title),
		Description:         auditText(req.Description),
		ExpectedRevision:    expectedRevision,
		ID:                  issue.ID,
		WorkspaceID:         issue.WorkspaceID,
	})
}
