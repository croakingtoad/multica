import type {
  RuntimeHookEntry,
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
  /** Offline with nothing stored, or a read that never produced a date. */
  | "no_observation"
  /** The read reached a terminal failure and carries the reason. */
  | "failed";

export interface HookObservationView {
  kind: HookObservationKind;
  /** Never null for `live` or `last_known`: those two are dated by definition. */
  observedAt: string | null;
  stale: boolean;
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
      error: null,
      resolutionError: null,
    };
  }
  if (queryError) {
    return {
      kind: "failed",
      observedAt: null,
      stale: false,
      error: queryError instanceof Error ? queryError.message : null,
      resolutionError: null,
    };
  }
  if (!data) {
    return {
      kind: "no_observation",
      observedAt: null,
      stale: false,
      error: null,
      resolutionError: null,
    };
  }
  const resolutionError = data.resolved?.error ?? null;
  const observedAt = data.observed_at ?? null;
  if (data.status !== "completed") {
    return {
      kind: "failed",
      observedAt,
      stale: false,
      error: data.error ?? null,
      resolutionError,
    };
  }
  if (!observedAt) {
    return {
      kind: "no_observation",
      observedAt: null,
      stale: false,
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
