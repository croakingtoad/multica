package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/redis/go-redis/v9"
)

type DaemonPathCheckStatus string

const (
	DaemonPathCheckPending   DaemonPathCheckStatus = "pending"
	DaemonPathCheckRunning   DaemonPathCheckStatus = "running"
	DaemonPathCheckCompleted DaemonPathCheckStatus = "completed"
	DaemonPathCheckFailed    DaemonPathCheckStatus = "failed"
	DaemonPathCheckTimeout   DaemonPathCheckStatus = "timeout"
)

// Reason values a path check can terminate with. They are the vocabulary the
// UI already speaks for local directories (packages/views/platform/
// local-directory.ts): the picker renders the same specific message whether
// the verdict came from the Electron preload bridge or from a remote daemon.
// not_absolute is produced only at the server API boundary (the daemon never
// sees a non-absolute path); the remaining four are produced by the daemon.
const (
	PathCheckReasonNotAbsolute   = "not_absolute"
	PathCheckReasonNotFound      = "not_found"
	PathCheckReasonNotADirectory = "not_a_directory"
	PathCheckReasonNotReadable   = "not_readable"
	PathCheckReasonNotWritable   = "not_writable"
)

const (
	// daemonPathCheckPendingTimeout bounds how long a request can sit in
	// pending before the server marks it timed out. The daemon claims on a
	// hint-driven heartbeat (~1s) or, if the hint is lost, on its next
	// scheduled 15s tick; 90s leaves wide margin for both.
	daemonPathCheckPendingTimeout = 90 * time.Second
	// daemonPathCheckRunningTimeout bounds the daemon's inspection + report.
	// A path check is a handful of stat/access calls plus a `.git` walk —
	// anything past 30s means the daemon is wedged, not slow.
	daemonPathCheckRunningTimeout = 30 * time.Second
	daemonPathCheckStoreRetention = 5 * time.Minute
)

// DefaultPathCheckRateLimit is the per-user budget for initiating path
// checks: 10/minute keeps a picker comfortable (one check per directory
// the user browses to) while making the endpoint useless as a
// filesystem-probing loop. In the same spirit as the daemon-side
// pendingWorkHintMinInterval throttle, which caps how fast hint-driven
// heartbeats can amplify.
func DefaultPathCheckRateLimit() SlidingWindowRateLimit {
	return SlidingWindowRateLimit{Limit: 10, Window: time.Minute}
}

// pathCheckRateLimiterKeyPrefix namespaces the per-user budgets so they
// never collide with the webhook or invitation gates sharing the
// sliding-window implementation.
const pathCheckRateLimiterKeyPrefix = "mul:path_check:user:"

// NewMemoryPathCheckRateLimiter builds a single-process per-user path-check
// budget for local and Redis-less deployments.
func NewMemoryPathCheckRateLimiter(cfg SlidingWindowRateLimit) SlidingWindowRateLimiter {
	return newMemorySlidingWindowRateLimiter(cfg)
}

// NewRedisPathCheckRateLimiter builds a replica-shared per-user path-check
// budget with an isolated key namespace.
func NewRedisPathCheckRateLimiter(rdb *redis.Client, cfg SlidingWindowRateLimit) SlidingWindowRateLimiter {
	return newRedisSlidingWindowRateLimiter(rdb, cfg, pathCheckRateLimiterKeyPrefix)
}

// PathCheckResult is the daemon's verdict on one absolute path. Every field
// is a plain boolean so the wire stays trivial to decode and the whole
// response is one bounded bundle — no file names, no contents, no listing.
// Reason is empty when the path passed every check.
type PathCheckResult struct {
	Exists      bool
	IsDirectory bool
	Readable    bool
	Writable    bool
	IsGitRepo   bool
	Reason      string
}

// DaemonPathCheckRequest is the pollable record for one path check.
type DaemonPathCheckRequest struct {
	ID           string                `json:"id"`
	RuntimeID    string                `json:"runtime_id"`
	Path         string                `json:"path"`
	Status       DaemonPathCheckStatus `json:"status"`
	Exists       bool                  `json:"exists"`
	IsDirectory  bool                  `json:"is_directory"`
	Readable     bool                  `json:"readable"`
	Writable     bool                  `json:"writable"`
	IsGitRepo    bool                  `json:"is_git_repo"`
	Reason       string                `json:"reason,omitempty"`
	Error        string                `json:"error,omitempty"`
	CreatedAt    time.Time             `json:"created_at"`
	UpdatedAt    time.Time             `json:"updated_at"`
	RunStartedAt *time.Time            `json:"-"`
}

