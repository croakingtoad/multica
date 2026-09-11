// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import type { AgentRuntime } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// The path-check client is mocked at its own boundary: this suite is about what
// the dialog does with a verdict, not about how the verdict is fetched (that is
// packages/core/runtimes/daemon-path-check.test.ts).
const checkDaemonPath = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/runtimes/daemon-path-check", () => ({
  checkDaemonPath: (...args: unknown[]) => checkDaemonPath(...args),
}));

const desktop = vi.hoisted(() => ({ shell: false }));
const pickDirectory = vi.hoisted(() => vi.fn());
const validateLocalDirectory = vi.hoisted(() => vi.fn());
vi.mock("../../platform/local-directory", () => ({
  isDesktopShell: () => desktop.shell,
  pickDirectory: (...args: unknown[]) => pickDirectory(...args),
  validateLocalDirectory: (...args: unknown[]) =>
    validateLocalDirectory(...args),
}));

import {
  LocalDirectoryPickerDialog,
  eligibleLocalDirectoryMachines,
  type LocalDirectoryMachine,
  type LocalDirectorySelection,
} from "./local-directory-picker-dialog";

const NOW = Date.parse("2026-09-11T12:00:00Z");
const ME = "user-me";

function runtime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "rt-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "Claude (workstation)",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "workstation · macOS (arm64)",
    metadata: {},
    owner_id: ME,
    visibility: "private",
    last_seen_at: "2026-09-11T11:59:00Z",
    created_at: "2026-09-01T00:00:00Z",
    updated_at: "2026-09-11T11:59:00Z",
    ...overrides,
  };
}

function machine(
  overrides: Partial<LocalDirectoryMachine> = {},
): LocalDirectoryMachine {
  return {
    id: "local:daemon-1",
    daemonId: "daemon-1",
    title: "workstation",
    subtitle: "arm64 macOS",
    lastSeenAt: "2026-09-11T11:59:00Z",
    isCurrent: false,
    ...overrides,
  };
}

function renderPicker(
  overrides: {
    machines?: LocalDirectoryMachine[];
    attachedDaemonIds?: Set<string>;
    onSelected?: (selection: LocalDirectorySelection) => void;
  } = {},
) {
  const onSelected = overrides.onSelected ?? vi.fn();
  const result = renderWithI18n(
    <LocalDirectoryPickerDialog
      open
      onOpenChange={() => {}}
      workspaceId="ws-1"
      machines={overrides.machines ?? [machine()]}
      attachedDaemonIds={overrides.attachedDaemonIds ?? new Set()}
      onSelected={onSelected}
    />,
  );
  return { onSelected, ...result };
}

function pathInput(): HTMLElement {
  return screen.getByLabelText("Directory path");
}

function confirm(): HTMLElement {
  return screen.getByRole("button", { name: "Continue" });
}

const USABLE = { ok: true, isGitRepo: true };

beforeEach(() => {
  desktop.shell = false;
  checkDaemonPath.mockReset();
  pickDirectory.mockReset();
  validateLocalDirectory.mockReset();
  checkDaemonPath.mockResolvedValue(USABLE);
});

