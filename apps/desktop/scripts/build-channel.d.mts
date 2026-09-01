export type BuildChannelName = "stable" | "dev";
export type SharedStateCompatibility = "stable" | "breaking";

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

export const BUILD_CHANNELS: Readonly<Record<BuildChannelName, BuildChannelConfig>>;
export function resolveBuildChannel(
  env?: Readonly<Record<string, string | undefined>>,
): BuildChannelConfig;
export function requireSharedStateCompatibility(
  env?: Readonly<Record<string, string | undefined>>,
): SharedStateCompatibility;
export function buildChannelDefines(
  channel: BuildChannelConfig,
  sharedStateCompat: SharedStateCompatibility,
): Record<string, string>;
export function builderConfigForChannel(
  channel: BuildChannelConfig,
  sharedStateCompat: SharedStateCompatibility,
): Record<string, unknown>;
