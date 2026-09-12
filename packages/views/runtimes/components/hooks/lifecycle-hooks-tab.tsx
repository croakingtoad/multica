"use client";

import { useMemo, useState } from "react";
import { RefreshCw, Search, TriangleAlert, Webhook } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { AgentRuntime, RuntimeHookEntry } from "@multica/core/types";
import { runtimeHooksKeys, runtimeHooksOptions } from "@multica/core/runtimes";
import { Button } from "@multica/ui/components/ui/button";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@multica/ui/components/ui/empty";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { useT, useTimeAgo } from "../../../i18n";
import { HookDetailDialog } from "./hook-detail-dialog";
import { HookEventSection, matchesHookQuery } from "./hook-event-section";
import { BehaviourStrip, ObservationBanner } from "./hook-observation-banner";
import { ScopeSourcesCard } from "./hook-source-list";
import { HookSummaryCard } from "./hook-summary-card";
import {
  hookEventGroups,
  hookObservationShowsEntries,
  hookObservationView,
  hookScopeRows,
  hookTotals,
  providerSupportsHooks,
} from "./hooks-model";

// Lifecycle Hooks tab. Read-only: this pass adds no write path, no park or
// disable control, and no code or JSON editor.
//
// Structure LOCO-127 (what-actually-runs) and LOCO-128 (empty / offline /
// discovery states) extend:
//
//   LifecycleHooksTab      owns the query, the observation gate and the layout
//   ├─ ObservationBanner   every not-showing-entries state lands here
//   ├─ BehaviourStrip      the provider's merge rule, stated per provider
//   ├─ ScopeSourcesCard    one row per expected source, three distinct states
//   ├─ HookEventSection    one collapsible group per event, with its counts
//   │  └─ HookRow          one entry: matcher, handler, source, both axes
//   ├─ HookSummaryCard     runtime-level totals, in the right rail
//   └─ HookDetailDialog    per-entry read-only detail, on the Dialog surface
//
// Derivation lives in hooks-model.ts; per-state labels live in
// hook-state-badges.tsx. Both are shared, so a new screen adds a layout, not
// a second opinion about what a state means.
//
// Two rules hold across all of them. Nothing claims a hook is shadowed,
// overridden or displaced — no entry on either provider is ever displaced by
// another layer. And nothing renders an observation without its date.

