#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { envWithLocalBins } from "./package.mjs";

const env = {
  ...process.env,
  MULTICA_SHARED_STATE_COMPAT:
    process.env.MULTICA_SHARED_STATE_COMPAT ?? "stable",
};
const result = spawnSync("electron-vite", process.argv.slice(2), {
  stdio: "inherit",
  env: envWithLocalBins(env),
  shell: process.platform === "win32",
});

if (result.error) {
  console.error(`[desktop] failed to run electron-vite: ${result.error.message}`);
  process.exit(1);
}
process.exit(result.status ?? 1);
