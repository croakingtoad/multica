package runtimehooks

// Answering "which handlers run on this event" requires a value, because that
// is what a matcher is evaluated against. There is no valueless form of this
// answer: without a tool name, a changed file or a start source, the truthful
// statement is "these run if their matcher is satisfied", which is a
// condition and not an answer. AnswerEvent therefore always takes a value and
// routes it through MatchEvent, so the answer a reader sees is produced by the
// same matcher rules the provider's own evaluation is modelled on.
//
// Nothing here assigns an execution order. Every set below is unordered, as
// the package doc says: neither provider publishes one, and Claude's doc says
// "All matching hooks run in parallel"
// (https://code.claude.com/docs/en/hooks, "Configuration").

// ValueRole is what a provider evaluates an event's matcher against, so a
// reader knows what kind of value to supply. It is display guidance only —
// nothing matches on it, and ValueRoleUnspecified is returned wherever the
// provider's own documentation does not state the role, rather than inferring
// one from the event's name.
type ValueRole string

const (
	// ValueRoleIgnored is an event whose matcher the provider parses and then
	// discards, so the value supplied changes nothing.
	ValueRoleIgnored ValueRole = "ignored"
	// ValueRoleToolName is the canonical tool name, such as Bash.
	ValueRoleToolName ValueRole = "tool_name"
	// ValueRoleCompactionTrigger is Codex's manual / auto compaction trigger.
	ValueRoleCompactionTrigger ValueRole = "compaction_trigger"
	// ValueRoleSessionStartSource is Codex's startup / resume / clear / compact.
	ValueRoleSessionStartSource ValueRole = "session_start_source"
	// ValueRoleSessionEndReason is Codex's session end reason.
	ValueRoleSessionEndReason ValueRole = "session_end_reason"
	// ValueRoleSubagentType is Codex's subagent type.
	ValueRoleSubagentType ValueRole = "subagent_type"
	// ValueRoleChangedFileBasename is Claude's FileChanged basename.
	ValueRoleChangedFileBasename ValueRole = "changed_file_basename"
	// ValueRoleUnspecified is every event whose role the provider's docs do
	// not state. The matcher is still evaluated; only the caption is withheld.
	ValueRoleUnspecified ValueRole = "unspecified"
)

// EventValueRole reports what provider matches event's matcher against.
//
// Codex's roles are its documented table verbatim
// (https://developers.openai.com/codex/hooks, "Matcher patterns": PreToolUse,
// PostToolUse and PermissionRequest filter on "tool name"; PreCompact and
// PostCompact on "compaction trigger"; SessionStart on "start source";
// SessionEnd on "end reason"; SubagentStart and SubagentStop on "subagent
// type"; UserPromptSubmit, Stop and Interrupt are "not supported").
//
// Claude's tool events reuse isClaudeToolEvent rather than a second list, so
// the events told to expect a tool name are exactly the events the matcher
// layer already treats as tool events. FileChanged is its documented
// exception: the matcher "filters which hook groups run using the standard
// matcher rules against the changed file's basename"
// (https://code.claude.com/docs/en/hooks, "FileChanged").
func EventValueRole(provider Provider, event string) ValueRole {
	if provider == ProviderCodex {
		if codexIgnoresMatcher(event) {
			return ValueRoleIgnored
		}
		switch event {
		case "PreToolUse", "PostToolUse", "PermissionRequest":
			return ValueRoleToolName
		case "PreCompact", "PostCompact":
			return ValueRoleCompactionTrigger
		case "SessionStart":
			return ValueRoleSessionStartSource
		case "SessionEnd":
			return ValueRoleSessionEndReason
		case "SubagentStart", "SubagentStop":
			return ValueRoleSubagentType
		default:
			return ValueRoleUnspecified
		}
	}
	if claudeIgnoresMatcher(event) {
		return ValueRoleIgnored
	}
	if isClaudeToolEvent(event) {
		return ValueRoleToolName
	}
	if event == "FileChanged" {
		return ValueRoleChangedFileBasename
	}
	return ValueRoleUnspecified
}

// UnevaluableMatcher names one live matcher on an event that Go's RE2 refuses
// to compile, and carries the compiler's own message. It is Multica's limit,
// never a provider verdict.
//
// Presence here does not by itself make the event unanswerable: Claude's if
// pre-filter can exclude a handler on a non-tool event before its matcher is
// reached. EventAnswer.Answerable is the authoritative flag, and a client must
// key its "cannot answer" state off that rather than off this list.
type UnevaluableMatcher struct {
	HookID  string `json:"hook_id"`
	Matcher string `json:"matcher"`
	Error   string `json:"error"`
}

