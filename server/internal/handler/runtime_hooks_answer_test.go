package handler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/runtimehooks"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func decodeHookAnswerResponse(t *testing.T, w *httptest.ResponseRecorder) hookEventAnswerResponse {
	t.Helper()
	var response hookEventAnswerResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	return response
}

func answerRequest(t *testing.T, h *Handler, runtimeID, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := withURLParams(
		newRequestAsUser(testUserID, http.MethodGet, "/api/runtimes/"+runtimeID+"/hooks/answer?"+query, nil),
		"runtimeId", runtimeID,
	)
	w := httptest.NewRecorder()
	h.AnswerHookEvent(w, req)
	return w
}

func seedHookSnapshot(t *testing.T, h *Handler, runtimeID, provider, scope, hooks, disabled string, observedAt time.Time) {
	t.Helper()
	if err := h.Queries.UpsertHookStateSnapshot(context.Background(), db.UpsertHookStateSnapshotParams{
		RuntimeID: parseUUID(runtimeID), Provider: provider, Scope: scope, Format: "json",
		Hooks: []byte(hooks), DisabledHooks: []byte(disabled),
		SourcePath: pgtype.Text{String: "/home/u/." + provider + "/" + scope + "-settings.json", Valid: true},
		// The column's check constraint takes a lowercase sha256 hex digest.
		ContentHash: pgtype.Text{String: fmt.Sprintf("%x", sha256.Sum256([]byte(scope+hooks))), Valid: true},
		ObservedAt:  pgtype.Timestamptz{Time: observedAt, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAnswerHookEventReportsTheMatchedSetAndItsObservationDate(t *testing.T) {
	h, runtimeID, _, _ := hookReadWebHandler(t, "claude")
	observedAt := time.Now().UTC().Truncate(time.Microsecond)
	seedHookSnapshot(t, h, runtimeID, "claude", "user",
		`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"audit.sh"}]}]}`, `{}`, observedAt)

	w := answerRequest(t, h, runtimeID, "event=PreToolUse&value=Bash")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	response := decodeHookAnswerResponse(t, w)
	if response.Answer == nil {
		t.Fatalf("no answer in %s", w.Body.String())
	}
	if !response.Answer.Answerable || len(response.Answer.Matched) != 1 {
		t.Fatalf("answer = %+v, want one matched entry", response.Answer)
	}
	// Snapshot invariant 3: an answer without its observation date would be an
	// undated claim about a host.
	if response.ObservedAt == nil || !response.ObservedAt.Equal(observedAt) {
		t.Fatalf("observed_at = %v, want %v", response.ObservedAt, observedAt)
	}
	if response.Cached {
		t.Fatal("an online runtime's answer is not a last-known one")
	}
}

// An offline runtime is answered from the stored snapshot, and says so, for
// the same reason the tab's own read does.
func TestAnswerHookEventMarksAnOfflineRuntimeAnswerAsCached(t *testing.T) {
	h, runtimeID, _, _ := hookReadWebHandler(t, "claude")
	seedHookSnapshot(t, h, runtimeID, "claude", "user",
		`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"audit.sh"}]}]}`, `{}`,
		time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond))
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent_runtime SET status = 'offline' WHERE id = $1`, runtimeID); err != nil {
		t.Fatal(err)
	}

	response := decodeHookAnswerResponse(t, answerRequest(t, h, runtimeID, "event=PreToolUse&value=Bash"))
	if !response.Cached {
		t.Fatalf("an offline runtime's answer must read as last known: %+v", response)
	}
	if response.Answer == nil || len(response.Answer.Matched) != 1 {
		t.Fatalf("answer = %+v, want the stored snapshot's answer", response.Answer)
	}
}

// A matcher is evaluated against a value, so a request without one has no
// answer. Refusing it is what stops a valueless read being rendered as
// "these fire".
func TestAnswerHookEventRequiresAnEventAndAValue(t *testing.T) {
	h, runtimeID, _, _ := hookReadWebHandler(t, "claude")
	seedHookSnapshot(t, h, runtimeID, "claude", "user", `{}`, `{}`, time.Now().UTC())

	for _, query := range []string{"", "event=PreToolUse", "value=Bash", "event=PreToolUse&value="} {
		w := answerRequest(t, h, runtimeID, query)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("query %q: status = %d, want 400 (body=%s)", query, w.Code, w.Body.String())
		}
	}
}

// No observation at all is reported as having nothing to answer from. It is
// not served as an answerable event with an empty matched set, because that
// would read as "nothing runs" for a host nobody has read.
func TestAnswerHookEventReportsAnUnobservedRuntimeInsteadOfAnEmptyAnswer(t *testing.T) {
	h, runtimeID, _, _ := hookReadWebHandler(t, "claude")

	w := answerRequest(t, h, runtimeID, "event=PreToolUse&value=Bash")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	response := decodeHookAnswerResponse(t, w)
	if response.Answer != nil {
		t.Fatalf("an unobserved runtime must carry no answer: %+v", response.Answer)
	}
	if response.Error == "" {
		t.Fatal("an unobserved runtime must say so")
	}
	if response.ObservedAt != nil {
		t.Fatalf("observed_at = %v, want none", response.ObservedAt)
	}
}

// The defect class, end to end: an unevaluable matcher reaches the client as
// answerable false with the compiler's message, never as an empty answer.
func TestAnswerHookEventSurfacesAnUnevaluableMatcher(t *testing.T) {
	h, runtimeID, _, _ := hookReadWebHandler(t, "claude")
	seedHookSnapshot(t, h, runtimeID, "claude", "user",
		`{"PreToolUse":[`+
			`{"matcher":"^(?!Notebook).*","hooks":[{"type":"command","command":"guard.sh"}]},`+
			`{"matcher":"Bash","hooks":[{"type":"command","command":"audit.sh"}]}]}`, `{}`,
		time.Now().UTC().Truncate(time.Microsecond))

	response := decodeHookAnswerResponse(t, answerRequest(t, h, runtimeID, "event=PreToolUse&value=Bash"))
	if response.Answer == nil {
		t.Fatal("an unanswerable event still returns an answer object carrying why")
	}
	if response.Answer.Answerable {
		t.Fatalf("answer = %+v, want unanswerable", response.Answer)
	}
	if len(response.Answer.Unevaluable) != 1 || response.Answer.Unevaluable[0].Error == "" {
		t.Fatalf("unevaluable = %+v, want the offending matcher and its message", response.Answer.Unevaluable)
	}
	if len(response.Answer.Matched) != 0 {
		t.Fatalf("matched = %+v, want nothing asserted", response.Answer.Matched)
	}
}

func TestAnswerHookEventRejectsAProviderWithoutHooks(t *testing.T) {
	h, runtimeID, _, _ := hookReadWebHandler(t, "gemini")
	w := answerRequest(t, h, runtimeID, "event=PreToolUse&value=Bash")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
}

// The static /hooks/answer segment is registered beside the /hooks/{requestId}
// wildcard in cmd/server/router.go. This holds chi's static-before-param
// precedence, which is the assumption that registration rests on: without it
// an answer request would be served as a read-request poll for a request id
// literally named "answer".
func TestHookAnswerRouteWinsOverTheRequestIDWildcard(t *testing.T) {
	router := chi.NewRouter()
	var reached string
	router.Route("/api/runtimes/{runtimeId}", func(r chi.Router) {
		r.Get("/hooks/answer", func(http.ResponseWriter, *http.Request) { reached = "answer" })
		r.Get("/hooks/{requestId}", func(_ http.ResponseWriter, req *http.Request) {
			reached = "read:" + chi.URLParam(req, "requestId")
		})
	})

	for _, tt := range []struct{ path, want string }{
		{"/api/runtimes/rt-1/hooks/answer?event=PreToolUse&value=Bash", "answer"},
		{"/api/runtimes/rt-1/hooks/answer", "answer"},
		{"/api/runtimes/rt-1/hooks/req-9", "read:req-9"},
	} {
		reached = ""
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, tt.path, nil))
		if reached != tt.want {
			t.Fatalf("%s reached %q, want %q", tt.path, reached, tt.want)
		}
	}
}

// The handler decides no provider rule of its own: the response's answer is
// byte-identical to calling the resolution layer directly on the same rows.
// A second opinion forming in the handler would show up here.
func TestAnswerHookEventDelegatesEveryRuleToTheResolutionLayer(t *testing.T) {
	h, runtimeID, _, _ := hookReadWebHandler(t, "codex")
	hooks := `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"audit.sh"}]}]}`
	seedHookSnapshot(t, h, runtimeID, "codex", "user", hooks, `{}`, time.Now().UTC().Truncate(time.Microsecond))

	response := decodeHookAnswerResponse(t, answerRequest(t, h, runtimeID, "event=PreToolUse&value=Bash"))
	rows, err := h.Queries.ListHookStateSnapshot(context.Background(), db.ListHookStateSnapshotParams{
		RuntimeID: parseUUID(runtimeID), Provider: "codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	refs, observed, err := hookResolutionInputs("codex", rows)
	if err != nil {
		t.Fatal(err)
	}
	want := runtimehooks.AnswerEvent(runtimehooks.ProviderCodex, refs, observed, "PreToolUse", "Bash")

	got, err := json.Marshal(response.Answer)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := json.Marshal(&want)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(expected) {
		t.Fatalf("handler answer diverged from the resolution layer:\n got %s\nwant %s", got, expected)
	}
}
