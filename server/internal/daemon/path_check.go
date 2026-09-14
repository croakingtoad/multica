package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// Reason values a path check can terminate with. They must stay in lockstep
// with the server-side constants (handler.PathCheckReason*) and with the
// desktop picker's vocabulary (packages/views/platform/local-directory.ts):
// the UI renders the same specific message whether the verdict came from the
// Electron preload bridge or from a remote daemon.
const (
	pathCheckReasonNotAbsolute   = "not_absolute"
	pathCheckReasonNotFound      = "not_found"
	pathCheckReasonNotADirectory = "not_a_directory"
	pathCheckReasonNotReadable   = "not_readable"
	pathCheckReasonNotWritable   = "not_writable"
)

// pathCheckVerdict is the bounded result of inspecting one absolute path.
// One path in, one boolean bundle out: no file names, no contents, no
// listing — deliberately nothing that could turn the endpoint into a
// filesystem-enumeration oracle (LOCO-1772).
type pathCheckVerdict struct {
	Exists      bool   `json:"exists"`
	IsDirectory bool   `json:"is_directory"`
	Readable    bool   `json:"readable"`
	Writable    bool   `json:"writable"`
	IsGitRepo   bool   `json:"is_git_repo"`
	Reason      string `json:"reason,omitempty"`
	// Error is set (and status "failed" is reported) for unexpected OS
	// failures that do not map to one of the reason values above.
	Error string `json:"error,omitempty"`
}

// checkLocalPath inspects path and returns the verdict the picker needs.
// The check order and reasons mirror the desktop validateLocalDirectory
// implementation (apps/desktop/src/main/local-directory.ts) exactly —
// not_absolute, not_found, not_a_directory, not_readable, not_writable — so
// a directory that is valid in the desktop shell is valid here, and
// is_git_repo means the same thing ("inside a git working tree") because it
// drives the worktree vs in_place choice.
func checkLocalPath(path string) pathCheckVerdict {
	if !isNonEmptyAbsolutePath(path) {
		return pathCheckVerdict{Reason: pathCheckReasonNotAbsolute}
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return pathCheckVerdict{Reason: pathCheckReasonNotFound}
		}
		// EACCES on a parent component or the like: the check ran and
		// cannot decide, which is not the same as "not found". Report it as
		// a failure with the OS error so the UI shows a generic error
		// instead of a misleading not_found.
		return pathCheckVerdict{Error: err.Error()}
	}
	if !info.IsDir() {
		return pathCheckVerdict{Exists: true, Reason: pathCheckReasonNotADirectory}
	}

	readable := accessAllowed(path, pathAccessReadOnly)
	if !readable {
		return pathCheckVerdict{Exists: true, IsDirectory: true, Reason: pathCheckReasonNotReadable}
	}
	if !accessAllowed(path, pathAccessWriteOnly) {
		return pathCheckVerdict{Exists: true, IsDirectory: true, Readable: true, Reason: pathCheckReasonNotWritable}
	}

	return pathCheckVerdict{
		Exists:      true,
		IsDirectory: true,
		Readable:    true,
		Writable:    true,
		IsGitRepo:   insideGitWorkTree(path),
	}
}

// isNonEmptyAbsolutePath is the daemon-side re-check of the server's
// boundary validation. The server already rejects empty / non-absolute
// paths, so this only fires if a future server regression (or a
// misconfigured relay) forwards one — it must stay fail-closed.
func isNonEmptyAbsolutePath(path string) bool {
	trimmed := strings.TrimSpace(path)
	return trimmed != "" && filepath.IsAbs(trimmed)
}

// accessAllowed wraps the platform accessMode (access(2) / Windows
// access()) so tests can stub it — e.g. the unreadable directory case,
// which is meaningless when running as root.
var accessAllowed = func(path string, mode int) bool {
	return accessMode(path, mode) == nil
}

// insideGitWorkTree walks up from path looking for a `.git` entry, mirroring
// how git itself resolves a working tree — so a subdirectory of a repo
// reports true. `.git` is accepted as either a directory (ordinary clone) or
// a file (a linked worktree, where it holds a gitdir pointer). Any error
// means "can't tell", which is reported as not-a-repo: this only drives a
// UI hint, and the daemon re-checks authoritatively before running anything.
//
// The walk starts from the symlink-resolved path when resolution succeeds,
// via canonicalPath (canonical_path.go), so a symlinked repo root is
// detected the same way the task-time git invocation sees it.
func insideGitWorkTree(path string) bool {
	root := path
	if real, err := canonicalPath(path); err == nil {
		root = real
	}
	for {
		if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(root)
		if parent == root {
			return false
		}
		root = parent
	}
}

// handlePathCheck executes one heartbeat-carried path check and reports the
// verdict back. The path arrives in the heartbeat payload (see
// protocol.DaemonHeartbeatPendingPathCheck); requestID correlates the report
// to the server-side request record the caller polls.
func (d *Daemon) handlePathCheck(ctx context.Context, rt Runtime, pending PendingPathCheck) {
	d.logger.Info("daemon path check requested", "runtime_id", rt.ID, "request_id", pending.ID)

	verdict := checkLocalPath(pending.Path)
	if verdict.Error != "" {
		d.reportPathCheckResult(ctx, rt, pending.ID, map[string]any{
			"status": "failed",
			"error":  verdict.Error,
		})
		return
	}
	d.reportPathCheckResult(ctx, rt, pending.ID, map[string]any{
		"status":       "completed",
		"exists":       verdict.Exists,
		"is_directory": verdict.IsDirectory,
		"readable":     verdict.Readable,
		"writable":     verdict.Writable,
		"is_git_repo":  verdict.IsGitRepo,
		"reason":       verdict.Reason,
	})
}

// reportPathCheckResult delivers the verdict to the server with retry on
// transient failures. Same schedule and semantics as the other daemon→server
// async result reports.
func (d *Daemon) reportPathCheckResult(ctx context.Context, rt Runtime, requestID string, payload map[string]any) {
	d.reportRuntimeResultWithRetry(ctx, "path_check", rt.ID, requestID, func(ctx context.Context) error {
		return d.client.ReportPathCheckResult(ctx, rt.ID, requestID, payload)
	})
}
