package runtimehooks

import (
	"encoding/json"
	"fmt"
	"testing"
)

func claudeExpected() []SourceRef {
	return []SourceRef{
		{Scope: "user", Format: "json"},
		{Scope: "project", Format: "json"},
		{Scope: "local", Format: "json"},
	}
}

func codexExpected() []SourceRef {
	return []SourceRef{
		{Scope: "user", Format: "json"},
		{Scope: "user", Format: "toml"},
		{Scope: "project", Format: "json"},
		{Scope: "project", Format: "toml"},
	}
}

// Builds on the package's existing foundSource helper, adding the
// disabled_hooks sidecar the projection reads for parked and disabled state.
func projectedSource(scope, format, hooks, disabled string) ObservedSource {
	source := foundSource(SourceRef{Scope: scope, Format: format}, hooks)
	source.DisabledHooks = json.RawMessage(disabled)
	return source
}

// The Codex state key is built from the observed source path, which
// foundSource derives as "<scope><name>.<format>".
func codexStateKeyFor(scope, format, event string, group, handler int) string {
	return fmt.Sprintf("%s.%s:%s:%d:%d", scope, format, snakeCase(event), group, handler)
}

func findProjected(t *testing.T, projection Projection, event, matcher string) ProjectedEntry {
	t.Helper()
	for _, entry := range projection.Entries {
		if entry.Event == event && entry.Matcher == matcher {
			return entry
		}
	}
	t.Fatalf("no projected entry for %s/%q in %+v", event, matcher, projection.Entries)
	return ProjectedEntry{}
}

// A parked, matcher-ineligible hook is the case that proves the two axes are
// independent: unparking it still would not make it run, and one collapsed
// state cannot say that.
func TestProjectKeepsConfigurationAndEffectivenessIndependent(t *testing.T) {
	hooks := `{"Notification":[{"matcher":"","hooks":[{"type":"command","command":"ping","if":"Bash(git *)"}]}]}`
	parked := `{"parked-1":{"event":"Notification","matcher":"","handler":{"command":"ping","if":"Bash(git *)","type":"command"},"parked_at":"2026-09-10T10:00:00Z"}}`
	projection := Project(ProviderClaude, claudeExpected(), []ObservedSource{
		projectedSource("user", "json", hooks, parked),
	})
	if projection.Error != "" {
		t.Fatalf("unexpected projection error: %s", projection.Error)
	}
	entry := findProjected(t, projection, "Notification", "")
	if entry.Configuration != string(ConfigurationParked) {
		t.Fatalf("configuration = %q, want parked", entry.Configuration)
	}
	if entry.ParkedAt != "2026-09-10T10:00:00Z" {
		t.Fatalf("parked_at = %q, want the recorded timestamp", entry.ParkedAt)
	}
	if entry.Effectiveness != string(EffectivenessNeverRuns) {
		t.Fatalf("effectiveness = %q, want never_runs", entry.Effectiveness)
	}
	if entry.NeverRunsReason != string(NeverRunsMatcherIneligible) {
		t.Fatalf("never_runs_reason = %q, want matcher_ineligible", entry.NeverRunsReason)
	}
	if entry.HandlerType != "command" {
		t.Fatalf("handler_type = %q, want command", entry.HandlerType)
	}
	if len(entry.Sources) != 1 || entry.Sources[0].Scope != "user" || entry.Sources[0].Kind != string(SourceSettings) {
		t.Fatalf("sources = %+v, want one settings source in user scope", entry.Sources)
	}
}

// Codex's two user-scope sources are distinct sources in one scope, and both
// are live. Nothing in the projection may mark either as displaced.
func TestProjectKeepsBothCodexUserSourcesLive(t *testing.T) {
	hooks := `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"json-side"}]}]}`
	inline := `{"PreToolUse":[{"matcher":"apply_patch","hooks":[{"type":"command","command":"toml-side"}]}]}`
	projection := Project(ProviderCodex, codexExpected(), []ObservedSource{
		projectedSource("user", "json", hooks, `{}`),
		projectedSource("user", "toml", inline, `{}`),
	})
	if projection.Error != "" {
		t.Fatalf("unexpected projection error: %s", projection.Error)
	}
	if len(projection.Entries) != 2 {
		t.Fatalf("entries = %d, want both Codex user sources represented", len(projection.Entries))
	}
	formats := map[string]string{}
	for _, entry := range projection.Entries {
		if entry.Configuration != string(ConfigurationLive) {
			t.Fatalf("configuration = %q for %q, want live", entry.Configuration, entry.Matcher)
		}
		// Codex trust is unknowable from a file read, so effectiveness stays
		// trust_unknown rather than claiming the hook runs.
		if entry.Effectiveness != string(EffectivenessTrustUnknown) {
			t.Fatalf("effectiveness = %q for %q, want trust_unknown", entry.Effectiveness, entry.Matcher)
		}
		if entry.Trust != string(TrustPendingReview) {
			t.Fatalf("trust = %q for %q, want pending_review", entry.Trust, entry.Matcher)
		}
		if entry.MatcherKind != string(MatcherRegex) {
			t.Fatalf("matcher_kind = %q for %q, want regex — Codex has no exact path", entry.MatcherKind, entry.Matcher)
		}
		formats[entry.Sources[0].Format] = entry.Matcher
	}
	if len(formats) != 2 {
		t.Fatalf("formats = %+v, want one entry from json and one from toml", formats)
	}
}

