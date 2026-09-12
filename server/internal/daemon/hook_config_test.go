package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func writeHookTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadRuntimeHookConfigClaudeChecksAllScopes(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	writeHookTestFile(t, filepath.Join(home, ".claude", "settings.json"), `{"hooks":{"Stop":[{"hooks":[]}]}}`)
	writeHookTestFile(t, filepath.Join(project, ".claude", "settings.local.json"), `{"hooks":{"PreToolUse":[]},"_disabledHooks":{"hook-id-1":{"event":"PreToolUse","matcher":"Bash","handler":{"type":"command","command":"./check.sh"},"parked_at":"2026-09-12T08:00:00Z"}}}`)

	sources, err := readRuntimeHookConfig("claude", home, project)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 3 {
		t.Fatalf("sources = %d, want user/project/local", len(sources))
	}
	for i, wantScope := range []string{"user", "project", "local"} {
		if sources[i].Scope != wantScope {
			t.Errorf("source %d scope = %q, want %q", i, sources[i].Scope, wantScope)
		}
	}
	if sources[0].SourcePath == nil || sources[0].ContentHash == nil {
		t.Fatal("present user source lacks source identity")
	}
	if sources[1].SourcePath != nil || sources[1].ContentHash != nil {
		t.Fatal("checked-absent project source must have nil source identity")
	}
	if got := string(sources[2].DisabledHooks); got != `{"hook-id-1":{"event":"PreToolUse","handler":{"command":"./check.sh","type":"command"},"matcher":"Bash","parked_at":"2026-09-12T08:00:00Z"}}` {
		t.Fatalf("disabled hooks = %s", got)
	}
}

func TestExtractHookConfigClaudePayloadIsUnchanged(t *testing.T) {
	raw := []byte(`{"hooks":{"PreToolUse":[]},"_disabledHooks":{"hook-id-1":{"event":"PreToolUse","matcher":"Bash","handler":{"type":"command","command":"./check.sh"},"parked_at":"2026-09-12T08:00:00Z"}}}`)

	hooks, disabled, err := extractHookConfig(raw, "claude", "json")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(hooks), `{"PreToolUse":[]}`; got != want {
		t.Fatalf("hooks = %s, want %s", got, want)
	}
	if got, want := string(disabled), `{"hook-id-1":{"event":"PreToolUse","handler":{"command":"./check.sh","type":"command"},"matcher":"Bash","parked_at":"2026-09-12T08:00:00Z"}}`; got != want {
		t.Fatalf("disabled hooks = %s, want %s", got, want)
	}
}

func TestHandleHookReadCWDAtHomeDoesNotDuplicateUserSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Chdir(home)
	writeHookTestFile(t, filepath.Join(home, ".claude", "settings.json"), `{"hooks":{"Stop":[]}}`)

	reports := make(chan protocol.HookConfigReadReport, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var report protocol.HookConfigReadReport
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			t.Errorf("decode hook report: %v", err)
		}
		reports <- report
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client: NewClient(srv.URL),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	d.handleHookRead(context.Background(), Runtime{ID: "runtime-1", Provider: "claude"}, "request-1")
	report := <-reports
	if len(report.Sources) != 1 {
		t.Fatalf("sources = %d, want only the user source when project root is unknown", len(report.Sources))
	}
	if report.Sources[0].Scope != "user" {
		t.Fatalf("source scope = %q, want user", report.Sources[0].Scope)
	}
}

