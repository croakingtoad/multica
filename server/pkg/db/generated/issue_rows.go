package db

// Issue converts the row returned by the audited issue update CTE back to the
// canonical model used by existing service and handler boundaries.
func (r UpdateIssueRow) Issue() Issue {
	return Issue{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Title: r.Title,
		Description: r.Description, Status: r.Status, Priority: r.Priority,
		AssigneeType: r.AssigneeType, AssigneeID: r.AssigneeID,
		CreatorType: r.CreatorType, CreatorID: r.CreatorID,
		ParentIssueID: r.ParentIssueID, AcceptanceCriteria: r.AcceptanceCriteria,
		ContextRefs: r.ContextRefs, Position: r.Position, DueDate: r.DueDate,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Number: r.Number,
		ProjectID: r.ProjectID, OriginType: r.OriginType, OriginID: r.OriginID,
		FirstExecutedAt: r.FirstExecutedAt, StartDate: r.StartDate,
		Metadata: r.Metadata, Stage: r.Stage, Properties: r.Properties,
		Revision: r.Revision, LastActivityAt: r.LastActivityAt,
	}
}

// Issue converts the audited channel-media materialization row to the same
// canonical issue model returned before audit recording was added.
func (r MaterializeIssueChannelMediaMarkdownRow) Issue() Issue {
	return Issue{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Title: r.Title,
		Description: r.Description, Status: r.Status, Priority: r.Priority,
		AssigneeType: r.AssigneeType, AssigneeID: r.AssigneeID,
		CreatorType: r.CreatorType, CreatorID: r.CreatorID,
		ParentIssueID: r.ParentIssueID, AcceptanceCriteria: r.AcceptanceCriteria,
		ContextRefs: r.ContextRefs, Position: r.Position, DueDate: r.DueDate,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Number: r.Number,
		ProjectID: r.ProjectID, OriginType: r.OriginType, OriginID: r.OriginID,
		FirstExecutedAt: r.FirstExecutedAt, StartDate: r.StartDate,
		Metadata: r.Metadata, Stage: r.Stage, Properties: r.Properties,
		Revision: r.Revision, LastActivityAt: r.LastActivityAt,
	}
}
