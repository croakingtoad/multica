import { join } from "node:path";
import type { DaemonPrefs } from "../shared/daemon-types";

interface DaemonPrefsChannelConfig {
  filename: string;
  autoStart: boolean;
}

export function daemonPrefsConfig(
  homeDirectory: string,
  channel: DaemonPrefsChannelConfig,
): { path: string; defaults: DaemonPrefs } {
  return {
    path: join(homeDirectory, ".multica", channel.filename),
    defaults: { autoStart: channel.autoStart, autoStop: false },
  };
}
