package runtimehooks

import (
	"encoding/json"
	"sort"
)

// The projection exists so a client never re-derives a provider rule.
// Configuration, effectiveness, trust and matcher semantics are decided once,
// here, and travel outward as data. A second implementation in the UI would be
// a second place for the schema claims this feature has already got wrong to
// go wrong again — and the UI has no way to check itself against the docs.

// ProjectedSourceRef is a source identity as a client sees it. Kind is always
// populated, so an absent value never has to be interpreted.
type ProjectedSourceRef struct {
	Scope  string `json:"scope"`
	Format string `json:"format"`
	Kind   string `json:"kind"`
	Name   string `json:"name,omitempty"`
}

// ProjectedEntry keeps Configuration and Effectiveness as two fields on
// purpose. A hook can be parked and matcher-ineligible at once, and one
// collapsed badge cannot say that unparking it still would not make it run.
type ProjectedEntry struct {
	HookID          string               `json:"hook_id"`
	Event           string               `json:"event"`
	Matcher         string               `json:"matcher"`
	MatcherKind     string               `json:"matcher_kind"`
	MatcherError    string               `json:"matcher_error,omitempty"`
	Handler         json.RawMessage      `json:"handler"`
	HandlerType     string               `json:"handler_type"`
	Sources         []ProjectedSourceRef `json:"sources"`
	Configuration   string               `json:"configuration"`
	ParkedAt        string               `json:"parked_at,omitempty"`
	Effectiveness   string               `json:"effectiveness"`
	NeverRunsReason string               `json:"never_runs_reason,omitempty"`
	Trust           string               `json:"trust"`
	TrustCaveat     string               `json:"trust_caveat,omitempty"`
}

// ProjectedUnrecognizedKey is a source key that was not an event's
// matcher-group array. It travels to the client so the row is visibly skipped
// rather than silently dropped.
type ProjectedUnrecognizedKey struct {
	Source ProjectedSourceRef `json:"source"`
	Key    string             `json:"key"`
	Reason string             `json:"reason"`
}

// Projection carries Error rather than replacing the result: one malformed
// source must not hide the sources that did resolve, and the honest report of
// an unreadable snapshot is that it could not be resolved — not "no hooks".
type Projection struct {
	Provider string           `json:"provider"`
	Entries  []ProjectedEntry `json:"entries"`
	// EventValueRoles carries EventValueRole for each event the projection
	// holds an entry for. It travels with the projection rather than only
	// with an answer so a client can caption the value field correctly before
	// any value exists — the role is a property of the event, not of a
	// particular answer, and asking for one to learn it would be backwards.
	EventValueRoles  map[string]string          `json:"event_value_roles,omitempty"`
	UnrecognizedKeys []ProjectedUnrecognizedKey `json:"unrecognized_keys,omitempty"`
	Error            string                     `json:"error,omitempty"`
}

// Project resolves observed against expected and derives per-entry state.
// Sources observed outside expected are skipped rather than failing the whole
// projection: Resolve rejects an unexpected source outright, and one stale row
// must not cost the caller every readable scope.
func Project(provider Provider, expected []SourceRef, observed []ObservedSource) Projection {
	projection := Projection{Provider: string(provider), Entries: []ProjectedEntry{}}
	resolution, err := resolveObserved(provider, expected, observed)
	if err != nil {
		projection.Error = err.Error()
		return projection
	}
	states, err := resolution.RunStates()
	if err != nil {
		projection.Error = err.Error()
		return projection
	}

	for _, state := range states {
		projection.Entries = append(projection.Entries, projectEntry(provider, state))
	}
	sortProjectedEntries(projection.Entries)
	projection.EventValueRoles = make(map[string]string, len(projection.Entries))
	for _, entry := range projection.Entries {
		projection.EventValueRoles[entry.Event] = string(EventValueRole(provider, entry.Event))
	}
	for _, key := range resolution.UnrecognizedKeys {
		projection.UnrecognizedKeys = append(projection.UnrecognizedKeys, ProjectedUnrecognizedKey{
			Source: projectSourceRef(key.Source), Key: key.Key, Reason: key.Reason,
		})
	}
	return projection
}

// resolveObserved drops observations outside expected and then resolves what
// is left. Resolve rejects an unexpected source outright, so filtering first
// is what stops one stale row costing the caller every readable scope. Every
// consumer of a snapshot goes through here, so the filter cannot be applied
// one way for the projection and another way for an answer.
func resolveObserved(provider Provider, expected []SourceRef, observed []ObservedSource) (Resolution, error) {
	allowed := make(map[SourceRef]struct{}, len(expected))
	for _, ref := range normalizedRefs(expected) {
		allowed[ref] = struct{}{}
	}
	placeable := make([]ObservedSource, 0, len(observed))
	for _, source := range observed {
		if _, ok := allowed[normalizeRef(source.Source)]; !ok {
			continue
		}
		placeable = append(placeable, source)
	}
	return Resolve(provider, expected, placeable)
}

func projectSourceRef(ref SourceRef) ProjectedSourceRef {
	ref = normalizeRef(ref)
	return ProjectedSourceRef{Scope: ref.Scope, Format: ref.Format, Kind: string(ref.Kind), Name: ref.Name}
}

func projectEntry(provider Provider, state RunState) ProjectedEntry {
	matcher := EvaluateMatcher(provider, state.Hook.Event, state.Hook.Matcher)
	entry := ProjectedEntry{
		HookID:          state.Hook.HookID,
		Event:           state.Hook.Event,
		Matcher:         state.Hook.Matcher,
		MatcherKind:     string(matcher.Kind),
		MatcherError:    matcher.Error,
		Handler:         state.Hook.Handler,
		HandlerType:     state.Hook.HandlerType(),
		Sources:         make([]ProjectedSourceRef, 0, len(state.Hook.Sources)),
		Configuration:   string(state.Configuration),
		ParkedAt:        state.ParkedAt,
		Effectiveness:   string(state.Effectiveness),
		NeverRunsReason: string(state.NeverReason),
		Trust:           string(state.Trust),
	}
	if state.Trust == TrustTrustedAtSnapshot {
		entry.TrustCaveat = CodexTrustSnapshotCaveat
	}
	for _, ref := range state.Hook.Sources {
		entry.Sources = append(entry.Sources, projectSourceRef(ref))
	}
	return entry
}

// RunStates walks configured entries in source order and then drains a map of
// orphan parked hooks, so its tail is unordered. Sorting here gives a client a
// stable list without implying an execution order: no provider assigns one,
// and every key below is identity, not precedence.
func sortProjectedEntries(entries []ProjectedEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		left, right := entries[i], entries[j]
		if left.Event != right.Event {
			return left.Event < right.Event
		}
		leftSource, rightSource := primaryProjectedSource(left), primaryProjectedSource(right)
		if leftSource.Scope != rightSource.Scope {
			return leftSource.Scope < rightSource.Scope
		}
		if leftSource.Format != rightSource.Format {
			return leftSource.Format < rightSource.Format
		}
		if left.Matcher != right.Matcher {
			return left.Matcher < right.Matcher
		}
		return left.HookID < right.HookID
	})
}

func primaryProjectedSource(entry ProjectedEntry) ProjectedSourceRef {
	if len(entry.Sources) == 0 {
		return ProjectedSourceRef{}
	}
	return entry.Sources[0]
}
