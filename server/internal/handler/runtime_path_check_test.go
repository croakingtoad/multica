package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func createPathCheckTestRuntime(t *testing.T, ownerID, daemonID, status string) string {
	return createPathCheckTestRuntimeForProvider(t, ownerID, daemonID, "claude", status, time.Now())
}

func createPathCheckTestRuntimeForProvider(t *testing.T, ownerID, daemonID, provider, status string, lastSeenAt time.Time) string {
	t.Helper()

	runtimeName := fmt.Sprintf("path-check-%d", time.Now().UnixNano())

	var runtimeID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, $2, $3, 'local', $4, $5, 'Path Check Test', '{}'::jsonb, $6, $7)
		RETURNING id
	`, testWorkspaceID, daemonID, runtimeName, provider, status, ownerID, lastSeenAt).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create path check runtime: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	return runtimeID
}

// Regression: polling must authorize against every provider runtime the owner
// has registered under the daemon, not only the most recently seen runtime.
func TestDaemonPathCheck_OwnerPollsOnlineProviderWhenNewestProviderIsOffline(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	const daemonID = "path-check-multi-provider-daemon"
	now := time.Now()
	onlineRuntimeID := createPathCheckTestRuntimeForProvider(t, testUserID, daemonID, "codex", "online", now.Add(-time.Minute))
	createPathCheckTestRuntimeForProvider(t, testUserID, daemonID, "claude", "offline", now)

	w, initBody := initiatePathCheck(t, testHandler, testUserID, testWorkspaceID, daemonID, "/tmp/multi-provider-project")
	if w.Code != http.StatusOK {
		t.Fatalf("initiate: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if initBody["runtime_id"] != onlineRuntimeID {
		t.Fatalf("initiate runtime_id = %v, want online runtime %s", initBody["runtime_id"], onlineRuntimeID)
	}

	requestID, _ := initBody["id"].(string)
	wp, pollBody := pollPathCheck(t, testHandler, testUserID, testWorkspaceID, daemonID, requestID)
	if wp.Code != http.StatusOK {
		t.Fatalf("poll: expected 200, got %d: %s", wp.Code, wp.Body.String())
	}
	if pollBody["runtime_id"] != onlineRuntimeID {
		t.Fatalf("poll runtime_id = %v, want %s", pollBody["runtime_id"], onlineRuntimeID)
	}
}

func initiatePathCheck(t *testing.T, h *Handler, userID, workspaceID, daemonID, path string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()

	req := withURLParams(
		newRequestAsUser(userID, http.MethodPost, "/api/workspaces/"+workspaceID+"/daemons/"+daemonID+"/path-checks", map[string]any{
			"path": path,
		}),
		"workspaceId", workspaceID,
		"daemonId", daemonID,
	)
	w := httptest.NewRecorder()
	h.InitiateDaemonPathCheck(w, req)
	if w.Code == http.StatusOK {
		var body map[string]any
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("decode initiate response: %v", err)
		}
		return w, body
	}
	return w, nil
}

func pollPathCheck(t *testing.T, h *Handler, userID, workspaceID, daemonID, requestID string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()

	req := withURLParams(
		newRequestAsUser(userID, http.MethodGet, "/api/workspaces/"+workspaceID+"/daemons/"+daemonID+"/path-checks/"+requestID, nil),
		"workspaceId", workspaceID,
		"daemonId", daemonID,
		"requestId", requestID,
	)
	w := httptest.NewRecorder()
	h.GetDaemonPathCheck(w, req)
	var body map[string]any
	if w.Code == http.StatusOK {
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("decode poll response: %v", err)
		}
	}
	return w, body
}

func reportPathCheck(t *testing.T, h *Handler, runtimeID, requestID string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()

	req := withURLParams(
		newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/path-checks/"+requestID+"/result", payload, testWorkspaceID, "path-check-daemon-token"),
		"runtimeId", runtimeID,
		"requestId", requestID,
	)
	w := httptest.NewRecorder()
	h.ReportDaemonPathCheckResult(w, req)
	return w
}

// Acceptance: the full initiate → heartbeat claim → report → poll round trip
// on the daemon-token boundary, with the pending-work hint fired.
func TestDaemonPathCheck_RoundTrip(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	const daemonID = "path-check-roundtrip-daemon"
	runtimeID := createPathCheckTestRuntime(t, testUserID, daemonID, "online")

	recorder := &runtimeLocalSkillPendingWorkRecorder{}
	h := *testHandler
	h.DaemonPendingWork = recorder

	w, initBody := initiatePathCheck(t, &h, testUserID, testWorkspaceID, daemonID, "/tmp/some-project")
	if w.Code != http.StatusOK {
		t.Fatalf("initiate: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	requestID, _ := initBody["id"].(string)
	if requestID == "" {
		t.Fatalf("initiate response missing id: %v", initBody)
	}
	if initBody["status"] != "pending" {
		t.Fatalf("initiate status = %v, want pending", initBody["status"])
	}
	if initBody["path"] != "/tmp/some-project" {
		t.Fatalf("initiate path = %v", initBody["path"])
	}
	if initBody["runtime_id"] != runtimeID {
		t.Fatalf("initiate runtime_id = %v, want %s", initBody["runtime_id"], runtimeID)
	}

	// The hint wakes the daemon for the chosen runtime.
	wantHint := runtimeID + ":" + protocol.PendingWorkKindPathCheck
	if len(recorder.hints) != 1 || recorder.hints[0] != wantHint {
		t.Fatalf("pending-work hints = %v, want [%s]", recorder.hints, wantHint)
	}

	// The heartbeat claims the pending check and hands the daemon the path.
	heartbeatReq := newDaemonTokenRequest(http.MethodPost, "/api/daemon/heartbeat", map[string]any{
		"runtime_id": runtimeID,
	}, testWorkspaceID, daemonID)
	wb := httptest.NewRecorder()
	h.DaemonHeartbeat(wb, heartbeatReq)
	if wb.Code != http.StatusOK {
		t.Fatalf("heartbeat: expected 200, got %d: %s", wb.Code, wb.Body.String())
	}
	var heartbeatResp map[string]any
	if err := json.NewDecoder(wb.Body).Decode(&heartbeatResp); err != nil {
		t.Fatalf("decode heartbeat: %v", err)
	}
	pending, ok := heartbeatResp["pending_path_check"].(map[string]any)
	if !ok {
		t.Fatalf("heartbeat missing pending_path_check: %v", heartbeatResp)
	}
	if pending["id"] != requestID || pending["path"] != "/tmp/some-project" {
		t.Fatalf("pending_path_check = %v, want id %s path /tmp/some-project", pending, requestID)
	}

	// The daemon reports the verdict.
	wr := reportPathCheck(t, &h, runtimeID, requestID, map[string]any{
		"status":       "completed",
		"exists":       true,
		"is_directory": true,
		"readable":     true,
		"writable":     true,
		"is_git_repo":  true,
		"reason":       "",
	})
	if wr.Code != http.StatusOK {
		t.Fatalf("report: expected 200, got %d: %s", wr.Code, wr.Body.String())
	}

	// The owner polls the result.
	wp, pollBody := pollPathCheck(t, &h, testUserID, testWorkspaceID, daemonID, requestID)
	if wp.Code != http.StatusOK {
		t.Fatalf("poll: expected 200, got %d: %s", wp.Code, wp.Body.String())
	}
	if pollBody["status"] != "completed" {
		t.Fatalf("poll status = %v, want completed", pollBody["status"])
	}
	for _, field := range []string{"exists", "is_directory", "readable", "writable", "is_git_repo"} {
		if pollBody[field] != true {
			t.Fatalf("poll %s = %v, want true", field, pollBody[field])
		}
	}
	if _, present := pollBody["reason"]; present {
		t.Fatalf("poll response must omit empty reason: %v", pollBody)
	}
}

// Acceptance: a path check issued against a daemon the caller does not own
// — even by a workspace admin — is invisible: no initiate, no poll of a real
// request, no result.
func TestDaemonPathCheck_SecondUserCannotPathCheck(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	const ownerDaemon = "path-check-owner-daemon"
	createPathCheckTestRuntime(t, testUserID, ownerDaemon, "online")
	adminID := createRuntimeLocalSkillTestMember(t, "admin")

	// A different daemon in the same workspace (the admin's own machine).
	const adminDaemon = "path-check-admin-daemon"
	createPathCheckTestRuntime(t, adminID, adminDaemon, "online")

	// The admin cannot initiate a check on the owner's daemon...
	w, _ := initiatePathCheck(t, testHandler, adminID, testWorkspaceID, ownerDaemon, "/tmp/owner-project")
	if w.Code != http.StatusNotFound {
		t.Fatalf("admin initiate on owner daemon: expected 404, got %d: %s", w.Code, w.Body.String())
	}
	// ...even against an unknown daemon id in the same workspace.
	w, _ = initiatePathCheck(t, testHandler, adminID, testWorkspaceID, "no-such-daemon", "/tmp/x")
	if w.Code != http.StatusNotFound {
		t.Fatalf("initiate on unknown daemon: expected 404, got %d: %s", w.Code, w.Body.String())
	}
	// ...and cannot poll the owner's real request.
	wo, initBody := initiatePathCheck(t, testHandler, testUserID, testWorkspaceID, ownerDaemon, "/tmp/owner-project")
	if wo.Code != http.StatusOK {
		t.Fatalf("owner initiate: expected 200, got %d: %s", wo.Code, wo.Body.String())
	}
	requestID, _ := initBody["id"].(string)
	wa, _ := pollPathCheck(t, testHandler, adminID, testWorkspaceID, ownerDaemon, requestID)
	if wa.Code != http.StatusNotFound {
		t.Fatalf("admin poll of owner request: expected 404, got %d: %s", wa.Code, wa.Body.String())
	}
	// ...and the report endpoint stays bound to the request's own runtime:
	// a daemon token for the admin's runtime cannot report the owner's check.
	wr := reportPathCheckWithDaemon(t, testHandler, adminDaemon, requestID, map[string]any{"status": "completed"})
	if wr.Code != http.StatusNotFound {
		t.Fatalf("cross-daemon report: expected 404, got %d: %s", wr.Code, wr.Body.String())
	}

	// The owner's request is still pending and readable by the owner.
	wp, pollBody := pollPathCheck(t, testHandler, testUserID, testWorkspaceID, ownerDaemon, requestID)
	if wp.Code != http.StatusOK || pollBody["status"] != "pending" {
		t.Fatalf("owner poll after cross-daemon attempts: %d %v", wp.Code, pollBody)
	}
}

func reportPathCheckWithDaemon(t *testing.T, h *Handler, daemonID, requestID string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()

	// The daemon token is workspace-scoped; the runtime id in the URL must
	// belong to that workspace, so resolve a runtime under the daemon first.
	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id FROM agent_runtime
		WHERE workspace_id = $1 AND daemon_id = $2
		LIMIT 1
	`, testWorkspaceID, daemonID).Scan(&runtimeID); err != nil {
		t.Fatalf("find runtime for daemon %s: %v", daemonID, err)
	}

	req := withURLParams(
		newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/path-checks/"+requestID+"/result", payload, testWorkspaceID, daemonID),
		"runtimeId", runtimeID,
		"requestId", requestID,
	)
	w := httptest.NewRecorder()
	h.ReportDaemonPathCheckResult(w, req)
	return w
}

