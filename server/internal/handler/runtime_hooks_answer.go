package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/runtimehooks"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// hookEventAnswerResponse dates the answer it carries, because an answer
// computed from an undated snapshot would be a claim about a host nobody has
// read. A nil Answer with Error set is the honest shape for every case where
// Multica has no answer; a client that renders a missing Answer as "nothing
// runs" is stating something the server never established.
type hookEventAnswerResponse struct {
	RuntimeID  string                    `json:"runtime_id"`
	Provider   string                    `json:"provider"`
	Cached     bool                      `json:"cached"`
	ObservedAt *time.Time                `json:"observed_at,omitempty"`
	Answer     *runtimehooks.EventAnswer `json:"answer,omitempty"`
	Error      string                    `json:"error,omitempty"`
}

// AnswerHookEvent answers which configured handlers run on one event for one
// candidate value, against the snapshot the last completed read stored.
//
// Both query parameters are required. A matcher is evaluated against a value,
// so a request without one has no answer to be given — refusing it here is
// what stops a valueless read being served, and then rendered, as "these
// fire". Read-only: it touches no write path and stores nothing.
func (h *Handler) AnswerHookEvent(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, _, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeAPI, runtimeID)
	if !ok {
		return
	}
	if _, err := expectedHookSources(rt.Provider); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	event := r.URL.Query().Get("event")
	if event == "" {
		writeError(w, http.StatusBadRequest, "event is required")
		return
	}
	value := r.URL.Query().Get("value")
	if value == "" {
		writeError(w, http.StatusBadRequest, "value is required: a matcher is evaluated against a value, and Multica gives no answer without one")
		return
	}

	rows, err := h.Queries.ListHookStateSnapshot(r.Context(), db.ListHookStateSnapshotParams{
		RuntimeID: rt.ID, Provider: rt.Provider,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load hook snapshot")
		return
	}
	observedAt := latestHookObservedAt(rows)
	response := hookEventAnswerResponse{
		RuntimeID: uuidToString(rt.ID),
		Provider:  rt.Provider,
		// An offline runtime can only be answered from the stored snapshot, so
		// the answer is last-known exactly when the tab's own read is.
		Cached:     rt.Status != "online",
		ObservedAt: observedAt,
	}
	if observedAt == nil {
		response.Error = "runtime has no hook observation to answer from"
		writeJSON(w, http.StatusOK, response)
		return
	}
	refs, observed, err := hookResolutionInputs(rt.Provider, rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to prepare hook answer: %v", err))
		return
	}
	answer := runtimehooks.AnswerEvent(runtimehooks.Provider(rt.Provider), refs, observed, event, value)
	response.Answer = &answer
	writeJSON(w, http.StatusOK, response)
}
