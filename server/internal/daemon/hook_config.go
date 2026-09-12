package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/pelletier/go-toml/v2"
)

const maxHookConfigFileSize int64 = 8 << 20

type hookConfigPath struct {
	provider string
	scope    string
	format   string
	path     string
}

func (d *Daemon) handleHookRead(ctx context.Context, rt Runtime, requestID string) {
	d.logger.Info("runtime hooks requested", "runtime_id", rt.ID, "request_id", requestID, "provider", rt.Provider)

	home, err := os.UserHomeDir()
	if err != nil {
		d.reportHookReadResult(ctx, rt, requestID, protocol.HookConfigReadReport{Status: "failed", Error: fmt.Sprintf("resolve user home: %v", err)})
		return
	}
	projectRoot, err := os.Getwd()
	if err != nil {
		d.reportHookReadResult(ctx, rt, requestID, protocol.HookConfigReadReport{Status: "failed", Error: fmt.Sprintf("resolve project root: %v", err)})
		return
	}

	sources, err := readRuntimeHookConfig(rt.Provider, home, projectRoot)
	if err != nil {
		d.reportHookReadResult(ctx, rt, requestID, protocol.HookConfigReadReport{Status: "failed", Error: err.Error()})
		return
	}
	d.reportHookReadResult(ctx, rt, requestID, protocol.HookConfigReadReport{
		Status:     "completed",
		ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Sources:    sources,
	})
}

func readRuntimeHookConfig(provider, home, projectRoot string) ([]protocol.HookConfigSource, error) {
	paths, err := hookConfigPaths(provider, home, projectRoot)
	if err != nil {
		return nil, err
	}
	sources := make([]protocol.HookConfigSource, 0, len(paths))
	for _, candidate := range paths {
		source, err := readHookConfigSource(candidate)
		if err != nil {
			return nil, fmt.Errorf("read %s %s hooks: %w", candidate.provider, candidate.scope, err)
		}
		sources = append(sources, source)
	}
	return sources, nil
}

func hookConfigPaths(provider, home, projectRoot string) ([]hookConfigPath, error) {
	switch provider {
	case "claude":
		return []hookConfigPath{
			{provider: provider, scope: "user", format: "json", path: filepath.Join(home, ".claude", "settings.json")},
			{provider: provider, scope: "project", format: "json", path: filepath.Join(projectRoot, ".claude", "settings.json")},
			{provider: provider, scope: "local", format: "json", path: filepath.Join(projectRoot, ".claude", "settings.local.json")},
		}, nil
	case "codex":
		codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
		if codexHome == "" {
			codexHome = filepath.Join(home, ".codex")
		}
		return []hookConfigPath{
			{provider: provider, scope: "user", format: "json", path: filepath.Join(codexHome, "hooks.json")},
			{provider: provider, scope: "user", format: "toml", path: filepath.Join(codexHome, "config.toml")},
			{provider: provider, scope: "project", format: "json", path: filepath.Join(projectRoot, ".codex", "hooks.json")},
			{provider: provider, scope: "project", format: "toml", path: filepath.Join(projectRoot, ".codex", "config.toml")},
		}, nil
	default:
		return nil, fmt.Errorf("provider %q does not expose lifecycle hooks", provider)
	}
}

func readHookConfigSource(candidate hookConfigPath) (protocol.HookConfigSource, error) {
	empty := json.RawMessage(`{}`)
	source := protocol.HookConfigSource{
		Provider: candidate.provider, Scope: candidate.scope, Format: candidate.format,
		Hooks: empty, DisabledHooks: empty,
	}
	info, err := os.Stat(candidate.path)
	if os.IsNotExist(err) {
		return source, nil
	}
	if err != nil {
		return source, err
	}
	if !info.Mode().IsRegular() {
		return source, fmt.Errorf("source is not a regular file")
	}
	if info.Size() > maxHookConfigFileSize {
		return source, fmt.Errorf("source exceeds %d bytes", maxHookConfigFileSize)
	}
	raw, err := os.ReadFile(candidate.path)
	if err != nil {
		return source, err
	}
	hooks, disabled, err := extractHookConfig(raw, candidate.format)
	if err != nil {
		return source, err
	}
	hash := sha256.Sum256(raw)
	path := candidate.path
	hashText := hex.EncodeToString(hash[:])
	source.SourcePath = &path
	source.ContentHash = &hashText
	source.Hooks = hooks
	source.DisabledHooks = disabled
	return source, nil
}

func extractHookConfig(raw []byte, format string) (json.RawMessage, json.RawMessage, error) {
	var document map[string]any
	switch format {
	case "json":
		if err := json.Unmarshal(raw, &document); err != nil {
			return nil, nil, fmt.Errorf("decode JSON: %w", err)
		}
	case "toml":
		if err := toml.Unmarshal(raw, &document); err != nil {
			return nil, nil, fmt.Errorf("decode TOML: %w", err)
		}
	default:
		return nil, nil, fmt.Errorf("unsupported format %q", format)
	}
	return marshalHookField(document, "hooks"), marshalHookField(document, "_disabledHooks"), nil
}

func marshalHookField(document map[string]any, key string) json.RawMessage {
	value, ok := document[key]
	if !ok {
		return json.RawMessage(`{}`)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func (d *Daemon) reportHookReadResult(ctx context.Context, rt Runtime, requestID string, payload protocol.HookConfigReadReport) {
	d.reportRuntimeResultWithRetry(ctx, "hook_read", rt.ID, requestID, func(ctx context.Context) error {
		return d.client.ReportHookReadResult(ctx, rt.ID, requestID, payload)
	})
}
