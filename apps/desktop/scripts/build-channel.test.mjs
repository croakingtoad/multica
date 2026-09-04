// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  BUILD_CHANNELS,
  builderConfigForChannel,
  buildChannelDefines,
  requireSharedStateCompatibility,
  resolveBuildChannel,
} from "./build-channel.mjs";

describe("resolveBuildChannel", () => {
  it("keeps stable as the default when MULTICA_CHANNEL is unset", () => {
    expect(resolveBuildChannel({})).toBe(BUILD_CHANNELS.stable);
    expect(resolveBuildChannel({ MULTICA_CHANNEL: "stable" })).toBe(
      BUILD_CHANNELS.stable,
    );
  });

  it("selects a completely separate packaged identity for dev", () => {
    const stable = resolveBuildChannel({});
    const dev = resolveBuildChannel({ MULTICA_CHANNEL: "dev" });

    expect(dev).toMatchObject({
      name: "dev",
      packageName: "@multica/dev-desktop",
      productName: "Multica Dev",
      appId: "ai.multica.desktop.dev",
      executableName: "multica-desktop-dev",
      startupWmClass: "Multica Dev",
      protocolScheme: "multica-dev",
      titlePrefix: "[DEV] ",
      titleFallback: "Multica Dev",
      runtimeIcon: "resources/icon-dev.png",
      macIcon: "build/dev/icon.icns",
      windowsIcon: "build/dev/icon.ico",
      linuxIcon: "build/dev/icons",
      daemonProfileSuffix: "-dev",
      daemonPrefsFilename: "desktop_prefs-dev.json",
      runtimeConfigFilename: "desktop-dev.json",
      daemonAutoStart: false,
      updatesEnabled: false,
    });
    for (const key of [
      "packageName",
      "productName",
      "appId",
      "executableName",
      "startupWmClass",
      "protocolScheme",
      "daemonPrefsFilename",
      "runtimeConfigFilename",
    ]) {
      expect(dev[key]).not.toBe(stable[key]);
    }
    expect(stable.runtimeConfigFilename).toBe("desktop.json");
  });

  it("rejects unknown channels instead of silently producing stable", () => {
    expect(() => resolveBuildChannel({ MULTICA_CHANNEL: "canary" })).toThrow(
      /MULTICA_CHANNEL must be "stable" or "dev"/,
    );
  });
});

describe("buildChannelDefines", () => {
  it("bakes the selected channel and metadata into literal Vite definitions", () => {
    const definitions = buildChannelDefines(BUILD_CHANNELS.dev, "breaking");

    expect(JSON.parse(definitions.__MULTICA_BUILD_CHANNEL__)).toBe("dev");
    expect(JSON.parse(definitions.__MULTICA_SHARED_STATE_COMPAT__)).toBe(
      "breaking",
    );
    expect(JSON.parse(definitions.__MULTICA_BUILD_CHANNEL_CONFIG__)).toEqual(
      BUILD_CHANNELS.dev,
    );
    expect(JSON.stringify(definitions)).not.toContain("process.env");
  });
});

describe("requireSharedStateCompatibility", () => {
  it("requires an explicit stable or breaking package declaration", () => {
    expect(() => requireSharedStateCompatibility({})).toThrow(
      /MULTICA_SHARED_STATE_COMPAT must be "stable" or "breaking"/,
    );
    expect(() =>
      requireSharedStateCompatibility({ MULTICA_SHARED_STATE_COMPAT: "safe" }),
    ).toThrow(/MULTICA_SHARED_STATE_COMPAT must be "stable" or "breaking"/);
    expect(
      requireSharedStateCompatibility({
        MULTICA_SHARED_STATE_COMPAT: "stable",
      }),
    ).toBe("stable");
    expect(
      requireSharedStateCompatibility({
        MULTICA_SHARED_STATE_COMPAT: "breaking",
      }),
    ).toBe("breaking");
  });
});

describe("builderConfigForChannel", () => {
  it("drives every stable identity surface from the canonical definition", () => {
    expect(
      builderConfigForChannel(BUILD_CHANNELS.stable, "stable"),
    ).toMatchObject({
      appId: "ai.multica.desktop",
      productName: "Multica",
      extraMetadata: {
        name: "@multica/desktop",
        productName: "Multica",
        multicaChannel: "stable",
        multicaSharedStateCompat: "stable",
      },
      protocols: { name: "Multica", schemes: ["multica"] },
      nsis: {
        oneClick: true,
        perMachine: false,
        createDesktopShortcut: true,
        createStartMenuShortcut: true,
        shortcutName: "Multica",
        uninstallDisplayName: "Multica ${version}",
      },
      linux: {
        executableName: "multica-desktop",
        icon: "build/icons",
        desktop: { entry: { StartupWMClass: "Multica" } },
      },
    });
  });

  it("produces a complete, separate builder identity for dev", () => {
    expect(
      builderConfigForChannel(BUILD_CHANNELS.dev, "breaking"),
    ).toMatchObject({
      appId: "ai.multica.desktop.dev",
      productName: "Multica Dev",
      extraMetadata: {
        name: "@multica/dev-desktop",
        productName: "Multica Dev",
        multicaChannel: "dev",
        multicaSharedStateCompat: "breaking",
      },
      protocols: { name: "Multica Dev", schemes: ["multica-dev"] },
      nsis: {
        oneClick: true,
        perMachine: false,
        createDesktopShortcut: true,
        createStartMenuShortcut: true,
        shortcutName: "Multica Dev",
        uninstallDisplayName: "Multica Dev ${version}",
      },
      mac: {
        icon: "build/dev/icon.icns",
        artifactName: "multica-desktop-dev-${version}-mac-${arch}.${ext}",
      },
      win: {
        icon: "build/dev/icon.ico",
        artifactName: "multica-desktop-dev-${version}-windows-${arch}.${ext}",
      },
      linux: {
        icon: "build/dev/icons",
        executableName: "multica-desktop-dev",
        artifactName: "multica-desktop-dev-${version}-linux-${arch}.${ext}",
        desktop: { entry: { StartupWMClass: "Multica Dev" } },
      },
    });

    // electron-builder 26.8.1 removes the package-name slash to derive the
    // one-click NSIS install directory. The generated installer matches and
    // kills every process whose path starts with that directory, so neither
    // channel's directory may equal or prefix the other.
    const stableInstallDir = BUILD_CHANNELS.stable.packageName.replace(
      "/",
      "",
    );
    const devInstallDir = BUILD_CHANNELS.dev.packageName.replace("/", "");
    expect(stableInstallDir).toBe("@multicadesktop");
    expect(devInstallDir).toBe("@multicadev-desktop");
    expect(stableInstallDir.startsWith(devInstallDir)).toBe(false);
    expect(devInstallDir.startsWith(stableInstallDir)).toBe(false);
  });
});
