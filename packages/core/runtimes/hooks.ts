import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { RuntimeHookReadRequest } from "../types";

export const runtimeHooksKeys = {
  all: () => ["runtimes", "hooks"] as const,
  forRuntime: (runtimeId: string) =>
    [...runtimeHooksKeys.all(), runtimeId] as const,
};

const POLL_INTERVAL_MS = 500;
const POLL_TIMEOUT_MS = 100_000;

export async function resolveRuntimeHooks(
  runtimeId: string,
): Promise<RuntimeHookReadRequest> {
  const initial = await api.initiateHookRead(runtimeId);
  const start = Date.now();
  let current = initial;

  while (current.status === "pending" || current.status === "running") {
    if (Date.now() - start > POLL_TIMEOUT_MS) {
      throw new Error("runtime hook discovery timed out while polling");
    }
    if (!initial.id) {
      throw new Error("runtime hook discovery did not return a request id");
    }
    await new Promise((resolve) => setTimeout(resolve, POLL_INTERVAL_MS));
    current = await api.getHookReadResult(runtimeId, initial.id);
  }

  return current;
}

export function runtimeHooksOptions(runtimeId: string | null | undefined) {
  return queryOptions({
    queryKey: runtimeId
      ? runtimeHooksKeys.forRuntime(runtimeId)
      : runtimeHooksKeys.all(),
    queryFn: () => resolveRuntimeHooks(runtimeId as string),
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
