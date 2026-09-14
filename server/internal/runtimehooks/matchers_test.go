package runtimehooks

import (
	"fmt"
	"strings"
	"testing"
)

func TestClaudeMatcherPatterns(t *testing.T) {
	// https://docs.claude.com/en/docs/claude-code/hooks.md, "Matcher patterns",
	// lines 292-293: "Only letters, digits, `_`, `-`, spaces, `,`, and `|`"
	// are exact strings or lists split on | or , with optional whitespace.
	tests := []struct {
		name    string
		matcher string
		value   string
		want    bool
	}{
		{name: "omitted matches all", value: "Bash", want: true},
		{name: "star matches all", matcher: "*", value: "Bash", want: true},
		{name: "simple exact match", matcher: "Bash", value: "Bash", want: true},
		{name: "simple matcher is not substring regex", matcher: "Bash", value: "PrefixBashSuffix"},
		{name: "pipe separates exact alternatives", matcher: "Edit|Write", value: "Write", want: true},
		{name: "pipe alternatives stay exact", matcher: "Edit|Write", value: "PreWritePost"},
		{name: "hyphenated name is exact", matcher: "code-reviewer", value: "senior-code-reviewer"},
		{name: "hyphenated MCP name is exact", matcher: "mcp__brave-search", value: "mcp__brave-search__web"},
		{name: "comma separates exact alternatives", matcher: "Edit,Write", value: "Write", want: true},
		{name: "comma allows surrounding whitespace", matcher: "Edit, Write", value: "Write", want: true},
		{name: "spaces remain exact", matcher: "Bash Tool", value: "PreBash ToolPost"},
		{name: "regex metacharacter switches to unanchored regex", matcher: `mcp__filesystem__.*`, value: "Pre-mcp__filesystem__read-Post", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolution := resolutionWithMatcher(t, ProviderClaude, "PreToolUse", tt.matcher, `{"type":"command","command":"check"}`)
			result, err := resolution.MatchEvent("PreToolUse", tt.value)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(result.Matched) == 1; got != tt.want {
				t.Fatalf("matched = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClaudeNarrowMatcherEvents(t *testing.T) {
	// https://docs.claude.com/en/docs/claude-code/hooks.md, "Matcher patterns",
	// line 301: FileChanged and StopFailure use [A-Za-z0-9_|] only; a hyphen,
	// space, or comma selects regex and only | separates exact alternatives.
	tests := []struct {
		name    string
		event   string
		matcher string
		value   string
		want    bool
	}{
		{name: "FileChanged hyphen uses unanchored regex", event: "FileChanged", matcher: "code-reviewer", value: "senior-code-reviewer", want: true},
		{name: "StopFailure comma is not a separator", event: "StopFailure", matcher: "Edit, Write", value: "Write"},
		{name: "FileChanged pipe remains a separator", event: "FileChanged", matcher: "env|config", value: "config", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolution := resolutionWithMatcher(t, ProviderClaude, tt.event, tt.matcher, `{"type":"command","command":"check"}`)
			result, err := resolution.MatchEvent(tt.event, tt.value)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(result.Matched) == 1; got != tt.want {
				t.Fatalf("matched = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClaudeIgnoresMatchersForUnsupportedEvents(t *testing.T) {
	// https://docs.claude.com/en/docs/claude-code/hooks.md, "Matcher patterns",
	// lines 319 and 327: these events have no matcher support and always fire.
	events := []string{
		"UserPromptSubmit", "PostToolBatch", "Stop", "TeammateIdle", "TaskCreated",
		"TaskCompleted", "WorktreeCreate", "WorktreeRemove", "MessageDisplay", "CwdChanged",
	}
	for _, event := range events {
		t.Run(event, func(t *testing.T) {
			resolution := resolutionWithMatcher(t, ProviderClaude, event, "[", `{"type":"command","command":"check"}`)
			result, err := resolution.MatchEvent(event, "does-not-match")
			if err != nil {
				t.Fatalf("ignored matcher returned error: %v", err)
			}
			if len(result.Matched) != 1 {
				t.Fatalf("matched entries = %d, want 1", len(result.Matched))
			}
		})
	}
}

func TestClaudeInvalidRegexIsReported(t *testing.T) {
	// https://docs.claude.com/en/docs/claude-code/hooks.md, "Matcher patterns",
	// line 293: characters outside the exact set select unanchored regex.
	resolution := resolutionWithMatcher(t, ProviderClaude, "PreToolUse", "Bash[", `{"type":"command","command":"check"}`)
	_, err := resolution.MatchEvent("PreToolUse", "Bash")
	if err == nil || !strings.Contains(err.Error(), `matcher "Bash["`) {
		t.Fatalf("error = %v, want invalid matcher context", err)
	}
}

func TestCodexMatchersAreAlwaysUnanchoredRegex(t *testing.T) {
	// Codex hooks reference, "Matcher patterns": matcher is a regex string.
	tests := []struct {
		name    string
		matcher string
		value   string
		want    bool
	}{
		{name: "omitted matches all", value: "Bash", want: true},
		{name: "star matches all", matcher: "*", value: "Bash", want: true},
		{name: "plain text remains regex", matcher: "Bash", value: "PrefixBashSuffix", want: true},
		{name: "regex alternation", matcher: "Edit|Write", value: "PreWritePost", want: true},
		{name: "regex mismatch", matcher: `^Bash$`, value: "PrefixBashSuffix"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolution := resolutionWithMatcher(t, ProviderCodex, "PreToolUse", tt.matcher, `{"type":"command","command":"check"}`)
			result, err := resolution.MatchEvent("PreToolUse", tt.value)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(result.Matched) == 1; got != tt.want {
				t.Fatalf("matched = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCodexIgnoresMatchersForUnsupportedEvents(t *testing.T) {
	// Codex hooks reference, "Matcher patterns" event table: these three
	// events ignore any configured matcher.
	for _, event := range []string{"UserPromptSubmit", "Stop", "Interrupt"} {
		t.Run(event, func(t *testing.T) {
			resolution := resolutionWithMatcher(t, ProviderCodex, event, "[", `{"type":"command","command":"check"}`)
			result, err := resolution.MatchEvent(event, "does-not-match")
			if err != nil {
				t.Fatalf("ignored matcher returned error: %v", err)
			}
			if len(result.Matched) != 1 {
				t.Fatalf("matched entries = %d, want 1", len(result.Matched))
			}
		})
	}
}

func TestClaudeIfOnNonToolEventIsNeverRuns(t *testing.T) {
	// Claude hooks reference, "Common fields": if is evaluated only on tool
	// events and prevents a handler on every other event from running.
	resolution := resolutionWithMatcher(t, ProviderClaude, "Stop", "", `{"type":"command","if":"Bash(git *)","command":"check"}`)
	result, err := resolution.MatchEvent("Stop", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matched) != 0 || len(result.NeverRuns) != 1 {
		t.Fatalf("result = %#v, want one never-runs entry", result)
	}
}

func TestClaudeIfOnToolEventsIsNotClassifiedNeverRuns(t *testing.T) {
	// Claude hooks reference, "Common fields": this is the complete tool-event
	// list on which if is evaluated.
	for _, event := range []string{"PreToolUse", "PostToolUse", "PostToolUseFailure", "PermissionRequest", "PermissionDenied"} {
		t.Run(event, func(t *testing.T) {
			resolution := resolutionWithMatcher(t, ProviderClaude, event, "Bash", `{"type":"command","if":"Bash(git *)","command":"check"}`)
			result, err := resolution.MatchEvent(event, "Bash")
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Matched) != 1 || len(result.NeverRuns) != 0 {
				t.Fatalf("result = %#v, want one matched entry", result)
			}
		})
	}
}

func TestCodexIfFieldDoesNotApplyClaudePermissionFiltering(t *testing.T) {
	resolution := resolutionWithMatcher(t, ProviderCodex, "Stop", "ignored", `{"type":"command","if":"Bash(git *)","command":"check"}`)
	result, err := resolution.MatchEvent("Stop", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matched) != 1 || len(result.NeverRuns) != 0 {
		t.Fatalf("result = %#v, want Codex entry matched", result)
	}
}

func TestMatchEventExcludesParkedClaudeHookFromRuns(t *testing.T) {
	ref := SourceRef{Scope: "local", Format: "json"}
	path, hash := "/repo/.claude/settings.local.json", "settings-hash"
	hooks := raw(`{"PreToolUse":[{"matcher":"Bash","hooks":[
		{"type":"command","command":"./park.sh"},
		{"type":"command","command":"./live.sh"}
	]}]}`)
	base := mustResolve(t, ProviderClaude, []ObservedSource{{
		Source: ref, SourcePath: &path, ContentHash: &hash, Hooks: hooks,
	}})
	parkedHook := entriesByCommand(t, base.EntriesForEvent("PreToolUse"))["./park.sh"]
	parked := fmt.Sprintf(`{%q:{"event":"PreToolUse","matcher":"Bash","handler":{"type":"command","command":"./park.sh"},"parked_at":"2026-09-12T08:00:00Z"}}`, parkedHook.HookID)
	resolution := mustResolve(t, ProviderClaude, []ObservedSource{{
		Source: ref, SourcePath: &path, ContentHash: &hash, Hooks: hooks, DisabledHooks: raw(parked),
	}})

	result, err := resolution.MatchEvent("PreToolUse", "Bash")
	if err != nil {
		t.Fatal(err)
	}
	runs := resolution.MergeMatched(result.Matched)
	if got := entriesByCommand(t, runs); len(got) != 1 || got["./live.sh"].HookID == "" {
		t.Fatalf("runs = %#v, want only live hook", runs)
	}
	if len(result.ConfigurationExcluded) != 1 || result.ConfigurationExcluded[0].Configuration != ConfigurationParked || result.ConfigurationExcluded[0].ParkedAt == "" {
		t.Fatalf("configuration-excluded = %#v, want parked hook with timestamp", result.ConfigurationExcluded)
	}
}

func TestMatchEventExcludesDisabledCodexHookFromRuns(t *testing.T) {
	ref := SourceRef{Scope: "user", Format: "json"}
	configRef := SourceRef{Scope: "user", Format: "toml"}
	hooksPath, configPath := "/home/marty/.codex/hooks.json", "/home/marty/.codex/config.toml"
	hooksHash, configHash := "hooks-hash", "config-hash"
	resolution := mustResolve(t, ProviderCodex, []ObservedSource{
		{
			Source: ref, SourcePath: &hooksPath, ContentHash: &hooksHash,
			Hooks: raw(`{"PreToolUse":[{"matcher":"Bash","hooks":[
				{"type":"command","command":"./off.sh"},
				{"type":"command","command":"./live.sh"}
			]}]}`),
		},
		{
			Source: configRef, SourcePath: &configPath, ContentHash: &configHash, Hooks: raw(`{}`),
			DisabledHooks: raw(`{"state":{"/home/marty/.codex/hooks.json:pre_tool_use:0:0":{"enabled":false}}}`),
		},
	})

	result, err := resolution.MatchEvent("PreToolUse", "Bash")
	if err != nil {
		t.Fatal(err)
	}
	runs := resolution.MergeMatched(result.Matched)
	if got := entriesByCommand(t, runs); len(got) != 1 || got["./live.sh"].HookID == "" {
		t.Fatalf("runs = %#v, want only live hook", runs)
	}
	if len(result.ConfigurationExcluded) != 1 || result.ConfigurationExcluded[0].Configuration != ConfigurationDisabled {
		t.Fatalf("configuration-excluded = %#v, want disabled hook", result.ConfigurationExcluded)
	}
}

func resolutionWithMatcher(t *testing.T, provider Provider, event, matcher, handler string) Resolution {
	t.Helper()
	source := foundSource(SourceRef{Scope: "user", Format: "json"}, `{"`+event+`":[{"matcher":"`+matcher+`","hooks":[`+handler+`]}]}`)
	resolution, err := Resolve(provider, []SourceRef{source.Source}, []ObservedSource{source})
	if err != nil {
		t.Fatal(err)
	}
	return resolution
}
