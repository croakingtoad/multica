// @vitest-environment jsdom

import type { ReactNode } from "react";
import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { AgentRuntime, RuntimeHookFire } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";

const TEST_RESOURCES = { en: { common: enCommon, runtimes: enRuntimes } };

// Mutable feed holder so each test drives the component with its own rows
// without re-mocking the query layer.
const feed = vi.hoisted(() => ({
  data: null as {
    fires: RuntimeHookFire[];
    limit: number;
    truncated: boolean;
  } | null,
  isLoading: false,
  isError: false,
}));

vi.mock("@multica/core/runtimes/queries", () => ({
  runtimeHookFiresOptions: (runtimeId: string, limit: number) => ({
    runtimeId,
    limit,
  }),
}));

vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  return {
    ...actual,
    useQuery: () => ({
      data: feed.data,
      isLoading: feed.isLoading,
      isError: feed.isError,
    }),
  };
});

import { HookFiresSection } from "./hook-fires-section";

const RUNTIME: AgentRuntime = {
  id: "r-1",
  workspace_id: "ws-1",
  daemon_id: null,
  name: "test-runtime",
  runtime_mode: "local",
  provider: "claude",
  launch_header: "",
  status: "online",
  device_info: "",
  metadata: {},
  owner_id: null,
  visibility: "private",
  last_seen_at: null,
  created_at: "2026-09-01T00:00:00Z",
  updated_at: "2026-09-01T00:00:00Z",
};

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function fire(overrides: Partial<RuntimeHookFire> = {}): RuntimeHookFire {
  return {
    id: "f-1",
    provider: "claude",
    event: "PreToolUse",
    execution_id: "exec-1",
    hook_spec: { hook_name: "my-hook", type: "claude_hook_response" },
    fired_at: new Date().toISOString(),
    provenance: "inferred",
    outcome: "success",
    detail: { debug_outcome: "success", exit_code: 0 },
    ...overrides,
  };
}

function renderFeed(
  fires: RuntimeHookFire[],
  opts: { limit?: number; truncated?: boolean } = {},
) {
  feed.data = {
    fires,
    limit: opts.limit ?? 100,
    truncated: opts.truncated ?? false,
  };
  feed.isLoading = false;
  feed.isError = false;
  return render(<HookFiresSection runtime={RUNTIME} />, { wrapper: Wrapper });
}

// ---------------------------------------------------------------------------
// DP-LOCO-114-03 condition 2: the rendered provenance equals the STORED
// column, for both values. No derivation, no upgrade, no render-time "this
// looks like a debug_log match".
//
// Each row publishes its provenance on `data-provenance`, so these assertions
// compare what is on screen against the exact string the fixture stored.
// A mutation check accompanies them — see the comment on the last test in
// this block.
// ---------------------------------------------------------------------------
describe("HookFiresSection — provenance is rendered verbatim", () => {
  it.each(["debug_log", "inferred"])(
    "renders the stored provenance %s unchanged",
    (stored) => {
      renderFeed([fire({ provenance: stored })]);

      const chips = document.querySelectorAll("[data-provenance]");
      expect(chips).toHaveLength(1);
      expect(chips[0]!.getAttribute("data-provenance")).toBe(stored);
    },
  );

  it("keeps each row on its own stored provenance in a mixed feed", () => {
    // The measured reality: a Claude feed is mixed, 70.1% inferred. A screen
    // that resolves provenance once for the feed would collapse this.
    renderFeed([
      fire({ id: "a", provenance: "inferred" }),
      fire({ id: "b", provenance: "debug_log" }),
      fire({ id: "c", provenance: "inferred" }),
    ]);

    const rendered = [...document.querySelectorAll("[data-provenance]")].map(
      (node) => node.getAttribute("data-provenance"),
    );
    expect(rendered).toEqual(["inferred", "debug_log", "inferred"]);
  });

  it("does not upgrade an inferred row that carries a debug-log-shaped detail", () => {
    // The round-3 defect in shape: a row whose detail looks like a matched
    // debug record, but whose stored provenance says inferred. The screen
    // must say inferred. If a render-time decision is ever introduced, this
    // is the fixture that catches it.
    renderFeed([
      fire({
        provenance: "inferred",
        detail: {
          debug_outcome: "success",
          exit_code: 0,
          message: "hook completed",
        },
      }),
    ]);

    // Scope to the row: "Receipt-timed" and "Host-timed" both also appear in
    // the always-visible provenance key above the rows, which is the point of
    // that block. What matters here is which label the ROW carries.
    const row = screen.getByTestId("hook-fire-row");
    const chip = row.querySelector("[data-provenance]");
    expect(chip?.getAttribute("data-provenance")).toBe("inferred");
    expect(chip?.textContent).toContain("Receipt-timed");
    expect(chip?.textContent).not.toContain("Host-timed");
  });

  it("makes no strength claim about an unrecognised provenance value", () => {
    // Not narrowed to `inferred`: asserting the weaker of two known claims
    // about a value we do not understand is still an unfounded assertion.
    renderFeed([fire({ provenance: "observed" })]);

    const row = screen.getByTestId("hook-fire-row");
    const chip = row.querySelector("[data-provenance]");
    expect(chip?.getAttribute("data-provenance")).toBe("observed");
    expect(chip?.textContent).toContain("Unrecognized");
    expect(chip?.textContent).not.toContain("Receipt-timed");
    expect(chip?.textContent).not.toContain("Host-timed");

    // And the key gains a definition that states Multica makes no claim.
    expect(
      screen.getByText(/makes no claim about what the time shown means/i),
    ).toBeTruthy();
  });
});

