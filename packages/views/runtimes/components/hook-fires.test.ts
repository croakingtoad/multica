// @vitest-environment node
import { describe, it, expect } from "vitest";
import type { RuntimeHookFire } from "@multica/core/types";
import {
  countProvenance,
  exitCodeOf,
  hookNameOf,
  outcomeKind,
  provenanceKind,
  providerOutcomeOf,
  unknownOutcomeCause,
} from "./hook-fires";

// Canonical matrix for the hook-fire presentation helpers. The render suite
// (hook-fires-section.test.tsx) keeps the on-screen contract and does not
// re-run this matrix through a DOM mount.

function fire(overrides: Partial<RuntimeHookFire> = {}): RuntimeHookFire {
  return {
    id: "f-1",
    provider: "claude",
    event: "PreToolUse",
    execution_id: "exec-1",
    hook_spec: { hook_name: "my-hook", type: "claude_hook_response" },
    fired_at: "2026-09-12T10:00:00Z",
    provenance: "inferred",
    outcome: "success",
    detail: { debug_outcome: "success" },
    ...overrides,
  };
}

describe("provenanceKind", () => {
  it("classifies the two stored values by exact equality", () => {
    expect(provenanceKind("debug_log")).toBe("debug_log");
    expect(provenanceKind("inferred")).toBe("inferred");
  });

  // This is the load-bearing case. DP-LOCO-114-03 condition 2 forbids
  // provenance computation at read time, so an unknown value must NOT be
  // narrowed into a known member — not even into `inferred`, the weaker of
  // the two. Asserting `inferred` about a value we do not understand is still
  // an assertion we have no basis for.
  it.each([
    "",
    "debug",
    "debug_log ",
    "DEBUG_LOG",
    "debug_log_match",
    "inferred_from_debug_log",
    "observed",
    "host",
  ])("never narrows the unrecognised value %o into a known member", (value) => {
    expect(provenanceKind(value)).toBe("unrecognized");
  });
});

describe("outcomeKind", () => {
  it.each(["success", "failure", "blocked", "skipped", "unknown"])(
    "passes the stored outcome %s through",
    (value) => {
      expect(outcomeKind(value)).toBe(value);
    },
  );

  it.each(["", "cancelled", "SUCCESS", "error"])(
    "does not narrow the unrecognised outcome %o",
    (value) => {
      expect(outcomeKind(value)).toBe("unrecognized");
    },
  );
});

describe("unknownOutcomeCause", () => {
  // Mirrors hookFireOutcome in server/internal/daemon/hook_fire.go: the
  // provider said `error` and the exit code was 126 or 127, so a hook that
  // ran and failed cannot be told apart from a shell that never started it.
  it.each([126, 127])("reads exit %i as spawn-ambiguous", (code) => {
    expect(
      unknownOutcomeCause(
        fire({ outcome: "unknown", detail: { debug_outcome: "error", exit_code: code } }),
      ),
    ).toBe("spawn_ambiguous");
  });

  it("reads a provider error with no exit code as no_exit_code", () => {
    expect(
      unknownOutcomeCause(
        fire({ outcome: "unknown", detail: { debug_outcome: "error" } }),
      ),
    ).toBe("no_exit_code");
  });

  // The second real cause: a genuine provider `cancelled` (3 of 144 in the
  // measured corpus, from a hook that timed out) also lands in `unknown`.
  it("reads a non-success provider state as provider_state", () => {
    expect(
      unknownOutcomeCause(
        fire({ outcome: "unknown", detail: { debug_outcome: "cancelled" } }),
      ),
    ).toBe("provider_state");
    expect(
      providerOutcomeOf(fire({ detail: { debug_outcome: "cancelled" } })),
    ).toBe("cancelled");
  });

  it("falls back to unclassified when detail says nothing usable", () => {
    expect(unknownOutcomeCause(fire({ outcome: "unknown", detail: {} }))).toBe(
      "unclassified",
    );
    expect(
      unknownOutcomeCause(fire({ outcome: "unknown", detail: undefined })),
    ).toBe("unclassified");
  });

  it("does not treat an ordinary error exit code as spawn-ambiguous", () => {
    expect(
      unknownOutcomeCause(
        fire({ outcome: "unknown", detail: { debug_outcome: "error", exit_code: 1 } }),
      ),
    ).toBe("unclassified");
  });
});

describe("exitCodeOf", () => {
  it("returns the stored numeric exit code", () => {
    expect(exitCodeOf(fire({ detail: { exit_code: 0 } }))).toBe(0);
    expect(exitCodeOf(fire({ detail: { exit_code: 127 } }))).toBe(127);
  });

  it("returns null rather than coercing a non-numeric or absent code", () => {
    expect(exitCodeOf(fire({ detail: { exit_code: "127" } }))).toBeNull();
    expect(exitCodeOf(fire({ detail: {} }))).toBeNull();
    expect(exitCodeOf(fire({ detail: undefined }))).toBeNull();
  });
});

describe("hookNameOf", () => {
  it("reads the handler identity observed at fire time", () => {
    expect(hookNameOf(fire())).toBe("my-hook");
  });

  // A fire whose handler Multica never saw must not borrow an identity from
  // the event or from execution_id — execution_id is a per-execution
  // reference and presenting it as "this hook" is exactly what
  // DP-LOCO-114-02 item 3 forecloses.
  it("returns null instead of substituting another field", () => {
    expect(hookNameOf(fire({ hook_spec: {} }))).toBeNull();
    expect(hookNameOf(fire({ hook_spec: undefined }))).toBeNull();
    expect(hookNameOf(fire({ hook_spec: [] }))).toBeNull();
    expect(hookNameOf(fire({ hook_spec: { hook_name: "" } }))).toBeNull();
    expect(hookNameOf(fire({ hook_spec: { hook_name: 7 } }))).toBeNull();
  });
});

describe("countProvenance", () => {
  it("tallies each row into the bucket its own stored column names", () => {
    const counts = countProvenance([
      fire({ id: "a", provenance: "inferred" }),
      fire({ id: "b", provenance: "inferred" }),
      fire({ id: "c", provenance: "debug_log" }),
      fire({ id: "d", provenance: "who-knows" }),
    ]);
    expect(counts).toEqual({ debug_log: 1, inferred: 2, unrecognized: 1 });
  });

  it("counts nothing for an empty feed", () => {
    expect(countProvenance([])).toEqual({
      debug_log: 0,
      inferred: 0,
      unrecognized: 0,
    });
  });
});