describe("eligibleLocalDirectoryMachines", () => {
  // The whole point of the feature: a daemon on another host is attachable.
  it("lists every online machine the user owns", () => {
    const machines = eligibleLocalDirectoryMachines(
      [
        runtime({ id: "rt-1", daemon_id: "daemon-1", name: "Claude (alpha)" }),
        runtime({ id: "rt-2", daemon_id: "daemon-2", name: "Codex (beta)" }),
      ],
      {
        now: NOW,
        currentUserId: ME,
        localDaemonId: null,
        localMachineName: null,
      },
    );
    expect(machines.map((m) => m.daemonId)).toEqual(["daemon-1", "daemon-2"]);
    expect(machines.every((m) => m.isCurrent)).toBe(false);
  });

  it("puts the browser-local machine first and flags it", () => {
    const machines = eligibleLocalDirectoryMachines(
      [
        runtime({ id: "rt-1", daemon_id: "daemon-zz", name: "Claude (zulu)" }),
        runtime({ id: "rt-2", daemon_id: "daemon-aa", name: "Claude (alpha)" }),
      ],
      {
        now: NOW,
        currentUserId: ME,
        localDaemonId: "daemon-zz",
        localMachineName: "zulu",
      },
    );
    expect(machines[0]?.daemonId).toBe("daemon-zz");
    expect(machines[0]?.isCurrent).toBe(true);
    expect(machines[1]?.isCurrent).toBe(false);
  });

  // The runtime list is workspace-wide. A row for a daemon the caller does not
  // own could only ever produce a permission error from the path check, so it
  // must never be offered.
  it("excludes another member's online daemon", () => {
    const machines = eligibleLocalDirectoryMachines(
      [
        runtime({ id: "rt-1", daemon_id: "daemon-mine" }),
        runtime({
          id: "rt-2",
          daemon_id: "daemon-theirs",
          owner_id: "user-other",
          name: "Claude (their-laptop)",
        }),
      ],
      {
        now: NOW,
        currentUserId: ME,
        localDaemonId: null,
        localMachineName: null,
      },
    );
    expect(machines.map((m) => m.daemonId)).toEqual(["daemon-mine"]);
  });

  it("excludes offline machines and cloud runtimes", () => {
    const machines = eligibleLocalDirectoryMachines(
      [
        runtime({ id: "rt-1", daemon_id: "daemon-1" }),
        runtime({
          id: "rt-2",
          daemon_id: "daemon-off",
          status: "offline",
          last_seen_at: "2026-09-01T00:00:00Z",
          name: "Claude (sleeping)",
        }),
        runtime({
          id: "rt-3",
          daemon_id: "daemon-cloud",
          runtime_mode: "cloud",
          name: "Claude (cloud)",
        }),
      ],
      {
        now: NOW,
        currentUserId: ME,
        localDaemonId: null,
        localMachineName: null,
      },
    );
    expect(machines.map((m) => m.daemonId)).toEqual(["daemon-1"]);
  });

  it("lists nothing when the viewing user is unknown", () => {
    expect(
      eligibleLocalDirectoryMachines([runtime()], {
        now: NOW,
        currentUserId: null,
        localDaemonId: null,
        localMachineName: null,
      }),
    ).toEqual([]);
  });
});