// Acceptance: non-absolute and empty paths are rejected at the API boundary
// with a reason, without dispatching to the daemon (no store record, no hint).
func TestDaemonPathCheck_RejectsNonAbsolutePath(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	const daemonID = "path-check-validation-daemon"
	createPathCheckTestRuntime(t, testUserID, daemonID, "online")

	recorder := &runtimeLocalSkillPendingWorkRecorder{}
	h := *testHandler
	h.DaemonPendingWork = recorder

	for _, path := range []string{"", "   ", "relative/path", "~/home/user/project", "C:relative"} {
		req := withURLParams(
			newRequestAsUser(testUserID, http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/daemons/"+daemonID+"/path-checks", map[string]any{
				"path": path,
			}),
			"workspaceId", testWorkspaceID,
			"daemonId", daemonID,
		)
		w := httptest.NewRecorder()
		h.InitiateDaemonPathCheck(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("path %q: expected 400, got %d: %s", path, w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("path %q: decode: %v", path, err)
		}
		if body["reason"] != "not_absolute" {
			t.Fatalf("path %q: reason = %v, want not_absolute", path, body["reason"])
		}
	}

	if len(recorder.hints) != 0 {
		t.Fatalf("rejected paths must not wake the daemon, hints = %v", recorder.hints)
	}
	hasPending, err := h.PathCheckStore.HasPending(context.Background(), "any-runtime")
	if err != nil {
		t.Fatalf("HasPending: %v", err)
	}
	if hasPending {
		t.Fatal("rejected paths must not create a pending request")
	}
}

