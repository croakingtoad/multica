// @vitest-environment node
import { describe, expect, it } from "vitest";
import type {
  RuntimeHookEntry,
  RuntimeHookReadRequest,
  RuntimeHookSource,
} from "@multica/core/types";
import {
  HOOK_OBSERVATION_STALE_AFTER_MS,
  entryWillRun,
  hookEventGroups,
  hookObservationShowsEntries,
  hookObservationView,
  hookScopeIsWritable,
  hookScopeRows,
  hookSourcePath,
  hookTotals,
  providerSupportsHooks,
} from "./hooks-model";

function entry(overrides: Partial<RuntimeHookEntry> = {}): RuntimeHookEntry {
  return {
    hook_id: "sha256:1",
    event: "PreToolUse",
    matcher: "Bash",
    matcher_kind: "exact",
    handler: { type: "command", command: "guard" },
    handler_type: "command",
    sources: [{ scope: "user", format: "json", kind: "settings" }],
    configuration: "live",
    effectiveness: "will_run",
    trust: "not_applicable",
    ...overrides,
  };
}

function source(overrides: Partial<RuntimeHookSource> = {}): RuntimeHookSource {
  return {
    provider: "claude",
    scope: "user",
    format: "json",
    state: "found",
    source_path: "/home/u/.claude/settings.json",
    content_hash: "abc",
    ...overrides,
  };
}

const NOW = Date.parse("2026-09-12T12:00:00Z");

describe("provider support", () => {
  it("admits only the two providers with a hook surface", () => {
    expect(providerSupportsHooks("claude")).toBe(true);
    expect(providerSupportsHooks("codex")).toBe(true);
    expect(providerSupportsHooks("gemini")).toBe(false);
  });
});

describe("scope writability and paths", () => {
  // AC6: user scope is the only writable one. Project and local must read as
  // read-only, and they are read-only because of where they live, not because
  // this pass happens to have no write path.
  it("marks only user scope writable", () => {
    expect(hookScopeIsWritable("user")).toBe(true);
    expect(hookScopeIsWritable("project")).toBe(false);
    expect(hookScopeIsWritable("local")).toBe(false);
  });

  it("names the path each expected source lives at", () => {
    expect(hookSourcePath("claude", "local", "json")).toBe(
      "<project>/.claude/settings.local.json",
    );
    expect(hookSourcePath("codex", "user", "toml")).toBe("~/.codex/config.toml");
    expect(hookSourcePath("codex", "session", "json")).toBeNull();
  });
});

describe("hookScopeRows", () => {
  // AC3: the three source states stay distinct. "not checked" carries no
  // entries and no path of its own, and must never be read as "no hooks".
  it("keeps found, absent and not-checked apart", () => {
    const rows = hookScopeRows(
      "claude",
      [
        source(),
        source({ scope: "project", state: "absent", source_path: null, content_hash: null }),
        source({ scope: "local", state: "not_checked", source_path: null, content_hash: null }),
      ],
      [entry()],
    );

    expect(rows.map((row) => row.state)).toEqual(["found", "absent", "not_checked"]);
    expect(rows[0]?.entryCount).toBe(1);
    expect(rows[0]?.sourcePath).toBe("/home/u/.claude/settings.json");
    expect(rows[1]?.sourcePath).toBeNull();
    expect(rows[1]?.expectedPath).toBe("<project>/.claude/settings.json");
    expect(rows[2]?.entryCount).toBe(0);
    expect(rows[2]?.writable).toBe(false);
  });

  it("counts a deduplicated handler against every source that defines it", () => {
    const rows = hookScopeRows(
      "claude",
      [source(), source({ scope: "project" })],
      [
        entry({
          sources: [
            { scope: "user", format: "json", kind: "settings" },
            { scope: "project", format: "json", kind: "settings" },
          ],
        }),
      ],
    );

    expect(rows.map((row) => row.entryCount)).toEqual([1, 1]);
  });

  it("returns no rows when the read carried no sources", () => {
    expect(hookScopeRows("claude", undefined, [])).toEqual([]);
  });
});

