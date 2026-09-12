package runtimehooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

type ConfigurationState string

const (
	ConfigurationLive     ConfigurationState = "live"
	ConfigurationParked   ConfigurationState = "parked"
	ConfigurationDisabled ConfigurationState = "disabled"
)

type Effectiveness string

const (
	EffectivenessWillRun      Effectiveness = "will_run"
	EffectivenessNeverRuns    Effectiveness = "never_runs"
	EffectivenessTrustUnknown Effectiveness = "trust_unknown"
)

type NeverRunsReason string

const (
	NeverRunsMatcherIneligible          NeverRunsReason = "matcher_ineligible"
	NeverRunsHandlerUnsupportedProvider NeverRunsReason = "handler_type_unsupported_by_provider"
	NeverRunsHandlerUnsupportedEvent    NeverRunsReason = "handler_type_unsupported_for_event"
)

type TrustState string

const (
	TrustNotApplicable     TrustState = "not_applicable"
	TrustPendingReview     TrustState = "pending_review"
	TrustTrustedAtSnapshot TrustState = "trusted_as_of_snapshot"
	TrustDisabled          TrustState = "disabled"
	TrustManagedByPolicy   TrustState = "managed_by_policy"
)

// CodexTrustSnapshotCaveat is the qualification consumers must display for
// TrustTrustedAtSnapshot. Codex's hash algorithm is undocumented, so Multica
// cannot confirm that the observed trusted definition is still unchanged.
const CodexTrustSnapshotCaveat = "trusted when last reviewed; Multica cannot confirm it is unchanged"

// RunState keeps configuration and effectiveness separate: parking or
// disabling changes configuration, while an unsupported handler is a live but
// ineffective configuration. Trust is provider metadata used to explain a
// Codex effectiveness verdict.
type RunState struct {
	Hook          ResolvedHook
	Configuration ConfigurationState
	Effectiveness Effectiveness
	NeverReason   NeverRunsReason
	Trust         TrustState
	ParkedAt      string
}

type claudeParkedHook struct {
	Event    string          `json:"event"`
	Matcher  string          `json:"matcher"`
	Handler  json.RawMessage `json:"handler"`
	ParkedAt string          `json:"parked_at"`
}

type codexStateRecord struct {
	Enabled *bool `json:"enabled"`
}

type codexStateEntry struct {
	present bool
	record  codexStateRecord
}

// RunStates derives state for every configured entry. Its matcher-ineligible
// verdict is obtained from MatchEvent's existing NeverRuns partition instead
// of maintaining a second copy of that provider rule.
func (r Resolution) RunStates() ([]RunState, error) {
	parked, err := r.claudeParkedHooks()
	if err != nil {
		return nil, err
	}
	codexState := r.codexStateEntries()
	usedParked := make(map[string]struct{}, len(parked))
	states := make([]RunState, 0, len(r.entries)+len(parked))

	for _, entry := range r.entries {
		state := RunState{
			Hook:          cloneEntry(entry),
			Configuration: ConfigurationLive,
			Effectiveness: EffectivenessWillRun,
			Trust:         TrustNotApplicable,
		}
		if parkedEntry, ok := parked[entry.HookID]; ok {
			if !sameHookDefinition(entry, parkedEntry.Hook) {
				return nil, fmt.Errorf("parked hook %q does not match configured hook", entry.HookID)
			}
			state.Configuration = ConfigurationParked
			state.ParkedAt = parkedEntry.ParkedAt
			usedParked[entry.HookID] = struct{}{}
		}
		if r.Provider == ProviderCodex {
			r.applyCodexConfiguration(&state, codexState)
		}
		if err := r.applyEffectiveness(&state); err != nil {
			return nil, err
		}
		states = append(states, state)
	}

	for hookID, parkedEntry := range parked {
		if _, used := usedParked[hookID]; used {
			continue
		}
		state := RunState{
			Hook:          cloneEntry(parkedEntry.Hook),
			Configuration: ConfigurationParked,
			Effectiveness: EffectivenessWillRun,
			Trust:         TrustNotApplicable,
			ParkedAt:      parkedEntry.ParkedAt,
		}
		if err := r.applyEffectiveness(&state); err != nil {
			return nil, err
		}
		states = append(states, state)
	}

	return states, nil
}