// Acceptance: a daemon with no online runtime yields a distinguishable
// machine-offline error, not a hang or a generic 500.
func TestDaemonPathCheck_MachineOffline(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	const daemonID = "path-check-offline-daemon"
	createPathCheckTestRuntime(t, testUserID, daemonID, "offline")

	w, _ := initiatePathCheck(t, testHandler, testUserID, testWorkspaceID, daemonID, "/tmp/x")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["code"] != "machine_offline" {
		t.Fatalf("code = %v, want machine_offline", body["code"])
	}
}

// Acceptance: the timeout path — a pending request that the daemon never
// claims turns into a terminal "timeout" the poll can see, and a late report
// is ignored.
func TestDaemonPathCheck_Timeout(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	const daemonID = "path-check-timeout-daemon"
	runtimeID := createPathCheckTestRuntime(t, testUserID, daemonID, "online")

	store, ok := testHandler.PathCheckStore.(*InMemoryPathCheckStore)
	if !ok {
		t.Fatal("test handler must use the in-memory path check store")
	}

	// Pending timeout: age the record past the pending window.
	_, initBody := initiatePathCheck(t, testHandler, testUserID, testWorkspaceID, daemonID, "/tmp/a")
	ageRequestsForRuntime(t, store, runtimeID, daemonPathCheckPendingTimeout+time.Second)

	w, pollBody := pollPathCheck(t, testHandler, testUserID, testWorkspaceID, daemonID, firstInitID(initBody))
	if w.Code != http.StatusOK {
		t.Fatalf("poll: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if pollBody["status"] != "timeout" {
		t.Fatalf("poll status = %v, want timeout", pollBody["status"])
	}

	// A late report after the timeout is accepted but ignored.
	wr := reportPathCheck(t, testHandler, runtimeID, firstInitID(initBody), map[string]any{
		"status":       "completed",
		"exists":       true,
		"is_directory": true,
		"readable":     true,
		"writable":     true,
	})
	if wr.Code != http.StatusOK {
		t.Fatalf("late report: expected 200, got %d: %s", wr.Code, wr.Body.String())
	}
	_, pollBody = pollPathCheck(t, testHandler, testUserID, testWorkspaceID, daemonID, firstInitID(initBody))
	if pollBody["status"] != "timeout" {
		t.Fatalf("poll after late report = %v, want timeout", pollBody["status"])
	}

	// Running timeout: claim via heartbeat, age past the running window.
	_, initBody = initiatePathCheck(t, testHandler, testUserID, testWorkspaceID, daemonID, "/tmp/b")
	heartbeatReq := newDaemonTokenRequest(http.MethodPost, "/api/daemon/heartbeat", map[string]any{
		"runtime_id": runtimeID,
	}, testWorkspaceID, daemonID)
	wb := httptest.NewRecorder()
	testHandler.DaemonHeartbeat(wb, heartbeatReq)
	if wb.Code != http.StatusOK {
		t.Fatalf("heartbeat: expected 200, got %d: %s", wb.Code, wb.Body.String())
	}
	ageRunningRequestsForRuntime(t, store, runtimeID, daemonPathCheckRunningTimeout+time.Second)

	_, pollBody = pollPathCheck(t, testHandler, testUserID, testWorkspaceID, daemonID, firstInitID(initBody))
	if pollBody["status"] != "timeout" {
		t.Fatalf("running poll status = %v, want timeout", pollBody["status"])
	}
}

func firstInitID(initBody map[string]any) string {
	id, _ := initBody["id"].(string)
	return id
}

// ageRequestsForRuntime reaches into the in-memory store (same package) to
// move the records' clocks back, since Get applies timeout transitions to
// the stored records.
func ageRequestsForRuntime(t *testing.T, store *InMemoryPathCheckStore, runtimeID string, by time.Duration) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, req := range store.requests {
		if req.RuntimeID == runtimeID {
			req.CreatedAt = req.CreatedAt.Add(-by)
		}
	}
}