func applyDaemonPathCheckTimeout(req *DaemonPathCheckRequest, now time.Time) bool {
	switch req.Status {
	case DaemonPathCheckPending:
		if now.Sub(req.CreatedAt) > daemonPathCheckPendingTimeout {
			req.Status = DaemonPathCheckTimeout
			req.Error = "daemon did not respond within 90 seconds"
			req.UpdatedAt = now
			return true
		}
	case DaemonPathCheckRunning:
		if req.RunStartedAt != nil && now.Sub(*req.RunStartedAt) > daemonPathCheckRunningTimeout {
			req.Status = DaemonPathCheckTimeout
			req.Error = "daemon did not finish within 30 seconds"
			req.UpdatedAt = now
			return true
		}
	}
	return false
}

// PathCheckStore tracks pending / running / completed daemon path checks.
// Like LocalSkillListStore, the server MUST stay stateless: pending state
// lives in shared storage so initiate, heartbeat and poll can land on
// different API nodes and still agree on the request's state.
type PathCheckStore interface {
	Create(ctx context.Context, runtimeID, path string) (*DaemonPathCheckRequest, error)
	Get(ctx context.Context, id string) (*DaemonPathCheckRequest, error)
	// HasPending is a cheap read-only probe used by the heartbeat handler to
	// gate the side-effecting PopPending.
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	PopPending(ctx context.Context, runtimeID string) (*DaemonPathCheckRequest, error)
	Complete(ctx context.Context, id string, result PathCheckResult) error
	Fail(ctx context.Context, id string, errMsg string) error
}

// InMemoryPathCheckStore is the single-node implementation — good enough for
// local dev and the in-process test suite. Production (multi-node) must use
// RedisPathCheckStore so every API node agrees on the same pending set.
type InMemoryPathCheckStore struct {
	mu       sync.Mutex
	requests map[string]*DaemonPathCheckRequest
}

func NewInMemoryPathCheckStore() *InMemoryPathCheckStore {
	return &InMemoryPathCheckStore{requests: make(map[string]*DaemonPathCheckRequest)}
}

func (s *InMemoryPathCheckStore) Create(_ context.Context, runtimeID, path string) (*DaemonPathCheckRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, req := range s.requests {
		if time.Since(req.CreatedAt) > daemonPathCheckStoreRetention {
			delete(s.requests, id)
		}
	}

	now := time.Now()
	req := &DaemonPathCheckRequest{
		ID:        randomID(),
		RuntimeID: runtimeID,
		Path:      path,
		Status:    DaemonPathCheckPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.requests[req.ID] = req
	return req, nil
}

func (s *InMemoryPathCheckStore) Get(_ context.Context, id string) (*DaemonPathCheckRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	applyDaemonPathCheckTimeout(req, time.Now())
	return req, nil
}

func (s *InMemoryPathCheckStore) HasPending(_ context.Context, runtimeID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, req := range s.requests {
		applyDaemonPathCheckTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == DaemonPathCheckPending {
			return true, nil
		}
	}
	return false, nil
}

func (s *InMemoryPathCheckStore) PopPending(_ context.Context, runtimeID string) (*DaemonPathCheckRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var oldest *DaemonPathCheckRequest
	now := time.Now()
	for _, req := range s.requests {
		applyDaemonPathCheckTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == DaemonPathCheckPending {
			if oldest == nil || req.CreatedAt.Before(oldest.CreatedAt) {
				oldest = req
			}
		}
	}
	if oldest != nil {
		oldest.Status = DaemonPathCheckRunning
		startedAt := now
		oldest.RunStartedAt = &startedAt
		oldest.UpdatedAt = now
	}
	return oldest, nil
}

func (s *InMemoryPathCheckStore) Complete(_ context.Context, id string, result PathCheckResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = DaemonPathCheckCompleted
		req.Exists = result.Exists
		req.IsDirectory = result.IsDirectory
		req.Readable = result.Readable
		req.Writable = result.Writable
		req.IsGitRepo = result.IsGitRepo
		req.Reason = result.Reason
		req.UpdatedAt = time.Now()
	}
	return nil
}

func (s *InMemoryPathCheckStore) Fail(_ context.Context, id string, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = DaemonPathCheckFailed
		req.Error = errMsg
		req.UpdatedAt = time.Now()
	}
	return nil
}

// pathCheckRequestTerminal reports whether the request has reached a state
// the daemon cannot change any more.
func pathCheckRequestTerminal(status DaemonPathCheckStatus) bool {
	return status == DaemonPathCheckCompleted || status == DaemonPathCheckFailed ||
		status == DaemonPathCheckTimeout
}

