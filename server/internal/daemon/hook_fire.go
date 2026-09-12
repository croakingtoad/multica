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

// claudeHookFireCapture joins two host-observed records of the same
// invocation. Claude's structured hook_response supplies per-execution
// identity and exit status; its debug log supplies the durable provenance and
// host timestamp required by hook_fire_history. Neither source alone is
// sufficient, so unmatched records are never persisted.
type claudeHookFireCapture struct {
	debugPath string

	mu        sync.Mutex
	responses []agent.ClaudeHookResponse
}

func (c *claudeHookFireCapture) observe(response agent.ClaudeHookResponse) {
	if response.ExitCode != nil {
		exitCode := *response.ExitCode
		response.ExitCode = &exitCode
	}
	c.mu.Lock()
	c.responses = append(c.responses, response)
	c.mu.Unlock()
}

func (c *claudeHookFireCapture) responseSnapshot() []agent.ClaudeHookResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]agent.ClaudeHookResponse(nil), c.responses...)
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

func claudeFireSignature(hookName, event, outcome, output string) string {
	return hookName + "\x00" + event + "\x00" + outcome + "\x00" + output
}

func claudeHookNeverRan(response agent.ClaudeHookResponse, firstLine string) bool {
	lower := strings.ToLower(strings.TrimSpace(firstLine))
	if strings.HasPrefix(lower, "failed to run:") ||
		strings.HasPrefix(lower, "error occurred while executing hook command:") ||
		strings.Contains(lower, "executable not found in $path:") {
		return true
	}
	if response.ExitCode == nil || (*response.ExitCode != 126 && *response.ExitCode != 127) {
		return false
	}
	return strings.Contains(lower, ": not found") ||
		strings.Contains(lower, "command not found") ||
		strings.Contains(lower, ": permission denied") ||
		strings.Contains(lower, ": cannot execute")
}

func hookFireOutcome(response agent.ClaudeHookResponse, firstLine string) string {
	switch response.Outcome {
	case "success":
		return "success"
	case "error":
		if claudeHookNeverRan(response, firstLine) {
			return "skipped"
		}
		if response.ExitCode == nil {
			return "unknown"
		}
		return "failure"
	default:
		return "unknown"
	}
}

func hookFireID(runtimeID, taskID, streamHookID string, firedAt time.Time) string {
	hash := sha256.New()
	for _, value := range []string{runtimeID, taskID, streamHookID, firedAt.Format(time.RFC3339Nano)} {
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
		key := claudeFireSignature(record.hookName, record.event, record.debugOutcome, record.output)
		bySignature[key] = append(bySignature[key], record)
	}

	fires := make([]protocol.HookFire, 0)
	for _, response := range c.responseSnapshot() {
		if response.HookID == "" || response.HookName == "" || response.HookEvent == "" {
			continue
		}
		key := claudeFireSignature(response.HookName, response.HookEvent, response.Outcome, claudeResponseDebugOutput(response))
		matches := bySignature[key]
		if len(matches) == 0 {
			continue
		}
		record := matches[0]
		bySignature[key] = matches[1:]

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
		outcome := hookFireOutcome(response, record.firstLine)
		detailValue := map[string]any{
			"debug_outcome": response.Outcome,
			"message":       truncateHookFireDetail(record.firstLine),
		}
		if response.ExitCode != nil {
			detailValue["exit_code"] = *response.ExitCode
		}
		detail, err := json.Marshal(detailValue)
		if err != nil {
			return nil, fmt.Errorf("encode Claude hook fire detail: %w", err)
		}
		fires = append(fires, protocol.HookFire{
			ID:    hookFireID(runtimeID, taskID, response.HookID, record.firedAt),
			Event: response.HookEvent, HookID: response.HookID, HookSpec: hookSpec,
			FiredAt: record.firedAt.Format(time.RFC3339Nano), Outcome: outcome, Detail: detail,
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
	return &claudeHookFireCapture{
		debugPath: filepath.Join(debugDir, fmt.Sprintf("%s-%d.log", taskID, time.Now().UnixNano())),
	}
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
