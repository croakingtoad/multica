package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func hookReadWebHandler(t *testing.T, provider string) (*Handler, string, *InMemoryHookReadStore, *runtimeLocalSkillPendingWorkRecorder) {
	t.Helper()
	runtimeID := createProviderRuntime(t, provider)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent_runtime
		SET metadata = '{"capabilities":["hooks-v1"]}'::jsonb
		WHERE id = $1`, runtimeID); err != nil {
		t.Fatal(err)
	}
	store := NewInMemoryHookReadStore()
	recorder := &runtimeLocalSkillPendingWorkRecorder{}
	h := *testHandler
	h.HookReadStore = store
	h.DaemonPendingWork = recorder
	return &h, runtimeID, store, recorder
}

func decodeHookReadResponse(t *testing.T, w *httptest.ResponseRecorder) hookReadResponse {
	t.Helper()
	var response hookReadResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	return response
}

func TestInitiateHookReadOnlineAlwaysQueuesLiveDiscovery(t *testing.T) {
	h, runtimeID, store, recorder := hookReadWebHandler(t, "claude")
	oldObservedAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	if err := h.Queries.UpsertHookStateSnapshot(context.Background(), db.UpsertHookStateSnapshotParams{
		RuntimeID: parseUUID(runtimeID), Provider: "claude", Scope: "user", Format: "json",
		Hooks: []byte(`{}`), DisabledHooks: []byte(`{}`),
		ObservedAt: pgtype.Timestamptz{Time: oldObservedAt, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}

	req := withURLParams(newRequestAsUser(testUserID, http.MethodPost, "/api/runtimes/"+runtimeID+"/hooks", nil), "runtimeId", runtimeID)
	w := httptest.NewRecorder()
	h.InitiateHookRead(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	response := decodeHookReadResponse(t, w)
	if response.Status != HookReadPending || response.Cached || response.Offline || response.ObservedAt != nil || len(response.Sources) != 0 {
		t.Fatalf("online initiation served snapshot: %#v", response)
	}
	// The first answer already carries the phase bound, so the progress screen
	// never has to fill the gap with a number of its own.
	if response.PhaseTimeoutSeconds == nil {
		t.Fatalf("initiation did not advertise a phase bound: %#v", response)
	}
	stored, err := store.Get(context.Background(), response.ID)
	if err != nil || stored == nil {
		t.Fatalf("stored request = %#v, %v", stored, err)
	}
	if len(recorder.hints) != 1 || recorder.hints[0] != runtimeID+":"+protocol.PendingWorkKindHookRead {
		t.Fatalf("pending work hints = %#v", recorder.hints)
	}

	var observedAfter time.Time
	if err := testPool.QueryRow(context.Background(), `SELECT observed_at FROM hook_state_snapshot WHERE runtime_id=$1 AND scope='user' AND format='json'`, runtimeID).Scan(&observedAfter); err != nil {
		t.Fatal(err)
	}
	if !observedAfter.Equal(oldObservedAt) {
		t.Fatalf("initiate wrote snapshot observed_at = %s, want %s", observedAfter, oldObservedAt)
	}
}

func TestInitiateHookReadOfflineReturnsDatedSnapshotWithThreeSourceStates(t *testing.T) {
	h, runtimeID, _, _ := hookReadWebHandler(t, "codex")
	observedAt := time.Now().UTC().Truncate(time.Microsecond)
	path := "/home/test/.codex/hooks.json"
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, row := range []db.UpsertHookStateSnapshotParams{
		{RuntimeID: parseUUID(runtimeID), Provider: "codex", Scope: "user", Format: "json", Hooks: []byte(`{"found":true}`), DisabledHooks: []byte(`{}`), SourcePath: pgtype.Text{String: path, Valid: true}, ContentHash: pgtype.Text{String: hash, Valid: true}, ObservedAt: pgtype.Timestamptz{Time: observedAt, Valid: true}},
		{RuntimeID: parseUUID(runtimeID), Provider: "codex", Scope: "user", Format: "toml", Hooks: []byte(`{}`), DisabledHooks: []byte(`{}`), ObservedAt: pgtype.Timestamptz{Time: observedAt, Valid: true}},
	} {
		if err := h.Queries.UpsertHookStateSnapshot(context.Background(), row); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_runtime SET status='offline' WHERE id=$1`, runtimeID); err != nil {
		t.Fatal(err)
	}

	req := withURLParams(newRequestAsUser(testUserID, http.MethodPost, "/api/runtimes/"+runtimeID+"/hooks", nil), "runtimeId", runtimeID)
	w := httptest.NewRecorder()
	h.InitiateHookRead(w, req)
	response := decodeHookReadResponse(t, w)
	if w.Code != http.StatusOK || response.Status != HookReadCompleted || !response.Cached || !response.Offline || response.ObservedAt == nil || !response.ObservedAt.Equal(observedAt) {
		t.Fatalf("offline response = %#v, HTTP %d", response, w.Code)
	}
	if len(response.Sources) != 4 {
		t.Fatalf("sources = %#v, want four Codex source identities", response.Sources)
	}
	wantStates := []hookSourceState{hookSourceFound, hookSourceAbsent, hookSourceNotChecked, hookSourceNotChecked}
	for i, want := range wantStates {
		if response.Sources[i].State != want {
			t.Errorf("source %d state = %q, want %q", i, response.Sources[i].State, want)
		}
	}
	if response.Sources[0].SourcePath == nil || *response.Sources[0].SourcePath != path || response.Sources[1].SourcePath != nil {
		t.Fatalf("source identities = %#v", response.Sources)
	}
}

