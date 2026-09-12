"use client";

import { useMemo } from "react";
import { CircleHelp, FileClock, Inbox } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { Badge } from "@multica/ui/components/ui/badge";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { runtimeHookFiresOptions } from "@multica/core/runtimes/queries";
import type { AgentRuntime, RuntimeHookFire } from "@multica/core/types";
import {
  countProvenance,
  exitCodeOf,
  hookNameOf,
  outcomeKind,
  provenanceKind,
  providerOutcomeOf,
  unknownOutcomeCause,
  type OutcomeKind,
  type ProvenanceKind,
} from "./hook-fires";
import { useT, useTimeAgo } from "../../i18n";

const FEED_LIMIT = 100;

// ---------------------------------------------------------------------------
// Provenance visuals.
//
// `inferred` is the MAJORITY case — stage 7's audit measured 101 of 144 real
// responses (70.1%) as inferred, because 66% of hook responses print nothing
// on stdout and can never be matched to a host timestamp. So it is styled as
// normal and expected, NOT as a warning: a destructive or warning tone here
// would tell the reader that seven rows in ten are broken, which is false.
// What separates the two is the icon, the label and the explanation, never
// an alarm colour.
//
// `unrecognized` is the only one that gets a cautionary tone, because it is
// the one case where Multica genuinely cannot characterise the timestamp.
// ---------------------------------------------------------------------------
const PROVENANCE_VISUAL: Record<
  ProvenanceKind,
  { Icon: typeof FileClock; tone: string; chip: string }
> = {
  debug_log: {
    Icon: FileClock,
    tone: "text-foreground",
    chip: "border-border bg-background text-foreground",
  },
  inferred: {
    Icon: Inbox,
    tone: "text-muted-foreground",
    chip: "border-border bg-muted/50 text-muted-foreground",
  },
  unrecognized: {
    Icon: CircleHelp,
    tone: "text-warning",
    chip: "border-warning/40 bg-warning/10 text-warning",
  },
};

// Outcome tone. `unknown` stays neutral: it is an honest, common state (66.7%
// of all non-success outcomes in the measured corpus) and not a failure, so
// painting it red would overstate what the data says.
const OUTCOME_VARIANT: Record<
  OutcomeKind,
  "secondary" | "destructive" | "warning" | "outline"
> = {
  success: "secondary",
  failure: "destructive",
  blocked: "warning",
  skipped: "outline",
  unknown: "outline",
  unrecognized: "outline",
};

const OUTCOME_TONE: Record<OutcomeKind, string> = {
  success: "bg-success/10 text-success",
  failure: "",
  blocked: "",
  skipped: "text-muted-foreground",
  unknown: "text-muted-foreground",
  unrecognized: "text-muted-foreground",
};

function useProvenanceCopy() {
  const { t } = useT("runtimes");
  return (kind: ProvenanceKind) => ({
    label: t(($) => $.hook_fires.provenance[kind].label),
    meaning: t(($) => $.hook_fires.provenance[kind].meaning),
  });
}

/**
 * One definition row in the always-visible provenance key.
 *
 * "Say what `inferred` means where the reader sees it. Not a footnote." So
 * this block is rendered expanded, above the rows, every time the feed has
 * data — not behind a tooltip, a popover or a "learn more". The per-row chip
 * then carries the short label, and the two are tied together by showing the
 * stored column value in both places.
 */
function ProvenanceDefinition({ kind }: { kind: ProvenanceKind }) {
  const copy = useProvenanceCopy()(kind);
  const { Icon, tone } = PROVENANCE_VISUAL[kind];
  return (
    <li className="flex gap-2">
      <Icon className={`mt-0.5 h-3.5 w-3.5 shrink-0 ${tone}`} aria-hidden="true" />
      <p className="text-caption text-muted-foreground">
        <span className="font-medium text-foreground">{copy.label}</span>
        {kind === "unrecognized" ? null : (
          <code className="ml-1.5 rounded bg-muted px-1 font-mono text-micro text-muted-foreground">
            {kind}
          </code>
        )}
        <span className="mx-1.5 text-faint-foreground">·</span>
        {copy.meaning}
      </p>
    </li>
  );
}

/**
 * Per-row provenance chip.
 *
 * `fire.provenance` is rendered VERBATIM in the `data-provenance` attribute
 * and, for recognised values, as the visible `<code>` label. Nothing here
 * derives it: `provenanceKind` is exact-equality classification only, and an
 * unrecognised value takes the unrecognised branch rather than the nearest
 * plausible member. See DP-LOCO-114-03 condition 2.
 */
