package runtimehooks

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Every value in the role table below is the providers' own documented
// wording, not an inference from an event name. Sources:
//
//	https://developers.openai.com/codex/hooks, "Matcher patterns"
//	https://code.claude.com/docs/en/hooks, "Matcher patterns" / "FileChanged"
func TestEventValueRoleFollowsProviderDocs(t *testing.T) {
	tests := []struct {
		provider Provider
		event    string
		want     ValueRole
	}{
		// Codex's table, row for row.
		{ProviderCodex, "PermissionRequest", ValueRoleToolName},
		{ProviderCodex, "PostToolUse", ValueRoleToolName},
		{ProviderCodex, "PreToolUse", ValueRoleToolName},
		{ProviderCodex, "PostCompact", ValueRoleCompactionTrigger},
		{ProviderCodex, "PreCompact", ValueRoleCompactionTrigger},
		{ProviderCodex, "SessionEnd", ValueRoleSessionEndReason},
		{ProviderCodex, "SessionStart", ValueRoleSessionStartSource},
		{ProviderCodex, "SubagentStart", ValueRoleSubagentType},
		{ProviderCodex, "SubagentStop", ValueRoleSubagentType},
		{ProviderCodex, "UserPromptSubmit", ValueRoleIgnored},
		{ProviderCodex, "Stop", ValueRoleIgnored},
		{ProviderCodex, "Interrupt", ValueRoleIgnored},
		// An event Codex's table does not list keeps no caption rather than
		// borrowing one from a similarly named event.
		{ProviderCodex, "Notification", ValueRoleUnspecified},
		// Claude's tool events reuse the matcher layer's own classification.
		{ProviderClaude, "PreToolUse", ValueRoleToolName},
		{ProviderClaude, "PostToolUse", ValueRoleToolName},
		{ProviderClaude, "PermissionRequest", ValueRoleToolName},
		// "the same value filters which hook groups run using the standard
		// matcher rules against the changed file's basename".
		{ProviderClaude, "FileChanged", ValueRoleChangedFileBasename},
		{ProviderClaude, "UserPromptSubmit", ValueRoleIgnored},
		{ProviderClaude, "Stop", ValueRoleIgnored},
		{ProviderClaude, "StopFailure", ValueRoleUnspecified},
	}
	for _, tt := range tests {
		t.Run(string(tt.provider)+"/"+tt.event, func(t *testing.T) {
			if got := EventValueRole(tt.provider, tt.event); got != tt.want {
				t.Fatalf("EventValueRole(%s, %s) = %q, want %q", tt.provider, tt.event, got, tt.want)
			}
		})
	}
}

// The doc's own worked example, verbatim: matcher "Bash" on PreToolUse with a
// handler carrying if "Bash(rm *)". "The matcher `"Bash"` matches the tool
// name, so this hook group activates" — the tool name is the value, and the
// answer for "Bash" includes it (https://code.claude.com/docs/en/hooks,
// "How a hook runs").
func TestAnswerEventMatchesClaudeDocWorkedExample(t *testing.T) {
	source := projectedSource("user", "json",
		`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"block-rm.sh","if":"Bash(rm *)"}]}]}`, `{}`)
	answer := AnswerEvent(ProviderClaude, claudeExpected(), []ObservedSource{source}, "PreToolUse", "Bash")

	if !answer.Answerable {
		t.Fatalf("answer should be answerable: %+v", answer)
	}
	if len(answer.Matched) != 1 {
		t.Fatalf("matched = %d, want 1: %+v", len(answer.Matched), answer.Matched)
	}
	if answer.ValueRole != ValueRoleToolName {
		t.Fatalf("value role = %q, want tool_name", answer.ValueRole)
	}
	// Both axes travel with the entry rather than being collapsed into the
	// bucket it landed in.
	if answer.Matched[0].Configuration != string(ConfigurationLive) {
		t.Fatalf("configuration = %q, want live", answer.Matched[0].Configuration)
	}
	if answer.Matched[0].Effectiveness != string(EffectivenessWillRun) {
		t.Fatalf("effectiveness = %q, want will_run", answer.Matched[0].Effectiveness)
	}
	if len(answer.NotMatched) != 0 || len(answer.NeverRuns) != 0 {
		t.Fatalf("unexpected non-matched sets: %+v", answer)
	}

	// "If the command had been `npm test`, the `if` check would fail" — but an
	// if pre-filter is a permission rule over the tool's input, not over the
	// tool name, so the matcher answer for a different tool is a non-match
	// rather than a verdict about the if condition.
	other := AnswerEvent(ProviderClaude, claudeExpected(), []ObservedSource{source}, "PreToolUse", "Write")
	if !other.Answerable {
		t.Fatalf("answer should be answerable: %+v", other)
	}
	if len(other.Matched) != 0 {
		t.Fatalf("matched = %d for a different tool, want 0", len(other.Matched))
	}
	if len(other.NotMatched) != 1 {
		t.Fatalf("not matched = %d, want 1: %+v", len(other.NotMatched), other.NotMatched)
	}
}

