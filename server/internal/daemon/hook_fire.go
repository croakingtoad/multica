package daemon

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/runtimehooks"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	maxHookFireDetailBytes  = 2048
	hookFireReportBatchSize = 250
)

var (
	claudeDebugLinePattern = regexp.MustCompile(`^(\S+) \[[A-Z]+\] (.*)$`)
	claudeHookFirePattern  = regexp.MustCompile(`(?s)^Hook (.+) \(([A-Za-z][A-Za-z0-9]*)\) (success|error|cancelled):(?:\n(.*))?$`)
)

type claudeHookFireCapture struct {
	debugPath       string
	home            string
	projectRoot     string
	resolved        runtimehooks.Resolution
	runnable        map[string]bool
	sourceSnapshots map[runtimehooks.SourceRef]string
	sourceUnchanged map[runtimehooks.SourceRef]bool
}

func hookConfigSourceSnapshot(source protocol.HookConfigSource) string {
	if source.SourcePath == nil || source.ContentHash == nil {
		return "absent"
	}
	info, err := os.Stat(*source.SourcePath)
	if err != nil {
		return "unreadable"
	}
	return *source.ContentHash + ":" + strconv.FormatInt(info.ModTime().UnixNano(), 10) + ":" + strconv.FormatInt(info.Size(), 10)
}

func prepareClaudeHookFireCapture(home, projectRoot, debugPath string) (*claudeHookFireCapture, error) {
	sources, err := readRuntimeHookConfig("claude", home, projectRoot)
	if err != nil {
		return nil, fmt.Errorf("read Claude hook identity snapshot: %w", err)
	}
	expected := make([]runtimehooks.SourceRef, 0, len(sources))
	observed := make([]runtimehooks.ObservedSource, 0, len(sources))
	sourceSnapshots := make(map[runtimehooks.SourceRef]string, len(sources))
	for _, source := range sources {
		ref := runtimehooks.SourceRef{Scope: source.Scope, Format: source.Format, Kind: runtimehooks.SourceSettings}
		expected = append(expected, ref)
		sourceSnapshots[ref] = hookConfigSourceSnapshot(source)
		observed = append(observed, runtimehooks.ObservedSource{
			Source: ref, SourcePath: source.SourcePath, ContentHash: source.ContentHash,
			Hooks: source.Hooks, DisabledHooks: source.DisabledHooks,
		})
	}
	resolved, err := runtimehooks.Resolve(runtimehooks.ProviderClaude, expected, observed)
	if err != nil {
		return nil, fmt.Errorf("resolve Claude hook identity snapshot: %w", err)
	}
	states, err := resolved.RunStates()
	if err != nil {
		return nil, fmt.Errorf("classify Claude hook identity snapshot: %w", err)
	}
	runnable := make(map[string]bool, len(states))
	for _, state := range states {
		runnable[state.Hook.HookID] = state.Configuration == runtimehooks.ConfigurationLive &&
			state.Effectiveness == runtimehooks.EffectivenessWillRun
	}
	return &claudeHookFireCapture{
		debugPath: debugPath, home: home, projectRoot: projectRoot, resolved: resolved,
		runnable: runnable, sourceSnapshots: sourceSnapshots,
	}, nil
}

type claudeDebugFire struct {
	firedAt      time.Time
	hookName     string
	event        string
	debugOutcome string
	firstLine    string
}

func parseClaudeDebugFire(line, followingLine string) (claudeDebugFire, bool) {
	logParts := claudeDebugLinePattern.FindStringSubmatch(line)
	if logParts == nil {
		return claudeDebugFire{}, false
	}
	firedAt, err := time.Parse(time.RFC3339Nano, logParts[1])
	if err != nil {
		return claudeDebugFire{}, false
	}
	message := logParts[2]
	if strings.HasPrefix(message, `"`) {
		decoded, err := strconv.Unquote(message)
		if err != nil {
			return claudeDebugFire{}, false
		}
		message = decoded
	}
	parts := claudeHookFirePattern.FindStringSubmatch(message)
	if parts == nil {
		return claudeDebugFire{}, false
	}
	output := parts[4]
	if output == "" {
		output = followingLine
	}
	firstLine, _, _ := strings.Cut(output, "\n")
	return claudeDebugFire{
		firedAt: firedAt, hookName: parts[1], event: parts[2],
		debugOutcome: parts[3], firstLine: strings.TrimSpace(firstLine),
	}, true
}

func (c *claudeHookFireCapture) observedHook(record claudeDebugFire) (runtimehooks.ResolvedHook, bool) {
	value := ""
	if record.hookName != record.event {
		prefix := record.event + ":"
		if !strings.HasPrefix(record.hookName, prefix) {
			return runtimehooks.ResolvedHook{}, false
		}
		value = strings.TrimPrefix(record.hookName, prefix)
	}
	partition, err := c.resolved.MatchEvent(record.event, value)
	if err != nil {
		return runtimehooks.ResolvedHook{}, false
	}
	merged := c.resolved.MergeMatched(partition.Matched)
	candidates := merged[:0]
	for _, candidate := range merged {
		unchanged := true
		for _, source := range candidate.Sources {
			if !c.sourceUnchanged[source] {
				unchanged = false
				break
			}
		}
		if c.runnable[candidate.HookID] && unchanged {
			candidates = append(candidates, candidate)
		}
	}
	// Claude's debug record carries no content identity. Persist only when its
	// event/value resolves to exactly one launch-time definition; choosing among
	// multiple candidates would fabricate which handler produced the record.
	if len(candidates) != 1 {
		return runtimehooks.ResolvedHook{}, false
	}
	return candidates[0], true
}

