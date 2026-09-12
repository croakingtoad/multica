package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Read path for hook_fire_history (LOCO-135). What these tests defend is
// DP-LOCO-114-03 condition 2: the feed renders the STORED `provenance` column
// verbatim and never derives, upgrades or re-decides it. Round 3 of this
// effort shipped a greedy join that assigned one execution's timestamp to
// another and stamped the result `debug_log`; the capture path is now audited
// against that, and these tests are what keep the same defect out of the read
// path.

// seedFeedRuntime creates a runtime OWNED by the test user. The feed is behind
// requireRuntimeReadAccess, and canUseRuntimeForAgent refuses a runtime with no
// owner — so the shared seedIsolatedRuntime helper (owner_id NULL) would make
// every assertion below a 404 and prove nothing about the projection.
func seedFeedRuntime(t *testing.T, name string) string {
	t.Helper()
	ctx := context.Background()
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, last_seen_at
		)
		VALUES ($1, NULL, $2, 'local', 'claude', 'online',
			'hook fire feed test runtime', '{}'::jsonb, $3, 'private', now())
		RETURNING id
	`, testWorkspaceID, name, testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("seed feed runtime %q: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM hook_fire_history WHERE runtime_id = $1`, runtimeID)
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

// seedHookFire inserts one row with an explicitly chosen provenance and
// returns its id. The provenance is a parameter, not a constant, because the
// point of every assertion below is "whatever was stored came back".
func seedHookFire(
	t *testing.T,
	runtimeID, provider, event, executionID, provenance, outcome, detail string,
) string {
	t.Helper()
	ctx := context.Background()
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO hook_fire_history (
			runtime_id, provider, event, execution_id, hook_spec,
			fired_at, provenance, outcome, detail
		)
		VALUES ($1, $2, $3, $4, '{"hook_name": "feed-test-hook"}'::jsonb,
			now(), $5, $6, $7::jsonb)
		RETURNING id
	`, runtimeID, provider, event, executionID, provenance, outcome, detail).Scan(&id); err != nil {
		t.Fatalf("seed hook_fire_history (%s/%s): %v", provenance, outcome, err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM hook_fire_history WHERE id = $1`, id)
	})
	return id
}

