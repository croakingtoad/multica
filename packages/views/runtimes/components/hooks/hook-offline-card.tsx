"use client";

import { Eye, RefreshCw, WifiOff } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@multica/ui/components/ui/empty";
import { useT } from "../../../i18n";
import { hookToneClass } from "./hook-state-badges";

/**
 * The runtime is offline. Two cases share this card and differ in one
 * sentence and one button: there is a stored observation to fall back to, or
 * there is not. Neither is a failed read — nothing went wrong, the host is
 * simply not reachable — so this does not borrow the failure card's framing.
 *
 * Revealing the snapshot is deliberately a second step. The stored state is
 * offered as *last known*, and an explicit "view last known hooks" is what
 * keeps it from arriving as if it were current (snapshot invariant 3).
 */
export function HookOfflineCard({
  runtimeName,
  lastSeenAt,
  observedAt,
  datedAt,
  timeAgo,
  onReread,
  onReveal,
}: {
  runtimeName: string;
  lastSeenAt: string | null;
  /** The stored observation's stamp as sent, or null when nothing was stored. */
  observedAt: string | null;
  /**
   * The same stamp when it places as a date, null when it does not. Only this
   * one may be aged; `observedAt` is what says a snapshot exists at all, and
   * the two differ when the host dates its observation unreadably.
   */
  datedAt: string | null;
  timeAgo: (value: string) => string;
  onReread: () => void;
  /** Omitted when there is no snapshot, which removes the reveal button. */
  onReveal?: () => void;
}) {
  const { t } = useT("runtimes");
  const canReveal = Boolean(observedAt && onReveal);
  return (
    <Empty className="border border-dashed border-destructive/40">
      <EmptyHeader>
        <EmptyMedia
          variant="icon"
          className={hookToneClass("bad")}
        >
          <WifiOff aria-hidden="true" />
        </EmptyMedia>
        <EmptyTitle>
          {t(($) => $.hooks.offline.title, { name: runtimeName })}
        </EmptyTitle>
        <EmptyDescription>
          {lastSeenAt
            ? t(($) => $.hooks.offline.last_seen, { when: timeAgo(lastSeenAt) })
            : t(($) => $.hooks.offline.last_seen_never)}{" "}
          {/* A snapshot exists or it does not — that is `observedAt`. Whether
              its age can be stated is `datedAt`, a separate question, and the
              reveal button below turns on the first rather than the second. */}
          {observedAt
            ? datedAt === null
              ? t(($) => $.hooks.offline.with_snapshot_undated)
              : t(($) => $.hooks.offline.with_snapshot, {
                  when: timeAgo(datedAt),
                })
            : t(($) => $.hooks.offline.without_snapshot)}
        </EmptyDescription>
      </EmptyHeader>
      {observedAt ? (
        <p className="font-mono text-caption text-muted-foreground">
          {t(($) => $.hooks.observation.observed_at, { stamp: observedAt })}
        </p>
      ) : null}
      <div className="flex flex-wrap items-center justify-center gap-2">
        <Button type="button" variant="outline" size="sm" onClick={onReread}>
          <RefreshCw aria-hidden="true" className="h-3.5 w-3.5" />
          {t(($) => $.hooks.observation.retry)}
        </Button>
        {canReveal ? (
          <Button type="button" size="sm" onClick={onReveal}>
            <Eye aria-hidden="true" className="h-3.5 w-3.5" />
            {t(($) => $.hooks.offline.reveal)}
          </Button>
        ) : null}
      </div>
      <p className="max-w-prose text-caption text-muted-foreground">
        {t(($) => $.hooks.offline.read_needs_host)}
      </p>
    </Empty>
  );
}

/**
 * The rail counterpart shown while a revealed snapshot is on screen, so the
 * "last known" reading survives a scroll away from the banner at the top.
 */
export function HookLastKnownRail({
  observedAt,
  datedAt,
  timeAgo,
}: {
  observedAt: string;
  /** Null when the host's stamp does not place; the rail then states no age. */
  datedAt: string | null;
  timeAgo: (value: string) => string;
}) {
  const { t } = useT("runtimes");
  return (
    <div className="rounded-lg border border-warning/40 bg-warning/5">
      <div className="flex items-center gap-2 border-b border-warning/40 px-4 py-2.5">
        <WifiOff aria-hidden="true" className="h-3.5 w-3.5 text-foreground" />
        <h3 className="text-label font-medium">
          {t(($) => $.hooks.offline.rail_title)}
        </h3>
      </div>
      <div className="space-y-1 px-4 py-3">
        <p className="text-caption text-muted-foreground">
          {datedAt === null
            ? t(($) => $.hooks.offline.rail_body_undated)
            : t(($) => $.hooks.offline.rail_body, { when: timeAgo(datedAt) })}
        </p>
        <p className="font-mono text-caption text-muted-foreground">
          {t(($) => $.hooks.observation.observed_at, { stamp: observedAt })}
        </p>
      </div>
    </div>
  );
}
