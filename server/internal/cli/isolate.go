package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// IsolateConfigRoot redirects the Multica config root to a fresh per-test
// directory for the duration of t and returns that directory.
//
// TestMain in cmd/multica/cmd_auth_test.go unsets MULTICA_TASK_CONFIG_ROOT but
// does not pin HOME, so config resolution can still fall through to the
// ambient home directory. internal/daemon, internal/daemon/execenv, and
// internal/cli have no TestMain and use this helper per test instead.
//
// This is the single entry point any test in this repository should use when
// it wants to read or write the CLI config (CLIConfig, LoadCLIConfig,
// SaveCLIConfig, ProfileDir, CLIConfigPath...) without touching the ambient
// agent config directory. It pins every environment variable that
// multicaConfigRoot consults at or above the HOME fallback:
//
//   - MULTICA_TASK_CONFIG_ROOT (TaskConfigRootEnv) is the only variable that
//     outranks HOME in multicaConfigRoot. Pinning it to "" makes resolution
//     fall through to HOME, preserving the ~/.multica/... path shape that
//     HOME-isolating tests have always expected. Tests that need to exercise
//     the task-local path shape instead set this variable themselves after
//     calling this helper; they must not rely on the helper to leave it set.
//   - HOME and USERPROFILE are the fallback roots (os.UserHomeDir reads HOME on
//     Unix and USERPROFILE on Windows). Pinning both is what actually redirects
//     the root on every platform the test suite runs on.
//
// The guard below makes this class of bug fail loudly instead of silently:
// if a future change to multicaConfigRoot introduces a new variable that
// outranks HOME and this helper forgets to pin it, the resolved root will
// escape the test's own temp dir and every caller fails at setup, rather than
// a test quietly overwriting the live agent config.json and dropping its
// auth token. If you add such a variable to multicaConfigRoot, pin it here.
//
// Production code never calls this.
func IsolateConfigRoot(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	t.Setenv(TaskConfigRootEnv, "")
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)

	resolved, taskLocal, err := multicaConfigRoot()
	if err != nil {
		t.Fatalf("IsolateConfigRoot: resolve config root: %v", err)
	}
	if taskLocal {
		t.Fatalf("IsolateConfigRoot: config root %q resolved as task-local after pinning %s empty; HOME isolation is defeated", resolved, TaskConfigRootEnv)
	}
	if !isWithin(root, resolved) {
		t.Fatalf("IsolateConfigRoot: config root %q escapes the isolated temp dir %q; a test would read or write the live agent config directory", resolved, root)
	}
	return root
}

// isWithin reports whether path p is equal to base or nested beneath it.
func isWithin(base, p string) bool {
	rel, err := filepath.Rel(base, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}