func ageRunningRequestsForRuntime(t *testing.T, store *InMemoryPathCheckStore, runtimeID string, by time.Duration) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, req := range store.requests {
		if req.RuntimeID == runtimeID && req.RunStartedAt != nil {
			past := req.RunStartedAt.Add(-by)
			req.RunStartedAt = &past
		}
	}
}

// The per-user initiation budget: the third request in the window is a 429
// with a Retry-After, and the rejected request never reaches the store.
func TestDaemonPathCheck_RateLimited(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	const daemonID = "path-check-ratelimit-daemon"
	createPathCheckTestRuntime(t, testUserID, daemonID, "online")

	h := *testHandler
	h.PathCheckRateLimiter = newMemorySlidingWindowRateLimiter(SlidingWindowRateLimit{Limit: 2, Window: time.Minute})

	for i := 0; i < 2; i++ {
		w, _ := initiatePathCheck(t, &h, testUserID, testWorkspaceID, daemonID, fmt.Sprintf("/tmp/ratelimit-%d", i))
		if w.Code != http.StatusOK {
			t.Fatalf("initiate %d: expected 200, got %d: %s", i, w.Code, w.Body.String())
		}
	}

	req := withURLParams(
		newRequestAsUser(testUserID, http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/daemons/"+daemonID+"/path-checks", map[string]any{
			"path": "/tmp/ratelimit-2",
		}),
		"workspaceId", testWorkspaceID,
		"daemonId", daemonID,
	)
	w := httptest.NewRecorder()
	h.InitiateDaemonPathCheck(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("third initiate: expected 429, got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header on 429")
	}
}