describe("entryWillRun", () => {
  // AC2: the axes are independent. A parked hook that is also ineligible must
  // not reduce to one state, and unparking it would still not make it run.
  it("requires both a live configuration and a will_run verdict", () => {
    expect(entryWillRun(entry())).toBe(true);
    expect(entryWillRun(entry({ configuration: "parked" }))).toBe(false);
    expect(entryWillRun(entry({ effectiveness: "never_runs" }))).toBe(false);
    expect(entryWillRun(entry({ effectiveness: "trust_unknown" }))).toBe(false);
    expect(
      entryWillRun(entry({ configuration: "disabled", effectiveness: "will_run" })),
    ).toBe(false);
  });
});

describe("hookEventGroups", () => {
  it("counts each state category separately within an event", () => {
    const groups = hookEventGroups([
      entry({ hook_id: "a" }),
      entry({ hook_id: "b", configuration: "parked", parked_at: "2026-09-10T10:00:00Z" }),
      entry({ hook_id: "c", effectiveness: "never_runs", never_runs_reason: "matcher_ineligible" }),
      entry({ hook_id: "d", effectiveness: "trust_unknown", trust: "pending_review" }),
      entry({ hook_id: "e", matcher: "^(?!x).*", matcher_kind: "unevaluable" }),
    ]);

    expect(groups).toHaveLength(1);
    const group = groups[0]!;
    expect(group.willRun).toBe(2);
    expect(group.inactive).toBe(1);
    expect(group.neverRuns).toBe(1);
    expect(group.trustUnknown).toBe(1);
    expect(group.unevaluableMatchers).toBe(1);
  });

  // A hook that is parked AND ineligible counts once as inactive and not
  // again as never-running: the badges would otherwise double-count it.
  it("attributes a parked and ineligible hook to configuration only", () => {
    const group = hookEventGroups([
      entry({ configuration: "parked", effectiveness: "never_runs" }),
    ])[0]!;

    expect(group.inactive).toBe(1);
    expect(group.neverRuns).toBe(0);
    expect(group.willRun).toBe(0);
  });

  it("groups by event and orders the groups by name", () => {
    const groups = hookEventGroups([
      entry({ event: "Stop", hook_id: "s" }),
      entry({ event: "PreToolUse", hook_id: "p" }),
      entry({ event: "Stop", hook_id: "s2" }),
    ]);

    expect(groups.map((group) => group.event)).toEqual(["PreToolUse", "Stop"]);
    expect(groups[1]?.entries).toHaveLength(2);
  });

  // Codex keeps hooks.json and inline config.toml as two sources in one
  // scope, and both are live. The group has to report two sources, not one
  // scope, or the screen would hide half of what is configured.
  it("counts scope and format as distinct sources", () => {
    const group = hookEventGroups([
      entry({ hook_id: "j", sources: [{ scope: "user", format: "json", kind: "settings" }] }),
      entry({ hook_id: "t", sources: [{ scope: "user", format: "toml", kind: "settings" }] }),
    ])[0]!;

    expect(group.sourceCount).toBe(2);
    expect(group.willRun).toBe(2);
  });
});

describe("hookTotals", () => {
  it("summarises entries and the three source states", () => {
    const scopes = hookScopeRows(
      "claude",
      [
        source(),
        source({ scope: "project", state: "not_checked", source_path: null, content_hash: null }),
        source({ scope: "local", state: "absent", source_path: null, content_hash: null }),
      ],
      [],
    );
    const totals = hookTotals(
      [entry(), entry({ hook_id: "b", configuration: "parked" })],
      scopes,
    );

    expect(totals).toMatchObject({
      configured: 2,
      willRun: 1,
      inactive: 1,
      events: 1,
      foundSources: 1,
      absentSources: 1,
      notCheckedSources: 1,
    });
  });
});

