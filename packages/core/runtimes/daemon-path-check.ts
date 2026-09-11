import { api, ApiError } from "../api";
import type { DaemonPathCheckResponse } from "../types";

/**
 * The single seam between the local-directory picker and the daemon path-check
 * API (LOCO-171).
 *
 * Everything the UI needs to know about that endpoint lives here: the two
 * round trips, the poll loop, the HTTP-status-to-reason mapping and the
 * timeout. The picker calls `checkDaemonPath` and branches on the returned
 * reason — it never sees a status code, a request id or an ApiError. Keeping
 * the surface that narrow is deliberate: the server half shipped in parallel
 * with this client, so a contract correction is a change to this file and
 * nothing else.
 */

const POLL_INTERVAL_MS = 500;
// Generous enough to cover a daemon that only pops work on its next heartbeat,
// short enough that the confirm button does not spin past a user's patience.
// The server also enforces its own deadline and answers `timeout`; whichever
// fires first lands on the same `check_timed_out` reason.
const POLL_TIMEOUT_MS = 20_000;

/**
 * Why a path cannot be used, ready to map straight onto copy.
 *
 * The first five are the daemon's own vocabulary, shared verbatim with the
 * desktop bridge (`ValidateLocalDirectoryResult`) so one message table serves
 * both paths. The last three are transport-level facts only this module can
 * observe: the machine went away, nobody answered in time, or the caller does
 * not own that daemon.
 */
export type DaemonPathCheckFailure =
  | "not_absolute"
  | "not_found"
  | "not_a_directory"
  | "not_readable"
  | "not_writable"
  | "machine_offline"
  | "not_permitted"
  | "check_timed_out"
  | "error";

export interface DaemonPathCheckResult {
  ok: boolean;
  reason?: DaemonPathCheckFailure;
  /** Server-supplied detail, shown only when no reason maps to real copy. */
  error?: string;
  /**
   * Whether the path sits in a git working tree. Only meaningful when ok=true.
   * `undefined` means the daemon did not say, which callers must treat as
   * "unknown" rather than "not a repo" — the same rule as the desktop bridge.
   */
  isGitRepo?: boolean;
}

const DAEMON_REASONS = new Set<string>([
  "not_absolute",
  "not_found",
  "not_a_directory",
  "not_readable",
  "not_writable",
]);

/**
 * True for a path the daemon would accept as absolute.
 *
 * Checked here rather than only server-side because the answer costs nothing
 * and a typed path is the one input a user gets wrong constantly — a round trip
 * to learn "start with a /" is a worse dialog. The POSIX and Windows shapes are
 * both allowed: the daemon being asked may well be a Windows box, and rejecting
 * `C:\srv` locally would make that machine unreachable from the picker.
 */
export function isAbsoluteDaemonPath(path: string): boolean {
  if (path.startsWith("/")) return true;
  if (/^[A-Za-z]:[\\/]/.test(path)) return true;
  return path.startsWith("\\\\");
}

export interface CheckDaemonPathArgs {
  workspaceId: string;
  daemonId: string;
  path: string;
}

/**
 * Ask a daemon whether it can use `path`.
 *
 * Never throws: every failure comes back as a `reason`, because the caller is a
 * dialog that has to say something specific in every case. A thrown error there
 * would degrade to a generic toast, which is exactly the dead end this feature
 * exists to remove.
 */