// The report endpoint authenticates the daemon (or a workspace user token —
// the same trust model as ReportLocalSkillListResult). A daemon token scoped
// to a different workspace cannot deliver a verdict for this workspace's
// request.
func TestDaemonPathCheck_ReportRejectsCrossWorkspaceDaemonToken(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	const daemonID = "path-check-report-auth-daemon"
	runtimeID := createPathCheckTestRuntime(t, testUserID, daemonID, "online")

	_, initBody := initiatePathCheck(t, testHandler, testUserID, testWorkspaceID, daemonID, "/tmp/auth")
	requestID := firstInitID(initBody)

	req := withURLParams(
		newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/path-checks/"+requestID+"/result", map[string]any{
			"status": "completed",
			"exists": true,
		}, "00000000-0000-0000-0000-000000000000", "attacker-daemon"),
		"runtimeId", runtimeID,
		"requestId", requestID,
	)
	w := httptest.NewRecorder()
	testHandler.ReportDaemonPathCheckResult(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// The request is untouched.
	_, pollBody := pollPathCheck(t, testHandler, testUserID, testWorkspaceID, daemonID, requestID)
	if pollBody["status"] != "pending" {
		t.Fatalf("request must stay pending, got %v", pollBody["status"])
	}
}

// The store timeout transition is covered by the round-trip-style Get test:
// a running record aged past the running window reads back as timeout.
func TestInMemoryPathCheckStore_TimesOutPendingAndRunning(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryPathCheckStore()

	pending, err := store.Create(ctx, "runtime-xyz", "/tmp/p")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	pending.CreatedAt = time.Now().Add(-91 * time.Second)
	got, err := store.Get(ctx, pending.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.Status != DaemonPathCheckTimeout {
		t.Fatalf("pending timeout: got %+v", got)
	}
	if got.Error == "" {
		t.Fatal("expected timeout error message")
	}

	running, err := store.Create(ctx, "runtime-xyz", "/tmp/r")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	claimed, err := store.PopPending(ctx, "runtime-xyz")
	if err != nil {
		t.Fatalf("pop: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected a claimed request")
	}
	started := time.Now().Add(-31 * time.Second)
	claimed.RunStartedAt = &started
	claimed.Status = DaemonPathCheckRunning
	got, err = store.Get(ctx, running.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.Status != DaemonPathCheckTimeout {
		t.Fatalf("running timeout: got %+v", got)
	}
}
