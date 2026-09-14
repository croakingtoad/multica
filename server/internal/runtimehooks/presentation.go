package runtimehooks

import "regexp"

// MatcherKind is the evaluation path a provider takes for a configured
// matcher. It exists so a reader can tell an exact-string matcher from a
// regex one, and either of those from a matcher Multica cannot evaluate.
type MatcherKind string

const (
	// MatcherAll is the empty or "*" matcher: every occurrence fires.
	MatcherAll MatcherKind = "all"
	// MatcherExact is Claude's exact-string path, optionally a "|"/","
	// separated list. Codex has no exact path and never produces this.
	MatcherExact MatcherKind = "exact"
	// MatcherRegex is an unanchored regular expression.
	MatcherRegex MatcherKind = "regex"
	// MatcherIgnored is an event where the provider parses the matcher and
	// then discards it, so the hook fires on every occurrence anyway.
	MatcherIgnored MatcherKind = "ignored"
	// MatcherUnevaluable is a matcher Go's RE2 refuses to compile. The Claude
	// doc permits JavaScript regex constructs RE2 has no support for, a
	// lookahead such as `^(?!Notebook).*` among them. It is a limit of
	// Multica's evaluation, never a provider verdict: the hook may well run.
	MatcherUnevaluable MatcherKind = "unevaluable"
)

// MatcherEvaluation classifies one matcher. Error is populated only for
// MatcherUnevaluable and carries the compiler's own message.
type MatcherEvaluation struct {
	Kind  MatcherKind
	Error string
}

// EvaluateMatcher reports how provider evaluates matcher on event. It is the
// single decision point for matcher semantics: providerMatcherMatches routes
// through it so a displayed classification and an executed match cannot
// disagree.
func EvaluateMatcher(provider Provider, event, matcher string) MatcherEvaluation {
	if matcher == "" || matcher == "*" {
		return MatcherEvaluation{Kind: MatcherAll}
	}
	if provider == ProviderCodex {
		// https://developers.openai.com/codex/hooks, "Matcher patterns": the
		// matcher is always a regex, and three events ignore it entirely.
		if codexIgnoresMatcher(event) {
			return MatcherEvaluation{Kind: MatcherIgnored}
		}
		return evaluateRegexMatcher(matcher)
	}
	if claudeIgnoresMatcher(event) {
		return MatcherEvaluation{Kind: MatcherIgnored}
	}
	// https://code.claude.com/docs/en/hooks, "Matcher value": a value drawn
	// only from the exact-match set is compared as a string; anything else is
	// an unanchored regex. FileChanged and StopFailure use a narrower set.
	if isClaudeNarrowMatcherEvent(event) {
		if claudeNarrowExactMatcher.MatchString(matcher) {
			return MatcherEvaluation{Kind: MatcherExact}
		}
	} else if claudeExactMatcher.MatchString(matcher) {
		return MatcherEvaluation{Kind: MatcherExact}
	}
	return evaluateRegexMatcher(matcher)
}

func evaluateRegexMatcher(matcher string) MatcherEvaluation {
	if _, err := regexp.Compile(matcher); err != nil {
		return MatcherEvaluation{Kind: MatcherUnevaluable, Error: err.Error()}
	}
	return MatcherEvaluation{Kind: MatcherRegex}
}

// HandlerType is the handler's declared type, for display alongside the run
// state. An empty result means the handler declares no readable type; whether
// that stops the hook is RunState.Effectiveness's answer, not this one's.
func (h ResolvedHook) HandlerType() string {
	name, err := handlerType(h.Handler)
	if err != nil {
		return ""
	}
	return name
}
