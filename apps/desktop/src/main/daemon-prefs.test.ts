// @vitest-environment node
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { daemonPrefsConfig } from "./daemon-prefs";

describe("daemonPrefsConfig", () => {
  it("preserves the stable prefs path and defaults", () => {
    expect(
      daemonPrefsConfig("/home/marty", {
        filename: "desktop_prefs.json",
        autoStart: true,
      }),
    ).toEqual({
      path: join("/home/marty", ".multica", "desktop_prefs.json"),
      defaults: { autoStart: true, autoStop: false },
    });
  });

  it("isolates dev preferences and defaults autoStart off", () => {
    expect(
      daemonPrefsConfig("/home/marty", {
        filename: "desktop_prefs-dev.json",
        autoStart: false,
      }),
    ).toEqual({
      path: join("/home/marty", ".multica", "desktop_prefs-dev.json"),
      defaults: { autoStart: false, autoStop: false },
    });
  });
});
