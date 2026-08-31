"use client";

import { useWorkspaceId } from "@multica/core/hooks";
import { usePlanOverview } from "@multica/core/project-plan/hooks";
import type { PlanViewMode } from "../types";
import { PlanErrorState, PlanLoadingState, PlanNoPlanState } from "./plan-states";
import { PlanDocumentPane } from "./plan-document-pane";
import { PlanPipelinePane } from "./plan-pipeline-pane";
import { PlanCoveragePane } from "./plan-coverage-pane";

/**
 * Resolves one of the three Plan view modes into exactly one of four states —
 * loading, error, no-plan, or the pane itself wired to live data. Collapsing
 * error into no-plan (or vice versa) is the bug this component exists to
 * prevent: a project with a plan must never render "no plan" just because
 * the request hasn't resolved yet or failed (LOCO-549 Addition 2).
 */
export function PlanModePane({ mode, projectId }: { mode: PlanViewMode; projectId: string }) {
  const wsId = useWorkspaceId();
  const state = usePlanOverview(wsId, projectId);

  switch (state.status) {
    case "loading":
      return <PlanLoadingState />;
    case "error":
      return <PlanErrorState onRetry={state.retry} />;
    case "no-plan":
      return <PlanNoPlanState />;
    case "present":
      switch (mode) {
        case "plan_document":
          return <PlanDocumentPane overview={state.data} />;
        case "plan_pipeline":
          return <PlanPipelinePane overview={state.data} />;
        case "plan_coverage":
          return <PlanCoveragePane overview={state.data} />;
      }
  }
}
