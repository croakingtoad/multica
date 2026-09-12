"use client";

import { Separator } from "@multica/ui/components/ui/separator";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../../i18n";
import type { hookTotals } from "./hooks-model";

export function HookSummaryCard({
  totals,
}: {
  totals: ReturnType<typeof hookTotals>;
}) {
  const { t } = useT("runtimes");
  const rows: { label: string; value: number; tone?: string }[] = [
    { label: t(($) => $.hooks.summary.configured), value: totals.configured },
    {
      label: t(($) => $.hooks.summary.will_run),
      value: totals.willRun,
      tone: "text-success",
    },
    { label: t(($) => $.hooks.summary.inactive), value: totals.inactive },
    {
      label: t(($) => $.hooks.summary.never_runs),
      value: totals.neverRuns,
      tone: totals.neverRuns > 0 ? "text-destructive" : undefined,
    },
    {
      label: t(($) => $.hooks.summary.trust_unknown),
      value: totals.trustUnknown,
      // No amber here: `--warning` is 2.25:1 as text on the light surface.
      // The label carries the meaning, so the count stays on the foreground.
      tone: undefined,
    },
    {
      label: t(($) => $.hooks.summary.unevaluable),
      value: totals.unevaluableMatchers,
      tone: undefined,
    },
    { label: t(($) => $.hooks.summary.events), value: totals.events },
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
            <dd
              className={cn("font-mono text-label tabular-nums", row.tone ?? "")}
            >
              {row.value}
            </dd>
          </div>
        ))}
      </dl>
      <Separator />
      <p className="px-4 py-3 text-caption text-muted-foreground">
        {t(($) => $.hooks.summary.no_order_note)}
      </p>
    </div>
  );
}