// EventAnswer is Multica's answer for one event and one candidate value.
//
// Answerable false means Multica has no answer — it never means nothing runs.
// All four sets are empty in that case, and rendering an empty Matched as
// "nothing fires" would be a false statement about the host rather than a
// report of Multica's limit. MatchEvent scopes its failure to the whole
// event, so the whole event is what goes unanswered; narrowing that to the
// offending hook is a decision held open elsewhere and is not taken here.
type EventAnswer struct {
	Provider  string    `json:"provider"`
	Event     string    `json:"event"`
	Value     string    `json:"value"`
	ValueRole ValueRole `json:"value_role"`

	Answerable  bool                 `json:"answerable"`
	Error       string               `json:"error,omitempty"`
	Unevaluable []UnevaluableMatcher `json:"unevaluable,omitempty"`

	// Matched is the answer: live entries whose matcher Value satisfies. Each
	// one still carries both axes, because a matched entry can still be one
	// the provider will not act on or one whose Codex trust is unconfirmed.
	// An entry the provider parses and skips — an unsupported handler type,
	// say — stays here, carrying Effectiveness never_runs; membership of this
	// set is about the matcher, not about whether the provider acts.
	// Claude's cross-source collapse has been applied, so a handler defined
	// byte-identically in two settings files appears once, carrying both
	// sources — a deduplication of one definition seen twice, not one source
	// winning over another.
	Matched []ProjectedEntry `json:"matched"`
	// NotMatched is live configuration this Value does not satisfy. It is
	// here so the answer is legible as a selection from the configured set.
	NotMatched []ProjectedEntry `json:"not_matched"`
	// NeverRuns is live configuration the provider's own pre-matcher filter
	// excludes before any matcher is evaluated: Claude entries carrying an if
	// pre-filter on an event where Claude evaluates no permission rules. It is
	// not every entry the provider will not act on — one it parses and skips
	// for any later reason stays in Matched with the never-runs axis on it.
	NeverRuns []ProjectedEntry `json:"never_runs"`
	// ConfigurationExcluded is parked or disabled, so the provider never sees
	// it. Configuration, not effectiveness: each entry keeps its own verdict.
	ConfigurationExcluded []ProjectedEntry `json:"configuration_excluded"`
}

// AnswerEvent resolves observed against expected and answers event for value.
// It decides no provider rule of its own: matching goes through MatchEvent,
// matcher classification through EvaluateMatcher, the cross-source collapse
// through MergeMatched, and both state axes through RunStates.
func AnswerEvent(provider Provider, expected []SourceRef, observed []ObservedSource, event, value string) EventAnswer {
	answer := EventAnswer{
		Provider:              string(provider),
		Event:                 event,
		Value:                 value,
		ValueRole:             EventValueRole(provider, event),
		Matched:               []ProjectedEntry{},
		NotMatched:            []ProjectedEntry{},
		NeverRuns:             []ProjectedEntry{},
		ConfigurationExcluded: []ProjectedEntry{},
	}

	resolution, err := resolveObserved(provider, expected, observed)
	if err != nil {
		answer.Error = err.Error()
		return answer
	}
	states, err := resolution.RunStates()
	if err != nil {
		answer.Error = err.Error()
		return answer
	}

	byHookID := make(map[string]RunState, len(states))
	for _, state := range states {
		byHookID[state.Hook.HookID] = state
		if state.Hook.Event != event || state.Configuration != ConfigurationLive {
			continue
		}
		// Collected from the same classifier providerMatcherMatches routes
		// through, so a matcher named here as unevaluable is the matcher the
		// match itself would refuse.
		if evaluation := EvaluateMatcher(provider, event, state.Hook.Matcher); evaluation.Kind == MatcherUnevaluable {
			answer.Unevaluable = append(answer.Unevaluable, UnevaluableMatcher{
				HookID:  state.Hook.HookID,
				Matcher: state.Hook.Matcher,
				Error:   evaluation.Error,
			})
		}
	}

	result, err := resolution.MatchEvent(event, value)
	if err != nil {
		answer.Error = err.Error()
		return answer
	}
	answer.Answerable = true

	// MergeMatched is applied only to Matched, which is the one set whose
	// matchers have already matched — its documented input. The other three
	// stay one row per source occurrence, as the tab's own list does.
	answer.Matched = answerEntries(provider, byHookID, resolution.MergeMatched(result.Matched))
	answer.NeverRuns = answerEntries(provider, byHookID, result.NeverRuns)

	decided := make(map[string]struct{}, len(result.Matched)+len(result.NeverRuns))
	for _, entry := range result.Matched {
		decided[entry.HookID] = struct{}{}
	}
	for _, entry := range result.NeverRuns {
		decided[entry.HookID] = struct{}{}
	}
	var notMatched []ResolvedHook
	for _, state := range states {
		if state.Hook.Event != event || state.Configuration != ConfigurationLive {
			continue
		}
		if _, ok := decided[state.Hook.HookID]; ok {
			continue
		}
		notMatched = append(notMatched, state.Hook)
	}
	answer.NotMatched = answerEntries(provider, byHookID, notMatched)

	excluded := make([]ResolvedHook, 0, len(result.ConfigurationExcluded))
	for _, state := range result.ConfigurationExcluded {
		excluded = append(excluded, state.Hook)
	}
	answer.ConfigurationExcluded = answerEntries(provider, byHookID, excluded)

	for _, set := range [][]ProjectedEntry{
		answer.Matched, answer.NotMatched, answer.NeverRuns, answer.ConfigurationExcluded,
	} {
		// Identity ordering only, exactly as sortProjectedEntries is used on
		// the projection: it gives a client a stable list and states no
		// precedence, because no provider assigns one.
		sortProjectedEntries(set)
	}
	return answer
}

// answerEntries projects hooks through their own RunState, so both axes come
// from the resolution layer and none is re-derived here. A hook merged across
// sources keeps its merged source list; every other identity field is equal
// across the merged members by construction, and MergeMatched only ever
// merges live Claude settings entries, whose trust is not applicable.
func answerEntries(provider Provider, byHookID map[string]RunState, hooks []ResolvedHook) []ProjectedEntry {
	entries := make([]ProjectedEntry, 0, len(hooks))
	for _, hook := range hooks {
		state, ok := byHookID[hook.HookID]
		if !ok {
			continue
		}
		state.Hook = hook
		entries = append(entries, projectEntry(provider, state))
	}
	return entries
}
