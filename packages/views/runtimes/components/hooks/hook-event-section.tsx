"use client";

import { ChevronDown, CircleSlash } from "lucide-react";
import type { RuntimeHookEntry } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../../i18n";
import {
  ConfigurationBadge,
  EffectivenessBadge,
  MatcherKindNote,
  ScopeBadge,
  TrustBadge,
  hookToneClass,
  useNeverRunsReason,
} from "./hook-state-badges";
import { type HookEventGroup, entryWillRun } from "./hooks-model";

/**
 * The handler's most identifying field. Each provider names it differently and
 * a handler may declare none, which is why the fallback is a stated absence
 * rather than an empty cell.
 */
export function handlerTarget(entry: RuntimeHookEntry): string {
  for (const key of ["command", "url", "tool", "prompt", "agent"]) {
    const value = entry.handler[key];
    if (typeof value === "string" && value) return value;
  }
  return "";
}


export function matchesHookQuery(entry: RuntimeHookEntry, needle: string): boolean {
  if (entry.event.toLowerCase().includes(needle)) return true;
  if (entry.matcher.toLowerCase().includes(needle)) return true;
  if (entry.handler_type.toLowerCase().includes(needle)) return true;
  return handlerTarget(entry).toLowerCase().includes(needle);
}


export function HookEventSection({
  group,
  open,
  onToggle,
  onSelect,
}: {
  group: HookEventGroup;
  open: boolean;
  onToggle: () => void;
  onSelect: (entry: RuntimeHookEntry) => void;
}) {
  const { t } = useT("runtimes");
  return (
    <div className="overflow-hidden rounded-lg border bg-card">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        title={t(($) => $.hooks.events.group_toggle, { event: group.event })}
        className="flex w-full flex-wrap items-center gap-2 px-4 py-2.5 text-left hover:bg-muted/40"
      >
        <ChevronDown
          aria-hidden="true"
          className={cn(
            "h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform",
            open ? "" : "-rotate-90",
          )}
        />
        <span className="font-mono text-label font-medium">{group.event}</span>
        <Badge variant="ghost" className="bg-muted text-muted-foreground">
          {t(($) => $.hooks.events.group_configured, { count: group.entries.length })}
        </Badge>
        <Badge
          variant="ghost"
          className={hookToneClass(group.willRun > 0 ? "ok" : "muted")}
        >
          {t(($) => $.hooks.events.group_will_run, { count: group.willRun })}
        </Badge>
        {group.inactive > 0 ? (
          <Badge variant="ghost" className="bg-muted text-muted-foreground">
            {t(($) => $.hooks.events.group_inactive, { count: group.inactive })}
          </Badge>
        ) : null}
        {group.neverRuns > 0 ? (
          <Badge variant="ghost" className={hookToneClass("bad")}>
            {t(($) => $.hooks.events.group_never_runs, { count: group.neverRuns })}
          </Badge>
        ) : null}
        {group.trustUnknown > 0 ? (
          <Badge variant="ghost" className={hookToneClass("warn")}>
            {t(($) => $.hooks.events.group_trust_unknown, {
              count: group.trustUnknown,
            })}
          </Badge>
        ) : null}
        {group.unevaluableMatchers > 0 ? (
          <Badge variant="ghost" className={hookToneClass("unknown")}>
            {t(($) => $.hooks.events.group_unevaluable, {
              count: group.unevaluableMatchers,
            })}
          </Badge>
        ) : null}
        <span className="flex-1" />
        <span className="text-caption text-muted-foreground">
          {t(($) => $.hooks.events.group_sources, { count: group.sourceCount })}
        </span>
      </button>
      {/* table-fixed, because on an auto table a long never-runs reason pushes
          the two state columns off the right edge — which is exactly the
          information the row exists to carry. */}
      {open ? (
        <Table className="table-fixed">
          <TableHeader>
            <TableRow>
              <TableHead className="w-[17%]">
                {t(($) => $.hooks.table.matcher)}
              </TableHead>
              <TableHead className="w-[9%]">
                {t(($) => $.hooks.table.handler)}
              </TableHead>
              <TableHead className="w-[24%]">
                {t(($) => $.hooks.table.target)}
              </TableHead>
              <TableHead className="w-[11%]">
                {t(($) => $.hooks.table.source)}
              </TableHead>
              {/* Two columns for two axes: the layout itself refuses to
                  collapse configuration and the provider's verdict. */}
              <TableHead className="w-[13%]">
                {t(($) => $.hooks.table.configuration)}
              </TableHead>
              <TableHead className="w-[18%]">
                {t(($) => $.hooks.table.effectiveness)}
              </TableHead>
              <TableHead className="w-[8%]">
                <span className="sr-only">{t(($) => $.hooks.table.actions)}</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {group.entries.map((entry) => (
              <HookRow key={entry.hook_id} entry={entry} onSelect={onSelect} />
            ))}
          </TableBody>
        </Table>
      ) : null}
    </div>
  );
}

