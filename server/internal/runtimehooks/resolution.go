// Package runtimehooks resolves provider hook snapshots without assigning
// precedence or execution order to their source layers.
package runtimehooks

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type Provider string

const (
	ProviderClaude Provider = "claude"
	ProviderCodex  Provider = "codex"
)

type SourceKind string

const (
	SourceSettings SourceKind = "settings"
	SourcePlugin   SourceKind = "plugin"
	SourceSkill    SourceKind = "skill"
)

// SourceRef is a provider source identity. Name distinguishes independently
// installed components such as plugins and skills. An empty Kind is settings.
type SourceRef struct {
	Scope  string
	Format string
	Kind   SourceKind
	Name   string
}

type SourceState string

const (
	SourceFound      SourceState = "found"
	SourceAbsent     SourceState = "absent"
	SourceNotChecked SourceState = "not_checked"
)

// ObservedSource is one source that the host checked. Nil SourcePath and
// ContentHash together mean the source was checked and absent.
type ObservedSource struct {
	Source        SourceRef
	SourcePath    *string
	ContentHash   *string
	Hooks         json.RawMessage
	DisabledHooks json.RawMessage
}

// ResolvedSource preserves the difference between found, checked-absent, and
// not-checked sources. Not-checked is produced by diffing observations against
// the caller-supplied expected source set.
type ResolvedSource struct {
	Source        SourceRef
	State         SourceState
	SourcePath    *string
	ContentHash   *string
	Hooks         json.RawMessage
	DisabledHooks json.RawMessage
}

// ResolvedHook is one handler contributed by one or more source files.
// Sources has multiple elements only when Claude deduplicates the same
// settings handler across files. The source slice carries provenance, not an
// execution sequence.
type ResolvedHook struct {
	Event   string
	Matcher string
	Handler json.RawMessage
	Sources []SourceRef
}

// Resolution is the merged, unordered view of every expected source.
type Resolution struct {
	Provider Provider
	Sources  []ResolvedSource
	entries  []ResolvedHook
}

type matcherGroup struct {
	Matcher string            `json:"matcher"`
	Hooks   []json.RawMessage `json:"hooks"`
}

// Resolve merges all observed layers and formats. Expected sources absent from
// observed are retained as not checked; observed entries with nil identity are
// retained as checked and absent.
func Resolve(provider Provider, expected []SourceRef, observed []ObservedSource) (Resolution, error) {
	if provider != ProviderClaude && provider != ProviderCodex {
		return Resolution{}, fmt.Errorf("provider %q does not expose lifecycle hooks", provider)
	}

	expected = normalizedRefs(expected)
	byRef := make(map[SourceRef]ObservedSource, len(observed))
	for _, source := range observed {
		source.Source = normalizeRef(source.Source)
		if _, duplicate := byRef[source.Source]; duplicate {
			return Resolution{}, fmt.Errorf("duplicate observed source %s", describeSource(source.Source))
		}
		if (source.SourcePath == nil) != (source.ContentHash == nil) {
			return Resolution{}, fmt.Errorf("source %s path and content hash must both be present or both be nil", describeSource(source.Source))
		}
		byRef[source.Source] = source
	}

	resolution := Resolution{Provider: provider, Sources: make([]ResolvedSource, 0, len(expected))}
	seenExpected := make(map[SourceRef]struct{}, len(expected))
	for _, ref := range expected {
		if _, duplicate := seenExpected[ref]; duplicate {
			return Resolution{}, fmt.Errorf("duplicate expected source %s", describeSource(ref))
		}
		seenExpected[ref] = struct{}{}
		observedSource, checked := byRef[ref]
		if !checked {
			resolution.Sources = append(resolution.Sources, ResolvedSource{Source: ref, State: SourceNotChecked})
			continue
		}
		delete(byRef, ref)
		resolved := ResolvedSource{
			Source: ref, State: SourceAbsent, SourcePath: observedSource.SourcePath,
			ContentHash: observedSource.ContentHash, Hooks: cloneRaw(observedSource.Hooks),
			DisabledHooks: cloneRaw(observedSource.DisabledHooks),
		}
		if observedSource.SourcePath != nil {
			resolved.State = SourceFound
			entries, err := parseEntries(ref, observedSource.Hooks)
			if err != nil {
				return Resolution{}, err
			}
			resolution.entries = append(resolution.entries, entries...)
		}
		resolution.Sources = append(resolution.Sources, resolved)
	}
	for ref := range byRef {
		return Resolution{}, fmt.Errorf("observed unexpected source %s", describeSource(ref))
	}
	return resolution, nil
}

