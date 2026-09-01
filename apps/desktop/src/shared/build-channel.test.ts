// @vitest-environment node
import { describe, expect, it } from "vitest";
import { decorateWindowTitle, packagedUserDataPath } from "./build-channel";

describe("build channel runtime helpers", () => {
  it("leaves stable titles unchanged and visibly decorates dev titles", () => {
    expect(decorateWindowTitle("Issues", "", "")).toBe("Issues");
    expect(decorateWindowTitle("MUL-1", "[DEV] ", "Multica Dev")).toBe(
      "[DEV] MUL-1",
    );
    expect(decorateWindowTitle("", "[DEV] ", "Multica Dev")).toBe(
      "[DEV] Multica Dev",
    );
  });

  it("derives separate packaged userData paths from product names", () => {
    expect(packagedUserDataPath("/config", "Multica")).toBe(
      "/config/Multica",
    );
    expect(packagedUserDataPath("/config", "Multica Dev")).toBe(
      "/config/Multica Dev",
    );
  });
});
