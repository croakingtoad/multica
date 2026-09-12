"use client";

import { useMemo, useState } from "react";
import {
  Archive,
  ChevronDown,
  CircleAlert,
  CircleHelp,
  CircleSlash,
  Copy,
  Eye,
  Play,
  RefreshCw,
  Search,
  Shuffle,
  TriangleAlert,
} from "lucide-react";
import type { RuntimeHookEntry } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../../i18n";
import { handlerTarget } from "./hook-event-section";
import {
  ConfigurationBadge,
  EffectivenessBadge,
  MatcherKindNote,
  ScopeBadge,
  TrustBadge,
  hookToneClass,
  hookToneIconClass,
  useHookValueRoleLabel,
  useNeverRunsReason,
} from "./hook-state-badges";
import {
  type HookAnswerTotals,
  type HookAnswerView,
  hookAnswerTotals,
} from "./hooks-model";

// The what-actually-runs panel.
//
// Three rules hold everywhere in this file, and every one of them is a thing
// the screen must not say:
//
//  1. Nothing claims an outcome without a value. A matcher is evaluated
//     against a value, so before the reader gives one this panel states
//     conditions — "these run if their matcher is satisfied" — and never a
//     set of handlers that fire.
//  2. "Multica cannot answer" is never rendered as "nothing runs". The
//     unanswerable state shows no counts and no sets at all, only the matcher
//     it could not evaluate and the engine's own message.
//  3. Nothing is ordered. Every set is a `<ul>`, nothing is numbered, and the
//     panel says in words that Multica does not know the order the provider
//     runs them in. Neither provider publishes one.
//
// Every state opinion — configuration, the provider's verdict, trust, matcher
// kind — comes from hook-state-badges.tsx, and every count from hooks-model.
// This file owns layout and copy, and forms no second opinion about meaning.

export interface HookAnswerRequest {
  event: string;
  value: string;
}

