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
  hookAnswerMembers,
  hookAnswerTotals,
  hookEmptySummary,
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

function read(
  overrides: Partial<RuntimeHookReadRequest> = {},
): RuntimeHookReadRequest {
  return {
    runtime_id: "rt-1",
    status: "completed",
    cached: false,
    offline: false,
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
      null,
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
      null,
    );

    expect(rows.map((row) => row.entryCount)).toEqual([1, 1]);
  });

  it("returns no rows when the read carried no sources", () => {
    expect(hookScopeRows("claude", undefined, [], null)).toEqual([]);
  });

  // LOCO-408 AC1: a source whose entries could not be resolved has no entry
  // count. Zero would be the "no hooks" claim restated as a number, and
  // server/internal/runtimehooks/projection.go states the opposite rule: the
  // honest report of an unreadable snapshot is that it could not be resolved.
  it("states no entry count for a found source whose resolution failed", () => {
    const rows = hookScopeRows(
      "claude",
      [source()],
      [],
      "parse ~/.claude/settings.json: unexpected end of JSON input",
    );

    expect(rows[0]?.state).toBe("found");
    expect(rows[0]?.entryCount).toBeNull();
  });

  // LOCO-408 AC3: the fix is not "suppress zeros". A source that resolved
  // cleanly and genuinely holds nothing still says so.
  it("still states zero for a source that resolved cleanly and holds nothing", () => {
    const rows = hookScopeRows("claude", [source()], [], null);

    expect(rows[0]?.entryCount).toBe(0);
  });

  // LOCO-408 AC1, partial half: Project() carries its error alongside the
  // entries it could resolve, so a source that contributed entries
  // demonstrably parsed and keeps its real count. Only the found source that
  // contributed none is ambiguous — it may be the one that failed.
  it("keeps the counts it has when only some sources resolved", () => {
    const rows = hookScopeRows(
      "claude",
      [
        source(),
        source({ scope: "project", source_path: "/w/.claude/settings.json" }),
        source({ scope: "local", state: "absent", source_path: null, content_hash: null }),
      ],
      [entry()],
      "parse /w/.claude/settings.json: invalid character '}'",
    );

    expect(rows.map((row) => row.entryCount)).toEqual([1, null, 0]);
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
      null,
    );
    const totals = hookTotals(
      [entry(), entry({ hook_id: "b", configuration: "parked" })],
      scopes,
      null,
    );

    expect(totals).toMatchObject({
      entryCoverage: "complete",
      configured: 2,
      willRun: 1,
      inactive: 1,
      events: 1,
      foundSources: 1,
      absentSources: 1,
      notCheckedSources: 1,
    });
  });

  // LOCO-408 AC2: the runtime rail has to separate "zero" from "not
  // established" too. With nothing resolved there is no entry total to state
  // — but the source states came from the host's own diff of expected against
  // observed, which a parse failure does not touch, so those stay.
  it("establishes no entry total when resolution produced nothing", () => {
    const scopes = hookScopeRows(
      "claude",
      [
        source(),
        source({ scope: "project", state: "not_checked", source_path: null, content_hash: null }),
      ],
      [],
      "parse ~/.claude/settings.json: unexpected end of JSON input",
    );

    const totals = hookTotals([], scopes, "parse ~/.claude/settings.json: unexpected end of JSON input");

    expect(totals.entryCoverage).toBe("unestablished");
    expect(totals.foundSources).toBe(1);
    expect(totals.notCheckedSources).toBe(1);
  });

  // LOCO-408 AC2, second half: a partial failure keeps the counts it
  // legitimately has and is not allowed to present them as a complete total.
  it("marks a partly resolved read as a floor rather than a total", () => {
    const error = "parse /w/.claude/settings.json: invalid character '}'";
    const scopes = hookScopeRows(
      "claude",
      [source(), source({ scope: "project" })],
      [entry()],
      error,
    );

    const totals = hookTotals([entry()], scopes, error);

    expect(totals.entryCoverage).toBe("partial");
    expect(totals.configured).toBe(1);
  });
});

