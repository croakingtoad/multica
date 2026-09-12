// @vitest-environment node

import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { resolveRuntimeHooks, runtimeHooksKeys, runtimeHooksOptions } from "./hooks";

const initiateHookRead = vi.fn();
const getHookReadResult = vi.fn();

vi.mock("../api", () => ({
  api: {
    initiateHookRead: (runtimeId: string) => initiateHookRead(runtimeId),
    getHookReadResult: (runtimeId: string, requestId: string) =>
      getHookReadResult(runtimeId, requestId),
  },
}));

beforeEach(() => {
  initiateHookRead.mockReset();
  getHookReadResult.mockReset();
});

describe("runtime hooks", () => {
  it("returns an offline dated snapshot without polling", async () => {
    const snapshot = {
      runtime_id: "rt-1",
      status: "completed",
      cached: true,
      observed_at: "2026-09-12T12:00:00Z",
      sources: [],
    };
    initiateHookRead.mockResolvedValue(snapshot);

    await expect(resolveRuntimeHooks("rt-1")).resolves.toEqual(snapshot);
    expect(getHookReadResult).not.toHaveBeenCalled();
  });

  it("preserves did-not-answer as a distinct terminal result", async () => {
    const timedOut = {
      id: "req-1",
      runtime_id: "rt-1",
      status: "timed_out",
      cached: false,
      error: "daemon did not answer within 30 seconds",
    };
    initiateHookRead.mockResolvedValue(timedOut);

    await expect(resolveRuntimeHooks("rt-1")).resolves.toEqual(timedOut);
  });

  it("builds disabled and per-runtime query options", () => {
    expect(runtimeHooksOptions(null).queryKey).toEqual(runtimeHooksKeys.all());
    expect(runtimeHooksOptions(null).enabled).toBe(false);
    expect(runtimeHooksOptions("rt-1").queryKey).toEqual(
      runtimeHooksKeys.forRuntime("rt-1"),
    );
  });

  it("does not serve a previous observation after remount", async () => {
    const previous = {
      runtime_id: "rt-1",
      status: "completed",
      cached: false,
      observed_at: "2026-09-12T12:00:00Z",
      sources: [],
    };
    initiateHookRead.mockResolvedValueOnce(previous);

    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false, staleTime: Infinity, gcTime: 10 * 60_000 },
      },
    });
    const options = runtimeHooksOptions("rt-1");
    const firstObserver = new QueryObserver(client, options);
    const unsubscribeFirst = firstObserver.subscribe(() => {});

    await vi.waitFor(() => {
      expect(firstObserver.getCurrentResult().data).toEqual(previous);
    });
    unsubscribeFirst();
    await vi.waitFor(() => {
      expect(client.getQueryState(options.queryKey)).toBeUndefined();
    });

    initiateHookRead.mockImplementationOnce(() => new Promise(() => {}));
    const secondObserver = new QueryObserver(client, options);
    const remountObservations: (string | undefined)[] = [];
    const unsubscribeSecond = secondObserver.subscribe((result) => {
      remountObservations.push(result.data?.observed_at);
    });

    try {
      expect(secondObserver.getCurrentResult().data).toBeUndefined();
      expect(remountObservations).not.toContain(previous.observed_at);
    } finally {
      unsubscribeSecond();
      client.clear();
    }
  });
});