func listHookFires(t *testing.T, runtimeID, query string) RuntimeHookFireFeedResponse {
	t.Helper()
	req := newRequest(http.MethodGet, "/api/runtimes/"+runtimeID+"/hook-fires"+query, nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	var out RuntimeHookFireFeedResponse
	testutil.Call(t, testHandler.ListHookFires, req).Want(http.StatusOK).JSON(&out)
	return out
}

// The load-bearing test. Both stored provenance values must survive the read
// path byte-for-byte. A feed that decided provenance at read time would show
// the same string for both of these fixtures.
func TestListHookFires_RendersStoredProvenanceVerbatim(t *testing.T) {
	runtimeID := seedFeedRuntime(t, "Hook Fire Feed Provenance Runtime")

	for _, provenance := range []string{"debug_log", "inferred"} {
		t.Run(provenance, func(t *testing.T) {
			id := seedHookFire(t, runtimeID, "claude", "PreToolUse",
				"exec-"+provenance, provenance, "success", `{"debug_outcome": "success"}`)

			feed := listHookFires(t, runtimeID, "")

			var found *RuntimeHookFireResponse
			for i := range feed.Fires {
				if feed.Fires[i].ID == id {
					found = &feed.Fires[i]
				}
			}
			if found == nil {
				t.Fatalf("seeded fire %s not in feed of %d rows", id, len(feed.Fires))
			}
			if found.Provenance != provenance {
				t.Fatalf("rendered provenance = %q, stored %q — the read path changed it",
					found.Provenance, provenance)
			}
		})
	}
}

// A mixed feed is the measured reality: stage 7's Tier 2 audit found 101 of
// 144 real Claude responses (70.1%) stored as `inferred` alongside 43
// `debug_log`. Each row must keep its own value; a per-provider or
// per-response decision would flatten them to one.
func TestListHookFires_KeepsPerRowProvenanceInMixedFeed(t *testing.T) {
	runtimeID := seedFeedRuntime(t, "Hook Fire Feed Mixed Runtime")

	inferredID := seedHookFire(t, runtimeID, "claude", "PreToolUse",
		"exec-mixed-inferred", "inferred", "success", `{"debug_outcome": "success"}`)
	// Same provider, same event, and a detail that carries the `message`
	// field a debug-log match produces — the shape that tempts a read-time
	// "this looks matched" upgrade. Stored provenance still decides.
	debugID := seedHookFire(t, runtimeID, "claude", "PreToolUse",
		"exec-mixed-debug", "debug_log", "success",
		`{"debug_outcome": "success", "message": "hook completed"}`)
	// And the inverse: a `message` in detail with `inferred` stored.
	trapID := seedHookFire(t, runtimeID, "claude", "Stop",
		"exec-mixed-trap", "inferred", "success",
		`{"debug_outcome": "success", "message": "hook completed"}`)

	feed := listHookFires(t, runtimeID, "")
	byID := map[string]string{}
	for _, fire := range feed.Fires {
		byID[fire.ID] = fire.Provenance
	}

	for id, want := range map[string]string{
		inferredID: "inferred",
		debugID:    "debug_log",
		trapID:     "inferred",
	} {
		if got := byID[id]; got != want {
			t.Fatalf("fire %s provenance = %q, want the stored %q", id, got, want)
		}
	}
}

// The outcome column gets the same treatment. `unknown` in particular must
// arrive as `unknown` — it is a real state with two causes (exit 126/127
// ambiguity and a provider `cancelled`), not a value to be repaired into
// `failure` or `skipped` on the way out.
func TestListHookFires_RendersStoredOutcomeVerbatim(t *testing.T) {
	runtimeID := seedFeedRuntime(t, "Hook Fire Feed Outcome Runtime")

	for _, outcome := range []string{"success", "failure", "blocked", "skipped", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			id := seedHookFire(t, runtimeID, "claude", "PreToolUse",
				"exec-outcome-"+outcome, "inferred", outcome,
				`{"debug_outcome": "error", "exit_code": 127}`)

			feed := listHookFires(t, runtimeID, "")
			for _, fire := range feed.Fires {
				if fire.ID == id {
					if fire.Outcome != outcome {
						t.Fatalf("rendered outcome = %q, stored %q", fire.Outcome, outcome)
					}
					return
				}
			}
			t.Fatalf("seeded fire %s not in feed", id)
		})
	}
}

// A Codex row reads back as a Codex row with its own provenance, so the feed
// is provider-agnostic: nothing about the projection is keyed to `claude`.
// (The write path is Claude-only today; this row is inserted directly, which
// is what a later Codex capture path would produce.)
func TestListHookFires_ProjectsCodexRowsWithTheirOwnProvenance(t *testing.T) {
	runtimeID := seedFeedRuntime(t, "Hook Fire Feed Codex Runtime")
	id := seedHookFire(t, runtimeID, "codex", "Stop",
		"codex-trust-hash-1", "inferred", "unknown", `{"exit_code": 127}`)

	feed := listHookFires(t, runtimeID, "")
	for _, fire := range feed.Fires {
		if fire.ID == id {
			if fire.Provider != "codex" || fire.Provenance != "inferred" {
				t.Fatalf("codex fire = provider %q provenance %q, want codex/inferred",
					fire.Provider, fire.Provenance)
			}
			return
		}
	}
	t.Fatalf("seeded codex fire %s not in feed", id)
}

// Ordering is newest-first, matching the mandated feed index
// (runtime_id, fired_at DESC).
func TestListHookFires_OrdersNewestFirst(t *testing.T) {
	ctx := context.Background()
	runtimeID := seedFeedRuntime(t, "Hook Fire Feed Order Runtime")

	var olderID, newerID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO hook_fire_history (runtime_id, provider, event, execution_id, fired_at, provenance, outcome)
		VALUES ($1, 'claude', 'Stop', 'exec-older', now() - interval '1 hour', 'inferred', 'success')
		RETURNING id
	`, runtimeID).Scan(&olderID); err != nil {
		t.Fatalf("seed older fire: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO hook_fire_history (runtime_id, provider, event, execution_id, fired_at, provenance, outcome)
		VALUES ($1, 'claude', 'Stop', 'exec-newer', now(), 'debug_log', 'success')
		RETURNING id
	`, runtimeID).Scan(&newerID); err != nil {
		t.Fatalf("seed newer fire: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM hook_fire_history WHERE runtime_id = $1`, runtimeID)
	})

	feed := listHookFires(t, runtimeID, "")
	if len(feed.Fires) != 2 {
		t.Fatalf("feed rows = %d, want 2", len(feed.Fires))
	}
	if feed.Fires[0].ID != newerID || feed.Fires[1].ID != olderID {
		t.Fatalf("order = [%s %s], want newest-first [%s %s]",
			feed.Fires[0].ID, feed.Fires[1].ID, newerID, olderID)
	}
}