describe("LocalDirectoryPickerDialog", () => {
  // Two online daemons, neither co-located with the browser: the flow that
  // previously dead-ended on "start the local daemon" (LOCO-171).
  it("lists both remote machines without any start-the-daemon copy", () => {
    renderPicker({
      machines: eligibleLocalDirectoryMachines(
        [
          runtime({ id: "rt-1", daemon_id: "daemon-1", name: "Claude (alpha)" }),
          runtime({ id: "rt-2", daemon_id: "daemon-2", name: "Codex (beta)" }),
        ],
        {
          now: NOW,
          currentUserId: ME,
          localDaemonId: null,
          localMachineName: null,
        },
      ),
    });

    expect(screen.getAllByRole("radio")).toHaveLength(2);
    expect(screen.getByText("alpha")).toBeTruthy();
    expect(screen.getByText("beta")).toBeTruthy();
    expect(screen.queryByText(/Start the local daemon/i)).toBeNull();
    expect(screen.queryByText("This machine")).toBeNull();
  });

  it("never offers another member's online daemon", () => {
    renderPicker({
      machines: eligibleLocalDirectoryMachines(
        [
          runtime({ id: "rt-1", daemon_id: "daemon-mine", name: "Claude (mine)" }),
          runtime({
            id: "rt-2",
            daemon_id: "daemon-theirs",
            owner_id: "user-other",
            name: "Claude (their-laptop)",
          }),
        ],
        {
          now: NOW,
          currentUserId: ME,
          localDaemonId: null,
          localMachineName: null,
        },
      ),
    });

    expect(screen.getByText("mine")).toBeTruthy();
    expect(screen.queryByText("their-laptop")).toBeNull();
    expect(screen.getAllByRole("radio")).toHaveLength(1);
  });

  // The native dialog is the only path-entry method that can show the right
  // filesystem, and it only works for the machine the browser is running on.
  it("uses the native folder dialog for this machine and the daemon check for others", async () => {
    desktop.shell = true;
    pickDirectory.mockResolvedValue({
      ok: true,
      path: "/Users/dev/work/client",
      basename: "client",
    });
    validateLocalDirectory.mockResolvedValue({ ok: true, is_git_repo: true });
    const { onSelected } = renderPicker({
      machines: [
        machine({ id: "local:d1", daemonId: "d1", title: "laptop", isCurrent: true }),
        machine({ id: "local:d2", daemonId: "d2", title: "workstation" }),
      ],
    });

    expect(screen.getByText("This machine")).toBeTruthy();
    expect(screen.queryByLabelText("Directory path")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: /Choose folder/ }));
    await waitFor(() => expect(pickDirectory).toHaveBeenCalled());
    fireEvent.click(confirm());

    await waitFor(() =>
      expect(onSelected).toHaveBeenCalledWith({
        daemonId: "d1",
        path: "/Users/dev/work/client",
        label: "client",
        isGitRepo: true,
      }),
    );
    expect(checkDaemonPath).not.toHaveBeenCalled();

    // Switching to the other machine swaps the native button for a typed path.
    fireEvent.click(screen.getAllByRole("radio")[1] as HTMLElement);
    expect(pathInput()).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Choose folder/ })).toBeNull();
  });

  it("asks the selected daemon about a typed path", async () => {
    const { onSelected } = renderPicker({
      machines: [machine({ id: "local:d2", daemonId: "d2", title: "workstation" })],
    });

    fireEvent.change(pathInput(), { target: { value: "/srv/app" } });
    fireEvent.click(confirm());

    await waitFor(() =>
      expect(checkDaemonPath).toHaveBeenCalledWith({
        workspaceId: "ws-1",
        daemonId: "d2",
        path: "/srv/app",
      }),
    );
    expect(onSelected).toHaveBeenCalledWith({
      daemonId: "d2",
      path: "/srv/app",
      label: "app",
      isGitRepo: true,
    });
  });

  // Every rejected path has to name its reason in the dialog the user is
  // looking at, and must not let the flow continue — the resource is created
  // downstream of onSelected, so not calling it is what "no resource row" means
  // here. The end-to-end version lives in project-resources-local-picker.test.tsx.
  it.each([
    ["not_found", /doesn't exist/i],
    ["not_a_directory", /not a directory/i],
    ["not_readable", /not readable/i],
    ["not_writable", /not writable/i],
    ["not_absolute", /absolute path/i],
    ["machine_offline", /went offline/i],
    ["check_timed_out", /didn't answer in time/i],
    ["not_permitted", /can't check paths/i],
  ])("shows %s inline and goes no further", async (reason, copy) => {
    checkDaemonPath.mockResolvedValue({ ok: false, reason });
    const { onSelected } = renderPicker();

    fireEvent.change(pathInput(), { target: { value: "/srv/app" } });
    fireEvent.click(confirm());

    await waitFor(() => expect(screen.getByRole("alert")).toBeTruthy());
    expect(screen.getByRole("alert").textContent).toMatch(copy);
    expect(onSelected).not.toHaveBeenCalled();
  });

  // `worktree` needs somewhere to put the branch. The mode dialog disables the
  // option on `not_git`; this is where that fact enters the flow.
  it("hands back is_git_repo: false so worktree mode can be blocked", async () => {
    checkDaemonPath.mockResolvedValue({ ok: true, isGitRepo: false });
    const { onSelected } = renderPicker();

    fireEvent.change(pathInput(), { target: { value: "/srv/notes" } });
    fireEvent.click(confirm());

    await waitFor(() =>
      expect(onSelected).toHaveBeenCalledWith(
        expect.objectContaining({ isGitRepo: false }),
      ),
    );
  });

  it("shows a pending state on confirm while the daemon is being asked", async () => {
    let release: (value: unknown) => void = () => {};
    checkDaemonPath.mockReturnValue(
      new Promise((resolve) => {
        release = resolve;
      }),
    );
    renderPicker();

    fireEvent.change(pathInput(), { target: { value: "/srv/app" } });
    fireEvent.click(confirm());

    const pending = await screen.findByRole("button", { name: "Checking…" });
    expect(pending.hasAttribute("disabled")).toBe(true);
    release(USABLE);
    await waitFor(() => expect(confirm()).toBeTruthy());
  });

  // One local_directory per (project, daemon) — the server enforces it, and
  // discovering it on a 409 after typing a path is a worse dialog.
  it("blocks a machine that already has a directory on this project", () => {
    renderPicker({
      machines: [
        machine({ id: "local:d1", daemonId: "d1", title: "taken" }),
        machine({ id: "local:d2", daemonId: "d2", title: "free" }),
      ],
      attachedDaemonIds: new Set(["d1"]),
    });

    const [taken, free] = screen.getAllByRole("radio") as HTMLElement[];
    expect(taken?.hasAttribute("disabled")).toBe(true);
    expect(screen.getByText(/Already has a directory attached/i)).toBeTruthy();
    // Preselection skips it, so the dialog opens on something usable.
    expect(free?.getAttribute("aria-checked")).toBe("true");
  });

  it("keeps the confirm button closed until a path is entered", () => {
    renderPicker();
    expect(confirm().hasAttribute("disabled")).toBe(true);
    fireEvent.change(pathInput(), { target: { value: "/srv/app" } });
    expect(confirm().hasAttribute("disabled")).toBe(false);
  });
});
