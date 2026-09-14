import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    globals: true,
    include: ["**/*.test.{ts,tsx}"],
    passWithNoTests: true,
    // Vitest 4 injects --experimental-import-meta-resolve into test workers.
    // On Node >= 25, that option also enables an experimental Web Storage
    // global whose localStorage methods are undefined unless a storage file is
    // provided, shadowing jsdom's browser localStorage. Disable it here so
    // jsdom remains the test browser environment on supported Node versions.
    execArgv: ["--no-experimental-webstorage"],
  },
});
