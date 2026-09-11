// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import type { AgentRuntime, ProjectResource } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// End-to-end wiring for the machine selector (LOCO-171): sidebar button →
// machine + path picker → execution mode → created resource. The picker's own
// states are covered in local-directory-picker-dialog.test.tsx; this file
// exists for the seams between the three, which is where a resource either
// gets created against the right daemon or does not get created at all.

const ME = "user-me";
const NOW = "2026-09-11T11:59:00Z";

const runtimes = vi.hoisted(() => ({ list: [] as unknown[] }));
const resources = vi.hoisted(() => ({ list: [] as unknown[] }));
const createMock = vi.hoisted(() => vi.fn().mockResolvedValue({}));
const checkDaemonPath = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: unknown[] }) => {
    const key = options?.queryKey?.[0];
    if (key === "project-resources") return { data: resources.list };
    if (key === "runtimes") return { data: runtimes.list };
    return { data: [] };
  },
  queryOptions: (options: unknown) => options,
}));

vi.mock("@multica/core/projects", () => ({
  projectResourcesOptions: () => ({
    queryKey: ["project-resources"],
    queryFn: vi.fn(),
  }),
  useCreateProjectResource: () => ({
    mutateAsync: createMock,
    isPending: false,
  }),
  useUpdateProjectResource: () => ({ mutateAsync: vi.fn() }),
  useDeleteProjectResource: () => ({ mutateAsync: vi.fn() }),
}));

// A server that DOES gate the worktree mode, so the only thing that can block
// the option in these tests is the path check's own verdict.
vi.mock("@multica/core/config", () => ({
  useConfigStore: (
    selector: (state: { localWorktreeSupported: boolean }) => unknown,
  ) => selector({ localWorktreeSupported: true }),
}));

// Partial: the machine list is built from the REAL health derivation, so
// "online" in these fixtures means what it means in production. Only the query
// options and the daemon capability probe are stubbed.
vi.mock("@multica/core/runtimes", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/runtimes")>()),
  runtimeListOptions: () => ({ queryKey: ["runtimes"], queryFn: vi.fn() }),
  runtimeAdvertisesLocalWorktree: () => true,
}));

vi.mock("@multica/core/runtimes/daemon-path-check", () => ({
  checkDaemonPath: (...args: unknown[]) => checkDaemonPath(...args),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", slug: "ws", repos: [] }),
}));
vi.mock("@multica/core/auth", () => {
  // Inlined rather than referencing ME: this factory is hoisted above the
  // module's own consts.
  const state = { user: { id: "user-me" } };
  const useAuthStore = (selector: (s: typeof state) => unknown) =>
    selector(state);
  useAuthStore.getState = () => state;
  return { useAuthStore };
});

