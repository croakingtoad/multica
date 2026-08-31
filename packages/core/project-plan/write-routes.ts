/**
 * THE one place any project-plan write URL or HTTP verb is written down.
 *
 * Slice A (LOCO-584) owns the server routes these describe and had not landed
 * its route table when this authoring UI was built, so the paths below are the
 * only part of the plan-write contract that is still provisional. Everything
 * else — argument shapes, domain errors, which operations exist — comes from
 * the audited service (`server/internal/projectplan/service.go`) and from
 * `server/internal/projectplan/errors.go`.
 *
 * When Slice A publishes its real route table, edit THIS FILE and nothing
 * else. `ApiClient`'s plan-write methods hold no path literals; the mutation
 * hooks and every dialog reach the API only through those methods. Correcting
 * a path here propagates everywhere with no other change.
 *
 * The paths mirror the read routes that already exist
 * (`server/cmd/server/router.go:1918-1919`: `GET /api/projects/{id}/plan`,
 * `GET /api/projects/{id}/plans/{planId}`) and the conventional REST nesting
 * the rest of the router uses for project sub-resources.
 */

/** HTTP verb per operation — provisional alongside the paths. */
export const PLAN_WRITE_METHODS = {
  createPlan: "POST",
  updatePlan: "PATCH",
  supersedePlan: "POST",
  deletePlan: "DELETE",
  addPhase: "POST",
  updatePhase: "PATCH",
  reorderPhases: "POST",
  deletePhase: "DELETE",
  addPart: "POST",
  updatePart: "PATCH",
  reorderParts: "POST",
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

  /** `Service.LinkIssue` */
  linkIssue: (projectId: string, planId: string, partId: string) =>
    `${plan(projectId, planId)}/parts/${partId}/issues`,
  /** `Service.UnlinkIssue` */
  unlinkIssue: (projectId: string, planId: string, partId: string, issueId: string) =>
    `${plan(projectId, planId)}/parts/${partId}/issues/${issueId}`,
} as const;
