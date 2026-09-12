// @vitest-environment node
import { readdirSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import en from "../../../locales/en/runtimes.json";
import ja from "../../../locales/ja/runtimes.json";
import ko from "../../../locales/ko/runtimes.json";
import zhHans from "../../../locales/zh-Hans/runtimes.json";

// LOCO-533 R1. `hooks.answer.entry_matcher` outlived its only call site: the
// answer entry caption became `entry_handler`, and the key stayed in all four
// bundles. locales/parity.test.ts could not see it, because parity is a
// cross-locale question and this was a usage question — a key present in
// every bundle and referenced by none is perfectly in parity.
//
// So assert usage, at the only place that can notice: every caption the answer
// panel's bundle declares must be named by the components that render it. A
// translated string nothing reads is a string four translators maintained for
// nothing, and its presence implies a surface that no longer exists.
//
// Scoped to `hooks.answer.*` — the answer panel's own captions, which is the
// surface this issue is about. Other `hooks.*` subtrees carry their own dead
// keys; they are not this issue's and are not silently swept in here.

const HOOKS_DIR = dirname(fileURLToPath(import.meta.url));
const LOCALES = { en, ja, ko, "zh-Hans": zhHans } as const;

/** The hooks components, which are the only readers of these captions. */
function hookSources(): string {
  return readdirSync(HOOKS_DIR)
    .filter((name) => /\.tsx?$/.test(name) && !/\.test\.tsx?$/.test(name))
    .map((name) => readFileSync(resolve(HOOKS_DIR, name), "utf8"))
    .join("\n");
}

/**
 * i18next resolves `key_one` / `key_other` from a `key` the caller writes, so
 * a plural key is referenced under its base name.
 */
function baseKey(key: string): string {
  return key.replace(/_(zero|one|two|few|many|other)$/, "");
}

describe("hooks answer captions", () => {
  const sources = hookSources();

  it("reads at least one caption, so an empty read cannot pass this suite", () => {
    expect(sources).toContain("$.hooks.answer.entry_handler");
  });

  for (const [locale, bundle] of Object.entries(LOCALES)) {
    it(`${locale}: declares no answer caption the components never read`, () => {
      const declared = Object.keys(bundle.hooks.answer);
      expect(declared.length).toBeGreaterThan(0);
      const unread = [
        ...new Set(declared.map(baseKey)),
      ].filter((key) => !sources.includes(`$.hooks.answer.${key}`));
      expect(unread).toEqual([]);
    });
  }
});