// Codex's documented examples for a tool-name matcher, turned into cases
// directly (https://developers.openai.com/codex/hooks, "Matcher patterns":
// "Bash", "^apply_patch$", "Edit|Write", "mcp__filesystem__.*").
func TestAnswerEventMatchesCodexDocMatcherExamples(t *testing.T) {
	tests := []struct {
		matcher string
		value   string
		want    bool
	}{
		{matcher: "Bash", value: "Bash", want: true},
		// Codex's matcher is always a regex, so an unanchored "Bash" also
		// matches a longer name. That is the documented difference from
		// Claude's exact-match path and must not be flattened.
		{matcher: "Bash", value: "BashOutput", want: true},
		{matcher: "^apply_patch$", value: "apply_patch", want: true},
		{matcher: "^apply_patch$", value: "apply_patch_v2"},
		{matcher: "Edit|Write", value: "Write", want: true},
		{matcher: "mcp__filesystem__.*", value: "mcp__filesystem__read_file", want: true},
		{matcher: "mcp__filesystem__.*", value: "mcp__memory__read", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.matcher+"/"+tt.value, func(t *testing.T) {
			source := projectedSource("user", "json",
				`{"PreToolUse":[{"matcher":"`+tt.matcher+`","hooks":[{"type":"command","command":"check.sh"}]}]}`, `{}`)
			answer := AnswerEvent(ProviderCodex, codexExpected(), []ObservedSource{source}, "PreToolUse", tt.value)
			if !answer.Answerable {
				t.Fatalf("answer should be answerable: %+v", answer)
			}
			if got := len(answer.Matched) == 1; got != tt.want {
				t.Fatalf("matched = %v, want %v (%+v)", got, tt.want, answer)
			}
		})
	}
}

// "Any configured `matcher` is ignored for this event" — so the value the
// reader supplies cannot change the answer, and every live hook is matched.
func TestAnswerEventIgnoresMatcherWhereTheProviderDoes(t *testing.T) {
	source := projectedSource("user", "json",
		`{"UserPromptSubmit":[{"matcher":"never-matches-anything","hooks":[{"type":"command","command":"log.sh"}]}]}`, `{}`)
	answer := AnswerEvent(ProviderCodex, codexExpected(), []ObservedSource{source}, "UserPromptSubmit", "anything")
	if !answer.Answerable || len(answer.Matched) != 1 {
		t.Fatalf("an ignored matcher must still match: %+v", answer)
	}
	if answer.ValueRole != ValueRoleIgnored {
		t.Fatalf("value role = %q, want ignored", answer.ValueRole)
	}
	if answer.Matched[0].MatcherKind != string(MatcherIgnored) {
		t.Fatalf("matcher kind = %q, want ignored", answer.Matched[0].MatcherKind)
	}
}

