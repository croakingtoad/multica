// @vitest-environment node

import { describe, expect, it, vi } from "vitest";
import {
  createSharedStateMutationGuard,
  sacrificialTargetWarning,
} from "./shared-state-mutation-guard";

describe("createSharedStateMutationGuard", () => {
  it("never installs a guard in a stable-channel build", () => {
    expect(
      createSharedStateMutationGuard("stable", "breaking", vi.fn()),
    ).toBeUndefined();
  });

  it("only installs a guard for a breaking dev build", () => {
    expect(
      createSharedStateMutationGuard("dev", "stable", vi.fn()),
    ).toBeUndefined();
    expect(
      createSharedStateMutationGuard("dev", "breaking", vi.fn()),
    ).toEqual({ confirmTarget: expect.any(Function) });
  });
});

describe("sacrificialTargetWarning", () => {
  it("names the exact backend and workspace being unlocked", () => {
    expect(
      sacrificialTargetWarning({
        backendUrl: "https://api.example.test",
        workspaceSlug: "throwaway",
      }),
    ).toContain("https://api.example.test\nWorkspace: throwaway");
  });
});