// listOwnedRuntimesForDaemon loads every runtime the member registered
// under daemonID in the workspace, most recently seen first.
//
// The owner predicate in the query is the security boundary, not workspace
// membership: a second member of the workspace who happens to share the
// hostname (and therefore the daemon_id) has no runtime under the query —
// they get 404 and never learn the other user's machine exists.
func (h *Handler) listOwnedRuntimesForDaemon(
	w http.ResponseWriter,
	r *http.Request,
	workspaceID, daemonID string,
	member db.Member,
) ([]db.AgentRuntime, bool) {
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return nil, false
	}
	runtimes, err := h.Queries.ListRuntimesByDaemonOwner(r.Context(), db.ListRuntimesByDaemonOwnerParams{
		WorkspaceID: wsUUID,
		DaemonID:    strToText(daemonID),
		OwnerID:     member.UserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load runtimes")
		return nil, false
	}
	return runtimes, true
}

// resolveOwnedOnlinePathCheckRuntime picks the online runtime the caller
// owns under daemonID in workspaceID — the machine the check runs on. When
// the caller does not own any runtime under the daemon: 404. When they do
// but none is online: 503 with a machine-readable code, never a hang.
func (h *Handler) resolveOwnedOnlinePathCheckRuntime(
	w http.ResponseWriter,
	r *http.Request,
	workspaceID, daemonID string,
	member db.Member,
) (db.AgentRuntime, bool) {
	runtimes, ok := h.listOwnedRuntimesForDaemon(w, r, workspaceID, daemonID, member)
	if !ok {
		return db.AgentRuntime{}, false
	}
	if len(runtimes) == 0 {
		writeError(w, http.StatusNotFound, "not found")
		return db.AgentRuntime{}, false
	}
	for _, rt := range runtimes {
		if rt.Status == "online" {
			return rt, true
		}
	}
	writeErrorCode(w, http.StatusServiceUnavailable, "machine_offline", "daemon is offline")
	return db.AgentRuntime{}, false
}

// checkPathCheckRateLimit enforces the per-user initiation budget. Check is
// non-consuming and fail-open on backend errors (a Redis hiccup must not
// take the picker down); the unit is consumed only after the request is
// enqueued, matching the invitation admission pattern.
func (h *Handler) checkPathCheckRateLimit(w http.ResponseWriter, r *http.Request, userID string) bool {
	allowed, err := slidingWindowLimiterCheckWithError(r.Context(), h.PathCheckRateLimiter, userID)
	if err != nil {
		slog.Warn("path check rate limiter unavailable; allowing request", "error", err)
		return true
	}
	if allowed {
		return true
	}
	retryAfter := slidingWindowLimiterRetryAfter(r.Context(), h.PathCheckRateLimiter, userID)
	seconds := int64((retryAfter + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", fmt.Sprintf("%d", seconds))
	writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many path checks"})
	return false
}

func (h *Handler) consumePathCheckRateLimit(r *http.Request, userID string) {
	if _, err := slidingWindowLimiterAllow(r.Context(), h.PathCheckRateLimiter, userID); err != nil {
		slog.Warn("path check rate limiter consume failed after successful check; allowing bounded overshoot", "error", err)
	}
}

// InitiateDaemonPathCheck enqueues a path check for the caller's daemon and
// wakes it. POST /api/workspaces/{workspaceId}/daemons/{daemonId}/path-checks
//
// Request:  { "path": "/absolute/path" }
// Response: the DaemonPathCheckRequest record (status "pending").
// 400 with reason "not_absolute" when the path is empty or relative — the
// daemon never sees such a request. 404 when the caller does not own a
// runtime under that daemon in that workspace. 503 machine_offline when the
// daemon has no online runtime.
func (h *Handler) InitiateDaemonPathCheck(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceId")
	daemonID := chi.URLParam(r, "daemonId")

	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}

	rt, ok := h.resolveOwnedOnlinePathCheckRuntime(w, r, workspaceID, daemonID, member)
	if !ok {
		return
	}

	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	path := strings.TrimSpace(body.Path)
	if !isAbsoluteLocalPath(path) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":  "path must be an absolute path",
			"reason": PathCheckReasonNotAbsolute,
		})
		return
	}

	userKey := uuidToString(member.UserID)
	if !h.checkPathCheckRateLimit(w, r, userKey) {
		return
	}

	req, err := h.PathCheckStore.Create(r.Context(), uuidToString(rt.ID), path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue path check: "+err.Error())
		return
	}
	h.consumePathCheckRateLimit(r, userKey)
	h.requestDaemonPendingWork(uuidToString(rt.ID), protocol.PendingWorkKindPathCheck)
	writeJSON(w, http.StatusOK, req)
}

