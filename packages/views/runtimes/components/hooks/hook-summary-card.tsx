"use client";

import { Separator } from "@multica/ui/components/ui/separator";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../../i18n";
import type { hookTotals } from "./hooks-model";

/**
 * Runtime-level totals, in two blocks that are established by different
 * things. The source-state rows come from the host's diff of expected against
 * observed and survive a parse failure untouched. The entry-derived rows come
 * from the projection, so when resolution failed they are either a floor or
 * nothing at all — `entryCoverage` says which, and a number is only printed
 * for a count Multica actually established.
 */
export function HookSummaryCard({
  totals,
}: {
  totals: ReturnType<typeof hookTotals>;
}) {
  const { t } = useT("runtimes");
  // Null means "not established" and prints as a phrase, never as a zero.
  const established = totals.entryCoverage !== "unestablished";
  const derived = (value: number): number | null => (established ? value : null);
  const rows: { label: string; value: number | null; tone?: string }[] = [
    {
      label: t(($) => $.hooks.summary.configured),
      value: derived(totals.configured),
    },
    {
      label: t(($) => $.hooks.summary.will_run),
      value: derived(totals.willRun),
      tone: "text-success",
    },
    {
      label: t(($) => $.hooks.summary.inactive),
      value: derived(totals.inactive),
    },
    {
      label: t(($) => $.hooks.summary.never_runs),
      value: derived(totals.neverRuns),
      tone: established && totals.neverRuns > 0 ? "text-destructive" : undefined,
    },
    {
      label: t(($) => $.hooks.summary.trust_unknown),
      value: derived(totals.trustUnknown),
      // No amber here: `--warning` is 2.25:1 as text on the light surface.
      // The label carries the meaning, so the count stays on the foreground.
      tone: undefined,
    },
    {
      label: t(($) => $.hooks.summary.unevaluable),
      value: derived(totals.unevaluableMatchers),
      tone: undefined,
    },
    { label: t(($) => $.hooks.summary.events), value: derived(totals.events) },
    { label: t(($) => $.hooks.summary.sources_found), value: totals.foundSources },
    {
      label: t(($) => $.hooks.summary.sources_not_checked),
      value: totals.notCheckedSources,
    },
  ];

  return (
    <div className="rounded-lg border bg-card">
      <div className="border-b px-4 py-2.5">
        <h3 className="text-label font-medium">{t(($) => $.hooks.summary.title)}</h3>
      </div>
      <dl className="space-y-1.5 px-4 py-3">
        {rows.map((row) => (
          <div key={row.label} className="flex items-baseline justify-between gap-3">
            <dt className="text-caption text-muted-foreground">{row.label}</dt>
            {row.value === null ? (
              // The phrase itself, not a dash with a tooltip: a reader who
              // cannot hover, and a screen reader, get the same sentence.
              <dd className="text-caption text-right text-muted-foreground">
                {t(($) => $.hooks.summary.not_established)}
              </dd>
            ) : (
              <dd
                className={cn("font-mono text-label tabular-nums", row.tone ?? "")}
              >
                {row.value}
              </dd>
            )}
          </div>
        ))}
      </dl>
      {totals.entryCoverage !== "complete" ? (
        <>
          <Separator />
          <p className="px-4 py-3 text-caption text-muted-foreground">
            {totals.entryCoverage === "partial"
              ? t(($) => $.hooks.summary.partial_note)
              : t(($) => $.hooks.summary.unestablished_note)}
          </p>
        </>
      ) : null}
      <Separator />
      <p className="px-4 py-3 text-caption text-muted-foreground">
        {t(($) => $.hooks.summary.no_order_note)}
      </p>
    </div>
  );
}
