import type {
  RuntimeHookEntry,
  RuntimeHookEventAnswer,
  RuntimeHookEventAnswerResult,
  RuntimeHookMember,
  RuntimeHookReadRequest,
  RuntimeHookSource,
} from "@multica/core/types";

// Pure derivation for the Lifecycle Hooks tab. Everything here is shape and
// arithmetic over the server's projection — no provider rule is decided in
// this file. Configuration, effectiveness, trust and matcher semantics come
// from server/internal/runtimehooks, which is the only place they are checked
// against the providers' own documentation.
//
// Canonical test file: hooks-model.test.ts. The component suite keeps the
// happy path and the named regressions and does not re-run this matrix.

/** Providers that expose a lifecycle hook surface at all. */
export const HOOK_PROVIDERS = ["claude", "codex"] as const;

export function providerSupportsHooks(provider: string): boolean {
  return (HOOK_PROVIDERS as readonly string[]).includes(provider);
}

/**
 * Where the provider looks for each expected source. The daemon reads exactly
 * these paths (`server/internal/daemon/hook_config.go`); the Codex set matches
 * the four locations its documentation names. Shown for scopes that were never
 * checked too, because "we would have looked here" is the honest caption for a
 * scope with no observation.
 */
const SCOPE_PATHS: Record<string, Record<string, string>> = {
  claude: {
    "user:json": "~/.claude/settings.json",
    "project:json": "<project>/.claude/settings.json",
    "local:json": "<project>/.claude/settings.local.json",
  },
  codex: {
    "user:json": "~/.codex/hooks.json",
    "user:toml": "~/.codex/config.toml",
    "project:json": "<project>/.codex/hooks.json",
    "project:toml": "<project>/.codex/config.toml",
  },
};

export function hookSourcePath(
  provider: string,
  scope: string,
  format: string,
): string | null {
  return SCOPE_PATHS[provider]?.[`${scope}:${format}`] ?? null;
}

/**
 * User scope is the only source that lives on the daemon host, so it is the
 * only one Multica could ever write. Project and local depend on a project
 * checkout the runtime has no identity for, and stay read-only.
 */
export function hookScopeIsWritable(scope: string): boolean {
  return scope === "user";
}

export interface HookScopeRow {
  key: string;
  scope: string;
  format: string;
  /** found | absent | not_checked, straight from the server's diff. */
  state: string;
  sourcePath: string | null;
  expectedPath: string | null;
  contentHash: string | null;
  observedAt: string | null;
  writable: boolean;
  entryCount: number;
}

export function hookScopeRows(
  provider: string,
  sources: RuntimeHookSource[] | undefined,
  entries: RuntimeHookEntry[],
): HookScopeRow[] {
  const counts = new Map<string, number>();
  for (const entry of entries) {
    // Every source that contributed the handler is counted. More than one
    // means the provider's own cross-file deduplication matched it; the entry
    // genuinely belongs to each of those scopes.
    for (const ref of entry.sources) {
      const key = `${ref.scope}:${ref.format}`;
      counts.set(key, (counts.get(key) ?? 0) + 1);
    }
  }
  return (sources ?? []).map((source) => {
    const key = `${source.scope}:${source.format}`;
    return {
      key,
      scope: source.scope,
      format: source.format,
      state: source.state,
      sourcePath: source.source_path ?? null,
      expectedPath: hookSourcePath(provider, source.scope, source.format),
      contentHash: source.content_hash ?? null,
      observedAt: source.observed_at ?? null,
      writable: hookScopeIsWritable(source.scope),
      entryCount: counts.get(key) ?? 0,
    };
  });
}

export interface HookEventGroup {
  event: string;
  entries: RuntimeHookEntry[];
  /** Live configuration and a will_run verdict — both axes had to agree. */
  willRun: number;
  /** Configuration is parked or disabled, whatever effectiveness says. */
  inactive: number;
  /** Live configuration that the provider will not act on. */
  neverRuns: number;
  /** Live configuration whose Codex trust Multica cannot confirm. */
  trustUnknown: number;
  /** Matchers Multica cannot evaluate; a count of unknowns, not of failures. */
  unevaluableMatchers: number;
  /** Distinct sources the group's entries came from. */
  sourceCount: number;
}

