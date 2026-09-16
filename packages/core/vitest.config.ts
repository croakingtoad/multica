import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    globals: true,
    // Suites needing Web Storage opt into jsdom per file with
    // `// @vitest-environment jsdom`. Keep any jsdom URL on a tuple origin:
    // test/setup.ts reads Web Storage from a same-origin frame, and an opaque
    // URL such as about:blank throws here.
    setupFiles: ["./test/setup.ts"],
    include: ["**/*.test.{ts,tsx}"],
    passWithNoTests: true,
  },
});