export async function checkDaemonPath({
  workspaceId,
  daemonId,
  path,
}: CheckDaemonPathArgs): Promise<DaemonPathCheckResult> {
  const trimmed = path.trim();
  if (!trimmed || !isAbsoluteDaemonPath(trimmed)) {
    return { ok: false, reason: "not_absolute" };
  }

  let requestId: string;
  try {
    const created = await api.initiateDaemonPathCheck(
      workspaceId,
      daemonId,
      trimmed,
    );
    requestId = created.request_id;
  } catch (err) {
    return failureFromError(err);
  }
  // The api client answers a drifted 202 body with an empty request id rather
  // than throwing. Polling `.../path-checks/` would 404 and read as "you do
  // not own this daemon", so name it as a failed check instead.
  if (!requestId) return { ok: false, reason: "error" };

  const start = Date.now();
  let current: DaemonPathCheckResponse;
  try {
    current = await api.getDaemonPathCheck(workspaceId, daemonId, requestId);
    while (current.status === "pending") {
      if (Date.now() - start > POLL_TIMEOUT_MS) {
        return { ok: false, reason: "check_timed_out" };
      }
      await sleep(POLL_INTERVAL_MS);
      current = await api.getDaemonPathCheck(workspaceId, daemonId, requestId);
    }
  } catch (err) {
    return failureFromError(err);
  }

  if (current.status === "timeout") {
    return { ok: false, reason: "check_timed_out" };
  }
  if (current.status === "failed") {
    return failureFromOutcome(current, "error");
  }

  const result = current.result;
  if (!result) {
    // A completed check with no payload is a contract violation, not a verdict
    // about the path. Refuse rather than let an undefined bundle read as "fine".
    return { ok: false, reason: "error", error: current.error ?? undefined };
  }

  const objection = derivePathObjection(result);
  if (objection) return { ok: false, reason: objection };

  return { ok: true, isGitRepo: result.is_git_repo };
}

/**
 * The daemon reports both a `reason` and the individual booleans. Trust the
 * reason when it is one we know, and fall back to the booleans otherwise — an
 * older or newer daemon that leaves `reason` empty while reporting
 * `exists: false` still has to produce a named error, not a silent success.
 */
function derivePathObjection(result: {
  exists: boolean;
  is_directory: boolean;
  readable: boolean;
  writable: boolean;
  reason: string;
}): DaemonPathCheckFailure | null {
  if (DAEMON_REASONS.has(result.reason)) {
    return result.reason as DaemonPathCheckFailure;
  }
  if (!result.exists) return "not_found";
  if (!result.is_directory) return "not_a_directory";
  if (!result.readable) return "not_readable";
  if (!result.writable) return "not_writable";
  if (result.reason) return "error";
  return null;
}

function failureFromOutcome(
  response: DaemonPathCheckResponse,
  fallback: DaemonPathCheckFailure,
): DaemonPathCheckResult {
  const reason = response.result?.reason;
  if (reason && DAEMON_REASONS.has(reason)) {
    return { ok: false, reason: reason as DaemonPathCheckFailure };
  }
  return { ok: false, reason: fallback, error: response.error ?? undefined };
}

function failureFromError(err: unknown): DaemonPathCheckResult {
  if (err instanceof ApiError) {
    // 503 is the server saying the daemon has no online runtime to ask. It is
    // not a verdict about the path, and the copy has to say so — otherwise a
    // machine that went to sleep reads as a bad path.
    if (err.status === 503) return { ok: false, reason: "machine_offline" };
    // Ownership. 404 rather than 403 for a daemon the caller does not own is
    // the server refusing to confirm the daemon exists; either way the user's
    // answer is the same, so both land here.
    if (err.status === 403 || err.status === 404) {
      return { ok: false, reason: "not_permitted" };
    }
    const bodyReason = readBodyReason(err.body);
    if (bodyReason) return { ok: false, reason: bodyReason };
    return { ok: false, reason: "error", error: err.message };
  }
  return {
    ok: false,
    reason: "error",
    error: err instanceof Error ? err.message : undefined,
  };
}

/** A 4xx may carry the daemon vocabulary (e.g. a non-absolute path rejected at
 *  the API boundary). Use it when present so the inline error still names the
 *  actual problem instead of echoing an HTTP message. */
function readBodyReason(body: unknown): DaemonPathCheckFailure | null {
  if (!body || typeof body !== "object") return null;
  const candidate = (body as { reason?: unknown; code?: unknown }).reason ??
    (body as { code?: unknown }).code;
  if (typeof candidate !== "string") return null;
  return DAEMON_REASONS.has(candidate)
    ? (candidate as DaemonPathCheckFailure)
    : null;
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