// EntriesForEvent returns the unordered candidates contributed for event.
// Matcher evaluation is deliberately left to the provider-specific matching
// layer. Pass the matched subset to MergeMatched before execution.
func (r Resolution) EntriesForEvent(event string) []ResolvedHook {
	var entries []ResolvedHook
	for _, entry := range r.entries {
		if entry.Event == event {
			entries = append(entries, cloneEntry(entry))
		}
	}
	return entries
}

// MergeMatched applies the providers' cross-source execution rule to a set of
// entries whose matchers already matched. Codex keeps every source copy.
// Claude runs an identical settings handler once across settings files while
// retaining each plugin and skill copy as a separate run.
func (r Resolution) MergeMatched(matched []ResolvedHook) []ResolvedHook {
	if r.Provider != ProviderClaude {
		return cloneEntries(matched)
	}

	merged := make([]ResolvedHook, 0, len(matched))
	for _, entry := range matched {
		entry = cloneEntry(entry)
		if !settingsOnly(entry.Sources) {
			merged = append(merged, entry)
			continue
		}
		key := entry.Event + "\x00" + string(entry.Handler)
		found := -1
		for i := range merged {
			if !settingsOnly(merged[i].Sources) || merged[i].Event+"\x00"+string(merged[i].Handler) != key {
				continue
			}
			if !sharesSource(merged[i].Sources, entry.Sources) {
				found = i
				break
			}
		}
		if found < 0 {
			merged = append(merged, entry)
			continue
		}
		merged[found].Sources = append(merged[found].Sources, entry.Sources...)
	}
	return merged
}

func parseEntries(source SourceRef, raw json.RawMessage) ([]ResolvedHook, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var events map[string]json.RawMessage
	if err := json.Unmarshal(raw, &events); err != nil {
		return nil, fmt.Errorf("decode hooks from %s: %w", describeSource(source), err)
	}
	var entries []ResolvedHook
	for event, groupsRaw := range events {
		var groups []matcherGroup
		if err := json.Unmarshal(groupsRaw, &groups); err != nil {
			return nil, fmt.Errorf("decode %s hook groups from %s: %w", event, describeSource(source), err)
		}
		for _, group := range groups {
			for _, handler := range group.Hooks {
				canonical, err := canonicalJSON(handler)
				if err != nil {
					return nil, fmt.Errorf("decode %s handler from %s: %w", event, describeSource(source), err)
				}
				entries = append(entries, ResolvedHook{
					Event: event, Matcher: group.Matcher, Handler: canonical, Sources: []SourceRef{source},
				})
			}
		}
	}
	return entries, nil
}

func canonicalJSON(raw json.RawMessage) (json.RawMessage, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return canonical, nil
}

func normalizeRef(ref SourceRef) SourceRef {
	if ref.Kind == "" {
		ref.Kind = SourceSettings
	}
	return ref
}

func normalizedRefs(refs []SourceRef) []SourceRef {
	normalized := make([]SourceRef, len(refs))
	for i, ref := range refs {
		normalized[i] = normalizeRef(ref)
	}
	return normalized
}

func settingsOnly(sources []SourceRef) bool {
	if len(sources) == 0 {
		return false
	}
	for _, source := range sources {
		if normalizeRef(source).Kind != SourceSettings {
			return false
		}
	}
	return true
}

func sharesSource(a, b []SourceRef) bool {
	for _, left := range a {
		for _, right := range b {
			if normalizeRef(left) == normalizeRef(right) {
				return true
			}
		}
	}
	return false
}

func cloneEntries(entries []ResolvedHook) []ResolvedHook {
	cloned := make([]ResolvedHook, len(entries))
	for i, entry := range entries {
		cloned[i] = cloneEntry(entry)
	}
	return cloned
}

func cloneEntry(entry ResolvedHook) ResolvedHook {
	entry.Handler = cloneRaw(entry.Handler)
	entry.Sources = append([]SourceRef(nil), entry.Sources...)
	return entry
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), raw...)
}

func describeSource(source SourceRef) string {
	source = normalizeRef(source)
	if source.Name != "" {
		return fmt.Sprintf("%s %q (%s/%s)", source.Kind, source.Name, source.Scope, source.Format)
	}
	return fmt.Sprintf("%s (%s/%s)", source.Kind, source.Scope, source.Format)
}
