// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type {
  AgentRuntime,
  RuntimeHookEntry,
  RuntimeHookEventAnswer,
  RuntimeHookEventAnswerResult,
  RuntimeHookReadRequest,
} from "@multica/core/types";
import enCommon from "../../../locales/en/common.json";
import enRuntimes from "../../../locales/en/runtimes.json";

// Canonical matrix for the derivations under test lives in hooks-model.test.ts.
// This suite keeps the wiring and the named honesty regressions: what the tab
// is allowed to show for each observation state, and what it must never claim.

const hookQuery = vi.fn();
// The per-event answer is a second query on the same component, so the mock
// routes by query key rather than returning one shape for both. A single
// return value would let the observation's result stand in for the answer's,
// which is the confusion the panel exists to prevent.
const answerQuery = vi.fn();

vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  return {
    ...actual,
    useQuery: (options: { queryKey: readonly unknown[] }) =>
      options.queryKey[2] === "answer" ? answerQuery() : hookQuery(),
    useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  };
});
vi.mock("@multica/core/runtimes", () => ({
  runtimeHooksKeys: { forRuntime: (id: string) => ["runtimes", "hooks", id] },
  runtimeHooksOptions: (id: string | null) => ({ queryKey: ["runtimes", "hooks", id] }),
  // The discovery card prints the real poll interval, so the mock has to
  // carry it rather than let the screen invent a friendlier number. It no
  // longer prints a client-side timeout: the bound it shows comes from the
  // server's `phase_timeout_seconds`, and this suite mocks useQuery, so the
  // card renders here with the tab's initial no-bound progress.
  HOOK_READ_POLL_INTERVAL_MS: 500,
  runtimeHookAnswerKeys: {
    forRuntime: (id: string) => ["runtimes", "hooks", "answer", id],
  },
  runtimeHookAnswerOptions: (
    id: string | null,
    event: string | null,
    value: string,
  ) => ({ queryKey: ["runtimes", "hooks", "answer", id, event, value] }),
}));

import { LifecycleHooksTab } from "./lifecycle-hooks-tab";

function runtime(
  provider = "claude",
  overrides: Partial<AgentRuntime> = {},
): AgentRuntime {
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
    ...overrides,
  };
}

function mount(provider = "claude", overrides: Partial<AgentRuntime> = {}) {
  return render(
    <I18nProvider
      locale="en"
      resources={{ en: { common: enCommon, runtimes: enRuntimes } }}
    >
      <LifecycleHooksTab runtime={runtime(provider, overrides)} />
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
    offline: false,
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
      event_value_roles: { PreToolUse: "tool_name", Notification: "unspecified" },
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
  answerQuery.mockReset();
  // Idle by default: no value has been given, so no answer exists.
  answerQuery.mockReturnValue({ data: undefined, error: null, isFetching: false });
});

