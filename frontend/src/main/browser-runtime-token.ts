import { createHmac, randomBytes } from "node:crypto";
import { chmodSync, mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import path from "node:path";

// Owner-only key under the AO data dir. The daemon's browser runtime token is
// derived from it rather than stored directly, so the file is a key and not a
// replayable credential, and it lives beside the other owner-only AO secrets
// (cloud auth tokens, remotes credentials, telemetry authority).
const KEY_FILE = "browser-runtime.key";
const KEY_BYTES = 32;
const KEY_PATTERN = /^[A-Za-z0-9_-]{43}$/;
const DERIVATION_LABEL = "ao-browser-runtime-token-v1";

export function browserRuntimeKeyPath(dataDir: string): string {
	return path.join(dataDir, KEY_FILE);
}

/**
 * The token the desktop app presents to the daemon's browser runtime broker.
 *
 * It must survive an app restart: AO_KEEP_DAEMON (and a wrapper that pins
 * AO_APP_RUN_ID) lets a daemon outlive the Electron process that spawned it, and
 * the next launch adopts that daemon instead of replacing it. A per-launch
 * random token made such an adoption permanently unauthenticatable — the daemon
 * held the previous launch's token — so the browser panel could never come back
 * for the life of the app. Deriving the token from a per-data-dir key lets an
 * adopted daemon recognize its own app across restarts.
 *
 * Never place the result in argv or the daemon environment; it is handed over
 * through the child's private stdin pipe.
 */
export function resolveBrowserRuntimeToken(dataDir: string): string {
	return createHmac("sha256", readOrCreateKey(dataDir)).update(DERIVATION_LABEL).digest("base64url");
}

/**
 * Read the data dir's key, creating it on first use. Concurrent launches (two
 * app instances on one data dir) race here, so creation is exclusive and a lost
 * race re-reads the winner's key instead of overwriting it.
 */
function readOrCreateKey(dataDir: string): Buffer {
	mkdirSync(dataDir, { recursive: true, mode: 0o750 });
	const keyPath = browserRuntimeKeyPath(dataDir);
	const existing = readKey(keyPath);
	if (existing) return existing;
	const created = randomBytes(KEY_BYTES).toString("base64url");
	try {
		writeFileSync(keyPath, `${created}\n`, { mode: 0o600, flag: "wx" });
		chmodSync(keyPath, 0o600);
		return Buffer.from(created, "base64url");
	} catch (error) {
		if ((error as NodeJS.ErrnoException).code !== "EEXIST") throw error;
		const raced = readKey(keyPath);
		if (raced) return raced;
		// The key file exists but is unusable, so it holds no token any daemon
		// could be expecting. Replace it atomically instead of leaving the app
		// unable to derive one.
		const temporary = `${keyPath}.tmp-${process.pid}`;
		writeFileSync(temporary, `${created}\n`, { mode: 0o600 });
		chmodSync(temporary, 0o600);
		renameSync(temporary, keyPath);
		return Buffer.from(created, "base64url");
	}
}

// A truncated or corrupt key is replaced rather than propagated: a wrong key
// only produces another daemon-side token, and the app has no way to recover the
// previous one. Replacing keeps the app usable after a damaged file.
function readKey(keyPath: string): Buffer | null {
	let raw: string;
	try {
		raw = readFileSync(keyPath, "utf8");
	} catch {
		return null;
	}
	const trimmed = raw.trim();
	if (!KEY_PATTERN.test(trimmed)) return null;
	const decoded = Buffer.from(trimmed, "base64url");
	return decoded.length === KEY_BYTES ? decoded : null;
}