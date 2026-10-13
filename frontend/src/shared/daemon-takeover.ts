// Pure decision helper for the wedged-orphan kill+replace path.
//
// Context: on app launch, after both attach attempts fail (inspectExistingDaemon
// and resolveDaemonFromPort both returned null/non-ready), a process may still
// be holding the daemon port. Spawning a new daemon then makes the Go child
// collide on the port and exit 1. This helper encodes the decision: kill the
// holder when the run-file names a PID that is still alive (a hung/wedged holder
// that bound the port but is not answering /healthz). The probe disjunct
// (probe !== null) is kept as a defensive guard only: by the time this helper
// is called, resolveDaemonFromPort already returned null, so a holder that
// answers /healthz at this point is unexpected. If it does happen (e.g. a race),
// we still replace it rather than colliding on spawn.
//
// Kept side-effect free and dependency-injected (no node:* or electron imports)
// so it can be exercised in vitest without the Electron polyfill layer.

import type { DaemonProbe } from "./daemon-attach";

/**
 * Reports whether something is holding the daemon port that we must kill before
 * spawning. By the time it is called, both healthy-reuse attach paths have already
 * returned null. The primary trigger is holderPidAlive: the run-file names a PID
 * that is still alive but is not answering /healthz (a hung/wedged holder). The
 * probe disjunct is a defensive guard: a holder that answers at this point is
 * unexpected (resolveDaemonFromPort already returned null), but if it does appear,
 * we replace it rather than colliding on spawn.
 *
 * Returns true when the caller should kill the holder, wait for the port to
 * free, clear the stale run-file, then spawn a fresh daemon.
 *
 * Returns false when there is no detectable holder; spawn immediately.
 *
 * ponytail: two-condition OR covers the entire decision surface; the probe's
 * content (pid, executablePath) is for the caller's kill logic, not ours.
 */
export function shouldReplacePortHolder(probe: DaemonProbe | null, holderPidAlive: boolean): boolean {
	return probe !== null || holderPidAlive;
}

export type BrowserDaemonOwnershipDecision =
	| { action: "attach" }
	| { action: "replace"; keepAlive: boolean };

/**
 * A daemon belongs to the Electron launch that reported its app run id: reuse
 * within that launch is safe, and across launches we replace it gracefully,
 * preserving the original persistence mode (app-owned daemons remain app-owned;
 * persistent/headless daemons remain alive after the window closes).
 *
 * This is a daemon ownership/lifecycle decision, not an authentication one. The
 * browser runtime token is derived from the data dir and survives an app restart
 * (see browser-runtime-token.ts), so a kept-alive daemon still accepts this app
 * after a relaunch; replacing it no longer exists to repair a token mismatch.
 */
export function browserDaemonOwnershipDecision(
	currentAppRunId: string,
	existing: { owner?: string; appRunId?: string },
): BrowserDaemonOwnershipDecision {
	if (existing.appRunId === currentAppRunId) return { action: "attach" };
	return { action: "replace", keepAlive: existing.owner !== "app" };
}

export type BrowserRuntimeRecoveryDecision =
	| { action: "wait" }
	| { action: "replace"; reason: string };

/**
 * A daemon the app merely adopted holds the runtime token from whoever started
 * it, so a refusal cannot be retried away: it means this daemon can never drive
 * our browser panel for the rest of the launch. Retiring it and spawning our own
 * is the only recovery, and it is limited to that unrecoverable case.
 *
 * Waits when the app spawned the daemon itself (our token was handed to it, so a
 * refusal means something is wrong that a restart would not fix), when the
 * refusal is not a token mismatch (a version skew resolves on the next launch,
 * not by killing a healthy daemon), and after one attempt per launch so a daemon
 * that keeps refusing cannot be replaced in a loop.
 */
export function browserRuntimeRecoveryDecision(input: {
	rejectedReason: string | null;
	spawnedHere: boolean;
	attempted: boolean;
}): BrowserRuntimeRecoveryDecision {
	if (input.rejectedReason !== "invalid-token") return { action: "wait" };
	if (input.spawnedHere) return { action: "wait" };
	if (input.attempted) return { action: "wait" };
	return { action: "replace", reason: input.rejectedReason ?? "invalid-token" };
}
