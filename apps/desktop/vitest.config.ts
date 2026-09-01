import { resolve } from "path";
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import {
  BUILD_CHANNELS,
  buildChannelDefines,
} from "./scripts/build-channel.mjs";

export default defineConfig({
  define: buildChannelDefines(BUILD_CHANNELS.stable, "stable"),
  plugins: [react()],
  resolve: {
    alias: {
      "@": resolve(__dirname, "src/renderer/src"),
    },
  },
  test: {
    globals: true,
    include: ["src/**/*.test.{ts,tsx}", "scripts/**/*.test.mjs"],
    environment: "jsdom",
    setupFiles: ["./test/setup.ts"],
    passWithNoTests: true,
  },
});
