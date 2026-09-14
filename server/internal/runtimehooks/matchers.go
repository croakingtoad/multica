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

// MatchResult partitions the unordered candidates for an event.
// ConfigurationExcluded contains parked or disabled hooks, including their
// configuration metadata. NeverRuns contains Claude handlers whose if
// pre-filter is set on an event where Claude does not evaluate permission
// rules. No slice conveys execution order.
type MatchResult struct {
	Matched               []ResolvedHook
	NeverRuns             []ResolvedHook
	ConfigurationExcluded []RunState
}

// MatchEvent applies the resolution provider's matcher semantics to the
// unordered candidates for event. Value is the event-specific field described
// by the provider, such as a tool name or session-start source.
func (r Resolution) MatchEvent(event, value string) (MatchResult, error) {
	states, err := r.RunStates()
	if err != nil {
		return MatchResult{}, fmt.Errorf("derive %s hook configuration: %w", r.Provider, err)
	}

	var result MatchResult
	var live []ResolvedHook
	for _, state := range states {
		if state.Hook.Event != event {
			continue
		}
		if state.Configuration != ConfigurationLive {
			result.ConfigurationExcluded = append(result.ConfigurationExcluded, state)
			continue
		}
		live = append(live, state.Hook)
	}
	matched, err := matchEventEntries(r.Provider, event, value, live)
	if err != nil {
		return MatchResult{}, err
	}
	result.Matched = matched.Matched
	result.NeverRuns = matched.NeverRuns
	return result, nil
}

// matchEventEntries applies only provider matcher and effectiveness rules. It
// is also used while deriving RunStates, before configuration is available.
func matchEventEntries(provider Provider, event, value string, entries []ResolvedHook) (MatchResult, error) {
	var result MatchResult
	for _, entry := range entries {
		if provider == ProviderClaude && !isClaudeToolEvent(event) {
			hasIf, err := handlerHasIf(entry.Handler)
			if err != nil {
				return MatchResult{}, fmt.Errorf("inspect %s handler if pre-filter: %w", event, err)
			}
			if hasIf {
				result.NeverRuns = append(result.NeverRuns, entry)
				continue
			}
		}

		matched, err := providerMatcherMatches(provider, event, entry.Matcher, value)
		if err != nil {
			return MatchResult{}, err
		}
		if matched {
			result.Matched = append(result.Matched, entry)
		}
	}
	return result, nil
}

// providerMatcherMatches evaluates matcher against value on the path
// EvaluateMatcher classifies. Sharing that classifier is deliberate: the
// matcher kind a reader is shown and the rule a match is decided by are then
// the same rule, and cannot drift apart.
func providerMatcherMatches(provider Provider, event, matcher, value string) (bool, error) {
	switch EvaluateMatcher(provider, event, matcher).Kind {
	case MatcherAll, MatcherIgnored:
		return true, nil
	case MatcherExact:
		// Only Claude reaches the exact path, and only its wider set admits
		// the comma separator that the narrow-matcher events exclude.
		return exactMatcherMatches(matcher, value, !isClaudeNarrowMatcherEvent(event)), nil
	default:
		// MatcherRegex and MatcherUnevaluable alike: regexMatches returns the
		// compiler's error for the latter rather than a silent false.
		return regexMatches(provider, matcher, value)
	}
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
