"use client";

import {
  CircleAlert,
  Eye,
  FileQuestion,
  GitBranch,
  Layers,
  RefreshCw,
  Webhook,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@multica/ui/components/ui/empty";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../../i18n";
import type { HookObservationView } from "./hooks-model";

/**
 * Dates the observation on screen, and names the two states that have nothing
 * to date: a read that never produced an observation, and one that failed.
 * The two states that do show rows still render their date, because an undated
 * observation must never appear as current (invariant 3).
 *
 * Two states deliberately do not live here. A read in flight is
 * `HookDiscoveryCard` and an offline runtime is `HookOfflineCard`: both are
 * whole-screen answers rather than a caption over content, and neither may be
 * mistaken for the settled "nothing here" this banner can sit above.
 */
export function ObservationBanner({
  view,
  runtimeName,
  timeAgo,
  onReread,
}: {
  view: HookObservationView;
  runtimeName: string;
  timeAgo: (value: string) => string;
  onReread: () => void;
}) {
  const { t } = useT("runtimes");

  if (view.kind === "no_observation" || view.kind === "failed") {
    const isFailure = view.kind === "failed";
    return (
      <Empty className="border border-dashed border-destructive/40">
        <EmptyHeader>
          {/* Icon only, no text: destructive on destructive/10 measures
              3.97:1, which clears the 3:1 WCAG floor for non-text but not the
              4.5:1 one. The badge tones in hook-state-badges.tsx carry label
              text on the same tint and were moved off it for that reason. */}
          <EmptyMedia
            variant="icon"
            className="bg-destructive/10 text-destructive"
          >
            {isFailure ? (
              <CircleAlert aria-hidden="true" />
            ) : (
              <FileQuestion aria-hidden="true" />
            )}
          </EmptyMedia>
          <EmptyTitle>
            {isFailure
              ? t(($) => $.hooks.observation.failed_title)
              : t(($) => $.hooks.observation.no_observation_title)}
          </EmptyTitle>
          <EmptyDescription>
            {isFailure
              ? t(($) => $.hooks.observation.failed_body)
              : t(($) => $.hooks.observation.no_observation_body)}
          </EmptyDescription>
        </EmptyHeader>
        {view.error ? (
          <p className="max-w-prose font-mono text-caption text-muted-foreground">
            {view.error}
          </p>
        ) : null}
        <Button type="button" variant="outline" size="sm" onClick={onReread}>
          <RefreshCw aria-hidden="true" className="h-3.5 w-3.5" />
          {t(($) => $.hooks.observation.retry)}
        </Button>
      </Empty>
    );
  }

  const when = view.observedAt ? timeAgo(view.observedAt) : "";
  const lastKnown = view.kind === "last_known";
  return (
    <div
      className={cn(
        "rounded-lg border p-4",
        lastKnown ? "border-warning/40 bg-warning/5" : "bg-card",
      )}
    >
      <div className="flex flex-wrap items-start gap-3">
        {lastKnown ? (
          <Eye
            aria-hidden="true"
            className="mt-0.5 h-4 w-4 shrink-0 text-foreground"
          />
        ) : (
          <Webhook
            aria-hidden="true"
            className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground"
          />
        )}
        <div className="min-w-0 flex-1 space-y-1">
          <p className="text-label font-medium">
            {lastKnown
              ? t(($) => $.hooks.observation.last_known_title, { when })
              : view.stale
                ? t(($) => $.hooks.observation.live_stale, { when })
                : t(($) => $.hooks.observation.live, { when })}
          </p>
          {lastKnown ? (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.hooks.observation.last_known_body)}
            </p>
          ) : null}
          {/* The absolute stamp sits beside the relative one so a far-past
              observation cannot hide behind a vague "a while ago". */}
          <p className="font-mono text-caption text-muted-foreground">
            {t(($) => $.hooks.observation.observed_at, {
              stamp: view.observedAt ?? "",
            })}
          </p>
          <p className="text-caption text-muted-foreground">{runtimeName}</p>
        </div>
        <Button type="button" variant="outline" size="sm" onClick={onReread}>
          <RefreshCw aria-hidden="true" className="h-3.5 w-3.5" />
          {t(($) => $.hooks.observation.retry)}
        </Button>
      </div>
      {view.resolutionError ? (
        <div className="mt-3 rounded-md border border-destructive/40 bg-destructive/5 p-3">
          <p className="text-label font-medium text-destructive">
            {t(($) => $.hooks.observation.resolution_error_title)}
          </p>
          <p className="mt-1 text-caption text-muted-foreground">
            {t(($) => $.hooks.observation.resolution_error_body)}
          </p>
          <p className="mt-1 font-mono text-caption text-muted-foreground">
            {view.resolutionError}
          </p>
        </div>
      ) : null}
    </div>
  );
}

/**
 * The provider's merge rule, stated per provider. Persistent, because it is
 * the rule every row below is read under — and because "nothing is displaced"
 * is the claim this screen must not contradict anywhere.
 */
export function BehaviourStrip({ provider }: { provider: string }) {
  const { t } = useT("runtimes");
  const claude = provider === "claude";
  return (
    <div className="flex items-start gap-3 rounded-lg border bg-muted/30 p-3">
      {claude ? (
        <Layers
          aria-hidden="true"
          className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground"
        />
      ) : (
        <GitBranch
          aria-hidden="true"
          className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground"
        />
      )}
      <div className="min-w-0 space-y-1">
        <p className="text-label">
          <span className="font-medium">
            {claude
              ? t(($) => $.hooks.behaviour.claude_title)
              : t(($) => $.hooks.behaviour.codex_title)}
          </span>{" "}
          {claude
            ? t(($) => $.hooks.behaviour.claude_body)
            : t(($) => $.hooks.behaviour.codex_body)}
        </p>
        <p className="text-caption text-muted-foreground">
          {claude
            ? t(($) => $.hooks.behaviour.claude_note)
            : t(($) => $.hooks.behaviour.codex_note)}
        </p>
      </div>
    </div>
  );
}

