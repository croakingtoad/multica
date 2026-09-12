package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func seedRuntimeHookRows(t *testing.T, ctx context.Context, runtimeID string) {
	t.Helper()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO hook_state_snapshot (
			runtime_id, provider, scope, format, hooks, disabled_hooks, observed_at
		)
		VALUES ($1, 'codex', 'user', 'json', '{"hooks": []}'::jsonb, '{}'::jsonb, now())
	`, runtimeID); err != nil {
		t.Fatalf("seed hook_state_snapshot: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO hook_fire_history (
			runtime_id, provider, event, execution_id, hook_spec, fired_at, provenance, outcome
		)
		VALUES ($1, 'codex', 'Stop', 'hook-cascade-test', '{"command": "true"}'::jsonb, now(), 'inferred', 'success')
	`, runtimeID); err != nil {
		t.Fatalf("seed hook_fire_history: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM hook_state_snapshot WHERE runtime_id = $1`, runtimeID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM hook_fire_history WHERE runtime_id = $1`, runtimeID)
	})
}

func assertNoRuntimeHookRows(t *testing.T, ctx context.Context, runtimeID string) {
	t.Helper()
	for _, table := range []string{"hook_state_snapshot", "hook_fire_history"} {
		var count int
		if err := testPool.QueryRow(ctx,
			`SELECT count(*) FROM `+table+` WHERE runtime_id = $1`, runtimeID,
		).Scan(&count); err != nil {
			t.Fatalf("count %s rows: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s rows after runtime delete = %d, want 0", table, count)
		}
	}
}

func TestDeleteAgentRuntime_CleansRuntimeHookRows(t *testing.T) {
	ctx := context.Background()
	runtimeID := seedIsolatedRuntime(t, "Hook Cascade Runtime")
	seedRuntimeHookRows(t, ctx, runtimeID)

	w := httptest.NewRecorder()
	req := newRequest(http.MethodDelete, "/api/runtimes/"+runtimeID, nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.DeleteAgentRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DeleteAgentRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	assertNoRuntimeHookRows(t, ctx, runtimeID)
}

func TestDeleteWorkspace_CleansRuntimeHookRows(t *testing.T) {
	ctx := context.Background()
	const slug = "handler-tests-delete-runtime-hooks"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var workspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description)
		VALUES ('Handler Test Delete Runtime Hooks', $1, 'runtime hook cleanup test')
		RETURNING id
	`, slug).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		workspaceID, testUserID,
	); err != nil {
		t.Fatalf("create workspace owner: %v", err)
	}

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id
		)
		VALUES ($1, 'Workspace Hook Cascade Runtime', 'cloud', 'codex', 'offline', '', '{}'::jsonb, $2)
		RETURNING id
	`, workspaceID, testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("create workspace runtime: %v", err)
	}
	seedRuntimeHookRows(t, ctx, runtimeID)

	w := httptest.NewRecorder()
	req := newRequest(http.MethodDelete, "/api/workspaces/"+workspaceID, nil)
	req = withURLParam(req, "id", workspaceID)
	testHandler.DeleteWorkspace(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteWorkspace: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	assertNoRuntimeHookRows(t, ctx, runtimeID)
}