describe("hookObservationView", () => {
  // Snapshot invariant 1 + the gcTime caveat: a value still in cache during a
  // new discovery must not be presented as this mount's observation. Deciding
  // from isFetching before touching `data` is what enforces that.
  it("reports discovering while a read is in flight, whatever data holds", () => {
    const previous: RuntimeHookReadRequest = {
      runtime_id: "rt-1",
      status: "completed",
      cached: false,
      observed_at: "2026-09-12T11:59:00Z",
    };

    const view = hookObservationView(previous, true, null, NOW);

    expect(view.kind).toBe("discovering");
    expect(view.observedAt).toBeNull();
    expect(hookObservationShowsEntries(view)).toBe(false);
  });

  // Snapshot invariant 3: a cached snapshot is dated and reads as last known.
  it("dates a cached snapshot and marks it last known", () => {
    const view = hookObservationView(
      {
        runtime_id: "rt-1",
        status: "completed",
        cached: true,
        observed_at: "2026-09-12T11:00:00Z",
      },
      false,
      null,
      NOW,
    );

    expect(view.kind).toBe("last_known");
    expect(view.observedAt).toBe("2026-09-12T11:00:00Z");
    expect(hookObservationShowsEntries(view)).toBe(true);
  });

  // Snapshot invariant 3, second half: no prior observation is an explicit
  // failure, never an undated empty cache that reads as "no hooks".
  it("reports no observation when an offline runtime has nothing stored", () => {
    const view = hookObservationView(
      {
        runtime_id: "rt-1",
        status: "failed",
        cached: false,
        error: "runtime is offline and has no last known hook observation",
      },
      false,
      null,
      NOW,
    );

    expect(view.kind).toBe("failed");
    expect(view.error).toMatch(/no last known hook observation/);
    expect(hookObservationShowsEntries(view)).toBe(false);
  });

  it("refuses to show entries for a completed read with no date", () => {
    const view = hookObservationView(
      { runtime_id: "rt-1", status: "completed", cached: true },
      false,
      null,
      NOW,
    );

    expect(view.kind).toBe("no_observation");
    expect(hookObservationShowsEntries(view)).toBe(false);
  });

  // The observed_at bound is one-directional, so a far-past timestamp can
  // arrive on a live read and must read as stale rather than current.
  it("flags a far-past observation as stale on a live read", () => {
    const view = hookObservationView(
      {
        runtime_id: "rt-1",
        status: "completed",
        cached: false,
        observed_at: new Date(NOW - HOOK_OBSERVATION_STALE_AFTER_MS - 1_000).toISOString(),
      },
      false,
      null,
      NOW,
    );

    expect(view.kind).toBe("live");
    expect(view.stale).toBe(true);
  });

  it("does not flag a just-taken observation as stale", () => {
    const view = hookObservationView(
      {
        runtime_id: "rt-1",
        status: "completed",
        cached: false,
        observed_at: new Date(NOW - 1_000).toISOString(),
      },
      false,
      null,
      NOW,
    );

    expect(view.stale).toBe(false);
  });

  it("treats an unparseable observation date as stale", () => {
    const view = hookObservationView(
      { runtime_id: "rt-1", status: "completed", cached: false, observed_at: "soon" },
      false,
      null,
      NOW,
    );

    expect(view.stale).toBe(true);
  });

  it("surfaces a query failure as a failure, not an empty runtime", () => {
    const view = hookObservationView(
      undefined,
      false,
      new Error("runtime hook discovery timed out while polling"),
      NOW,
    );

    expect(view.kind).toBe("failed");
    expect(view.error).toMatch(/timed out/);
  });

  // A resolution failure travels alongside a perfectly good observation: the
  // sources were read, their contents could not be resolved. It must not be
  // flattened into either a read failure or an empty hook list.
  it("carries a resolution failure without discarding the observation", () => {
    const view = hookObservationView(
      {
        runtime_id: "rt-1",
        status: "completed",
        cached: false,
        observed_at: "2026-09-12T11:59:30Z",
        resolved: { provider: "claude", entries: [], error: "duplicate parked hook id" },
      },
      false,
      null,
      NOW,
    );

    expect(view.kind).toBe("live");
    expect(view.resolutionError).toBe("duplicate parked hook id");
    expect(hookObservationShowsEntries(view)).toBe(true);
  });
});
