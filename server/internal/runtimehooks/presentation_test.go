package runtimehooks

import (
	"encoding/json"
	"testing"
)

func TestEvaluateMatcherKinds(t *testing.T) {
	cases := []struct {
		name     string
		provider Provider
		event    string
		matcher  string
		want     MatcherKind
	}{
		{"claude empty matches all", ProviderClaude, "PreToolUse", "", MatcherAll},
		{"claude star matches all", ProviderClaude, "PreToolUse", "*", MatcherAll},
		{"claude exact set", ProviderClaude, "PreToolUse", "Edit, Write", MatcherExact},
		{"claude hyphen stays exact", ProviderClaude, "PreToolUse", "code-reviewer", MatcherExact},
		{"claude regex outside the set", ProviderClaude, "PreToolUse", "mcp__memory__.*", MatcherRegex},
		{"claude narrow event rejects comma", ProviderClaude, "FileChanged", "a,b", MatcherRegex},
		{"claude narrow event accepts pipe", ProviderClaude, "FileChanged", "a|b", MatcherExact},
		{"claude matcherless event", ProviderClaude, "UserPromptSubmit", "Bash", MatcherIgnored},
		{"codex has no exact path", ProviderCodex, "PreToolUse", "Bash", MatcherRegex},
		{"codex matcherless event", ProviderCodex, "Stop", "Bash", MatcherIgnored},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := EvaluateMatcher(testCase.provider, testCase.event, testCase.matcher)
			if got.Kind != testCase.want {
				t.Fatalf("kind = %q, want %q", got.Kind, testCase.want)
			}
			if got.Error != "" {
				t.Fatalf("unexpected error %q", got.Error)
			}
		})
	}
}

// A lookahead is legal in the JavaScript regex Claude documents and illegal in
// Go's RE2. The classification must say Multica cannot evaluate it, because
// the alternative — reporting a normal regex, or a "never runs" verdict — both
// assert something about the hook that Multica has not established.
func TestEvaluateMatcherReportsUnevaluableLookahead(t *testing.T) {
	got := EvaluateMatcher(ProviderClaude, "PreToolUse", "^(?!Notebook).*")
	if got.Kind != MatcherUnevaluable {
		t.Fatalf("kind = %q, want %q", got.Kind, MatcherUnevaluable)
	}
	if got.Error == "" {
		t.Fatal("unevaluable matcher must carry the compiler's message")
	}
}

// The classifier is also the matcher rule MatchEvent applies, so an exact
// matcher must still match by string and an unevaluable one must still error
// the event rather than silently failing to match.
func TestProviderMatcherMatchesRoutesThroughEvaluateMatcher(t *testing.T) {
	matched, err := providerMatcherMatches(ProviderClaude, "PreToolUse", "Edit, Write", "Write")
	if err != nil || !matched {
		t.Fatalf("exact list: matched = %v, err = %v", matched, err)
	}
	matched, err = providerMatcherMatches(ProviderClaude, "FileChanged", "a,b", "a")
	if err != nil || matched {
		t.Fatalf("narrow event comma is a regex, not a list: matched = %v, err = %v", matched, err)
	}
	if _, err = providerMatcherMatches(ProviderClaude, "PreToolUse", "^(?!Notebook).*", "Edit"); err == nil {
		t.Fatal("an unevaluable matcher must surface an error")
	}
}

func TestHandlerTypeReadsDeclaredType(t *testing.T) {
	hook := ResolvedHook{Handler: json.RawMessage(`{"type":"command","command":"true"}`)}
	if got := hook.HandlerType(); got != "command" {
		t.Fatalf("HandlerType() = %q, want %q", got, "command")
	}
	undeclared := ResolvedHook{Handler: json.RawMessage(`{"command":"true"}`)}
	if got := undeclared.HandlerType(); got != "" {
		t.Fatalf("HandlerType() = %q, want empty", got)
	}
	unreadable := ResolvedHook{Handler: json.RawMessage(`"command"`)}
	if got := unreadable.HandlerType(); got != "" {
		t.Fatalf("HandlerType() = %q, want empty", got)
	}
}
