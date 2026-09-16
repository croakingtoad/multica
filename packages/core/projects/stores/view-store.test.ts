// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { useProjectViewStore } from "./view-store";
import { setCurrentWorkspace } from "../../platform/workspace-storage";

const flush = () => new Promise((resolve) => queueMicrotask(() => resolve(null)));

beforeEach(() => {
  localStorage.clear();
  useProjectViewStore.setState({ viewMode: "compact" });
  setCurrentWorkspace(null, null);
});

afterEach(() => {
  setCurrentWorkspace(null, null);
});

describe("useProjectViewStore", () => {

  it("partialize persists view prefs (no actions) under the workspace-namespaced key", async () => {
    setCurrentWorkspace("acme", "ws_a");
    await flush();
    useProjectViewStore.getState().setViewMode("comfortable");

    const raw = localStorage.getItem("multica_projects_view:acme");
    expect(raw).not.toBeNull();
    const parsed = JSON.parse(raw as string);
    expect(Object.keys(parsed.state).sort()).toEqual([
      "filters",
      "hiddenColumns",
      "sortDirection",
      "sortField",
      "viewMode",
    ]);
    expect(parsed.state.viewMode).toBe("comfortable");
  });

  it("rehydrates a different saved viewMode on workspace switch", async () => {
    localStorage.setItem(
      "multica_projects_view:acme",
      JSON.stringify({ state: { viewMode: "comfortable" }, version: 0 }),
    );
    localStorage.setItem(
      "multica_projects_view:beta",
      JSON.stringify({ state: { viewMode: "compact" }, version: 0 }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();
    expect(useProjectViewStore.getState().viewMode).toBe("comfortable");

    setCurrentWorkspace("beta", "ws_b");
    await flush();
    await flush();
    expect(useProjectViewStore.getState().viewMode).toBe("compact");
  });

  it("resets to 'compact' when switching to a workspace with no persisted value", async () => {
    localStorage.setItem(
      "multica_projects_view:acme",
      JSON.stringify({ state: { viewMode: "comfortable" }, version: 0 }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();
    expect(useProjectViewStore.getState().viewMode).toBe("comfortable");

    setCurrentWorkspace("beta", "ws_b");
    await flush();
    await flush();
    expect(useProjectViewStore.getState().viewMode).toBe("compact");
    expect(localStorage.getItem("multica_projects_view:acme")).not.toBeNull();
  });
});
