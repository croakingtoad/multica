"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ChevronRight,
  FolderGit,
  FolderOpen,
  GitBranch,
  Pencil,
  Plus,
  Search,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import {
  projectResourcesOptions,
  useCreateProjectResource,
  useDeleteProjectResource,
  useUpdateProjectResource,
} from "@multica/core/projects";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import type {
  GithubRepoResourceRef,
  LocalDirectoryExecutionMode,
  LocalDirectoryResourceRef,
  ProjectResource,
} from "@multica/core/types";
import {
  runtimeAdvertisesLocalWorktree,
  runtimeListOptions,
} from "@multica/core/runtimes";
import { useConfigStore } from "@multica/core/config";
import { useAuthStore } from "@multica/core/auth";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { isDesktopShell, useLocalDaemonStatus } from "../../platform";
import {
  LocalDirectoryModeDialog,
  type WorktreeUnavailableReason,
} from "./local-directory-mode-dialog";
import {
  LocalDirectoryPickerDialog,
  eligibleLocalDirectoryMachines,
} from "./local-directory-picker-dialog";
import { localDirectoryLabel } from "./local-directory-label";
import { useT } from "../../i18n";
import { githubShortLabel } from "../../common/github-url";

// Project Resources sidebar section.
//
// Type-dispatched at the row + add-flow level. Add a new resource_type by:
//   (1) extending the server validator
//   (2) extending ProjectResourceType in @multica/core/types
//   (3) adding a render case in ResourceRow and an add-control here
function isGithubRef(r: ProjectResource): r is ProjectResource & {
  resource_ref: GithubRepoResourceRef;
} {
  return r.resource_type === "github_repo";
}

function isLocalDirectoryRef(r: ProjectResource): r is ProjectResource & {
  resource_ref: LocalDirectoryResourceRef;
} {
  return r.resource_type === "local_directory";
}

/**
 * Reads the execution mode off a stored ref. An absent or unrecognised value is
 * reported as in_place, matching the server: the field is optional, and a mode
 * written by a newer client must not render as anything other than the
 * conservative default here.
 */
function executionModeOf(
  ref: LocalDirectoryResourceRef,
): LocalDirectoryExecutionMode {
  return ref.execution_mode === "worktree" ? "worktree" : "in_place";
}

/** Pending mode edit — either for a directory being added, or an existing row. */
type ModeDialogState = {
  path: string;
  daemonId: string | null;
  mode: LocalDirectoryExecutionMode;
  /** undefined = unknown (older desktop build); treated as "cannot verify". */
  isGitRepo: boolean | undefined;
  /** Set for an edit; absent when adding a new resource. */
  resource?: ProjectResource & { resource_ref: LocalDirectoryResourceRef };
  /** Only used when adding. */
  label?: string;
};

