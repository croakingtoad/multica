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
	"sync"
	"time"
	"unicode"

	"github.com/multica-ai/multica/server/pkg/agent"
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

// claudeHookFireCapture records every valid structured hook_response. A
// provably unique debug-log match can upgrade the response's timestamp,
// provenance, and message, but it never supplies row identity.
type claudeHookFireCapture struct {
	debugPath string

	mu        sync.Mutex
	responses []observedClaudeHookResponse
}

type observedClaudeHookResponse struct {
	response   agent.ClaudeHookResponse
	receivedAt time.Time
}

func (c *claudeHookFireCapture) observe(response agent.ClaudeHookResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	receivedAt := time.Now()
	if response.ExitCode != nil {
		exitCode := *response.ExitCode
		response.ExitCode = &exitCode
	}
	c.responses = append(c.responses, observedClaudeHookResponse{response: response, receivedAt: receivedAt})
}

func (c *claudeHookFireCapture) responseSnapshot() []observedClaudeHookResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]observedClaudeHookResponse(nil), c.responses...)
}

type claudeDebugFire struct {
	firedAt      time.Time
	hookName     string
	event        string
	debugOutcome string
	output       string
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
		debugOutcome: parts[3], output: output, firstLine: strings.TrimSpace(firstLine),
	}, true
}

func claudeResponseDebugOutput(response agent.ClaudeHookResponse) string {
	// This is Claude 2.1.269's exact debug logger selection: stdout || stderr
	// || output. Matching that value joins the structured response to the log
	// entry without guessing from event or completion order.
	if response.Stdout != "" {
		return response.Stdout
	}
	if response.Stderr != "" {
		return response.Stderr
	}
	return response.Output
}

func claudeFireSignature(hookName, event, outcome, output string) (string, bool) {
	// Claude's debug logger applies JavaScript trimEnd() to the selected output.
	// Mirror its Unicode-whitespace behavior, but never treat an empty result as
	// evidence: an empty bucket has no output bytes with which to prove a join.
	output = strings.TrimRightFunc(output, unicode.IsSpace)
	if output == "" {
		return "", false
	}
	return hookName + "\x00" + event + "\x00" + outcome + "\x00" + output, true
}

func hookFireOutcome(response agent.ClaudeHookResponse) string {
	switch response.Outcome {
	case "success":
		return "success"
	case "error":
		if response.ExitCode == nil {
			return "unknown"
		}
		// Claude 2.1.269's hook_response has no distinct spawn-failure field.
		// Exit 126/127 can mean either that the shell could not start the hook or
		// that a hook which did run returned the same conventional status. Without
		// structured execution evidence, neither "skipped" nor "failure" is true.
		if *response.ExitCode == 126 || *response.ExitCode == 127 {
			return "unknown"
		}
		return "failure"
	default:
		return "unknown"
	}
}