export function HookAnswerPanel({
  provider,
  events,
  draftEvent,
  valueRole,
  draftValue,
  onDraftEventChange,
  onDraftValueChange,
  onAsk,
  onReask,
  asked,
  view,
  timeAgo,
  tabObservedAt,
  notCheckedSources,
  onSelectEntry,
}: {
  provider: string;
  events: string[];
  draftEvent: string;
  /** The selected event's role, from the projection — not from an answer. */
  valueRole: string;
  draftValue: string;
  onDraftEventChange: (event: string) => void;
  onDraftValueChange: (value: string) => void;
  onAsk: () => void;
  onReask: () => void;
  asked: HookAnswerRequest | null;
  view: HookAnswerView;
  timeAgo: (value: string) => string;
  tabObservedAt: string | null;
  notCheckedSources: number;
  onSelectEntry: (entry: RuntimeHookEntry) => void;
}) {
  const { t } = useT("runtimes");
  const totals = useMemo(() => hookAnswerTotals(view.answer), [view.answer]);
  // The role of the event currently selected, not of the answered one: the
  // caption has to describe the value the reader is about to type, which is
  // why it comes from the projection rather than from an answer.
  const role = useHookValueRoleLabel(valueRole);

  return (
    <section
      aria-labelledby="hook-answer-title"
      className="overflow-hidden rounded-lg border bg-card"
    >
      <div className="flex flex-wrap items-start gap-2 border-b px-4 py-2.5">
        <Play aria-hidden="true" className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <div className="min-w-0 flex-1">
          <h3 id="hook-answer-title" className="text-label font-medium">
            {t(($) => $.hooks.answer.title)}
          </h3>
          <p className="mt-0.5 text-caption text-muted-foreground">
            {t(($) => $.hooks.answer.subtitle)}
          </p>
        </div>
      </div>

      <form
        className="space-y-3 border-b px-4 py-3"
        onSubmit={(event) => {
          event.preventDefault();
          onAsk();
        }}
      >
        <div className="flex flex-wrap items-end gap-3">
          <div className="min-w-[12rem] flex-1 space-y-1.5">
            <Label
              htmlFor="hook-answer-event"
              className="text-caption text-muted-foreground"
            >
              {t(($) => $.hooks.answer.event_label)}
            </Label>
            <Select
              items={events.map((event) => ({ value: event, label: event }))}
              value={draftEvent}
              onValueChange={(next) => onDraftEventChange(next ?? draftEvent)}
            >
              <SelectTrigger
                id="hook-answer-event"
                className="h-9 w-full rounded-md font-mono text-body"
              >
                <SelectValue>
                  {() => <span className="truncate">{draftEvent}</span>}
                </SelectValue>
              </SelectTrigger>
              <SelectContent align="start">
                {events.map((event) => (
                  <SelectItem key={event} value={event} className="font-mono">
                    {event}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="min-w-[12rem] flex-[2] space-y-1.5">
            <Label
              htmlFor="hook-answer-value"
              className="text-caption text-muted-foreground"
            >
              {role.label}
            </Label>
            <Input
              id="hook-answer-value"
              value={draftValue}
              onChange={(event) => onDraftValueChange(event.target.value)}
              placeholder={role.placeholder}
              className="h-9 font-mono"
            />
          </div>
          <Button
            type="submit"
            size="sm"
            className="h-9"
            disabled={draftValue.trim().length === 0 || draftEvent.length === 0}
          >
            <Search aria-hidden="true" className="h-3.5 w-3.5" />
            {t(($) => $.hooks.answer.ask)}
          </Button>
        </div>
        {/* Said out loud rather than left to be inferred from an answer that
            never changes: on these events the provider discards the matcher. */}
        {role.inert ? (
          <p className="flex items-start gap-2 rounded-md border border-warning/40 bg-warning/5 p-2.5 text-caption text-muted-foreground">
            <TriangleAlert aria-hidden="true" className="mt-0.5 h-3 w-3 shrink-0 text-foreground" />
            {t(($) => $.hooks.answer.value_inert)}
          </p>
        ) : null}
      </form>

      <div className="px-4 py-3">
        <AnswerBody
          provider={provider}
          view={view}
          totals={totals}
          asked={asked}
          timeAgo={timeAgo}
          tabObservedAt={tabObservedAt}
          notCheckedSources={notCheckedSources}
          onReask={onReask}
          onSelectEntry={onSelectEntry}
        />
      </div>
    </section>
  );
}

function AnswerBody({
  provider,
  view,
  totals,
  asked,
  timeAgo,
  tabObservedAt,
  notCheckedSources,
  onReask,
  onSelectEntry,
}: {
  provider: string;
  view: HookAnswerView;
  totals: HookAnswerTotals;
  asked: HookAnswerRequest | null;
  timeAgo: (value: string) => string;
  tabObservedAt: string | null;
  notCheckedSources: number;
  onReask: () => void;
  onSelectEntry: (entry: RuntimeHookEntry) => void;
}) {
  const { t } = useT("runtimes");

  if (view.kind === "idle") {
    return (
      <p className="flex items-start gap-2 text-caption text-muted-foreground">
        <CircleHelp aria-hidden="true" className="mt-0.5 h-3.5 w-3.5 shrink-0" />
        {t(($) => $.hooks.answer.idle)}
      </p>
    );
  }

  if (view.kind === "asking") {
    return (
      <div className="space-y-2">
        <p className="flex items-center gap-2 text-caption text-muted-foreground">
          <RefreshCw
            aria-hidden="true"
            className="h-3.5 w-3.5 shrink-0 animate-spin motion-reduce:animate-none"
          />
          {t(($) => $.hooks.answer.asking)}
        </p>
        <Skeleton className="h-8 w-full rounded-md" />
        <Skeleton className="h-8 w-3/4 rounded-md" />
      </div>
    );
  }

  if (view.kind === "unobserved" || view.kind === "failed") {
    const unobserved = view.kind === "unobserved";
    // The same shape the badge tones took: destructive text on a destructive
    // tint is 4.36:1 in the light theme, under the 4.5:1 floor for this size.
    // The tint and the border stay as the recognition cue, the icon keeps the
    // state colour at the 3:1 non-text floor, and the title moves to
    // foreground — 18.21:1 light, 16.01:1 dark.
    return (
      <div className="rounded-md border border-destructive/40 bg-destructive/5 p-3">
        <p className="flex items-start gap-2 text-label font-medium text-foreground">
          <CircleAlert
            aria-hidden="true"
            className="mt-0.5 h-3.5 w-3.5 shrink-0 text-destructive"
          />
          {unobserved
            ? t(($) => $.hooks.answer.unobserved_title)
            : t(($) => $.hooks.answer.failed_title)}
        </p>
        <p className="mt-1 text-caption text-muted-foreground">
          {unobserved
            ? t(($) => $.hooks.answer.unobserved_body)
            : t(($) => $.hooks.answer.failed_body)}
        </p>
        {view.error ? (
          <p className="mt-1 font-mono text-caption break-words text-muted-foreground">
            {view.error}
          </p>
        ) : null}
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="mt-2"
          onClick={onReask}
        >
          <RefreshCw aria-hidden="true" className="h-3.5 w-3.5" />
          {t(($) => $.hooks.answer.reask)}
        </Button>
      </div>
    );
  }

  if (view.kind === "unanswerable") {
    // No counts, no sets, nothing partitioned. "Nothing runs" is a statement
    // about the host; this state is a statement about Multica's engine, and
    // the two are not interchangeable.
    return (
      <div className="space-y-3">
        <div className="rounded-md border border-warning/40 bg-warning/5 p-3">
          <p className="flex items-start gap-2 text-label font-medium">
            <TriangleAlert
              aria-hidden="true"
              className="mt-0.5 h-3.5 w-3.5 shrink-0 text-foreground"
            />
            {/* The answer's own event, not the picker's: the reader can
                change the selection after asking, and this heading is about
                what was answered. */}
            {t(($) => $.hooks.answer.unanswerable_title, {
              event: view.answer?.event ?? asked?.event ?? "",
            })}
          </p>
          <p className="mt-1 text-caption text-muted-foreground">
            {t(($) => $.hooks.answer.unanswerable_body)}
          </p>
          <p className="mt-1 text-caption text-muted-foreground">
            {t(($) => $.hooks.answer.unanswerable_not_empty)}
          </p>
        </div>
        <ul className="space-y-2">
          {(view.answer?.unevaluable ?? []).map((item) => (
            <li
              key={item.hook_id}
              className="rounded-md border bg-muted/30 p-3"
            >
              <p className="text-caption text-muted-foreground">
                {t(($) => $.hooks.answer.unevaluable_matcher)}
              </p>
              <p className="mt-0.5 font-mono text-caption break-all">
                {item.matcher}
              </p>
              <p className="mt-1.5 text-caption text-muted-foreground">
                {t(($) => $.hooks.matcher.compiler_message, {
                  message: item.error,
                })}
              </p>
            </li>
          ))}
        </ul>
        {view.error ? (
          <div className="space-y-1">
            <p className="text-caption text-muted-foreground">
              {t(($) => $.hooks.answer.unanswerable_evaluator)}
            </p>
            <p className="font-mono text-caption break-words text-muted-foreground">
              {view.error}
            </p>
          </div>
        ) : null}
        <ObservationLine
          view={view}
          timeAgo={timeAgo}
          tabObservedAt={tabObservedAt}
          onReask={onReask}
        />
      </div>
    );
  }

  const answer = view.answer;
  if (!answer) return null;

  return (
    <div className="space-y-4">
      <div className="space-y-2">
        <p className="text-label">
          <span className="font-medium">
            {t(($) => $.hooks.answer.headline, {
              runs: totals.runs,
              configured: totals.configured,
            })}
          </span>{" "}
          {t(($) => $.hooks.answer.headline_context, {
            event: answer.event,
            value: answer.value,
          })}
        </p>
        <div className="flex flex-wrap gap-1.5">
          <Badge variant="ghost" className={hookToneClass("ok")}>
            {t(($) => $.hooks.answer.count_runs, { count: totals.runs })}
          </Badge>
          {totals.trustUnconfirmed > 0 ? (
            <Badge variant="ghost" className={hookToneClass("warn")}>
              {t(($) => $.hooks.answer.count_trust_unconfirmed, {
                count: totals.trustUnconfirmed,
              })}
            </Badge>
          ) : null}
          {totals.providerSkips > 0 ? (
            <Badge variant="ghost" className={hookToneClass("bad")}>
              {t(($) => $.hooks.answer.count_provider_skips, {
                count: totals.providerSkips,
              })}
            </Badge>
          ) : null}
          {totals.collapsed > 0 ? (
            <Badge variant="ghost" className="bg-muted text-muted-foreground">
              {t(($) => $.hooks.answer.count_collapsed, {
                count: totals.collapsed,
              })}
            </Badge>
          ) : null}
        </div>
        {/* Criterion: no invented ordering. Stated, not implied by layout. */}
        <p className="flex items-start gap-2 text-caption text-muted-foreground">
          <Shuffle aria-hidden="true" className="mt-0.5 h-3 w-3 shrink-0" />
          {t(($) => $.hooks.answer.unordered)}
        </p>
        {/* The provider's own gate on a matched hook, per provider: Claude has
            no per-hook off switch at all, Codex has two. Stated as an
            extension of BehaviourStrip's rule, never as a precedence. */}
        <p className="flex items-start gap-2 text-caption text-muted-foreground">
          <Eye aria-hidden="true" className="mt-0.5 h-3 w-3 shrink-0" />
          {provider === "claude"
            ? t(($) => $.hooks.answer.provider_note_claude)
            : t(($) => $.hooks.answer.provider_note_codex)}
        </p>
        {totals.collapsed > 0 ? (
          <p className="flex items-start gap-2 text-caption text-muted-foreground">
            <Copy aria-hidden="true" className="mt-0.5 h-3 w-3 shrink-0" />
            {t(($) => $.hooks.answer.collapsed_note)}
          </p>
        ) : null}
        {/* Criterion: not-checked is never "nothing here". The answer covers
            only the sources somebody actually read. */}
        {notCheckedSources > 0 ? (
          <p className="flex items-start gap-2 rounded-md border border-warning/40 bg-warning/5 p-2.5 text-caption text-muted-foreground">
            <TriangleAlert
              aria-hidden="true"
              className="mt-0.5 h-3 w-3 shrink-0 text-foreground"
            />
            {t(($) => $.hooks.answer.partial_sources, {
              count: notCheckedSources,
            })}
          </p>
        ) : null}
      </div>

      {answer.matched.length === 0 ? (
        <p className="rounded-md border border-dashed p-3 text-caption text-muted-foreground">
          {t(($) => $.hooks.answer.none_matched)}
        </p>
      ) : (
        <AnswerSet
          title={t(($) => $.hooks.answer.set_matched)}
          hint={t(($) => $.hooks.answer.set_matched_hint)}
          entries={answer.matched}
          tone="ok"
          defaultOpen
          onSelectEntry={onSelectEntry}
        />
      )}

      {answer.never_runs.length > 0 ? (
        <AnswerSet
          title={t(($) => $.hooks.answer.set_never_runs)}
          hint={t(($) => $.hooks.answer.set_never_runs_hint)}
          entries={answer.never_runs}
          tone="bad"
          onSelectEntry={onSelectEntry}
        />
      ) : null}
      {answer.not_matched.length > 0 ? (
        <AnswerSet
          title={t(($) => $.hooks.answer.set_not_matched)}
          hint={t(($) => $.hooks.answer.set_not_matched_hint, {
            value: answer.value,
          })}
          entries={answer.not_matched}
          tone="muted"
          onSelectEntry={onSelectEntry}
        />
      ) : null}
      {answer.configuration_excluded.length > 0 ? (
        <AnswerSet
          title={t(($) => $.hooks.answer.set_excluded)}
          hint={t(($) => $.hooks.answer.set_excluded_hint)}
          entries={answer.configuration_excluded}
          tone="muted"
          onSelectEntry={onSelectEntry}
        />
      ) : null}

      <ObservationLine
        view={view}
        timeAgo={timeAgo}
        tabObservedAt={tabObservedAt}
        onReask={onReask}
      />
    </div>
  );
}

/**
 * One set of the answer. A `<ul>` with no markers and no numbers: the element
 * itself says unordered, and nothing in the layout implies a sequence.
 */
function AnswerSet({
  title,
  hint,
  entries,
  tone,
  defaultOpen = false,
  onSelectEntry,
}: {
  title: string;
  hint: string;
  entries: RuntimeHookEntry[];
  tone: "ok" | "bad" | "muted";
  defaultOpen?: boolean;
  onSelectEntry: (entry: RuntimeHookEntry) => void;
}) {
  const { t } = useT("runtimes");
  const [open, setOpen] = useState(defaultOpen);
  const icon =
    tone === "ok" ? Play : tone === "bad" ? CircleSlash : Archive;
  const Icon = icon;

  return (
    <div className="overflow-hidden rounded-md border">
      <button
        type="button"
        onClick={() => setOpen((previous) => !previous)}
        aria-expanded={open}
        className="flex w-full flex-wrap items-center gap-2 px-3 py-2 text-left hover:bg-muted/40"
      >
        <ChevronDown
          aria-hidden="true"
          className={cn(
            "h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform",
            open ? "" : "-rotate-90",
          )}
        />
        {/* The tone's icon colour comes from hook-state-badges, not from a
            pair written out here: one measured source per tone is what keeps
            the badges and this heading saying the same thing. */}
        <Icon
          aria-hidden="true"
          className={cn("h-3.5 w-3.5 shrink-0", hookToneIconClass(tone))}
        />
        <span className="text-label font-medium">{title}</span>
        <Badge variant="ghost" className="bg-muted text-muted-foreground">
          {t(($) => $.hooks.answer.set_count, { count: entries.length })}
        </Badge>
      </button>
      {open ? (
        <div className="space-y-2 border-t px-3 py-2.5">
          <p className="text-caption text-muted-foreground">{hint}</p>
          <ul className="space-y-2">
            {entries.map((entry) => (
              <AnswerEntry
                key={entry.hook_id}
                entry={entry}
                onSelectEntry={onSelectEntry}
              />
            ))}
          </ul>
        </div>
      ) : null}
    </div>
  );
}

function AnswerEntry({
  entry,
  onSelectEntry,
}: {
  entry: RuntimeHookEntry;
  onSelectEntry: (entry: RuntimeHookEntry) => void;
}) {
  const { t } = useT("runtimes");
  const neverRunsReason = useNeverRunsReason(entry);
  const target = handlerTarget(entry);

  return (
    <li className="rounded-md border bg-background p-2.5">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0 flex-1 space-y-1">
          <p className="font-mono text-caption break-all">
            {target ||
              entry.handler_type ||
              t(($) => $.hooks.table.no_target)}
          </p>
          <p className="text-caption text-muted-foreground">
            {t(($) => $.hooks.answer.entry_matcher, {
              matcher: entry.matcher || t(($) => $.hooks.table.matcher_all),
              type: entry.handler_type || t(($) => $.hooks.detail.handler_type_undeclared),
            })}
          </p>
          <MatcherKindNote entry={entry} />
          {neverRunsReason ? (
            <p className="flex items-start gap-1 text-caption text-destructive">
              <CircleSlash aria-hidden="true" className="mt-0.5 h-3 w-3 shrink-0" />
              <span className="min-w-0 break-words">{neverRunsReason}</span>
            </p>
          ) : null}
          {/* Every source, none of them marked as the winner. More than one is
              a handler seen twice and run once, not a layer taking priority. */}
          <div className="flex flex-wrap gap-1.5">
            {entry.sources.map((source) => (
              <ScopeBadge
                key={`${source.scope}:${source.format}:${source.kind}:${source.name ?? ""}`}
                scope={source.scope}
                format={source.format}
                writable={source.scope === "user"}
              />
            ))}
          </div>
        </div>
        {/* Both axes, side by side, on the answer too. A matched entry the
            provider skips shows a matched membership and a never-runs verdict
            at once, and neither reading answers the other. */}
        <div className="flex max-w-[16rem] flex-wrap items-start gap-1.5">
          <ConfigurationBadge entry={entry} />
          <EffectivenessBadge entry={entry} />
          <TrustBadge entry={entry} />
        </div>
      </div>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="mt-1 h-7"
        onClick={() => onSelectEntry(entry)}
        title={t(($) => $.hooks.table.open_details)}
      >
        {t(($) => $.hooks.table.actions)}
      </Button>
    </li>
  );
}

/**
 * The observation the answer was computed from. Never omitted: an undated
 * answer would be a claim about a host state nobody dated (invariant 3). When
 * it is not the observation the rows above came from, that is said rather than
 * silently blended into one.
 */
function ObservationLine({
  view,
  timeAgo,
  tabObservedAt,
  onReask,
}: {
  view: HookAnswerView;
  timeAgo: (value: string) => string;
  tabObservedAt: string | null;
  onReask: () => void;
}) {
  const { t } = useT("runtimes");
  if (!view.observedAt) return null;
  const when = timeAgo(view.observedAt);
  return (
    <div className="space-y-1.5 border-t pt-2.5">
      <p className="text-caption text-muted-foreground">
        {view.cached
          ? t(($) => $.hooks.answer.observed_last_known, { when })
          : t(($) => $.hooks.answer.observed_live, { when })}
      </p>
      <p className="font-mono text-caption text-muted-foreground">
        {t(($) => $.hooks.observation.observed_at, { stamp: view.observedAt })}
      </p>
      {view.observationMismatch ? (
        <div className="rounded-md border border-warning/40 bg-warning/5 p-2.5">
          <p className="flex items-start gap-2 text-caption">
            <TriangleAlert
              aria-hidden="true"
              className="mt-0.5 h-3 w-3 shrink-0 text-foreground"
            />
            <span className="min-w-0">
              {t(($) => $.hooks.answer.observation_mismatch, {
                answer: view.observedAt ?? "",
                rows: tabObservedAt ?? "",
              })}
            </span>
          </p>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="mt-2"
            onClick={onReask}
          >
            <RefreshCw aria-hidden="true" className="h-3.5 w-3.5" />
            {t(($) => $.hooks.answer.reask)}
          </Button>
        </div>
      ) : null}
    </div>
  );
}