func sameHookDefinition(configured, parked ResolvedHook) bool {
	return configured.Event == parked.Event &&
		configured.Matcher == parked.Matcher &&
		bytes.Equal(configured.Handler, parked.Handler)
}

func (r Resolution) applyEffectiveness(state *RunState) error {
	entry := cloneEntry(state.Hook)
	entry.Matcher = ""
	partition, err := (Resolution{Provider: r.Provider, entries: []ResolvedHook{entry}}).MatchEvent(entry.Event, "")
	if err != nil {
		return fmt.Errorf("classify %s hook %s: %w", r.Provider, entry.HookID, err)
	}
	if len(partition.NeverRuns) != 0 {
		state.Effectiveness = EffectivenessNeverRuns
		state.NeverReason = NeverRunsMatcherIneligible
		return nil
	}

	typeName, err := handlerType(entry.Handler)
	if err != nil {
		return fmt.Errorf("classify %s hook %s handler: %w", r.Provider, entry.HookID, err)
	}
	supported, reason := handlerTypeSupported(r.Provider, entry.Event, typeName)
	if !supported {
		state.Effectiveness = EffectivenessNeverRuns
		state.NeverReason = reason
	}
	return nil
}

func handlerType(raw json.RawMessage) (string, error) {
	var handler struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &handler); err != nil {
		return "", err
	}
	return handler.Type, nil
}

func handlerTypeSupported(provider Provider, event, typeName string) (bool, NeverRunsReason) {
	if provider == ProviderCodex {
		if typeName == "command" || typeName == "mcp_tool" {
			return true, ""
		}
		return false, NeverRunsHandlerUnsupportedProvider
	}
	if typeName == "command" {
		return true, ""
	}
	if typeName == "mcp_tool" {
		if event == "Setup" {
			return false, NeverRunsHandlerUnsupportedEvent
		}
		return true, ""
	}
	if typeName == "http" {
		if event == "SessionStart" || event == "Setup" {
			return false, NeverRunsHandlerUnsupportedEvent
		}
		return true, ""
	}
	if typeName == "prompt" || typeName == "agent" {
		if claudeAllHandlerTypeEvents[event] {
			return true, ""
		}
		return false, NeverRunsHandlerUnsupportedEvent
	}
	return false, NeverRunsHandlerUnsupportedProvider
}

// https://code.claude.com/docs/en/hooks, "Prompt-based hooks":
// "Events that support all five hook types" are exactly these thirteen.
var claudeAllHandlerTypeEvents = map[string]bool{
	"PermissionDenied":    true,
	"PermissionRequest":   true,
	"PostToolBatch":       true,
	"PostToolUse":         true,
	"PostToolUseFailure":  true,
	"PreToolUse":          true,
	"Stop":                true,
	"SubagentStop":        true,
	"TaskCompleted":       true,
	"TaskCreated":         true,
	"TeammateIdle":        true,
	"UserPromptExpansion": true,
	"UserPromptSubmit":    true,
}

type parkedRunState struct {
	Hook     ResolvedHook
	ParkedAt string
}

func (r Resolution) claudeParkedHooks() (map[string]parkedRunState, error) {
	parked := make(map[string]parkedRunState)
	if r.Provider != ProviderClaude {
		return parked, nil
	}
	for _, source := range r.Sources {
		if source.State != SourceFound || emptyJSONObject(source.DisabledHooks) {
			continue
		}
		var values map[string]json.RawMessage
		if err := json.Unmarshal(source.DisabledHooks, &values); err != nil {
			return nil, fmt.Errorf("decode parked hooks from %s: %w", describeSource(source.Source), err)
		}
		for hookID, raw := range values {
			var value claudeParkedHook
			if err := json.Unmarshal(raw, &value); err != nil {
				return nil, fmt.Errorf("decode parked hook %q from %s: %w", hookID, describeSource(source.Source), err)
			}
			if hookID == "" || value.Event == "" || len(bytes.TrimSpace(value.Handler)) == 0 || value.ParkedAt == "" {
				return nil, fmt.Errorf("parked hook %q from %s must include event, handler, and parked_at", hookID, describeSource(source.Source))
			}
			canonical, err := canonicalJSON(value.Handler)
			if err != nil {
				return nil, fmt.Errorf("decode parked hook %q handler from %s: %w", hookID, describeSource(source.Source), err)
			}
			if _, duplicate := parked[hookID]; duplicate {
				return nil, fmt.Errorf("duplicate parked hook id %q", hookID)
			}
			parked[hookID] = parkedRunState{
				Hook: ResolvedHook{
					HookID: hookID, Event: value.Event, Matcher: value.Matcher,
					Handler: canonical, Sources: []SourceRef{source.Source},
				},
				ParkedAt: value.ParkedAt,
			}
		}
	}
	return parked, nil
}

func emptyJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte(`{}`)) || bytes.Equal(trimmed, []byte(`null`))
}

func (r Resolution) codexStateEntries() map[string]codexStateEntry {
	entries := make(map[string]codexStateEntry)
	if r.Provider != ProviderCodex {
		return entries
	}
	for _, source := range r.Sources {
		if source.State != SourceFound || emptyJSONObject(source.DisabledHooks) {
			continue
		}
		// Observed option-A input from DP-LOCO-114-04-E, not a documented
		// Codex contract. Unknown or changed envelopes degrade to pending review.
		var envelope map[string]json.RawMessage
		if json.Unmarshal(source.DisabledHooks, &envelope) != nil {
			continue
		}
		stateRaw, ok := envelope["state"]
		if !ok {
			continue
		}
		var state map[string]json.RawMessage
		if json.Unmarshal(stateRaw, &state) != nil {
			continue
		}
		for key, raw := range state {
			var record codexStateRecord
			trimmed := bytes.TrimSpace(raw)
			if len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(trimmed, &record) != nil {
				continue
			}
			entries[codexStateKey(source.Source.Scope, key)] = codexStateEntry{present: true, record: record}
		}
	}
	return entries
}

func (r Resolution) applyCodexConfiguration(state *RunState, entries map[string]codexStateEntry) {
	if len(state.Hook.Sources) == 0 {
		state.Trust = TrustPendingReview
		state.Effectiveness = EffectivenessTrustUnknown
		return
	}
	ref := normalizeRef(state.Hook.Sources[0])
	if isCodexManagedScope(ref.Scope) {
		state.Trust = TrustManagedByPolicy
		return
	}
	// Non-managed Codex effectiveness remains unknown even after a review was
	// observed: without Codex's hash algorithm we cannot confirm that the state
	// record still describes the current handler.
	state.Effectiveness = EffectivenessTrustUnknown
	path := r.sourcePath(ref)
	key := fmt.Sprintf("%s:%s:%d:%d", path, snakeCase(state.Hook.Event), state.Hook.groupIndex, state.Hook.handlerIndex)
	observed := entries[codexStateKey(ref.Scope, key)]
	if !observed.present {
		state.Trust = TrustPendingReview
		return
	}
	if observed.record.Enabled != nil && !*observed.record.Enabled {
		state.Configuration = ConfigurationDisabled
		state.Trust = TrustDisabled
		return
	}
	// Provisional option A from DP-LOCO-114-04-E: presence proves only that
	// Codex reviewed this hook at snapshot time. We intentionally do not read,
	// compute, or compare trusted_hash because its algorithm is undocumented.
	state.Trust = TrustTrustedAtSnapshot
}

func (r Resolution) sourcePath(ref SourceRef) string {
	for _, source := range r.Sources {
		if normalizeRef(source.Source) == ref && source.SourcePath != nil {
			return *source.SourcePath
		}
	}
	return ""
}

func codexStateKey(scope, key string) string {
	return scope + "\x00" + key
}

func isCodexManagedScope(scope string) bool {
	switch strings.ToLower(scope) {
	case "system", "managed", "mdm", "cloud", "requirements":
		return true
	default:
		return false
	}
}

func snakeCase(value string) string {
	var result strings.Builder
	for i, current := range value {
		if unicode.IsUpper(current) && i > 0 {
			result.WriteByte('_')
		}
		result.WriteRune(unicode.ToLower(current))
	}
	return result.String()
}
