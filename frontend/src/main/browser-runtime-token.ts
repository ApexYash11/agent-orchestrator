import { createHmac, randomBytes } from "node:crypto";
import {
	chmodSync,
	linkSync,
	mkdirSync,
	readFileSync,
	renameSync,
	rmSync,
	writeFileSync,
	type PathLike,
} from "node:fs";
import path from "node:path";

// Owner-only key under the AO data dir. The daemon's browser runtime token is
// derived from it rather than stored directly, so the file is a key and not a
// replayable credential, and it lives beside the other owner-only AO secrets
// (cloud auth tokens, remotes credentials, telemetry authority).
const KEY_FILE = "browser-runtime.key";
const KEY_BYTES = 32;
const KEY_PATTERN = /^[A-Za-z0-9_-]{43}$/;
const DERIVATION_LABEL = "ao-browser-runtime-token-v1";
// One publish attempt each for first use, a lost create race, and a damaged key.
const PUBLISH_ATTEMPTS = 3;

/**
 * The filesystem surface this module needs, injected so the publish protocol can
 * be exercised against a real disk with an interleaving forced at a chosen point.
 */
export type BrowserRuntimeKeyFs = {
	chmodSync: (target: PathLike, mode: number) => void;
	linkSync: (target: PathLike, destination: PathLike) => void;
	mkdirSync: (target: PathLike, options: { recursive: true; mode: number }) => void;
	readFileSync: (target: PathLike, encoding: "utf8") => string;
	renameSync: (oldPath: PathLike, newPath: PathLike) => void;
	rmSync: (target: PathLike, options: { force: true }) => void;
	writeFileSync: (
		target: PathLike,
		data: string,
		options: { mode: number },
	) => void;
};

const nodeKeyFs: BrowserRuntimeKeyFs = {
	chmodSync,
	linkSync,
	mkdirSync,
	readFileSync,
	renameSync,
	rmSync,
	writeFileSync,
};

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
export function resolveBrowserRuntimeToken(dataDir: string, io: BrowserRuntimeKeyFs = nodeKeyFs): string {
	return createHmac("sha256", readOrCreateKey(dataDir, io)).update(DERIVATION_LABEL).digest("base64url");
}

function readOrCreateKey(dataDir: string, io: BrowserRuntimeKeyFs): Buffer {
	io.mkdirSync(dataDir, { recursive: true, mode: 0o750 });
	const keyPath = browserRuntimeKeyPath(dataDir);
	for (let attempt = 0; attempt < PUBLISH_ATTEMPTS; attempt += 1) {
		const existing = readKey(keyPath, io);
		if (existing) return existing;
		const published = publishKey(keyPath, io);
		if (published) return published;
		// Another launch published or repaired a key first: re-read on the next pass.
	}
	const raced = readKey(keyPath, io);
	if (raced) return raced;
	throw new Error(`could not establish a browser runtime key at ${keyPath}`);
}

/**
 * Publish a candidate key atomically, without overwriting one another launch
 * already published.
 *
 * The candidate is written to a private temporary file and published with
 * link(), which fails when the destination exists. Writing the key in place with
 * an exclusive create would expose a truncated file to a concurrent reader,
 * which would then "repair" it and leave the creator holding a token that is not
 * the one on disk. link() needs a filesystem with hard-link support (NTFS, APFS,
 * ext4 and friends); where it is unavailable the caller falls back to a
 * per-launch token rather than using a weaker publish.
 *
 * Returns null when another launch owns the key path, or when a damaged key had
 * to be moved aside first, so the caller retries.
 */
function publishKey(keyPath: string, io: BrowserRuntimeKeyFs): Buffer | null {
	const candidate = randomBytes(KEY_BYTES).toString("base64url");
	const temporary = `${keyPath}.tmp-${process.pid}-${randomBytes(6).toString("hex")}`;
	io.writeFileSync(temporary, `${candidate}\n`, { mode: 0o600 });
	try {
		io.linkSync(temporary, keyPath);
	} catch (error) {
		io.rmSync(temporary, { force: true });
		if ((error as NodeJS.ErrnoException).code !== "EEXIST") throw error;
		const raced = readKey(keyPath, io);
		if (raced) return raced;
		displaceUnusableKey(keyPath, io);
		return null;
	}
	io.rmSync(temporary, { force: true });
	io.chmodSync(keyPath, 0o600);
	return Buffer.from(candidate, "base64url");
}

/**
 * A published key is always complete, so a file that will not parse was damaged
 * and holds no token any daemon could be expecting. Rename is atomic and only
 * one racer can win it, so the winner retries the publish and the others read
 * whatever the winner wrote.
 */
function displaceUnusableKey(keyPath: string, io: BrowserRuntimeKeyFs): void {
	const displaced = `${keyPath}.unusable-${randomBytes(6).toString("hex")}`;
	try {
		io.renameSync(keyPath, displaced);
	} catch {
		// Another launch repaired it first; nothing to move aside.
		return;
	}
	io.rmSync(displaced, { force: true });
}

function readKey(keyPath: string, io: BrowserRuntimeKeyFs): Buffer | null {
	let raw: string;
	try {
		raw = io.readFileSync(keyPath, "utf8");
	} catch {
		return null;
	}
	const trimmed = raw.trim();
	if (!KEY_PATTERN.test(trimmed)) return null;
	const decoded = Buffer.from(trimmed, "base64url");
	return decoded.length === KEY_BYTES ? decoded : null;
}