func hookFireID(runtimeID, taskID, executionID string) string {
	hash := sha256.New()
	for _, value := range []string{runtimeID, taskID, executionID} {
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

func readClaudeDebugFires(path string) ([]claudeDebugFire, error) {
	file, err := os.Open(path)
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

	records := make([]claudeDebugFire, 0)
	for i, line := range lines {
		following := ""
		if i+1 < len(lines) && claudeDebugLinePattern.FindStringSubmatch(lines[i+1]) == nil {
			following = lines[i+1]
		}
		if record, ok := parseClaudeDebugFire(line, following); ok {
			records = append(records, record)
		}
	}
	return records, nil
}

func (c *claudeHookFireCapture) read(runtimeID, taskID string) ([]protocol.HookFire, error) {
	debugRecords, err := readClaudeDebugFires(c.debugPath)
	if err != nil {
		return nil, err
	}
	bySignature := make(map[string][]claudeDebugFire, len(debugRecords))
	for _, record := range debugRecords {
		key, matchable := claudeFireSignature(record.hookName, record.event, record.debugOutcome, record.output)
		if !matchable {
			continue
		}
		bySignature[key] = append(bySignature[key], record)
	}

	responses := c.responseSnapshot()
	responseCounts := make(map[string]int, len(responses))
	for _, observed := range responses {
		response := observed.response
		if response.HookID == "" || response.HookName == "" || response.HookEvent == "" {
			continue
		}
		if key, matchable := claudeFireSignature(response.HookName, response.HookEvent, response.Outcome, claudeResponseDebugOutput(response)); matchable {
			responseCounts[key]++
		}
	}

	fires := make([]protocol.HookFire, 0, len(responses))
	for _, observed := range responses {
		response := observed.response
		if response.HookID == "" || response.HookName == "" || response.HookEvent == "" {
			continue
		}
		// hook_response deliberately does not expose the configured handler.
		// Persist the execution identity it does expose and an honest observed
		// spec marker; binding this record to any current settings handler would
		// fabricate identity when plugins, skills, or managed settings also ran.
		hookSpec, err := json.Marshal(map[string]string{
			"hook_name": response.HookName,
			"type":      "claude_hook_response",
		})
		if err != nil {
			return nil, fmt.Errorf("encode Claude hook fire spec: %w", err)
		}
		outcome := hookFireOutcome(response)
		detailValue := map[string]any{
			"debug_outcome": response.Outcome,
		}
		firedAt := observed.receivedAt
		provenance := "inferred"
		key, matchable := claudeFireSignature(response.HookName, response.HookEvent, response.Outcome, claudeResponseDebugOutput(response))
		if matchable && responseCounts[key] == 1 && len(bySignature[key]) == 1 {
			record := bySignature[key][0]
			firedAt = record.firedAt
			provenance = "debug_log"
			detailValue["message"] = truncateHookFireDetail(record.firstLine)
		}
		if response.ExitCode != nil {
			detailValue["exit_code"] = *response.ExitCode
		}
		detail, err := json.Marshal(detailValue)
		if err != nil {
			return nil, fmt.Errorf("encode Claude hook fire detail: %w", err)
		}
		fires = append(fires, protocol.HookFire{
			ID: hookFireID(runtimeID, taskID, response.HookID), Event: response.HookEvent,
			ExecutionID: response.HookID, HookSpec: hookSpec,
			FiredAt: firedAt.Format(time.RFC3339Nano), Provenance: provenance,
			Outcome: outcome, Detail: detail,
		})
	}
	return fires, nil
}

func (d *Daemon) startClaudeHookFireCapture(runtimeID, taskID, envRoot string) *claudeHookFireCapture {
	debugDir := filepath.Join(envRoot, ".multica", "claude-hook-debug")
	if err := os.MkdirAll(debugDir, 0o700); err != nil {
		d.logger.Warn("Claude hook fire capture directory unavailable", "runtime_id", runtimeID, "task_id", taskID, "error", err)
		return nil
	}
	debugPath := filepath.Join(debugDir, fmt.Sprintf("%s-%d.log", taskID, time.Now().UnixNano()))
	d.hookFireCaptureMu.Lock()
	defer d.hookFireCaptureMu.Unlock()
	entries, err := os.ReadDir(debugDir)
	if err != nil {
		d.logger.Warn("Claude hook fire capture directory unreadable", "runtime_id", runtimeID, "task_id", taskID, "error", err)
		return nil
	}
	for _, entry := range entries {
		path := filepath.Join(debugDir, entry.Name())
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".log" {
			continue
		}
		if _, active := d.activeHookFireCaptures[path]; active {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			d.logger.Warn("orphaned Claude hook fire debug log cleanup failed", "path", path, "error", err)
		}
	}
	if d.activeHookFireCaptures == nil {
		d.activeHookFireCaptures = make(map[string]struct{})
	}
	d.activeHookFireCaptures[debugPath] = struct{}{}
	return &claudeHookFireCapture{debugPath: debugPath}
}

func (d *Daemon) finishClaudeHookFireCapture(capture *claudeHookFireCapture, runtimeID, taskID string) {
	if capture == nil {
		return
	}
	defer func() {
		if err := os.Remove(capture.debugPath); err != nil && !os.IsNotExist(err) {
			d.logger.Warn("Claude hook fire debug log cleanup failed", "runtime_id", runtimeID, "task_id", taskID, "error", err)
		}
		d.hookFireCaptureMu.Lock()
		delete(d.activeHookFireCaptures, capture.debugPath)
		d.hookFireCaptureMu.Unlock()
	}()
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
