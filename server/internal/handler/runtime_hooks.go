package handler

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/runtimehooks"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type HookReadStatus string

const (
	HookReadPending   HookReadStatus = "pending"
	HookReadRunning   HookReadStatus = "running"
	HookReadCompleted HookReadStatus = "completed"
	HookReadFailed    HookReadStatus = "failed"
	HookReadTimedOut  HookReadStatus = "timed_out"

	hookReadPendingTimeout = 30 * time.Second
	hookReadRunningTimeout = 60 * time.Second
	hookReadStoreRetention = 2 * time.Minute

	// Preserve the host's observation time while allowing ordinary NTP drift.
	// Larger future offsets would make an old snapshot appear fresh indefinitely.
	maxHookObservationFutureSkew = 5 * time.Minute
)

type HookReadRequest struct {
	ID           string         `json:"id"`
	RuntimeID    string         `json:"runtime_id"`
	Status       HookReadStatus `json:"status"`
	ObservedAt   *time.Time     `json:"observed_at,omitempty"`
	Error        string         `json:"error,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	RunStartedAt *time.Time     `json:"-"`
}

// HookReadStore holds only the request lifecycle. Hook contents live in the
// non-authoritative database snapshot and are written only by the daemon
// report handler below. Create always queues a host read; it has no cache path.
type HookReadStore interface {
	Create(ctx context.Context, runtimeID string) (*HookReadRequest, error)
	Get(ctx context.Context, id string) (*HookReadRequest, error)
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	PopPending(ctx context.Context, runtimeID string) (*HookReadRequest, error)
	Complete(ctx context.Context, id string, observedAt time.Time) error
	Fail(ctx context.Context, id, errMsg string) error
}

func applyHookReadTimeout(req *HookReadRequest, now time.Time) bool {
	switch req.Status {
	case HookReadPending:
		if now.Sub(req.CreatedAt) > hookReadPendingTimeout {
			req.Status = HookReadTimedOut
			req.Error = "daemon did not answer within 30 seconds"
			req.UpdatedAt = now
			return true
		}
	case HookReadRunning:
		if req.RunStartedAt != nil && now.Sub(*req.RunStartedAt) > hookReadRunningTimeout {
			req.Status = HookReadTimedOut
			req.Error = "daemon did not finish within 60 seconds"
			req.UpdatedAt = now
			return true
		}
	}
	return false
}

func hookReadTerminal(status HookReadStatus) bool {
	return status == HookReadCompleted || status == HookReadFailed || status == HookReadTimedOut
}

type InMemoryHookReadStore struct {
	mu       sync.Mutex
	requests map[string]*HookReadRequest
}

func NewInMemoryHookReadStore() *InMemoryHookReadStore {
	return &InMemoryHookReadStore{requests: make(map[string]*HookReadRequest)}
}

func (s *InMemoryHookReadStore) Create(_ context.Context, runtimeID string) (*HookReadRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, req := range s.requests {
		if now.Sub(req.CreatedAt) > hookReadStoreRetention {
			delete(s.requests, id)
		}
	}
	req := &HookReadRequest{ID: randomID(), RuntimeID: runtimeID, Status: HookReadPending, CreatedAt: now, UpdatedAt: now}
	s.requests[req.ID] = req
	return req, nil
}

func (s *InMemoryHookReadStore) Get(_ context.Context, id string) (*HookReadRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	req := s.requests[id]
	if req != nil {
		applyHookReadTimeout(req, time.Now())
	}
	return req, nil
}

func (s *InMemoryHookReadStore) HasPending(_ context.Context, runtimeID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, req := range s.requests {
		applyHookReadTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == HookReadPending {
			return true, nil
		}
	}
	return false, nil
}

func (s *InMemoryHookReadStore) PopPending(_ context.Context, runtimeID string) (*HookReadRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	var oldest *HookReadRequest
	for _, req := range s.requests {
		applyHookReadTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == HookReadPending && (oldest == nil || req.CreatedAt.Before(oldest.CreatedAt)) {
			oldest = req
		}
	}
	if oldest != nil {
		oldest.Status = HookReadRunning
		oldest.RunStartedAt = &now
		oldest.UpdatedAt = now
	}
	return oldest, nil
}

func (s *InMemoryHookReadStore) Complete(_ context.Context, id string, observedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req := s.requests[id]; req != nil {
		req.Status = HookReadCompleted
		req.ObservedAt = &observedAt
		req.UpdatedAt = time.Now()
	}
	return nil
}

func (s *InMemoryHookReadStore) Fail(_ context.Context, id, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req := s.requests[id]; req != nil {
		req.Status = HookReadFailed
		req.Error = errMsg
		req.UpdatedAt = time.Now()
	}
	return nil
}

type hookSourceKey struct {
	Scope  string
	Format string
}

type hookSourceState string

const (
	hookSourceFound      hookSourceState = "found"
	hookSourceAbsent     hookSourceState = "absent"
	hookSourceNotChecked hookSourceState = "not_checked"
)

type hookSourceResponse struct {
	Provider      string          `json:"provider"`
	Scope         string          `json:"scope"`
	Format        string          `json:"format"`
	State         hookSourceState `json:"state"`
	SourcePath    *string         `json:"source_path"`
	ContentHash   *string         `json:"content_hash"`
	Hooks         json.RawMessage `json:"hooks,omitempty"`
	DisabledHooks json.RawMessage `json:"disabled_hooks,omitempty"`
	ObservedAt    *time.Time      `json:"observed_at,omitempty"`
}

// `cached` and `offline` are two bits, not one. `cached` says this answer came
// from the snapshot; `offline` says the runtime was not online when the answer
// was produced. Cached implies offline, but offline does not imply cached — an
// offline runtime with nothing ever observed is the one case that needs both,
// and it is a state to name rather than a read that failed.
type hookReadResponse struct {
	ID         string                   `json:"id,omitempty"`
	RuntimeID  string                   `json:"runtime_id"`
	Status     HookReadStatus           `json:"status"`
	Cached     bool                     `json:"cached"`
	Offline    bool                     `json:"offline"`
	ObservedAt *time.Time               `json:"observed_at,omitempty"`
	Sources    []hookSourceResponse     `json:"sources,omitempty"`
	Resolved   *runtimehooks.Projection `json:"resolved,omitempty"`
	Error      string                   `json:"error,omitempty"`
	CreatedAt  *time.Time               `json:"created_at,omitempty"`
	UpdatedAt  *time.Time               `json:"updated_at,omitempty"`
}

// hookObservation is one read of the snapshot: the per-source states, the
// observation time that dates them, and the resolved per-entry projection.
// They travel together because rendering any one of them without the other
// two loses either the date or the not-checked scopes.
type hookObservation struct {
	Sources    []hookSourceResponse
	ObservedAt *time.Time
	Resolved   *runtimehooks.Projection
}

func nullableString(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func hookSourceResponses(provider string, rows []db.HookStateSnapshot) ([]hookSourceResponse, *time.Time, error) {
	expected, err := expectedHookSources(provider)
	if err != nil {
		return nil, nil, err
	}
	byKey := make(map[hookSourceKey]db.HookStateSnapshot, len(rows))
	var latest *time.Time
	for _, row := range rows {
		key := hookSourceKey{Scope: row.Scope, Format: row.Format}
		byKey[key] = row
		if row.ObservedAt.Valid && (latest == nil || row.ObservedAt.Time.After(*latest)) {
			observedAt := row.ObservedAt.Time
			latest = &observedAt
		}
	}
	sources := make([]hookSourceResponse, 0, len(expected))
	for _, key := range expected {
		response := hookSourceResponse{
			Provider: provider, Scope: key.Scope, Format: key.Format,
			State: hookSourceNotChecked,
		}
		if row, ok := byKey[key]; ok {
			response.State = hookSourceAbsent
			if row.SourcePath.Valid {
				response.State = hookSourceFound
			}
			response.SourcePath = nullableString(row.SourcePath)
			response.ContentHash = nullableString(row.ContentHash)
			response.Hooks = row.Hooks
			response.DisabledHooks = row.DisabledHooks
			if row.ObservedAt.Valid {
				observedAt := row.ObservedAt.Time
				response.ObservedAt = &observedAt
			}
		}
		sources = append(sources, response)
	}
	return sources, latest, nil
}

func (h *Handler) loadHookSnapshotResponse(ctx context.Context, runtimeID pgtype.UUID, provider string) (hookObservation, error) {
	rows, err := h.Queries.ListHookStateSnapshot(ctx, db.ListHookStateSnapshotParams{RuntimeID: runtimeID, Provider: provider})
	if err != nil {
		return hookObservation{}, fmt.Errorf("list hook snapshot: %w", err)
	}
	sources, observedAt, err := hookSourceResponses(provider, rows)
	if err != nil {
		return hookObservation{}, err
	}
	resolved := hookResolution(provider, rows)
	return hookObservation{Sources: sources, ObservedAt: observedAt, Resolved: &resolved}, nil
}

func (h *Handler) writeOfflineHookSnapshot(w http.ResponseWriter, r *http.Request, rt db.AgentRuntime) {
	observation, err := h.loadHookSnapshotResponse(r.Context(), rt.ID, rt.Provider)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load hook snapshot")
		return
	}
	status := HookReadCompleted
	errorMessage := ""
	if observation.ObservedAt == nil {
		status = HookReadFailed
		errorMessage = "runtime is offline and has no last known hook observation"
	}
	writeJSON(w, http.StatusOK, hookReadResponse{
		RuntimeID: uuidToString(rt.ID), Status: status, Cached: observation.ObservedAt != nil,
		Offline:    true,
		ObservedAt: observation.ObservedAt, Sources: observation.Sources,
		Resolved: observation.Resolved, Error: errorMessage,
	})
}

// InitiateHookRead always asks an online runtime to inspect its host. The
// snapshot is only a last-known fallback for an offline runtime.
func (h *Handler) InitiateHookRead(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, _, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeAPI, runtimeID)
	if !ok {
		return
	}
	if rt.Status != "online" {
		h.writeOfflineHookSnapshot(w, r, rt)
		return
	}
	if !runtimeHasCapability(rt.Metadata, protocol.DaemonCapabilityHooksV1) {
		writeError(w, http.StatusUpgradeRequired, "runtime does not support hook discovery")
		return
	}
	req, err := h.HookReadStore.Create(r.Context(), runtimeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue hook read")
		return
	}
	h.requestDaemonPendingWork(runtimeID, protocol.PendingWorkKindHookRead)
	writeJSON(w, http.StatusOK, hookReadResponse{
		ID: req.ID, RuntimeID: req.RuntimeID, Status: req.Status,
		CreatedAt: &req.CreatedAt, UpdatedAt: &req.UpdatedAt,
	})
}

// GetHookReadRequest reports lifecycle state and attaches the observation only
// after the daemon has completed the read. If the runtime went offline, the
// only valid answer is the dated last-known snapshot.
func (h *Handler) GetHookReadRequest(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, _, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeHookPoll, runtimeID)
	if !ok {
		return
	}
	if rt.Status != "online" {
		h.writeOfflineHookSnapshot(w, r, rt)
		return
	}
	requestID := chi.URLParam(r, "requestId")
	req, err := h.HookReadStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load hook read request")
		return
	}
	if req == nil || req.RuntimeID != runtimeID {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	response := hookReadResponse{
		ID: req.ID, RuntimeID: req.RuntimeID, Status: req.Status,
		ObservedAt: req.ObservedAt, Error: req.Error,
		CreatedAt: &req.CreatedAt, UpdatedAt: &req.UpdatedAt,
	}
	if req.Status == HookReadCompleted {
		observation, loadErr := h.loadHookSnapshotResponse(r.Context(), rt.ID, rt.Provider)
		if loadErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to load hook observation")
			return
		}
		if observation.ObservedAt == nil {
			writeError(w, http.StatusInternalServerError, "completed hook read has no dated observation")
			return
		}
		response.Sources = observation.Sources
		response.ObservedAt = observation.ObservedAt
		response.Resolved = observation.Resolved
	}
	writeJSON(w, http.StatusOK, response)
}

func expectedHookSources(provider string) ([]hookSourceKey, error) {
	switch provider {
	case "claude":
		return []hookSourceKey{{"user", "json"}, {"project", "json"}, {"local", "json"}}, nil
	case "codex":
		return []hookSourceKey{{"user", "json"}, {"user", "toml"}, {"project", "json"}, {"project", "toml"}}, nil
	default:
		return nil, fmt.Errorf("provider %q does not expose lifecycle hooks", provider)
	}
}

func validateHookSources(provider string, sources []protocol.HookConfigSource) (map[hookSourceKey]protocol.HookConfigSource, []hookSourceKey, error) {
	expected, err := expectedHookSources(provider)
	if err != nil {
		return nil, nil, err
	}
	allowed := make(map[hookSourceKey]struct{}, len(expected))
	for _, key := range expected {
		allowed[key] = struct{}{}
	}
	seen := make(map[hookSourceKey]protocol.HookConfigSource, len(sources))
	for _, source := range sources {
		key := hookSourceKey{source.Scope, source.Format}
		if source.Provider != provider {
			return nil, nil, fmt.Errorf("source provider %q does not match runtime provider %q", source.Provider, provider)
		}
		if _, ok := allowed[key]; !ok {
			return nil, nil, fmt.Errorf("unexpected %s source %s/%s", provider, source.Scope, source.Format)
		}
		if _, duplicate := seen[key]; duplicate {
			return nil, nil, fmt.Errorf("duplicate source %s/%s", source.Scope, source.Format)
		}
		if (source.SourcePath == nil) != (source.ContentHash == nil) {
			return nil, nil, fmt.Errorf("source_path and content_hash must both be present or both be null")
		}
		if source.ContentHash != nil {
			decoded, decodeErr := hex.DecodeString(*source.ContentHash)
			if decodeErr != nil || len(decoded) != 32 || *source.ContentHash != fmt.Sprintf("%x", decoded) {
				return nil, nil, fmt.Errorf("content_hash must be a lowercase sha256 hex digest")
			}
		}
		if err := validateHookJSON("hooks", source.Hooks); err != nil {
			return nil, nil, err
		}
		if err := validateHookJSON("disabled_hooks", source.DisabledHooks); err != nil {
			return nil, nil, err
		}
		seen[key] = source
	}
	return seen, expected, nil
}

func validateHookJSON(field string, raw json.RawMessage) error {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("%s must be valid JSON", field)
	}
	switch value.(type) {
	case map[string]any, []any:
		return nil
	default:
		return fmt.Errorf("%s must be a JSON object or array", field)
	}
}

func nullableText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

func (h *Handler) refreshHookSnapshot(ctx context.Context, runtimeID pgtype.UUID, provider string, observedAt time.Time, sources []protocol.HookConfigSource) error {
	// Keep this store-level guard in addition to the handler's 400-producing validation so no future caller can write unvalidated sources.
	seen, expected, err := validateHookSources(provider, sources)
	if err != nil {
		return err
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin hook snapshot refresh: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if _, err := qtx.LockAgentRuntime(ctx, runtimeID); err != nil {
		return fmt.Errorf("lock runtime for hook snapshot refresh: %w", err)
	}
	for _, key := range expected {
		source, present := seen[key]
		if !present {
			if err := qtx.DeleteHookStateSnapshotSource(ctx, db.DeleteHookStateSnapshotSourceParams{RuntimeID: runtimeID, Provider: provider, Scope: key.Scope, Format: key.Format}); err != nil {
				return fmt.Errorf("delete omitted hook source: %w", err)
			}
			continue
		}
		if err := qtx.UpsertHookStateSnapshot(ctx, db.UpsertHookStateSnapshotParams{
			RuntimeID: runtimeID, Provider: provider, Scope: source.Scope, Format: source.Format,
			Hooks: source.Hooks, DisabledHooks: source.DisabledHooks,
			SourcePath: nullableText(source.SourcePath), ContentHash: nullableText(source.ContentHash),
			ObservedAt: pgtype.Timestamptz{Time: observedAt, Valid: true},
		}); err != nil {
			return fmt.Errorf("upsert hook source %s/%s: %w", source.Scope, source.Format, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit hook snapshot refresh: %w", err)
	}
	return nil
}

// ReportHookReadResult is the sole write path into hook_state_snapshot.
func (h *Handler) ReportHookReadResult(w http.ResponseWriter, r *http.Request) {
	if !requestHasClientCapability(r, protocol.DaemonCapabilityHooksV1) {
		writeError(w, http.StatusUpgradeRequired, "hooks-v1 client capability required")
		return
	}
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}
	requestID := chi.URLParam(r, "requestId")
	req, err := h.HookReadStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load hook read request")
		return
	}
	if req == nil || req.RuntimeID != runtimeID {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if hookReadTerminal(req.Status) {
		slog.Debug("ignoring stale hook read report", "runtime_id", runtimeID, "request_id", requestID, "status", req.Status)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	var report protocol.HookConfigReadReport
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch report.Status {
	case "failed":
		if err := h.HookReadStore.Fail(r.Context(), requestID, report.Error); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to persist hook read failure")
			return
		}
	case "completed":
		observedAt, err := time.Parse(time.RFC3339Nano, report.ObservedAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "completed hook read requires a valid observed_at")
			return
		}
		if observedAt.After(time.Now().Add(maxHookObservationFutureSkew)) {
			writeError(w, http.StatusBadRequest, "observed_at must not be more than 5 minutes in the future")
			return
		}
		if _, _, err := validateHookSources(rt.Provider, report.Sources); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := h.refreshHookSnapshot(r.Context(), rt.ID, rt.Provider, observedAt, report.Sources); err != nil {
			slog.Error("hook snapshot refresh failed", "error", err, "runtime_id", runtimeID, "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "failed to refresh hook snapshot")
			return
		}
		if err := h.HookReadStore.Complete(r.Context(), requestID, observedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to persist hook read completion")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "status must be completed or failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
