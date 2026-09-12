package runtimehooks

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestResolveSkipsAndReportsUnrecognizedHookKeys(t *testing.T) {
	source := foundSource(SourceRef{Scope: "user", Format: "json"}, `{
		"state":{"observed":"reserved provider data"},
		"Stop":[{"matcher":"","hooks":[{"type":"command","command":"finish"}]}]
	}`)
	resolution, err := Resolve(ProviderCodex, []SourceRef{source.Source}, []ObservedSource{source})
	if err != nil {
		t.Fatalf("one unrecognized key discarded the resolution: %v", err)
	}
	if got := resolution.EntriesForEvent("Stop"); len(got) != 1 {
		t.Fatalf("recognized entries = %#v, want Stop handler", got)
	}
	if len(resolution.UnrecognizedKeys) != 1 || resolution.UnrecognizedKeys[0].Key != "state" {
		t.Fatalf("unrecognized keys = %#v, want reported state key", resolution.UnrecognizedKeys)
	}
}

func TestResolvedHookIdentityAndMatcher(t *testing.T) {
	ref := SourceRef{Scope: "user", Format: "json"}
	first := foundSource(ref, `{
		"Stop":[
			{"matcher":"first","hooks":[{"type":"command","command":"one"},{"type":"command","command":"two"}]},
			{"hooks":[{"type":"command","command":"three"}]}
		],
		"SessionEnd":[{"hooks":[{"type":"command","command":"end"}]}]
	}`)
	second := foundSource(ref, `{
		"SessionEnd":[{"hooks":[{"type":"command","command":"end"}]}],
		"Stop":[
			{"hooks":[{"type":"command","command":"three"}]},
			{"matcher":"first","hooks":[{"type":"command","command":"two"},{"type":"command","command":"one"}]}
		]
	}`)

	left := mustResolve(t, ProviderClaude, []ObservedSource{first})
	right := mustResolve(t, ProviderClaude, []ObservedSource{second})
	leftByCommand := entriesByCommand(t, append(left.EntriesForEvent("Stop"), left.EntriesForEvent("SessionEnd")...))
	rightByCommand := entriesByCommand(t, append(right.EntriesForEvent("Stop"), right.EntriesForEvent("SessionEnd")...))
	for command, entry := range leftByCommand {
		if got := rightByCommand[command].HookID; got != entry.HookID {
			t.Errorf("%s HookID changed after distinct-entry reorder: %q != %q", command, entry.HookID, got)
		}
		if !strings.HasPrefix(entry.HookID, "sha256:") {
			t.Errorf("%s HookID = %q, want sha256 identity", command, entry.HookID)
		}
	}
	for command, wantOccurrence := range map[string]int{"one": 0, "two": 0, "three": 0} {
		if got := leftByCommand[command].Occurrence; got != wantOccurrence {
			t.Errorf("%s occurrence = %d, want %d", command, got, wantOccurrence)
		}
	}
	if leftByCommand["one"].Matcher != "first" || leftByCommand["two"].Matcher != "first" {
		t.Fatalf("explicit matcher was not preserved: %#v", left.EntriesForEvent("Stop"))
	}
	if leftByCommand["three"].Matcher != "" {
		t.Fatalf("omitted matcher = %q, want empty string", leftByCommand["three"].Matcher)
	}

	changed := foundSource(ref, `{"Stop":[{"matcher":"first","hooks":[{"type":"command","command":"edited"}]}]}`)
	changedResolution := mustResolve(t, ProviderClaude, []ObservedSource{changed})
	if leftByCommand["one"].HookID == changedResolution.EntriesForEvent("Stop")[0].HookID {
		t.Fatal("HookID did not change when canonical handler content changed")
	}

}

