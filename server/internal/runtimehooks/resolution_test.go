package runtimehooks

import (
	"encoding/json"
	"sort"
	"testing"
)

func TestResolvePreservesSourceObservationStates(t *testing.T) {
	expected := []SourceRef{
		{Scope: "user", Format: "json"},
		{Scope: "project", Format: "json"},
		{Scope: "local", Format: "json"},
	}
	projectPath, projectHash := "/repo/.claude/settings.json", "project-hash"
	resolution, err := Resolve(ProviderClaude, expected, []ObservedSource{
		{Source: expected[0], Hooks: raw(`{}`)},
		{Source: expected[1], SourcePath: &projectPath, ContentHash: &projectHash, Hooks: raw(`{}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []SourceState{SourceAbsent, SourceFound, SourceNotChecked}
	for i, source := range resolution.Sources {
		if source.State != want[i] {
			t.Errorf("source %d state = %q, want %q", i, source.State, want[i])
		}
	}
}

func TestResolveMergesEveryLayerForBothProviders(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		sources  []ObservedSource
	}{
		{
			name:     "claude settings layers",
			provider: ProviderClaude,
			sources: []ObservedSource{
				foundSource(SourceRef{Scope: "user", Format: "json"}, `{"Stop":[{"hooks":[{"type":"command","command":"user"}]}]}`),
				foundSource(SourceRef{Scope: "project", Format: "json"}, `{"Stop":[{"hooks":[{"type":"command","command":"project"}]}]}`),
			},
		},
		{
			name:     "codex settings layers and formats",
			provider: ProviderCodex,
			sources: []ObservedSource{
				foundSource(SourceRef{Scope: "user", Format: "json"}, `{"Stop":[{"hooks":[{"type":"command","command":"user-json"}]}]}`),
				foundSource(SourceRef{Scope: "user", Format: "toml"}, `{"Stop":[{"hooks":[{"type":"command","command":"user-toml"}]}]}`),
				foundSource(SourceRef{Scope: "project", Format: "json"}, `{"Stop":[{"hooks":[{"type":"command","command":"project-json"}]}]}`),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expected := make([]SourceRef, len(tt.sources))
			for i := range tt.sources {
				expected[i] = tt.sources[i].Source
			}
			resolution, err := Resolve(tt.provider, expected, tt.sources)
			if err != nil {
				t.Fatal(err)
			}
			entries := resolution.EntriesForEvent("Stop")
			if len(entries) != len(tt.sources) {
				t.Fatalf("Stop entries = %d, want %d", len(entries), len(tt.sources))
			}
			if got := uniqueSourceCount(entries); got != len(tt.sources) {
				t.Fatalf("source identities = %d, want %d", got, len(tt.sources))
			}
		})
	}
}

func TestMergeMatchedDeduplicatesClaudeSettingsHandlersAcrossFiles(t *testing.T) {
	user := foundSource(SourceRef{Scope: "user", Format: "json"}, `{"PreToolUse":[{"matcher":"Bash|Edit","hooks":[{"type":"command","command":"check","timeout":10}]}]}`)
	project := foundSource(SourceRef{Scope: "project", Format: "json"}, `{"PreToolUse":[{"matcher":"Bash","hooks":[{"timeout":10,"command":"check","type":"command"}]}]}`)
	resolution, err := Resolve(ProviderClaude, []SourceRef{user.Source, project.Source}, []ObservedSource{user, project})
	if err != nil {
		t.Fatal(err)
	}
	runs := resolution.MergeMatched(resolution.EntriesForEvent("PreToolUse"))
	if len(runs) != 1 {
		t.Fatalf("runs = %#v, want one deduplicated handler", runs)
	}
	if len(runs[0].Sources) != 2 {
		t.Fatalf("deduplicated sources = %#v, want both settings files", runs[0].Sources)
	}
}

func TestMergeMatchedKeepsClaudePluginAndSkillCopiesSeparate(t *testing.T) {
	settings := SourceRef{Scope: "user", Format: "json"}
	plugin := SourceRef{Kind: SourcePlugin, Name: "formatter", Format: "json"}
	skill := SourceRef{Kind: SourceSkill, Name: "review", Format: "yaml"}
	hooks := `{"Stop":[{"hooks":[{"type":"command","command":"same"}]}]}`
	observed := []ObservedSource{
		foundSource(settings, hooks), foundSource(plugin, hooks), foundSource(skill, hooks),
	}
	resolution, err := Resolve(ProviderClaude, []SourceRef{settings, plugin, skill}, observed)
	if err != nil {
		t.Fatal(err)
	}
	runs := resolution.MergeMatched(resolution.EntriesForEvent("Stop"))
	if len(runs) != 3 {
		t.Fatalf("runs = %#v, want separate settings, plugin, and skill copies", runs)
	}
}

func TestMergeMatchedDoesNotDeduplicateCodexSources(t *testing.T) {
	jsonSource := foundSource(SourceRef{Scope: "user", Format: "json"}, `{"Stop":[{"hooks":[{"type":"command","command":"same"}]}]}`)
	tomlSource := foundSource(SourceRef{Scope: "user", Format: "toml"}, `{"Stop":[{"hooks":[{"type":"command","command":"same"}]}]}`)
	resolution, err := Resolve(ProviderCodex, []SourceRef{jsonSource.Source, tomlSource.Source}, []ObservedSource{jsonSource, tomlSource})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(resolution.MergeMatched(resolution.EntriesForEvent("Stop"))); got != 2 {
		t.Fatalf("runs = %d, want both Codex source copies", got)
	}
}

func TestResolutionDoesNotGiveSourceOrderExecutionMeaning(t *testing.T) {
	a := foundSource(SourceRef{Scope: "user", Format: "json"}, `{"Stop":[{"hooks":[{"type":"command","command":"a"}]}]}`)
	b := foundSource(SourceRef{Scope: "project", Format: "json"}, `{"Stop":[{"hooks":[{"type":"command","command":"b"}]}]}`)
	forward, err := Resolve(ProviderClaude, []SourceRef{a.Source, b.Source}, []ObservedSource{a, b})
	if err != nil {
		t.Fatal(err)
	}
	reverse, err := Resolve(ProviderClaude, []SourceRef{b.Source, a.Source}, []ObservedSource{b, a})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := handlerSet(forward.EntriesForEvent("Stop")), handlerSet(reverse.EntriesForEvent("Stop")); !equalStrings(got, want) {
		t.Fatalf("permuting layers changed the execution set: %v != %v", got, want)
	}
}

func raw(value string) json.RawMessage { return json.RawMessage(value) }

func foundSource(source SourceRef, hooks string) ObservedSource {
	path, hash := source.Scope+source.Name+"."+source.Format, "hash-"+source.Scope+source.Name+source.Format
	return ObservedSource{Source: source, SourcePath: &path, ContentHash: &hash, Hooks: raw(hooks)}
}

func uniqueSourceCount(entries []ResolvedHook) int {
	seen := map[SourceRef]struct{}{}
	for _, entry := range entries {
		for _, source := range entry.Sources {
			seen[source] = struct{}{}
		}
	}
	return len(seen)
}

func handlerSet(entries []ResolvedHook) []string {
	values := make([]string, 0, len(entries))
	for _, entry := range entries {
		values = append(values, string(entry.Handler))
	}
	sort.Strings(values)
	return values
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