describe("hookEmptySummary", () => {
  // "No hooks" is two different statements depending on whether anything was
  // read, and an unchecked scope supports neither. The empty screen needs all
  // three separated so it can say which files its claim is about.
  it("separates read-and-empty from nothing-to-read, and carries unchecked scopes", () => {
    const scopes = hookScopeRows(
      "claude",
      [
        source({ scope: "user", source_path: "/home/u/.claude/settings.json" }),
        source({ scope: "project", state: "absent" }),
        source({ scope: "local", state: "not_checked" }),
      ],
      [],
      null,
    );

    const summary = hookEmptySummary(scopes);

    expect(summary.nothingRead).toBe(false);
    expect(summary.readPaths).toEqual(["/home/u/.claude/settings.json"]);
    expect(summary.absent.map((scope) => scope.key)).toEqual(["project:json"]);
    expect(summary.notChecked.map((scope) => scope.key)).toEqual(["local:json"]);
  });

  it("reports nothing read when no source was found", () => {
    const scopes = hookScopeRows(
      "claude",
      [
        source({ scope: "user", state: "absent" }),
        source({ scope: "project", state: "not_checked" }),
      ],
      [],
      null,
    );

    const summary = hookEmptySummary(scopes);

    expect(summary.nothingRead).toBe(true);
    expect(summary.readPaths).toEqual([]);
  });

  it("falls back to the expected path for a found source with no reported path", () => {
    const scopes = hookScopeRows(
      "claude",
      [source({ scope: "user", source_path: null })],
      [],
      null,
    );

    expect(hookEmptySummary(scopes).readPaths).toEqual([
      "~/.claude/settings.json",
    ]);
  });
});

describe("hookObservationView", () => {
  // Snapshot invariant 1 + the gcTime caveat: a value still in cache during a
  // new discovery must not be presented as this mount's observation. Deciding
  // from isFetching before touching `data` is what enforces that.
  it("reports discovering while a read is in flight, whatever data holds", () => {
    const previous = read({ observed_at: "2026-09-12T11:59:00Z" });

    const view = hookObservationView(previous, true, null, NOW);

    expect(view.kind).toBe("discovering");
    expect(view.observedAt).toBeNull();
    expect(hookObservationShowsEntries(view)).toBe(false);
  });

  // Snapshot invariant 3: a cached snapshot is dated and reads as last known.
  it("dates a cached snapshot and marks it last known", () => {
    const view = hookObservationView(
      read({ cached: true, offline: true, observed_at: "2026-09-12T11:00:00Z" }),
      false,
      null,
      NOW,
    );

    expect(view.kind).toBe("last_known");
    expect(view.offline).toBe(true);
    expect(view.observedAt).toBe("2026-09-12T11:00:00Z");
    expect(hookObservationShowsEntries(view)).toBe(true);
  });

  // Offline with nothing stored is its own state, not a failed read: nothing
  // went wrong, the host is unreachable and has never been read. The UI has to
  // say that rather than blame the read, so the two must not share a kind.
  it("names offline-with-no-snapshot instead of calling it a failure", () => {
    const view = hookObservationView(
      read({
        status: "failed",
        offline: true,
        error: "runtime is offline and has no last known hook observation",
      }),
      false,
      null,
      NOW,
    );

    expect(view.kind).toBe("offline_no_snapshot");
    expect(view.offline).toBe(true);
    expect(view.error).toMatch(/no last known hook observation/);
    expect(hookObservationShowsEntries(view)).toBe(false);
  });

  // A backend that predates the `offline` field still marks the snapshot path
  // `cached`, so the offline reading survives without it.
  it("treats a cached answer as offline even without the offline flag", () => {
    const view = hookObservationView(
      read({ cached: true, observed_at: "2026-09-12T11:00:00Z" }),
      false,
      null,
      NOW,
    );

    expect(view.kind).toBe("last_known");
    expect(view.offline).toBe(true);
  });

  // A non-terminal answer from an online runtime is still a failure — the
  // offline branch must not swallow every undated non-completed read.
  it("keeps an online non-completed read a failure", () => {
    const view = hookObservationView(
      read({ status: "timed_out", error: "daemon did not answer" }),
      false,
      null,
      NOW,
    );

    expect(view.kind).toBe("failed");
    expect(view.offline).toBe(false);
  });

  it("refuses to show entries for a completed online read with no date", () => {
    const view = hookObservationView(read(), false, null, NOW);

    expect(view.kind).toBe("no_observation");
    expect(hookObservationShowsEntries(view)).toBe(false);
  });

  it("reports an undated completed offline answer as offline, not undated", () => {
    const view = hookObservationView(
      read({ cached: true, offline: true }),
      false,
      null,
      NOW,
    );

    expect(view.kind).toBe("offline_no_snapshot");
    expect(hookObservationShowsEntries(view)).toBe(false);
  });

  // The observed_at bound is one-directional, so a far-past timestamp can
  // arrive on a live read and must read as stale rather than current.
  it("flags a far-past observation as stale on a live read", () => {
    const view = hookObservationView(
      read({
        observed_at: new Date(
          NOW - HOOK_OBSERVATION_STALE_AFTER_MS - 1_000,
        ).toISOString(),
      }),
      false,
      null,
      NOW,
    );

    expect(view.kind).toBe("live");
    expect(view.stale).toBe(true);
  });

  it("does not flag a just-taken observation as stale", () => {
    const view = hookObservationView(
      read({ observed_at: new Date(NOW - 1_000).toISOString() }),
      false,
      null,
      NOW,
    );

    expect(view.stale).toBe(false);
  });

  it("treats an unparseable observation date as stale", () => {
    const view = hookObservationView(
      read({ observed_at: "soon" }),
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
      read({
        observed_at: "2026-09-12T11:59:30Z",
        resolved: {
          provider: "claude",
          entries: [],
          error: "duplicate parked hook id",
        },
      }),
      false,
      null,
      NOW,
    );

    expect(view.kind).toBe("live");
    expect(view.resolutionError).toBe("duplicate parked hook id");
    expect(hookObservationShowsEntries(view)).toBe(true);
  });
});