// The defect class this view exists to avoid. A matcher Go's RE2 refuses
// makes the whole event unanswerable; the answer carries the compiler's own
// message and every set stays empty, because "nothing runs" would be a
// statement about the host that Multica never established.
func TestAnswerEventReportsUnevaluableMatcherInsteadOfAnEmptyAnswer(t *testing.T) {
	source := projectedSource("user", "json",
		`{"PreToolUse":[`+
			`{"matcher":"^(?!Notebook).*","hooks":[{"type":"command","command":"guard.sh"}]},`+
			`{"matcher":"Bash","hooks":[{"type":"command","command":"other.sh"}]}]}`, `{}`)
	answer := AnswerEvent(ProviderClaude, claudeExpected(), []ObservedSource{source}, "PreToolUse", "Bash")

	if answer.Answerable {
		t.Fatal("an unevaluable matcher must make the event unanswerable")
	}
	if answer.Error == "" {
		t.Fatal("an unanswerable event must carry the evaluator's message")
	}
	if len(answer.Unevaluable) != 1 {
		t.Fatalf("unevaluable = %d, want 1: %+v", len(answer.Unevaluable), answer.Unevaluable)
	}
	if answer.Unevaluable[0].Matcher != "^(?!Notebook).*" {
		t.Fatalf("unevaluable matcher = %q, want the offending pattern", answer.Unevaluable[0].Matcher)
	}
	// The compiler's own words, quoted rather than paraphrased: RE2 rejects
	// the lookahead as unsupported Perl syntax, and that is what a reader
	// needs to see to recognise the construct it refused.
	if !strings.Contains(answer.Unevaluable[0].Error, "invalid or unsupported Perl syntax: `(?!`") {
		t.Fatalf("unevaluable error = %q, want the compiler's own message", answer.Unevaluable[0].Error)
	}
	// The sibling hook whose matcher does match "Bash" is NOT reported as
	// running, and the entry that could not be evaluated is NOT reported as
	// not running. Neither statement was established.
	if len(answer.Matched) != 0 || len(answer.NotMatched) != 0 ||
		len(answer.NeverRuns) != 0 || len(answer.ConfigurationExcluded) != 0 {
		t.Fatalf("an unanswerable event must not partition anything: %+v", answer)
	}
}

// A parked hook's matcher is never evaluated, so an unevaluable one on a
// parked entry does not cost the event its answer. Answerable is the
// authoritative flag, which is why it is separate from Unevaluable.
func TestAnswerEventStaysAnswerableWhenOnlyAParkedMatcherIsUnevaluable(t *testing.T) {
	hooks := `{"PreToolUse":[` +
		`{"matcher":"^(?!Notebook).*","hooks":[{"type":"command","command":"guard.sh"}]},` +
		`{"matcher":"Bash","hooks":[{"type":"command","command":"other.sh"}]}]}`
	source := projectedSource("user", "json", hooks, `{}`)
	live := AnswerEvent(ProviderClaude, claudeExpected(), []ObservedSource{source}, "PreToolUse", "Bash")
	if live.Answerable {
		t.Fatal("precondition: the live form of this fixture must be unanswerable")
	}

	// Park the offending entry through the sidecar the resolution layer reads.
	parkedID := parkedHookIDFor(t, hooks, "^(?!Notebook).*")
	parked := projectedSource("user", "json", hooks,
		`{"`+parkedID+`":{"event":"PreToolUse","matcher":"^(?!Notebook).*","handler":{"type":"command","command":"guard.sh"},"parked_at":"2026-09-12T00:00:00Z"}}`)
	answer := AnswerEvent(ProviderClaude, claudeExpected(), []ObservedSource{parked}, "PreToolUse", "Bash")

	if !answer.Answerable {
		t.Fatalf("a parked unevaluable matcher must not block the answer: %+v", answer)
	}
	if len(answer.Matched) != 1 {
		t.Fatalf("matched = %d, want 1: %+v", len(answer.Matched), answer.Matched)
	}
	if len(answer.ConfigurationExcluded) != 1 {
		t.Fatalf("excluded = %d, want 1: %+v", len(answer.ConfigurationExcluded), answer.ConfigurationExcluded)
	}
	// Both axes on the excluded row, uncollapsed: parked configuration, and
	// the provider's own verdict left untouched beside it.
	if answer.ConfigurationExcluded[0].Configuration != string(ConfigurationParked) {
		t.Fatalf("configuration = %q, want parked", answer.ConfigurationExcluded[0].Configuration)
	}
	if answer.ConfigurationExcluded[0].Effectiveness == "" {
		t.Fatal("an excluded entry must keep its own provider verdict")
	}
}