func TestHookIDKeepsParkedMatcherStableAcrossGroupReorder(t *testing.T) {
	ref := SourceRef{Scope: "local", Format: "json"}
	path, hash := "/repo/.claude/settings.local.json", "settings-hash"
	before := ObservedSource{
		Source: ref, SourcePath: &path, ContentHash: &hash,
		Hooks: raw(`{"PreToolUse":[
			{"matcher":"Bash","hooks":[{"type":"command","command":"./check.sh"}]},
			{"matcher":"Write","hooks":[{"type":"command","command":"./check.sh"}]}
		]}`),
	}
	beforeResolution := mustResolve(t, ProviderClaude, []ObservedSource{before})
	beforeByMatcher := entriesByMatcher(beforeResolution.EntriesForEvent("PreToolUse"))
	bashID := beforeByMatcher["Bash"].HookID

	parked := fmt.Sprintf(`{%q:{"event":"PreToolUse","matcher":"Bash","handler":{"type":"command","command":"./check.sh"},"parked_at":"2026-09-12T08:00:00Z"}}`, bashID)
	after := ObservedSource{
		Source: ref, SourcePath: &path, ContentHash: &hash,
		Hooks: raw(`{"PreToolUse":[
			{"matcher":"Write","hooks":[{"type":"command","command":"./check.sh"}]},
			{"matcher":"Bash","hooks":[{"type":"command","command":"./check.sh"}]}
		]}`),
		DisabledHooks: raw(parked),
	}
	afterResolution := mustResolve(t, ProviderClaude, []ObservedSource{after})
	afterEntries := entriesByMatcher(afterResolution.EntriesForEvent("PreToolUse"))
	if afterEntries["Bash"].HookID != beforeByMatcher["Bash"].HookID || afterEntries["Write"].HookID != beforeByMatcher["Write"].HookID {
		t.Fatalf("matcher-bound IDs changed across group reorder: before=%#v after=%#v", beforeByMatcher, afterEntries)
	}
	states, err := afterResolution.RunStates()
	if err != nil {
		t.Fatal(err)
	}
	statesByMatcher := runStatesByMatcher(states)
	if statesByMatcher["Bash"].Configuration != ConfigurationParked {
		t.Fatalf("Bash state = %#v, want parked", statesByMatcher["Bash"])
	}
	if statesByMatcher["Write"].Configuration != ConfigurationLive {
		t.Fatalf("Write state = %#v, want live", statesByMatcher["Write"])
	}
}

func TestHookIDDistinguishesSameMatcherTrueDuplicatesByOccurrence(t *testing.T) {
	source := foundSource(SourceRef{Scope: "user", Format: "json"}, `{"Stop":[
		{"matcher":"same","hooks":[{"type":"command","command":"same"}]},
		{"matcher":"same","hooks":[{"command":"same","type":"command"}]}
	]}`)
	entries := mustResolve(t, ProviderClaude, []ObservedSource{source}).EntriesForEvent("Stop")
	if entries[0].HookID == entries[1].HookID {
		t.Fatal("same-matcher true duplicates received the same HookID")
	}
	if entries[0].Occurrence != 0 || entries[1].Occurrence != 1 {
		t.Fatalf("duplicate occurrences = %d, %d; want 0, 1", entries[0].Occurrence, entries[1].Occurrence)
	}
}

