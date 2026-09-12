package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	maxHookFireBatchSize = 500
	maxHookFireBodyBytes = 4 << 20
)

type validatedHookFire struct {
	id       pgtype.UUID
	firedAt  time.Time
	hookSpec []byte
	detail   []byte
	fire     protocol.HookFire
}

func validateHookFireJSON(field string, raw json.RawMessage) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("%s must be valid JSON", field)
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, fmt.Errorf("%s must be a JSON object", field)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", field, err)
	}
	return canonical, nil
}

func validateHookFire(fire protocol.HookFire, now time.Time) (validatedHookFire, error) {
	var id pgtype.UUID
	if err := id.Scan(fire.ID); err != nil || !id.Valid {
		return validatedHookFire{}, fmt.Errorf("id must be a UUID")
	}
	if strings.TrimSpace(fire.Event) == "" || len(fire.Event) > 128 {
		return validatedHookFire{}, fmt.Errorf("event must be between 1 and 128 characters")
	}
	if strings.TrimSpace(fire.HookID) == "" || len(fire.HookID) > 256 {
		return validatedHookFire{}, fmt.Errorf("hook_id must be between 1 and 256 characters")
	}
	hookSpec, err := validateHookFireJSON("hook_spec", fire.HookSpec)
	if err != nil {
		return validatedHookFire{}, err
	}
	detail, err := validateHookFireJSON("detail", fire.Detail)
	if err != nil {
		return validatedHookFire{}, err
	}
	firedAt, err := time.Parse(time.RFC3339Nano, fire.FiredAt)
	if err != nil {
		return validatedHookFire{}, fmt.Errorf("fired_at must be an RFC3339 timestamp")
	}
	if firedAt.Before(time.Unix(0, 0)) {
		return validatedHookFire{}, fmt.Errorf("fired_at must not predate the Unix epoch")
	}
	if firedAt.After(now.Add(maxHookObservationFutureSkew)) {
		return validatedHookFire{}, fmt.Errorf("fired_at must not be more than 5 minutes in the future")
	}
	switch fire.Outcome {
	case "success", "failure", "blocked", "skipped", "unknown":
	default:
		return validatedHookFire{}, fmt.Errorf("outcome is not recognized")
	}
	return validatedHookFire{id: id, firedAt: firedAt, hookSpec: hookSpec, detail: detail, fire: fire}, nil
}

// ReportHookFires is the sole Claude debug-log write path into the
// server-authoritative hook_fire_history table. Runtime identity and
// provenance come from the authenticated route, never from host-supplied
// fields.
func (h *Handler) ReportHookFires(w http.ResponseWriter, r *http.Request) {
	if !requestHasClientCapability(r, protocol.DaemonCapabilityHooksV1) {
		writeError(w, http.StatusUpgradeRequired, "hooks-v1 client capability required")
		return
	}
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}
	if rt.Provider != "claude" {
		writeError(w, http.StatusBadRequest, "debug-log hook fires require a claude runtime")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxHookFireBodyBytes)
	var report protocol.HookFireReport
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(report.Fires) > maxHookFireBatchSize {
		writeError(w, http.StatusBadRequest, "too many hook fires in one report")
		return
	}
	validated := make([]validatedHookFire, len(report.Fires))
	now := time.Now()
	for i, fire := range report.Fires {
		value, err := validateHookFire(fire, now)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("fires[%d]: %s", i, err))
			return
		}
		validated[i] = value
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin hook fire report")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if _, err := qtx.LockAgentRuntime(r.Context(), rt.ID); err != nil {
		slog.Error("hook fire runtime lock failed", "runtime_id", runtimeID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to lock runtime for hook fires")
		return
	}
	inserted := int64(0)
	for _, value := range validated {
		rows, err := qtx.InsertHookFireHistory(r.Context(), db.InsertHookFireHistoryParams{
			ID: value.id, RuntimeID: rt.ID, Provider: "claude", Event: value.fire.Event,
			HookID: value.fire.HookID, HookSpec: value.hookSpec,
			FiredAt:    pgtype.Timestamptz{Time: value.firedAt, Valid: true},
			Provenance: "debug_log", Outcome: value.fire.Outcome, Detail: value.detail,
		})
		if err != nil {
			slog.Error("hook fire insert failed", "runtime_id", runtimeID, "fire_id", value.fire.ID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to persist hook fires")
			return
		}
		inserted += rows
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit hook fires")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"inserted": inserted})
}
