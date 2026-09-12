"use client";

import {
  Archive,
  Ban,
  CircleCheck,
  CircleHelp,
  CircleSlash,
  KeyRound,
  Lock,
  Pencil,
  ShieldCheck,
  TriangleAlert,
} from "lucide-react";
import type { RuntimeHookEntry } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../../i18n";

// The two axes get two badges, always, side by side. Collapsing them into one
// would lose the case the resolution layer exists to keep: a hook can be
// parked *and* ineligible, and unparking it would still not make it run.
//
// Nothing in this file has a "shadowed", "overridden" or "displaced" state,
// because no entry on either provider is ever displaced by another layer.
// Every label below describes the entry itself.

type BadgeTone = "ok" | "muted" | "bad" | "warn" | "unknown";

// Contrast, measured against the light theme's white surface: success 4.5:1,
// destructive 4.8:1 and muted-foreground 4.8:1 all clear WCAG AA as text, but
// `--warning` is 2.25:1 and cannot be used for type at all. The warn tone
// therefore takes the solid amber chip (warning-foreground on warning is
// 7.7:1 light / 7.0:1 dark), and the unknown tone keeps an amber ring for
// recognition while its text stays on a readable foreground.
const TONE_CLASS: Record<BadgeTone, string> = {
  ok: "bg-success/10 text-success",
  muted: "bg-muted text-muted-foreground",
  bad: "bg-destructive/10 text-destructive",
  warn: "bg-warning text-warning-foreground",
  unknown: "bg-muted text-foreground ring-1 ring-inset ring-warning",
};

function StateBadge({
  tone,
  icon: Icon,
  children,
  className,
}: {
  tone: BadgeTone;
  icon: typeof CircleCheck;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <Badge
      variant="ghost"
      className={cn(
        // h-auto + whitespace-normal: these labels are sentences, and Badge's
        // fixed height and nowrap would push them out of a fixed-width column.
        // rounded-md, because the pill radius reads as a lozenge once the
        // label wraps onto a second line.
        "h-auto items-start gap-1 rounded-md border-transparent px-1.5 py-0.5 text-left whitespace-normal",
        TONE_CLASS[tone],
        className,
      )}
    >
      <Icon aria-hidden="true" className="mt-0.5 shrink-0" />
      <span className="min-w-0">{children}</span>
    </Badge>
  );
}

export function ConfigurationBadge({ entry }: { entry: RuntimeHookEntry }) {
  const { t } = useT("runtimes");
  switch (entry.configuration) {
    case "live":
      return (
        <StateBadge tone="ok" icon={CircleCheck}>
          {t(($) => $.hooks.configuration.live)}
        </StateBadge>
      );
    case "parked":
      return (
        <StateBadge tone="muted" icon={Archive}>
          {t(($) => $.hooks.configuration.parked)}
        </StateBadge>
      );
    case "disabled":
      return (
        <StateBadge tone="muted" icon={Ban}>
          {t(($) => $.hooks.configuration.disabled)}
        </StateBadge>
      );
    default:
      // A state this build does not know is shown as unrecognised rather than
      // guessed at. A newer server may report one, and mapping it onto
      // "registered" would be an invention.
      return (
        <StateBadge tone="unknown" icon={CircleHelp}>
          {t(($) => $.hooks.configuration.unknown, { value: entry.configuration })}
        </StateBadge>
      );
  }
}

export function EffectivenessBadge({ entry }: { entry: RuntimeHookEntry }) {
  const { t } = useT("runtimes");
  switch (entry.effectiveness) {
    case "will_run":
      return (
        <StateBadge tone="ok" icon={CircleCheck}>
          {t(($) => $.hooks.effectiveness.will_run)}
        </StateBadge>
      );
    case "never_runs":
      return (
        <StateBadge tone="bad" icon={CircleSlash}>
          {t(($) => $.hooks.effectiveness.never_runs)}
        </StateBadge>
      );
    case "trust_unknown":
      return (
        <StateBadge tone="warn" icon={KeyRound}>
          {t(($) => $.hooks.effectiveness.trust_unknown)}
        </StateBadge>
      );
    default:
      return (
        <StateBadge tone="unknown" icon={CircleHelp}>
          {t(($) => $.hooks.effectiveness.unknown, { value: entry.effectiveness })}
        </StateBadge>
      );
  }
}

/** The provider's reason, or null when the verdict carries none. */
export function useNeverRunsReason(entry: RuntimeHookEntry): string | null {
  const { t } = useT("runtimes");
  if (entry.effectiveness !== "never_runs") return null;
  const type = entry.handler_type || t(($) => $.hooks.detail.handler_type_undeclared);
  switch (entry.never_runs_reason) {
    case "matcher_ineligible":
      return t(($) => $.hooks.effectiveness.reason_matcher_ineligible);
    case "handler_type_unsupported_by_provider":
      return t(($) => $.hooks.effectiveness.reason_handler_unsupported_provider, { type });
    case "handler_type_unsupported_for_event":
      return t(($) => $.hooks.effectiveness.reason_handler_unsupported_event, { type });
    case undefined:
      return null;
    default:
      return t(($) => $.hooks.effectiveness.reason_unknown, {
        value: entry.never_runs_reason,
      });
  }
}