// GetDaemonPathCheck polls a path check result.
// GET /api/workspaces/{workspaceId}/daemons/{daemonId}/path-checks/{requestId}
//
// 404 when the request does not exist or does not belong to a runtime the
// caller owns under that daemon — a second user in the workspace can never
// read someone else's result. Terminal statuses: completed, failed, timeout.
func (h *Handler) GetDaemonPathCheck(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceId")
	daemonID := chi.URLParam(r, "daemonId")

	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	ownedRuntimeIDs, ok := h.resolveOwnedRuntimeIDsForPathCheckPoll(w, r, workspaceID, daemonID, member)
	if !ok {
		return
	}

	requestID := chi.URLParam(r, "requestId")
	req, err := h.PathCheckStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load request: "+err.Error())
		return
	}
	if req == nil {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if _, owned := ownedRuntimeIDs[req.RuntimeID]; !owned {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}

	writeJSON(w, http.StatusOK, req)
}

// resolveOwnedRuntimeIDsForPathCheckPoll is the polling variant of the
// resolver: the daemon may have gone offline between initiation and the
// last poll, so it returns every runtime the caller owns under the daemon
// without requiring an online one. The request record names the exact
// runtime it ran on, and the caller only ever gets 200 for a request that
// ran on one of their own runtimes — that match is what keeps a second
// user's requests unreadable.
func (h *Handler) resolveOwnedRuntimeIDsForPathCheckPoll(
	w http.ResponseWriter,
	r *http.Request,
	workspaceID, daemonID string,
	member db.Member,
) (map[string]struct{}, bool) {
	runtimes, ok := h.listOwnedRuntimesForDaemon(w, r, workspaceID, daemonID, member)
	if !ok {
		return nil, false
	}
	if len(runtimes) == 0 {
		writeError(w, http.StatusNotFound, "not found")
		return nil, false
	}
	ownedRuntimeIDs := make(map[string]struct{}, len(runtimes))
	for _, rt := range runtimes {
		ownedRuntimeIDs[uuidToString(rt.ID)] = struct{}{}
	}
	return ownedRuntimeIDs, true
}

// ReportDaemonPathCheckResult receives the daemon's verdict.
// POST /api/daemon/runtimes/{runtimeId}/path-checks/{requestId}/result
//
// Owner-authenticated: current daemons use the owner's CLI PAT/JWT, while
// daemon tokens are bound to the runtime's daemon id.
// Request: { "status": "completed", "exists": true, "is_directory": true,
//
//	"readable": true, "writable": true, "is_git_repo": false, "reason": "" }
//
// or { "status": "failed", "error": "..." }. Responses: 200 {"status":"ok"}
// (also for stale reports after a terminal state), 404 for unknown ids.
func (h *Handler) ReportDaemonPathCheckResult(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtime, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}
	if !daemonPathCheckReporterOwnsRuntime(r, runtime) {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}

	requestID := chi.URLParam(r, "requestId")
	req, err := h.PathCheckStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load request: "+err.Error())
		return
	}
	if req == nil || req.RuntimeID != runtimeID {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if pathCheckRequestTerminal(req.Status) {
		slog.Debug("ignoring stale daemon path check report", "runtime_id", runtimeID, "request_id", requestID, "status", req.Status)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	var body struct {
		Status      string `json:"status"`
		Exists      bool   `json:"exists"`
		IsDirectory bool   `json:"is_directory"`
		Readable    bool   `json:"readable"`
		Writable    bool   `json:"writable"`
		IsGitRepo   bool   `json:"is_git_repo"`
		Reason      string `json:"reason"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if body.Status == string(DaemonPathCheckCompleted) {
		result := PathCheckResult{
			Exists:      body.Exists,
			IsDirectory: body.IsDirectory,
			Readable:    body.Readable,
			Writable:    body.Writable,
			IsGitRepo:   body.IsGitRepo,
			Reason:      strings.TrimSpace(body.Reason),
		}
		if err := h.PathCheckStore.Complete(r.Context(), requestID, result); err != nil {
			slog.Error("path check Complete failed", "error", err, "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "failed to persist completion")
			return
		}
	} else {
		if err := h.PathCheckStore.Fail(r.Context(), requestID, body.Error); err != nil {
			slog.Error("path check Fail failed", "error", err, "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "failed to persist failure")
			return
		}
	}

	slog.Debug("daemon path check report", "runtime_id", runtimeID, "request_id", requestID, "status", body.Status)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func daemonPathCheckReporterOwnsRuntime(r *http.Request, runtime db.AgentRuntime) bool {
	if middleware.DaemonWorkspaceIDFromContext(r.Context()) != "" {
		return middleware.DaemonIDFromContext(r.Context()) == runtime.DaemonID.String
	}
	userID := requestUserID(r)
	return userID != "" && userID == uuidToString(runtime.OwnerID)
}
