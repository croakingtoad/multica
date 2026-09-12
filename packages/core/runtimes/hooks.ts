import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { RuntimeHookReadRequest } from "../types";

export const runtimeHooksKeys = {
  all: () => ["runtimes", "hooks"] as const,
  forRuntime: (runtimeId: string) =>
    [...runtimeHooksKeys.all(), runtimeId] as const,
};

export const HOOK_READ_POLL_INTERVAL_MS = 500;

/**
 * A backstop against a server that never returns a terminal status, and only
 * that. It is deliberately far longer than any bound the server enforces —
 * `applyHookReadTimeout` ends a queued read at 30 s and a running one at 60 s
 * — so reaching this value means the response stopped changing rather than
 * that the read ran out of time. It is not the timeout a reader should be
 * shown: that number is `phase_timeout_seconds`, which the server sends.
 */
export const HOOK_READ_POLL_BACKSTOP_MS = 100_000;

/**
 * Where a discovery has actually got to. Reported as it happens so the
 * in-progress screen can name the wait instead of showing an undifferentiated
 * spinner: `queued` in particular is time spent waiting for the daemon's next
 * heartbeat, which is the runtime's turn rather than ours.
 *
 * These are the only three phases the read has. There is no progress fraction
 * here, because the pipeline reports none — a percentage would be invented.
 */
export type RuntimeHookDiscoveryPhase = "initiating" | "queued" | "reading";

/**
 * A phase plus the bound the server says applies to it. The bound travels with
 * the phase because it is phase-specific and because it is not ours to know:
 * the server enforces it, so the server reports it, and `null` until an answer
 * carrying one has arrived. A screen with `null` here shows no number, which
 * is the only honest alternative to repeating a server constant.
 */
export interface RuntimeHookDiscoveryProgress {
  phase: RuntimeHookDiscoveryPhase;
  phaseTimeoutSeconds: number | null;
}

export async function resolveRuntimeHooks(
  runtimeId: string,
  onProgress?: (progress: RuntimeHookDiscoveryProgress) => void,
): Promise<RuntimeHookReadRequest> {
  // Nothing has answered yet, so there is no bound to report with it.
  onProgress?.({ phase: "initiating", phaseTimeoutSeconds: null });
  const initial = await api.initiateHookRead(runtimeId);
  const start = Date.now();
  let current = initial;

  while (current.status === "pending" || current.status === "running") {
    onProgress?.({
      phase: current.status === "pending" ? "queued" : "reading",
      phaseTimeoutSeconds: current.phase_timeout_seconds ?? null,
    });
    if (Date.now() - start > HOOK_READ_POLL_BACKSTOP_MS) {
      throw new Error("runtime hook discovery stopped answering while polling");
    }
    if (!initial.id) {
      throw new Error("runtime hook discovery did not return a request id");
    }
    await new Promise((resolve) =>
      setTimeout(resolve, HOOK_READ_POLL_INTERVAL_MS),
    );
    current = await api.getHookReadResult(runtimeId, initial.id);
  }

  return current;
}

export function runtimeHooksOptions(
  runtimeId: string | null | undefined,
  onProgress?: (progress: RuntimeHookDiscoveryProgress) => void,
) {
  return queryOptions({
    queryKey: runtimeId
      ? runtimeHooksKeys.forRuntime(runtimeId)
      : runtimeHooksKeys.all(),
    queryFn: () => resolveRuntimeHooks(runtimeId as string, onProgress),
    enabled: Boolean(runtimeId),
    // Snapshot invariant 1: an online runtime must render only the observation
    // produced by the discovery initiated for the current mount. staleTime: 0
    // forces that discovery instead of extending a prior observation's
    // freshness, while gcTime: 0 removes that observation when its final
    // observer unmounts so it cannot render during the next discovery.
    staleTime: 0,
    gcTime: 0,
    retry: false,
  });
}