// A reviewed Codex hook carries the caveat verbatim: presence in the state
// record proves a review happened at snapshot time and nothing more.
func TestProjectAttachesCodexTrustCaveat(t *testing.T) {
	hooks := `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"guard"}]}]}`
	state := fmt.Sprintf(`{"state":{%q:{"enabled":true}}}`,
		codexStateKeyFor("user", "json", "PreToolUse", 0, 0))
	projection := Project(ProviderCodex, codexExpected(), []ObservedSource{
		projectedSource("user", "json", hooks, state),
	})
	if projection.Error != "" {
		t.Fatalf("unexpected projection error: %s", projection.Error)
	}
	entry := findProjected(t, projection, "PreToolUse", "Bash")
	if entry.Trust != string(TrustTrustedAtSnapshot) {
		t.Fatalf("trust = %q, want trusted_as_of_snapshot", entry.Trust)
	}
	if entry.TrustCaveat != CodexTrustSnapshotCaveat {
		t.Fatalf("trust_caveat = %q, want the snapshot caveat", entry.TrustCaveat)
	}
	if entry.Effectiveness != string(EffectivenessTrustUnknown) {
		t.Fatalf("effectiveness = %q: a reviewed hook is still not a confirmed one", entry.Effectiveness)
	}
}

// A Codex hook switched off in /hooks is a configuration change, and it must
// not be reported as an effectiveness verdict.
func TestProjectReportsCodexDisabledAsConfiguration(t *testing.T) {
	hooks := `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"guard"}]}]}`
	state := fmt.Sprintf(`{"state":{%q:{"enabled":false}}}`,
		codexStateKeyFor("user", "json", "PreToolUse", 0, 0))
	projection := Project(ProviderCodex, codexExpected(), []ObservedSource{
		projectedSource("user", "json", hooks, state),
	})
	entry := findProjected(t, projection, "PreToolUse", "Bash")
	if entry.Configuration != string(ConfigurationDisabled) {
		t.Fatalf("configuration = %q, want disabled", entry.Configuration)
	}
	if entry.Trust != string(TrustDisabled) {
		t.Fatalf("trust = %q, want disabled", entry.Trust)
	}
	if entry.NeverRunsReason != "" {
		t.Fatalf("never_runs_reason = %q: being switched off is not a provider verdict", entry.NeverRunsReason)
	}
}

// The escalating case: a matcher Go's RE2 cannot compile must be reported as
// unevaluable with the compiler's message, not folded into a normal regex and
// not turned into a "never runs" verdict Multica has not established.
func TestProjectReportsUnevaluableMatcher(t *testing.T) {
	hooks := `{"PreToolUse":[{"matcher":"^(?!Notebook).*","hooks":[{"type":"command","command":"guard"}]}]}`
	projection := Project(ProviderClaude, claudeExpected(), []ObservedSource{
		projectedSource("user", "json", hooks, `{}`),
	})
	entry := findProjected(t, projection, "PreToolUse", "^(?!Notebook).*")
	if entry.MatcherKind != string(MatcherUnevaluable) || entry.MatcherError == "" {
		t.Fatalf("matcher = %q/%q, want unevaluable with a message", entry.MatcherKind, entry.MatcherError)
	}
	if entry.Effectiveness != string(EffectivenessWillRun) {
		t.Fatalf("effectiveness = %q: an unevaluable matcher is Multica's limit, not a verdict", entry.Effectiveness)
	}
}

// A malformed source must not be reported as "no hooks": the projection says
// it could not resolve, and the caller still has the per-source states.
func TestProjectSurfacesFailureInsteadOfAnEmptyList(t *testing.T) {
	projection := Project(ProviderClaude, claudeExpected(), []ObservedSource{
		projectedSource("user", "json", `{}`,
			`{"broken":{"event":"Notification","handler":{"type":"command"}}}`),
	})
	if projection.Error == "" {
		t.Fatal("a parked hook missing parked_at must project an error")
	}
	if len(projection.Entries) != 0 {
		t.Fatalf("entries = %+v, want none alongside the error", projection.Entries)
	}
}

