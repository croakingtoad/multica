package handler

// Read path for the hook-fire feed (LOCO-135).
//
// This file is deliberately separate from runtime_hook_fires.go, which owns
// the write path. DP-LOCO-114-03 authorises a read of hook_fire_history under
// four binding conditions, and two of them are about which file may do what:
//
//   1. Read-only. Nothing here inserts, updates or deletes. The mutating query
//      set for this table stays exactly InsertHookFireHistory (write path),
//      the two cascade deletes in runtime.sql and workspace_delete.sql,
//      MergeRuntimeHookData's runtime_id-only UPDATE, and retention's
//      delete-by-predicate.
//   2. No provenance computation at read time. ListHookFires copies the stored
//      `provenance` string onto the wire and does nothing else with it: no
//      derivation, no upgrade, no "this row looks like a debug_log match".
//      Round 3 of this effort shipped a greedy join that assigned one
//      execution's timestamp to another and stamped the result debug_log,
//      fabricating provenance. The capture path is now guarded against that;
//      a read path that recomputes provenance would reintroduce the same
//      defect in a layer nobody is watching.
//   3. No join on execution_id as configured-hook identity. execution_id is
//      Claude's per-execution reference, fresh on every fire. It travels to
//      the client as an opaque diagnostic string and is never grouped,
//      joined, DISTINCTed or deduped on.
//   4. The capture path keeps zero reads. internal/daemon still contains no
//      read of hook_fire_history; this feed lives in internal/handler.

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	defaultHookFireFeedLimit = 100
	maxHookFireFeedLimit     = 500
)

// RuntimeHookFireResponse is one row of hook_fire_history as the feed sees it.
//
// Provenance is per record and is never collapsed into a per-provider claim.
// A Claude feed is mixed: stage 7's Tier 2 audit measured 101 of 144 real
// responses (70.1%) as `inferred`, because 66% of hook responses print nothing
// on stdout and so can never be matched to a host-recorded timestamp. Whoever
// renders this must state the limit per row rather than labelling the feed.
//
// FiredAt therefore means different things per row, and the pair is only
// honest read together:
//
//   - provenance "debug_log": FiredAt is the time the host's own debug log
//     recorded for a uniquely matched execution.
//   - provenance "inferred": Multica knows from the provider's structured
//     stream that the hook ran, and knows its outcome and exit code, but the
//     host recorded no timestamp it could match — FiredAt is when Multica
//     received the record. A materially weaker claim.
//
// ExecutionID is the provider's per-execution reference, fresh on every fire
// for Claude. It is a diagnostic identifier only. It MUST NOT be presented or
// joined on as stable configured-hook identity (DP-LOCO-114-02 item 3).
type RuntimeHookFireResponse struct {
	ID          string          `json:"id"`
	Provider    string          `json:"provider"`
	Event       string          `json:"event"`
	ExecutionID string          `json:"execution_id"`
	HookSpec    json.RawMessage `json:"hook_spec"`
	FiredAt     string          `json:"fired_at"`
	Provenance  string          `json:"provenance"`
	Outcome     string          `json:"outcome"`
	Detail      json.RawMessage `json:"detail"`
}

// RuntimeHookFireFeedResponse carries the page plus the two facts the screen
// needs to describe its own completeness: the limit that was applied, and
// whether more rows exist behind it.
type RuntimeHookFireFeedResponse struct {
	Fires     []RuntimeHookFireResponse `json:"fires"`
	Limit     int                       `json:"limit"`
	Truncated bool                      `json:"truncated"`
}

// parseHookFireFeedLimit reads ?limit=, clamping to [1, maxHookFireFeedLimit].
// A malformed or absent value takes the default rather than erroring: the feed
// is a diagnostic view and a bad query string should not deny it.
func parseHookFireFeedLimit(r *http.Request) int {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return defaultHookFireFeedLimit
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return defaultHookFireFeedLimit
	}
	if value > maxHookFireFeedLimit {
		return maxHookFireFeedLimit
	}
	return value
}

// ListHookFires returns a runtime's most recent hook fires, newest first.
//
// The projection is mechanical by design: every field is copied from the
// stored row. `provenance` and `outcome` in particular are passed through
// verbatim, so what the screen renders is what the capture path wrote.
func (h *Handler) ListHookFires(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, _, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeAPI, runtimeID)
	if !ok {
		return
	}

	limit := parseHookFireFeedLimit(r)
	rows, err := h.Queries.ListRuntimeHookFires(r.Context(), db.ListRuntimeHookFiresParams{
		RuntimeID: rt.ID,
		RowLimit:  int32(limit),
	})
	if err != nil {
		slog.Error("hook fire feed read failed", "runtime_id", runtimeID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list hook fires")
		return
	}

	fires := make([]RuntimeHookFireResponse, len(rows))
	for i, row := range rows {
		fires[i] = RuntimeHookFireResponse{
			ID:          uuidToString(row.ID),
			Provider:    row.Provider,
			Event:       row.Event,
			ExecutionID: row.ExecutionID,
			HookSpec:    json.RawMessage(row.HookSpec),
			FiredAt:     row.FiredAt.Time.UTC().Format(time.RFC3339Nano),
			// Verbatim. See condition 2 in the file header.
			Provenance: row.Provenance,
			Outcome:    row.Outcome,
			Detail:     json.RawMessage(row.Detail),
		}
	}

	writeJSON(w, http.StatusOK, RuntimeHookFireFeedResponse{
		Fires:     fires,
		Limit:     limit,
		Truncated: len(rows) == limit,
	})
}
