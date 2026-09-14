"use client";

import { Lock } from "lucide-react";
import type { RuntimeHookEntry, RuntimeHookMember } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Separator } from "@multica/ui/components/ui/separator";
import { useT } from "../../../i18n";
import {
  ConfigurationBadge,
  EffectivenessBadge,
  MatcherKindNote,
  ScopeBadge,
  TrustBadge,
  useMatcherKindLabel,
  useNeverRunsReason,
  useTrustLabel,
} from "./hook-state-badges";
import { hookScopeIsWritable, hookSourcePath } from "./hooks-model";

// Read-only detail for one entry, on the Dialog surface McpServerDialog and
// runtime-profiles-dialog use. Handler fields render as labelled rows, not as
// editable JSON: the design system has no code or JSON editor, and this pass
// has no write path to put behind one.

/** Handler fields as sorted label/value rows, with `type` hoisted out. */
function handlerFieldRows(handler: Record<string, unknown>): {
  key: string;
  value: string;
}[] {
  return Object.entries(handler)
    .filter(([key]) => key !== "type")
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([key, value]) => ({
      key,
      value:
        typeof value === "string"
          ? value
          : // Objects and arrays are stringified rather than dropped: an `if`
            // pre-filter or an args array is part of what the entry declares,
            // and hiding it would understate the handler.
            JSON.stringify(value),
    }));
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[minmax(0,9rem)_minmax(0,1fr)] items-start gap-3 py-1.5">
      <dt className="text-caption text-muted-foreground">{label}</dt>
      <dd className="min-w-0 text-body break-words">{children}</dd>
    </div>
  );
}

export function HookDetailDialog({
  entry,
  member,
  provider,
  onOpenChange,
}: {
  entry: RuntimeHookEntry | null;
  member?: RuntimeHookMember;
  provider: string;
  onOpenChange: (open: boolean) => void;
}) {
  // Mounted only for a selected entry, so the body below can read `entry`
  // unconditionally and still call its label hooks in a stable order.
  if (!entry) return null;
  return (
    <HookDetailBody
      entry={entry}
      member={member}
      provider={provider}
      onOpenChange={onOpenChange}
    />
  );
}