export function useMatcherKindLabel(entry: RuntimeHookEntry): {
  label: string;
  hint: string | null;
  tone: BadgeTone;
} {
  const { t } = useT("runtimes");
  switch (entry.matcher_kind) {
    case "all":
      return { label: t(($) => $.hooks.matcher.all), hint: null, tone: "muted" };
    case "exact":
      return { label: t(($) => $.hooks.matcher.exact), hint: null, tone: "muted" };
    case "regex":
      return { label: t(($) => $.hooks.matcher.regex), hint: null, tone: "muted" };
    case "ignored":
      return {
        label: t(($) => $.hooks.matcher.ignored),
        hint: t(($) => $.hooks.matcher.ignored_hint),
        tone: "warn",
      };
    case "unevaluable":
      // Multica's limit, not the provider's verdict. It gets its own visible
      // state so it can never be read as an ordinary matcher — and the hint
      // says explicitly that the hook may still run.
      return {
        label: t(($) => $.hooks.matcher.unevaluable),
        hint: t(($) => $.hooks.matcher.unevaluable_hint),
        tone: "unknown",
      };
    default:
      return {
        label: t(($) => $.hooks.matcher.unknown, { value: entry.matcher_kind }),
        hint: null,
        tone: "unknown",
      };
  }
}

/**
 * The matcher's evaluation path. An ordinary path is a quiet line of text; the
 * two that matter — "parsed, then ignored" and "Multica cannot evaluate this"
 * — take the flagged chip, so neither reads as routine and neither depends on
 * colour alone to say so.
 */
export function MatcherKindNote({ entry }: { entry: RuntimeHookEntry }) {
  const { label, tone } = useMatcherKindLabel(entry);
  if (tone === "warn" || tone === "unknown") {
    return (
      <StateBadge
        tone={tone}
        icon={tone === "unknown" ? CircleHelp : TriangleAlert}
      >
        {label}
      </StateBadge>
    );
  }
  return (
    <span className="block text-caption break-words text-muted-foreground">
      {label}
    </span>
  );
}

export function useTrustLabel(entry: RuntimeHookEntry): {
  label: string;
  hint: string | null;
} | null {
  const { t } = useT("runtimes");
  switch (entry.trust) {
    case "not_applicable":
      // Claude has no trust model; showing "not applicable" on every Claude
      // row would be noise, so the whole cell is dropped instead.
      return null;
    case "pending_review":
      return {
        label: t(($) => $.hooks.trust.pending_review),
        hint: t(($) => $.hooks.trust.pending_review_hint),
      };
    case "trusted_as_of_snapshot":
      return {
        label: t(($) => $.hooks.trust.trusted_as_of_snapshot),
        // The server's own caveat: Codex's hash algorithm is undocumented, so
        // Multica cannot confirm the definition is still unchanged.
        hint: entry.trust_caveat ?? null,
      };
    case "disabled":
      return { label: t(($) => $.hooks.trust.disabled), hint: null };
    case "managed_by_policy":
      return {
        label: t(($) => $.hooks.trust.managed_by_policy),
        hint: t(($) => $.hooks.trust.managed_by_policy_hint),
      };
    default:
      return {
        label: t(($) => $.hooks.trust.unknown, { value: entry.trust }),
        hint: null,
      };
  }
}

export function TrustBadge({ entry }: { entry: RuntimeHookEntry }) {
  const trust = useTrustLabel(entry);
  if (!trust) return null;
  const tone: BadgeTone =
    entry.trust === "managed_by_policy"
      ? "ok"
      : entry.trust === "pending_review"
        ? "warn"
        : "muted";
  return (
    <StateBadge tone={tone} icon={entry.trust === "managed_by_policy" ? ShieldCheck : KeyRound}>
      {trust.label}
    </StateBadge>
  );
}

/**
 * Scope badge. A writable scope gets the pencil, everything else gets the
 * lock — and project and local are never writable, so they always read as
 * read-only wherever they appear.
 */
export function ScopeBadge({
  scope,
  format,
  writable,
}: {
  scope: string;
  format: string;
  writable: boolean;
}) {
  const { t } = useT("runtimes");
  const Icon = writable ? Pencil : Lock;
  return (
    <Badge
      variant="ghost"
      className={cn(
        "h-auto gap-1 rounded-md border-transparent py-0.5 font-mono whitespace-normal",
        writable ? "bg-brand/10 text-brand" : "bg-muted text-muted-foreground",
      )}
      title={
        writable
          ? t(($) => $.hooks.scopes.writable)
          : t(($) => $.hooks.scopes.read_only)
      }
    >
      <Icon aria-hidden="true" />
      {`${scope}/${format}`}
    </Badge>
  );
}