export function ProjectResourcesSection({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const workspace = useCurrentWorkspace();
  const daemonStatus = useLocalDaemonStatus();
  const [open, setOpen] = useState(true);
  const [addOpen, setAddOpen] = useState(false);
  const [repoSearch, setRepoSearch] = useState("");
  const [pickerOpen, setPickerOpen] = useState(false);
  const [modeDialog, setModeDialog] = useState<ModeDialogState | null>(null);
  const [modeSaving, setModeSaving] = useState(false);
  const [modeError, setModeError] = useState<string | null>(null);

  const { data: resources = [] } = useQuery(
    projectResourcesOptions(wsId, projectId),
  );
  const createResource = useCreateProjectResource(wsId, projectId);
  const updateResource = useUpdateProjectResource(wsId, projectId);
  const deleteResource = useDeleteProjectResource(wsId, projectId);

  // Renaming a local_directory row stays desktop-only: the label belongs to the
  // machine that registered it. ATTACHING no longer does — the path check is
  // server-side now, so a browser can complete the flow for any machine the
  // user owns (LOCO-171).
  const desktopMode = isDesktopShell();
  const localDaemonId = daemonStatus.daemonId;

  // Only ever used to decide what to PRESELECT. Whether the machine can run
  // worktree mode is the server's call — it knows its own version, the client
  // would have to infer it from data the server wrote, and that inference is
  // what told a user on the newest release to upgrade it (#7113). The save is
  // gated server-side and surfaced here as an inline error instead.
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  // The one thing the client must still check up front: whether this server
  // performs that gate at all. One declared boolean, no inference — servers
  // that predate it drop execution_mode and answer 201.
  const serverValidatesWorktree = useConfigStore((state) => state.localWorktreeSupported);
  // Keyed on the resource's OWN daemon, not the machine the browser happens to
  // be on: a resource is pinned to one machine, and its mode can legitimately
  // be changed from the web app or from a different device. Using the local
  // daemon here would report "too old" for every resource whenever the viewer
  // is not on that machine.
  // Capability, not version, and judged by the daemon's newest runtime row —
  // see runtimeAdvertisesLocalWorktree for why an any-match would keep saying
  // yes after a downgrade.
  const advertisesWorktree = (daemonId: string | null) =>
    runtimeAdvertisesLocalWorktree(runtimes, daemonId);

  const attachedUrls = new Set(
    resources.filter(isGithubRef).map((r) => r.resource_ref.url),
  );
  // Per (project, daemon) we allow at most one local_directory — the
  // daemon-side resolver picks the first match by daemon_id, so two rows
  // on the same daemon would silently route the agent into one of them.
  // The server enforces this at the API boundary; the UI mirrors it per
  // MACHINE now that several are offered, otherwise users would only discover
  // the limit on a 409 toast for the one machine that was already taken.
  const attachedDaemonIds = useMemo(
    () =>
      new Set(
        resources
          .filter(isLocalDirectoryRef)
          .map((r) => r.resource_ref.daemon_id),
      ),
    [resources],
  );

  // Every machine this user could attach a directory on, "this machine" first.
  // Sourced from the runtime list already loaded above rather than from the
  // Electron bridge: the server knows about every daemon the user owns, and
  // which one happens to sit next to the browser is not what decides whether a
  // directory can be attached (LOCO-171).
  const currentUserId = useAuthStore((state) => state.user?.id ?? null);
  const machines = useMemo(
    () =>
      eligibleLocalDirectoryMachines(runtimes, {
        now: Date.now(),
        currentUserId,
        localDaemonId,
        localMachineName: daemonStatus.deviceName,
      }),
    [runtimes, currentUserId, localDaemonId, daemonStatus.deviceName],
  );
  const availableMachines = machines.filter(
    (machine) => !attachedDaemonIds.has(machine.daemonId),
  );

  const repoQuery = repoSearch.trim().toLowerCase();
  const filteredRepos =
    workspace?.repos?.filter((repo) => repo.url.toLowerCase().includes(repoQuery)) ?? [];

  const handleAttach = async (url: string) => {
    try {
      await createResource.mutateAsync({
        resource_type: "github_repo",
        resource_ref: { url },
      });
      toast.success(t(($) => $.resources.toast_attached));
    } catch (err) {
      const msg = err instanceof Error ? err.message : t(($) => $.resources.toast_attach_failed);
      toast.error(msg);
    }
  };

  // The picker hands back a (machine, path) pair the daemon has already
  // vouched for. All that is left is the execution mode, which is part of what
  // the user is choosing — whether tasks edit this folder or hand back a
  // branch — not a setting to discover afterwards.
  const handleDirectoryPicked = (selection: {
    daemonId: string;
    path: string;
    label: string;
    isGitRepo: boolean | undefined;
  }) => {
    setPickerOpen(false);
    setModeError(null);
    setModeDialog({
      path: selection.path,
      daemonId: selection.daemonId,
      // Same preselection rule as the create-project flow: a git repo that
      // daemon can actually run worktree mode on starts on parallel, anything
      // else starts on direct. Only the PRESELECTION differs by folder — the
      // user still confirms, and existing resources keep whatever they have.
      mode:
        selection.isGitRepo === true &&
        serverValidatesWorktree &&
        advertisesWorktree(selection.daemonId)
          ? "worktree"
          : "in_place",
      isGitRepo: selection.isGitRepo,
      label: selection.label,
    });
    setAddOpen(false);
  };

  const handleConfirmMode = async (mode: LocalDirectoryExecutionMode) => {
    if (!modeDialog || modeSaving) return;
    setModeSaving(true);
    setModeError(null);
    try {
      if (modeDialog.resource) {
        const ref = modeDialog.resource.resource_ref;
        if (executionModeOf(ref) === mode) {
          setModeDialog(null);
          return;
        }
        await updateResource.mutateAsync({
          resourceId: modeDialog.resource.id,
          data: {
            // Spread first so every other ref field survives the edit — the
            // server replaces the whole ref, it does not deep-merge.
            resource_ref: { ...ref, execution_mode: mode },
          },
        });
        toast.success(t(($) => $.resources.toast_local_mode_updated));
      } else {
        // The daemon the PICKER selected, never the browser-local one — the
        // whole point of the machine selector is that they differ.
        const daemonId = modeDialog.daemonId;
        if (!daemonId) return;
        await createResource.mutateAsync({
          resource_type: "local_directory",
          resource_ref: {
            local_path: modeDialog.path,
            daemon_id: daemonId,
            label: modeDialog.label ?? modeDialog.path,
            execution_mode: mode,
          },
        });
        toast.success(t(($) => $.resources.toast_local_attached));
      }
      setModeDialog(null);
    } catch (err) {
      // Keep the dialog open and show the reason inline: the most likely
      // failure is the server's daemon-version gate, and closing the dialog
      // would leave the user with a toast and no way to act on it.
      setModeError(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.resources.toast_local_mode_update_failed),
      );
    } finally {
      setModeSaving(false);
    }
  };

  const handleRemove = async (resource: ProjectResource) => {
    try {
      await deleteResource.mutateAsync(resource.id);
      toast.success(t(($) => $.resources.toast_removed));
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.resources.toast_remove_failed),
      );
    }
  };

  const handleRenameLocalDirectory = async (
    resource: ProjectResource & { resource_ref: LocalDirectoryResourceRef },
    nextLabel: string,
  ) => {
    const trimmed = nextLabel.trim();
    if (trimmed === localDirectoryLabel(resource)) return;
    try {
      // Top-level label ONLY — renaming must not resend resource_ref.
      //
      // The server replaces the ref wholesale with whatever it can parse, so a
      // server that predates a ref field drops it and answers 200. On a backend
      // rolled back below v0.4.25 (documented as supported while the runtimes
      // stay current) that turned "rename this folder" into "silently forget
      // this folder was isolated", and the next task edited the working copy
      // (#7113). Omitting the ref keeps the stored one untouched on every
      // server version — the same reason it must not be resent for any other
      // unrelated edit either.
      await updateResource.mutateAsync({
        resourceId: resource.id,
        data: { label: trimmed },
      });
      toast.success(t(($) => $.resources.toast_local_renamed));
    } catch (err) {
      const msg =
        err instanceof Error
          ? err.message
          : t(($) => $.resources.toast_local_rename_failed);
      toast.error(msg);
    }
  };

  return (
    <div>
      <button
        type="button"
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.resources.section_header)}
        <ChevronRight
          className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`}
        />
      </button>
      {open && (
        <div className="pl-2 space-y-1.5">
          {resources.length === 0 && (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.resources.empty)}
            </p>
          )}
          {resources.length > 0 && (
            <div className="max-h-64 space-y-1.5 overflow-y-auto pr-1">
              {resources.map((resource) => (
                <ResourceRow
                  key={resource.id}
                  resource={resource}
                  localDaemonId={localDaemonId}
                  canEdit={desktopMode}
                  onRemove={() => handleRemove(resource)}
                  onRenameLocalDirectory={handleRenameLocalDirectory}
                  onEditLocalDirectoryMode={(target) => {
                    setModeError(null);
                    setModeDialog({
                      path: target.resource_ref.local_path,
                      daemonId: target.resource_ref.daemon_id,
                      mode: executionModeOf(target.resource_ref),
                      // The path is already saved, so there is nothing to
                      // re-validate from the browser; the desktop check only
                      // runs at pick time. Unknown means the option stays
                      // available and the daemon has the final say.
                      isGitRepo: undefined,
                      resource: target,
                    });
                  }}
                />
              ))}
            </div>
          )}
          <Popover
            open={addOpen}
            onOpenChange={(v) => {
              setAddOpen(v);
              if (!v) setRepoSearch("");
            }}
          >
            <PopoverTrigger
              render={
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-7 px-2 text-caption text-muted-foreground hover:text-foreground"
                >
                  <Plus className="size-3" />
                  {t(($) => $.resources.add_button)}
                </Button>
              }
            />
            <PopoverContent align="start" className="w-72 p-2 space-y-2">
              <div className="text-caption font-medium text-muted-foreground">
                {t(($) => $.resources.popover_title)}
              </div>
              {workspace?.repos && workspace.repos.length > 0 && (
                <>
                  <div className="relative">
                    <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
                    <input
                      type="text"
                      value={repoSearch}
                      onChange={(e) => setRepoSearch(e.target.value)}
                      aria-label={t(($) => $.resources.repos_search_placeholder)}
                      placeholder={t(($) => $.resources.repos_search_placeholder)}
                      className="h-8 w-full rounded-md border bg-transparent pl-7 pr-2 text-caption outline-none placeholder:text-muted-foreground focus-visible:ring-1 focus-visible:ring-ring"
                    />
                  </div>
                  <div className="max-h-48 space-y-1 overflow-y-auto">
                    {filteredRepos.length === 0 && repoQuery && (
                      <p className="py-2 text-center text-caption text-muted-foreground">
                        {t(($) => $.resources.repos_search_empty)}
                      </p>
                    )}
                    {filteredRepos.map((repo) => {
                      const isAttached = attachedUrls.has(repo.url);
                      const isDisabled = isAttached || createResource.isPending;
                      return (
                        // Use aria-disabled instead of the native `disabled` attribute so
                        // hover events still reach the tooltip trigger on attached rows
                        // (browsers suppress pointer events on disabled form controls).
                        <button
                          key={repo.url}
                          type="button"
                          aria-disabled={isDisabled}
                          onClick={async () => {
                            if (isDisabled) return;
                            await handleAttach(repo.url);
                            setAddOpen(false);
                          }}
                          className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-caption text-left hover:bg-accent transition-colors aria-disabled:opacity-50 aria-disabled:cursor-not-allowed aria-disabled:hover:bg-transparent"
                        >
                          <FolderGit className="size-3.5" />
                          <Tooltip>
                            <TooltipTrigger
                              render={
                                <span className="truncate flex-1">{githubShortLabel(repo.url)}</span>
                              }
                            />
                            <TooltipContent side="top">{repo.url}</TooltipContent>
                          </Tooltip>
                          {isAttached && (
                            <span className="text-micro text-muted-foreground">
                              {t(($) => $.resources.attached_badge)}
                            </span>
                          )}
                        </button>
                      );
                    })}
                  </div>
                </>
              )}
              <CustomRepoForm
                onSubmit={async (url) => {
                  await handleAttach(url);
                  setAddOpen(false);
                }}
              />
            </PopoverContent>
          </Popover>
          {/* Rendered on web as well as desktop. The flow used to be hidden in
              the browser because the daemon-id check could only run in the
              Electron shell; that check is server-side now, so the browser can
              complete it for any machine the user owns (LOCO-171). */}
          <div className="flex flex-col">
            <Button
              variant="ghost"
              size="sm"
              className="h-7 justify-start px-2 text-caption text-muted-foreground hover:text-foreground"
              disabled={
                createResource.isPending || availableMachines.length === 0
              }
              onClick={() => setPickerOpen(true)}
            >
              <FolderOpen className="size-3" />
              {t(($) => $.resources.add_local_directory_button)}
            </Button>
            {machines.length === 0 && (
              <p className="px-2 pt-0.5 text-micro text-muted-foreground">
                {t(($) => $.resources.local_daemon_offline_hint)}
              </p>
            )}
            {machines.length > 0 && availableMachines.length === 0 && (
              <p className="px-2 pt-0.5 text-micro text-muted-foreground">
                {machines.length === 1
                  ? t(($) => $.resources.local_daemon_already_attached_hint)
                  : t(($) => $.resources.local_all_machines_attached_hint)}
              </p>
            )}
          </div>
        </div>
      )}
      {pickerOpen && (
        <LocalDirectoryPickerDialog
          open
          onOpenChange={setPickerOpen}
          workspaceId={wsId}
          machines={machines}
          attachedDaemonIds={attachedDaemonIds}
          onSelected={handleDirectoryPicked}
        />
      )}
      {modeDialog && (
        <LocalDirectoryModeDialog
          open
          onOpenChange={(next) => {
            if (!next) {
              setModeDialog(null);
              setModeError(null);
            }
          }}
          path={modeDialog.path}
          value={modeDialog.mode}
          unavailableReason={worktreeUnavailableReason(
            modeDialog.isGitRepo,
            serverValidatesWorktree,
          )}
          errorMessage={modeError ?? undefined}
          saving={modeSaving}
          confirmLabel={
            modeDialog.resource
              ? t(($) => $.resources.mode_save)
              : t(($) => $.resources.mode_add)
          }
          onConfirm={(mode) => void handleConfirmMode(mode)}
        />
      )}
    </div>
  );
}

/**
 * Which blocker (if any) applies to the worktree option.
 *
 * `isGitRepo === false` is a hard no — the daemon would fail every task on that
 * folder. `undefined` means we could not check (an older desktop build, or an
 * existing row whose path was validated at pick time), and is deliberately
 * permissive: the daemon re-checks authoritatively, so guessing "not a repo"
 * here would block a perfectly valid setup.
 *
 * Daemon capability is deliberately absent. It is the server's question, asked
 * on save; predicting it here is what produced an unfixable blocker for a user
 * already on the newest release (#7113). Deferring to the server does require
 * knowing it will answer, though — `serverValidates` is the server saying so.
 */
function worktreeUnavailableReason(
  isGitRepo: boolean | undefined,
  serverValidates: boolean,
): WorktreeUnavailableReason | undefined {
  if (isGitRepo === false) return "not_git";
  if (!serverValidates) return "server_outdated";
  return undefined;
}

interface ResourceRowProps {
  resource: ProjectResource;
  localDaemonId: string | null;
  canEdit: boolean;
  onRemove: () => void;
  onRenameLocalDirectory: (
    resource: ProjectResource & { resource_ref: LocalDirectoryResourceRef },
    nextLabel: string,
  ) => Promise<void>;
  onEditLocalDirectoryMode: (
    resource: ProjectResource & { resource_ref: LocalDirectoryResourceRef },
  ) => void;
}

function ResourceRow({
  resource,
  localDaemonId,
  canEdit,
  onRemove,
  onRenameLocalDirectory,
  onEditLocalDirectoryMode,
}: ResourceRowProps) {
  const { t } = useT("projects");
  if (isGithubRef(resource)) {
    const ref = resource.resource_ref;
    const display = resource.label || (ref.ref ? `${githubShortLabel(ref.url)} @ ${ref.ref}` : githubShortLabel(ref.url));
    const tooltip = ref.ref ? `${ref.url}\nref: ${ref.ref}` : ref.url;
    return (
      <div className="flex items-center gap-2 text-caption group">
        <FolderGit className="size-3.5 text-muted-foreground shrink-0" />
        <Tooltip>
          <TooltipTrigger
            render={
              <a
                href={ref.url}
                target="_blank"
                rel="noopener noreferrer"
                className="truncate flex-1 hover:underline"
              >
                {display}
              </a>
            }
          />
          <TooltipContent side="top" className="whitespace-pre-line">{tooltip}</TooltipContent>
        </Tooltip>
        <button
          type="button"
          onClick={onRemove}
          className="opacity-0 group-hover:opacity-100 transition-opacity rounded-sm p-0.5 hover:bg-accent"
          title={t(($) => $.resources.remove_tooltip)}
        >
          <Trash2 className="size-3 text-muted-foreground" />
        </button>
      </div>
    );
  }

  if (isLocalDirectoryRef(resource)) {
    return (
      <LocalDirectoryRow
        resource={resource}
        localDaemonId={localDaemonId}
        canEdit={canEdit}
        onRemove={onRemove}
        onRename={onRenameLocalDirectory}
        onEditMode={onEditLocalDirectoryMode}
      />
    );
  }

  return (
    <div className="flex items-center gap-2 text-caption text-muted-foreground">
      <span className="truncate flex-1">
        {resource.label || resource.resource_type}
      </span>
      <button
        type="button"
        onClick={onRemove}
        className="rounded-sm p-0.5 hover:bg-accent"
        title={t(($) => $.resources.remove_tooltip)}
      >
        <Trash2 className="size-3" />
      </button>
    </div>
  );
}

interface LocalDirectoryRowProps {
  resource: ProjectResource & { resource_ref: LocalDirectoryResourceRef };
  localDaemonId: string | null;
  canEdit: boolean;
  onRemove: () => void;
  onRename: (
    resource: ProjectResource & { resource_ref: LocalDirectoryResourceRef },
    nextLabel: string,
  ) => Promise<void>;
  onEditMode: (
    resource: ProjectResource & { resource_ref: LocalDirectoryResourceRef },
  ) => void;
}

function LocalDirectoryRow({
  resource,
  localDaemonId,
  canEdit,
  onRemove,
  onRename,
  onEditMode,
}: LocalDirectoryRowProps) {
  const { t } = useT("projects");
  const ref = resource.resource_ref;
  const mode = executionModeOf(ref);
  const display = localDirectoryLabel(resource);
  const isForeignDaemon =
    localDaemonId !== null && ref.daemon_id !== localDaemonId;
  const isLocalUnknown = localDaemonId === null;
  // "disabled" in the spec sense — visual de-emphasis + no chat hint, and
  // rename is hidden on foreign / unknown-daemon rows because the label
  // belongs to the owning device. Delete stays available so the user can
  // drop a stale registration from any device.
  const mismatch = isForeignDaemon || isLocalUnknown;

  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(display);

  const startEdit = () => {
    setDraft(display);
    setEditing(true);
  };
  const commit = async () => {
    setEditing(false);
    await onRename(resource, draft);
  };
  const cancel = () => {
    setEditing(false);
    setDraft(display);
  };

  return (
    <div
      className={`flex items-center gap-2 text-caption group ${
        mismatch ? "opacity-60" : ""
      }`}
    >
      <FolderOpen className="size-3.5 text-muted-foreground shrink-0" />
      {editing ? (
        <input
          autoFocus
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={() => void commit()}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              void commit();
            } else if (e.key === "Escape") {
              e.preventDefault();
              cancel();
            }
          }}
          className="flex-1 min-w-0 rounded-sm border bg-transparent px-1 py-0.5 text-caption outline-none focus-visible:ring-1 focus-visible:ring-ring"
          aria-label={t(($) => $.resources.local_rename_label)}
        />
      ) : (
        <Tooltip>
          <TooltipTrigger
            render={
              <span className="truncate flex-1">{display}</span>
            }
          />
          <TooltipContent side="top">
            <div className="space-y-0.5 text-micro">
              <div className="font-mono">{ref.local_path}</div>
              {mismatch && (
                <div className="text-muted-foreground">
                  {isLocalUnknown
                    ? t(($) => $.resources.local_no_daemon_tooltip)
                    : t(($) => $.resources.local_other_machine_tooltip)}
                </div>
              )}
            </div>
          </TooltipContent>
        </Tooltip>
      )}
      {/* Always visible, unlike the hover-only actions: without it there is no
          way to tell whether tasks on this folder edit it directly or hand back
          a branch, which is the first thing someone asks when a task queues (or
          does not). */}
      {mode === "worktree" && !editing && (
        <Tooltip>
          <TooltipTrigger
            render={
              <Badge variant="secondary" className="shrink-0 gap-1 font-normal">
                <GitBranch className="size-3" />
                {t(($) => $.resources.mode_badge_worktree)}
              </Badge>
            }
          />
          <TooltipContent side="top">
            {t(($) => $.resources.mode_badge_worktree_tooltip)}
          </TooltipContent>
        </Tooltip>
      )}
      {/* Not gated on `mismatch`: switching the mode only rewrites a field, so
          it works from the web app or another device, unlike rename (whose
          label belongs to the owning machine) or the folder picker. */}
      {!editing && (
        <button
          type="button"
          onClick={() => onEditMode(resource)}
          className="opacity-0 group-hover:opacity-100 transition-opacity rounded-sm p-0.5 hover:bg-accent"
          title={t(($) => $.resources.mode_edit_tooltip)}
        >
          <GitBranch className="size-3 text-muted-foreground" />
        </button>
      )}
      {canEdit && !mismatch && !editing && (
        <button
          type="button"
          onClick={startEdit}
          className="opacity-0 group-hover:opacity-100 transition-opacity rounded-sm p-0.5 hover:bg-accent"
          title={t(($) => $.resources.local_rename_tooltip)}
        >
          <Pencil className="size-3 text-muted-foreground" />
        </button>
      )}
      <button
        type="button"
        onClick={onRemove}
        className="opacity-0 group-hover:opacity-100 transition-opacity rounded-sm p-0.5 hover:bg-accent"
        title={t(($) => $.resources.remove_tooltip)}
      >
        <Trash2 className="size-3 text-muted-foreground" />
      </button>
    </div>
  );
}

function CustomRepoForm({
  onSubmit,
}: {
  onSubmit: (url: string) => Promise<void> | void;
}) {
  const { t } = useT("projects");
  const [url, setUrl] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const handle = async (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = url.trim();
    if (!trimmed) return;
    setSubmitting(true);
    try {
      await onSubmit(trimmed);
      setUrl("");
    } finally {
      setSubmitting(false);
    }
  };
  return (
    <form onSubmit={handle} className="flex items-center gap-1.5 pt-1 border-t">
      <input
        type="text"
        value={url}
        onChange={(e) => setUrl(e.target.value)}
        placeholder={t(($) => $.resources.url_placeholder)}
        className="flex-1 bg-transparent text-caption px-2 py-1 outline-none placeholder:text-muted-foreground"
      />
      <Button
        type="submit"
        size="sm"
        variant="ghost"
        className="h-6 px-2 text-caption"
        disabled={!url.trim() || submitting}
      >
        {t(($) => $.resources.url_submit)}
      </Button>
    </form>
  );
}
