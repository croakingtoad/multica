"use client";

import { CircleHelp, FileQuestion, FileX2, Layers, Lock } from "lucide-react";
import { Badge } from "@multica/ui/components/ui/badge";
import { useT } from "../../../i18n";
import { ScopeBadge } from "./hook-state-badges";
import type { HookScopeRow } from "./hooks-model";

/**
 * One row per expected source, in three visually distinct states. A scope with
 * no observation reads as "not checked" with an explanation of why nobody
 * looked — never as a scope that was found to hold nothing.
 */
export function ScopeSourcesCard({ scopes }: { scopes: HookScopeRow[] }) {
  const { t } = useT("runtimes");
  return (
    <div className="rounded-lg border bg-card">
      <div className="flex items-center gap-2 border-b px-4 py-2.5">
        <Layers aria-hidden="true" className="h-3.5 w-3.5 text-muted-foreground" />
        <h3 className="text-label font-medium">{t(($) => $.hooks.scopes.title)}</h3>
      </div>
      <ul className="divide-y">
        {scopes.map((scope) => (
          <li key={scope.key} className="space-y-1.5 px-4 py-3">
            <div className="flex flex-wrap items-center gap-2">
              <ScopeBadge
                scope={scope.scope}
                format={scope.format}
                writable={scope.writable}
              />
              <SourceStateBadge state={scope.state} />
              {scope.state === "found" ? (
                <Badge variant="ghost" className="bg-muted text-muted-foreground">
                  {t(($) => $.hooks.scopes.entry_count, { count: scope.entryCount })}
                </Badge>
              ) : null}
              {!scope.writable ? (
                <span className="inline-flex items-center gap-1 text-caption text-muted-foreground">
                  <Lock aria-hidden="true" className="h-3 w-3" />
                  {t(($) => $.hooks.scopes.read_only)}
                </span>
              ) : null}
            </div>
            {scope.state === "found" ? (
              <>
                <p className="font-mono text-caption break-all text-muted-foreground">
                  {scope.sourcePath ?? scope.expectedPath ?? ""}
                </p>
                {scope.contentHash ? (
                  <p className="font-mono text-caption text-muted-foreground">
                    {t(($) => $.hooks.scopes.content_hash, {
                      hash: scope.contentHash.slice(0, 12),
                    })}
                  </p>
                ) : null}
              </>
            ) : (
              <>
                {scope.expectedPath ? (
                  <p className="font-mono text-caption break-all text-muted-foreground">
                    {t(($) => $.hooks.scopes.expected_path, {
                      path: scope.expectedPath,
                    })}
                  </p>
                ) : null}
                <p className="text-caption text-muted-foreground">
                  {scope.state === "absent"
                    ? t(($) => $.hooks.scopes.absent_hint)
                    : scope.state === "not_checked"
                      ? t(($) => $.hooks.scopes.not_checked_hint)
                      : t(($) => $.hooks.scopes.state_unknown, { value: scope.state })}
                </p>
              </>
            )}
          </li>
        ))}
      </ul>
      <div className="flex items-start gap-2 border-t bg-muted/30 px-4 py-3">
        <Lock
          aria-hidden="true"
          className="mt-0.5 h-3 w-3 shrink-0 text-muted-foreground"
        />
        <p className="text-caption text-muted-foreground">
          {t(($) => $.hooks.scopes.write_note)}
        </p>
      </div>
    </div>
  );
}

function SourceStateBadge({ state }: { state: string }) {
  const { t } = useT("runtimes");
  if (state === "found") {
    return (
      <Badge variant="ghost" className="gap-1 bg-success/10 text-success">
        {t(($) => $.hooks.scopes.state_found)}
      </Badge>
    );
  }
  if (state === "absent") {
    return (
      <Badge variant="ghost" className="gap-1 bg-muted text-muted-foreground">
        <FileX2 aria-hidden="true" />
        {t(($) => $.hooks.scopes.state_absent)}
      </Badge>
    );
  }
  if (state === "not_checked") {
    // Visually distinct from "absent" on purpose: the dashed amber outline
    // reads as an open question rather than a settled negative result. The
    // label itself stays on a foreground that clears AA — `--warning` is
    // 2.25:1 as text on the light surface and cannot carry type.
    return (
      <Badge
        variant="ghost"
        className="h-auto gap-1 rounded-md border border-dashed border-warning bg-warning/10 py-0.5 text-foreground whitespace-normal"
      >
        <FileQuestion aria-hidden="true" />
        {t(($) => $.hooks.scopes.state_not_checked)}
      </Badge>
    );
  }
  return (
    <Badge
      variant="ghost"
      className="h-auto gap-1 rounded-md bg-muted py-0.5 text-foreground ring-1 ring-inset ring-warning whitespace-normal"
    >
      <CircleHelp aria-hidden="true" />
      {t(($) => $.hooks.scopes.state_unknown, { value: state })}
    </Badge>
  );
}

