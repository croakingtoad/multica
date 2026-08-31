"use client";

import { createContext, useContext, useMemo, useState, type ReactNode } from "react";
import type { ProjectPlanOverview, ProjectPlanPart, ProjectPlanPhase } from "@multica/core/types";

/**
 * Which authoring dialog is open, and what it is acting on. One discriminated
 * union rather than a boolean per dialog: two dialogs can never be open at
 * once, and a target (phase / part) can never be missing for the dialog that
 * needs it.
 */
export type PlanAuthoringDialog =
  | { kind: "create-plan" }
  | { kind: "edit-plan" }
  | { kind: "supersede-plan" }
  | { kind: "delete-plan" }
  | { kind: "add-phase" }
  | { kind: "edit-phase"; phase: ProjectPlanPhase }
  | { kind: "delete-phase"; phase: ProjectPlanPhase }
  | { kind: "add-part"; phase: ProjectPlanPhase }
  | { kind: "edit-part"; phase: ProjectPlanPhase; part: ProjectPlanPart }
  | { kind: "delete-part"; phase: ProjectPlanPhase; part: ProjectPlanPart }
  | { kind: "link-issues"; phase: ProjectPlanPhase; part: ProjectPlanPart };

interface PlanAuthoringValue {
  /**
   * False whenever authoring must not be reachable — the `project_plans` flag
   * being off, or a superseded (read-only) plan version being displayed. Every
   * affordance checks this; nothing renders an edit control it would then have
   * to refuse.
   */
  enabled: boolean;
  projectId: string;
  /** The plan being authored, or null on a project that has none yet. */
  overview: ProjectPlanOverview | null;
  dialog: PlanAuthoringDialog | null;
  open: (dialog: PlanAuthoringDialog) => void;
  close: () => void;
}

const noop = () => {};

const PlanAuthoringContext = createContext<PlanAuthoringValue>({
  enabled: false,
  projectId: "",
  overview: null,
  dialog: null,
  open: noop,
  close: noop,
});

export function PlanAuthoringProvider({
  enabled,
  projectId,
  overview,
  children,
}: {
  enabled: boolean;
  projectId: string;
  overview: ProjectPlanOverview | null;
  children: ReactNode;
}) {
  const [dialog, setDialog] = useState<PlanAuthoringDialog | null>(null);
  const value = useMemo<PlanAuthoringValue>(
    () => ({
      enabled,
      projectId,
      overview,
      // A disabled provider reports no open dialog and ignores `open`, so a
      // stale affordance rendered by mistake still cannot start a write.
      dialog: enabled ? dialog : null,
      open: enabled ? setDialog : noop,
      close: () => setDialog(null),
    }),
    [enabled, projectId, overview, dialog],
  );
  return <PlanAuthoringContext.Provider value={value}>{children}</PlanAuthoringContext.Provider>;
}

export function usePlanAuthoring(): PlanAuthoringValue {
  return useContext(PlanAuthoringContext);
}

/**
 * The plan id every write needs. Null when there is no plan to write to —
 * callers that reach a mutation without one have a bug, not a state to render.
 */
export function usePlanAuthoringTarget(): { planId: string; projectId: string } | null {
  const { overview, projectId, enabled } = usePlanAuthoring();
  return useMemo(
    () => (enabled && overview ? { planId: overview.plan.id, projectId } : null),
    [enabled, overview, projectId],
  );
}