export function hookEventGroups(entries: RuntimeHookEntry[]): HookEventGroup[] {
  const byEvent = new Map<string, RuntimeHookEntry[]>();
  for (const entry of entries) {
    const group = byEvent.get(entry.event);
    if (group) group.push(entry);
    else byEvent.set(entry.event, [entry]);
  }
  return [...byEvent.entries()]
    .map(([event, group]) => {
      const sources = new Set<string>();
      for (const entry of group) {
        for (const ref of entry.sources) sources.add(`${ref.scope}:${ref.format}`);
      }
      return {
        event,
        entries: group,
        willRun: group.filter(entryWillRun).length,
        inactive: group.filter((entry) => entry.configuration !== "live").length,
        neverRuns: group.filter(
          (entry) =>
            entry.configuration === "live" && entry.effectiveness === "never_runs",
        ).length,
        trustUnknown: group.filter(
          (entry) =>
            entry.configuration === "live" &&
            entry.effectiveness === "trust_unknown",
        ).length,
        unevaluableMatchers: group.filter(
          (entry) => entry.matcher_kind === "unevaluable",
        ).length,
        sourceCount: sources.size,
      };
    })
    .sort((left, right) => left.event.localeCompare(right.event));
}

/**
 * Both axes have to be satisfied, and only the axes: a hook runs when its
 * configuration is live and the provider's verdict is will_run. Anything else
 * — parked, disabled, ineligible, trust Multica cannot confirm — is not a
 * "will run", and none of it means another layer displaced the entry.
 */
export function entryWillRun(entry: RuntimeHookEntry): boolean {
  return entry.configuration === "live" && entry.effectiveness === "will_run";
}

export interface HookTotals {
  configured: number;
  willRun: number;
  inactive: number;
  neverRuns: number;
  trustUnknown: number;
  unevaluableMatchers: number;
  events: number;
  foundSources: number;
  absentSources: number;
  notCheckedSources: number;
}

export function hookTotals(
  entries: RuntimeHookEntry[],
  scopes: HookScopeRow[],
): HookTotals {
  return {
    configured: entries.length,
    willRun: entries.filter(entryWillRun).length,
    inactive: entries.filter((entry) => entry.configuration !== "live").length,
    neverRuns: entries.filter(
      (entry) => entry.configuration === "live" && entry.effectiveness === "never_runs",
    ).length,
    trustUnknown: entries.filter(
      (entry) =>
        entry.configuration === "live" && entry.effectiveness === "trust_unknown",
    ).length,
    unevaluableMatchers: entries.filter(
      (entry) => entry.matcher_kind === "unevaluable",
    ).length,
    events: new Set(entries.map((entry) => entry.event)).size,
    foundSources: scopes.filter((scope) => scope.state === "found").length,
    absentSources: scopes.filter((scope) => scope.state === "absent").length,
    notCheckedSources: scopes.filter((scope) => scope.state === "not_checked").length,
  };
}

export interface HookEmptySummary {
  /** Paths that were read and held no hook entries. */
  readPaths: string[];
  /** Scopes Multica looked for and did not find a file at. */
  absent: HookScopeRow[];
  /** Scopes nobody looked at, so they say nothing either way. */
  notChecked: HookScopeRow[];
  /**
   * No source was found at all, so "no hooks" is a statement about files that
   * are not there rather than about files that were read and held nothing.
   */
  nothingRead: boolean;
}

/**
 * Why there are no entries, from the sources themselves. The two reasons read
 * differently and must not share one sentence: every source was read and held
 * nothing, or there was nothing to read. A scope nobody checked supports
 * neither claim, so it is carried separately in both cases.
 */
