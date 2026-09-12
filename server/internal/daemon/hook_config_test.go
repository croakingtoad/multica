package daemon

import (
	"context"
	"encoding/json"
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
	writeHookTestFile(t, filepath.Join(project, ".claude", "settings.local.json"), `{"hooks":{"PreToolUse":[]},"_disabledHooks":{"one":true}}`)

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
	if got := string(sources[2].DisabledHooks); got != `{"one":true}` {
		t.Fatalf("disabled hooks = %s", got)
	}
}

func TestReadRuntimeHookConfigCodexChecksJSONAndInlineTOMLAtBothLayers(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex-custom"))
	writeHookTestFile(t, filepath.Join(home, ".codex-custom", "hooks.json"), `{"hooks":{"Stop":[]}}`)
	writeHookTestFile(t, filepath.Join(home, ".codex-custom", "config.toml"), "[hooks]\nStop = []\n")
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
		var hooks map[string]any
		if err := json.Unmarshal(sources[i].Hooks, &hooks); err != nil || len(hooks) != 1 {
			t.Errorf("source %d hooks = %s, err = %v", i, sources[i].Hooks, err)
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
	for i := range wants {
		if paths[i].path != wants[i] {
			t.Errorf("path %d = %q, want %q", i, paths[i].path, wants[i])
		}
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