func hookFireOutcome(record claudeDebugFire) string {
	switch record.debugOutcome {
	case "success":
		return "success"
	case "error":
		if strings.HasPrefix(record.firstLine, "Failed to run:") ||
			strings.HasPrefix(record.firstLine, "Error occurred while executing hook command:") {
			return "skipped"
		}
		return "failure"
	default:
		return "unknown"
	}
}

func hookFireID(runtimeID, taskID string, firedAt time.Time, event, outcome, firstLine string, ordinal int) string {
	hash := sha256.New()
	for _, value := range []string{runtimeID, taskID, firedAt.Format(time.RFC3339Nano), event, outcome, firstLine, strconv.Itoa(ordinal)} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	sum := hash.Sum(nil)[:16]
	// Encode an RFC 4122 variant, version-5-shaped deterministic UUID. The
	// namespace material above is Multica-specific rather than RFC SHA-1.
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(sum)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}

func truncateHookFireDetail(value string) string {
	if len(value) <= maxHookFireDetailBytes {
		return value
	}
	return value[:maxHookFireDetailBytes]
}

func (c *claudeHookFireCapture) read(runtimeID, taskID string) ([]protocol.HookFire, error) {
	currentSources, err := readRuntimeHookConfig("claude", c.home, c.projectRoot)
	if err != nil {
		return nil, fmt.Errorf("re-read Claude hook identity snapshot: %w", err)
	}
	c.sourceUnchanged = make(map[runtimehooks.SourceRef]bool, len(currentSources))
	for _, source := range currentSources {
		ref := runtimehooks.SourceRef{Scope: source.Scope, Format: source.Format, Kind: runtimehooks.SourceSettings}
		c.sourceUnchanged[ref] = c.sourceSnapshots[ref] == hookConfigSourceSnapshot(source)
	}

	file, err := os.Open(c.debugPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open Claude hook debug log: %w", err)
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read Claude hook debug log: %w", err)
	}

	ordinals := make(map[string]int)
	fires := make([]protocol.HookFire, 0)
	for i, line := range lines {
		following := ""
		if i+1 < len(lines) && claudeDebugLinePattern.FindStringSubmatch(lines[i+1]) == nil {
			following = lines[i+1]
		}
		record, ok := parseClaudeDebugFire(line, following)
		if !ok {
			continue
		}
		hook, ok := c.observedHook(record)
		if !ok {
			continue
		}
		outcome := hookFireOutcome(record)
		detail, err := json.Marshal(map[string]string{
			"debug_outcome": record.debugOutcome,
			"message":       truncateHookFireDetail(record.firstLine),
		})
		if err != nil {
			return nil, fmt.Errorf("encode Claude hook fire detail: %w", err)
		}
		key := record.firedAt.Format(time.RFC3339Nano) + "\x00" + record.event + "\x00" + outcome + "\x00" + record.firstLine
		ordinal := ordinals[key]
		ordinals[key]++
		fires = append(fires, protocol.HookFire{
			ID:    hookFireID(runtimeID, taskID, record.firedAt, record.event, outcome, record.firstLine, ordinal),
			Event: record.event, HookID: hook.HookID, HookSpec: append(json.RawMessage(nil), hook.Handler...),
			FiredAt: record.firedAt.Format(time.RFC3339Nano), Outcome: outcome, Detail: detail,
		})
	}
	return fires, nil
}

func (d *Daemon) startClaudeHookFireCapture(runtimeID, taskID, home, projectRoot, envRoot string) *claudeHookFireCapture {
	debugDir := filepath.Join(envRoot, ".multica", "claude-hook-debug")
	if err := os.MkdirAll(debugDir, 0o700); err != nil {
		d.logger.Warn("Claude hook fire capture directory unavailable", "runtime_id", runtimeID, "task_id", taskID, "error", err)
		return nil
	}
	debugPath := filepath.Join(debugDir, fmt.Sprintf("%s-%d.log", taskID, time.Now().UnixNano()))
	capture, err := prepareClaudeHookFireCapture(home, projectRoot, debugPath)
	if err != nil {
		d.logger.Warn("Claude hook fire identity snapshot unavailable", "runtime_id", runtimeID, "task_id", taskID, "error", err)
		return nil
	}
	return capture
}

func (d *Daemon) finishClaudeHookFireCapture(capture *claudeHookFireCapture, runtimeID, taskID string) {
	if capture == nil {
		return
	}
	fires, err := capture.read(runtimeID, taskID)
	if err != nil {
		d.logger.Warn("Claude hook fire debug log unreadable", "runtime_id", runtimeID, "task_id", taskID, "error", err)
		return
	}
	if len(fires) == 0 {
		return
	}
	ctx := d.rootCtx
	if ctx == nil {
		ctx = context.Background()
	}
	reportCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for start := 0; start < len(fires); start += hookFireReportBatchSize {
		end := min(start+hookFireReportBatchSize, len(fires))
		batch := fires[start:end]
		d.reportRuntimeResultWithRetry(reportCtx, "hook_fires", runtimeID, taskID, func(ctx context.Context) error {
			return d.client.ReportHookFires(ctx, runtimeID, protocol.HookFireReport{Fires: batch})
		})
		if reportCtx.Err() != nil {
			return
		}
	}
}