export function hookEmptySummary(scopes: HookScopeRow[]): HookEmptySummary {
  const found = scopes.filter((scope) => scope.state === "found");
  return {
    readPaths: found.map(
      (scope) => scope.sourcePath ?? scope.expectedPath ?? scope.key,
    ),
    absent: scopes.filter((scope) => scope.state === "absent"),
    notChecked: scopes.filter((scope) => scope.state === "not_checked"),
    nothingRead: found.length === 0,
  };
}

/**
 * An observation is older than this after the read that produced it, so it is
 * labelled as stale rather than current. The server bounds `observed_at` only
 * against the future (`maxHookObservationFutureSkew`), so an arbitrarily
 * far-past timestamp can arrive on an otherwise healthy read and must not
 * read as fresh.
 */
export const HOOK_OBSERVATION_STALE_AFTER_MS = 5 * 60_000;

export type HookObservationKind =
  /** A host read is in flight; nothing may be presented as current yet. */
  | "discovering"
  /** This mount's own read of an online runtime. */
  | "live"
  /** The stored snapshot of an offline runtime, with its observation time. */
  | "last_known"
  /**
   * The runtime is offline and no snapshot was ever stored for it. Not a
   * failure: nothing went wrong, there is simply nothing to show and no way to
   * look until the runtime reconnects.
   */
  | "offline_no_snapshot"
  /** A read that completed or returned without ever producing a date. */
  | "no_observation"
  /** The read reached a terminal failure and carries the reason. */
  | "failed";

export interface HookObservationView {
  kind: HookObservationKind;
  /** Never null for `live` or `last_known`: those two are dated by definition. */
  observedAt: string | null;
  stale: boolean;
  /**
   * Whether the runtime was offline when the server answered. Server-reported,
   * never inferred from the client's own runtime row, which can be staler than
   * the answer it is framing.
   */
  offline: boolean;
  error: string | null;
  resolutionError: string | null;
}

/**
 * Snapshot invariant 3 — never render cached state undated. Every kind that
 * shows hook rows carries `observedAt`, and a runtime with no prior
 * observation lands on `no_observation`, which is an explicit failure rather
 * than an undated empty cache.
 *
 * The `isFetching` argument is load-bearing, not a convenience. `gcTime: 0`
 * collects one macrotask late, so a same-tick remount can briefly hand back
 * the previous mount's observation; deciding from `isFetching` first means
 * that value is never presented as this mount's answer.
 */
export function hookObservationView(
  data: RuntimeHookReadRequest | undefined,
  isFetching: boolean,
  queryError: unknown,
  now: number,
): HookObservationView {
  if (isFetching) {
    return {
      kind: "discovering",
      observedAt: null,
      stale: false,
      offline: false,
      error: null,
      resolutionError: null,
    };
  }
  if (queryError) {
    return {
      kind: "failed",
      observedAt: null,
      stale: false,
      offline: false,
      error: queryError instanceof Error ? queryError.message : null,
      resolutionError: null,
    };
  }
  if (!data) {
    return {
      kind: "no_observation",
      observedAt: null,
      stale: false,
      offline: false,
      error: null,
      resolutionError: null,
    };
  }
  const resolutionError = data.resolved?.error ?? null;
  const observedAt = data.observed_at ?? null;
  // A cached answer is an offline answer by construction, so the two agree on
  // every server that reports both. Taking either as offline keeps the reading
  // right on a backend that predates the `offline` field.
  const offline = data.offline || data.cached;
  if (data.status !== "completed") {
    // Offline with nothing stored is the one non-completed answer that is not
    // a failure. It is the honest state of an unreachable runtime Multica has
    // never read, and it says so instead of blaming the read.
    return {
      kind: offline && !observedAt ? "offline_no_snapshot" : "failed",
      observedAt,
      stale: false,
      offline,
      error: data.error ?? null,
      resolutionError,
    };
  }
  if (!observedAt) {
    return {
      kind: offline ? "offline_no_snapshot" : "no_observation",
      observedAt: null,
      stale: false,
      offline,
      error: data.error ?? null,
      resolutionError,
    };
  }
  const parsed = Date.parse(observedAt);
  const stale =
    Number.isNaN(parsed) || now - parsed > HOOK_OBSERVATION_STALE_AFTER_MS;
  return {
    kind: data.cached ? "last_known" : "live",
    observedAt,
    // A cached observation is always last-known, whatever its age, so the
    // staleness flag only has to add the age warning a live read can also need.
    stale,
    offline,
    error: null,
    resolutionError,
  };
}

