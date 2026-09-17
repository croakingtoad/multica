package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func newTaskClaimRacePool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestClaimTaskConcurrentCapacityRespected(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	queries := db.New(pool)

	agentID := createClaimCapacityFixture(t, ctx, pool)
	agentUUID := util.MustParseUUID(agentID)

	triggerName := fmt.Sprintf("claim_capacity_sleep_%d", time.Now().UnixNano())
	functionName := triggerName + "_fn"
	createSleepTrigger(t, ctx, pool, triggerName, functionName, agentID)
	svc := NewTaskService(queries, pool, nil, events.New())

	const workers = 2
	start := make(chan struct{})
	claimed := make(chan string, workers)
	errs := make(chan error, workers)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			task, err := svc.ClaimTask(ctx, agentUUID)
			if err != nil {
				errs <- err
				return
			}
			if task != nil {
				claimed <- util.UUIDToString(task.ID)
			}
		}()
	}

	close(start)
	wg.Wait()
	close(claimed)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("claim task: %v", err)
		}
	}

	var claimedIDs []string
	for id := range claimed {
		claimedIDs = append(claimedIDs, id)
	}
	if len(claimedIDs) != 1 {
		t.Fatalf("expected exactly 1 claimed task with max_concurrent_tasks=1, got %d (%v)", len(claimedIDs), claimedIDs)
	}

	var active int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE agent_id = $1 AND status IN ('dispatched', 'running', 'waiting_local_directory')
	`, agentID).Scan(&active); err != nil {
		t.Fatalf("count active tasks: %v", err)
	}
	if active != 1 {
		t.Fatalf("expected 1 active task after concurrent claims, got %d", active)
	}
}

// TestClaimTaskSerializesSameIssueWithSpareCapacity distinguishes the
// per-(issue, agent) claim fence from the agent-wide capacity limit. The agent
// has room for two concurrent tasks and receives two independently queued
// thread wakes for one issue. Only one may dispatch; the other must remain a
// durable, observable queued row until its predecessor reaches a terminal
// state.
func TestClaimTaskSerializesSameIssueWithSpareCapacity(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	agentID := createClaimCapacityFixture(t, ctx, pool)
	agentUUID := util.MustParseUUID(agentID)

	if _, err := pool.Exec(ctx, `UPDATE agent SET max_concurrent_tasks = 2 WHERE id = $1`, agentID); err != nil {
		t.Fatalf("give agent spare capacity: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		WITH task_scope AS (
			SELECT id, first_value(issue_id) OVER (ORDER BY created_at, id) AS issue_id
			FROM agent_task_queue
			WHERE agent_id = $1 AND status = 'queued'
		)
		UPDATE agent_task_queue AS task
		SET issue_id = task_scope.issue_id,
			comment_thread_id = gen_random_uuid()
		FROM task_scope
		WHERE task.id = task_scope.id
	`, agentID); err != nil {
		t.Fatalf("put queued wakes on distinct threads of one issue: %v", err)
	}

	triggerName := fmt.Sprintf("claim_issue_serial_sleep_%d", time.Now().UnixNano())
	createSleepTrigger(t, ctx, pool, triggerName, triggerName+"_fn", agentID)
	svc := NewTaskService(db.New(pool), pool, nil, events.New())

	const workers = 2
	start := make(chan struct{})
	claimed := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			task, err := svc.ClaimTask(ctx, agentUUID)
			if err != nil {
				errs <- err
				return
			}
			if task != nil {
				claimed <- util.UUIDToString(task.ID)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(claimed)
	close(errs)

	for err := range errs {
		t.Fatalf("claim task: %v", err)
	}
	var firstTaskID string
	for id := range claimed {
		if firstTaskID != "" {
			t.Fatalf("same issue dispatched concurrently: %s and %s", firstTaskID, id)
		}
		firstTaskID = id
	}
	if firstTaskID == "" {
		t.Fatal("expected one task to dispatch")
	}

	var dispatched, queued int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status = 'dispatched'),
		       count(*) FILTER (WHERE status = 'queued')
		FROM agent_task_queue
		WHERE agent_id = $1
	`, agentID).Scan(&dispatched, &queued); err != nil {
		t.Fatalf("read serialized queue state: %v", err)
	}
	if dispatched != 1 || queued != 1 {
		t.Fatalf("serialized queue state = dispatched:%d queued:%d, want 1/1", dispatched, queued)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'completed', completed_at = now(), prepare_lease_expires_at = NULL
		WHERE id = $1 AND status = 'dispatched'
	`, firstTaskID); err != nil {
		t.Fatalf("complete predecessor: %v", err)
	}
	second, err := svc.ClaimTask(ctx, agentUUID)
	if err != nil {
		t.Fatalf("claim queued successor: %v", err)
	}
	if second == nil {
		t.Fatal("queued successor was lost after predecessor completed")
	}
	if util.UUIDToString(second.ID) == firstTaskID {
		t.Fatalf("claimed predecessor %s twice", firstTaskID)
	}
}

