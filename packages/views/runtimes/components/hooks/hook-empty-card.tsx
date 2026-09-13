"use client";

import { Webhook } from "lucide-react";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@multica/ui/components/ui/empty";
import { useT } from "../../../i18n";
import { ScopeBadge } from "./hook-state-badges";
import type { HookEmptySummary } from "./hooks-model";

/**
 * A runtime whose sources hold no hook entries. It says which files that
 * statement is about — and, separately, which scopes nobody looked at, since
 * an unchecked scope supports no claim either way.
 *
 * It carries the observation date itself rather than relying on the banner
 * above it: "no hooks" is a claim about a moment, and an undated one would
 * read as timeless (snapshot invariant 3).
 *
 * The headline is scope-limited, not just the body. A reader takes the
 * headline away, and a runtime whose local scope was never checked has not
 * earned "no lifecycle hooks on this runtime" — so the title names what was
 * read, and the two reasons for zero entries get their own title as well as
 * their own body.
 */
export function HookEmptyCard({
  runtimeName,
  summary,
  observedAt,
  datedAt,
  timeAgo,
}: {
  runtimeName: string;
  summary: HookEmptySummary;
  /** The observation's stamp as the host sent it, or null when it sent none. */
  observedAt: string | null;
  /** The same stamp when it places as a date; null leaves the line age-less. */
  datedAt: string | null;
  timeAgo: (value: string) => string;
}) {
  const { t } = useT("runtimes");
  return (
    <Empty className="border border-dashed">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <Webhook aria-hidden="true" />
        </EmptyMedia>
        <EmptyTitle>
          {summary.nothingRead
            ? t(($) => $.hooks.events.empty_nothing_read_title, {
                name: runtimeName,
              })
            : t(($) => $.hooks.events.empty_title, { name: runtimeName })}
        </EmptyTitle>
        <EmptyDescription>
          {summary.nothingRead
            ? t(($) => $.hooks.events.empty_nothing_read_body)
            : t(($) => $.hooks.events.empty_read_body)}
        </EmptyDescription>
      </EmptyHeader>

      {summary.readPaths.length > 0 ? (
        <div className="space-y-1">
          <p className="text-caption text-muted-foreground">
            {t(($) => $.hooks.events.empty_read_paths)}
          </p>
          <ul>
            {summary.readPaths.map((path) => (
              <li
                key={path}
                className="font-mono text-caption break-all text-muted-foreground"
              >
                {path}
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      {summary.absent.length > 0 ? (
        <ScopeList
          label={t(($) => $.hooks.events.empty_absent)}
          scopes={summary.absent}
        />
      ) : null}

      {summary.notChecked.length > 0 ? (
        <div className="space-y-1.5">
          <ScopeList
            label={t(($) => $.hooks.events.empty_not_checked)}
            scopes={summary.notChecked}
          />
          <p className="max-w-prose text-caption text-muted-foreground">
            {t(($) => $.hooks.events.empty_not_checked_note)}
          </p>
        </div>
      ) : null}

      {observedAt ? (
        <p className="text-caption text-muted-foreground">
          {datedAt === null
            ? t(($) => $.hooks.events.empty_last_read_undated)
            : t(($) => $.hooks.events.empty_last_read, {
                when: timeAgo(datedAt),
              })}
        </p>
      ) : null}
    </Empty>
  );
}

function ScopeList({
  label,
  scopes,
}: {
  label: string;
  scopes: HookEmptySummary["absent"];
}) {
  return (
    <div className="space-y-1">
      <p className="text-caption text-muted-foreground">{label}</p>
      <div className="flex flex-wrap items-center justify-center gap-1.5">
        {scopes.map((scope) => (
          <ScopeBadge
            key={scope.key}
            scope={scope.scope}
            format={scope.format}
            writable={scope.writable}
          />
        ))}
      </div>
    </div>
  );
}