/** Whether this view state may show hook rows at all. */
export function hookObservationShowsEntries(
  view: HookObservationView,
): boolean {
  return view.kind === "live" || view.kind === "last_known";
}

// ---------------------------------------------------------------------------
// The per-event answer. Everything below is shape and arithmetic over the
// server's EventAnswer; no matcher is evaluated here and no provider rule is
// decided here. `answerable` is the server's own flag and is never inferred
// from an empty set — an empty `matched` on an unanswerable event means
// Multica established nothing, not that nothing runs.

export type HookAnswerKind =
  /** No value supplied yet. Nothing about outcomes may be claimed. */
  | "idle"
  /** A request is in flight; the previous answer is no longer this one. */
  | "asking"
  /** The server has no observation to answer from. */
  | "unobserved"
  /**
   * The request failed, the payload was a parse refusal, or the answer
   * arrived with no observation date behind it.
   */
  | "failed"
  /** The server answered that it cannot answer this event. */
  | "unanswerable"
  /** A real answer. */
  | "answered";

export interface HookAnswerView {
  kind: HookAnswerKind;
  /** Non-null only for `unanswerable` and `answered`. */
  answer: RuntimeHookEventAnswer | null;
  /** The observation the answer was computed from. Null unless dated. */
  observedAt: string | null;
  cached: boolean;
  error: string | null;
  /**
   * The answer was computed from a different observation than the rows on
   * screen. The read endpoint and the answer endpoint both go to the stored
   * snapshot, so a re-read between them can move it; saying so is cheaper
   * than presenting two observations as one.
   */
  observationMismatch: boolean;
}

const IDLE_ANSWER: HookAnswerView = {
  kind: "idle",
  answer: null,
  observedAt: null,
  cached: false,
  error: null,
  observationMismatch: false,
};

/**
 * The single gate on whether an answer may be shown, mirroring
 * `hookObservationView`'s role for the observation. Decide from `isFetching`
 * first for the same reason: `gcTime: 0` collects one macrotask late, so a
 * value from the previous event or value can briefly survive and must never
 * be presented as the answer to the current one.
 */
export function hookAnswerView(
  data: RuntimeHookEventAnswerResult | undefined,
  isFetching: boolean,
  queryError: unknown,
  requestedValue: string,
  tabObservedAt: string | null,
): HookAnswerView {
  if (requestedValue.trim().length === 0) return IDLE_ANSWER;
  if (isFetching) return { ...IDLE_ANSWER, kind: "asking" };
  if (queryError) {
    return {
      ...IDLE_ANSWER,
      kind: "failed",
      error: queryError instanceof Error ? queryError.message : null,
    };
  }
  if (!data) return IDLE_ANSWER;

  const observedAt = data.observed_at ?? null;
  const cached = data.cached === true;
  if (!data.answer) {
    // No answer object at all: either nothing has been read from the host, or
    // the payload did not parse. Both are refusals, and neither is an event
    // with nothing on it.
    return {
      ...IDLE_ANSWER,
      kind: observedAt ? "failed" : "unobserved",
      observedAt,
      cached,
      error: data.error ?? null,
    };
  }
  if (!observedAt) {
    // An answer with no observation date is a refusal too, and for the same
    // reason as the branch above: counts and sets would be a claim about a
    // host state nobody dated. Today's server cannot produce this body, but
    // an installed client meets newer backends, so the shape is refused here
    // rather than trusted. The tab's own observation is deliberately not
    // borrowed as a date — it is a different read.
    return {
      ...IDLE_ANSWER,
      kind: "failed",
      cached,
      error: data.error ?? data.answer.error ?? null,
    };
  }
  const mismatch = Boolean(tabObservedAt && observedAt && tabObservedAt !== observedAt);
  if (data.answer.answerable !== true) {
    return {
      kind: "unanswerable",
      answer: data.answer,
      observedAt,
      cached,
      error: data.answer.error ?? data.error ?? null,
      observationMismatch: mismatch,
    };
  }
  return {
    kind: "answered",
    answer: data.answer,
    observedAt,
    cached,
    error: null,
    observationMismatch: mismatch,
  };
}