func TestReadRuntimeHookConfigCodexChecksJSONAndInlineTOMLAtBothLayers(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex-custom"))
	writeHookTestFile(t, filepath.Join(home, ".codex-custom", "hooks.json"), `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"if [ -f '/home/marty/.orca/agent-hooks/codex-hook.sh' ] && [ -r '/home/marty/.orca/agent-hooks/codex-hook.sh' ] && [ -x '/home/marty/.orca/agent-hooks/codex-hook.sh' ]; then /bin/sh '/home/marty/.orca/agent-hooks/codex-hook.sh'; else { command -p cat 2>/dev/null || cat; } >/dev/null 2>&1 || :; fi","timeout":10}]}]}}`)
	writeHookTestFile(t, filepath.Join(home, ".codex-custom", "config.toml"), `[hooks.state]

[hooks.state."/home/marty/.codex/hooks.json:pre_tool_use:0:0"]
trusted_hash = "sha256:adae0c83d982bb4230f29f0a038d225be681dae9467dc684b1f4ed220093a07b"
`)
	writeHookTestFile(t, filepath.Join(project, ".codex", "hooks.json"), `{"hooks":{"PreToolUse":[]}}`)
	writeHookTestFile(t, filepath.Join(project, ".codex", "config.toml"), "[hooks]\nPostToolUse = []\n")

	sources, err := readRuntimeHookConfig("codex", home, project)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 4 {
		t.Fatalf("sources = %d, want four Codex layer/format combinations", len(sources))
	}
	wants := []struct{ scope, format string }{{"user", "json"}, {"user", "toml"}, {"project", "json"}, {"project", "toml"}}
	for i, want := range wants {
		if sources[i].Scope != want.scope || sources[i].Format != want.format {
			t.Errorf("source %d = %s/%s, want %s/%s", i, sources[i].Scope, sources[i].Format, want.scope, want.format)
		}
		if sources[i].SourcePath == nil || sources[i].ContentHash == nil {
			t.Errorf("source %d lacks source identity", i)
		}
	}
	var codexHooks struct {
		PreToolUse []struct {
			Hooks []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"PreToolUse"`
	}
	if err := json.Unmarshal(sources[0].Hooks, &codexHooks); err != nil {
		t.Fatalf("decode Codex hooks.json hooks: %v", err)
	}
	if len(codexHooks.PreToolUse) != 1 || len(codexHooks.PreToolUse[0].Hooks) != 1 {
		t.Fatalf("Codex hooks.json hooks = %s, want the reviewed PreToolUse handler", sources[0].Hooks)
	}
	handler := codexHooks.PreToolUse[0].Hooks[0]
	if wantCommand := "if [ -f '/home/marty/.orca/agent-hooks/codex-hook.sh' ] && [ -r '/home/marty/.orca/agent-hooks/codex-hook.sh' ] && [ -x '/home/marty/.orca/agent-hooks/codex-hook.sh' ]; then /bin/sh '/home/marty/.orca/agent-hooks/codex-hook.sh'; else { command -p cat 2>/dev/null || cat; } >/dev/null 2>&1 || :; fi"; handler.Type != "command" || handler.Command != wantCommand || handler.Timeout != 10 {
		t.Fatalf("Codex hooks.json handler = %#v, want reviewed command fixture", handler)
	}
	if got, want := string(sources[1].Hooks), `{}`; got != want {
		t.Fatalf("Codex config.toml hooks = %s, want event keys only: %s", got, want)
	}
	if got, want := string(sources[1].DisabledHooks), `{"state":{"/home/marty/.codex/hooks.json:pre_tool_use:0:0":{"trusted_hash":"sha256:adae0c83d982bb4230f29f0a038d225be681dae9467dc684b1f4ed220093a07b"}}}`; got != want {
		t.Fatalf("Codex config.toml disabled hooks = %s, want lossless state envelope %s", got, want)
	}
	for _, i := range []int{0, 2, 3} {
		if got, want := string(sources[i].DisabledHooks), `{}`; got != want {
			t.Errorf("Codex source %d disabled hooks = %s, want %s without state", i, got, want)
		}
	}
	for i, wantEvent := range map[int]string{0: "PreToolUse", 2: "PreToolUse", 3: "PostToolUse"} {
		var hooks map[string]any
		if err := json.Unmarshal(sources[i].Hooks, &hooks); err != nil || len(hooks) != 1 || hooks[wantEvent] == nil {
			t.Errorf("source %d hooks = %s, want only %s, err = %v", i, sources[i].Hooks, wantEvent, err)
		}
	}
}

func TestHookConfigPathsAreProviderOwnedAndCarryNoRequestedTarget(t *testing.T) {
	home := filepath.Join("fixed", "home")
	project := filepath.Join("fixed", "project")
	paths, err := hookConfigPaths("claude", home, project)
	if err != nil {
		t.Fatal(err)
	}
	wants := []string{
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(project, ".claude", "settings.json"),
		filepath.Join(project, ".claude", "settings.local.json"),
	}
	if len(paths) != len(wants) {
		t.Fatalf("paths = %d, want %d", len(paths), len(wants))
	}
	for i := range wants {
		if paths[i].path != wants[i] {
			t.Errorf("path %d = %q, want %q", i, paths[i].path, wants[i])
		}
	}
}

func TestHookConfigPathsRejectsRelativeCodexHome(t *testing.T) {
	t.Setenv("CODEX_HOME", filepath.Join("relative", "codex-home"))

	if _, err := hookConfigPaths("codex", t.TempDir(), ""); err == nil {
		t.Fatal("expected relative CODEX_HOME to be rejected")
	}
}

func TestExtractHookConfigRejectsTOMLValuesThatCannotEncodeAsJSON(t *testing.T) {
	if _, _, err := extractHookConfig([]byte("[hooks]\nStop = nan\n"), "codex", "toml"); err == nil {
		t.Fatal("expected TOML nan hook value to fail JSON encoding")
	}
}

func TestReadRuntimeHookConfigRejectsUnsupportedProvider(t *testing.T) {
	if _, err := readRuntimeHookConfig("other", t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func TestReportHookReadResultUsesRuntimeResultEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL)
	err := client.ReportHookReadResult(context.Background(), "runtime-1", "request-1", protocol.HookConfigReadReport{Status: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "/api/daemon/runtimes/runtime-1/hooks/request-1/result"; gotPath != want {
		t.Fatalf("path = %q, want %q", gotPath, want)
	}
}

func TestHeartbeatHookReadRemainsAdditiveAcrossVersionSkew(t *testing.T) {
	var response HeartbeatResponse
	if err := json.Unmarshal([]byte(`{"status":"ok","pending_hook_read":{"id":"request-1","future_action_field":true},"future_ack_field":true}`), &response); err != nil {
		t.Fatalf("new daemon must ignore future heartbeat fields: %v", err)
	}
	if response.PendingHookRead == nil || response.PendingHookRead.ID != "request-1" {
		t.Fatalf("pending hook read = %#v", response.PendingHookRead)
	}

	var olderShape HeartbeatResponse
	if err := json.Unmarshal([]byte(`{"status":"ok"}`), &olderShape); err != nil {
		t.Fatalf("missing additive hook field must remain valid: %v", err)
	}
	if olderShape.PendingHookRead != nil {
		t.Fatalf("missing hook action decoded as %#v", olderShape.PendingHookRead)
	}
}
