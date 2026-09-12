// @vitest-environment node

import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  HOOK_READ_POLL_INTERVAL_MS,
  resolveRuntimeHooks,
  runtimeHooksKeys,
  runtimeHooksOptions,
  type RuntimeHookDiscoveryProgress,
} from "./hooks";

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

  // The in-progress screen has to distinguish "waiting for the daemon's next
  // heartbeat" from "the daemon is reading the host", because the first wait
  // is not ours and the second one is. Both come from the read's own status,
  // so neither is a guess about progress.
  it("reports each discovery phase as the read reaches it", async () => {
    vi.useFakeTimers();
    try {
      initiateHookRead.mockResolvedValue({
        id: "req-1",
        runtime_id: "rt-1",
        status: "pending",
        cached: false,
        offline: false,
      });
      getHookReadResult
        .mockResolvedValueOnce({
          id: "req-1",
          runtime_id: "rt-1",
          status: "running",
          cached: false,
          offline: false,
        })
        .mockResolvedValueOnce({
          id: "req-1",
          runtime_id: "rt-1",
          status: "completed",
          cached: false,
          offline: false,
          observed_at: "2026-09-12T12:00:00Z",
        });

      const progress: RuntimeHookDiscoveryProgress[] = [];
      const settled = resolveRuntimeHooks("rt-1", (step) =>
        progress.push(step),
      );
      await vi.advanceTimersByTimeAsync(HOOK_READ_POLL_INTERVAL_MS * 3);

      await expect(settled).resolves.toMatchObject({ status: "completed" });
      expect(progress.map((step) => step.phase)).toEqual([
        "initiating",
        "queued",
        "reading",
      ]);
    } finally {
      vi.useRealTimers();
    }
  });

  it("reports the initiating phase even when no poll follows", async () => {
    initiateHookRead.mockResolvedValue({
      runtime_id: "rt-1",
      status: "completed",
      cached: true,
      offline: true,
      observed_at: "2026-09-12T12:00:00Z",
    });

    const progress: RuntimeHookDiscoveryProgress[] = [];
    await resolveRuntimeHooks("rt-1", (step) => progress.push(step));

    expect(progress).toEqual([
      { phase: "initiating", phaseTimeoutSeconds: null },
    ]);
  });

  // R1: the bound a progress screen shows has to be the server's own, so the
  // only thing this layer is allowed to do with `phase_timeout_seconds` is
  // carry it. The values below are deliberately not 30 and 60 — a transport
  // that substituted the real constants, or any constant, would fail here.
  it("carries the server's per-phase bound through to progress", async () => {
    vi.useFakeTimers();
    try {
      initiateHookRead.mockResolvedValue({
        id: "req-1",
        runtime_id: "rt-1",
        status: "pending",
        cached: false,
        offline: false,
        phase_timeout_seconds: 17,
      });
      getHookReadResult
        .mockResolvedValueOnce({
          id: "req-1",
          runtime_id: "rt-1",
          status: "running",
          cached: false,
          offline: false,
          phase_timeout_seconds: 41,
        })
        .mockResolvedValueOnce({
          id: "req-1",
          runtime_id: "rt-1",
          status: "completed",
          cached: false,
          offline: false,
          observed_at: "2026-09-12T12:00:00Z",
        });

      const progress: RuntimeHookDiscoveryProgress[] = [];
      const settled = resolveRuntimeHooks("rt-1", (step) =>
        progress.push(step),
      );
      await vi.advanceTimersByTimeAsync(HOOK_READ_POLL_INTERVAL_MS * 3);
      await expect(settled).resolves.toMatchObject({ status: "completed" });

      expect(progress).toEqual([
        { phase: "initiating", phaseTimeoutSeconds: null },
        { phase: "queued", phaseTimeoutSeconds: 17 },
        { phase: "reading", phaseTimeoutSeconds: 41 },
      ]);
    } finally {
      vi.useRealTimers();
    }
  });

  // A backend that predates the field reports no bound, and the progress
  // screen then shows no number rather than one this layer made up.
  it("reports no bound when the server sends none", async () => {
    vi.useFakeTimers();
    try {
      initiateHookRead.mockResolvedValue({
        id: "req-1",
        runtime_id: "rt-1",
        status: "pending",
        cached: false,
        offline: false,
      });
      getHookReadResult.mockResolvedValue({
        id: "req-1",
        runtime_id: "rt-1",
        status: "completed",
        cached: false,
        offline: false,
        observed_at: "2026-09-12T12:00:00Z",
      });

      const progress: RuntimeHookDiscoveryProgress[] = [];
      const settled = resolveRuntimeHooks("rt-1", (step) =>
        progress.push(step),
      );
      await vi.advanceTimersByTimeAsync(HOOK_READ_POLL_INTERVAL_MS * 2);
      await settled;

      expect(progress).toEqual([
        { phase: "initiating", phaseTimeoutSeconds: null },
        { phase: "queued", phaseTimeoutSeconds: null },
      ]);
    } finally {
      vi.useRealTimers();
    }
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
