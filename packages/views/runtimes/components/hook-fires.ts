import type { RuntimeHookFire } from "@multica/core/types";

/**
 * Presentation helpers for the hook-fire feed (LOCO-135).
 *
 * The one rule every function here obeys: NOTHING in this file decides what a
 * record's provenance is. `provenanceKind` classifies the stored string by
 * exact equality so a label can be looked up, and returns "unrecognized" for
 * anything else rather than picking a plausible member. That is the whole
 * point — DP-LOCO-114-03 condition 2 forbids provenance computation at read
 * time, and round 3 of this effort shipped a defect that fabricated
 * `debug_log` by inference. A `startsWith`, a `?? "inferred"`, or a "looks
 * like a match" branch anywhere below would reintroduce it in the UI layer.
 */

export type ProvenanceKind = "debug_log" | "inferred" | "unrecognized";

export type OutcomeKind =
  | "success"
  | "failure"
  | "blocked"
  | "skipped"
  | "unknown"
  | "unrecognized";

/**
 * Classify a stored provenance value for label lookup. Exact equality only.
 *
 * An unrecognised value gets its own kind and its own copy, which states that
 * Multica cannot characterise the timestamp. It is NOT folded into `inferred`:
 * `inferred` is a specific, defensible claim ("we saw it run, we just could
 * not time it"), and asserting it about a value we do not understand would be
 * a render-time provenance decision.
 */
export function provenanceKind(value: string): ProvenanceKind {
  if (value === "debug_log") return "debug_log";
  if (value === "inferred") return "inferred";
  return "unrecognized";
}

/** Classify a stored outcome value for label lookup. Exact equality only. */
export function outcomeKind(value: string): OutcomeKind {
  switch (value) {
    case "success":
    case "failure":
    case "blocked":
    case "skipped":
    case "unknown":
      return value;
    default:
      return "unrecognized";
  }
}

/**
 * Why this row's outcome is `unknown`. Two real causes, per stage 7's audit,
 * and `unknown` was 66.7% of all non-success outcomes in the measured corpus
 * — common enough that a bare "unknown" chip would be the screen's biggest
 * unexplained element.
 *
 * The branches mirror the capture path's own mapping exactly
 * (`hookFireOutcome`, server/internal/daemon/hook_fire.go):
 *
 *  - provider said `error` with exit 126/127 → "spawn_ambiguous". Claude
 *    exposes no spawn-failure field, so a hook that ran and exited 127 is
 *    indistinguishable from a shell that never started it. Neither `failure`
 *    nor `skipped` would be true.
 *  - provider said `error` with no exit code → "no_exit_code".
 *  - provider said anything else non-success (a real `cancelled`, e.g. a
 *    timed-out hook) → "provider_state", which names the reported state.
 *
 * Read only from stored `detail`. This explains an outcome; it never touches
 * provenance.
 */
export type UnknownOutcomeCause =
  | "spawn_ambiguous"
  | "no_exit_code"
  | "provider_state"
  | "unclassified";

export function exitCodeOf(fire: RuntimeHookFire): number | null {
  const value = fire.detail?.["exit_code"];
  return typeof value === "number" ? value : null;
}

export function providerOutcomeOf(fire: RuntimeHookFire): string | null {
  const value = fire.detail?.["debug_outcome"];
  return typeof value === "string" && value !== "" ? value : null;
}

export function unknownOutcomeCause(fire: RuntimeHookFire): UnknownOutcomeCause {
  const providerOutcome = providerOutcomeOf(fire);
  const exitCode = exitCodeOf(fire);
  if (providerOutcome === "error") {
    if (exitCode === 126 || exitCode === 127) return "spawn_ambiguous";
    if (exitCode === null) return "no_exit_code";
    return "unclassified";
  }
  if (providerOutcome !== null && providerOutcome !== "success") {
    return "provider_state";
  }
  return "unclassified";
}

/**
 * The handler identity observed AT FIRE TIME, from the denormalized
 * `hook_spec`. Returns null when the spec carries no name rather than
 * substituting the event or the execution reference — a fire whose handler
 * Multica never saw must not borrow an identity from elsewhere.
 */
export function hookNameOf(fire: RuntimeHookFire): string | null {
  const spec = fire.hook_spec;
  if (spec === undefined || spec === null || Array.isArray(spec)) return null;
  const value = spec["hook_name"];
  return typeof value === "string" && value !== "" ? value : null;
}

export interface ProvenanceCounts {
  debug_log: number;
  inferred: number;
  unrecognized: number;
}

/**
 * Tally the loaded page by stored provenance.
 *
 * This is counting, not deciding: every row contributes to the bucket its own
 * stored column names. The tally exists because the split is the headline
 * fact about this data — stage 7's audit measured 70.1% `inferred` across 144
 * real responses — and a reader who cannot see the mix will assume the feed is
 * observed. It describes the loaded page only, never the provider.
 */
export function countProvenance(fires: RuntimeHookFire[]): ProvenanceCounts {
  const counts: ProvenanceCounts = { debug_log: 0, inferred: 0, unrecognized: 0 };
  for (const fire of fires) {
    counts[provenanceKind(fire.provenance)] += 1;
  }
  return counts;
}
