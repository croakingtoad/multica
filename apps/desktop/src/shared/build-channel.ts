export type BuildChannelName = "stable" | "dev";

export interface BuildChannelConfig {
  readonly name: BuildChannelName;
  readonly productName: string;
  readonly appId: string;
  readonly executableName: string;
  readonly startupWmClass: string;
  readonly protocolName: string;
  readonly protocolScheme: string;
  readonly titlePrefix: string;
  readonly titleFallback: string;
  readonly runtimeIcon: string;
  readonly macIcon: string;
  readonly windowsIcon: string;
  readonly linuxIcon: string;
  readonly daemonProfileSuffix: string;
  readonly daemonPrefsFilename: string;
  readonly daemonAutoStart: boolean;
  readonly updatesEnabled: boolean;
}

declare const __MULTICA_BUILD_CHANNEL__: BuildChannelName;
declare const __MULTICA_BUILD_CHANNEL_CONFIG__: BuildChannelConfig;

// electron-vite replaces both identifiers with JSON literals. Runtime code
// deliberately never reads MULTICA_CHANNEL, so a packaged stable binary cannot
// be switched into dev behavior by changing its launch environment.
export const BUILD_CHANNEL = __MULTICA_BUILD_CHANNEL__;
export const BUILD_CHANNEL_CONFIG = __MULTICA_BUILD_CHANNEL_CONFIG__;

export function decorateWindowTitle(
  title: string,
  prefix: string,
  fallback: string,
): string {
  const resolvedTitle = title || fallback;
  return resolvedTitle ? `${prefix}${resolvedTitle}` : resolvedTitle;
}

export function packagedUserDataPath(
  appDataPath: string,
  productName: string,
): string {
  const separator = appDataPath.includes("\\") && !appDataPath.includes("/")
    ? "\\"
    : "/";
  return `${appDataPath.replace(/[\\/]+$/, "")}${separator}${productName}`;
}