// "If you define the same handler in more than one settings file, it runs
// once. A plugin's or skill's copy of the same handler stays separate."
// (https://code.claude.com/docs/en/hooks, "Configuration"). The answer shows
// one row carrying both sources — a deduplication, not a precedence.
func TestAnswerEventCollapsesIdenticalClaudeSettingsHandlerOnce(t *testing.T) {
	handler := `{"type":"command","command":"format.sh"}`
	hooks := `{"PreToolUse":[{"matcher":"Bash","hooks":[` + handler + `]}]}`
	answer := AnswerEvent(ProviderClaude, claudeExpected(), []ObservedSource{
		projectedSource("user", "json", hooks, `{}`),
		projectedSource("project", "json", hooks, `{}`),
	}, "PreToolUse", "Bash")

	if !answer.Answerable {
		t.Fatalf("answer should be answerable: %+v", answer)
	}
	if len(answer.Matched) != 1 {
		t.Fatalf("matched = %d, want 1 collapsed row: %+v", len(answer.Matched), answer.Matched)
	}
	if len(answer.Matched[0].Sources) != 2 {
		t.Fatalf("sources = %d, want both retained: %+v", len(answer.Matched[0].Sources), answer.Matched[0].Sources)
	}
	// Neither source is marked as the winner: both are simply present.
	scopes := map[string]bool{}
	for _, ref := range answer.Matched[0].Sources {
		scopes[ref.Scope] = true
	}
	if !scopes["user"] || !scopes["project"] {
		t.Fatalf("both scopes must be retained: %+v", answer.Matched[0].Sources)
	}
}

// The answer endpoint, rather than MergeMatched alone, preserves one member
// triple for every configured entry that reached a row. Differing matchers
// still collapse across settings; plugin and skill copies remain separate.
func TestAnswerEventCarriesEveryCollapsedMemberIdentity(t *testing.T) {
	handler := `{"type":"command","command":"format.sh"}`
	settings := func(scope, matcher string) (SourceRef, ObservedSource) {
		ref := SourceRef{Scope: scope, Format: "json"}
		hooks := `{"PreToolUse":[{"matcher":"` + matcher + `","hooks":[` + handler + `]}]}`
		return ref, foundSource(ref, hooks)
	}
	extension := func(kind SourceKind, name string) (SourceRef, ObservedSource) {
		ref := SourceRef{Kind: kind, Name: name, Format: "json"}
		hooks := `{"PreToolUse":[{"matcher":"Bash","hooks":[` + handler + `]}]}`
		return ref, foundSource(ref, hooks)
	}

	userBashRef, userBash := settings("user", "Bash")
	projectBashRef, projectBash := settings("project", "Bash")
	projectWideRef, projectWide := settings("project", "Bash|Read")
	pluginRef, plugin := extension(SourcePlugin, "formatter")
	skillRef, skill := extension(SourceSkill, "review")

	tests := []struct {
		name      string
		expected  []SourceRef
		observed  []ObservedSource
		wantRows  int
		wantInput int
	}{
		{
			name: "settings with differing matchers", expected: []SourceRef{userBashRef, projectWideRef},
			observed: []ObservedSource{userBash, projectWide}, wantRows: 1, wantInput: 2,
		},
		{
			name: "settings with identical matchers", expected: []SourceRef{userBashRef, projectBashRef},
			observed: []ObservedSource{userBash, projectBash}, wantRows: 1, wantInput: 2,
		},
		{
			name: "plugin copy", expected: []SourceRef{userBashRef, pluginRef},
			observed: []ObservedSource{userBash, plugin}, wantRows: 2, wantInput: 2,
		},
		{
			name: "skill copy", expected: []SourceRef{userBashRef, skillRef},
			observed: []ObservedSource{userBash, skill}, wantRows: 2, wantInput: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			answer := AnswerEvent(ProviderClaude, tt.expected, tt.observed, "PreToolUse", "Bash")
			if len(answer.Matched) != tt.wantRows {
				t.Fatalf("matched rows = %d, want %d: %+v", len(answer.Matched), tt.wantRows, answer.Matched)
			}
			triples := map[string]struct{}{}
			memberCount := 0
			for _, row := range answer.Matched {
				if len(row.Members) == 0 {
					t.Fatalf("row has no members: %+v", row)
				}
				for _, member := range row.Members {
					key := member.Source.Scope + "\x00" + member.Source.Format + "\x00" +
						member.Source.Kind + "\x00" + member.Source.Name + "\x00" +
						member.Matcher + "\x00" + member.HookID
					triples[key] = struct{}{}
					memberCount++
				}
			}
			if memberCount != tt.wantInput || len(triples) != tt.wantInput {
				t.Fatalf("member triples = %d distinct / %d total, want %d input entries: %+v",
					len(triples), memberCount, tt.wantInput, answer.Matched)
			}
		})
	}
}

