// @vitest-environment node

/**
 * Pins the one provisional part of the plan-write contract: the paths.
 *
 * Slice A (LOCO-584) owns the real routes. These assertions are not a claim
 * that the server serves these paths — they exist so that re-pointing this
 * file at Slice A's route table is a visible, reviewed edit rather than a
 * silent drift, and so the nesting stays internally consistent (a part path is
 * always under its plan, a plan path always under its project).
 */

import { describe, expect, it } from "vitest";
import { PLAN_WRITE_METHODS, planWriteRoutes } from "./write-routes";

const P = "proj-1";
const PLAN = "plan-1";

describe("planWriteRoutes", () => {
  it("nests plan paths under the project, matching the read routes", () => {
    expect(planWriteRoutes.createPlan(P)).toBe("/api/projects/proj-1/plans");
    expect(planWriteRoutes.updatePlan(P, PLAN)).toBe("/api/projects/proj-1/plans/plan-1");
    expect(planWriteRoutes.deletePlan(P, PLAN)).toBe("/api/projects/proj-1/plans/plan-1");
    expect(planWriteRoutes.supersedePlan(P, PLAN)).toBe(
      "/api/projects/proj-1/plans/plan-1/supersede",
    );
  });

  it("nests phase paths under the plan", () => {
    expect(planWriteRoutes.addPhase(P, PLAN)).toBe("/api/projects/proj-1/plans/plan-1/phases");
    expect(planWriteRoutes.updatePhase(P, PLAN, "ph-1")).toBe(
      "/api/projects/proj-1/plans/plan-1/phases/ph-1",
    );
    expect(planWriteRoutes.deletePhase(P, PLAN, "ph-1")).toBe(
      "/api/projects/proj-1/plans/plan-1/phases/ph-1",
    );
    expect(planWriteRoutes.reorderPhases(P, PLAN)).toBe(
      "/api/projects/proj-1/plans/plan-1/phases/reorder",
    );
  });

  it("creates a part under its phase but addresses an existing one by id", () => {
    // `AddPart` needs the phase (it validates the phase belongs to the plan);
    // `UpdatePart` / `DeletePart` take only plan + part.
    expect(planWriteRoutes.addPart(P, PLAN, "ph-1")).toBe(
      "/api/projects/proj-1/plans/plan-1/phases/ph-1/parts",
    );
    expect(planWriteRoutes.updatePart(P, PLAN, "pt-1")).toBe(
      "/api/projects/proj-1/plans/plan-1/parts/pt-1",
    );
    expect(planWriteRoutes.reorderParts(P, PLAN, "ph-1")).toBe(
      "/api/projects/proj-1/plans/plan-1/phases/ph-1/parts/reorder",
    );
  });

  it("scopes issue links to a part", () => {
    expect(planWriteRoutes.linkIssue(P, PLAN, "pt-1")).toBe(
      "/api/projects/proj-1/plans/plan-1/parts/pt-1/issues",
    );
    expect(planWriteRoutes.unlinkIssue(P, PLAN, "pt-1", "iss-1")).toBe(
      "/api/projects/proj-1/plans/plan-1/parts/pt-1/issues/iss-1",
    );
  });

  it("declares a verb for every route", () => {
    expect(Object.keys(PLAN_WRITE_METHODS).sort()).toEqual(Object.keys(planWriteRoutes).sort());
  });

  it("uses a non-mutating verb for nothing", () => {
    expect(Object.values(PLAN_WRITE_METHODS)).not.toContain("GET");
  });
});
