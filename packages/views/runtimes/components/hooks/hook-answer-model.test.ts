// @vitest-environment node
import { describe, expect, it } from "vitest";
import type {
  RuntimeHookEntry,
  RuntimeHookEventAnswer,
  RuntimeHookEventAnswerResult,
} from "@multica/core/types";
import {
  hookAnsweredEvents,
  hookAnswerTotals,
  hookAnswerView,
} from "./hooks-model";

// Canonical matrix for the per-event answer derivations. The component suite
// keeps the wiring and the named honesty regressions and does not re-run this.
//
// The matcher rules themselves are not tested here at all: they are decided
// in server/internal/runtimehooks and covered by answer_test.go against the
// providers' own docs. Everything below is shape and arithmetic.

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

function answer(
  overrides: Partial<RuntimeHookEventAnswer> = {},
): RuntimeHookEventAnswer {
  return {
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
  };
}

function result(
  overrides: Partial<RuntimeHookEventAnswerResult> = {},
): RuntimeHookEventAnswerResult {
  return {
    runtime_id: "rt-1",
    provider: "claude",
    cached: false,
    observed_at: "2026-09-12T12:00:00Z",
    answer: answer(),
    ...overrides,
  };
}

describe("hookAnswerView", () => {
  it("claims nothing before a value is given", () => {
    const view = hookAnswerView(result(), false, null, "", null);
    expect(view.kind).toBe("idle");
    expect(view.answer).toBeNull();
  });

  it("claims nothing while a request is in flight", () => {
    // gcTime: 0 collects one macrotask late, so a previous event's answer can
    // briefly survive. isFetching is read first so it is never shown as the
    // answer to the current question.
    const view = hookAnswerView(result(), true, null, "Bash", null);
    expect(view.kind).toBe("asking");
    expect(view.answer).toBeNull();
  });

  it("reports a query failure rather than an empty answer", () => {
    const view = hookAnswerView(
      undefined,
      false,
      new Error("network down"),
      "Bash",
      null,
    );
    expect(view.kind).toBe("failed");
    expect(view.error).toBe("network down");
    expect(view.answer).toBeNull();
  });

  it("reports an unobserved runtime rather than an empty answer", () => {
    const view = hookAnswerView(
      result({ answer: undefined, observed_at: undefined, error: "no observation" }),
      false,
      null,
      "Bash",
      null,
    );
    expect(view.kind).toBe("unobserved");
    expect(view.answer).toBeNull();
    expect(view.error).toBe("no observation");
  });

  it("reports a dated payload with no answer as a failure, not as nothing running", () => {
    // The zod fallback shape: dated observation, refused payload.
    const view = hookAnswerView(
      result({ answer: undefined, error: "hook answer response did not match the expected shape" }),
      false,
      null,
      "Bash",
      null,
    );
    expect(view.kind).toBe("failed");
    expect(view.answer).toBeNull();
  });

  it("passes the server's unanswerable verdict through without inferring it", () => {
    const view = hookAnswerView(
      result({
        answer: answer({
          answerable: false,
          error: "compile claude matcher failed",
          unevaluable: [
            { hook_id: "sha256:1", matcher: "^(?!Notebook).*", error: "invalid or unsupported Perl syntax: `(?!`" },
          ],
        }),
      }),
      false,
      null,
      "Bash",
      null,
    );
    expect(view.kind).toBe("unanswerable");
    expect(view.error).toBe("compile claude matcher failed");
    expect(view.answer?.unevaluable?.[0]?.matcher).toBe("^(?!Notebook).*");
  });

  it("never derives answerability from an empty matched set", () => {
    // An answerable event with nothing matched is a real answer about this
    // value. It must not be downgraded to "cannot answer", and an
    // unanswerable event must not be upgraded by having empty sets.
    const empty = hookAnswerView(result(), false, null, "Bash", null);
    expect(empty.kind).toBe("answered");
    const unanswerable = hookAnswerView(
      result({ answer: answer({ answerable: false }) }),
      false,
      null,
      "Bash",
      null,
    );
    expect(unanswerable.kind).toBe("unanswerable");
  });

  it("treats a missing answerable flag as unanswerable", () => {
    // Explicit boolean check: a newer or malformed server value must not be
    // read as truthy and rendered as a real answer.
    const view = hookAnswerView(
      result({
        answer: { ...answer(), answerable: undefined as unknown as boolean },
      }),
      false,
      null,
      "Bash",
      null,
    );
    expect(view.kind).toBe("unanswerable");
  });

  it("treats an answer with no observation date as a refusal", () => {
    // Acceptance rule: nothing renders as "these fire" without a value and a
    // dated observation behind it. Today's server returns early with no
    // answer when it has no date, but an installed client meets newer
    // backends, so the undated body is refused here rather than trusted.
    const view = hookAnswerView(
      result({ observed_at: undefined, answer: answer({ matched: [entry()] }) }),
      false,
      null,
      "Bash",
      "2026-09-12T12:00:00Z",
    );
    expect(view.kind).toBe("failed");
    expect(view.answer).toBeNull();
    expect(view.observedAt).toBeNull();
  });

  it("does not borrow the tab's observation to date an undated answer", () => {
    const view = hookAnswerView(
      result({ observed_at: undefined }),
      false,
      null,
      "Bash",
      "2026-09-12T12:00:00Z",
    );
    expect(view.observedAt).toBeNull();
    expect(view.observationMismatch).toBe(false);
  });

  it("refuses an undated unanswerable body too", () => {
    // Both answer branches take the same date rule: unanswerable is still a
    // statement about a host state somebody read.
    const view = hookAnswerView(
      result({
        observed_at: undefined,
        answer: answer({ answerable: false, error: "compile claude matcher failed" }),
      }),
      false,
      null,
      "Bash",
      null,
    );
    expect(view.kind).toBe("failed");
    expect(view.error).toBe("compile claude matcher failed");
  });

  // LOCO-408 AC7/AC8: the undated guard was falsiness only, so any non-empty
  // string dated an answer. `Date.parse("not-a-date")` is NaN, and the panel
  // rendered `Observed at not-a-date` beside `Answered from the read NaN days
  // ago` plus a mismatch box quoting the garbage. An unparsable stamp is not a
  // weaker date than none, so it is refused exactly as an absent one is.
  //
  // The whole input space in one table, valid date last: it must still answer.
  //
  // `"0"` is the one row that answers rather than failing, and deliberately.
  // The LOCO-457 write-up grouped it with `not-a-date` and `"   "`, but
  // `Date.parse("0")` is 946684800000 — V8's legacy parser reads it as the
  // year 2000, so it is a date Multica can place and age, not a NaN. Refusing
  // it would take a second, stricter date rule than the one
  // `hookObservationView` applies, and the format of `observed_at` is the
  // boundary schema's business, not this derivation's.
  it.each([
    ["absent", undefined, "failed"],
    ["null", null, "failed"],
    ["empty", "", "failed"],
    ["blank", "   ", "failed"],
    ["unparsable", "not-a-date", "failed"],
    ["year-2000 numeric string", "0", "answered"],
    ["RFC3339", "2026-09-12T12:00:00Z", "answered"],
  ] as const)("handles %s observed_at (%s)", (_label, observedAt, kind) => {
    const view = hookAnswerView(
      result({
        // The schema types this optional-string, so null models a backend
        // that sends the field explicitly empty.
        observed_at: observedAt as string | undefined,
        answer: answer({ matched: [entry()] }),
      }),
      false,
      null,
      "Bash",
      "2026-09-12T12:00:00Z",
    );

    expect(view.kind).toBe(kind);
    if (kind === "answered") {
      expect(view.observedAt).toBe(observedAt);
      expect(view.answer?.matched).toHaveLength(1);
      return;
    }
    // No counts, no sets, no observation line, no mismatch box.
    expect(view.answer).toBeNull();
    expect(view.observedAt).toBeNull();
    expect(view.observationMismatch).toBe(false);
  });

  it("refuses an unparsable date on an unanswerable body too", () => {
    const view = hookAnswerView(
      result({
        observed_at: "not-a-date",
        answer: answer({ answerable: false, error: "compile claude matcher failed" }),
      }),
      false,
      null,
      "Bash",
      null,
    );

    expect(view.kind).toBe("failed");
    expect(view.observedAt).toBeNull();
  });

  it("carries the answer's own observation date and cached flag", () => {
    const view = hookAnswerView(
      result({ cached: true, observed_at: "2026-09-11T09:00:00Z" }),
      false,
      null,
      "Bash",
      null,
    );
    expect(view.observedAt).toBe("2026-09-11T09:00:00Z");
    expect(view.cached).toBe(true);
  });

  it("flags an answer computed from a different observation than the rows", () => {
    const view = hookAnswerView(
      result({ observed_at: "2026-09-12T12:00:00Z" }),
      false,
      null,
      "Bash",
      "2026-09-12T11:00:00Z",
    );
    expect(view.observationMismatch).toBe(true);
  });

  it("does not flag a mismatch when both observations are the same read", () => {
    const view = hookAnswerView(
      result({ observed_at: "2026-09-12T12:00:00Z" }),
      false,
      null,
      "Bash",
      "2026-09-12T12:00:00Z",
    );
    expect(view.observationMismatch).toBe(false);
  });
});

