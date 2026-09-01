// @vitest-environment node

import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, SharedStateMutationBlockedError } from "./client";

afterEach(() => {
  vi.unstubAllGlobals();
});

const okResponse = () => new Response(null, { status: 204 });

describe("ApiClient shared-state mutation guard", () => {
  it("leaves every mutating method unchanged when no breaking-build guard is installed", async () => {
    const fetchMock = vi.fn().mockImplementation(async () => okResponse());
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://stable.example.test");

    await client.logout();
    await client.patchOnboarding({ questionnaire: {} });
    await client.updateIssue("issue-1", {});
    await client.deleteIssue("issue-1");

    expect(fetchMock.mock.calls.map((call) => call[1]?.method)).toEqual([
      "POST",
      "PATCH",
      "PUT",
      "DELETE",
    ]);
  });

  it("blocks all mutations until the selected sacrificial target is confirmed", async () => {
    const fetchMock = vi.fn().mockImplementation(async () => okResponse());
    const confirmTarget = vi
      .fn()
      .mockResolvedValueOnce(false)
      .mockResolvedValueOnce(true);
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://sacrificial.example.test", {
      sharedStateMutationGuard: { confirmTarget },
    });

    await expect(client.logout()).rejects.toBeInstanceOf(
      SharedStateMutationBlockedError,
    );
    expect(fetchMock).not.toHaveBeenCalled();

    await client.logout();
    await client.patchOnboarding({ questionnaire: {} });
    await client.updateIssue("issue-1", {});
    await client.deleteIssue("issue-1");

    expect(confirmTarget).toHaveBeenCalledTimes(2);
    expect(confirmTarget).toHaveBeenLastCalledWith({
      backendUrl: "https://sacrificial.example.test",
      workspaceSlug: null,
    });
    expect(fetchMock.mock.calls.map((call) => call[1]?.method)).toEqual([
      "POST",
      "PATCH",
      "PUT",
      "DELETE",
    ]);
  });

  it("always permits GET and HEAD in a breaking build", async () => {
    const fetchMock = vi.fn().mockImplementation(async () => okResponse());
    const confirmTarget = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://sacrificial.example.test", {
      sharedStateMutationGuard: { confirmTarget },
    });

    await client.getMe();
    await client["fetchRaw"]("/health", { method: "HEAD" });

    expect(confirmTarget).not.toHaveBeenCalled();
    expect(fetchMock.mock.calls.map((call) => call[1]?.method ?? "GET")).toEqual([
      "GET",
      "HEAD",
    ]);
  });

  it("requires a new confirmation when an explicit workspace target changes", async () => {
    const fetchMock = vi.fn().mockImplementation(async () => okResponse());
    const confirmTarget = vi.fn().mockResolvedValue(true);
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://sacrificial.example.test", {
      sharedStateMutationGuard: { confirmTarget },
    });

    const payload = { runtime_id: "runtime-1", language: "en" as const };
    await client.createMikaAgent(payload, "throwaway-one");
    await client.createMikaAgent(payload, "throwaway-one");
    await client.createMikaAgent(payload, "throwaway-two");

    expect(confirmTarget).toHaveBeenCalledTimes(2);
    expect(confirmTarget.mock.calls.map(([target]) => target.workspaceSlug)).toEqual([
      "throwaway-one",
      "throwaway-two",
    ]);
  });
});
