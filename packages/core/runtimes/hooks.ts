import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { RuntimeHookReadRequest } from "../types";

export const runtimeHooksKeys = {
  all: () => ["runtimes", "hooks"] as const,
  forRuntime: (runtimeId: string) =>
    [...runtimeHooksKeys.all(), runtimeId] as const,
};

export const HOOK_READ_POLL_INTERVAL_MS = 500;
export const HOOK_READ_POLL_TIMEOUT_MS = 100_000;

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

export async function resolveRuntimeHooks(
  runtimeId: string,
  onPhase?: (phase: RuntimeHookDiscoveryPhase) => void,
): Promise<RuntimeHookReadRequest> {
  onPhase?.("initiating");
  const initial = await api.initiateHookRead(runtimeId);
  const start = Date.now();
  let current = initial;

  while (current.status === "pending" || current.status === "running") {
    onPhase?.(current.status === "pending" ? "queued" : "reading");
    if (Date.now() - start > HOOK_READ_POLL_TIMEOUT_MS) {
      throw new Error("runtime hook discovery timed out while polling");
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
  onPhase?: (phase: RuntimeHookDiscoveryPhase) => void,
) {
  return queryOptions({
    queryKey: runtimeId
      ? runtimeHooksKeys.forRuntime(runtimeId)
      : runtimeHooksKeys.all(),
    queryFn: () => resolveRuntimeHooks(runtimeId as string, onPhase),
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