describe("hookAnswerTotals", () => {
  it("counts every entry exactly once across the four sets", () => {
    const totals = hookAnswerTotals(
      answer({
        matched: [entry({ hook_id: "a" })],
        not_matched: [entry({ hook_id: "b", matcher: "Write" })],
        never_runs: [
          entry({
            hook_id: "c",
            effectiveness: "never_runs",
            never_runs_reason: "matcher_ineligible",
          }),
        ],
        configuration_excluded: [entry({ hook_id: "d", configuration: "parked" })],
      }),
    );
    expect(totals.configured).toBe(4);
    expect(totals.runs).toBe(1);
    expect(totals.notMatched).toBe(1);
    expect(totals.neverRuns).toBe(1);
    expect(totals.excluded).toBe(1);
  });

  it("counts a parked entry that would also never run once, on the configuration axis", () => {
    // Both axes are still on the entry; only the count is single. Collapsing
    // them would lose that unparking it would not make it run.
    const parkedAndIneligible = entry({
      hook_id: "a",
      configuration: "parked",
      effectiveness: "never_runs",
      never_runs_reason: "matcher_ineligible",
    });
    const totals = hookAnswerTotals(
      answer({ configuration_excluded: [parkedAndIneligible] }),
    );
    expect(totals.configured).toBe(1);
    expect(totals.excluded).toBe(1);
    expect(totals.neverRuns).toBe(0);
    expect(totals.runs).toBe(0);
    expect(parkedAndIneligible.configuration).toBe("parked");
    expect(parkedAndIneligible.effectiveness).toBe("never_runs");
  });

  it("keeps a matched entry the provider skips out of the run count", () => {
    const totals = hookAnswerTotals(
      answer({
        matched: [
          entry({ hook_id: "a" }),
          entry({
            hook_id: "b",
            effectiveness: "never_runs",
            never_runs_reason: "handler_type_unsupported_by_provider",
          }),
        ],
      }),
    );
    expect(totals.runs).toBe(1);
    expect(totals.providerSkips).toBe(1);
    expect(totals.configured).toBe(2);
  });

  it("keeps a matched Codex entry with unconfirmed trust out of the run count", () => {
    const totals = hookAnswerTotals(
      answer({
        provider: "codex",
        matched: [
          entry({
            hook_id: "a",
            effectiveness: "trust_unknown",
            trust: "pending_review",
          }),
        ],
      }),
    );
    expect(totals.runs).toBe(0);
    expect(totals.trustUnconfirmed).toBe(1);
  });

  it("counts a cross-source collapse as one entry with two sources", () => {
    const totals = hookAnswerTotals(
      answer({
        matched: [
          entry({
            hook_id: "a",
            sources: [
              { scope: "user", format: "json", kind: "settings" },
              { scope: "project", format: "json", kind: "settings" },
            ],
          }),
        ],
      }),
    );
    expect(totals.configured).toBe(1);
    expect(totals.runs).toBe(1);
    expect(totals.collapsed).toBe(1);
  });

  it("counts the definitions absorbed, not the rows that absorbed them", () => {
    // Three byte-identical copies are one row carrying three sources, and two
    // definitions were folded away. Two is what the chip says.
    const totals = hookAnswerTotals(
      answer({
        matched: [
          entry({
            hook_id: "a",
            sources: [
              { scope: "user", format: "json", kind: "settings" },
              { scope: "project", format: "json", kind: "settings" },
              { scope: "local", format: "json", kind: "settings" },
            ],
          }),
          entry({ hook_id: "b", matcher: "Write" }),
        ],
      }),
    );
    expect(totals.configured).toBe(2);
    expect(totals.collapsed).toBe(2);
  });

  it("is all zeroes for a null answer rather than throwing", () => {
    expect(hookAnswerTotals(null).configured).toBe(0);
    expect(hookAnswerTotals(null).runs).toBe(0);
  });
});

describe("hookAnsweredEvents", () => {
  it("lists the configured events once each, sorted", () => {
    expect(
      hookAnsweredEvents([
        entry({ hook_id: "a", event: "Stop" }),
        entry({ hook_id: "b", event: "PreToolUse" }),
        entry({ hook_id: "c", event: "PreToolUse" }),
      ]),
    ).toEqual(["PreToolUse", "Stop"]);
  });

  it("is empty when nothing is configured", () => {
    expect(hookAnsweredEvents([])).toEqual([]);
  });
});
