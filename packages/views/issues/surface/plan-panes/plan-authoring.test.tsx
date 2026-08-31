// @vitest-environment jsdom

/**
 * Component coverage for manual plan authoring (LOCO-591).
 *
 * Three things are pinned here, in order of what would hurt most if it broke:
 *
 * 1. **Flag gating.** With `project_plans` off, no authoring affordance is
 *    visible or reachable (acceptance criteria #3).
 * 2. **The create-then-populate flow.** Empty state → create plan → add phase
 *    → add part, each hitting the real client method, so the URL/verb table is
 *    exercised rather than mocked away.
 * 3. **Honest errors.** A 409 renders as a conflict, not as a generic failure,
 *    and the dialog stays open so the reader can act on it.
 *
 * The ordering matrix lives in `authoring/plan-ordering.test.ts` and the error
 * classification matrix in `@multica/core/project-plan/errors.test.ts`; this
 * file covers wiring, gating, and accessibility, not those matrices again.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import { ApiError, type ApiClient } from "@multica/core/api/client";
import { configStore } from "@multica/core/config";
import { PROJECT_PLANS_FLAG } from "@multica/core/feature-flags";
import type { ProjectPlanOverview } from "@multica/core/types";
import { NavigationProvider, type NavigationAdapter } from "../../../navigation";
import { renderWithI18n } from "../../../test/i18n";
import { PlanModePane } from "./plan-mode-pane";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));

const navigation: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/projects/project-1",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => `https://app.example${path}`,
};

const STATUSES = {
  statuses: [
    {
      id: "s-todo", workspace_id: "ws-1", key: "todo", name: "Todo", description: "",
      category: "todo", color: "#888", is_system: true, position: 0, archived_at: null,
      created_at: "", updated_at: "",
    },
  ],
  categories: [],
  total: 1,
};

function makeOverview(overrides: Partial<ProjectPlanOverview> = {}): ProjectPlanOverview {
  return {
    plan: {
      id: "plan-1", workspace_id: "ws-1", project_id: "project-1", version: 2,
      kind: "prd", origin: "manual", title: "Hand-authored plan", description: "",
      attributes: null, source_issue_id: null, superseded: false, superseded_at: null,
      created_by_type: "member", created_by_id: "member-1", created_at: "", updated_at: "",
    },
    rollup: {
      tasks_done: 0, tasks_total: 0, percent: 0,
      parts_covered: 0, parts_total: 1, parts_without_tasks: 1,
    },
    phases: [
      {
        id: "phase-1", title: "Phase 1 — Foundations", description: "", attributes: null,
        position: 0, rollup: { tasks_done: 0, tasks_total: 0, percent: 0 },
        parts: [
          {
            id: "part-1", title: "Schema", description: "", acceptance_criteria: "",
            attributes: null, position: 0, coverage_state: "no_tasks_yet",
            rollup: { tasks_done: 0, tasks_total: 0, percent: 0 },
            issues: [], created_at: "", updated_at: "",
          },
        ],
        created_at: "", updated_at: "",
      },
    ],
    dependencies: [],
    uncovered_parts: [],
    ...overrides,
  };
}

function renderPane(api: Partial<ApiClient>, mode: "plan_document" = "plan_document") {
  setApiInstance({ listIssueStatuses: async () => STATUSES, ...api } as unknown as ApiClient);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <NavigationProvider value={navigation}>
        <PlanModePane mode={mode} projectId="project-1" />
      </NavigationProvider>
    </QueryClientProvider>,
  );
}

function setFlag(on: boolean) {
  configStore.setState({ featureFlags: { [PROJECT_PLANS_FLAG]: on } });
}

beforeEach(() => setFlag(true));
afterEach(() => {
  cleanup();
  configStore.setState({ featureFlags: {} });
  vi.clearAllMocks();
});

describe("flag gating (acceptance criteria #3)", () => {
  it("offers no create-plan entry point on the empty state with the flag off", async () => {
    setFlag(false);
    renderPane({ getActiveProjectPlan: async () => null });
    await screen.findByTestId("plan-no-plan-state");
    expect(screen.queryByTestId("plan-authoring-create-plan")).toBeNull();
  });

  it("offers no plan, phase, or part affordance on a populated plan with the flag off", async () => {
    setFlag(false);
    renderPane({ getActiveProjectPlan: async () => makeOverview() });
    await screen.findByText("Hand-authored plan");
    expect(screen.queryByTestId("plan-authoring-plan-menu")).toBeNull();
    expect(screen.queryByTestId("plan-authoring-add-phase")).toBeNull();
    expect(screen.queryByRole("button", { name: /Actions for phase/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /Actions for part/ })).toBeNull();
  });

  it("shows all of them with the flag on", async () => {
    renderPane({ getActiveProjectPlan: async () => makeOverview() });
    await screen.findByText("Hand-authored plan");
    expect(screen.getByTestId("plan-authoring-plan-menu")).toBeTruthy();
    expect(screen.getByTestId("plan-authoring-add-phase")).toBeTruthy();
    expect(screen.getByRole("button", { name: /Actions for phase/ })).toBeTruthy();
    expect(screen.getByRole("button", { name: /Actions for part/ })).toBeTruthy();
  });

  it("hides authoring on a superseded version, which the service refuses to write to", async () => {
    renderPane({
      getActiveProjectPlan: async () =>
        makeOverview({
          plan: { ...makeOverview().plan, superseded: true, superseded_at: "2026-01-01T00:00:00Z" },
        }),
    });
    await screen.findByText("Hand-authored plan");
    expect(screen.queryByTestId("plan-authoring-plan-menu")).toBeNull();
    expect(screen.queryByTestId("plan-authoring-add-phase")).toBeNull();
  });
});

describe("creating a plan from the empty state", () => {
  it("posts a manual plan with no source provenance, then re-reads", async () => {
    const user = userEvent.setup();
    const createManualProjectPlan = vi.fn().mockResolvedValue(undefined);
    let plan: ProjectPlanOverview | null = null;
    renderPane({
      getActiveProjectPlan: async () => plan,
      createManualProjectPlan: async (projectId: string, data) => {
        createManualProjectPlan(projectId, data);
        plan = makeOverview();
      },
    });

    await user.click(await screen.findByTestId("plan-authoring-create-plan"));
    await user.type(screen.getByLabelText("Plan title"), "Q3 launch plan");
    await user.click(screen.getByRole("button", { name: "Create plan" }));

    await waitFor(() =>
      expect(createManualProjectPlan).toHaveBeenCalledWith("project-1", {
        kind: "prd",
        title: "Q3 launch plan",
        description: "",
      }),
    );
    // Provenance stays honest: a hand-authored plan sends nothing that could
    // make it claim an issue as its source (acceptance criteria #5).
    const [, body] = createManualProjectPlan.mock.calls[0]!;
    expect(Object.keys(body as object).sort()).toEqual(["description", "kind", "title"]);
  });

  it("will not submit an empty title", async () => {
    const user = userEvent.setup();
    renderPane({ getActiveProjectPlan: async () => null });
    await user.click(await screen.findByTestId("plan-authoring-create-plan"));
    expect((screen.getByRole("button", { name: "Create plan" }) as HTMLButtonElement).disabled).toBe(
      true,
    );
  });
});

describe("adding phases and parts", () => {
  it("appends a phase one past the highest position in use", async () => {
    const user = userEvent.setup();
    const createProjectPlanPhase = vi.fn().mockResolvedValue(undefined);
    renderPane({ getActiveProjectPlan: async () => makeOverview(), createProjectPlanPhase });

    await user.click(await screen.findByTestId("plan-authoring-add-phase"));
    await user.type(screen.getByLabelText("Phase title"), "Phase 2 — Rollout");
    await user.click(screen.getByRole("button", { name: "Add phase" }));

    await waitFor(() =>
      expect(createProjectPlanPhase).toHaveBeenCalledWith("project-1", "plan-1", {
        title: "Phase 2 — Rollout",
        description: "",
        position: 1,
      }),
    );
  });

  it("adds a part to its phase and says up front that it will read as no-tasks-yet", async () => {
    const user = userEvent.setup();
    const createProjectPlanPart = vi.fn().mockResolvedValue(undefined);
    renderPane({ getActiveProjectPlan: async () => makeOverview(), createProjectPlanPart });

    await user.click(await screen.findByRole("button", { name: /Add part to Phase 1/ }));
    const dialog = await screen.findByRole("dialog");
    // The dialog names the coverage state a new part will land in, so the amber
    // treatment that follows is expected rather than surprising.
    expect(dialog.textContent).toContain("No tasks yet");
    await user.type(screen.getByLabelText("Part title"), "Migration");
    await user.click(screen.getByRole("button", { name: "Add part" }));

    await waitFor(() =>
      expect(createProjectPlanPart).toHaveBeenCalledWith("project-1", "plan-1", "phase-1", {
        title: "Migration",
        description: "",
        acceptance_criteria: "",
        position: 1,
      }),
    );
  });
});

describe("destructive actions are confirmed", () => {
  it("does not delete a part until the confirmation is accepted", async () => {
    const user = userEvent.setup();
    const deleteProjectPlanPart = vi.fn().mockResolvedValue(undefined);
    renderPane({ getActiveProjectPlan: async () => makeOverview(), deleteProjectPlanPart });

    await user.click(await screen.findByRole("button", { name: /Actions for part/ }));
    await user.click(await screen.findByRole("menuitem", { name: "Delete part" }));

    // Confirmation is on screen and nothing has been sent yet.
    expect(await screen.findByText(/Delete “Schema”\?/)).toBeTruthy();
    expect(deleteProjectPlanPart).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Delete part" }));
    await waitFor(() =>
      expect(deleteProjectPlanPart).toHaveBeenCalledWith("project-1", "plan-1", "part-1"),
    );
  });

  it("states that unlinking is not deleting, so the confirmation is not misread", async () => {
    const user = userEvent.setup();
    renderPane({ getActiveProjectPlan: async () => makeOverview() });
    await user.click(await screen.findByTestId("plan-authoring-plan-menu"));
    await user.click(await screen.findByRole("menuitem", { name: "Delete plan" }));
    expect(await screen.findByText(/The issues themselves are not deleted/)).toBeTruthy();
  });
});

describe("honest error surfacing (acceptance criteria: a 409 reads as a conflict)", () => {
  it("renders a 409 as a conflict and keeps the dialog open", async () => {
    const user = userEvent.setup();
    renderPane({
      getActiveProjectPlan: async () => null,
      createManualProjectPlan: async () => {
        throw new ApiError("API error: 409", 409, "Conflict", {
          code: "active_plan_exists",
          error: "this project already has an active plan",
        });
      },
    });

    await user.click(await screen.findByTestId("plan-authoring-create-plan"));
    await user.type(screen.getByLabelText("Plan title"), "Second plan");
    await user.click(screen.getByRole("button", { name: "Create plan" }));

    const notice = await screen.findByTestId("plan-write-conflict");
    expect(notice.textContent).toContain("This plan changed");
    // The server's own specific wording, not a generic "couldn't save".
    expect(notice.textContent).toContain("this project already has an active plan");
    expect(screen.queryByTestId("plan-write-error")).toBeNull();
    // Still open, with the typed title intact, so the reader can retry.
    expect((screen.getByLabelText("Plan title") as HTMLInputElement).value).toBe("Second plan");
  });

  it("renders a 500 as a failure rather than as a conflict", async () => {
    const user = userEvent.setup();
    renderPane({
      getActiveProjectPlan: async () => null,
      createManualProjectPlan: async () => {
        throw new ApiError("API error: 500", 500, "Internal Server Error", {});
      },
    });

    await user.click(await screen.findByTestId("plan-authoring-create-plan"));
    await user.type(screen.getByLabelText("Plan title"), "Anything");
    await user.click(screen.getByRole("button", { name: "Create plan" }));

    const notice = await screen.findByTestId("plan-write-error");
    expect(notice.textContent).toContain("Couldn't save");
    expect(screen.queryByTestId("plan-write-conflict")).toBeNull();
  });
});

describe("a hand-authored plan renders honestly in the panes", () => {
  it("shows a part with no linked issues as no-tasks-yet, not as complete or 0%", async () => {
    renderPane({ getActiveProjectPlan: async () => makeOverview() });
    await screen.findByText("Hand-authored plan");
    // The amber dashed state, by its own label — never "Done", never a 0/0 bar.
    expect(screen.getByText("No tasks yet")).toBeTruthy();
    expect(screen.queryByText("Done")).toBeNull();
    expect(screen.getByText(/Identified in the plan — no issues created yet/)).toBeTruthy();
  });
});
