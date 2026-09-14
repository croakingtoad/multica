import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { RuntimeHookEventAnswerResult } from "../types";

// The per-event answer query. It lives beside hooks.ts rather than inside it
// because hooks.ts owns the observation that the Lifecycle Hooks tab renders,
// and its staleTime/gcTime pair carries snapshot invariant 1 for that
// observation. Nothing here may relax either of them.
//
// The same two zeroes are set here for the same reason. An answer is computed
// from the stored snapshot, so a cached one would be an answer about a host
// state that has since been re-read — and the answer's own observed_at is how
// a consumer tells whether it belongs to the observation on screen.

export const runtimeHookAnswerKeys = {
  all: () => ["runtimes", "hooks", "answer"] as const,
  forRuntime: (runtimeId: string) =>
    [...runtimeHookAnswerKeys.all(), runtimeId] as const,
  forEvent: (runtimeId: string, event: string, value: string) =>
    [...runtimeHookAnswerKeys.forRuntime(runtimeId), event, value] as const,
};

/**
 * Answer one event for one candidate value.
 *
 * `enabled` needs all three: a matcher is evaluated against a value, so an
 * empty value has no answer and must not produce a request whose empty result
 * could be rendered as "nothing fires". The server refuses such a request too
 * — this is the client half of the same rule, not a substitute for it.
 */
export function runtimeHookAnswerOptions(
  runtimeId: string | null | undefined,
  event: string | null | undefined,
  value: string,
) {
  const trimmed = value.trim();
  const enabled = Boolean(runtimeId) && Boolean(event) && trimmed.length > 0;
  return queryOptions({
    queryKey: enabled
      ? runtimeHookAnswerKeys.forEvent(runtimeId as string, event as string, trimmed)
      : runtimeHookAnswerKeys.all(),
    queryFn: (): Promise<RuntimeHookEventAnswerResult> =>
      api.answerHookEvent(runtimeId as string, event as string, trimmed),
    enabled,
    staleTime: 0,
    gcTime: 0,
    retry: false,
  });
}