export interface HookAnswerTotals {
  /** Every entry the answer accounts for, each counted exactly once. */
  configured: number;
  /** Matched, live, and the provider's verdict is will_run. These run. */
  runs: number;
  /** Matched, but whether the provider acts depends on host-recorded trust. */
  trustUnconfirmed: number;
  /** Matched, but the provider parses the entry and skips it. */
  providerSkips: number;
  /** Live configuration this value does not satisfy. */
  notMatched: number;
  /**
   * Live configuration the provider's pre-matcher filter excluded before any
   * matcher ran. Not every entry the provider will not act on: one it parses
   * and skips is in `matched`, counted as `providerSkips`.
   */
  neverRuns: number;
  /** Parked or disabled, so the provider never sees it. */
  excluded: number;
  /**
   * Definitions the provider's own cross-source rule absorbed: the extra
   * copies, not the rows that survived them. Three byte-identical copies of
   * one handler are one row carrying three sources and two absorbed
   * definitions, and two is the number the chip states. A deduplication of
   * one definition seen more than once, never one source winning.
   */
  collapsed: number;
}

/**
 * The server-projected identities represented by one answer row. The fallback
 * only keeps an installed client usable with an older backend: it repeats the
 * surviving row identity once per source because that wire shape did not
 * retain the identities the backend discarded.
 */
export function hookAnswerMembers(entry: RuntimeHookEntry): RuntimeHookMember[] {
  if (entry.members && entry.members.length > 0) return entry.members;
  return entry.sources.map((source) => ({
    hook_id: entry.hook_id,
    occurrence: 0,
    matcher: entry.matcher,
    matcher_kind: entry.matcher_kind,
    matcher_error: entry.matcher_error,
    source,
  }));
}

/**
 * Each entry lands in exactly one of the answer's four sets, and within the
 * matched set each entry has exactly one effectiveness, so nothing below
 * counts an entry twice — including a parked entry that would also never run,
 * which is counted once, as `excluded`, while still showing both its axes.
 */
export function hookAnswerTotals(
  answer: RuntimeHookEventAnswer | null,
): HookAnswerTotals {
  const matched = answer?.matched ?? [];
  const notMatched = answer?.not_matched ?? [];
  const neverRuns = answer?.never_runs ?? [];
  const excluded = answer?.configuration_excluded ?? [];
  return {
    configured:
      matched.length + notMatched.length + neverRuns.length + excluded.length,
    runs: matched.filter(entryWillRun).length,
    trustUnconfirmed: matched.filter(
      (entry) => entry.effectiveness === "trust_unknown",
    ).length,
    providerSkips: matched.filter(
      (entry) => entry.effectiveness === "never_runs",
    ).length,
    notMatched: notMatched.length,
    neverRuns: neverRuns.length,
    excluded: excluded.length,
    collapsed: matched.reduce(
      (total, entry) => total + Math.max(0, hookAnswerMembers(entry).length - 1),
      0,
    ),
  };
}

/** The events the observation actually configured, for the event picker. */
export function hookAnsweredEvents(entries: RuntimeHookEntry[]): string[] {
  return [...new Set(entries.map((entry) => entry.event))].sort((left, right) =>
    left.localeCompare(right),
  );
}