// "Higher-precedence config layers don't replace lower-precedence hooks"
// (https://developers.openai.com/codex/hooks, "Where Codex looks for hooks").
// Codex keeps every source copy, so the same handler in two layers answers as
// two entries, and neither is displaced.
func TestAnswerEventKeepsEveryCodexLayerCopy(t *testing.T) {
	hooks := `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"audit.sh"}]}]}`
	answer := AnswerEvent(ProviderCodex, codexExpected(), []ObservedSource{
		projectedSource("user", "json", hooks, `{}`),
		projectedSource("project", "json", hooks, `{}`),
	}, "PreToolUse", "Bash")

	if len(answer.Matched) != 2 {
		t.Fatalf("matched = %d, want both layer copies: %+v", len(answer.Matched), answer.Matched)
	}
	for _, entry := range answer.Matched {
		if len(entry.Sources) != 1 {
			t.Fatalf("a Codex entry must keep its own single source: %+v", entry.Sources)
		}
	}
}

// A live handler the provider will not act on for this event lands in
// NeverRuns rather than being reported as a non-match: the distinction is
// "the provider skips this" against "your value did not match it".
func TestAnswerEventSeparatesNeverRunsFromNotMatched(t *testing.T) {
	// Claude evaluates the if pre-filter only on tool events, so an if on a
	// non-tool event can never be satisfied.
	source := projectedSource("user", "json",
		`{"Stop":[{"matcher":"","hooks":[`+
			`{"type":"command","command":"never.sh","if":"Bash(git *)"},`+
			`{"type":"command","command":"runs.sh"}]}]}`, `{}`)
	answer := AnswerEvent(ProviderClaude, claudeExpected(), []ObservedSource{source}, "Stop", "anything")

	if !answer.Answerable {
		t.Fatalf("answer should be answerable: %+v", answer)
	}
	if len(answer.NeverRuns) != 1 {
		t.Fatalf("never runs = %d, want 1: %+v", len(answer.NeverRuns), answer.NeverRuns)
	}
	if answer.NeverRuns[0].Effectiveness != string(EffectivenessNeverRuns) {
		t.Fatalf("effectiveness = %q, want never_runs", answer.NeverRuns[0].Effectiveness)
	}
	if len(answer.Matched) != 1 {
		t.Fatalf("matched = %d, want 1: %+v", len(answer.Matched), answer.Matched)
	}
}

// A Codex hook runs only while its exact definition is trusted on the host, so
// a matched entry can still be one Multica cannot promise will run. The trust
// axis stays on the entry instead of moving it out of the matched set.
func TestAnswerEventKeepsCodexTrustOnMatchedEntries(t *testing.T) {
	source := projectedSource("user", "json",
		`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"audit.sh"}]}]}`, `{}`)
	answer := AnswerEvent(ProviderCodex, codexExpected(), []ObservedSource{source}, "PreToolUse", "Bash")

	if len(answer.Matched) != 1 {
		t.Fatalf("matched = %d, want 1: %+v", len(answer.Matched), answer.Matched)
	}
	entry := answer.Matched[0]
	if entry.Trust == string(TrustNotApplicable) {
		t.Fatal("a Codex entry must carry a trust state")
	}
	if entry.Effectiveness != string(EffectivenessTrustUnknown) {
		t.Fatalf("effectiveness = %q, want trust_unknown for an unreviewed Codex hook", entry.Effectiveness)
	}
}

// Claude has no trust model at all, so nothing on a Claude answer may imply
// one. This is the per-provider difference criterion in its smallest form.
func TestAnswerEventLeavesClaudeTrustNotApplicable(t *testing.T) {
	source := projectedSource("user", "json",
		`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"audit.sh"}]}]}`, `{}`)
	answer := AnswerEvent(ProviderClaude, claudeExpected(), []ObservedSource{source}, "PreToolUse", "Bash")
	if len(answer.Matched) != 1 {
		t.Fatalf("matched = %d, want 1", len(answer.Matched))
	}
	if answer.Matched[0].Trust != string(TrustNotApplicable) {
		t.Fatalf("trust = %q, want not_applicable on Claude", answer.Matched[0].Trust)
	}
}

