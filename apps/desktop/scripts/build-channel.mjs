const stable = Object.freeze({
  name: "stable",
  packageName: "@multica/desktop",
  productName: "Multica",
  appId: "ai.multica.desktop",
  executableName: "multica-desktop",
  startupWmClass: "Multica",
  protocolName: "Multica",
  protocolScheme: "multica",
  titlePrefix: "",
  titleFallback: "",
  runtimeIcon: "resources/icon.png",
  macIcon: "build/icon.icns",
  windowsIcon: "build/icon.ico",
  linuxIcon: "build/icons",
  daemonProfileSuffix: "",
  daemonPrefsFilename: "desktop_prefs.json",
  runtimeConfigFilename: "desktop.json",
  daemonAutoStart: true,
  updatesEnabled: true,
});

const dev = Object.freeze({
  name: "dev",
  // A one-click NSIS install directory comes from package name, not
  // productName. Keep this name distinct without making either channel's
  // sanitized directory a prefix of the other: electron-builder's generated
  // process check uses Path.StartsWith($INSTDIR) before replacing files.
  packageName: "@multica/dev-desktop",
  productName: "Multica Dev",
  appId: "ai.multica.desktop.dev",
  executableName: "multica-desktop-dev",
  startupWmClass: "Multica Dev",
  protocolName: "Multica Dev",
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

export const BUILD_CHANNELS = Object.freeze({ stable, dev });

export function resolveBuildChannel(env = process.env) {
  const requested = env.MULTICA_CHANNEL ?? "stable";
  if (requested !== "stable" && requested !== "dev") {
    throw new Error(
      `[package] MULTICA_CHANNEL must be "stable" or "dev" (received ${JSON.stringify(requested)})`,
    );
  }
  return BUILD_CHANNELS[requested];
}

export function requireSharedStateCompatibility(env = process.env) {
  const requested = env.MULTICA_SHARED_STATE_COMPAT;
  if (requested !== "stable" && requested !== "breaking") {
    throw new Error(
      `[package] MULTICA_SHARED_STATE_COMPAT must be "stable" or "breaking" (received ${JSON.stringify(requested)})`,
    );
  }
  return requested;
}

export function buildChannelDefines(channel, sharedStateCompat) {
  return {
    __MULTICA_BUILD_CHANNEL__: JSON.stringify(channel.name),
    __MULTICA_BUILD_CHANNEL_CONFIG__: JSON.stringify(channel),
    __MULTICA_SHARED_STATE_COMPAT__: JSON.stringify(sharedStateCompat),
  };
}

export function builderConfigForChannel(channel, sharedStateCompat) {
  const platformArtifactNames = {
    stable: {
      linux: "multica-desktop-${version}-linux-${arch}.${ext}",
      mac: "multica-desktop-${version}-mac-${arch}.${ext}",
      win: "multica-desktop-${version}-windows-${arch}.${ext}",
    },
    dev: {
      linux: "multica-desktop-dev-${version}-linux-${arch}.${ext}",
      mac: "multica-desktop-dev-${version}-mac-${arch}.${ext}",
      win: "multica-desktop-dev-${version}-windows-${arch}.${ext}",
    },
  };
  return {
    extends: "./electron-builder.yml",
    appId: channel.appId,
    productName: channel.productName,
    extraMetadata: {
      // electron-builder derives one-click NSIS's APP_FILENAME (and therefore
      // $INSTDIR) from package name. This must be channel-specific even though
      // productName already gives the payload executable a different name.
      name: channel.packageName,
      productName: channel.productName,
      multicaChannel: channel.name,
      multicaSharedStateCompat: sharedStateCompat,
    },
    protocols: {
      name: channel.protocolName,
      schemes: [channel.protocolScheme],
    },
    // Keep Windows installer identity explicit. electron-builder currently
    // derives these values from productName, but pinning them here makes a
    // future default change fail the channel contract instead of silently
    // replacing stable's shortcuts or Add/Remove Programs entry.
    nsis: {
      oneClick: true,
      perMachine: false,
      createDesktopShortcut: true,
      createStartMenuShortcut: true,
      shortcutName: channel.productName,
      uninstallDisplayName: `${channel.productName} \${version}`,
    },
    mac: {
      icon: channel.macIcon,
      artifactName: platformArtifactNames[channel.name].mac,
    },
    win: {
      icon: channel.windowsIcon,
      artifactName: platformArtifactNames[channel.name].win,
    },
    linux: {
      icon: channel.linuxIcon,
      executableName: channel.executableName,
      artifactName: platformArtifactNames[channel.name].linux,
      desktop: { entry: { StartupWMClass: channel.startupWmClass } },
    },
  };
}
