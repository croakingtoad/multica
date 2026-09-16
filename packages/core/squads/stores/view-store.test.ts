// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { useSquadsViewStore } from "./view-store";
import { setCurrentWorkspace } from "../../platform/workspace-storage";

const flush = () => new Promise((resolve) => queueMicrotask(() => resolve(null)));

beforeEach(() => {
  localStorage.clear();
  useSquadsViewStore.setState({ scope: "mine" });
  setCurrentWorkspace(null, null);
});

afterEach(() => {
  setCurrentWorkspace(null, null);
});

describe("useSquadsViewStore", () => {

  it("partialize persists view prefs (no actions) under the workspace-namespaced key", async () => {
    setCurrentWorkspace("acme", "ws_a");
    await flush();
    useSquadsViewStore.getState().setScope("all");

    const raw = localStorage.getItem("multica_squads_view:acme");
    expect(raw).not.toBeNull();
    const parsed = JSON.parse(raw as string);
    expect(Object.keys(parsed.state).sort()).toEqual([
      "filters",
      "hiddenColumns",
      "scope",
      "sortDirection",
      "sortField",
    ]);
    expect(parsed.state.scope).toBe("all");
  });

  it("rehydrates a different saved scope on workspace switch", async () => {
    localStorage.setItem(
      "multica_squads_view:acme",
      JSON.stringify({ state: { scope: "all" }, version: 0 }),
    );
    localStorage.setItem(
      "multica_squads_view:beta",
      JSON.stringify({ state: { scope: "mine" }, version: 0 }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();
    expect(useSquadsViewStore.getState().scope).toBe("all");

    setCurrentWorkspace("beta", "ws_b");
    await flush();
    await flush();
    expect(useSquadsViewStore.getState().scope).toBe("mine");
  });

  it("resets to 'mine' when switching to a workspace with no persisted value", async () => {
    localStorage.setItem(
      "multica_squads_view:acme",
      JSON.stringify({ state: { scope: "all" }, version: 0 }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();
    expect(useSquadsViewStore.getState().scope).toBe("all");

    setCurrentWorkspace("beta", "ws_b");
    await flush();
    await flush();
    expect(useSquadsViewStore.getState().scope).toBe("mine");
    expect(localStorage.getItem("multica_squads_view:acme")).not.toBeNull();
  });
});