func TestParkedHookIDMatchRequiresMatchingBody(t *testing.T) {
	ref := SourceRef{Scope: "local", Format: "json"}
	path, hash := "/repo/.claude/settings.local.json", "settings-hash"
	hooks := raw(`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./check.sh"}]}]}`)
	base := mustResolve(t, ProviderClaude, []ObservedSource{{
		Source: ref, SourcePath: &path, ContentHash: &hash, Hooks: hooks,
	}})
	hookID := base.EntriesForEvent("PreToolUse")[0].HookID
	tests := []struct {
		name    string
		event   string
		matcher string
		handler string
	}{
		{name: "event drift", event: "PostToolUse", matcher: "Bash", handler: `{"type":"command","command":"./check.sh"}`},
		{name: "matcher drift", event: "PreToolUse", matcher: "Write", handler: `{"type":"command","command":"./check.sh"}`},
		{name: "handler drift", event: "PreToolUse", matcher: "Bash", handler: `{"type":"command","command":"./other.sh"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			disabled := fmt.Sprintf(`{%q:{"event":%q,"matcher":%q,"handler":%s,"parked_at":"2026-09-12T08:00:00Z"}}`, hookID, tt.event, tt.matcher, tt.handler)
			resolution := mustResolve(t, ProviderClaude, []ObservedSource{{
				Source: ref, SourcePath: &path, ContentHash: &hash,
				Hooks: hooks, DisabledHooks: raw(disabled),
			}})
			if _, err := resolution.RunStates(); err == nil || !strings.Contains(err.Error(), "does not match configured hook") {
				t.Fatalf("RunStates error = %v, want fail-safe parked body mismatch", err)
			}
		})
	}
}

func TestClaudePromptHandlerSupportMatchesLiveEventTable(t *testing.T) {
	// https://code.claude.com/docs/en/hooks, "Prompt-based hooks":
	// "Not all events support every hook type." The section lists thirteen
	// all-five events; the remaining twenty documented events exclude prompt.
	supported := []string{
		"PermissionDenied", "PermissionRequest", "PostToolBatch", "PostToolUse",
		"PostToolUseFailure", "PreToolUse", "Stop", "SubagentStop", "TaskCompleted",
		"TaskCreated", "TeammateIdle", "UserPromptExpansion", "UserPromptSubmit",
	}
	unsupported := []string{
		"ConfigChange", "CwdChanged", "DirectoryAdded", "Elicitation", "ElicitationResult",
		"FileChanged", "InstructionsLoaded", "MessageDisplay", "Notification", "PostCompact",
		"PostModelSwitch", "PreCompact", "PreModelSwitch", "SessionEnd", "StopFailure",
		"SubagentStart", "WorktreeCreate", "WorktreeRemove", "SessionStart", "Setup",
	}
	if len(supported)+len(unsupported) != 33 {
		t.Fatal("Claude live event table fixture must cover all 33 events")
	}
	for _, event := range supported {
		t.Run(event+" supported", func(t *testing.T) {
			state := onlyRunState(t, resolutionForHandler(t, ProviderClaude, event, `{"type":"prompt","prompt":"review"}`))
			if state.Effectiveness != EffectivenessWillRun || state.NeverReason != "" {
				t.Fatalf("state = %#v, want effective prompt handler", state)
			}
		})
	}
	for _, event := range unsupported {
		t.Run(event+" unsupported", func(t *testing.T) {
			state := onlyRunState(t, resolutionForHandler(t, ProviderClaude, event, `{"type":"prompt","prompt":"review"}`))
			if state.Effectiveness != EffectivenessNeverRuns || state.NeverReason != NeverRunsHandlerUnsupportedEvent {
				t.Fatalf("state = %#v, want event-specific unsupported reason", state)
			}
		})
	}
}

func TestClaudeWorkedPromptExampleIsEffective(t *testing.T) {
	// Verbatim from https://code.claude.com/docs/en/hooks, "Prompt hook
	// configuration": "This `Stop` hook asks the LLM to evaluate whether all
	// tasks are complete before allowing Claude to finish."
	source := foundSource(SourceRef{Scope: "user", Format: "json"}, `{
		"Stop": [
			{
				"hooks": [
					{
						"type": "prompt",
						"prompt": "Evaluate if Claude should stop: $ARGUMENTS. Check if all tasks are complete."
					}
				]
			}
		]
	}`)
	state := onlyRunState(t, mustResolve(t, ProviderClaude, []ObservedSource{source}))
	if state.Effectiveness != EffectivenessWillRun {
		t.Fatalf("worked prompt example state = %#v, want will_run", state)
	}
}

func TestClaudeStopSupportsAllFiveDocumentedHandlerTypes(t *testing.T) {
	// https://code.claude.com/docs/en/hooks, "Prompt-based hooks": Stop is in
	// "Events that support all five hook types (command, http, mcp_tool, prompt,
	// and agent)."
	handlers := []string{
		`{"type":"command","command":"check"}`,
		`{"type":"http","url":"https://example.test/hooks"}`,
		`{"type":"mcp_tool","server":"review","tool":"check"}`,
		`{"type":"prompt","prompt":"check"}`,
		`{"type":"agent","prompt":"check"}`,
	}
	for _, handler := range handlers {
		state := onlyRunState(t, resolutionForHandler(t, ProviderClaude, "Stop", handler))
		if state.Effectiveness != EffectivenessWillRun {
			t.Errorf("handler %s effectiveness = %q, want will_run", handler, state.Effectiveness)
		}
	}
}

func TestClaudeNarrowHandlerTypeCases(t *testing.T) {
	// https://code.claude.com/docs/en/hooks, "Prompt-based hooks":
	// "SessionStart and Setup support command and mcp_tool hooks" and do not
	// support HTTP. "MCP tool hook fields" adds that Setup always skips MCP.
	tests := []struct {
		name    string
		event   string
		handler string
		want    Effectiveness
	}{
		{name: "session start command", event: "SessionStart", handler: `{"type":"command","command":"load"}`, want: EffectivenessWillRun},
		{name: "session start http", event: "SessionStart", handler: `{"type":"http","url":"https://example.test"}`, want: EffectivenessNeverRuns},
		{name: "setup mcp always skipped", event: "Setup", handler: `{"type":"mcp_tool","server":"context","tool":"load"}`, want: EffectivenessNeverRuns},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := onlyRunState(t, resolutionForHandler(t, ProviderClaude, tt.event, tt.handler))
			if state.Effectiveness != tt.want {
				t.Fatalf("effectiveness = %q, want %q", state.Effectiveness, tt.want)
			}
		})
	}
}

func TestNeverRunsExtendsMatcherPartition(t *testing.T) {
	resolution := resolutionForHandler(t, ProviderClaude, "Stop", `{"type":"command","if":"Bash(git *)","command":"check"}`)
	partition, err := resolution.MatchEvent("Stop", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(partition.NeverRuns) != 1 {
		t.Fatalf("matcher partition = %#v, want never-runs handler", partition)
	}
	state := onlyRunState(t, resolution)
	if state.Effectiveness != EffectivenessNeverRuns || state.NeverReason != NeverRunsMatcherIneligible {
		t.Fatalf("run state = %#v, want matcher-ineligible reason", state)
	}
}

func TestCodexUnsupportedHandlerTypesAreNeverRuns(t *testing.T) {
	// https://developers.openai.com/codex/hooks, "Config shape", Notes:
	// "command and mcp_tool handlers are supported. prompt and agent handlers
	// are parsed but skipped."
	for _, typeName := range []string{"prompt", "agent"} {
		t.Run(typeName, func(t *testing.T) {
			state := onlyRunState(t, resolutionForHandler(t, ProviderCodex, "Stop", fmt.Sprintf(`{"type":%q,"prompt":"review"}`, typeName)))
			if state.Effectiveness != EffectivenessNeverRuns || state.NeverReason != NeverRunsHandlerUnsupportedProvider {
				t.Fatalf("state = %#v, want provider-unsupported reason", state)
			}
		})
	}
}

func TestCodexSupportedHandlerTypesAreNotNeverRuns(t *testing.T) {
	// https://developers.openai.com/codex/hooks, "Config shape", Notes:
	// "command and mcp_tool handlers are supported."
	for _, handler := range []string{
		`{"type":"command","command":"check"}`,
		`{"type":"mcp_tool","server":"review","tool":"check"}`,
	} {
		state := onlyRunState(t, resolutionForHandler(t, ProviderCodex, "Stop", handler))
		if state.Effectiveness != EffectivenessTrustUnknown || state.NeverReason != "" {
			t.Errorf("handler %s state = %#v, want supported and pending review", handler, state)
		}
	}
}

func TestCodexTrustStatesAndCrossSourceJoin(t *testing.T) {
	// The hooks handler and trusted_hash below are copied from the real reviewed
	// ~/.codex/hooks.json and ~/.codex/config.toml fixture exercised by the
	// daemon reader. https://developers.openai.com/codex/hooks, "Review and
	// trust hooks": Codex "records trust against the hook's current hash."
	ref := SourceRef{Scope: "user", Format: "json"}
	hooksPath := "/home/marty/.codex/hooks.json"
	configPath := "/home/marty/.codex/config.toml"
	hash := "hooks-json-hash"
	hooks := ObservedSource{
		Source: ref, SourcePath: &hooksPath, ContentHash: &hash,
		Hooks:         raw(`{"PreToolUse":[{"hooks":[{"type":"command","command":"if [ -f '/home/marty/.orca/agent-hooks/codex-hook.sh' ] && [ -r '/home/marty/.orca/agent-hooks/codex-hook.sh' ] && [ -x '/home/marty/.orca/agent-hooks/codex-hook.sh' ]; then /bin/sh '/home/marty/.orca/agent-hooks/codex-hook.sh'; else { command -p cat 2>/dev/null || cat; } >/dev/null 2>&1 || :; fi","timeout":10}]}]}`),
		DisabledHooks: raw(`{}`),
	}
	configRef := SourceRef{Scope: "user", Format: "toml"}
	configHash := "config-hash"

	tests := []struct {
		name          string
		disabledHooks string
		configuration ConfigurationState
		effectiveness Effectiveness
		trust         TrustState
	}{
		{name: "pending review", disabledHooks: `{}`, configuration: ConfigurationLive, effectiveness: EffectivenessTrustUnknown, trust: TrustPendingReview},
		{
			name:          "trusted as of snapshot",
			disabledHooks: `{"state":{"/home/marty/.codex/hooks.json:pre_tool_use:0:0":{"trusted_hash":"sha256:adae0c83d982bb4230f29f0a038d225be681dae9467dc684b1f4ed220093a07b"}}}`,
			configuration: ConfigurationLive, effectiveness: EffectivenessTrustUnknown, trust: TrustTrustedAtSnapshot,
		},
		{
			name:          "disabled",
			disabledHooks: `{"state":{"/home/marty/.codex/hooks.json:pre_tool_use:0:0":{"trusted_hash":"sha256:adae0c83d982bb4230f29f0a038d225be681dae9467dc684b1f4ed220093a07b","enabled":false}}}`,
			configuration: ConfigurationDisabled, effectiveness: EffectivenessTrustUnknown, trust: TrustDisabled,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := ObservedSource{
				Source: configRef, SourcePath: &configPath, ContentHash: &configHash,
				Hooks: raw(`{}`), DisabledHooks: raw(tt.disabledHooks),
			}
			resolution, err := Resolve(ProviderCodex, []SourceRef{ref, configRef}, []ObservedSource{hooks, config})
			if err != nil {
				t.Fatal(err)
			}
			state := onlyRunState(t, resolution)
			if state.Configuration != tt.configuration || state.Effectiveness != tt.effectiveness || state.Trust != tt.trust {
				t.Fatalf("state = %#v, want configuration=%q effectiveness=%q trust=%q", state, tt.configuration, tt.effectiveness, tt.trust)
			}
		})
	}
	if CodexTrustSnapshotCaveat != "trusted when last reviewed; Multica cannot confirm it is unchanged" {
		t.Fatalf("trust caveat = %q", CodexTrustSnapshotCaveat)
	}
}

func TestCodexWorkedCommandExampleIsSupported(t *testing.T) {
	// Verbatim from https://developers.openai.com/codex/hooks, "Config shape".
	// The section says a hook config has event, matcher-group, and handler levels.
	source := foundSource(SourceRef{Scope: "user", Format: "json"}, `{
		"SessionStart": [
			{
				"matcher": "startup|resume",
				"hooks": [
					{
						"type": "command",
						"command": "python3 ~/.codex/hooks/session_start.py",
						"statusMessage": "Loading session notes",
						"additionalContextLimit": 5000
					}
				]
			}
		]
	}`)
	state := onlyRunState(t, mustResolve(t, ProviderCodex, []ObservedSource{source}))
	if state.NeverReason != "" || state.Effectiveness != EffectivenessTrustUnknown {
		t.Fatalf("worked command example state = %#v, want supported but pending review", state)
	}
}

func TestCodexStateJoinUsesScopeEventAndArrayPosition(t *testing.T) {
	jsonRef := SourceRef{Scope: "user", Format: "json"}
	tomlRef := SourceRef{Scope: "user", Format: "toml"}
	hooksPath, hooksHash := "/home/marty/.codex/hooks.json", "hooks-hash"
	configPath, configHash := "/home/marty/.codex/config.toml", "config-hash"
	resolution, err := Resolve(ProviderCodex, []SourceRef{jsonRef, tomlRef}, []ObservedSource{
		{
			Source: jsonRef, SourcePath: &hooksPath, ContentHash: &hooksHash,
			Hooks:         raw(`{"Stop":[{"hooks":[{"type":"command","command":"first"}]},{"hooks":[{"type":"command","command":"second"}]}]}`),
			DisabledHooks: raw(`{}`),
		},
		{
			Source: tomlRef, SourcePath: &configPath, ContentHash: &configHash, Hooks: raw(`{}`),
			DisabledHooks: raw(`{"state":{"/home/marty/.codex/hooks.json:stop:1:0":{"enabled":false}}}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	states, err := resolution.RunStates()
	if err != nil {
		t.Fatal(err)
	}
	byCommand := runStatesByCommand(t, states)
	if byCommand["first"].Configuration != ConfigurationLive || byCommand["first"].Trust != TrustPendingReview {
		t.Fatalf("first state = %#v, want live and pending", byCommand["first"])
	}
	if byCommand["second"].Configuration != ConfigurationDisabled || byCommand["second"].Trust != TrustDisabled {
		t.Fatalf("second state = %#v, want positionally disabled", byCommand["second"])
	}
}

func TestCodexStateJoinDoesNotCrossScopes(t *testing.T) {
	jsonRef := SourceRef{Scope: "user", Format: "json"}
	tomlRef := SourceRef{Scope: "project", Format: "toml"}
	hooksPath, hooksHash := "/home/marty/.codex/hooks.json", "hooks-hash"
	configPath, configHash := "/repo/.codex/config.toml", "config-hash"
	resolution, err := Resolve(ProviderCodex, []SourceRef{jsonRef, tomlRef}, []ObservedSource{
		{
			Source: jsonRef, SourcePath: &hooksPath, ContentHash: &hooksHash,
			Hooks: raw(`{"Stop":[{"hooks":[{"type":"command","command":"finish"}]}]}`), DisabledHooks: raw(`{}`),
		},
		{
			Source: tomlRef, SourcePath: &configPath, ContentHash: &configHash, Hooks: raw(`{}`),
			DisabledHooks: raw(`{"state":{"/home/marty/.codex/hooks.json:stop:0:0":{"enabled":false}}}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	state := onlyRunState(t, resolution)
	if state.Configuration != ConfigurationLive || state.Trust != TrustPendingReview {
		t.Fatalf("cross-scope state applied to user hook: %#v", state)
	}
}

func TestClaudeParkedHookIsNotReportedLive(t *testing.T) {
	ref := SourceRef{Scope: "local", Format: "json"}
	canonical := raw(`{"command":"./check.sh","type":"command"}`)
	id := hookID(ref, "PreToolUse", "Bash", canonical, 0)
	path, hash := "/repo/.claude/settings.local.json", "settings-hash"
	parked := fmt.Sprintf(`{%q:{"event":"PreToolUse","matcher":"Bash","handler":{"type":"command","command":"./check.sh"},"parked_at":"2026-09-12T08:00:00Z"}}`, id)
	observed := ObservedSource{
		Source: ref, SourcePath: &path, ContentHash: &hash,
		Hooks:         raw(`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./check.sh"}]}]}`),
		DisabledHooks: raw(parked),
	}
	state := onlyRunState(t, mustResolve(t, ProviderClaude, []ObservedSource{observed}))
	if state.Configuration != ConfigurationParked || state.ParkedAt != "2026-09-12T08:00:00Z" {
		t.Fatalf("state = %#v, want parked configuration", state)
	}
	if state.Configuration == ConfigurationLive {
		t.Fatal("parked Claude handler was reported live")
	}
}

func TestClaudeParkedHookRemainsRenderableOutsideHooks(t *testing.T) {
	ref := SourceRef{Scope: "local", Format: "json"}
	path, hash := "/repo/.claude/settings.local.json", "settings-hash"
	observed := ObservedSource{
		Source: ref, SourcePath: &path, ContentHash: &hash, Hooks: raw(`{}`),
		DisabledHooks: raw(`{"sha256:parked":{"event":"Stop","matcher":"","handler":{"type":"command","command":"finish"},"parked_at":"2026-09-12T08:00:00Z"}}`),
	}
	state := onlyRunState(t, mustResolve(t, ProviderClaude, []ObservedSource{observed}))
	if state.Hook.HookID != "sha256:parked" || state.Configuration != ConfigurationParked || string(state.Hook.Handler) != `{"command":"finish","type":"command"}` {
		t.Fatalf("parked run state = %#v, want renderable canonical entry", state)
	}
}

func TestClaudeBooleanDisabledHookIsRejected(t *testing.T) {
	ref := SourceRef{Scope: "local", Format: "json"}
	path, hash := "/repo/.claude/settings.local.json", "settings-hash"
	resolution, err := Resolve(ProviderClaude, []SourceRef{ref}, []ObservedSource{{
		Source: ref, SourcePath: &path, ContentHash: &hash,
		Hooks: raw(`{}`), DisabledHooks: raw(`{"one":true}`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolution.RunStates(); err == nil || !strings.Contains(err.Error(), `parked hook "one"`) {
		t.Fatalf("error = %v, want boolean sidecar rejection", err)
	}
}

func mustResolve(t *testing.T, provider Provider, observed []ObservedSource) Resolution {
	t.Helper()
	expected := make([]SourceRef, len(observed))
	for i := range observed {
		expected[i] = observed[i].Source
	}
	resolution, err := Resolve(provider, expected, observed)
	if err != nil {
		t.Fatal(err)
	}
	return resolution
}

func resolutionForHandler(t *testing.T, provider Provider, event, handler string) Resolution {
	t.Helper()
	source := foundSource(SourceRef{Scope: "user", Format: "json"}, fmt.Sprintf(`{%q:[{"hooks":[%s]}]}`, event, handler))
	return mustResolve(t, provider, []ObservedSource{source})
}

func onlyRunState(t *testing.T, resolution Resolution) RunState {
	t.Helper()
	states, err := resolution.RunStates()
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("run states = %#v, want exactly one", states)
	}
	return states[0]
}

func entriesByCommand(t *testing.T, entries []ResolvedHook) map[string]ResolvedHook {
	t.Helper()
	result := make(map[string]ResolvedHook, len(entries))
	for _, entry := range entries {
		var handler struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(entry.Handler, &handler); err != nil {
			t.Fatal(err)
		}
		result[handler.Command] = entry
	}
	return result
}

func entriesByMatcher(entries []ResolvedHook) map[string]ResolvedHook {
	result := make(map[string]ResolvedHook, len(entries))
	for _, entry := range entries {
		result[entry.Matcher] = entry
	}
	return result
}

func runStatesByMatcher(states []RunState) map[string]RunState {
	result := make(map[string]RunState, len(states))
	for _, state := range states {
		result[state.Hook.Matcher] = state
	}
	return result
}

func runStatesByCommand(t *testing.T, states []RunState) map[string]RunState {
	t.Helper()
	result := make(map[string]RunState, len(states))
	for _, state := range states {
		var handler struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(state.Hook.Handler, &handler); err != nil {
			t.Fatal(err)
		}
		result[handler.Command] = state
	}
	return result
}
