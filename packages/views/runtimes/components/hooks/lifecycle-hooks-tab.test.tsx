// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AgentRuntime, RuntimeHookReadRequest } from "@multica/core/types";
import enCommon from "../../../locales/en/common.json";
import enRuntimes from "../../../locales/en/runtimes.json";

// Canonical matrix for the derivations under test lives in hooks-model.test.ts.
// This suite keeps the wiring and the named honesty regressions: what the tab
// is allowed to show for each observation state, and what it must never claim.

const hookQuery = vi.fn();

vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  return {
    ...actual,
    useQuery: () => hookQuery(),
    useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  };
});
vi.mock("@multica/core/runtimes", () => ({
  runtimeHooksKeys: { forRuntime: (id: string) => ["runtimes", "hooks", id] },
  runtimeHooksOptions: (id: string | null) => ({ queryKey: ["runtimes", "hooks", id] }),
}));

import { LifecycleHooksTab } from "./lifecycle-hooks-tab";

function runtime(provider = "claude"): AgentRuntime {
  return {
    id: "rt-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "claude (daemon-1)",
    runtime_mode: "local",
    provider,
    launch_header: provider,
    status: "online",
    device_info: "host.local",
    metadata: {},
    owner_id: "user-1",
    visibility: "private",
    profile_id: null,
    last_seen_at: "2026-09-12T11:00:00Z",
    created_at: "2026-09-12T10:00:00Z",
    updated_at: "2026-09-12T11:00:00Z",
  };
}

function mount(provider = "claude") {
  return render(
    <I18nProvider
      locale="en"
      resources={{ en: { common: enCommon, runtimes: enRuntimes } }}
    >
      <LifecycleHooksTab runtime={runtime(provider)} />
    </I18nProvider>,
  );
}

function observation(
  overrides: Partial<RuntimeHookReadRequest> = {},
): RuntimeHookReadRequest {
  return {
    runtime_id: "rt-1",
    status: "completed",
    cached: false,
    observed_at: new Date().toISOString(),
    sources: [
      {
        provider: "claude",
        scope: "user",
        format: "json",
        state: "found",
        source_path: "/home/u/.claude/settings.json",
        content_hash: "a".repeat(64),
      },
      {
        provider: "claude",
        scope: "project",
        format: "json",
        state: "not_checked",
        source_path: null,
        content_hash: null,
      },
      {
        provider: "claude",
        scope: "local",
        format: "json",
        state: "not_checked",
        source_path: null,
        content_hash: null,
      },
    ],
    resolved: {
      provider: "claude",
      entries: [
        {
          hook_id: "sha256:parked-and-ineligible",
          event: "Notification",
          matcher: "",
          matcher_kind: "all",
          handler: { type: "command", command: "notify.sh", if: "Bash(git *)" },
          handler_type: "command",
          sources: [{ scope: "user", format: "json", kind: "settings" }],
          configuration: "parked",
          parked_at: "2026-09-10T10:00:00Z",
          effectiveness: "never_runs",
          never_runs_reason: "matcher_ineligible",
          trust: "not_applicable",
        },
        {
          hook_id: "sha256:live",
          event: "PreToolUse",
          matcher: "Bash",
          matcher_kind: "exact",
          handler: { type: "command", command: "guard.sh" },
          handler_type: "command",
          sources: [{ scope: "user", format: "json", kind: "settings" }],
          configuration: "live",
          effectiveness: "will_run",
          trust: "not_applicable",
        },
      ],
    },
    ...overrides,
  };
}

beforeEach(() => {
  hookQuery.mockReset();
});