function ProvenanceChip({ fire }: { fire: RuntimeHookFire }) {
  const kind = provenanceKind(fire.provenance);
  const copy = useProvenanceCopy()(kind);
  const { Icon, chip } = PROVENANCE_VISUAL[kind];
  return (
    <span
      data-provenance={fire.provenance}
      title={copy.meaning}
      className={`inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-micro font-medium ${chip}`}
    >
      <Icon className="h-3 w-3 shrink-0" aria-hidden="true" />
      {copy.label}
      <code className="font-mono text-micro opacity-70">{fire.provenance}</code>
    </span>
  );
}

/**
 * The `unknown` outcome has two real causes and neither is self-explanatory,
 * so a row that says `unknown` says why on the same row. Derived from stored
 * `detail` only — this explains an outcome and never touches provenance.
 */
function UnknownOutcomeNote({ fire }: { fire: RuntimeHookFire }) {
  const { t } = useT("runtimes");
  const cause = unknownOutcomeCause(fire);
  const exitCode = exitCodeOf(fire);
  if (cause === "spawn_ambiguous" && exitCode !== null) {
    return (
      <p className="text-caption text-muted-foreground">
        {t(($) => $.hook_fires.unknown_cause.spawn_ambiguous, { code: exitCode })}
      </p>
    );
  }
  if (cause === "provider_state") {
    return (
      <p className="text-caption text-muted-foreground">
        {t(($) => $.hook_fires.unknown_cause.provider_state, {
          state: providerOutcomeOf(fire) ?? "",
        })}
      </p>
    );
  }
  if (cause === "no_exit_code") {
    return (
      <p className="text-caption text-muted-foreground">
        {t(($) => $.hook_fires.unknown_cause.no_exit_code)}
      </p>
    );
  }
  return (
    <p className="text-caption text-muted-foreground">
      {t(($) => $.hook_fires.unknown_cause.unclassified)}
    </p>
  );
}

function HookFireRow({ fire }: { fire: RuntimeHookFire }) {
  const { t } = useT("runtimes");
  const timeAgo = useTimeAgo();
  const kind = outcomeKind(fire.outcome);
  const hookName = hookNameOf(fire);
  const exitCode = exitCodeOf(fire);

  return (
    <li
      data-testid="hook-fire-row"
      className="flex flex-col gap-1.5 border-t px-1 py-3 first:border-t-0"
    >
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <span className="font-mono text-caption font-medium text-foreground">
          {fire.event}
        </span>
        <span className="text-faint-foreground">·</span>
        <span
          className={`truncate text-caption ${
            hookName === null ? "italic text-muted-foreground" : "text-foreground"
          }`}
        >
          {hookName ?? t(($) => $.hook_fires.row.hook_unknown)}
        </span>

        <span className="ml-auto flex items-center gap-2">
          <Badge
            variant={OUTCOME_VARIANT[kind]}
            className={OUTCOME_TONE[kind]}
            data-outcome={fire.outcome}
          >
            {t(($) => $.hook_fires.outcome[kind])}
          </Badge>
          {exitCode !== null && (
            <span className="font-mono text-micro text-muted-foreground">
              {t(($) => $.hook_fires.row.exit_code, { code: exitCode })}
            </span>
          )}
        </span>
      </div>

      {/* Time and provenance sit on the same line on purpose: the timestamp
          means something different depending on the chip beside it, so the
          reader should never be able to read one without the other. */}
      <div className="flex flex-wrap items-center gap-2">
        <time
          dateTime={fire.fired_at}
          title={fire.fired_at}
          className="text-caption text-muted-foreground tabular-nums"
        >
          {timeAgo(fire.fired_at)}
        </time>
        <ProvenanceChip fire={fire} />
      </div>

      {kind === "unknown" && <UnknownOutcomeNote fire={fire} />}

      {/* Compact by design. What execution_id is NOT is stated once, in the
          key block above the rows — repeating the caveat on every row buried
          the provenance copy it sits beside, which is the copy that matters. */}
      <p className="text-micro text-faint-foreground">
        {t(($) => $.hook_fires.row.execution_ref)}
        <span className="mx-1 text-faint-foreground">·</span>
        <code className="font-mono">{fire.execution_id}</code>
      </p>
    </li>
  );
}

/**
 * Hook-fire feed for a runtime (LOCO-135).
 *
 * Flat and chronological by design. There is no grouping anywhere in this
 * component: `execution_id` is the provider's per-execution reference, fresh
 * on every fire, and grouping or deduping on it is foreclosed
 * (DP-LOCO-114-02 item 3). A flat projection has no grouping to get wrong.
 *
 * What this screen is obliged to be honest about, and how it is:
 *
 *  - Provenance per row, never per provider. Every row carries its own chip;
 *    there is no feed-level "observed" label anywhere.
 *  - What `inferred` means, where the reader sees it. The provenance key is
 *    rendered expanded above the rows, not in a tooltip or a footnote.
 *  - `unknown`, explained on the rows that have it, with its actual cause.
 *  - `skipped` is unreachable on Claude, so nothing here advertises a
 *    "never ran" state. Outcome labels are only ever rendered from a row's
 *    own stored value; there is no legend enumerating possible outcomes.
 */