// Repeated execution ids produce repeated rows. execution_id is Claude's
// per-execution reference, fresh on every fire, so the feed must not treat it
// as an identity to dedupe on (DP-LOCO-114-02 item 3).
func TestListHookFires_DoesNotDedupeOnExecutionID(t *testing.T) {
	runtimeID := seedFeedRuntime(t, "Hook Fire Feed Dedupe Runtime")
	seedHookFire(t, runtimeID, "claude", "PreToolUse", "same-execution-ref",
		"inferred", "success", `{}`)
	seedHookFire(t, runtimeID, "claude", "PreToolUse", "same-execution-ref",
		"debug_log", "failure", `{}`)

	feed := listHookFires(t, runtimeID, "")
	if len(feed.Fires) != 2 {
		t.Fatalf("feed rows = %d, want 2 — the feed collapsed rows sharing an execution_id",
			len(feed.Fires))
	}
}

// The feed reports its own completeness so the screen can say so rather than
// implying it shows everything that ever fired.
func TestListHookFires_ReportsLimitAndTruncation(t *testing.T) {
	runtimeID := seedFeedRuntime(t, "Hook Fire Feed Limit Runtime")
	for _, ref := range []string{"exec-limit-1", "exec-limit-2", "exec-limit-3"} {
		seedHookFire(t, runtimeID, "claude", "Stop", ref, "inferred", "success", `{}`)
	}

	truncated := listHookFires(t, runtimeID, "?limit=2")
	if len(truncated.Fires) != 2 || truncated.Limit != 2 || !truncated.Truncated {
		t.Fatalf("limit=2 → rows %d limit %d truncated %v, want 2/2/true",
			len(truncated.Fires), truncated.Limit, truncated.Truncated)
	}

	whole := listHookFires(t, runtimeID, "?limit=50")
	if len(whole.Fires) != 3 || whole.Truncated {
		t.Fatalf("limit=50 → rows %d truncated %v, want 3/false",
			len(whole.Fires), whole.Truncated)
	}
}

// A malformed limit takes the default rather than denying a diagnostic view,
// and an over-large one is clamped.
func TestListHookFires_ClampsLimit(t *testing.T) {
	runtimeID := seedFeedRuntime(t, "Hook Fire Feed Clamp Runtime")

	for _, tc := range []struct {
		query string
		want  int
	}{
		{"", defaultHookFireFeedLimit},
		{"?limit=abc", defaultHookFireFeedLimit},
		{"?limit=0", defaultHookFireFeedLimit},
		{"?limit=-5", defaultHookFireFeedLimit},
		{"?limit=99999", maxHookFireFeedLimit},
		{"?limit=7", 7},
	} {
		if got := listHookFires(t, runtimeID, tc.query).Limit; got != tc.want {
			t.Fatalf("limit%q → %d, want %d", tc.query, got, tc.want)
		}
	}
}

// Rows belonging to another runtime never leak in; the predicate is the
// mandated index's leading column.
func TestListHookFires_ScopesToRuntime(t *testing.T) {
	mine := seedFeedRuntime(t, "Hook Fire Feed Scope Mine")
	theirs := seedFeedRuntime(t, "Hook Fire Feed Scope Theirs")
	seedHookFire(t, theirs, "claude", "Stop", "exec-other-runtime",
		"debug_log", "success", `{}`)

	if feed := listHookFires(t, mine, ""); len(feed.Fires) != 0 {
		t.Fatalf("feed for a runtime with no fires returned %d rows", len(feed.Fires))
	}
}