// A browser, not the desktop shell: no preload bridge, no co-located daemon.
// The flow used to be hidden entirely in this situation.
vi.mock("../../platform/local-directory", () => ({
  isDesktopShell: () => false,
  pickDirectory: vi.fn(),
  validateLocalDirectory: vi.fn(),
}));
vi.mock("../../platform/use-local-daemon-status", () => ({
  useLocalDaemonStatus: () => ({
    daemonId: null,
    deviceName: null,
    running: false,
  }),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { ProjectResourcesSection } from "./project-resources-section";

function runtime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "rt-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "Claude (alpha)",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "alpha · Linux (x86_64)",
    metadata: {},
    owner_id: ME,
    visibility: "private",
    last_seen_at: NOW,
    created_at: "2026-09-01T00:00:00Z",
    updated_at: NOW,
    ...overrides,
  };
}

function localDirectoryResource(daemonId: string): ProjectResource {
  return {
    id: `res-${daemonId}`,
    project_id: "p1",
    workspace_id: "ws-1",
    resource_type: "local_directory",
    resource_ref: { daemon_id: daemonId, local_path: "/srv/old" },
    label: null,
    position: 0,
    created_at: "2026-09-01T00:00:00Z",
    created_by: ME,
  };
}

const TWO_ONLINE_MACHINES = [
  runtime({ id: "rt-1", daemon_id: "daemon-1", name: "Claude (alpha)" }),
  runtime({ id: "rt-2", daemon_id: "daemon-2", name: "Codex (beta)" }),
];

function addLocalDirectoryButton(): HTMLElement {
  return screen.getByRole("button", { name: /Add local directory/i });
}

beforeEach(() => {
  runtimes.list = TWO_ONLINE_MACHINES;
  resources.list = [];
  createMock.mockReset().mockResolvedValue({});
  checkDaemonPath.mockReset().mockResolvedValue({ ok: true, isGitRepo: true });
});

describe("ProjectResourcesSection — attaching a directory on another machine", () => {
  // The reported dead end: two online daemons the user owns, neither beside the
  // browser, and the old UI answered "start the local daemon to attach a
  // directory on this machine".
  it("offers both online machines from a browser, with no start-the-daemon copy", () => {
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);

    expect(screen.queryByText(/Start the local daemon/i)).toBeNull();
    fireEvent.click(addLocalDirectoryButton());

    expect(screen.getAllByRole("radio")).toHaveLength(2);
    expect(screen.getByText("alpha")).toBeTruthy();
    expect(screen.getByText("beta")).toBeTruthy();
    expect(screen.queryByText(/Start the local daemon/i)).toBeNull();
  });

  it("creates the resource against the machine that was picked", async () => {
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);
    fireEvent.click(addLocalDirectoryButton());

    fireEvent.click(screen.getAllByRole("radio")[1] as HTMLElement);
    fireEvent.change(screen.getByLabelText("Directory path"), {
      target: { value: "/srv/app" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    const add = await screen.findByRole("button", { name: "Add folder" });
    fireEvent.click(add);

    await waitFor(() => expect(createMock).toHaveBeenCalledTimes(1));
    expect(createMock).toHaveBeenCalledWith({
      resource_type: "local_directory",
      resource_ref: {
        local_path: "/srv/app",
        daemon_id: "daemon-2",
        label: "app",
        execution_mode: "worktree",
      },
    });
  });

  // A folder with no repository has nowhere to put the branch, so the option
  // has to be closed where the user chooses — and the check is what says so.
  it("makes parallel mode unselectable when the daemon reports no git repo", async () => {
    checkDaemonPath.mockResolvedValue({ ok: true, isGitRepo: false });
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);
    fireEvent.click(addLocalDirectoryButton());

    fireEvent.change(screen.getByLabelText("Directory path"), {
      target: { value: "/srv/notes" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    await screen.findByRole("button", { name: "Add folder" });
    const worktree = screen.getAllByRole("radio")[1] as HTMLElement;
    expect(worktree.hasAttribute("disabled")).toBe(true);
    expect(screen.getByText(/not a git repository/i)).toBeTruthy();

    fireEvent.click(worktree);
    fireEvent.click(screen.getByRole("button", { name: "Add folder" }));

    await waitFor(() => expect(createMock).toHaveBeenCalledTimes(1));
    expect(createMock.mock.calls[0]?.[0]).toMatchObject({
      resource_ref: expect.objectContaining({ execution_mode: "in_place" }),
    });
  });

  it("creates nothing when the daemon rejects the path", async () => {
    checkDaemonPath.mockResolvedValue({ ok: false, reason: "not_found" });
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);
    fireEvent.click(addLocalDirectoryButton());

    fireEvent.change(screen.getByLabelText("Directory path"), {
      target: { value: "/srv/gone" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    await waitFor(() => expect(screen.getByRole("alert")).toBeTruthy());
    expect(screen.getByRole("alert").textContent).toMatch(/doesn't exist/i);
    // Still on the picker — no mode dialog, and nothing written.
    expect(screen.queryByRole("button", { name: "Add folder" })).toBeNull();
    expect(createMock).not.toHaveBeenCalled();
  });

  // The one case that keeps its old shape: nothing online to attach anything
  // on, so the instructional copy is still the honest answer.
  it("keeps the instructional empty state when the user has no online daemon", () => {
    runtimes.list = [
      runtime({ status: "offline", last_seen_at: "2026-09-01T00:00:00Z" }),
    ];
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);

    expect(
      screen.getByText(
        "Start the local daemon to attach a directory on this machine.",
      ),
    ).toBeTruthy();
    expect(addLocalDirectoryButton().hasAttribute("disabled")).toBe(true);
  });

  it("says so when every online machine is already attached", () => {
    resources.list = [
      localDirectoryResource("daemon-1"),
      localDirectoryResource("daemon-2"),
    ];
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);

    expect(screen.getByText(/Every online machine already has/i)).toBeTruthy();
    expect(addLocalDirectoryButton().hasAttribute("disabled")).toBe(true);
  });
});
