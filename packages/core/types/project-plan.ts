// A project's decomposed plan: phases -> parts -> issues, plus the
// dependency edges and rollups the three Plan views (Document, Pipeline,
// Coverage) render. Mirrors `projectplan.Overview` in
// server/internal/projectplan/read.go — every number here comes from that
// read model, never computed or guessed client-side.

/** Coverage state of one plan part, computed server-side from its linked issues. */
export type ProjectPlanCoverageState =
  | "no_tasks_yet"
  | "covered_no_active_tasks"
  | "not_started"
  | "in_progress"
  | "complete";

export interface ProjectPlan {
  id: string;
  workspace_id: string;
  project_id: string;
  version: number;
  kind: string;
  origin: string;
  title: string;
  description: string;
  attributes: unknown;
  source_issue_id: string | null;
  superseded: boolean;
  superseded_at: string | null;
  created_by_type: string;
  created_by_id: string;
  created_at: string;
  updated_at: string;
}

export interface ProjectPlanRollup {
  tasks_done: number;
  tasks_total: number;
  percent: number;
  parts_covered: number;
  parts_total: number;
  parts_without_tasks: number;
}

export interface ProjectPlanTaskRollup {
  tasks_done: number;
  tasks_total: number;
  percent: number;
}

export interface ProjectPlanIssueDetail {
  id: string | null;
  number: number;
  identifier: string;
  title: string;
  status: string;
  status_category: string;
  assignee_type: string | null;
  assignee_id: string | null;
  deleted: boolean;
}

export interface ProjectPlanPart {
  id: string;
  title: string;
  description: string;
  acceptance_criteria: string;
  attributes: unknown;
  position: number;
  coverage_state: ProjectPlanCoverageState;
  rollup: ProjectPlanTaskRollup;
  issues: ProjectPlanIssueDetail[];
  created_at: string;
  updated_at: string;
}

export interface ProjectPlanPhase {
  id: string;
  title: string;
  description: string;
  attributes: unknown;
  position: number;
  rollup: ProjectPlanTaskRollup;
  parts: ProjectPlanPart[];
  created_at: string;
  updated_at: string;
}

export interface ProjectPlanDependencyNode {
  type: string;
  id: string;
  title: string;
  phase_id?: string | null;
  phase_title?: string | null;
  missing: boolean;
}

export interface ProjectPlanDependency {
  id: string;
  blocked: ProjectPlanDependencyNode;
  blocking: ProjectPlanDependencyNode;
}

export interface ProjectPlanUncoveredPart {
  id: string;
  title: string;
  position: number;
  phase_id: string;
  phase_title: string;
}

export interface ProjectPlanOverview {
  plan: ProjectPlan;
  rollup: ProjectPlanRollup;
  phases: ProjectPlanPhase[];
  dependencies: ProjectPlanDependency[];
  uncovered_parts: ProjectPlanUncoveredPart[];
}
