"use client";

import { useMemo, useState } from "react";
import { FolderOpen, Monitor, TriangleAlert } from "lucide-react";
import type { AgentRuntime } from "@multica/core/types";
import {
  checkDaemonPath,
  type DaemonPathCheckFailure,
} from "@multica/core/runtimes/daemon-path-check";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { buildRuntimeMachines } from "../../runtimes/components/runtime-machines";
import {
  isDesktopShell,
  pickDirectory,
  validateLocalDirectory,
} from "../../platform";
import { useT, useTimeAgo } from "../../i18n";

/**
 * Machine + path picker for a `local_directory` project resource.
 *
 * Replaces a flow that could only ever attach a directory on the machine the
 * browser itself was running on (LOCO-171). Browser locality was never the
 * right question: the server already knows every daemon the user owns, so the
 * user picks one of those and the DAEMON answers whether the path is usable.
 * Only the browser-local machine keeps the native folder dialog, because only
 * there can a native dialog show the right filesystem.
 *
 * The dialog stops at a validated (machine, path) pair. Execution mode is the
 * next question and belongs to `LocalDirectoryModeDialog`, which already models
 * the "not a git repo" blocker this check feeds it.
 */

/** One selectable machine. A machine with no `daemonId` cannot be addressed by
 *  the path-check API or stored in a resource ref, so it never reaches here. */
export interface LocalDirectoryMachine {
  id: string;
  daemonId: string;
  title: string;
  /** OS / arch / host line, already formatted by the runtime machine builder. */
  subtitle: string | null;
  lastSeenAt: string | null;
  isCurrent: boolean;
}

export interface LocalDirectorySelection {
  daemonId: string;
  path: string;
  /** Folder basename when the native dialog supplied one, else the path. */
  label: string;
  /** `undefined` when the check could not tell — never treat it as "not a repo". */
  isGitRepo: boolean | undefined;
}

interface EligibleMachineOptions {
  now: number;
  /** The viewing user. Nothing is listed when this is unknown. */
  currentUserId: string | null;
  /** Set on desktop, where a daemon runs beside the browser. */
  localDaemonId: string | null;
  localMachineName: string | null;
}

/**
 * The machines a user may attach a directory on: their own, not cloud, online.
 *
 * Ownership is filtered BEFORE the machines are built, not after. The runtime
 * list is workspace-wide, and grouping first would let another member's
 * identically-named host merge into one of the user's machines — the row would
 * then look like theirs and its daemon id would not be. The path-check API
 * refuses a daemon the caller does not own, so such a row could only ever
 * produce a permission error; the honest thing is for it never to appear.
 *
 * Cloud runtimes are excluded because there is no user filesystem to attach,
 * and offline machines because nothing would answer the path check.
 */
export function eligibleLocalDirectoryMachines(
  runtimes: AgentRuntime[],
  options: EligibleMachineOptions,
): LocalDirectoryMachine[] {
  const { currentUserId } = options;
  if (!currentUserId) return [];

  const mine = runtimes.filter((runtime) => runtime.owner_id === currentUserId);

  return buildRuntimeMachines(mine, {
    now: options.now,
    currentUserId,
    localDaemonId: options.localDaemonId,
    localMachineName: options.localMachineName,
  })
    .flatMap((machine) => {
      if (machine.section === "cloud" || machine.health !== "online") return [];
      const daemonId = machine.daemonId;
      if (!daemonId) return [];
      return [
        {
          id: machine.id,
          daemonId,
          title: machine.title,
          subtitle: machine.subtitle,
          lastSeenAt: machine.lastSeenAt,
          isCurrent: machine.isCurrent,
        },
      ];
    });
}

interface LocalDirectoryPickerDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  workspaceId: string;
  /** Already filtered and ordered — `isCurrent` first. */
  machines: LocalDirectoryMachine[];
  /** Daemons that already carry a local_directory for this project. The server
   *  allows one per (project, daemon), so those rows are shown but blocked. */
  attachedDaemonIds: Set<string>;
  onSelected: (selection: LocalDirectorySelection) => void;
}