function HookRow({
  entry,
  onSelect,
}: {
  entry: RuntimeHookEntry;
  onSelect: (entry: RuntimeHookEntry) => void;
}) {
  const { t } = useT("runtimes");
  const neverRunsReason = useNeverRunsReason(entry);
  const target = handlerTarget(entry);
  // Dimmed, never struck through or hidden: the entry is still in the file,
  // and no other layer took its place.
  const inert = !entryWillRun(entry);

  return (
    <TableRow className={cn(inert ? "text-muted-foreground" : "")}>
      <TableCell className="align-top whitespace-normal">
        <div className="space-y-1">
          <span className="block font-mono text-caption break-all">
            {entry.matcher || t(($) => $.hooks.table.matcher_all)}
          </span>
          {/* Plain text, not a pill: these labels are sentences, and a dense
              row of pills reads as status rather than as explanation. */}
          <MatcherKindNote entry={entry} />
        </div>
      </TableCell>
      <TableCell className="align-top whitespace-normal">
        <span className="font-mono text-caption">
          {entry.handler_type || t(($) => $.hooks.detail.handler_type_undeclared)}
        </span>
      </TableCell>
      <TableCell className="align-top whitespace-normal">
        {target ? (
          <span className="block font-mono text-caption break-all">{target}</span>
        ) : (
          <span className="text-caption text-muted-foreground">
            {t(($) => $.hooks.table.no_target)}
          </span>
        )}
        {neverRunsReason ? (
          <p className="mt-1 flex items-start gap-1 text-caption text-destructive">
            <CircleSlash aria-hidden="true" className="mt-0.5 h-3 w-3 shrink-0" />
            <span className="min-w-0 break-words">{neverRunsReason}</span>
          </p>
        ) : null}
      </TableCell>
      <TableCell className="align-top whitespace-normal">
        <div className="space-y-1">
          {entry.sources.map((source) => (
            <div
              key={`${source.scope}:${source.format}:${source.kind}:${source.name ?? ""}`}
            >
              <ScopeBadge
                scope={source.scope}
                format={source.format}
                writable={source.scope === "user"}
              />
            </div>
          ))}
        </div>
      </TableCell>
      <TableCell className="align-top whitespace-normal">
        <ConfigurationBadge entry={entry} />
        {entry.configuration === "parked" && entry.parked_at ? (
          <p className="mt-1 text-caption break-words text-muted-foreground">
            {t(($) => $.hooks.configuration.parked_at, { stamp: entry.parked_at })}
          </p>
        ) : null}
      </TableCell>
      {/* The provider's verdict, with Codex trust beside it when it has one.
          Kept out of the configuration cell so no reading of this row can
          treat one axis as an answer to the other. */}
      <TableCell className="align-top whitespace-normal">
        <div className="flex flex-wrap items-start gap-1.5">
          <EffectivenessBadge entry={entry} />
          <TrustBadge entry={entry} />
        </div>
      </TableCell>
      <TableCell className="align-top whitespace-normal">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => onSelect(entry)}
          title={t(($) => $.hooks.table.open_details)}
        >
          {t(($) => $.hooks.table.actions)}
        </Button>
      </TableCell>
    </TableRow>
  );
}

