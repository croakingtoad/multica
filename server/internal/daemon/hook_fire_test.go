package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func intPointer(value int) *int { return &value }

func writeClaudeHookDebugLog(t *testing.T, capture *claudeHookFireCapture, content string) {
	t.Helper()
	if err := os.WriteFile(capture.debugPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeHookFireCaptureJoinsStructuredResponsesToRealDebugRecords(t *testing.T) {
	capture := &claudeHookFireCapture{debugPath: filepath.Join(t.TempDir(), "debug.log")}
	writeClaudeHookDebugLog(t, capture, `2026-09-12T12:00:00.123Z [DEBUG] "Hook PostToolUse:Write (PostToolUse) success:\nhook-ran"
2026-09-12T12:00:01.123Z [DEBUG] "Hook Stop (Stop) error:\nqc-boom"
2026-09-12T12:00:02.123Z [DEBUG] "Hook Stop (Stop) error:\n/bin/sh: 1: /nonexistent/qc-binary-does-not-exist: not found"
`)
	capture.observe(agent.ClaudeHookResponse{
		HookID: "stream-success", HookName: "PostToolUse:Write", HookEvent: "PostToolUse",
		Stdout: "hook-ran", Output: "hook-ran", ExitCode: intPointer(0), Outcome: "success",
	})
	capture.observe(agent.ClaudeHookResponse{
		HookID: "stream-failure", HookName: "Stop", HookEvent: "Stop",
		Stderr: "qc-boom", Output: "qc-boom", ExitCode: intPointer(1), Outcome: "error",
	})
	capture.observe(agent.ClaudeHookResponse{
		HookID: "stream-never-ran", HookName: "Stop", HookEvent: "Stop",
		Stderr:   "/bin/sh: 1: /nonexistent/qc-binary-does-not-exist: not found",
		Output:   "/bin/sh: 1: /nonexistent/qc-binary-does-not-exist: not found",
		ExitCode: intPointer(127), Outcome: "error",
	})

	fires, err := capture.read("runtime-1", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(fires) != 3 {
		t.Fatalf("fires = %#v, want three joined observations", fires)
	}
	wantOutcomes := []string{"success", "failure", "skipped"}
	wantIDs := []string{"stream-success", "stream-failure", "stream-never-ran"}
	for i, fire := range fires {
		if fire.Outcome != wantOutcomes[i] || fire.HookID != wantIDs[i] {
			t.Errorf("fire %d = %#v, want outcome %q and hook id %q", i, fire, wantOutcomes[i], wantIDs[i])
		}
		if len(fire.HookSpec) == 0 || fire.FiredAt == "" || fire.ID == "" {
			t.Errorf("fire %d omitted observed identity or debug timestamp: %#v", i, fire)
		}
	}
	if got := string(fires[0].HookSpec); got != `{"hook_name":"PostToolUse:Write","type":"claude_hook_response"}` {
		t.Fatalf("hook spec = %s", got)
	}
}

func TestClaudeHookFireCapturePersistsMatcherInsensitiveMultipleHandlers(t *testing.T) {
	capture := &claudeHookFireCapture{debugPath: filepath.Join(t.TempDir(), "debug.log")}
	writeClaudeHookDebugLog(t, capture, `2026-09-12T12:00:00Z [DEBUG] "Hook Stop (Stop) success:\nsame-output"
2026-09-12T12:00:01Z [DEBUG] "Hook Stop (Stop) success:\nsame-output"
2026-09-12T12:00:02Z [DEBUG] "Hook Stop (Stop) success:\nsame-output"
`)
	for _, id := range []string{"stream-one", "stream-two", "stream-three"} {
		capture.observe(agent.ClaudeHookResponse{
			HookID: id, HookName: "Stop", HookEvent: "Stop",
			Stdout: "same-output", Output: "same-output", ExitCode: intPointer(0), Outcome: "success",
		})
	}
	fires, err := capture.read("runtime-1", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(fires) != 3 {
		t.Fatalf("matcher-insensitive fires = %#v, want all three", fires)
	}
}

func TestClaudeHookFireCaptureRequiresStructuredAndDebugEvidence(t *testing.T) {
	// Coverage limit: Claude does not write a debug record for a hook with no
	// output. Such a structured-only response has neither a debug_log source
	// record nor its timestamp, so the ingestion path intentionally omits it.
	capture := &claudeHookFireCapture{debugPath: filepath.Join(t.TempDir(), "debug.log")}
	writeClaudeHookDebugLog(t, capture, "2026-09-12T12:00:00Z [DEBUG] Hook Stop (Stop) success:\ndebug-only\n")
	capture.observe(agent.ClaudeHookResponse{
		HookID: "stream-only", HookName: "Stop", HookEvent: "Stop",
		ExitCode: intPointer(0), Outcome: "success",
	})
	fires, err := capture.read("runtime-1", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(fires) != 0 {
		t.Fatalf("unmatched observations produced fires: %#v", fires)
	}
}

func TestClaudeHookFireCaptureDoesNotAttributeAbsentHandlerToSnapshot(t *testing.T) {
	capture := &claudeHookFireCapture{debugPath: filepath.Join(t.TempDir(), "debug.log")}
	writeClaudeHookDebugLog(t, capture, "2026-09-12T12:00:00Z [DEBUG] Hook Stop (Stop) success:\nplugin-output\n")
	capture.observe(agent.ClaudeHookResponse{
		HookID: "plugin-execution-id", HookName: "Stop", HookEvent: "Stop",
		Stdout: "plugin-output", Output: "plugin-output", ExitCode: intPointer(0), Outcome: "success",
	})
	fires, err := capture.read("runtime-1", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(fires) != 1 {
		t.Fatalf("fires = %#v, want one positively observed plugin fire", fires)
	}
	if fires[0].HookID != "plugin-execution-id" || strings.Contains(string(fires[0].HookSpec), "configured-command") {
		t.Fatalf("absent handler was attributed to snapshot state: %#v", fires[0])
	}
}

func TestHookFireOutcomeDoesNotCallUnknownErrorFailure(t *testing.T) {
	tests := []struct {
		name      string
		response  agent.ClaudeHookResponse
		firstLine string
		want      string
	}{
		{
			name: "real shell command not found", response: agent.ClaudeHookResponse{Outcome: "error", ExitCode: intPointer(127)},
			firstLine: "/bin/sh: 1: /nonexistent/qc-binary-does-not-exist: not found", want: "skipped",
		},
		{name: "missing exit status", response: agent.ClaudeHookResponse{Outcome: "error"}, firstLine: "unclassified", want: "unknown"},
		{name: "executed failure", response: agent.ClaudeHookResponse{Outcome: "error", ExitCode: intPointer(1)}, firstLine: "qc-boom", want: "failure"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hookFireOutcome(test.response, test.firstLine); got != test.want {
				t.Fatalf("outcome = %q, want %q", got, test.want)
			}
		})
	}
}

func TestReportHookFiresUsesCapabilityGatedEndpoint(t *testing.T) {
	var gotPath string
	var gotCapability string
	var got protocol.HookFireReport
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotCapability = r.Header.Get("X-Client-Capabilities")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL)
	client.SetToken("token")
	report := protocol.HookFireReport{Fires: []protocol.HookFire{{ID: "019946d0-e800-7000-8000-000000000001"}}}
	if err := client.ReportHookFires(context.Background(), "runtime-1", report); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/daemon/runtimes/runtime-1/hook-fires" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotCapability == "" || got.Fires[0].ID != report.Fires[0].ID {
		t.Fatalf("capability/report missing: capability=%q report=%#v", gotCapability, got)
	}
}

func TestHookFireIDIsStableAndRecordSpecific(t *testing.T) {
	firedAt := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	a := hookFireID("runtime", "task", "stream-a", firedAt)
	b := hookFireID("runtime", "task", "stream-a", firedAt)
	c := hookFireID("runtime", "task", "stream-b", firedAt)
	if a != b || a == c {
		t.Fatalf("ids a=%q b=%q c=%q", a, b, c)
	}
}
