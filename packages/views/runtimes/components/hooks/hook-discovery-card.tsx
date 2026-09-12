"use client";

import { useEffect, useState } from "react";
import { Check, Clock, RefreshCw } from "lucide-react";
import {
  HOOK_READ_POLL_INTERVAL_MS,
  HOOK_READ_POLL_TIMEOUT_MS,
  type RuntimeHookDiscoveryPhase,
} from "@multica/core/runtimes";
import { Badge } from "@multica/ui/components/ui/badge";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../../i18n";

/**
 * Discovery in progress. This is a different answer from an empty runtime and
 * has to look like one: a named wait with a spinner and skeleton rows, never
 * the empty card's settled "nothing here".
 *
 * Every phase shown is one the read actually reported. There is no progress
 * bar and no percentage, because the pipeline produces neither — initiate,
 * wait for a heartbeat, read — and a fraction would be invented. The two
 * numbers on screen are the real poll interval and the real timeout.
 */
const PHASE_ORDER: RuntimeHookDiscoveryPhase[] = [
  "initiating",
  "queued",
  "reading",
];

export function HookDiscoveryCard({
  runtimeName,
  phase,
}: {
  runtimeName: string;
  phase: RuntimeHookDiscoveryPhase;
}) {
  const { t } = useT("runtimes");
  const elapsed = useElapsedSeconds();
  const current = PHASE_ORDER.indexOf(phase);

  const label: Record<RuntimeHookDiscoveryPhase, string> = {
    initiating: t(($) => $.hooks.discovery.phase_initiating),
    queued: t(($) => $.hooks.discovery.phase_queued),
    reading: t(($) => $.hooks.discovery.phase_reading),
  };
  const detail: Record<RuntimeHookDiscoveryPhase, string> = {
    initiating: t(($) => $.hooks.discovery.phase_initiating_detail),
    queued: t(($) => $.hooks.discovery.phase_queued_detail),
    reading: t(($) => $.hooks.discovery.phase_reading_detail),
  };

  return (
    <div className="space-y-4">
      <div className="rounded-lg border bg-card">
        <div className="flex flex-wrap items-center gap-2 border-b px-4 py-2.5">
          <RefreshCw
            aria-hidden="true"
            className="h-3.5 w-3.5 shrink-0 animate-spin text-muted-foreground motion-reduce:animate-none"
          />
          <h3 className="text-label font-medium">
            {t(($) => $.hooks.discovery.title, { name: runtimeName })}
          </h3>
          <Badge variant="ghost" className="bg-brand/10 text-brand">
            {t(($) => $.hooks.discovery.polling)}
          </Badge>
          <span className="flex-1" />
          {/* The seconds tick, so this stays out of the live region: a screen
              reader wants the phase change, not a stopwatch. */}
          <span
            aria-hidden="true"
            className="font-mono text-caption text-muted-foreground"
          >
            {t(($) => $.hooks.discovery.timing, {
              interval: HOOK_READ_POLL_INTERVAL_MS,
              elapsed,
              timeout: Math.round(HOOK_READ_POLL_TIMEOUT_MS / 1000),
            })}
          </span>
        </div>
        <ol className="space-y-2.5 px-4 py-3" role="status" aria-live="polite">
          {PHASE_ORDER.map((step, index) => {
            const done = index < current;
            const active = index === current;
            return (
              <li key={step} className="flex items-start gap-2.5">
                <span
                  className={cn(
                    "mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full",
                    done
                      ? "bg-success/15 text-success"
                      : active
                        ? "bg-brand/15 text-brand"
                        : "bg-muted text-muted-foreground",
                  )}
                >
                  {done ? (
                    <Check aria-hidden="true" className="h-2.5 w-2.5" />
                  ) : active ? (
                    <RefreshCw
                      aria-hidden="true"
                      className="h-2.5 w-2.5 animate-spin motion-reduce:animate-none"
                    />
                  ) : (
                    <Clock aria-hidden="true" className="h-2.5 w-2.5" />
                  )}
                </span>
                <div className="min-w-0">
                  <p
                    className={cn(
                      "text-label",
                      active ? "font-medium" : "",
                      index > current ? "text-muted-foreground" : "",
                    )}
                  >
                    {label[step]}
                  </p>
                  <p className="text-caption text-muted-foreground">
                    {detail[step]}
                  </p>
                </div>
              </li>
            );
          })}
        </ol>
        <p className="border-t px-4 py-3 text-caption text-muted-foreground">
          {t(($) => $.hooks.discovery.body)}
        </p>
      </div>

      <div aria-hidden="true" className="rounded-lg border bg-card">
        <div className="flex items-center gap-2 border-b px-4 py-2.5">
          <Skeleton className="h-3.5 w-24 rounded-md" />
          <span className="flex-1" />
          <Skeleton className="h-3.5 w-16 rounded-md" />
        </div>
        <ul className="divide-y">
          {[0, 1, 2].map((row) => (
            <li key={row} className="space-y-2 px-4 py-3">
              <div className="flex flex-wrap items-center gap-2">
                <Skeleton className="h-4 w-28 rounded-md" />
                <Skeleton className="h-4 w-16 rounded-md" />
                <Skeleton className="h-4 w-20 rounded-md" />
              </div>
              <Skeleton className="h-3 w-3/5 rounded-md" />
              <Skeleton className="h-3 w-2/5 rounded-md" />
            </li>
          ))}
        </ul>
        <div className="border-t px-4 py-3">
          <Skeleton className="h-3 w-1/3 rounded-md" />
        </div>
      </div>
    </div>
  );
}

/**
 * Seconds since this card mounted, which is when the wait began — the card is
 * rendered only while a read is in flight. Measured rather than counted, so a
 * throttled background tab reports the real elapsed time on return.
 */
function useElapsedSeconds(): number {
  const [seconds, setSeconds] = useState(0);
  useEffect(() => {
    const startedAt = Date.now();
    const timer = setInterval(() => {
      setSeconds(Math.floor((Date.now() - startedAt) / 1000));
    }, 1000);
    return () => clearInterval(timer);
  }, []);
  return seconds;
}
