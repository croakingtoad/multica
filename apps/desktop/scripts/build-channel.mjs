const stable = Object.freeze({
  name: "stable",
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
  daemonAutoStart: true,
  updatesEnabled: true,
});

const dev = Object.freeze({
  name: "dev",
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

export function buildChannelDefines(channel) {
  return {
    __MULTICA_BUILD_CHANNEL__: JSON.stringify(channel.name),
    __MULTICA_BUILD_CHANNEL_CONFIG__: JSON.stringify(channel),
  };
}

export function builderConfigForChannel(channel) {
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
      productName: channel.productName,
      multicaChannel: channel.name,
    },
    protocols: {
      name: channel.protocolName,
      schemes: [channel.protocolScheme],
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
