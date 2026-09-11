// @vitest-environment node

import { describe, it, expect, vi, beforeEach } from "vitest";
import { ApiError } from "../api";

const initiate = vi.hoisted(() => vi.fn());
const poll = vi.hoisted(() => vi.fn());

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    api: {
      initiateDaemonPathCheck: (...args: unknown[]) => initiate(...args),
      getDaemonPathCheck: (...args: unknown[]) => poll(...args),
    },
  };
});

import { checkDaemonPath, isAbsoluteDaemonPath } from "./daemon-path-check";

const ARGS = { workspaceId: "ws-1", daemonId: "daemon-a", path: "/srv/app" };

function completed(overrides: Record<string, unknown> = {}) {
  return {
    status: "completed",
    result: {
      exists: true,
      is_directory: true,
      readable: true,
      writable: true,
      is_git_repo: true,
      reason: "",
      ...overrides,
    },
  };
}

beforeEach(() => {
  initiate.mockReset();
  poll.mockReset();
  initiate.mockResolvedValue({ request_id: "req-1" });
});

describe("isAbsoluteDaemonPath", () => {
  // The daemon being asked may be a Windows box; rejecting its native path
  // shape in the browser would make that machine unreachable from the picker.
  it("accepts POSIX, Windows drive and UNC paths", () => {
    expect(isAbsoluteDaemonPath("/srv/app")).toBe(true);
    expect(isAbsoluteDaemonPath("C:\\srv\\app")).toBe(true);
    expect(isAbsoluteDaemonPath("C:/srv/app")).toBe(true);
    expect(isAbsoluteDaemonPath("\\\\nas\\share")).toBe(true);
  });

  it("rejects a relative path", () => {
    expect(isAbsoluteDaemonPath("srv/app")).toBe(false);
    expect(isAbsoluteDaemonPath("./app")).toBe(false);
    expect(isAbsoluteDaemonPath("~/app")).toBe(false);
  });
});

describe("checkDaemonPath", () => {
  it("reports a usable directory and whether it is a repo", async () => {
    poll.mockResolvedValue(completed());
    await expect(checkDaemonPath(ARGS)).resolves.toEqual({
      ok: true,
      isGitRepo: true,
    });
  });

  it("passes is_git_repo: false through so worktree mode can be blocked", async () => {
    poll.mockResolvedValue(completed({ is_git_repo: false }));
    await expect(checkDaemonPath(ARGS)).resolves.toEqual({
      ok: true,
      isGitRepo: false,
    });
  });

  // A relative path never reaches the network: the answer is known, and a round
  // trip to learn "start with a /" is a worse dialog.
  it("rejects a non-absolute path without calling the API", async () => {
    await expect(
      checkDaemonPath({ ...ARGS, path: "work/app" }),
    ).resolves.toEqual({ ok: false, reason: "not_absolute" });
    expect(initiate).not.toHaveBeenCalled();
  });

  it("trims the path before sending it", async () => {
    poll.mockResolvedValue(completed());
    await checkDaemonPath({ ...ARGS, path: "  /srv/app  " });
    expect(initiate).toHaveBeenCalledWith("ws-1", "daemon-a", "/srv/app");
  });

  it("polls until the check leaves pending", async () => {
    poll
      .mockResolvedValueOnce({ status: "pending" })
      .mockResolvedValueOnce({ status: "pending" })
      .mockResolvedValueOnce(completed());
    await expect(checkDaemonPath(ARGS)).resolves.toEqual({
      ok: true,
      isGitRepo: true,
    });
    expect(poll).toHaveBeenCalledTimes(3);
  });

  it.each([
    ["not_found", { exists: false }],
    ["not_a_directory", { is_directory: false }],
    ["not_readable", { readable: false }],
    ["not_writable", { writable: false }],
  ])("names %s from the daemon's booleans alone", async (reason, flags) => {
    poll.mockResolvedValue(completed(flags));
    await expect(checkDaemonPath(ARGS)).resolves.toEqual({
      ok: false,
      reason,
    });
  });

  // The daemon's own `reason` outranks the booleans when it sets one: it knows
  // which check failed first, and a stat that could not run leaves the rest of
  // the flags at their pessimistic defaults.
  it("prefers the daemon's stated reason over the flags", async () => {
    poll.mockResolvedValue(
      completed({ exists: false, is_directory: false, reason: "not_readable" }),
    );
    await expect(checkDaemonPath(ARGS)).resolves.toEqual({
      ok: false,
      reason: "not_readable",
    });
  });

  it("reports a machine with no online runtime as offline, not a bad path", async () => {
    initiate.mockRejectedValue(
      new ApiError("machine offline", 503, "Service Unavailable"),
    );
    await expect(checkDaemonPath(ARGS)).resolves.toEqual({
      ok: false,
      reason: "machine_offline",
    });
  });

  it.each([403, 404])(
    "reports a daemon the caller does not own (%i) as not permitted",
    async (status) => {
      initiate.mockRejectedValue(new ApiError("nope", status, "Forbidden"));
      await expect(checkDaemonPath(ARGS)).resolves.toEqual({
        ok: false,
        reason: "not_permitted",
      });
    },
  );

  it("uses a 4xx body reason when the API rejects at its own boundary", async () => {
    initiate.mockRejectedValue(
      new ApiError("bad request", 400, "Bad Request", {
        reason: "not_absolute",
      }),
    );
    await expect(checkDaemonPath(ARGS)).resolves.toEqual({
      ok: false,
      reason: "not_absolute",
    });
  });

  it("surfaces a server-side timeout as its own reason", async () => {
    poll.mockResolvedValue({ status: "timeout" });
    await expect(checkDaemonPath(ARGS)).resolves.toEqual({
      ok: false,
      reason: "check_timed_out",
    });
  });

  // The api client answers a drifted response with the malformed fallback
  // (status "failed") rather than throwing, which must not read as a verdict
  // about the path — and must never read as success.
  it("fails the check when the response could not be parsed", async () => {
    poll.mockResolvedValue({
      status: "failed",
      error: "invalid path check response",
    });
    await expect(checkDaemonPath(ARGS)).resolves.toEqual({
      ok: false,
      reason: "error",
      error: "invalid path check response",
    });
  });

  it("fails the check when a completed response carries no result", async () => {
    poll.mockResolvedValue({ status: "completed" });
    const result = await checkDaemonPath(ARGS);
    expect(result.ok).toBe(false);
    expect(result.reason).toBe("error");
  });

  // An empty request id is what the malformed-202 fallback produces. Polling
  // `.../path-checks/` would 404 and be reported as "not your daemon".
  it("fails the check when the create response has no request id", async () => {
    initiate.mockResolvedValue({ request_id: "" });
    await expect(checkDaemonPath(ARGS)).resolves.toEqual({
      ok: false,
      reason: "error",
    });
    expect(poll).not.toHaveBeenCalled();
  });

  it("never throws when the transport does", async () => {
    initiate.mockRejectedValue(new Error("network down"));
    await expect(checkDaemonPath(ARGS)).resolves.toEqual({
      ok: false,
      reason: "error",
      error: "network down",
    });
  });
});
