/**
 * THE one place any project-plan write URL or HTTP verb is written down.
 *
 * These are Slice A's (LOCO-584) real routes, as reported on LOCO-591 and
 * implemented in `8472c39bf` on `agent/senior-developer/01a058ec`. The rest of
 * the contract — argument shapes, domain errors, which operations exist —
 * comes from the audited service (`server/internal/projectplan/service.go`)
 * and `server/internal/projectplan/errors.go`.
 *
 * Slice A is still `in_review` with its Tier 1 audit open, so a path could yet
 * move. That is exactly why this file exists: `ApiClient`'s plan-write methods
 * hold no path literals, and the mutation hooks and every dialog reach the API
 * only through those methods. Correcting a route here propagates everywhere
 * with no other change.
 *
 * Every route is gated on `project_plans` and answers 404 when the flag is
 * off — not 401/403/500. See `classifyPlanWriteError` for why the 404 copy has
 * to read for both "this item is gone" and "plans are off here".
 *
 * `CreateFromIssue` and `DeleteImpact` are part of Slice A's table but are not
 * listed below: issue-sourced plan creation is a later phase, and this slice's
 * delete confirmations state the consequence in words rather than fetching a
 * count. Adding either means adding its route here.
 */

/** HTTP verb per operation. Both reorders are PATCH, not POST. */
export const PLAN_WRITE_METHODS = {
  createPlan: "POST",
  updatePlan: "PATCH",
  supersedePlan: "POST",
  deletePlan: "DELETE",
  addPhase: "POST",
  updatePhase: "PATCH",
  reorderPhases: "PATCH",
  deletePhase: "DELETE",
  addPart: "POST",
  updatePart: "PATCH",
  reorderParts: "PATCH",
  deletePart: "DELETE",
  linkIssue: "POST",
  unlinkIssue: "DELETE",
} as const;

const plans = (projectId: string) => `/api/projects/${projectId}/plans`;
const plan = (projectId: string, planId: string) => `${plans(projectId)}/${planId}`;

export const planWriteRoutes = {
  /** `Service.CreateManual` */
  createPlan: (projectId: string) => plans(projectId),
  /** `Service.UpdatePlan` */
  updatePlan: (projectId: string, planId: string) => plan(projectId, planId),
  /** `Service.Supersede` */
  supersedePlan: (projectId: string, planId: string) => `${plan(projectId, planId)}/supersede`,
  /** `Service.DeletePlan` */
  deletePlan: (projectId: string, planId: string) => plan(projectId, planId),

  /** `Service.AddPhase` */
  addPhase: (projectId: string, planId: string) => `${plan(projectId, planId)}/phases`,
  /** `Service.UpdatePhase` */
  updatePhase: (projectId: string, planId: string, phaseId: string) =>
    `${plan(projectId, planId)}/phases/${phaseId}`,
  /** `Service.ReorderPhases` */
  reorderPhases: (projectId: string, planId: string) =>
    `${plan(projectId, planId)}/phases/reorder`,
  /** `Service.DeletePhase` */
  deletePhase: (projectId: string, planId: string, phaseId: string) =>
    `${plan(projectId, planId)}/phases/${phaseId}`,

  /** `Service.AddPart` */
  addPart: (projectId: string, planId: string, phaseId: string) =>
    `${plan(projectId, planId)}/phases/${phaseId}/parts`,
  /** `Service.UpdatePart` */
  updatePart: (projectId: string, planId: string, partId: string) =>
    `${plan(projectId, planId)}/parts/${partId}`,
  /** `Service.ReorderParts` — scoped to one phase; the service reorders that phase's parts only. */
  reorderParts: (projectId: string, planId: string, phaseId: string) =>
    `${plan(projectId, planId)}/phases/${phaseId}/parts/reorder`,
  /** `Service.DeletePart` */
  deletePart: (projectId: string, planId: string, partId: string) =>
    `${plan(projectId, planId)}/parts/${partId}`,

  /** `Service.LinkIssue` — the issue id is in the path; there is no request body. */
  linkIssue: (projectId: string, planId: string, partId: string, issueId: string) =>
    `${plan(projectId, planId)}/parts/${partId}/issues/${issueId}`,
  /** `Service.UnlinkIssue` */
  unlinkIssue: (projectId: string, planId: string, partId: string, issueId: string) =>
    `${plan(projectId, planId)}/parts/${partId}/issues/${issueId}`,
} as const;