// ---------------------------------------------------------------------------
// The provenance limits have to be ON SCREEN, not in a footnote or a tooltip.
// ---------------------------------------------------------------------------
describe("HookFiresSection — provenance limits stated on screen", () => {
  it("explains what inferred means in visible body copy", () => {
    renderFeed([fire({ provenance: "inferred" })]);

    // Not a tooltip, not a title attribute: real rendered text.
    expect(
      screen.getByText(/when Multica received the record/i),
    ).toBeTruthy();
    expect(
      screen.getByText(/materially weaker claim than host-timed/i),
    ).toBeTruthy();
  });

  it("explains what debug_log means in visible body copy", () => {
    renderFeed([fire({ provenance: "debug_log" })]);

    expect(
      screen.getByText(/host's own debug log recorded this execution/i),
    ).toBeTruthy();
  });

  it("states that provenance is per row rather than per provider", () => {
    renderFeed([fire()]);

    expect(
      screen.getByText(/stated per row, never per provider/i),
    ).toBeTruthy();
  });

  it("never labels the feed as observed", () => {
    renderFeed([
      fire({ id: "a", provenance: "debug_log" }),
      fire({ id: "b", provenance: "debug_log" }),
    ]);

    // Even an all-debug_log page must not acquire a feed-level "observed"
    // claim — the next page could be mixed.
    expect(screen.queryByText(/\bobserved\b/i)).toBeNull();
  });

  it("quantifies the receipt-timed share of the loaded page", () => {
    renderFeed([
      fire({ id: "a", provenance: "inferred" }),
      fire({ id: "b", provenance: "inferred" }),
      fire({ id: "c", provenance: "debug_log" }),
    ]);

    expect(
      screen.getByText(
        /2 of 3 rows carry Multica's receipt time rather than a time the host recorded/i,
      ),
    ).toBeTruthy();
  });
});

// ---------------------------------------------------------------------------
// The `unknown` outcome: two causes, neither self-explanatory, 66.7% of all
// non-success outcomes in the measured corpus.
// ---------------------------------------------------------------------------
describe("HookFiresSection — the unknown outcome is explained", () => {
  it.each([126, 127])("explains exit %i as indistinguishable", (code) => {
    renderFeed([
      fire({
        outcome: "unknown",
        detail: { debug_outcome: "error", exit_code: code },
      }),
    ]);

    expect(
      screen.getByText(
        new RegExp(`cannot be told apart from a shell that never started it`, "i"),
      ),
    ).toBeTruthy();
    expect(screen.getByText(new RegExp(`Exit ${code}:`))).toBeTruthy();
  });

  it("explains a timed-out (cancelled) fire as a provider-reported state", () => {
    renderFeed([
      fire({ outcome: "unknown", detail: { debug_outcome: "cancelled" } }),
    ]);

    expect(screen.getByText(/reported this hook as "cancelled"/i)).toBeTruthy();
  });

  it("leaves a successful row unexplained", () => {
    renderFeed([fire({ outcome: "success" })]);

    expect(screen.queryByText(/never started it/i)).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// The stored `outcome` column decides, and `detail` only annotates.
//
// This is the outcome mirror of the provenance trap above, and it was the gap
// the Tier 1 audit found: every outcome fixture in this file had its `detail`
// AGREEING with its stored `outcome`, so a render layer that re-derived the
// outcome from `detail` passed the whole suite. Only the server side pinned
// this (TestListHookFires_RendersStoredOutcomeVerbatim).
//
// The fixtures below make the two disagree on purpose. A row whose detail
// reads exactly like the capture path's `unknown` input — provider said
// `error`, exit 127 — but whose stored outcome says `failure` must render
// Failure, because the capture path already weighed that evidence once and
// wrote its verdict down. Re-deciding it here would put a second, divergent
// classifier in a layer nobody is watching, which is the same defect class as
// render-time provenance.
// ---------------------------------------------------------------------------
describe("HookFiresSection — the stored outcome is rendered verbatim", () => {
  it("renders a stored failure whose detail looks like the unknown case", () => {
    renderFeed([
      fire({
        outcome: "failure",
        detail: { debug_outcome: "error", exit_code: 127 },
      }),
    ]);

    const row = screen.getByTestId("hook-fire-row");
    const badge = row.querySelector("[data-outcome]");
    expect(badge?.getAttribute("data-outcome")).toBe("failure");
    expect(badge?.textContent).toContain("Failure");
    expect(badge?.textContent).not.toContain("Unknown");

    // The unknown-cause note is gated on the STORED outcome, so a failure row
    // must not acquire the 126/127 spawn-ambiguity explanation — that copy
    // says the result "cannot be told apart" from a hook that never started,
    // which contradicts a row that states it failed.
    expect(screen.queryByText(/cannot be told apart/i)).toBeNull();
    expect(screen.queryByText(/Exit 127:/)).toBeNull();
    // The bare exit code still shows; it is data, not a classification.
    expect(screen.getByText("Exit 127")).toBeTruthy();
  });

  it.each(["success", "blocked", "skipped"])(
    "renders a stored %s whose detail carries an error and exit 126",
    (stored) => {
      renderFeed([
        fire({
          outcome: stored,
          detail: { debug_outcome: "error", exit_code: 126 },
        }),
      ]);

      const badge = screen
        .getByTestId("hook-fire-row")
        .querySelector("[data-outcome]");
      expect(badge?.getAttribute("data-outcome")).toBe(stored);
      expect(badge?.textContent).not.toContain("Unknown");
      expect(screen.queryByText(/cannot be told apart/i)).toBeNull();
    },
  );

  it("does not claim spawn ambiguity for a stored unknown with an ordinary exit code", () => {
    // The inverse fixture. Stored `unknown` does get a cause note, but the
    // note must come from this row's own detail: exit 3 is an ordinary
    // non-zero exit, not one of the two codes Claude cannot disambiguate. The
    // spawn-ambiguity sentence here would be an invented claim.
    renderFeed([
      fire({
        outcome: "unknown",
        detail: { debug_outcome: "error", exit_code: 3 },
      }),
    ]);

    const badge = screen
      .getByTestId("hook-fire-row")
      .querySelector("[data-outcome]");
    expect(badge?.getAttribute("data-outcome")).toBe("unknown");
    expect(badge?.textContent).toContain("Unknown");

    expect(
      screen.getByText(/did not carry enough to classify this result/i),
    ).toBeTruthy();
    expect(screen.queryByText(/cannot be told apart/i)).toBeNull();
  });

  it("keeps each row on its own stored outcome in a mixed feed", () => {
    // Per row, like provenance. All three carry the same `unknown`-shaped
    // detail; only the stored column differs, so a feed-level or
    // detail-driven decision would flatten them to one label.
    renderFeed([
      fire({
        id: "a",
        outcome: "failure",
        detail: { debug_outcome: "error", exit_code: 127 },
      }),
      fire({
        id: "b",
        outcome: "unknown",
        detail: { debug_outcome: "error", exit_code: 127 },
      }),
      fire({
        id: "c",
        outcome: "success",
        detail: { debug_outcome: "error", exit_code: 127 },
      }),
    ]);

    const rendered = [...document.querySelectorAll("[data-outcome]")].map(
      (node) => node.getAttribute("data-outcome"),
    );
    expect(rendered).toEqual(["failure", "unknown", "success"]);

    // And exactly one row — the stored `unknown` — carries a cause note.
    expect(screen.getAllByText(/cannot be told apart/i)).toHaveLength(1);
  });

  it("makes no outcome claim about an unrecognised stored value", () => {
    renderFeed([fire({ outcome: "errored" })]);

    const badge = screen
      .getByTestId("hook-fire-row")
      .querySelector("[data-outcome]");
    expect(badge?.getAttribute("data-outcome")).toBe("errored");
    expect(badge?.textContent).toContain("Unrecognized");
    expect(badge?.textContent).not.toContain("Failure");
  });
});

// ---------------------------------------------------------------------------
// `skipped` is unreachable on Claude. Nothing may offer the reader a
// "never ran" state the data cannot supply.
// ---------------------------------------------------------------------------
describe("HookFiresSection — no unreachable states advertised", () => {
  it("does not enumerate possible outcomes anywhere", () => {
    renderFeed([
      fire({ id: "a", outcome: "success" }),
      fire({ id: "b", outcome: "failure" }),
      fire({ id: "c", outcome: "unknown", detail: { debug_outcome: "error" } }),
    ]);

    // No legend: "Skipped" and "Blocked" appear only if a row stores them.
    expect(screen.queryByText("Skipped")).toBeNull();
    expect(screen.queryByText("Blocked")).toBeNull();
    expect(screen.getByText("Success")).toBeTruthy();
    expect(screen.getByText("Failure")).toBeTruthy();
    expect(screen.getByText("Unknown")).toBeTruthy();
  });

  it("renders skipped only when a row actually stores it", () => {
    renderFeed([fire({ outcome: "skipped" })]);

    expect(screen.getByText("Skipped")).toBeTruthy();
  });
});

// ---------------------------------------------------------------------------
// execution_id is a per-execution reference, never configured-hook identity.
// ---------------------------------------------------------------------------
describe("HookFiresSection — execution_id is not presented as hook identity", () => {
  it("labels the execution reference as per-execution", () => {
    renderFeed([fire({ execution_id: "exec-abc" })]);

    expect(screen.getByText("exec-abc")).toBeTruthy();
    // The caveat is stated once, in the key block, rather than repeated on
    // every row where it would bury the provenance copy beside it.
    expect(
      screen.getByText(/fresh on every fire\. Not an identity for a configured hook/i),
    ).toBeTruthy();
  });

  it("renders one row per fire even when execution ids repeat", () => {
    // No dedupe, no grouping, no DISTINCT on execution_id — the feed is a
    // flat projection of stored rows.
    renderFeed([
      fire({ id: "a", execution_id: "same" }),
      fire({ id: "b", execution_id: "same" }),
    ]);

    expect(screen.getAllByTestId("hook-fire-row")).toHaveLength(2);
  });

  it("says the handler was not reported rather than showing the execution ref", () => {
    renderFeed([fire({ hook_spec: {}, execution_id: "exec-xyz" })]);

    expect(screen.getByText("Handler not reported")).toBeTruthy();
  });
});

describe("HookFiresSection — feed completeness and states", () => {
  it("states its own truncation rather than implying completeness", () => {
    renderFeed([fire()], { limit: 100, truncated: true });

    expect(
      screen.getByText(/Showing the 100 most recent fires/i),
    ).toBeTruthy();
  });

  it("says an empty feed means nothing was reported, not that nothing ran", () => {
    renderFeed([]);

    // The load-bearing clause: an empty feed is never evidence that no hook
    // ran. It survives the retention widening below unchanged.
    expect(screen.getByText(/not that no hook ran/i)).toBeTruthy();
  });

  it("allows retention as a reason an empty feed is empty", () => {
    // Stage 7's tip prunes fires older than 30 days (PruneHookFireHistory,
    // 3ab82108d). Without this clause the empty state asserts that an empty
    // feed means nothing was reported, which is false for a runtime whose
    // fires have simply aged out — a screen that exists to avoid overclaiming
    // would be making the wrong claim itself.
    renderFeed([]);

    expect(
      screen.getByText(/passed the 30-day retention window/i),
    ).toBeTruthy();
  });

  it("renders an error state without inventing rows", () => {
    feed.data = null;
    feed.isLoading = false;
    feed.isError = true;
    render(<HookFiresSection runtime={RUNTIME} />, { wrapper: Wrapper });

    expect(screen.getByText("Could not load hook fires.")).toBeTruthy();
    expect(screen.queryAllByTestId("hook-fire-row")).toHaveLength(0);
  });
});
