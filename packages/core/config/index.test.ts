// @vitest-environment node
import { afterEach, describe, expect, it } from "vitest";
import { configStore, featureFlagEnabled } from "./index";

afterEach(() => {
  configStore.getState().setFeatureFlags(undefined);
});

describe("featureFlagEnabled", () => {
  it("resolves true for a published flag", () => {
    expect(featureFlagEnabled({ composio_mcp_apps: true }, "composio_mcp_apps")).toBe(
      true,
    );
  });

  it("fails closed for an unpublished flag even when other flags are present", () => {
    expect(featureFlagEnabled({ composio_mcp_apps: true }, "project_plans")).toBe(
      false,
    );
  });

  it("fails closed when flags have not been hydrated yet", () => {
    expect(featureFlagEnabled(undefined, "composio_mcp_apps")).toBe(false);
  });

  it("respects an explicit default for callers that opt out of fail-closed", () => {
    expect(featureFlagEnabled(undefined, "composio_mcp_apps", true)).toBe(true);
  });
});

describe("configStore.setFeatureFlags", () => {
  it("hydrates featureFlags from a published server response", () => {
    configStore.getState().setFeatureFlags({ composio_mcp_apps: true });
    expect(configStore.getState().featureFlags).toEqual({
      composio_mcp_apps: true,
    });
  });

  it("resets to empty when called without a response, so every flag fails closed", () => {
    configStore.getState().setFeatureFlags({ composio_mcp_apps: true });
    configStore.getState().setFeatureFlags(undefined);
    expect(configStore.getState().featureFlags).toEqual({});
    expect(featureFlagEnabled(configStore.getState().featureFlags, "composio_mcp_apps")).toBe(
      false,
    );
  });
});