// An offline runtime with nothing ever observed is a state the UI has to name
// — "offline, and there is no last known snapshot" — not a read that failed on
// the way out. `offline` is the bit that separates the two, because `cached` is
// false in both cases and matching on the error prose would not survive a
// reword.
func TestOfflineHookReadWithoutSnapshotIsOfflineAndUncached(t *testing.T) {
	h, runtimeID, _, _ := hookReadWebHandler(t, "claude")
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_runtime SET status='offline' WHERE id=$1`, runtimeID); err != nil {
		t.Fatal(err)
	}

	req := withURLParams(newRequestAsUser(testUserID, http.MethodPost, "/api/runtimes/"+runtimeID+"/hooks", nil), "runtimeId", runtimeID)
	w := httptest.NewRecorder()
	h.InitiateHookRead(w, req)
	response := decodeHookReadResponse(t, w)
	if w.Code != http.StatusOK || response.Status != HookReadFailed {
		t.Fatalf("offline response = %#v, HTTP %d", response, w.Code)
	}
	if !response.Offline || response.Cached || response.ObservedAt != nil {
		t.Fatalf("offline-without-snapshot must be offline, uncached and undated: %#v", response)
	}
	if response.Error == "" {
		t.Fatalf("offline-without-snapshot response carries no reason: %#v", response)
	}
}

func TestGetHookReadRequestSurfacesProgressCompletionAndTimeout(t *testing.T) {
	h, runtimeID, store, _ := hookReadWebHandler(t, "claude")
	req, err := store.Create(context.Background(), runtimeID)
	if err != nil {
		t.Fatal(err)
	}
	poll := func() hookReadResponse {
		r := withURLParams(newRequestAsUser(testUserID, http.MethodGet, "/api/runtimes/"+runtimeID+"/hooks/"+req.ID, nil), "runtimeId", runtimeID, "requestId", req.ID)
		w := httptest.NewRecorder()
		h.GetHookReadRequest(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
		}
		return decodeHookReadResponse(t, w)
	}
	if response := poll(); response.Status != HookReadPending || len(response.Sources) != 0 {
		t.Fatalf("pending response = %#v", response)
	}

	observedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := h.Queries.UpsertHookStateSnapshot(context.Background(), db.UpsertHookStateSnapshotParams{
		RuntimeID: parseUUID(runtimeID), Provider: "claude", Scope: "user", Format: "json",
		Hooks: []byte(`{}`), DisabledHooks: []byte(`{}`), ObservedAt: pgtype.Timestamptz{Time: observedAt, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Complete(context.Background(), req.ID, observedAt); err != nil {
		t.Fatal(err)
	}
	if response := poll(); response.Status != HookReadCompleted || response.Cached || response.ObservedAt == nil || len(response.Sources) != 3 {
		t.Fatalf("completed response = %#v", response)
	}

	timedOut, err := store.Create(context.Background(), runtimeID)
	if err != nil {
		t.Fatal(err)
	}
	timedOut.CreatedAt = time.Now().Add(-hookReadPendingTimeout - time.Second)
	req = timedOut
	response := poll()
	if response.Status != HookReadTimedOut || response.ObservedAt != nil || response.Error != "daemon did not answer within 30 seconds" {
		t.Fatalf("timed-out response = %#v", response)
	}
}

func TestHookReadLifecycleDistinguishesTimeoutFromEmptyObservation(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryHookReadStore()
	timedOut, err := store.Create(ctx, "runtime-1")
	if err != nil {
		t.Fatal(err)
	}
	timedOut.CreatedAt = time.Now().Add(-hookReadPendingTimeout - time.Second)
	timedOut, err = store.Get(ctx, timedOut.ID)
	if err != nil {
		t.Fatal(err)
	}
	if timedOut.Status != HookReadTimedOut || timedOut.ObservedAt != nil {
		t.Fatalf("timed-out request = %#v", timedOut)
	}

	running, err := store.Create(ctx, "runtime-1")
	if err != nil {
		t.Fatal(err)
	}
	running, err = store.PopPending(ctx, "runtime-1")
	if err != nil {
		t.Fatal(err)
	}
	startedAt := time.Now().Add(-hookReadRunningTimeout - time.Second)
	running.RunStartedAt = &startedAt
	running, err = store.Get(ctx, running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if running.Status != HookReadTimedOut || running.ObservedAt != nil {
		t.Fatalf("running timed-out request = %#v", running)
	}

	empty, err := store.Create(ctx, "runtime-1")
	if err != nil {
		t.Fatal(err)
	}
	observedAt := time.Now().UTC()
	if err := store.Complete(ctx, empty.ID, observedAt); err != nil {
		t.Fatal(err)
	}
	empty, err = store.Get(ctx, empty.ID)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Status != HookReadCompleted || empty.ObservedAt == nil || !empty.ObservedAt.Equal(observedAt) {
		t.Fatalf("empty completed observation = %#v", empty)
	}
}

func TestRedisHookReadStoreLifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewRedisHookReadStore(newRedisTestClient(t))
	req, err := store.Create(ctx, "runtime-1")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := store.HasPending(ctx, "runtime-1")
	if err != nil || !pending {
		t.Fatalf("HasPending = %v, %v", pending, err)
	}
	claimed, err := store.PopPending(ctx, "runtime-1")
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.ID != req.ID || claimed.Status != HookReadRunning || claimed.RunStartedAt == nil {
		t.Fatalf("claimed request = %#v", claimed)
	}
	observedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := store.Complete(ctx, req.ID, observedAt); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Status != HookReadCompleted || got.ObservedAt == nil || !got.ObservedAt.Equal(observedAt) {
		t.Fatalf("completed request = %#v", got)
	}
}

func TestHeartbeatDispatchesPendingHookRead(t *testing.T) {
	ctx := context.Background()
	h := &Handler{
		UpdateStore: NewInMemoryUpdateStore(), ModelListStore: NewInMemoryModelListStore(),
		HookReadStore: NewInMemoryHookReadStore(), LocalSkillListStore: NewInMemoryLocalSkillListStore(),
		LocalSkillImportStore: NewInMemoryLocalSkillImportStore(),
	}
	req, err := h.HookReadStore.Create(ctx, "runtime-1")
	if err != nil {
		t.Fatal(err)
	}
	ack, _, err := h.processHeartbeat(ctx, "runtime-1", true)
	if err != nil {
		t.Fatal(err)
	}
	if ack.PendingHookRead == nil || ack.PendingHookRead.ID != req.ID {
		t.Fatalf("pending hook read = %#v, want %s", ack.PendingHookRead, req.ID)
	}
}

func TestReportHookReadResultRequiresHooksCapability(t *testing.T) {
	req := withURLParams(newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/runtime/hooks/request/result", nil, testWorkspaceID, "daemon"),
		"runtimeId", "runtime", "requestId", "request")
	w := httptest.NewRecorder()
	(&Handler{}).ReportHookReadResult(w, req)
	if w.Code != http.StatusUpgradeRequired {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUpgradeRequired, w.Body.String())
	}
}

func TestReportHookReadResultBoundsFutureObservedAt(t *testing.T) {
	h, runtimeID, store, _ := hookReadWebHandler(t, "claude")
	tests := []struct {
		name       string
		offset     time.Duration
		wantStatus int
		wantError  string
	}{
		{
			name:       "far future rejected",
			offset:     100 * 365 * 24 * time.Hour,
			wantStatus: http.StatusBadRequest,
			wantError:  "observed_at must not be more than 5 minutes in the future",
		},
		{
			name:       "clock skew tolerated",
			offset:     4 * time.Minute,
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request, err := store.Create(t.Context(), runtimeID)
			if err != nil {
				t.Fatal(err)
			}
			observedAt := time.Now().UTC().Add(tt.offset).Truncate(time.Microsecond)
			report := protocol.HookConfigReadReport{
				Status: "completed", ObservedAt: observedAt.Format(time.RFC3339Nano), Sources: []protocol.HookConfigSource{},
			}
			req := withURLParams(newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/hooks/"+request.ID+"/result", report, testWorkspaceID, "daemon"),
				"runtimeId", runtimeID, "requestId", request.ID)
			req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityHooksV1)

			response := testutil.Call(t, h.ReportHookReadResult, req).Want(tt.wantStatus)
			if tt.wantError != "" {
				if got := response.Map()["error"]; got != tt.wantError {
					t.Fatalf("error = %q, want %q", got, tt.wantError)
				}
				return
			}

			stored, err := store.Get(t.Context(), request.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored == nil || stored.ObservedAt == nil || !stored.ObservedAt.Equal(observedAt) {
				t.Fatalf("stored observed_at = %#v, want original host value %s", stored, observedAt)
			}
		})
	}
}

func TestReportHookReadResultPersistsCodexSourcesAndOmissionState(t *testing.T) {
	ctx := context.Background()
	runtimeID := createProviderRuntime(t, "codex")
	originalStore := testHandler.HookReadStore
	store := NewInMemoryHookReadStore()
	testHandler.HookReadStore = store
	t.Cleanup(func() { testHandler.HookReadStore = originalStore })

	runtimeUUID := parseUUID(runtimeID)
	oldHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := testHandler.Queries.UpsertHookStateSnapshot(ctx, db.UpsertHookStateSnapshotParams{
		RuntimeID: runtimeUUID, Provider: "codex", Scope: "project", Format: "json",
		Hooks: []byte(`{"old":true}`), DisabledHooks: []byte(`{}`),
		SourcePath:  pgtype.Text{String: "/old/project/hooks.json", Valid: true},
		ContentHash: pgtype.Text{String: oldHash, Valid: true},
		ObservedAt:  pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true},
	}); err != nil {
		t.Fatal(err)
	}

	request, err := store.Create(ctx, runtimeID)
	if err != nil {
		t.Fatal(err)
	}
	observedAt := time.Now().UTC().Truncate(time.Microsecond)
	tomlPath := "/home/test/.codex/config.toml"
	tomlHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	report := protocol.HookConfigReadReport{Status: "completed", ObservedAt: observedAt.Format(time.RFC3339Nano), Sources: []protocol.HookConfigSource{
		{Provider: "codex", Scope: "user", Format: "json", Hooks: []byte(`{}`), DisabledHooks: []byte(`{}`)},
		{Provider: "codex", Scope: "user", Format: "toml", SourcePath: &tomlPath, ContentHash: &tomlHash, Hooks: []byte(`{"SessionStart":[{"command":"ok"}]}`), DisabledHooks: []byte(`{}`)},
	}}
	req := withURLParams(newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/hooks/"+request.ID+"/result", report, testWorkspaceID, "daemon"),
		"runtimeId", runtimeID, "requestId", request.ID)
	req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityHooksV1)
	w := httptest.NewRecorder()
	testHandler.ReportHookReadResult(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	rows, err := testPool.Query(ctx, `
		SELECT scope, format, source_path, content_hash, observed_at
		FROM hook_state_snapshot WHERE runtime_id = $1 ORDER BY scope, format`, runtimeID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type snapshot struct {
		scope, format string
		path, hash    pgtype.Text
		observedAt    time.Time
	}
	var got []snapshot
	for rows.Next() {
		var row snapshot
		if err := rows.Scan(&row.scope, &row.format, &row.path, &row.hash, &row.observedAt); err != nil {
			t.Fatal(err)
		}
		got = append(got, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("snapshot rows = %#v, want exactly two reported user sources", got)
	}
	if got[0].scope != "user" || got[0].format != "json" || got[0].path.Valid || got[0].hash.Valid {
		t.Fatalf("checked-absent JSON row = %#v", got[0])
	}
	if got[1].format != "toml" || got[1].path.String != tomlPath || got[1].hash.String != tomlHash || !got[1].observedAt.Equal(observedAt) {
		t.Fatalf("populated TOML row = %#v", got[1])
	}

	newHash := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	if err := testHandler.Queries.UpsertHookStateSnapshot(ctx, db.UpsertHookStateSnapshotParams{
		RuntimeID: runtimeUUID, Provider: "codex", Scope: "user", Format: "json",
		Hooks: []byte(`{"changed":true}`), DisabledHooks: []byte(`{}`),
		SourcePath:  pgtype.Text{String: "/home/test/.codex/hooks.json", Valid: true},
		ContentHash: pgtype.Text{String: newHash, Valid: true},
		ObservedAt:  pgtype.Timestamptz{Time: observedAt.Add(time.Second), Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	var siblingHash string
	if err := testPool.QueryRow(ctx, `SELECT content_hash FROM hook_state_snapshot WHERE runtime_id=$1 AND provider='codex' AND scope='user' AND format='toml'`, runtimeID).Scan(&siblingHash); err != nil {
		t.Fatal(err)
	}
	if siblingHash != tomlHash {
		t.Fatalf("JSON upsert changed TOML sibling hash to %q", siblingHash)
	}
}

// R1: the bound the poll response advertises has to be the bound the server
// actually enforces. The assertion deliberately reads the number out of the
// response and drives applyHookReadTimeout with it rather than comparing
// against 30 or 60 — a literal here would pass against a client-side copy of
// the constant, which is the defect this field exists to remove.
func TestHookReadResponseAdvertisesTheTimeoutItEnforces(t *testing.T) {
	h, runtimeID, store, _ := hookReadWebHandler(t, "claude")
	ctx := context.Background()

	poll := func(requestID string) hookReadResponse {
		r := withURLParams(newRequestAsUser(testUserID, http.MethodGet, "/api/runtimes/"+runtimeID+"/hooks/"+requestID, nil), "runtimeId", runtimeID, "requestId", requestID)
		w := httptest.NewRecorder()
		h.GetHookReadRequest(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
		}
		return decodeHookReadResponse(t, w)
	}

	// Pending: the advertised bound runs from CreatedAt.
	pending, err := store.Create(ctx, runtimeID)
	if err != nil {
		t.Fatal(err)
	}
	response := poll(pending.ID)
	if response.Status != HookReadPending || response.PhaseTimeoutSeconds == nil {
		t.Fatalf("pending response = %#v", response)
	}
	pendingBound := time.Duration(*response.PhaseTimeoutSeconds) * time.Second

	now := time.Now()
	inside := &HookReadRequest{Status: HookReadPending, CreatedAt: now.Add(-pendingBound + time.Second)}
	if applyHookReadTimeout(inside, now) {
		t.Fatalf("pending read ended before its advertised %s bound", pendingBound)
	}
	outside := &HookReadRequest{Status: HookReadPending, CreatedAt: now.Add(-pendingBound - time.Second)}
	if !applyHookReadTimeout(outside, now) || outside.Status != HookReadTimedOut {
		t.Fatalf("pending read survived past its advertised %s bound: %#v", pendingBound, outside)
	}

	// Running: a different phase with a different bound, measured from the
	// daemon's own start rather than from the queue. The first request is
	// retired first so PopPending, which takes the oldest pending one, hands
	// back the request this half of the test is about.
	if err := store.Fail(ctx, pending.ID, "retired by the test"); err != nil {
		t.Fatal(err)
	}
	running, err := store.Create(ctx, runtimeID)
	if err != nil {
		t.Fatal(err)
	}
	popped, err := store.PopPending(ctx, runtimeID)
	if err != nil {
		t.Fatal(err)
	}
	if popped == nil || popped.ID != running.ID {
		t.Fatalf("popped %#v, want the request just created", popped)
	}
	response = poll(running.ID)
	if response.Status != HookReadRunning || response.PhaseTimeoutSeconds == nil {
		t.Fatalf("running response = %#v", response)
	}
	runningBound := time.Duration(*response.PhaseTimeoutSeconds) * time.Second
	if runningBound == pendingBound {
		t.Fatalf("phases advertise one shared bound (%s); the per-phase claim is then untested", runningBound)
	}

	startedInside := now.Add(-runningBound + time.Second)
	inside = &HookReadRequest{Status: HookReadRunning, RunStartedAt: &startedInside}
	if applyHookReadTimeout(inside, now) {
		t.Fatalf("running read ended before its advertised %s bound", runningBound)
	}
	startedOutside := now.Add(-runningBound - time.Second)
	outside = &HookReadRequest{Status: HookReadRunning, RunStartedAt: &startedOutside}
	if !applyHookReadTimeout(outside, now) || outside.Status != HookReadTimedOut {
		t.Fatalf("running read survived past its advertised %s bound: %#v", runningBound, outside)
	}

	// Terminal reads have no bound left to advertise, so the screen has no
	// number to show rather than a stale one.
	done, err := store.Create(ctx, runtimeID)
	if err != nil {
		t.Fatal(err)
	}
	observedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := h.Queries.UpsertHookStateSnapshot(ctx, db.UpsertHookStateSnapshotParams{
		RuntimeID: parseUUID(runtimeID), Provider: "claude", Scope: "user", Format: "json",
		Hooks: []byte(`{}`), DisabledHooks: []byte(`{}`), ObservedAt: pgtype.Timestamptz{Time: observedAt, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Complete(ctx, done.ID, observedAt); err != nil {
		t.Fatal(err)
	}
	if response := poll(done.ID); response.PhaseTimeoutSeconds != nil {
		t.Fatalf("completed response advertises a bound: %#v", response)
	}
}
