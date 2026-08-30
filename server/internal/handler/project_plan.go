package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/projectplan"
)

// GetActiveProjectPlan returns the complete read model for a project's active
// plan. A project without an active plan is a normal 404 empty state.
func (h *Handler) GetActiveProjectPlan(w http.ResponseWriter, r *http.Request) {
	if !h.projectPlanReadsEnabled(r) {
		writeError(w, http.StatusNotFound, "project plan not found")
		return
	}
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	overview, err := projectplan.NewReader(h.Queries).ReadActive(
		r.Context(), project.WorkspaceID, project.ID,
	)
	h.writeProjectPlanRead(w, r, overview, err)
}

// GetProjectPlan returns a retained plan version with live issue state.
func (h *Handler) GetProjectPlan(w http.ResponseWriter, r *http.Request) {
	if !h.projectPlanReadsEnabled(r) {
		writeError(w, http.StatusNotFound, "project plan not found")
		return
	}
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	planID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "planId"), "plan id")
	if !ok {
		return
	}

	overview, err := projectplan.NewReader(h.Queries).Read(
		r.Context(), project.WorkspaceID, project.ID, planID,
	)
	h.writeProjectPlanRead(w, r, overview, err)
}

func (h *Handler) projectPlanReadsEnabled(r *http.Request) bool {
	return featureflags.ProjectPlansEnabled(r.Context(), h.FeatureFlags)
}

func (h *Handler) writeProjectPlanRead(
	w http.ResponseWriter,
	r *http.Request,
	overview projectplan.Overview,
	err error,
) {
	if err == nil {
		writeJSON(w, http.StatusOK, overview)
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "project plan not found")
		return
	}
	slog.Error("project plan read failed", append(logger.RequestAttrs(r), "error", err)...)
	writeError(w, http.StatusInternalServerError, "failed to read project plan")
}