function HookDetailBody({
  entry,
  member,
  provider,
  onOpenChange,
}: {
  entry: RuntimeHookEntry;
  member?: RuntimeHookMember;
  provider: string;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("runtimes");
  const detailEntry = member
    ? {
        ...entry,
        hook_id: member.hook_id,
        matcher: member.matcher,
        matcher_kind: member.matcher_kind,
        matcher_error: member.matcher_error,
        sources: [member.source],
        members: [member],
      }
    : entry;
  const matcher = useMatcherKindLabel(detailEntry);
  const neverRunsReason = useNeverRunsReason(detailEntry);
  const trust = useTrustLabel(detailEntry);
  const fields = handlerFieldRows(entry.handler);

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[88vh] flex-col gap-0 overscroll-contain p-0 sm:max-w-2xl">
        <DialogHeader className="shrink-0 border-b border-surface-border px-6 py-5 pr-14">
          <DialogTitle className="font-mono">
            {t(($) => $.hooks.detail.title, { event: entry.event })}
          </DialogTitle>
          <DialogDescription>
            {t(($) => $.hooks.detail.description)}
          </DialogDescription>
        </DialogHeader>

        <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-6 py-5">
          {/* Both axes, side by side and never merged. */}
          <div className="space-y-2">
            <div className="flex flex-wrap items-center gap-3">
              <span className="text-caption text-muted-foreground">
                {t(($) => $.hooks.configuration.label)}
              </span>
              <ConfigurationBadge entry={detailEntry} />
              <Separator orientation="vertical" className="h-4" />
              <span className="text-caption text-muted-foreground">
                {t(($) => $.hooks.effectiveness.label)}
              </span>
              <EffectivenessBadge entry={detailEntry} />
              <TrustBadge entry={detailEntry} />
            </div>
            <p className="text-caption text-muted-foreground">
              {t(($) => $.hooks.effectiveness.independent_note)}
            </p>
            {entry.configuration === "parked" ? (
              <p className="text-caption text-muted-foreground">
                {entry.parked_at
                  ? `${t(($) => $.hooks.configuration.parked_at, { stamp: entry.parked_at })} — ${t(($) => $.hooks.configuration.parked_hint)}`
                  : t(($) => $.hooks.configuration.parked_hint)}
              </p>
            ) : null}
            {entry.configuration === "disabled" ? (
              <p className="text-caption text-muted-foreground">
                {t(($) => $.hooks.configuration.disabled_hint)}
              </p>
            ) : null}
            {neverRunsReason ? (
              <p className="text-caption text-destructive">{neverRunsReason}</p>
            ) : null}
            {entry.effectiveness === "trust_unknown" ? (
              <p className="text-caption text-muted-foreground">
                {t(($) => $.hooks.effectiveness.trust_unknown_hint)}
              </p>
            ) : null}
            {trust?.hint ? (
              <p className="text-caption text-muted-foreground">{trust.hint}</p>
            ) : null}
          </div>

          <Separator />

          <dl className="divide-y divide-surface-border">
            <Row label={t(($) => $.hooks.detail.event)}>
              <span className="font-mono">{entry.event}</span>
            </Row>
            <Row label={t(($) => $.hooks.matcher.label)}>
              <div className="space-y-1">
                <span className="font-mono">
                  {detailEntry.matcher || t(($) => $.hooks.table.matcher_all)}
                </span>
                <div>
                  <MatcherKindNote entry={detailEntry} />
                </div>
                {matcher.hint ? (
                  <p className="text-caption text-muted-foreground">{matcher.hint}</p>
                ) : null}
                {detailEntry.matcher_error ? (
                  <p className="font-mono text-caption text-muted-foreground">
                    {t(($) => $.hooks.matcher.compiler_message, {
                      message: detailEntry.matcher_error,
                    })}
                  </p>
                ) : null}
              </div>
            </Row>
            <Row label={t(($) => $.hooks.detail.handler_type)}>
              <span className="font-mono">
                {entry.handler_type ||
                  t(($) => $.hooks.detail.handler_type_undeclared)}
              </span>
            </Row>
            <Row label={t(($) => $.hooks.detail.hook_id)}>
              <span className="font-mono text-caption text-muted-foreground">
                {detailEntry.hook_id}
              </span>
            </Row>
            {member ? (
              <Row label={t(($) => $.hooks.detail.occurrence)}>
                <span className="font-mono">{member.occurrence}</span>
              </Row>
            ) : null}
          </dl>

          <div className="space-y-2">
            <h3 className="text-label font-medium">
              {t(($) => $.hooks.detail.handler_fields)}
            </h3>
            {fields.length === 0 ? (
              <p className="text-caption text-muted-foreground">
                {t(($) => $.hooks.detail.handler_fields_empty)}
              </p>
            ) : (
              <dl className="divide-y divide-surface-border rounded-md border border-surface-border px-3">
                {fields.map((field) => (
                  <Row key={field.key} label={field.key}>
                    <span className="font-mono text-caption">{field.value}</span>
                  </Row>
                ))}
              </dl>
            )}
          </div>

          <div className="space-y-2">
            <h3 className="text-label font-medium">
              {t(($) => $.hooks.detail.sources_title)}
            </h3>
            <p className="text-caption text-muted-foreground">
              {detailEntry.sources.length > 1
                ? t(($) => $.hooks.detail.source_shared_hint, {
                    count: detailEntry.sources.length,
                  })
                : t(($) => $.hooks.detail.source_single_hint)}
            </p>
            <ul className="space-y-2">
              {detailEntry.sources.map((source) => {
                const path = hookSourcePath(provider, source.scope, source.format);
                return (
                  <li
                    key={`${source.scope}:${source.format}:${source.kind}:${source.name ?? ""}`}
                    className="flex flex-wrap items-center gap-2"
                  >
                    <ScopeBadge
                      scope={source.scope}
                      format={source.format}
                      writable={hookScopeIsWritable(source.scope)}
                    />
                    {path ? (
                      <span className="font-mono text-caption text-muted-foreground">
                        {path}
                      </span>
                    ) : null}
                    <span className="text-caption text-muted-foreground">
                      {t(($) => $.hooks.detail.source_kind, {
                        kind: source.name ? `${source.kind} ${source.name}` : source.kind,
                      })}
                    </span>
                  </li>
                );
              })}
            </ul>
          </div>
        </div>

        <div className="flex shrink-0 items-center gap-2 border-t bg-muted/30 px-6 py-3">
          <Lock
            aria-hidden="true"
            className="h-3.5 w-3.5 shrink-0 text-muted-foreground"
          />
          <p className="min-w-0 flex-1 text-caption text-muted-foreground">
            {t(($) => $.hooks.detail.read_only_note)}
          </p>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => onOpenChange(false)}
          >
            {t(($) => $.hooks.detail.close)}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