export function LocalDirectoryPickerDialog({
  open,
  onOpenChange,
  workspaceId,
  machines,
  attachedDaemonIds,
  onSelected,
}: LocalDirectoryPickerDialogProps) {
  const { t } = useT("projects");
  const timeAgo = useTimeAgo();
  const failureMessage = useLocalDirectoryFailureMessage();

  const selectable = useMemo(
    () => machines.filter((machine) => !attachedDaemonIds.has(machine.daemonId)),
    [machines, attachedDaemonIds],
  );

  // Preselect the first machine the user can actually use. `isCurrent` sorts
  // first upstream, so on desktop that is "this machine" — the old flow's only
  // option — and elsewhere it is whichever online machine leads the list.
  //
  // Mounted only while open (see ProjectResourcesSection), so this initial
  // state IS the per-open reset: a path typed for one machine's filesystem can
  // never survive into the next attempt.
  const [selectedId, setSelectedId] = useState<string | null>(
    () => selectable[0]?.id ?? null,
  );
  const [path, setPath] = useState("");
  const [pickedLabel, setPickedLabel] = useState<string | null>(null);
  const [checking, setChecking] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const selected = machines.find((machine) => machine.id === selectedId) ?? null;
  // Only the browser-local machine can use the native dialog, and only inside
  // the desktop shell — on web there is no bridge to open one with.
  const useNativeDialog = selected?.isCurrent === true && isDesktopShell();

  const handlePickNative = async () => {
    setError(null);
    const picked = await pickDirectory();
    if (!picked.ok) {
      if (picked.reason && picked.reason !== "cancelled") {
        setError(
          picked.reason === "unsupported"
            ? failureMessage("unsupported")
            : failureMessage("error", picked.error),
        );
      }
      return;
    }
    setPath(picked.path ?? "");
    setPickedLabel(picked.basename ?? null);
  };

  const handleConfirm = async () => {
    if (!selected || checking) return;
    const candidate = path.trim();
    if (!candidate) {
      setError(failureMessage("not_absolute"));
      return;
    }

    setChecking(true);
    setError(null);
    try {
      // The local machine keeps the desktop bridge: it already answers the same
      // question without a round trip, and it is the only validator available
      // before the picked path has ever been sent anywhere.
      if (useNativeDialog) {
        const validation = await validateLocalDirectory(candidate);
        if (!validation.ok) {
          // Reason before detail, matching the table's own precedence: a named
          // filesystem objection always outranks whatever string came with it.
          setError(failureMessage(validation.reason, validation.error));
          return;
        }
        onSelected({
          daemonId: selected.daemonId,
          path: candidate,
          label: pickedLabel ?? candidate,
          isGitRepo: validation.is_git_repo,
        });
        return;
      }

      const result = await checkDaemonPath({
        workspaceId,
        daemonId: selected.daemonId,
        path: candidate,
      });
      if (!result.ok) {
        setError(failureMessage(result.reason, result.error));
        return;
      }
      onSelected({
        daemonId: selected.daemonId,
        path: candidate,
        label: basename(candidate) ?? candidate,
        isGitRepo: result.isGitRepo,
      });
    } finally {
      setChecking(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t(($) => $.resources.local_picker_title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.resources.local_picker_description)}
          </DialogDescription>
        </DialogHeader>

        <div
          role="radiogroup"
          aria-label={t(($) => $.resources.local_picker_machines_label)}
          className="flex max-h-56 flex-col gap-1.5 overflow-y-auto"
        >
          {machines.map((machine) => {
            const taken = attachedDaemonIds.has(machine.daemonId);
            return (
              <MachineOption
                key={machine.id}
                machine={machine}
                selected={machine.id === selectedId}
                disabled={taken || checking}
                blockedReason={
                  taken
                    ? t(($) => $.resources.local_picker_machine_attached)
                    : undefined
                }
                lastSeenLabel={
                  machine.lastSeenAt
                    ? t(($) => $.resources.local_picker_last_seen, {
                        when: timeAgo(machine.lastSeenAt),
                      })
                    : null
                }
                thisMachineLabel={t(
                  ($) => $.resources.local_picker_this_machine,
                )}
                onSelect={() => {
                  setSelectedId(machine.id);
                  setPath("");
                  setPickedLabel(null);
                  setError(null);
                }}
              />
            );
          })}
        </div>

        {selected && (
          <div className="space-y-1.5">
            <span className="block text-caption font-medium">
              {t(($) => $.resources.local_picker_path_label)}
            </span>
            {useNativeDialog ? (
              <div className="flex items-center gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={checking}
                  onClick={() => void handlePickNative()}
                >
                  <FolderOpen className="size-3.5" />
                  {path
                    ? t(($) => $.resources.local_picker_change_folder)
                    : t(($) => $.resources.local_picker_browse)}
                </Button>
                <span className="min-w-0 flex-1 truncate font-mono text-micro text-muted-foreground">
                  {path || t(($) => $.resources.local_picker_no_folder)}
                </span>
              </div>
            ) : (
              <>
                <Input
                  value={path}
                  onChange={(event) => {
                    setPath(event.target.value);
                    setError(null);
                  }}
                  disabled={checking}
                  spellCheck={false}
                  autoComplete="off"
                  className="font-mono text-caption"
                  aria-label={t(($) => $.resources.local_picker_path_label)}
                  placeholder={t(
                    ($) => $.resources.local_picker_path_placeholder,
                  )}
                />
                <p className="text-micro text-muted-foreground">
                  {t(($) => $.resources.local_picker_path_hint, {
                    machine: selected.title,
                  })}
                </p>
              </>
            )}
          </div>
        )}

        {machines.length === 0 && (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.resources.local_daemon_offline_hint)}
          </p>
        )}

        {error && (
          <div
            role="alert"
            className="flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-caption text-destructive"
          >
            <TriangleAlert className="size-3.5 mt-0.5 shrink-0" />
            <span>{error}</span>
          </div>
        )}

        <DialogFooter>
          <Button
            variant="ghost"
            onClick={() => onOpenChange(false)}
            disabled={checking}
          >
            {t(($) => $.resources.mode_cancel)}
          </Button>
          <Button
            onClick={() => void handleConfirm()}
            disabled={checking || !selected || !path.trim()}
          >
            {checking
              ? t(($) => $.resources.local_picker_checking)
              : t(($) => $.resources.local_picker_continue)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

interface MachineOptionProps {
  machine: LocalDirectoryMachine;
  selected: boolean;
  disabled: boolean;
  blockedReason?: string;
  lastSeenLabel: string | null;
  thisMachineLabel: string;
  onSelect: () => void;
}

function MachineOption({
  machine,
  selected,
  disabled,
  blockedReason,
  lastSeenLabel,
  thisMachineLabel,
  onSelect,
}: MachineOptionProps) {
  const meta = [machine.subtitle, lastSeenLabel].filter(Boolean).join(" · ");
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      disabled={disabled}
      onClick={onSelect}
      // Selection carried by border + ring, not a background tint, so it stays
      // readable under hover — same rule as the execution-mode options.
      className={`flex w-full items-start gap-3 rounded-lg border p-2.5 text-left transition-colors ${
        selected
          ? "border-primary ring-1 ring-primary"
          : "border-border hover:bg-muted/50"
      } ${disabled ? "cursor-not-allowed opacity-60" : ""}`}
    >
      <Monitor
        className={`mt-0.5 size-4 shrink-0 ${
          selected ? "text-primary" : "text-muted-foreground"
        }`}
      />
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-2">
          <span className="truncate text-body font-medium">{machine.title}</span>
          {machine.isCurrent && (
            <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-micro font-medium text-muted-foreground">
              {thisMachineLabel}
            </span>
          )}
        </span>
        {meta && (
          <span className="mt-0.5 block truncate text-caption text-muted-foreground">
            {meta}
          </span>
        )}
        {blockedReason && (
          <span className="mt-1 block text-caption text-warning">
            {blockedReason}
          </span>
        )}
      </span>
    </button>
  );
}

/**
 * One message table for both validators.
 *
 * The daemon and the desktop bridge answer in the same vocabulary by design, so
 * the five filesystem reasons reuse the existing `local_validate_*` copy
 * verbatim. Only the states that did not exist before the check became remote —
 * the machine going offline, nobody answering in time, a daemon that is not
 * yours — needed new strings.
 */
type LocalDirectoryFailure =
  | DaemonPathCheckFailure
  | "unsupported"
  | undefined;

function useLocalDirectoryFailureMessage(): (
  reason: LocalDirectoryFailure,
  detail?: string,
) => string {
  const { t } = useT("projects");
  return (reason, detail) => {
    switch (reason) {
      case "not_absolute":
        return t(($) => $.resources.local_validate_not_absolute);
      case "not_found":
        return t(($) => $.resources.local_validate_not_found);
      case "not_a_directory":
        return t(($) => $.resources.local_validate_not_a_directory);
      case "not_readable":
        return t(($) => $.resources.local_validate_not_readable);
      case "not_writable":
        return t(($) => $.resources.local_validate_not_writable);
      case "machine_offline":
        return t(($) => $.resources.local_validate_machine_offline);
      case "check_timed_out":
        return t(($) => $.resources.local_validate_check_timed_out);
      case "not_permitted":
        return t(($) => $.resources.local_validate_not_permitted);
      case "unsupported":
        return t(($) => $.resources.local_validate_unsupported);
      default:
        return detail ?? t(($) => $.resources.toast_local_pick_failed);
    }
  };
}

/** Last path segment, for the resource's default label. Handles both
 *  separators because the daemon may be on Windows, and tolerates a trailing
 *  one (`/srv/app/` should still read "app"). */
function basename(path: string): string | null {
  const parts = path.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] ?? null;
}
