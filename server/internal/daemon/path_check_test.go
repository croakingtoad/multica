package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubAccess swaps the accessAllowed hook and restores the real one on
// cleanup. The unreadable / not-writable cases go through this stub: when
// the suite runs as root, the kernel answers "yes" to every access(2) call
// and the OS-level cases become untestable.
func stubAccess(t *testing.T, fn func(path string, mode int) bool) {
	t.Helper()
	prev := accessAllowed
	accessAllowed = fn
	t.Cleanup(func() { accessAllowed = prev })
}

func makeGitRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("create .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatalf("write HEAD: %v", err)
	}
}

func TestCheckLocalPath(t *testing.T) {
	tmp := t.TempDir()

	repo := filepath.Join(tmp, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	makeGitRepo(t, repo)
	linked := filepath.Join(repo, "linked-worktree")
	if err := os.Mkdir(linked, 0o755); err != nil {
		t.Fatalf("mkdir linked: %v", err)
	}
	// A linked worktree's .git is a file holding a gitdir pointer.
	if err := os.WriteFile(filepath.Join(linked, ".git"), []byte("gitdir: "+filepath.Join(repo, ".git", "worktrees", "linked")+"\n"), 0o644); err != nil {
		t.Fatalf("write worktree .git file: %v", err)
	}

	aFile := filepath.Join(tmp, "a-file")
	if err := os.WriteFile(aFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	plain := filepath.Join(tmp, "plain")
	if err := os.Mkdir(plain, 0o755); err != nil {
		t.Fatalf("mkdir plain: %v", err)
	}

	unreadable := filepath.Join(tmp, "unreadable")
	if err := os.Mkdir(unreadable, 0o755); err != nil {
		t.Fatalf("mkdir unreadable: %v", err)
	}
	notWritable := filepath.Join(tmp, "not-writable")
	if err := os.Mkdir(notWritable, 0o755); err != nil {
		t.Fatalf("mkdir not-writable: %v", err)
	}

	tests := []struct {
		name       string
		path       string
		want       pathCheckVerdict
		wantFailed bool // status failed (Error set), not a reason verdict
	}{
		{
			name: "empty path",
			path: "",
			want: pathCheckVerdict{Reason: pathCheckReasonNotAbsolute},
		},
		{
			name: "relative path",
			path: "relative/project",
			want: pathCheckVerdict{Reason: pathCheckReasonNotAbsolute},
		},
		{
			name: "missing path",
			path: filepath.Join(tmp, "does-not-exist"),
			want: pathCheckVerdict{Reason: pathCheckReasonNotFound},
		},
		{
			name: "file is not a directory",
			path: aFile,
			want: pathCheckVerdict{Exists: true, Reason: pathCheckReasonNotADirectory},
		},
		{
			name: "existing non-git directory",
			path: plain,
			want: pathCheckVerdict{
				Exists: true, IsDirectory: true, Readable: true, Writable: true, IsGitRepo: false,
			},
		},
		{
			name: "existing git repo",
			path: repo,
			want: pathCheckVerdict{
				Exists: true, IsDirectory: true, Readable: true, Writable: true, IsGitRepo: true,
			},
		},
		{
			name: "subdirectory of a git repo",
			path: filepath.Join(repo, "src", "deep"),
			want: pathCheckVerdict{
				Exists: true, IsDirectory: true, Readable: true, Writable: true, IsGitRepo: true,
			},
		},
		{
			name: "linked worktree (.git is a file)",
			path: linked,
			want: pathCheckVerdict{
				Exists: true, IsDirectory: true, Readable: true, Writable: true, IsGitRepo: true,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "subdirectory of a git repo" {
				if err := os.MkdirAll(filepath.Join(repo, "src", "deep"), 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
			}
			got := checkLocalPath(tc.path)
			if got != tc.want {
				t.Fatalf("checkLocalPath(%q) = %+v, want %+v", tc.path, got, tc.want)
			}
		})
	}

	t.Run("unreadable directory", func(t *testing.T) {
		stubAccess(t, func(path string, mode int) bool {
			if mode == pathAccessReadOnly {
				return false
			}
			return true
		})
		got := checkLocalPath(unreadable)
		want := pathCheckVerdict{Exists: true, IsDirectory: true, Reason: pathCheckReasonNotReadable}
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("not writable directory", func(t *testing.T) {
		stubAccess(t, func(path string, mode int) bool {
			if mode == pathAccessWriteOnly {
				return false
			}
			return true
		})
		got := checkLocalPath(notWritable)
		want := pathCheckVerdict{Exists: true, IsDirectory: true, Readable: true, Reason: pathCheckReasonNotWritable}
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("unstatable path reports failed", func(t *testing.T) {
		// A symlink loop makes stat fail with ELOOP — neither "not found"
		// nor a reason the picker can render, so the check must come back
		// as a failure with the OS error.
		loop := filepath.Join(tmp, "loop-a")
		loopB := filepath.Join(tmp, "loop-b")
		if err := os.Symlink(loopB, loop); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}
		if err := os.Symlink(loop, loopB); err != nil {
			t.Fatalf("create loop: %v", err)
		}
		got := checkLocalPath(loop)
		if got.Error == "" {
			t.Fatalf("expected an error verdict, got %+v", got)
		}
		if got.Reason != "" {
			t.Fatalf("failed verdict must not carry a reason, got %+v", got)
		}
	})
}

func TestHandlePathCheck_ReportsCompletedVerdict(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}

	tmp := t.TempDir()
	makeGitRepo(t, tmp)

	d.handlePathCheck(context.Background(), Runtime{ID: "rt-1"}, PendingPathCheck{ID: "req-1", Path: tmp})

	wantPath := "/api/daemon/runtimes/rt-1/path-checks/req-1/result"
	if gotPath != wantPath {
		t.Fatalf("report path = %q, want %q", gotPath, wantPath)
	}
	if gotBody["status"] != "completed" {
		t.Fatalf("status = %v, want completed: %v", gotBody["status"], gotBody)
	}
	for _, field := range []string{"exists", "is_directory", "readable", "writable", "is_git_repo"} {
		if gotBody[field] != true {
			t.Fatalf("%s = %v, want true: %v", field, gotBody[field], gotBody)
		}
	}
	if reason, ok := gotBody["reason"]; ok && reason != "" {
		t.Fatalf("reason = %v, want empty: %v", reason, gotBody)
	}
}

func TestHandlePathCheck_ReportsNotFoundVerdict(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}

	missing := filepath.Join(t.TempDir(), "nope")
	d.handlePathCheck(context.Background(), Runtime{ID: "rt-2"}, PendingPathCheck{ID: "req-2", Path: missing})

	if gotBody["status"] != "completed" {
		t.Fatalf("status = %v, want completed", gotBody["status"])
	}
	if gotBody["exists"] != false || gotBody["reason"] != "not_found" {
		t.Fatalf("verdict = %v, want exists=false reason=not_found", gotBody)
	}
}

func TestHandlePathCheck_ReportsFailedOnUnstatablePath(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}

	tmp := t.TempDir()
	loopA := filepath.Join(tmp, "loop-a")
	loopB := filepath.Join(tmp, "loop-b")
	if err := os.Symlink(loopB, loopA); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if err := os.Symlink(loopA, loopB); err != nil {
		t.Fatalf("create loop: %v", err)
	}

	d.handlePathCheck(context.Background(), Runtime{ID: "rt-3"}, PendingPathCheck{ID: "req-3", Path: loopA})

	if gotBody["status"] != "failed" {
		t.Fatalf("status = %v, want failed", gotBody["status"])
	}
	if errStr, _ := gotBody["error"].(string); errStr == "" || !strings.Contains(errStr, "loop") {
		t.Fatalf("error = %v, want an ELOOP-style message", gotBody["error"])
	}
}