func TestProjectRejectsProviderWithoutHooks(t *testing.T) {
	projection := Project(Provider("gemini"), nil, nil)
	if projection.Error == "" {
		t.Fatal("a provider with no hook surface must project an error")
	}
	if projection.Entries == nil {
		t.Fatal("Entries must be a non-nil empty slice so a client never sees null")
	}
}

// The daemon omits project scope for both providers today, so those sources
// are unobserved by design. Resolve keeps them as expected-but-not-observed,
// which is what lets the UI render "not checked" instead of "none".
func TestProjectToleratesOmittedProjectScopes(t *testing.T) {
	projection := Project(ProviderClaude, claudeExpected(), []ObservedSource{
		projectedSource("user", "json", `{}`, `{}`),
	})
	if projection.Error != "" {
		t.Fatalf("unexpected projection error: %s", projection.Error)
	}
	if len(projection.Entries) != 0 {
		t.Fatalf("entries = %+v, want none from an empty user source", projection.Entries)
	}
}

// An unrecognized event key keeps the rest of the snapshot resolvable and
// travels outward so the row is not silently dropped.
func TestProjectReportsUnrecognizedKeys(t *testing.T) {
	hooks := `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"ok"}]}],"Nonsense":"not-a-group"}`
	projection := Project(ProviderClaude, claudeExpected(), []ObservedSource{
		projectedSource("user", "json", hooks, `{}`),
	})
	if projection.Error != "" {
		t.Fatalf("unexpected projection error: %s", projection.Error)
	}
	if len(projection.Entries) != 1 {
		t.Fatalf("entries = %d, want the recognized key to still resolve", len(projection.Entries))
	}
	if len(projection.UnrecognizedKeys) != 1 || projection.UnrecognizedKeys[0].Key != "Nonsense" {
		t.Fatalf("unrecognized_keys = %+v, want the Nonsense key", projection.UnrecognizedKeys)
	}
}

// A source outside the expected set would make Resolve reject the whole call.
// Skipping it keeps every other scope readable.
func TestProjectSkipsUnexpectedSources(t *testing.T) {
	hooks := `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"ok"}]}]}`
	projection := Project(ProviderClaude, claudeExpected(), []ObservedSource{
		projectedSource("user", "json", hooks, `{}`),
		projectedSource("session", "json", hooks, `{}`),
	})
	if projection.Error != "" {
		t.Fatalf("unexpected projection error: %s", projection.Error)
	}
	if len(projection.Entries) != 1 {
		t.Fatalf("entries = %d, want only the expected user scope", len(projection.Entries))
	}
}

// Deterministic ordering, keyed on identity rather than precedence: neither
// provider assigns an execution order and the list must not imply one.
func TestProjectOrdersEntriesStably(t *testing.T) {
	hooks := `{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"b"}]}],` +
		`"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"a"}]}]}`
	first := Project(ProviderClaude, claudeExpected(), []ObservedSource{
		projectedSource("user", "json", hooks, `{}`),
	})
	second := Project(ProviderClaude, claudeExpected(), []ObservedSource{
		projectedSource("user", "json", hooks, `{}`),
	})
	if len(first.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(first.Entries))
	}
	if first.Entries[0].Event != "PreToolUse" || first.Entries[1].Event != "Stop" {
		t.Fatalf("events = %q, %q, want them sorted by name", first.Entries[0].Event, first.Entries[1].Event)
	}
	for i := range first.Entries {
		if first.Entries[i].HookID != second.Entries[i].HookID {
			t.Fatalf("entry %d differs between projections of the same snapshot", i)
		}
	}
}

// The projection is the client contract, so its JSON must carry both axes as
// separate fields. A single merged "state" key would be the defect.
func TestProjectionJSONCarriesBothAxes(t *testing.T) {
	hooks := `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"guard"}]}]}`
	projection := Project(ProviderClaude, claudeExpected(), []ObservedSource{
		projectedSource("user", "json", hooks, `{}`),
	})
	raw, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Entries []map[string]json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(decoded.Entries))
	}
	for _, field := range []string{"configuration", "effectiveness", "trust", "matcher_kind", "sources"} {
		if _, ok := decoded.Entries[0][field]; !ok {
			t.Fatalf("entry JSON is missing %q: %s", field, raw)
		}
	}
}