// A snapshot that cannot be resolved is reported as unanswerable, not as an
// event with nothing on it.
func TestAnswerEventReportsResolutionFailureInsteadOfAnEmptyAnswer(t *testing.T) {
	// A parked-hook sidecar entry missing parked_at, the same fixture the
	// projection's own failure case uses.
	source := projectedSource("user", "json", `{}`,
		`{"broken":{"event":"Notification","handler":{"type":"command"}}}`)
	answer := AnswerEvent(ProviderClaude, claudeExpected(), []ObservedSource{source}, "PreToolUse", "Bash")
	if answer.Answerable {
		t.Fatal("an unresolvable snapshot cannot be answerable")
	}
	if answer.Error == "" {
		t.Fatal("an unresolvable snapshot must report why")
	}
}

func TestAnswerEventRejectsProviderWithoutHooks(t *testing.T) {
	answer := AnswerEvent(Provider("gemini"), claudeExpected(), nil, "PreToolUse", "Bash")
	if answer.Answerable || answer.Error == "" {
		t.Fatalf("an unsupported provider must be an unanswerable error: %+v", answer)
	}
}

// The wire shape a client renders. answerable and the four sets are always
// present, so a client never has to interpret an absent field, and an
// unanswerable event arrives with answerable false rather than empty sets
// that read as "nothing runs".
func TestEventAnswerJSONCarriesAnswerabilityAndEverySet(t *testing.T) {
	source := projectedSource("user", "json",
		`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"audit.sh"}]}]}`, `{}`)
	encoded, err := json.Marshal(AnswerEvent(ProviderClaude, claudeExpected(), []ObservedSource{source}, "PreToolUse", "Bash"))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		`"answerable":true`, `"value":"Bash"`, `"value_role":"tool_name"`,
		`"matched":[`, `"members":[`, `"occurrence":0`, `"not_matched":[`,
		`"never_runs":[`, `"configuration_excluded":[`,
	} {
		if !strings.Contains(string(encoded), field) {
			t.Fatalf("answer JSON missing %s: %s", field, encoded)
		}
	}
}

// The hook id the parked sidecar is keyed by, taken from the resolution layer
// itself rather than recomputed here — a second hashing implementation in a
// test would be a second thing to get wrong.
func parkedHookIDFor(t *testing.T, hooks, matcher string) string {
	t.Helper()
	source := projectedSource("user", "json", hooks, `{}`)
	resolution, err := Resolve(ProviderClaude, claudeExpected(), []ObservedSource{source})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range resolution.entries {
		if entry.Matcher == matcher {
			return entry.HookID
		}
	}
	t.Fatalf("no entry with matcher %q", matcher)
	return ""
}

// The role caption has to be available before any value exists, so it travels
// with the projection too. Asking for an answer in order to learn what value
// to supply would be backwards.
func TestProjectionCarriesAValueRolePerConfiguredEvent(t *testing.T) {
	source := projectedSource("user", "json",
		`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"a.sh"}]}],`+
			`"UserPromptSubmit":[{"matcher":"","hooks":[{"type":"command","command":"b.sh"}]}]}`, `{}`)
	projection := Project(ProviderClaude, claudeExpected(), []ObservedSource{source})

	if got := projection.EventValueRoles["PreToolUse"]; got != string(ValueRoleToolName) {
		t.Fatalf("PreToolUse role = %q, want tool_name", got)
	}
	if got := projection.EventValueRoles["UserPromptSubmit"]; got != string(ValueRoleIgnored) {
		t.Fatalf("UserPromptSubmit role = %q, want ignored", got)
	}
	// Only the events the projection actually holds entries for.
	if _, ok := projection.EventValueRoles["SessionStart"]; ok {
		t.Fatal("an unconfigured event must not appear in the role map")
	}
}

