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

    expect(screen.getByText(/nothing was reported — not that no hook ran/i)).toBeTruthy();
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