describe("LifecycleHooksTab", () => {
  it("lists an entry per event with its source and both state axes", () => {
    hookQuery.mockReturnValue({
      data: observation(),
      error: null,
      isFetching: false,
    });

    mount();

    expect(screen.getByText("PreToolUse")).toBeInTheDocument();
    expect(screen.getByText("Notification")).toBeInTheDocument();
    // Source per entry.
    expect(screen.getAllByText("user/json").length).toBeGreaterThanOrEqual(2);
    // Configuration axis and provider-verdict axis, both rendered.
    expect(screen.getByText("Parked")).toBeInTheDocument();
    // "Will never run" and "Will run" are also summary-rail labels, so both
    // appear more than once by design.
    expect(screen.getAllByText("Will never run").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("Registered").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("Will run").length).toBeGreaterThanOrEqual(1);
  });

  // AC2: the parked + ineligible entry must show both, and the parked date.
  it("keeps a parked and ineligible entry on both axes and dates the parking", () => {
    hookQuery.mockReturnValue({
      data: observation(),
      error: null,
      isFetching: false,
    });

    mount();

    expect(screen.getByText("Parked")).toBeInTheDocument();
    expect(screen.getAllByText("Will never run").length).toBeGreaterThanOrEqual(1);
    expect(
      screen.getByText(/Parked at 2026-09-10T10:00:00Z/),
    ).toBeInTheDocument();
  });

  // AC5: no screen may claim shadowing, override or displacement. The guard
  // is on affirmative claims only — the tab does state the negation ("no
  // layer displaces another's entries"), which is the point.
  it("never claims a hook is shadowed, overridden or displaced", () => {
    hookQuery.mockReturnValue({
      data: observation(),
      error: null,
      isFetching: false,
    });

    const { container } = mount();
    const text = container.textContent ?? "";

    expect(text).not.toMatch(/shadow/i);
    expect(text).not.toMatch(/overrid|overrode/i);
    expect(text).not.toMatch(/takes precedence|precedence over/i);
    expect(text).not.toMatch(/\bwins\b/i);
    expect(text).not.toMatch(/replaced by|instead of the .* entry/i);
    // And the merge rule is stated, so the absence above is a claim made
    // rather than a subject avoided.
    expect(text).toMatch(/no layer displaces another/i);
  });

  // AC3: not-checked must never read as "no hooks", and it must be visibly
  // distinct from a source that was checked and found absent.
  it("renders a scope with no observation as not checked, not as empty", () => {
    hookQuery.mockReturnValue({
      data: observation(),
      error: null,
      isFetching: false,
    });

    mount();

    expect(screen.getAllByText("Not checked").length).toBe(2);
    // One explanation per not-checked scope, and never the word "empty" as a
    // description of what was found there.
    expect(screen.getAllByText(/Nobody looked here/).length).toBe(2);
    expect(
      screen.getByText(/<project>\/\.claude\/settings\.json/),
    ).toBeInTheDocument();
  });

  // AC6: project and local are read-only wherever they appear.
  it("marks project and local read-only and names the one writable scope", () => {
    hookQuery.mockReturnValue({
      data: observation(),
      error: null,
      isFetching: false,
    });

    mount();

    expect(screen.getAllByText("Read-only").length).toBeGreaterThanOrEqual(2);
    expect(
      screen.getByText(/only source on the daemon host/),
    ).toBeInTheDocument();
  });

  // AC4: the offline view is the snapshot plus its observed_at, and it reads
  // as last known rather than current.
  it("dates a cached snapshot and says it is last known", () => {
    hookQuery.mockReturnValue({
      data: observation({ cached: true, observed_at: "2026-09-12T09:00:00Z" }),
      error: null,
      isFetching: false,
    });

    mount();

    expect(screen.getByText(/Last known state/)).toBeInTheDocument();
    expect(
      screen.getByText(/Observed at 2026-09-12T09:00:00Z/),
    ).toBeInTheDocument();
    expect(screen.getByText(/not as they are now/)).toBeInTheDocument();
  });

  // AC4, second half: no prior observation is an explicit failure.
  it("shows an explicit failure when there is no stored observation", () => {
    hookQuery.mockReturnValue({
      data: {
        runtime_id: "rt-1",
        status: "failed",
        cached: false,
        error: "runtime is offline and has no last known hook observation",
      },
      error: null,
      isFetching: false,
    });

    mount();

    expect(screen.getByText(/did not complete/)).toBeInTheDocument();
    expect(
      screen.getByText(/no last known hook observation/),
    ).toBeInTheDocument();
    expect(screen.queryByText("PreToolUse")).not.toBeInTheDocument();
  });

  // The gcTime: 0 caveat — a value still in cache during a new discovery must
  // not be presented as this mount's observation.
  it("shows nothing from a prior observation while a read is in flight", () => {
    hookQuery.mockReturnValue({
      data: observation(),
      error: null,
      isFetching: true,
    });

    mount();

    expect(screen.getByText(/Reading hooks from the host/)).toBeInTheDocument();
    expect(screen.queryByText("PreToolUse")).not.toBeInTheDocument();
    expect(screen.queryByText(/Observed at/)).not.toBeInTheDocument();
  });

  // A matcher Multica cannot evaluate gets its own state and an explicit "the
  // hook may still run" — never a normal regex badge and never a verdict.
  it("surfaces an unevaluable matcher as Multica's limit", () => {
    const data = observation();
    data.resolved!.entries = [
      {
        hook_id: "sha256:lookahead",
        event: "PreToolUse",
        matcher: "^(?!Notebook).*",
        matcher_kind: "unevaluable",
        matcher_error: "error parsing regexp: invalid or unsupported Perl syntax",
        handler: { type: "command", command: "guard.sh" },
        handler_type: "command",
        sources: [{ scope: "user", format: "json", kind: "settings" }],
        configuration: "live",
        effectiveness: "will_run",
        trust: "not_applicable",
      },
    ];
    hookQuery.mockReturnValue({ data, error: null, isFetching: false });

    mount();

    expect(
      screen.getByText("Multica cannot evaluate this matcher"),
    ).toBeInTheDocument();
    // The verdict stays will_run: an unevaluable matcher is Multica's limit,
    // not a reason to claim the hook does not run. Asserted inside the table
    // because the summary rail carries a "Will never run" label at all times.
    const table = screen.getByRole("table");
    expect(table.textContent).toMatch(/Will run/);
    expect(table.textContent).not.toMatch(/Will never run/);
  });

  // Codex keeps hooks.json and inline config.toml as two sources in one scope,
  // both live. Both must appear, and neither may be shown as displacing the
  // other.
  it("shows both Codex user-scope sources as live", () => {
    hookQuery.mockReturnValue({
      data: {
        runtime_id: "rt-1",
        status: "completed",
        cached: false,
        observed_at: new Date().toISOString(),
        sources: [
          {
            provider: "codex",
            scope: "user",
            format: "json",
            state: "found",
            source_path: "/home/u/.codex/hooks.json",
            content_hash: "b".repeat(64),
          },
          {
            provider: "codex",
            scope: "user",
            format: "toml",
            state: "found",
            source_path: "/home/u/.codex/config.toml",
            content_hash: "c".repeat(64),
          },
        ],
        resolved: {
          provider: "codex",
          entries: [
            {
              hook_id: "sha256:json",
              event: "PreToolUse",
              matcher: "Bash",
              matcher_kind: "regex",
              handler: { type: "command", command: "json-side.sh" },
              handler_type: "command",
              sources: [{ scope: "user", format: "json", kind: "settings" }],
              configuration: "live",
              effectiveness: "trust_unknown",
              trust: "pending_review",
            },
            {
              hook_id: "sha256:toml",
              event: "PreToolUse",
              matcher: "apply_patch",
              matcher_kind: "regex",
              handler: { type: "command", command: "toml-side.sh" },
              handler_type: "command",
              sources: [{ scope: "user", format: "toml", kind: "settings" }],
              configuration: "live",
              effectiveness: "trust_unknown",
              trust: "pending_review",
            },
          ],
        },
      },
      error: null,
      isFetching: false,
    });

    const { container } = mount("codex");

    // Once in the sources card and once on the entry, for each of the two.
    expect(screen.getAllByText("user/json").length).toBe(2);
    expect(screen.getAllByText("user/toml").length).toBe(2);
    expect(screen.getAllByText("Registered").length).toBe(2);
    expect(screen.getAllByText("Trust unconfirmed").length).toBeGreaterThanOrEqual(2);
    expect(container.textContent).not.toMatch(/shadow|overrid/i);
  });

  // A resolution failure must not be flattened into "this runtime has no
  // hooks", which is the answer a reader would otherwise act on.
  it("reports a resolution failure rather than an empty hook list", () => {
    hookQuery.mockReturnValue({
      data: observation({
        resolved: {
          provider: "claude",
          entries: [],
          error: "duplicate parked hook id \"x\"",
        },
      }),
      error: null,
      isFetching: false,
    });

    mount();

    expect(
      screen.getByText(/could not be resolved/),
    ).toBeInTheDocument();
    expect(screen.getByText(/duplicate parked hook id/)).toBeInTheDocument();
    // The banner reporting the failure is not enough on its own: the events
    // region below it must not simultaneously claim the checked sources held
    // nothing, which is a conclusion the failed resolution never reached.
    expect(
      screen.queryByText(enRuntimes.hooks.events.empty_title),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText(enRuntimes.hooks.events.empty_body),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText(enRuntimes.hooks.events.unresolved_title),
    ).toBeInTheDocument();
  });

  // Positive control for the assertion above: without a resolution error the
  // empty state is the honest answer and still renders, so the absence check
  // cannot pass just because zero entries renders nothing at all.
  it("still says no entries when zero entries resolved cleanly", () => {
    hookQuery.mockReturnValue({
      data: observation({
        resolved: { provider: "claude", entries: [] },
      }),
      error: null,
      isFetching: false,
    });

    mount();

    expect(
      screen.getByText(enRuntimes.hooks.events.empty_title),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(enRuntimes.hooks.events.unresolved_title),
    ).not.toBeInTheDocument();
    expect(screen.queryByText(/could not be resolved/)).not.toBeInTheDocument();
  });

  it("says a provider without a hook surface has none", () => {
    hookQuery.mockReturnValue({ data: undefined, error: null, isFetching: false });

    mount("gemini");

    expect(
      screen.getByText("This runtime has no lifecycle hooks"),
    ).toBeInTheDocument();
  });
});
