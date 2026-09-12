package runtimehooks

import (
	"strings"
	"testing"
)

func TestClaudeMatcherPatterns(t *testing.T) {
	// Claude hooks reference, "Matcher patterns": empty and * match all;
	// [A-Za-z0-9_|] matchers are exact alternatives; all others are regex.
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
		{name: "hyphen switches to unanchored regex", matcher: "Bash-Tool", value: "PreBash-ToolPost", want: true},
		{name: "comma switches to unanchored regex", matcher: "Bash,Edit", value: "PreBash,EditPost", want: true},
		{name: "space switches to unanchored regex", matcher: "Bash Tool", value: "PreBash ToolPost", want: true},
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

func TestClaudeInvalidRegexIsReported(t *testing.T) {
	// Claude hooks reference, "Matcher patterns": punctuation selects regex
	// evaluation, so malformed patterns must remain visible to callers.
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

func resolutionWithMatcher(t *testing.T, provider Provider, event, matcher, handler string) Resolution {
	t.Helper()
	source := foundSource(SourceRef{Scope: "user", Format: "json"}, `{"`+event+`":[{"matcher":"`+matcher+`","hooks":[`+handler+`]}]}`)
	resolution, err := Resolve(provider, []SourceRef{source.Source}, []ObservedSource{source})
	if err != nil {
		t.Fatal(err)
	}
	return resolution
}
