package runtimehooks

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var (
	claudeExactMatcher       = regexp.MustCompile(`^[-A-Za-z0-9_, |]+$`)
	claudeNarrowExactMatcher = regexp.MustCompile(`^[A-Za-z0-9_|]+$`)
)

// MatchResult partitions the unordered candidates for an event. NeverRuns
// contains Claude handlers whose if pre-filter is set on an event where Claude
// does not evaluate permission rules. Neither slice conveys execution order.
type MatchResult struct {
	Matched   []ResolvedHook
	NeverRuns []ResolvedHook
}

// MatchEvent applies the resolution provider's matcher semantics to the
// unordered candidates for event. Value is the event-specific field described
// by the provider, such as a tool name or session-start source.
func (r Resolution) MatchEvent(event, value string) (MatchResult, error) {
	var result MatchResult
	for _, entry := range r.EntriesForEvent(event) {
		if r.Provider == ProviderClaude && !isClaudeToolEvent(event) {
			hasIf, err := handlerHasIf(entry.Handler)
			if err != nil {
				return MatchResult{}, fmt.Errorf("inspect %s handler if pre-filter: %w", event, err)
			}
			if hasIf {
				result.NeverRuns = append(result.NeverRuns, entry)
				continue
			}
		}

		matched, err := providerMatcherMatches(r.Provider, event, entry.Matcher, value)
		if err != nil {
			return MatchResult{}, err
		}
		if matched {
			result.Matched = append(result.Matched, entry)
		}
	}
	return result, nil
}

func providerMatcherMatches(provider Provider, event, matcher, value string) (bool, error) {
	if matcher == "" || matcher == "*" {
		return true, nil
	}
	if provider == ProviderCodex {
		if codexIgnoresMatcher(event) {
			return true, nil
		}
		return regexMatches(provider, matcher, value)
	}
	if claudeIgnoresMatcher(event) {
		return true, nil
	}
	if isClaudeNarrowMatcherEvent(event) {
		if claudeNarrowExactMatcher.MatchString(matcher) {
			return exactMatcherMatches(matcher, value, false), nil
		}
	} else if claudeExactMatcher.MatchString(matcher) {
		return exactMatcherMatches(matcher, value, true), nil
	}
	return regexMatches(provider, matcher, value)
}

func exactMatcherMatches(matcher, value string, splitCommas bool) bool {
	if splitCommas {
		matcher = strings.ReplaceAll(matcher, ",", "|")
	}
	for _, candidate := range strings.Split(matcher, "|") {
		if strings.TrimSpace(candidate) == value {
			return true
		}
	}
	return false
}

func regexMatches(provider Provider, matcher, value string) (bool, error) {
	pattern, err := regexp.Compile(matcher)
	if err != nil {
		return false, fmt.Errorf("compile %s matcher %q: %w", provider, matcher, err)
	}
	return pattern.MatchString(value), nil
}

func isClaudeToolEvent(event string) bool {
	switch event {
	case "PreToolUse", "PostToolUse", "PostToolUseFailure", "PermissionRequest", "PermissionDenied":
		return true
	default:
		return false
	}
}

func codexIgnoresMatcher(event string) bool {
	switch event {
	case "UserPromptSubmit", "Stop", "Interrupt":
		return true
	default:
		return false
	}
}

func claudeIgnoresMatcher(event string) bool {
	switch event {
	case "UserPromptSubmit", "PostToolBatch", "Stop", "TeammateIdle", "TaskCreated", "TaskCompleted",
		"WorktreeCreate", "WorktreeRemove", "MessageDisplay", "CwdChanged":
		return true
	default:
		return false
	}
}

func isClaudeNarrowMatcherEvent(event string) bool {
	return event == "FileChanged" || event == "StopFailure"
}

func handlerHasIf(handler json.RawMessage) (bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(handler, &fields); err != nil {
		return false, err
	}
	_, ok := fields["if"]
	return ok, nil
}