func createSleepTrigger(t *testing.T, ctx context.Context, pool *pgxpool.Pool, triggerName, functionName, agentID string) {
	t.Helper()

	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		CREATE FUNCTION %s()
		RETURNS trigger
		LANGUAGE plpgsql
		AS $$
		BEGIN
			PERFORM pg_sleep(0.2);
			RETURN NEW;
		END;
		$$;
	`, quoteIdent(functionName))); err != nil {
		t.Fatalf("create sleep trigger function: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		CREATE TRIGGER %s
		BEFORE UPDATE OF status ON agent_task_queue
		FOR EACH ROW
		WHEN (OLD.status = 'queued' AND NEW.status = 'dispatched' AND NEW.agent_id = %s::uuid)
		EXECUTE FUNCTION %s();
	`, quoteIdent(triggerName), quoteLiteral(agentID), quoteIdent(functionName))); err != nil {
		t.Fatalf("create sleep trigger: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		pool.Exec(cleanupCtx, fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON agent_task_queue", quoteIdent(triggerName)))
		pool.Exec(cleanupCtx, fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", quoteIdent(functionName)))
	})
}

func createClaimCapacityFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()

	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("claim-capacity-%d@multica.ai", suffix)
	slug := fmt.Sprintf("claim-capacity-%d", suffix)

	var userID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Claim Capacity Test", email).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}

	var workspaceID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Claim Capacity Test", slug, "temporary task claim race test workspace", "CCR").Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}

	var runtimeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider,
			status, device_info, metadata, last_seen_at, visibility, owner_id
		)
		VALUES ($1, NULL, $2, 'cloud', 'claim_capacity_test', 'online', 'test runtime', '{}'::jsonb, now(), 'private', $3)
		RETURNING id
	`, workspaceID, "Claim Capacity Runtime", userID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	var agentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4)
		RETURNING id
	`, workspaceID, "Claim Capacity Agent", runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		pool.Exec(cleanupCtx, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
		pool.Exec(cleanupCtx, `DELETE FROM issue WHERE workspace_id = $1`, workspaceID)
		pool.Exec(cleanupCtx, `DELETE FROM agent WHERE id = $1`, agentID)
		pool.Exec(cleanupCtx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
		pool.Exec(cleanupCtx, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID)
		pool.Exec(cleanupCtx, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		pool.Exec(cleanupCtx, `DELETE FROM "user" WHERE id = $1`, userID)
	})

	for i := 0; i < 2; i++ {
		var issueID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
			VALUES ($1, $2, 'in_progress', 'none', $3, 'member', $4, $5)
			RETURNING id
		`, workspaceID, fmt.Sprintf("claim capacity issue %d", i+1), userID, 900000+i, i).Scan(&issueID); err != nil {
			t.Fatalf("create issue %d: %v", i+1, err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO agent_task_queue (agent_id, issue_id, status, priority, context, runtime_id)
			VALUES ($1, $2, 'queued', 0, '{}'::jsonb, $3)
		`, agentID, issueID, runtimeID); err != nil {
			t.Fatalf("create task %d: %v", i+1, err)
		}
	}

	return agentID
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func quoteLiteral(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}
