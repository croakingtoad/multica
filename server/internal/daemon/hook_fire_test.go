package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestClaudeHookFireCaptureReadsRealDebugRecords(t *testing.T) {
	home := t.TempDir()
	projectRoot := t.TempDir()
	settingsDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(settingsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	settings := `{"hooks":{"PostToolUse":[{"matcher":"Write","hooks":[{"type":"command","command":"check-write"}]}],"Stop":[{"hooks":[{"type":"command","command":"finish"}]}],"SessionStart":[{"hooks":[{"type":"command","command":"missing-command"}]}]}}`
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}

	capture, err := prepareClaudeHookFireCapture(home, projectRoot, filepath.Join(t.TempDir(), "debug.log"))
	if err != nil {
		t.Fatal(err)
	}
	debugLog := `2026-09-12T12:00:00.123Z [DEBUG] "Hook PostToolUse:Write (PostToolUse) success:\nhook-ran"
2026-09-12T12:00:01.123Z [DEBUG] "Hook Stop (Stop) error:\nvalidation failed"
2026-09-12T12:00:02.123Z [DEBUG] "Hook SessionStart (SessionStart) error:\nFailed to run: executable file not found"
`
	if err := os.WriteFile(capture.debugPath, []byte(debugLog), 0o600); err != nil {
		t.Fatal(err)
	}

	fires, err := capture.read("runtime-1", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(fires) != 3 {
		t.Fatalf("fires = %#v, want three observed records", fires)
	}
	wantOutcomes := []string{"success", "failure", "skipped"}
	for i, fire := range fires {
		if fire.Outcome != wantOutcomes[i] {
			t.Errorf("fire %d outcome = %q, want %q", i, fire.Outcome, wantOutcomes[i])
		}
		if fire.HookID == "" || len(fire.HookSpec) == 0 || fire.FiredAt == "" || fire.ID == "" {
			t.Errorf("fire %d omitted observed identity or source timestamp: %#v", i, fire)
		}
	}
	if got := string(fires[0].HookSpec); got != `{"command":"check-write","type":"command"}` {
		t.Fatalf("hook spec = %s", got)
	}
}

func TestClaudeHookFireCaptureSkipsAmbiguousIdentity(t *testing.T) {
	home := t.TempDir()
	settingsDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(settingsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	settings := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"one"},{"type":"command","command":"two"}]}]}}`
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	capture, err := prepareClaudeHookFireCapture(home, t.TempDir(), filepath.Join(t.TempDir(), "debug.log"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(capture.debugPath, []byte("2026-09-12T12:00:00Z [DEBUG] Hook Stop (Stop) success:\nran\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fires, err := capture.read("runtime-1", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(fires) != 0 {
		t.Fatalf("ambiguous debug record fabricated identities: %#v", fires)
	}
}

func TestClaudeHookFireCaptureSkipsIdentityChangedDuringRun(t *testing.T) {
	home := t.TempDir()
	settingsDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(settingsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"before"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	capture, err := prepareClaudeHookFireCapture(home, t.TempDir(), filepath.Join(t.TempDir(), "debug.log"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"after"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(capture.debugPath, []byte("2026-09-12T12:00:00Z [DEBUG] Hook Stop (Stop) success:\nran\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fires, err := capture.read("runtime-1", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(fires) != 0 {
		t.Fatalf("changed launch identity was attributed as a real fire: %#v", fires)
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
	a := hookFireID("runtime", "task", firedAt, "Stop", "success", "ok", 0)
	b := hookFireID("runtime", "task", firedAt, "Stop", "success", "ok", 0)
	c := hookFireID("runtime", "task", firedAt, "Stop", "success", "ok", 1)
	if a != b || a == c {
		t.Fatalf("ids a=%q b=%q c=%q", a, b, c)
	}
}