export function LifecycleHooksTab({ runtime }: { runtime: AgentRuntime }) {
  const { t } = useT("runtimes");
  const timeAgo = useTimeAgo();
  const queryClient = useQueryClient();
  const supported = providerSupportsHooks(runtime.provider);
  const [query, setQuery] = useState("");
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const [selected, setSelected] = useState<RuntimeHookEntry | null>(null);

  // staleTime and gcTime come from runtimeHooksOptions and are deliberately
  // not overridden here — read the comment above them before touching either.
  // Snapshot invariant 1 depends on both being 0.
  const { data, error, isFetching } = useQuery(
    runtimeHooksOptions(supported ? runtime.id : null),
  );

  // `Date.now()` is read once per render rather than held in state: its only
  // consumer is the staleness comparison, and a ticking clock would re-render
  // the whole tab to move a label the next read replaces anyway.
  const view = useMemo(
    () => hookObservationView(data, isFetching, error, Date.now()),
    [data, isFetching, error],
  );
  const showsEntries = hookObservationShowsEntries(view);
  const entries = useMemo(
    () => (showsEntries ? (data?.resolved?.entries ?? []) : []),
    [showsEntries, data?.resolved?.entries],
  );
  const scopes = useMemo(
    () =>
      showsEntries ? hookScopeRows(runtime.provider, data?.sources, entries) : [],
    [showsEntries, runtime.provider, data?.sources, entries],
  );
  const groups = useMemo(() => hookEventGroups(entries), [entries]);
  const totals = useMemo(() => hookTotals(entries, scopes), [entries, scopes]);
  const unrecognized = data?.resolved?.unrecognized_keys ?? [];

  const needle = query.trim().toLowerCase();
  const filtered = needle
    ? groups
        .map((group) => ({
          ...group,
          entries: group.entries.filter((entry) =>
            matchesHookQuery(entry, needle),
          ),
        }))
        .filter((group) => group.entries.length > 0)
    : groups;

  if (!supported) {
    return (
      <Empty className="border border-dashed">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <Webhook aria-hidden="true" />
          </EmptyMedia>
          <EmptyTitle>{t(($) => $.hooks.unsupported_title)}</EmptyTitle>
          <EmptyDescription>
            {t(($) => $.hooks.unsupported_body)}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  const reread = () =>
    queryClient.invalidateQueries({
      queryKey: runtimeHooksKeys.forRuntime(runtime.id),
    });

  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-[minmax(0,1fr)_300px]">
      <div className="min-w-0 space-y-4">
        <ObservationBanner
          view={view}
          runtimeName={runtime.name}
          timeAgo={timeAgo}
          onReread={reread}
        />

        {showsEntries ? (
          <>
            <BehaviourStrip provider={runtime.provider} />
            <ScopeSourcesCard scopes={scopes} />
            {unrecognized.length > 0 ? (
              <div className="rounded-lg border border-warning/40 bg-warning/5 p-3">
                <p className="flex items-center gap-2 text-label font-medium">
                  <TriangleAlert
                    aria-hidden="true"
                    className="h-3.5 w-3.5 shrink-0 text-foreground"
                  />
                  {t(($) => $.hooks.unrecognized.title, {
                    count: unrecognized.length,
                  })}
                </p>
                <p className="mt-1 text-caption text-muted-foreground">
                  {t(($) => $.hooks.unrecognized.body)}
                </p>
                <ul className="mt-2 space-y-0.5">
                  {unrecognized.map((item) => (
                    <li
                      key={`${item.source.scope}:${item.source.format}:${item.key}`}
                      className="font-mono text-caption text-muted-foreground"
                    >
                      {t(($) => $.hooks.unrecognized.entry, {
                        scope: item.source.scope,
                        format: item.source.format,
                        key: item.key,
                      })}
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}

            {entries.length === 0 ? (
              <Empty className="border border-dashed">
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <Webhook aria-hidden="true" />
                  </EmptyMedia>
                  <EmptyTitle>{t(($) => $.hooks.events.empty_title)}</EmptyTitle>
                  <EmptyDescription>
                    {t(($) => $.hooks.events.empty_body)}
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <>
                <div className="relative max-w-sm">
                  <Label htmlFor="hook-filter" className="sr-only">
                    {t(($) => $.hooks.events.filter_label)}
                  </Label>
                  <Search
                    aria-hidden="true"
                    className="pointer-events-none absolute top-1/2 left-2.5 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground"
                  />
                  <Input
                    id="hook-filter"
                    value={query}
                    onChange={(event) => setQuery(event.target.value)}
                    placeholder={t(($) => $.hooks.events.filter_placeholder)}
                    className="pl-8"
                  />
                </div>

                {filtered.length === 0 ? (
                  <Empty className="border border-dashed">
                    <EmptyHeader>
                      <EmptyMedia variant="icon">
                        <Search aria-hidden="true" />
                      </EmptyMedia>
                      <EmptyTitle>
                        {t(($) => $.hooks.events.filter_empty_title)}
                      </EmptyTitle>
                      <EmptyDescription>
                        {t(($) => $.hooks.events.filter_empty_body)}
                      </EmptyDescription>
                    </EmptyHeader>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={() => setQuery("")}
                    >
                      {t(($) => $.hooks.events.filter_clear)}
                    </Button>
                  </Empty>
                ) : (
                  <div className="space-y-3">
                    {filtered.map((group) => (
                      <HookEventSection
                        key={group.event}
                        group={group}
                        open={!collapsed[group.event]}
                        onToggle={() =>
                          setCollapsed((previous) => ({
                            ...previous,
                            [group.event]: !previous[group.event],
                          }))
                        }
                        onSelect={setSelected}
                      />
                    ))}
                  </div>
                )}
              </>
            )}
          </>
        ) : null}
      </div>

      <div className="space-y-4">
        {showsEntries ? <HookSummaryCard totals={totals} /> : null}
        {/* Only for a live read: a cached snapshot already reads as last
            known in the banner, and repeating it here would say it twice. */}
        {view.kind === "live" && view.stale ? (
          <p className="flex items-start gap-2 rounded-lg border border-warning/40 bg-warning/5 p-3 text-caption text-muted-foreground">
            <RefreshCw aria-hidden="true" className="mt-0.5 h-3 w-3 shrink-0" />
            {t(($) => $.hooks.observation.live_stale, {
              when: view.observedAt ? timeAgo(view.observedAt) : "",
            })}
          </p>
        ) : null}
      </div>

      <HookDetailDialog
        entry={selected}
        provider={runtime.provider}
        onOpenChange={(open) => {
          if (!open) setSelected(null);
        }}
      />
    </div>
  );
}
