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
// So assert usage, at the only place that can notice: every caption the hooks
// bundle declares must be named by the components that render it. A translated
// string nothing reads is a string four translators maintained for nothing,
// and its presence implies a surface that no longer exists.
//
// LOCO-548 widened the scope from `hooks.answer.*` to the whole `hooks.*`
// tree. The original narrow scope existed because the other subtrees still
// carried dead keys of their own (`scopes.found_hint`,
// `configuration.live_hint`, `effectiveness.will_run_hint`, `trust.label`,
// `trust.not_applicable`, `detail.identity`) and sweeping them in silently was
// not that issue's call. LOCO-548 deleted those six, so the reason for the
// narrow scope expired with them — and the same class had by then been
// rediscovered three times independently, which is what an unguarded subtree
// buys you.
//
// The ruling that came with the widening: a caption that is dead *by design*
// gets deleted, not allowlisted. `hooks.trust.not_applicable` was the test
// case — `hook-state-badges.tsx` deliberately renders no trust cell for Claude
// rows, so the string had no render path and never would. There is no escape
// hatch here on purpose; if a future caption is genuinely unreachable, remove
// it rather than teaching this guard to ignore it.

const HOOKS_DIR = dirname(fileURLToPath(import.meta.url));

// Readers are not confined to the hooks directory: `runtime-detail.tsx`, one
// level up, owns the tab strip and reads `hooks.tab_*`. Scanning only
// `HOOKS_DIR` would report those two live captions as dead, so the reader set
// is the whole runtimes feature.
const RUNTIMES_DIR = resolve(HOOKS_DIR, "..", "..");

const LOCALES = { en, ja, ko, "zh-Hans": zhHans } as const;

/** Every non-test source in the runtimes feature — the readers of these captions. */
function runtimeSources(): string {
  const files: string[] = [];
  const walk = (dir: string): void => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const full = resolve(dir, entry.name);
      if (entry.isDirectory()) walk(full);
      else if (/\.tsx?$/.test(entry.name) && !/\.test\.tsx?$/.test(entry.name)) {
        files.push(full);
      }
    }
  };
  walk(RUNTIMES_DIR);
  return files.map((file) => readFileSync(file, "utf8")).join("\n");
}

/** Dotted leaf paths of a bundle subtree, e.g. `scopes.title`. */
function leafPaths(value: unknown, prefix = ""): string[] {
  if (value === null || typeof value !== "object") return [prefix];
  return Object.entries(value as Record<string, unknown>).flatMap(([key, child]) =>
    leafPaths(child, prefix ? `${prefix}.${key}` : key),
  );
}

/**
 * i18next resolves `key_one` / `key_other` from a `key` the caller writes, so
 * a plural key is referenced under its base name.
 */
function baseKey(key: string): string {
  return key.replace(/_(zero|one|two|few|many|other)$/, "");
}

function escapeRegex(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

describe("hooks captions", () => {
  const sources = runtimeSources();

  it("reads a caption from the hooks directory, so an empty read cannot pass", () => {
    expect(sources).toContain("$.hooks.answer.entry_handler");
  });

  it("reads a caption from outside the hooks directory, so a narrow walk cannot pass", () => {
    expect(sources).toContain("$.hooks.tab_overview");
  });

  for (const [locale, bundle] of Object.entries(LOCALES)) {
    it(`${locale}: declares no hooks caption the components never read`, () => {
      const declared = [...new Set(leafPaths(bundle.hooks).map(baseKey))];
      expect(declared.length).toBeGreaterThan(0);
      const unread = declared.filter(
        (key) =>
          !new RegExp(`${escapeRegex(`$.hooks.${key}`)}(?![A-Za-z0-9_])`).test(sources),
      );
      expect(unread).toEqual([]);
    });
  }
});
