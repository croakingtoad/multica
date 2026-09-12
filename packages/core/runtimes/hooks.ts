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
    staleTime: 0,
    retry: false,
  });
}