export function HookFiresSection({ runtime }: { runtime: AgentRuntime }) {
  const { t } = useT("runtimes");
  const { data, isLoading, isError } = useQuery(
    runtimeHookFiresOptions(runtime.id, FEED_LIMIT),
  );

  // Memoized because `data?.fires ?? []` allocates a fresh array on every
  // render, which would make the tally memo below recompute unconditionally.
  const fires = useMemo(() => data?.fires ?? [], [data]);
  const counts = useMemo(() => countProvenance(fires), [fires]);
  const total = fires.length;

  return (
    <section className="rounded-lg border bg-card p-4">
      <div className="mb-3 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h4 className="text-body font-semibold">
            {t(($) => $.hook_fires.title)}
          </h4>
          <p className="mt-0.5 text-caption text-muted-foreground">
            {t(($) => $.hook_fires.subtitle)}
          </p>
        </div>
        {total > 0 && (
          <div className="flex flex-wrap items-center gap-1.5">
            {counts.debug_log > 0 && (
              <span className="rounded-md border bg-background px-1.5 py-0.5 text-micro font-medium text-foreground">
                {t(($) => $.hook_fires.counts_debug_log, {
                  count: counts.debug_log,
                })}
              </span>
            )}
            {counts.inferred > 0 && (
              <span className="rounded-md border bg-muted/50 px-1.5 py-0.5 text-micro font-medium text-muted-foreground">
                {t(($) => $.hook_fires.counts_inferred, {
                  count: counts.inferred,
                })}
              </span>
            )}
            {counts.unrecognized > 0 && (
              <span className="rounded-md border border-warning/40 bg-warning/10 px-1.5 py-0.5 text-micro font-medium text-warning">
                {t(($) => $.hook_fires.counts_unrecognized, {
                  count: counts.unrecognized,
                })}
              </span>
            )}
          </div>
        )}
      </div>

      {isLoading && (
        <div className="space-y-2" aria-label={t(($) => $.hook_fires.loading)}>
          <Skeleton className="h-12 w-full" />
          <Skeleton className="h-12 w-full" />
          <Skeleton className="h-12 w-full" />
        </div>
      )}

      {!isLoading && isError && (
        <p className="py-6 text-center text-caption text-muted-foreground">
          {t(($) => $.hook_fires.error)}
        </p>
      )}

      {!isLoading && !isError && total === 0 && (
        <div className="py-6 text-center">
          <p className="text-caption text-foreground">
            {t(($) => $.hook_fires.empty)}
          </p>
          <p className="mx-auto mt-1 max-w-prose text-caption text-muted-foreground">
            {t(($) => $.hook_fires.empty_hint)}
          </p>
        </div>
      )}

      {!isLoading && !isError && total > 0 && (
        <>
          {/* The provenance key. Expanded, above the data, always. */}
          <div
            data-testid="hook-fire-provenance-key"
            className="mb-3 rounded-md border bg-muted/30 p-3"
          >
            <h5 className="text-caption font-semibold text-foreground">
              {t(($) => $.hook_fires.how_we_know_title)}
            </h5>
            <p className="mt-1 text-caption text-muted-foreground">
              {t(($) => $.hook_fires.how_we_know_intro)}
            </p>
            <ul className="mt-2 space-y-1.5">
              <ProvenanceDefinition kind="debug_log" />
              <ProvenanceDefinition kind="inferred" />
              {counts.unrecognized > 0 && (
                <ProvenanceDefinition kind="unrecognized" />
              )}
            </ul>
            {counts.inferred > 0 && (
              <p className="mt-2 border-t pt-2 text-caption font-medium text-foreground">
                {t(($) => $.hook_fires.mix_summary, {
                  inferred: counts.inferred,
                  total,
                })}
              </p>
            )}
            {/* execution_id is a per-execution reference, not configured-hook
                identity (DP-LOCO-114-02 item 3). Said once, here, where the
                reader is already being told how to read these rows. */}
            <p className="mt-2 text-caption text-muted-foreground">
              <span className="font-medium text-foreground">
                {t(($) => $.hook_fires.row.execution_ref)}
              </span>
              <span className="mx-1.5 text-faint-foreground">·</span>
              {t(($) => $.hook_fires.execution_ref_note)}
            </p>
          </div>

          <ul className="divide-border">
            {fires.map((fire) => (
              <HookFireRow key={fire.id} fire={fire} />
            ))}
          </ul>

          {data?.truncated === true && (
            <p className="mt-3 border-t pt-3 text-caption text-muted-foreground">
              {t(($) => $.hook_fires.truncated, { count: data.limit })}
            </p>
          )}
        </>
      )}
    </section>
  );
}
