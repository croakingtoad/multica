import type {
  SharedStateMutationGuard,
  SharedStateMutationTarget,
} from "@multica/core/api";
import {
  BUILD_CHANNEL,
  SHARED_STATE_COMPAT,
  type BuildChannelName,
  type SharedStateCompatibility,
} from "../../shared/build-channel";

type ConfirmWarning = (message: string) => boolean;

export function sacrificialTargetWarning(
  target: SharedStateMutationTarget,
): string {
  const workspace = target.workspaceSlug ?? "none (backend-wide request)";
  return [
    "This Multica Dev build was declared incompatible with shared state.",
    "",
    "Writes are blocked to protect data used by the stable app. Continue only if this target is disposable:",
    `Backend: ${target.backendUrl}`,
    `Workspace: ${workspace}`,
    "",
    "Select OK to mark this exact target as sacrificial and unlock writes until this window closes.",
  ].join("\n");
}

export function createSharedStateMutationGuard(
  channel: BuildChannelName,
  compatibility: SharedStateCompatibility,
  confirmWarning: ConfirmWarning,
): SharedStateMutationGuard | undefined {
  if (channel !== "dev" || compatibility !== "breaking") return undefined;
  return {
    confirmTarget: (target) => confirmWarning(sacrificialTargetWarning(target)),
  };
}

export const DESKTOP_SHARED_STATE_MUTATION_GUARD =
  createSharedStateMutationGuard(
    BUILD_CHANNEL,
    SHARED_STATE_COMPAT,
    (message) => window.confirm(message),
  );
