package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

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

func capturedClaudeHookResponse(t *testing.T, raw string) agent.ClaudeHookResponse {
	t.Helper()
	var response struct {
		HookID    string `json:"hook_id"`
		HookName  string `json:"hook_name"`
		HookEvent string `json:"hook_event"`
		Output    string `json:"output"`
		Stdout    string `json:"stdout"`
		Stderr    string `json:"stderr"`
		ExitCode  *int   `json:"exit_code"`
		Outcome   string `json:"outcome"`
	}
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		t.Fatal(err)
	}
	return agent.ClaudeHookResponse{
		HookID: response.HookID, HookName: response.HookName, HookEvent: response.HookEvent,
		Output: response.Output, Stdout: response.Stdout, Stderr: response.Stderr,
		ExitCode: response.ExitCode, Outcome: response.Outcome,
	}
}

func TestClaudeHookFireCaptureJoinsStructuredResponsesToRealDebugRecords(t *testing.T) {
	capture := &claudeHookFireCapture{debugPath: filepath.Join(t.TempDir(), "debug.log")}
	// These are byte-for-byte debug records and hook_response events captured
	// from Claude Code 2.1.269. In particular, the stream fields retain the
	// trailing newline that the debug logger removes.
	writeClaudeHookDebugLog(t, capture, `2026-09-12T12:23:27.192Z [DEBUG] "Hook Stop (Stop) error:\n/bin/sh: 1: /nonexistent/qc-binary-does-not-exist: not found"
2026-09-12T12:23:27.193Z [DEBUG] "Hook Stop (Stop) error:\nqc-boom ran and failed"
2026-09-12T12:23:27.193Z [DEBUG] "Hook Stop (Stop) success:\nqc-probe-ok output line"
`)
	capture.observe(capturedClaudeHookResponse(t, `{"type":"system","subtype":"hook_response","hook_id":"2d39d186-6234-4653-9706-87ed9c5419e8","hook_name":"Stop","hook_event":"Stop","output":"/bin/sh: 1: /nonexistent/qc-binary-does-not-exist: not found\n","stdout":"","stderr":"/bin/sh: 1: /nonexistent/qc-binary-does-not-exist: not found\n","exit_code":127,"outcome":"error","uuid":"a9f9199c-0c7c-4016-9a08-8e644d198793","session_id":"4b2f06b7-5408-4c3b-b3d5-7c491ab1558d"}`))
	capture.observe(capturedClaudeHookResponse(t, `{"type":"system","subtype":"hook_response","hook_id":"2d0822cf-e5b2-49ca-a537-eaf1758c0cc6","hook_name":"Stop","hook_event":"Stop","output":"qc-boom ran and failed\n","stdout":"","stderr":"qc-boom ran and failed\n","exit_code":1,"outcome":"error","uuid":"c52fc2c2-fc11-43f4-9cfd-9a58b60a6ab3","session_id":"4b2f06b7-5408-4c3b-b3d5-7c491ab1558d"}`))
	capture.observe(capturedClaudeHookResponse(t, `{"type":"system","subtype":"hook_response","hook_id":"52ed1f27-9596-4028-8180-ed6c4096e4e5","hook_name":"Stop","hook_event":"Stop","output":"qc-probe-ok output line\n","stdout":"qc-probe-ok output line\n","stderr":"","exit_code":0,"outcome":"success","uuid":"a34f3aaa-1c6c-462b-9bf5-447b363ff592","session_id":"4b2f06b7-5408-4c3b-b3d5-7c491ab1558d"}`))

	fires, err := capture.read("runtime-1", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(fires) != 3 {
		t.Fatalf("fires = %#v, want three joined observations", fires)
	}
	wantOutcomes := map[string]string{
		"2d39d186-6234-4653-9706-87ed9c5419e8": "unknown",
		"2d0822cf-e5b2-49ca-a537-eaf1758c0cc6": "failure",
		"52ed1f27-9596-4028-8180-ed6c4096e4e5": "success",
	}
	for i, fire := range fires {
		if fire.Outcome != wantOutcomes[fire.ExecutionID] {
			t.Errorf("fire %d = %#v, want outcome %q", i, fire, wantOutcomes[fire.ExecutionID])
		}
		if len(fire.HookSpec) == 0 || fire.FiredAt == "" || fire.ID == "" || fire.Provenance != "debug_log" {
			t.Errorf("fire %d omitted observed identity or debug timestamp: %#v", i, fire)
		}
	}
	if got := string(fires[0].HookSpec); got != `{"hook_name":"Stop","type":"claude_hook_response"}` {
		t.Fatalf("hook spec = %s", got)
	}
}

func TestClaudeHookFireIDDoesNotChangeWhenDebugEvidenceUpgradesRow(t *testing.T) {
	capture := &claudeHookFireCapture{debugPath: filepath.Join(t.TempDir(), "debug.log")}
	capture.observe(agent.ClaudeHookResponse{
		HookID: "execution-1", HookName: "Stop", HookEvent: "Stop",
		Stdout: "same-output\n", ExitCode: intPointer(0), Outcome: "success",
	})
	baseline, err := capture.read("runtime-1", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	writeClaudeHookDebugLog(t, capture, "2020-01-02T03:04:05Z [DEBUG] \"Hook Stop (Stop) success:\\nsame-output\"\n")
	upgraded, err := capture.read("runtime-1", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(baseline) != 1 || len(upgraded) != 1 {
		t.Fatalf("baseline=%#v upgraded=%#v, want one row each", baseline, upgraded)
	}
	if baseline[0].Provenance != "inferred" || upgraded[0].Provenance != "debug_log" {
		t.Fatalf("baseline=%#v upgraded=%#v, want provenance upgrade", baseline[0], upgraded[0])
	}
	if baseline[0].ID != upgraded[0].ID {
		t.Fatalf("row ID changed on debug upgrade: %q != %q", baseline[0].ID, upgraded[0].ID)
	}
}

func TestClaudeHookFireCaptureRefusesAmbiguousAndEmptySignatureUpgrades(t *testing.T) {
	tests := []struct {
		name      string
		debugLog  string
		responses []agent.ClaudeHookResponse
		wantRows  int
	}{
		{
			name:     "ambiguous stream bucket",
			debugLog: "2026-09-12T12:00:00Z [DEBUG] \"Hook Stop (Stop) success:\\nsame-output\"\n",
			responses: []agent.ClaudeHookResponse{
				{HookID: "stream-one", HookName: "Stop", HookEvent: "Stop", Stdout: "same-output", Outcome: "success"},
				{HookID: "stream-two", HookName: "Stop", HookEvent: "Stop", Stdout: "same-output", Outcome: "success"},
			},
			wantRows: 2,
		},
		{
			name: "ambiguous debug bucket",
			debugLog: "2026-09-12T12:00:00Z [DEBUG] \"Hook Stop (Stop) success:\\nsame-output\"\n" +
				"2026-09-12T12:00:01Z [DEBUG] \"Hook Stop (Stop) success:\\nsame-output\"\n",
			responses: []agent.ClaudeHookResponse{
				{HookID: "stream-one", HookName: "Stop", HookEvent: "Stop", Stdout: "same-output", Outcome: "success"},
			},
			wantRows: 1,
		},
		{
			name:     "empty normalized signature",
			debugLog: "2026-09-12T12:00:00Z [DEBUG] \"Hook Stop (Stop) success:\\n   \"\n",
			responses: []agent.ClaudeHookResponse{
				{HookID: "stream-empty", HookName: "Stop", HookEvent: "Stop", Stdout: " \r\n\t", Outcome: "success"},
			},
			wantRows: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			capture := &claudeHookFireCapture{debugPath: filepath.Join(t.TempDir(), "debug.log")}
			writeClaudeHookDebugLog(t, capture, test.debugLog)
			for _, response := range test.responses {
				capture.observe(response)
			}
			fires, err := capture.read("runtime-1", "task-1")
			if err != nil {
				t.Fatal(err)
			}
			if len(fires) != test.wantRows {
				t.Fatalf("fires = %#v, want %d rows", fires, test.wantRows)
			}
			for _, fire := range fires {
				if fire.Provenance != "inferred" {
					t.Fatalf("ambiguous or empty bucket upgraded fire: %#v", fire)
				}
			}
		})
	}
}

func TestClaudeHookFireCaptureEmitsEmptyOutputResponse(t *testing.T) {
	capture := &claudeHookFireCapture{debugPath: filepath.Join(t.TempDir(), "debug.log")}
	capture.observe(agent.ClaudeHookResponse{
		HookID: "stream-only", HookName: "Stop", HookEvent: "Stop",
		ExitCode: intPointer(0), Outcome: "success",
	})
	fires, err := capture.read("runtime-1", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(fires) != 1 || fires[0].Provenance != "inferred" || fires[0].FiredAt == "" {
		t.Fatalf("empty-output response = %#v, want one receipt-timed inferred row", fires)
	}
}

func TestClaudeHookFireCaptureDoesNotAttributeAbsentHandlerToSnapshot(t *testing.T) {
	capture := &claudeHookFireCapture{debugPath: filepath.Join(t.TempDir(), "debug.log")}
	writeClaudeHookDebugLog(t, capture, "2026-09-12T12:00:00Z [DEBUG] \"Hook Stop (Stop) success:\\nplugin-output\"\n")
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
	if fires[0].ExecutionID != "plugin-execution-id" || strings.Contains(string(fires[0].HookSpec), "configured-command") {
		t.Fatalf("absent handler was attributed to snapshot state: %#v", fires[0])
	}
}

func TestClaudeHookFireCaptureJanitorRemovesOnlyOrphanedLogs(t *testing.T) {
	envRoot := t.TempDir()
	debugDir := filepath.Join(envRoot, ".multica", "claude-hook-debug")
	if err := os.MkdirAll(debugDir, 0o700); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(debugDir, "orphan.log")
	if err := os.WriteFile(orphan, []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	first := d.startClaudeHookFireCapture("runtime-1", "task-1", envRoot)
	if first == nil {
		t.Fatal("first capture is nil")
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan stat error = %v, want not exist", err)
	}
	if err := os.WriteFile(first.debugPath, []byte("active"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := d.startClaudeHookFireCapture("runtime-1", "task-2", envRoot)
	if second == nil {
		t.Fatal("second capture is nil")
	}
	if _, err := os.Stat(first.debugPath); err != nil {
		t.Fatalf("active capture removed by janitor: %v", err)
	}
	d.finishClaudeHookFireCapture(second, "runtime-1", "task-2")
	d.finishClaudeHookFireCapture(first, "runtime-1", "task-1")
}

func TestClaudeHookFireCaptureJanitorPreservesConcurrentCaptures(t *testing.T) {
	const captureCount = 16
	envRoot := t.TempDir()
	d := &Daemon{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	active := make([]*claudeHookFireCapture, captureCount)
	sweepers := make([]*claudeHookFireCapture, captureCount)

	var group sync.WaitGroup
	for i := range active {
		group.Add(1)
		go func() {
			defer group.Done()
			active[i] = d.startClaudeHookFireCapture("runtime-1", fmt.Sprintf("active-%d", i), envRoot)
		}()
	}
	group.Wait()
	for i, capture := range active {
		if capture == nil {
			t.Fatalf("active capture %d is nil", i)
		}
		if err := os.WriteFile(capture.debugPath, []byte("active"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	for i := range sweepers {
		group.Add(1)
		go func() {
			defer group.Done()
			sweepers[i] = d.startClaudeHookFireCapture("runtime-1", fmt.Sprintf("sweeper-%d", i), envRoot)
		}()
	}
	group.Wait()
	for i, capture := range active {
		if _, err := os.Stat(capture.debugPath); err != nil {
			t.Errorf("active capture %d removed by concurrent janitor: %v", i, err)
		}
	}

	for i := range active {
		group.Add(2)
		go func() {
			defer group.Done()
			d.finishClaudeHookFireCapture(active[i], "runtime-1", fmt.Sprintf("active-%d", i))
		}()
		go func() {
			defer group.Done()
			d.finishClaudeHookFireCapture(sweepers[i], "runtime-1", fmt.Sprintf("sweeper-%d", i))
		}()
	}
	group.Wait()
	d.hookFireCaptureMu.Lock()
	defer d.hookFireCaptureMu.Unlock()
	if got := len(d.activeHookFireCaptures); got != 0 {
		t.Fatalf("active capture registry contains %d entries after finish, want 0", got)
	}
}

func TestTruncateHookFireDetailPreservesUTF8(t *testing.T) {
	prefix := strings.Repeat("a", maxHookFireDetailBytes-1)
	got := truncateHookFireDetail(prefix + "—trailing")
	if !utf8.ValidString(got) {
		t.Fatalf("truncated detail is invalid UTF-8: %q", got)
	}
	if got != prefix {
		t.Fatalf("truncated detail length/content = %d/%q, want %d ASCII bytes", len(got), got[len(got)-8:], len(prefix))
	}
}

func TestHookFireOutcomeDoesNotCallUnknownErrorFailure(t *testing.T) {
	tests := []struct {
		name     string
		response agent.ClaudeHookResponse
		want     string
	}{
		{
			name: "ambiguous shell command not found", response: agent.ClaudeHookResponse{Outcome: "error", ExitCode: intPointer(127)},
			want: "unknown",
		},
		{name: "ambiguous cannot execute", response: agent.ClaudeHookResponse{Outcome: "error", ExitCode: intPointer(126)}, want: "unknown"},
		{name: "missing exit status", response: agent.ClaudeHookResponse{Outcome: "error"}, want: "unknown"},
		{name: "executed failure", response: agent.ClaudeHookResponse{Outcome: "error", ExitCode: intPointer(1)}, want: "failure"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hookFireOutcome(test.response); got != test.want {
				t.Fatalf("outcome = %q, want %q", got, test.want)
			}
		})
	}
}

func TestHookFireOutcomeIgnoresShellMessageText(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "B1 executed hook saying failed to run",
			raw:  `{"type":"system","subtype":"hook_response","hook_id":"0d96f337-c43d-4b38-bdb0-980ef7a38a48","hook_name":"Stop","hook_event":"Stop","output":"Failed to run: migration step 3 rejected the input\n","stdout":"","stderr":"Failed to run: migration step 3 rejected the input\n","exit_code":1,"outcome":"error","uuid":"377c015b-c0fd-453f-a426-933832c8c7e4","session_id":"d5dda65b-6957-4851-a79e-8b1a9cef1e8f"}`,
			want: "failure",
		},
		{
			name: "B2 executed hook returning 127",
			raw:  `{"type":"system","subtype":"hook_response","hook_id":"52618e50-c9bd-4f7a-9279-ec493ef11cb6","hook_name":"Stop","hook_event":"Stop","output":"qc-config-lookup: not found\n","stdout":"","stderr":"qc-config-lookup: not found\n","exit_code":127,"outcome":"error","uuid":"57629a6b-75ad-4e25-bb11-7971c6264a9b","session_id":"d5dda65b-6957-4851-a79e-8b1a9cef1e8f"}`,
			want: "unknown",
		},
		{
			name: "B3 shell could not start hook",
			raw:  `{"type":"system","subtype":"hook_response","hook_id":"f690eb91-631b-4784-962f-c07453d89fc3","hook_name":"Stop","hook_event":"Stop","output":"/bin/sh: 1: /nonexistent/qc-binary-does-not-exist: not found\n","stdout":"","stderr":"/bin/sh: 1: /nonexistent/qc-binary-does-not-exist: not found\n","exit_code":127,"outcome":"error","uuid":"35c00342-7416-4465-9874-ffbb6b94dde5","session_id":"d5dda65b-6957-4851-a79e-8b1a9cef1e8f"}`,
			want: "unknown",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hookFireOutcome(capturedClaudeHookResponse(t, test.raw)); got != test.want {
				t.Fatalf("outcome = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFinishClaudeHookFireCaptureRemovesDebugFile(t *testing.T) {
	capture := &claudeHookFireCapture{debugPath: filepath.Join(t.TempDir(), "debug.log")}
	writeClaudeHookDebugLog(t, capture, "")
	d := &Daemon{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	d.finishClaudeHookFireCapture(capture, "runtime-1", "task-1")
	if _, err := os.Stat(capture.debugPath); !os.IsNotExist(err) {
		t.Fatalf("debug file still exists or stat failed unexpectedly: %v", err)
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
	a := hookFireID("runtime", "task", "stream-a")
	b := hookFireID("runtime", "task", "stream-a")
	c := hookFireID("runtime", "task", "stream-b")
	if a != b || a == c {
		t.Fatalf("ids a=%q b=%q c=%q", a, b, c)
	}
}