// R3. A hook in an answer set, or a member of one, with no run state is an
// inconsistency between the resolution and the answer. It must surface as the
// event's own failure and must never remove the row: a hook that genuinely
// runs, silently absent from the answer, is the one failure mode this surface
// cannot have, because a reader who cannot see the row cannot know it is
// missing. No input reaches this state — AnswerEvent inserts every state into
// byHookID unconditionally — so the state is constructed directly here, which
// is also why the guard stays rather than being deleted as unreachable.
func TestAnswerEntriesSurfacesMissingRunStateInsteadOfDroppingTheRow(t *testing.T) {
	handler := json.RawMessage(`{"command":"format.sh","type":"command"}`)
	userRef := SourceRef{Scope: "user", Format: "json", Kind: SourceSettings}
	projectRef := SourceRef{Scope: "project", Format: "json", Kind: SourceSettings}
	member := func(id string, ref SourceRef) ResolvedHookMember {
		return ResolvedHookMember{HookID: id, Matcher: "Bash", Source: ref}
	}
	// One collapsed row standing for two configured entries, exactly as
	// MergeMatched produces: the survivor's identity, both members' identities.
	merged := ResolvedHook{
		HookID: "user-hook", Event: "PreToolUse", Matcher: "Bash", Handler: handler,
		Sources: []SourceRef{userRef, projectRef},
		Members: []ResolvedHookMember{member("user-hook", userRef), member("project-hook", projectRef)},
	}
	state := func(id string, ref SourceRef) RunState {
		return RunState{
			Hook: ResolvedHook{
				HookID: id, Event: "PreToolUse", Matcher: "Bash", Handler: handler,
				Sources: []SourceRef{ref}, Members: []ResolvedHookMember{member(id, ref)},
			},
			Configuration: ConfigurationLive,
			Effectiveness: EffectivenessWillRun,
			Trust:         TrustNotApplicable,
		}
	}
	complete := map[string]RunState{
		"user-hook":    state("user-hook", userRef),
		"project-hook": state("project-hook", projectRef),
	}
	missingMember := map[string]RunState{"user-hook": state("user-hook", userRef)}
	missingHook := map[string]RunState{"project-hook": state("project-hook", projectRef)}

	// The row is real: with both states present it projects, carrying both
	// member identities. Without this the assertions below would pass on a
	// row that was never producible in the first place.
	entries, err := answerEntries(ProviderClaude, complete, []ResolvedHook{merged})
	if err != nil {
		t.Fatalf("consistent states must project: %v", err)
	}
	if len(entries) != 1 || len(entries[0].Members) != 2 {
		t.Fatalf("want 1 row carrying 2 members, got %+v", entries)
	}

	for _, tt := range []struct {
		name      string
		byHookID  map[string]RunState
		wantInErr string
	}{
		{name: "member state missing", byHookID: missingMember, wantInErr: "project-hook"},
		{name: "row state missing", byHookID: missingHook, wantInErr: "user-hook"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			entries, err := answerEntries(ProviderClaude, tt.byHookID, []ResolvedHook{merged})
			if err == nil {
				t.Fatalf("want an error, got %d entries: %+v", len(entries), entries)
			}
			if !errors.Is(err, errMissingRunState) {
				t.Fatalf("want errMissingRunState, got %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantInErr) {
				t.Fatalf("error must name the hook %q: %v", tt.wantInErr, err)
			}
			// Not a partial row either: a half-projected row published beside
			// the error would still be a claim Multica cannot support.
			if len(entries) != 0 {
				t.Fatalf("no row may be emitted alongside the error: %+v", entries)
			}

			// And the caller's channel is the failure the screen renders.
			answer := unansweredEvent(EventAnswer{
				Provider: string(ProviderClaude), Event: "PreToolUse", Value: "Bash",
				Answerable: true,
				Matched:    entriesFor(merged), NotMatched: entriesFor(merged),
				NeverRuns: entriesFor(merged), ConfigurationExcluded: entriesFor(merged),
			}, err)
			if answer.Answerable {
				t.Fatal("an inconsistency must leave the event unanswerable")
			}
			if !strings.Contains(answer.Error, tt.wantInErr) {
				t.Fatalf("answer.Error must name the inconsistency: %q", answer.Error)
			}
			for name, set := range map[string][]ProjectedEntry{
				"matched": answer.Matched, "not_matched": answer.NotMatched,
				"never_runs": answer.NeverRuns, "configuration_excluded": answer.ConfigurationExcluded,
			} {
				if len(set) != 0 {
					t.Fatalf("%s must be empty on an unanswerable event: %+v", name, set)
				}
			}
		})
	}
}

// entriesFor is a non-empty set to prove unansweredEvent empties it rather
// than relying on the set having been empty already.
func entriesFor(hook ResolvedHook) []ProjectedEntry {
	return []ProjectedEntry{{HookID: hook.HookID, Event: hook.Event}}
}
