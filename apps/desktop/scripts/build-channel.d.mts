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

export const BUILD_CHANNELS: Readonly<Record<BuildChannelName, BuildChannelConfig>>;
export function resolveBuildChannel(
  env?: Readonly<Record<string, string | undefined>>,
): BuildChannelConfig;
export function buildChannelDefines(
  channel: BuildChannelConfig,
): Record<string, string>;
export function builderConfigForChannel(
  channel: BuildChannelConfig,
): Record<string, unknown>;