describe("LifecycleHooksTab", () => {
  it("lists an entry per event with its source and both state axes", () => {
    hookQuery.mockReturnValue({
      data: observation(),
      error: null,
      isFetching: false,
    });

    mount();

    // Both event names now appear more than once: the answer panel's event
    // picker names them alongside their configured group heading.
    expect(screen.getAllByText("PreToolUse").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("Notification").length).toBeGreaterThanOrEqual(1);
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
  it("offers a cached snapshot as last known instead of serving it", () => {
    hookQuery.mockReturnValue({
      data: observation({
        cached: true,
        offline: true,
        observed_at: "2026-09-12T09:00:00Z",
      }),
      error: null,
      isFetching: false,
    });

    mount("claude", { status: "offline" });

    // Gated first: the runtime is named as offline and the snapshot is an
    // offer, not the screen's answer.
    expect(
      screen.getByText("claude (daemon-1) is offline"),
    ).toBeInTheDocument();
    expect(screen.queryByText("PreToolUse")).not.toBeInTheDocument();
    // Dated even while gated, so its age is knowable before it is opened.
    expect(
      screen.getAllByText(/Observed at 2026-09-12T09:00:00Z/).length,
    ).toBeGreaterThanOrEqual(1);

    fireEvent.click(screen.getByRole("button", { name: /last known hooks/i }));

    // Revealed: the rows appear, and every frame around them still says the
    // observation is last known rather than current (invariant 3).
    expect(screen.getByText("PreToolUse")).toBeInTheDocument();
    expect(
      screen.getByText(/Last known state, read/),
    ).toBeInTheDocument();
    expect(screen.getByText(/not as they are now/)).toBeInTheDocument();
    expect(screen.getByText(/Shown read-only/)).toBeInTheDocument();
    expect(
      screen.getAllByText(/Observed at 2026-09-12T09:00:00Z/).length,
    ).toBeGreaterThanOrEqual(2);
  });

  // AC4, second half: offline with nothing stored is its own state. It must
  // not read as a failed read, and there must be nothing to reveal.
  it("names offline-with-no-snapshot and offers nothing to reveal", () => {
    hookQuery.mockReturnValue({
      data: {
        runtime_id: "rt-1",
        status: "failed",
        cached: false,
        offline: true,
        error: "runtime is offline and has no last known hook observation",
      },
      error: null,
      isFetching: false,
    });

    mount("claude", { status: "offline" });

    expect(
      screen.getByText("claude (daemon-1) is offline"),
    ).toBeInTheDocument();
    expect(screen.getByText(/no last known state to fall back to/)).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /last known hooks/i }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText(/did not complete/)).not.toBeInTheDocument();
    expect(screen.queryByText("PreToolUse")).not.toBeInTheDocument();
  });

  // An online read that failed still reads as a failure — the offline card
  // must not absorb every empty answer.
  it("keeps an online read failure a failure", () => {
    hookQuery.mockReturnValue({
      data: {
        runtime_id: "rt-1",
        status: "timed_out",
        cached: false,
        offline: false,
        error: "daemon did not answer within 30 seconds",
      },
      error: null,
      isFetching: false,
    });

    mount();

    expect(screen.getByText(/did not complete/)).toBeInTheDocument();
    expect(
      screen.getByText(/daemon did not answer within 30 seconds/),
    ).toBeInTheDocument();
    expect(screen.queryByText(/is offline/)).not.toBeInTheDocument();
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

    expect(
      screen.getByText("Reading hooks from claude (daemon-1)"),
    ).toBeInTheDocument();
    expect(screen.queryByText("PreToolUse")).not.toBeInTheDocument();
    expect(screen.queryByText(/Observed at/)).not.toBeInTheDocument();
  });

  // AC2: a read in flight and a runtime with no hooks must not look alike.
  // The in-flight screen names the wait and its phases; the empty screen is a
  // settled statement about files, with the date that statement is about.
  it("distinguishes discovery in progress from a runtime with no hooks", () => {
    hookQuery.mockReturnValue({
      data: observation(),
      error: null,
      isFetching: true,
    });
    const discovering = mount();
    const discoveringText = discovering.container.textContent ?? "";

    expect(discoveringText).toMatch(/Polling/);
    expect(discoveringText).toMatch(/Waiting for the daemon/);
    expect(discoveringText).toMatch(/Nothing is shown until this read returns/);
    expect(discoveringText).not.toMatch(/No hook entries in the sources/);
    discovering.unmount();

    const empty = observation({ observed_at: "2026-09-12T09:00:00Z" });
    empty.resolved!.entries = [];
    hookQuery.mockReturnValue({ data: empty, error: null, isFetching: false });
    const settled = mount();
    const settledText = settled.container.textContent ?? "";

    expect(
      screen.getByText(
        "No hook entries in the sources Multica read on claude (daemon-1)",
      ),
    ).toBeInTheDocument();
    expect(settledText).not.toMatch(/Polling/);
    expect(settledText).not.toMatch(/Waiting for the daemon/);
  });

  // An empty runtime has to say which files its claim is about, and which
  // scopes it says nothing about at all.
  it("names the sources an empty result is a statement about", () => {
    const empty = observation({ observed_at: "2026-09-12T09:00:00Z" });
    empty.resolved!.entries = [];
    hookQuery.mockReturnValue({ data: empty, error: null, isFetching: false });

    mount();

    expect(
      screen.getByText(/Every source Multica could read on this host/),
    ).toBeInTheDocument();
    expect(
      screen.getAllByText("/home/u/.claude/settings.json").length,
    ).toBeGreaterThanOrEqual(1);
    // The two not-checked scopes are listed and excluded from the claim.
    expect(screen.getByText("Not checked:")).toBeInTheDocument();
    expect(
      screen.getByText(/says nothing about them/),
    ).toBeInTheDocument();
    // Invariant 3: the claim is dated in the card that makes it.
    expect(screen.getByText(/Last read/)).toBeInTheDocument();
  });

  // The other empty case: nothing was read at all, which is a statement about
  // files that are not there rather than files that held nothing.
  it("says nothing was read when no source was found", () => {
    const empty = observation({ observed_at: "2026-09-12T09:00:00Z" });
    empty.sources = empty.sources!.map((source) => ({
      ...source,
      state: "absent" as const,
      source_path: null,
      content_hash: null,
    }));
    empty.resolved!.entries = [];
    hookQuery.mockReturnValue({ data: empty, error: null, isFetching: false });

    mount();

    expect(
      screen.getByText(/None of the sources Multica checked exist/),
    ).toBeInTheDocument();
    // R2, the nothing-read half: the headline is limited to the scopes that
    // were checked too, not just the body.
    expect(
      screen.getByText(
        "No hook source was found on claude (daemon-1) in the scopes Multica checked",
      ),
    ).toBeInTheDocument();
    expect(screen.getByText("Checked, absent:")).toBeInTheDocument();
    expect(screen.queryByText("Read:")).not.toBeInTheDocument();
  });

  // R2. LOCO-126's acceptance criterion: a scope nobody checked must never
  // read as "no hooks". The headline is what a reader takes away, so the
  // qualifier has to be in it — the base observation leaves project and local
  // never checked, so an unqualified runtime-wide headline would assert more
  // than was read.
  it("keeps the empty headline limited to the sources that were read", () => {
    const empty = observation({ observed_at: "2026-09-12T09:00:00Z" });
    empty.resolved!.entries = [];
    hookQuery.mockReturnValue({ data: empty, error: null, isFetching: false });

    mount();

    const headline = screen.getByText(/No hook entries/);
    expect(headline.textContent).toBe(
      "No hook entries in the sources Multica read on claude (daemon-1)",
    );
    // The unqualified runtime-wide claim this replaced.
    expect(screen.queryByText(/^No lifecycle hooks on/)).not.toBeInTheDocument();
    // Positive control: the scopes the headline says nothing about are on
    // screen, so the qualifier is not decorating an empty exclusion.
    expect(screen.getByText("Not checked:")).toBeInTheDocument();
  });

  // R3. Ported from LOCO-392, which fixed this at the call site HookEmptyCard
  // replaced. A completed, dated read whose projection could not be resolved
  // has established nothing about what the sources hold — projection.go says
  // the honest report is "could not be resolved", not "no hooks". The guard
  // has to take out the headline as well as the body, because the headline
  // makes the claim too.
  it("never claims the sources held nothing when resolution failed", () => {
    const unresolved = observation({ observed_at: "2026-09-12T09:00:00Z" });
    unresolved.resolved = {
      provider: "claude",
      entries: [],
      error: "user:json: invalid character '}' looking for beginning of value",
    };
    hookQuery.mockReturnValue({
      data: unresolved,
      error: null,
      isFetching: false,
    });

    const { container } = mount();
    const text = container.textContent ?? "";

    // The headline.
    expect(screen.queryByText(/No hook entries in the sources/)).not.toBeInTheDocument();
    expect(screen.queryByText(/^No hook source was found on/)).not.toBeInTheDocument();
    // The body.
    expect(text).not.toMatch(/Every source Multica could read on this host/);
    expect(text).not.toMatch(/None of the sources Multica checked exist/);
    // What it says instead.
    expect(
      screen.getByText(
        "Multica cannot list entries from sources it could not resolve",
      ),
    ).toBeInTheDocument();
    expect(text).toMatch(/could not interpret them/);
  });

  // The positive control for the assertion above: the same shape with the
  // resolution error removed renders the empty card, so the absence is caused
  // by the error and not by the state failing to render at all.
  it("still shows the empty card when resolution succeeded", () => {
    const empty = observation({ observed_at: "2026-09-12T09:00:00Z" });
    empty.resolved = { provider: "claude", entries: [] };
    hookQuery.mockReturnValue({ data: empty, error: null, isFetching: false });

    mount();

    expect(
      screen.getByText(/No hook entries in the sources/),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(/Multica cannot list entries/),
    ).not.toBeInTheDocument();
  });

  // R3, the partly-resolved case: one malformed source must not hide the
  // sources that did resolve. Error plus entries keeps the list.
  it("renders the entries of a partly-resolved read", () => {
    const partial = observation({ observed_at: "2026-09-12T09:00:00Z" });
    partial.resolved!.error =
      "project:json: invalid character '}' looking for beginning of value";
    hookQuery.mockReturnValue({ data: partial, error: null, isFetching: false });

    mount();

    expect(screen.getAllByRole("table").length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText("guard.sh")).toBeInTheDocument();
    expect(
      screen.queryByText(/Multica cannot list entries/),
    ).not.toBeInTheDocument();
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
        offline: false,
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
      screen.queryByText(
        enRuntimes.hooks.events.empty_title.replace(
          "{{name}}",
          "claude (daemon-1)",
        ),
      ),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText(enRuntimes.hooks.events.empty_read_body),
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
      screen.getByText(
        enRuntimes.hooks.events.empty_title.replace(
          "{{name}}",
          "claude (daemon-1)",
        ),
      ),
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

// The what-actually-runs panel. Canonical matrix for its derivations lives in
// hook-answer-model.test.ts; these are the wiring and the named honesty
// regressions — the statements this screen must never make.
describe("LifecycleHooksTab — what actually runs", () => {
  function answerEntry(
    overrides: Partial<RuntimeHookEntry> = {},
  ): RuntimeHookEntry {
    return {
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
      ...overrides,
    };
  }

  function answerResult(
    overrides: Partial<RuntimeHookEventAnswer> = {},
    envelope: Partial<RuntimeHookEventAnswerResult> = {},
  ): RuntimeHookEventAnswerResult {
    return {
      runtime_id: "rt-1",
      provider: "claude",
      cached: false,
      observed_at: "2026-09-12T12:00:00Z",
      answer: {
        provider: "claude",
        event: "PreToolUse",
        value: "Bash",
        value_role: "tool_name",
        answerable: true,
        matched: [],
        not_matched: [],
        never_runs: [],
        configuration_excluded: [],
        ...overrides,
      },
      ...envelope,
    };
  }

  function mountWithAnswer(
    result: RuntimeHookEventAnswerResult | undefined,
    provider = "claude",
    fetching = false,
  ) {
    hookQuery.mockReturnValue({
      data: observation(),
      error: null,
      isFetching: false,
    });
    answerQuery.mockReturnValue({
      data: result,
      error: null,
      isFetching: fetching,
    });
    const rendered = mount(provider);
    // A value has to be present for the panel to leave its idle state, the
    // same rule the server enforces: a matcher is evaluated against a value.
    // Addressed by id rather than by label, because the label is the selected
    // event's role and changes with the picker.
    if (result) {
      const input = rendered.container.querySelector("#hook-answer-value");
      if (!input) throw new Error("no value field");
      fireEvent.change(input, { target: { value: "Bash" } });
      fireEvent.click(screen.getByRole("button", { name: "Answer" }));
    }
    return rendered;
  }

  // The value field is captioned from the projection's per-event role, so it
  // is right before any answer exists. Asking in order to learn what to type
  // would be backwards.
  it("captions the value field from the event's own role, before any answer", () => {
    hookQuery.mockReturnValue({
      data: observation({
        resolved: {
          provider: "claude",
          event_value_roles: { PreToolUse: "tool_name" },
          entries: [
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
      }),
      error: null,
      isFetching: false,
    });

    mount();

    expect(
      screen.getByLabelText("Tool name", { selector: "input" }),
    ).toBeInTheDocument();
  });

  // An event whose matcher the provider discards says so, rather than leaving
  // the reader to infer it from an answer that never changes.
  it("says the value is inert where the provider discards the matcher", () => {
    hookQuery.mockReturnValue({
      data: observation({
        resolved: {
          provider: "claude",
          event_value_roles: { UserPromptSubmit: "ignored" },
          entries: [
            {
              hook_id: "sha256:ignored",
              event: "UserPromptSubmit",
              matcher: "anything",
              matcher_kind: "ignored",
              handler: { type: "command", command: "log.sh" },
              handler_type: "command",
              sources: [{ scope: "user", format: "json", kind: "settings" }],
              configuration: "live",
              effectiveness: "will_run",
              trust: "not_applicable",
            },
          ],
        },
      }),
      error: null,
      isFetching: false,
    });

    const { container } = mount();

    expect(container.textContent).toMatch(
      /the value changes nothing: every registered handler is matched/,
    );
  });

  it("claims no outcome before a value is given", () => {
    hookQuery.mockReturnValue({
      data: observation(),
      error: null,
      isFetching: false,
    });

    const { container } = mount();
    const text = container.textContent ?? "";

    expect(text).toMatch(/No value given yet, so nothing here claims an outcome/);
    // The prompt itself says it lists conditions, not outcomes.
    expect(text).toMatch(/under what condition each one fires/);
  });

  it("answers with the matched set and states that it is unordered", () => {
    const { container } = mountWithAnswer(
      answerResult({ matched: [answerEntry()] }),
    );
    const text = container.textContent ?? "";

    expect(text).toMatch(/1 of 1 handlers configured on this event run/);
    expect(text).toMatch(/Multica does not know the order in which they run/);
    // Nothing numbers the set, and the set is a real list element.
    expect(
      container.querySelectorAll("ol, [type='1'], [start]").length,
    ).toBe(0);
  });

  // The defect class this view exists to avoid: "cannot answer" must never be
  // rendered as "nothing runs".
  it("renders an unevaluable matcher as unanswerable, with no counts and no sets", () => {
    const { container } = mountWithAnswer(
      answerResult({
        answerable: false,
        error: 'compile claude matcher "^(?!Notebook).*" failed',
        unevaluable: [
          {
            hook_id: "sha256:bad",
            matcher: "^(?!Notebook).*",
            error: "error parsing regexp: invalid or unsupported Perl syntax: `(?!`",
          },
        ],
        // Present in the payload and still not rendered: an unanswerable
        // event partitions nothing.
        matched: [answerEntry()],
      }),
    );
    const text = container.textContent ?? "";

    expect(text).toMatch(/Multica cannot answer for PreToolUse/);
    expect(text).toMatch(/\^\(\?!Notebook\)\.\*/);
    expect(text).toMatch(/invalid or unsupported Perl syntax/);
    expect(text).toMatch(/Nothing is claimed to run and nothing is claimed not to run/);
    // No headline count anywhere, and no matched set heading.
    expect(text).not.toMatch(/handlers configured on this event run/);
    expect(text).not.toMatch(/Matched by this value/);
  });

  // An answerable event with an empty matched set is a real answer about the
  // value — and must say so, rather than reading as a verdict on the event.
  it("distinguishes nothing-matched-this-value from cannot-answer", () => {
    const { container } = mountWithAnswer(answerResult());
    const text = container.textContent ?? "";

    expect(text).toMatch(/That is an answer about this value, not about the event/);
    expect(text).not.toMatch(/Multica cannot answer/);
  });

  it("keeps both axes on a matched entry the provider skips", () => {
    const { container } = mountWithAnswer(
      answerResult({
        matched: [
          answerEntry({
            hook_id: "sha256:skipped",
            effectiveness: "never_runs",
            never_runs_reason: "handler_type_unsupported_by_provider",
          }),
        ],
      }),
    );
    const text = container.textContent ?? "";

    expect(text).toMatch(/0 of 1 handlers configured on this event run/);
    expect(text).toMatch(/1 matched but skipped by the provider/);
    // Configuration and verdict both present on the row.
    expect(screen.getAllByText("Registered").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("Will never run").length).toBeGreaterThanOrEqual(1);
  });

  it("renders a cross-source collapse as one entry with both sources", () => {
    const { container } = mountWithAnswer(
      answerResult({
        matched: [
          answerEntry({
            sources: [
              { scope: "user", format: "json", kind: "settings" },
              { scope: "project", format: "json", kind: "settings" },
            ],
          }),
        ],
      }),
    );
    const text = container.textContent ?? "";

    expect(text).toMatch(/1 of 1 handlers configured on this event run/);
    expect(text).toMatch(/1 duplicate definition absorbed/);
    expect(text).toMatch(/one definition seen twice and runs once/);
    expect(text).toMatch(/neither displaces the other/);
    expect(text).not.toMatch(/shadow|overrid|precedence|\bwins\b/i);
    expect(screen.getAllByText("project/json").length).toBeGreaterThanOrEqual(1);
  });

  it("teaches Claude's absent per-hook disable and trust gate", () => {
    const { container } = mountWithAnswer(
      answerResult({ matched: [answerEntry()] }),
    );
    expect(container.textContent).toMatch(
      /Claude has no per-hook disable and no trust gate/,
    );
  });

  it("teaches Codex's per-hook disable and its trust gate", () => {
    const { container } = mountWithAnswer(
      answerResult(
        { provider: "codex", matched: [answerEntry({ effectiveness: "trust_unknown", trust: "pending_review" })] },
        { provider: "codex" },
      ),
      "codex",
    );
    const text = container.textContent ?? "";

    expect(text).toMatch(/only while its exact definition is trusted on the host/);
    expect(text).toMatch(/individual non-managed hooks can be switched off/);
    expect(text).toMatch(/1 with trust Multica cannot confirm/);
    expect(text).toMatch(/0 of 1 handlers configured on this event run/);
  });

  // Criterion: not-checked is never "nothing here". The fixture observation
  // has two never-checked project scopes.
  it("says the answer covers only the sources somebody read", () => {
    const { container } = mountWithAnswer(
      answerResult({ matched: [answerEntry()] }),
    );
    expect(container.textContent).toMatch(
      /2 expected sources were never checked/,
    );
  });

  it("dates the answer and flags one computed from another observation", () => {
    const { container } = mountWithAnswer(
      answerResult({ matched: [answerEntry()] }, { observed_at: "2026-09-11T09:00:00Z" }),
    );
    const text = container.textContent ?? "";

    expect(text).toMatch(/Observed at 2026-09-11T09:00:00Z/);
    expect(text).toMatch(/They are two different reads/);
  });

  it("renders an answer with no observation date as a refusal, with no counts and no sets", () => {
    // The acceptance rule this panel exists for: nothing renders as "these
    // fire" without a value and a dated observation behind it. The body is
    // one a newer backend could send — a full answer, no date. Scoped to the
    // panel, because the tab's own rows and banner have their own dated read.
    const { container } = mountWithAnswer(
      answerResult({ matched: [answerEntry()] }, { observed_at: undefined }),
    );
    const panel = container.querySelector(
      'section[aria-labelledby="hook-answer-title"]',
    );
    const text = panel?.textContent ?? "";

    expect(text).toMatch(/The answer did not come back/);
    // No headline count, no count chip, no set.
    expect(text).not.toMatch(/handlers configured on this event run/);
    expect(text).not.toMatch(/will run/);
    expect(text).not.toMatch(/Matched by this value/);
    // And no observation line under it: the answer has no date to show.
    expect(text).not.toMatch(/Answered from the read/);
    expect(text).not.toMatch(/Answered from the last known state/);
  });

  it("reports an unobserved runtime rather than an event with nothing on it", () => {
    const { container } = mountWithAnswer(
      answerResult({}, { answer: undefined, observed_at: undefined, error: "runtime has no hook observation to answer from" }),
    );
    const text = container.textContent ?? "";

    expect(text).toMatch(/Nothing has been read from this host/);
    expect(text).toMatch(/not a runtime whose hooks do nothing/);
    expect(text).not.toMatch(/handlers configured on this event run/);
  });

  // Read-only: stages 5 and 6 own the write path.
  it("offers no add, edit, delete, park or disable control", () => {
    const { container } = mountWithAnswer(
      answerResult({ matched: [answerEntry()] }),
    );
    const labels = [...container.querySelectorAll("button")].map(
      (button) => `${button.textContent ?? ""} ${button.getAttribute("title") ?? ""}`,
    );
    for (const label of labels) {
      expect(label).not.toMatch(/\bpark\b|\bunpark\b|\bdisable\b|\benable\b/i);
      expect(label).not.toMatch(/\badd\b|\bedit\b|\bdelete\b|\bremove\b|\bsave\b/i);
    }
  });
});
