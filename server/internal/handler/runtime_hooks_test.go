package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

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