// LOCO-533 R2. A fallback may report less than the backend would; it may not
// report something the backend never said. The pre-member wire shape carried
// sources and no per-member identity, so the fallback stamped the surviving
// row's `hook_id`, `matcher` and `occurrence` onto every source — which put
// the id of the entry that survived the collapse beside the scope of an entry
// that did not. That is a fabricated identity (DP-LOCO-114-02 clause 2), and a
// fallback is not an exemption from the rule.
describe("answer members on an observation that predates member projection", () => {
  const userSource = { scope: "user", format: "json", kind: "settings" };
  const projectSource = { scope: "project", format: "json", kind: "settings" };
  const collapsed = entry({
    hook_id: "sha256:user-copy",
    matcher: "Bash",
    sources: [userSource, projectSource],
  });

  it("claims no identity for a source whose identity was discarded", () => {
    const members = hookAnswerMembers(collapsed);

    expect(members.map((member) => member.source)).toEqual([
      userSource,
      projectSource,
    ]);
    // Not the surviving row's id, not the surviving row's matcher, not an
    // occurrence of 0 standing in for an occurrence nobody recorded.
    for (const member of members) {
      expect(member.identity).toBeNull();
    }
    expect(JSON.stringify(members)).not.toContain("sha256:user-copy");
  });

  it("still reports one row per contributing source, so the collapsed count is unchanged", () => {
    // The collapsed chip counts absorbed definitions as members minus one.
    // Withholding identity must not quietly change that arithmetic.
    expect(hookAnswerMembers(collapsed)).toHaveLength(2);
    expect(
      hookAnswerTotals({
        provider: "claude",
        event: "PreToolUse",
        value: "Bash",
        value_role: "tool_name",
        answerable: true,
        matched: [collapsed],
        not_matched: [],
        never_runs: [],
        configuration_excluded: [],
      }).collapsed,
    ).toBe(1);
  });

  it("passes the server's own identities through untouched when it sent them", () => {
    const members = hookAnswerMembers(
      entry({
        hook_id: "sha256:user-copy",
        sources: [userSource, projectSource],
        members: [
          {
            hook_id: "sha256:user-copy",
            occurrence: 0,
            matcher: "Bash",
            matcher_kind: "exact",
            source: userSource,
          },
          {
            hook_id: "sha256:project-copy",
            occurrence: 1,
            matcher: "Bash|Read",
            matcher_kind: "regex",
            source: projectSource,
          },
        ],
      }),
    );

    expect(members.map((member) => member.identity?.hook_id)).toEqual([
      "sha256:user-copy",
      "sha256:project-copy",
    ]);
    expect(members.map((member) => member.identity?.matcher)).toEqual([
      "Bash",
      "Bash|Read",
    ]);
    expect(members.map((member) => member.identity?.occurrence)).toEqual([0, 1]);
  });
});
