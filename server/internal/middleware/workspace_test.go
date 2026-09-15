package middleware

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const testResolverSlug = "middleware-resolver-test"

// openPool returns a connected pgxpool. The package-level testdb gate fails
// first when Postgres is unavailable unless the contributor explicitly opts
// out, in which case this helper retains the suite's historical skip.
func openPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skipf("skipping: could not connect to database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("skipping: database not reachable: %v", err)
	}
	return pool
}

// setupResolverFixture inserts a workspace with a known slug and returns its
// UUID. The caller is responsible for calling the returned cleanup func.
func setupResolverFixture(t *testing.T, pool *pgxpool.Pool) (workspaceID string, cleanup func()) {
	t.Helper()
	ctx := context.Background()
	// Pre-cleanup in case a previous run didn't finish.
	_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, testResolverSlug)

	if err := pool.QueryRow(ctx,
		`INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ($1, $2, '', 'MRT') RETURNING id`,
		"Middleware Resolver Test", testResolverSlug,
	).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	return workspaceID, func() {
		_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, testResolverSlug)
	}
}

// TestResolveWorkspaceIDFromRequest pins down the priority order of the
// shared resolver. Every handler-level lookup of workspace identity — whether
// a route sits inside or outside the workspace middleware — must produce
// identical results, in the same priority, across all five supported
// mechanisms. Breaking any row here is a behavioral regression.
func TestResolveWorkspaceIDFromRequest(t *testing.T) {
	pool := openPool(t)
	defer pool.Close()
	queries := db.New(pool)

	workspaceID, cleanup := setupResolverFixture(t, pool)
	defer cleanup()

	const (
		uuidA = "00000000-0000-0000-0000-000000000001"
		uuidB = "00000000-0000-0000-0000-000000000002"
	)

	cases := []struct {
		name      string
		setup     func(r *http.Request)
		want      string
		wantEmpty bool
	}{
		{
			name: "context UUID wins over everything else",
			setup: func(r *http.Request) {
				ctx := context.WithValue(r.Context(), ctxKeyWorkspaceID, uuidA)
				*r = *r.WithContext(ctx)
				r.Header.Set("X-Workspace-Slug", testResolverSlug)
				r.Header.Set("X-Workspace-ID", uuidB)
			},
			want: uuidA,
		},
		{
			name: "X-Workspace-Slug header resolves to UUID via DB lookup",
			setup: func(r *http.Request) {
				r.Header.Set("X-Workspace-Slug", testResolverSlug)
			},
			want: workspaceID,
		},
		{
			name: "X-Workspace-Slug wins over X-Workspace-ID (post-refactor priority)",
			setup: func(r *http.Request) {
				r.Header.Set("X-Workspace-Slug", testResolverSlug)
				r.Header.Set("X-Workspace-ID", uuidB)
			},
			want: workspaceID,
		},
		{
			name: "unknown X-Workspace-Slug falls through to UUID header",
			setup: func(r *http.Request) {
				r.Header.Set("X-Workspace-Slug", "does-not-exist")
				r.Header.Set("X-Workspace-ID", uuidB)
			},
			want: uuidB,
		},
		{
			name: "?workspace_slug query resolves to UUID via DB lookup",
			setup: func(r *http.Request) {
				q := r.URL.Query()
				q.Set("workspace_slug", testResolverSlug)
				r.URL.RawQuery = q.Encode()
			},
			want: workspaceID,
		},
		{
			name: "X-Workspace-ID header is returned when no slug provided",
			setup: func(r *http.Request) {
				r.Header.Set("X-Workspace-ID", uuidA)
			},
			want: uuidA,
		},
		{
			name: "?workspace_id query is the last-resort fallback",
			setup: func(r *http.Request) {
				q := r.URL.Query()
				q.Set("workspace_id", uuidA)
				r.URL.RawQuery = q.Encode()
			},
			want: uuidA,
		},
		{
			name:      "no identifier at all returns empty",
			setup:     func(r *http.Request) {},
			wantEmpty: true,
		},
		{
			name: "unknown slug with no UUID fallback returns empty",
			setup: func(r *http.Request) {
				r.Header.Set("X-Workspace-Slug", "does-not-exist")
			},
			wantEmpty: true,
		},
		{
			// MUL-2600: a mat_ task token authenticates the request and
			// the auth middleware writes the token-bound workspace into
			// X-Workspace-ID along with X-Actor-Source=task_token. Any
			// other workspace identifier the agent puts on the wire — a
			// slug pointing at a sibling workspace, a different
			// workspace_id — must be ignored. Otherwise an agent could
			// route owner-token traffic at any workspace its host is
			// also a member of.
			name: "task_token actor: client-supplied slug/id cannot override token-bound workspace",
			setup: func(r *http.Request) {
				r.Header.Set("X-Actor-Source", "task_token")
				r.Header.Set("X-Workspace-ID", uuidA)
				// All of these should be ignored under task_token.
				r.Header.Set("X-Workspace-Slug", testResolverSlug)
				q := r.URL.Query()
				q.Set("workspace_slug", testResolverSlug)
				q.Set("workspace_id", uuidB)
				r.URL.RawQuery = q.Encode()
			},
			want: uuidA,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/anything", nil)
			tc.setup(req)

			got := ResolveWorkspaceIDFromRequest(req, queries)

			if tc.wantEmpty {
				if got != "" {
					t.Fatalf("expected empty, got %q", got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestWorkspaceMiddlewareResolutionStatus(t *testing.T) {
	pool := openPool(t)
	queries := db.New(pool)
	middleware := RequireWorkspaceMember(queries)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	t.Run("unknown slug returns not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/anything?workspace_slug=does-not-exist", nil)
		res := httptest.NewRecorder()

		middleware(next).ServeHTTP(res, req)

		if res.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d: %s", http.StatusNotFound, res.Code, res.Body.String())
		}
	})

	t.Run("missing identifier returns bad request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/anything", nil)
		res := httptest.NewRecorder()

		middleware(next).ServeHTTP(res, req)

		if res.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d: %s", http.StatusBadRequest, res.Code, res.Body.String())
		}
	})

	t.Run("task token cannot target another workspace", func(t *testing.T) {
		const (
			boundWorkspace  = "00000000-0000-0000-0000-000000000001"
			targetWorkspace = "00000000-0000-0000-0000-000000000002"
		)
		req := httptest.NewRequest(http.MethodGet, "/api/anything", nil)
		req.Header.Set("X-Actor-Source", "task_token")
		req.Header.Set("X-Workspace-ID", boundWorkspace)
		res := httptest.NewRecorder()
		resolveTarget := func(*http.Request) (string, error) { return targetWorkspace, nil }

		buildMiddleware(queries, resolveTarget, nil)(next).ServeHTTP(res, req)

		if res.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d: %s", http.StatusForbidden, res.Code, res.Body.String())
		}
	})

	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	pool.Close()

	t.Run("unavailable database returns service unavailable", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/anything?workspace_slug=anything", nil)
		req.Header.Set("X-Workspace-ID", "00000000-0000-0000-0000-000000000001")
		res := httptest.NewRecorder()

		middleware(next).ServeHTTP(res, req)

		if res.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d: %s", http.StatusServiceUnavailable, res.Code, res.Body.String())
		}
		if !strings.Contains(logs.String(), "closed pool") {
			t.Fatalf("expected underlying database error in log, got %q", logs.String())
		}
	})

	t.Run("resolver does not fall back after database failure", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/anything", nil)
		req.Header.Set("X-Workspace-Slug", "anything")
		req.Header.Set("X-Workspace-ID", "00000000-0000-0000-0000-000000000001")

		if got := ResolveWorkspaceIDFromRequest(req, queries); got != "" {
			t.Fatalf("expected empty workspace ID after database failure, got %q", got)
		}
	})
}

func TestWorkspaceMiddlewareMembershipLookupUnavailable(t *testing.T) {
	pool := openPool(t)
	queries := db.New(pool)
	pool.Close()

	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	req := httptest.NewRequest(http.MethodGet, "/api/anything?workspace_id=00000000-0000-0000-0000-000000000001", nil)
	req.Header.Set("X-User-ID", "00000000-0000-0000-0000-000000000002")
	res := httptest.NewRecorder()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	RequireWorkspaceMember(queries)(next).ServeHTTP(res, req)

	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected %d, got %d: %s", http.StatusServiceUnavailable, res.Code, res.Body.String())
	}
	if !strings.Contains(logs.String(), "closed pool") {
		t.Fatalf("expected underlying database error in log, got %q", logs.String())
	}
}
