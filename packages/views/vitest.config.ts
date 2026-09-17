import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  test: {
    // Keep jsdom on a tuple origin: test/setup.ts reads Web Storage from a
    // same-origin frame, and an opaque URL such as about:blank throws here.
    environment: "jsdom",
    env: { TZ: "UTC" },
    globals: true,
    setupFiles: ["./test/setup.ts"],
    include: ["**/*.test.{ts,tsx}"],
  },
});
