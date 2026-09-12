"use client";

import { useMemo, useState } from "react";
import { RefreshCw, Search, TriangleAlert, Webhook } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { AgentRuntime, RuntimeHookEntry } from "@multica/core/types";
import {
  runtimeHookAnswerKeys,
  runtimeHookAnswerOptions,
  runtimeHooksKeys,
  runtimeHooksOptions,
  type RuntimeHookDiscoveryProgress,
} from "@multica/core/runtimes";
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
import { type HookAnswerRequest, HookAnswerPanel } from "./hook-answer-panel";
import { HookDetailDialog } from "./hook-detail-dialog";
import { HookDiscoveryCard } from "./hook-discovery-card";
import { HookEmptyCard } from "./hook-empty-card";
import { HookEventSection, matchesHookQuery } from "./hook-event-section";
import { HookLastKnownRail, HookOfflineCard } from "./hook-offline-card";
import { BehaviourStrip, ObservationBanner } from "./hook-observation-banner";
import { ScopeSourcesCard } from "./hook-source-list";
import { HookSummaryCard } from "./hook-summary-card";
import {
  hookEmptySummary,
  hookAnsweredEvents,
  hookAnswerView,
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
//   ├─ HookDiscoveryCard   a read in flight, with the phase it has reached
//   ├─ HookOfflineCard     an offline runtime, with or without a snapshot
//   ├─ ObservationBanner   dates the observation; names undated states
//   ├─ BehaviourStrip      the provider's merge rule, stated per provider
//   ├─ HookAnswerPanel     what actually runs, for one event and one value
//   ├─ ScopeSourcesCard    one row per expected source, three distinct states
//   ├─ HookEmptyCard       sources read, no entries — and which files that is about
//   ├─ HookEventSection    one collapsible group per event, with its counts
//   │  └─ HookRow          one entry: matcher, handler, source, both axes
//   ├─ HookSummaryCard     runtime-level totals, in the right rail
//   ├─ HookLastKnownRail   the last-known caption, in the right rail
//   └─ HookDetailDialog    per-entry read-only detail, on the Dialog surface
//
// Derivation lives in hooks-model.ts; per-state labels live in
// hook-state-badges.tsx. Both are shared, so a new screen adds a layout, not
// a second opinion about what a state means.
//
// Three rules hold across all of them. Nothing claims a hook is shadowed,
// overridden or displaced — no entry on either provider is ever displaced by
// another layer. Nothing renders an observation without its date. And an
// in-flight read, an offline runtime and a runtime with no hooks are three
// different answers that never borrow each other's framing.

export function LifecycleHooksTab({ runtime }: { runtime: AgentRuntime }) {
  const { t } = useT("runtimes");
  const timeAgo = useTimeAgo();
  const queryClient = useQueryClient();
  const supported = providerSupportsHooks(runtime.provider);
  const [query, setQuery] = useState("");
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const [selected, setSelected] = useState<RuntimeHookEntry | null>(null);
  // Phase plus the bound the server reported for it. Held together because
  // the card renders them together and neither is derivable from the other.
  const [progress, setProgress] = useState<RuntimeHookDiscoveryProgress>({
    phase: "initiating",
    phaseTimeoutSeconds: null,
  });
  // Keyed by runtime rather than a bare boolean so switching runtimes cannot
  // carry one host's "yes, show me the stale copy" over to another's.
  const [revealedRuntimeId, setRevealedRuntimeId] = useState<string | null>(
    null,
  );
  // The answer panel's draft is separate from what was asked. A matcher is
  // evaluated against a value, so an answer must belong to the value that
  // produced it — not to whatever is currently in the field.
  const [draftEvent, setDraftEvent] = useState("");
  const [draftValue, setDraftValue] = useState("");
  const [asked, setAsked] = useState<HookAnswerRequest | null>(null);

  // staleTime and gcTime come from runtimeHooksOptions and are deliberately
  // not overridden here — read the comment above them before touching either.
  // Snapshot invariant 1 depends on both being 0.
  const { data, error, isFetching } = useQuery(
    runtimeHooksOptions(supported ? runtime.id : null, setProgress),
  );

  // `Date.now()` is read once per render rather than held in state: its only
  // consumer is the staleness comparison, and a ticking clock would re-render
  // the whole tab to move a label the next read replaces anyway.
  const view = useMemo(
    () => hookObservationView(data, isFetching, error, Date.now()),
    [data, isFetching, error],
  );
  const lastKnown = view.kind === "last_known";
  const revealed = revealedRuntimeId === runtime.id;
  // A last-known snapshot is offered behind an explicit reveal, so it arrives
  // as something the reader asked for rather than as the current state.
  const showsEntries =
    hookObservationShowsEntries(view) && (!lastKnown || revealed);
  const entries = useMemo(
    () => (showsEntries ? (data?.resolved?.entries ?? []) : []),
    [showsEntries, data?.resolved?.entries],
  );
  // Both derivations take the projection's own error: an entry count derived
  // without it states a number for a snapshot Multica may not have read.
  const scopes = useMemo(
    () =>
      showsEntries
        ? hookScopeRows(
            runtime.provider,
            data?.sources,
            entries,
            view.resolutionError,
          )
        : [],
    [showsEntries, runtime.provider, data?.sources, entries, view.resolutionError],
  );
  const groups = useMemo(() => hookEventGroups(entries), [entries]);
  const totals = useMemo(
    () => hookTotals(entries, scopes, view.resolutionError),
    [entries, scopes, view.resolutionError],
  );
  const emptySummary = useMemo(() => hookEmptySummary(scopes), [scopes]);
  const unrecognized = data?.resolved?.unrecognized_keys ?? [];
  const answerableEvents = useMemo(() => hookAnsweredEvents(entries), [entries]);
  // The event the picker is on. Falls back to the first configured event
  // rather than an empty selection, so the panel is usable without a click;
  // empty means the observation configured no events at all.
  const answerEvent = draftEvent || answerableEvents[0] || "";
  // The role travels with the projection, so the value field is captioned
  // correctly from first paint rather than only after an answer arrives.
  const answerValueRole =
    data?.resolved?.event_value_roles?.[answerEvent] ?? "unspecified";

  // staleTime and gcTime are 0 here too — see packages/core/runtimes/
  // hook-answer.ts. Nothing in this file overrides either.
  const {
    data: answerData,
    error: answerError,
    isFetching: answerFetching,
  } = useQuery(
    runtimeHookAnswerOptions(
      supported ? runtime.id : null,
      asked?.event ?? null,
      asked?.value ?? "",
    ),
  );
  const answerView = useMemo(
    () =>
      hookAnswerView(
        answerData,
        answerFetching,
        answerError,
        asked?.value ?? "",
        view.observedAt,
      ),
    [answerData, answerFetching, answerError, asked?.value, view.observedAt],
  );

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

  const reread = () => {
    // Both, together. An answer is computed from the snapshot, so leaving the
    // previous answer on screen beside a fresh read would show two
    // observations as one.
    queryClient.invalidateQueries({
      queryKey: runtimeHooksKeys.forRuntime(runtime.id),
    });
    queryClient.invalidateQueries({
      queryKey: runtimeHookAnswerKeys.forRuntime(runtime.id),
    });
  };

  const reask = () =>
    queryClient.invalidateQueries({
      queryKey: runtimeHookAnswerKeys.forRuntime(runtime.id),
    });

  const offlineGate =
    view.kind === "offline_no_snapshot" || (lastKnown && !revealed);

  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-[minmax(0,1fr)_300px]">
      <div className="min-w-0 space-y-4">
        {view.kind === "discovering" ? (
          <HookDiscoveryCard runtimeName={runtime.name} progress={progress} />
        ) : offlineGate ? (
          <HookOfflineCard
            runtimeName={runtime.name}
            lastSeenAt={runtime.last_seen_at}
            observedAt={view.observedAt}
            timeAgo={timeAgo}
            onReread={reread}
            onReveal={
              lastKnown ? () => setRevealedRuntimeId(runtime.id) : undefined
            }
          />
        ) : (
          <>
            <ObservationBanner
              view={view}
              runtimeName={runtime.name}
              timeAgo={timeAgo}
              onReread={reread}
            />

            {showsEntries ? (
              <>
                <BehaviourStrip provider={runtime.provider} />
                {/* Read after the merge rule and before the configured list: the
                    answer is a selection from that list, under that rule. */}
                {answerEvent ? (
                  <HookAnswerPanel
                    provider={runtime.provider}
                    events={answerableEvents}
                    draftEvent={answerEvent}
                    valueRole={answerValueRole}
                    draftValue={draftValue}
                    onDraftEventChange={setDraftEvent}
                    onDraftValueChange={setDraftValue}
                    onAsk={() =>
                      setAsked({ event: answerEvent, value: draftValue.trim() })
                    }
                    onReask={reask}
                    asked={asked}
                    view={answerView}
                    timeAgo={timeAgo}
                    tabObservedAt={view.observedAt}
                    notCheckedSources={totals.notCheckedSources}
                    onSelectEntry={setSelected}
                  />
                ) : null}
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

                {/* Zero entries has two causes and only one of them is
                    "there are none". When resolution failed, Multica did not
                    establish that the sources held no entries — it
                    established that it could not interpret them, which is the
                    rule server/internal/runtimehooks/projection.go states for
                    exactly this shape. So the error case replaces the whole
                    empty card rather than only its body: HookEmptyCard's
                    headline makes a claim about what was read too, and a
                    guard that swapped only the body would leave the stronger
                    sentence on screen. A partly-resolved read (error plus
                    entries) is not this branch and still renders its list. */}
                {entries.length === 0 ? (
                  view.resolutionError ? (
                    <Empty className="border border-dashed">
                      <EmptyHeader>
                        <EmptyMedia variant="icon">
                          <TriangleAlert aria-hidden="true" />
                        </EmptyMedia>
                        <EmptyTitle>
                          {t(($) => $.hooks.events.unresolved_title)}
                        </EmptyTitle>
                        <EmptyDescription>
                          {t(($) => $.hooks.events.unresolved_body)}
                        </EmptyDescription>
                      </EmptyHeader>
                    </Empty>
                  ) : (
                    <HookEmptyCard
                      runtimeName={runtime.name}
                      summary={emptySummary}
                      observedAt={view.observedAt}
                      timeAgo={timeAgo}
                    />
                  )
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
          </>
        )}
      </div>

      <div className="space-y-4">
        {/* Dated in the rail as well as the banner, so the last-known reading
            survives scrolling past the top of the page. */}
        {lastKnown && view.observedAt ? (
          <HookLastKnownRail observedAt={view.observedAt} timeAgo={timeAgo} />
        ) : null}
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
