import {
	chmodSync,
	linkSync,
	mkdirSync,
	mkdtempSync,
	readdirSync,
	readFileSync,
	renameSync,
	rmSync,
	statSync,
	writeFileSync,
} from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { browserRuntimeKeyPath, resolveBrowserRuntimeToken, type BrowserRuntimeKeyFs } from "./browser-runtime-token";

const dirs: string[] = [];

const nodeIo: BrowserRuntimeKeyFs = {
	chmodSync: (target, mode) => chmodSync(target, mode),
	linkSync: (target, destination) => linkSync(target, destination),
	mkdirSync: (target, options) => mkdirSync(target, options),
	readFileSync: (target, encoding) => readFileSync(target, encoding),
	renameSync: (oldPath, newPath) => renameSync(oldPath, newPath),
	rmSync: (target, options) => rmSync(target, options),
	writeFileSync: (target, data, options) => writeFileSync(target, data, options),
};

/**
 * The real filesystem, with a hook that runs once, immediately after this launch
 * writes its candidate key and before the key path is published — the exact
 * window in which another launch can observe the key path. Node builtins cannot
 * be module-mocked in this setup, so the injection seam is the filesystem.
 */
function ioWithKeyWriteHook(onCandidateWritten: () => void): BrowserRuntimeKeyFs {
	let fired = false;
	return {
		...nodeIo,
		writeFileSync: (target, data, options) => {
			nodeIo.writeFileSync(target, data, options);
			if (fired) return;
			fired = true;
			onCandidateWritten();
		},
	};
}

function tempDataDir(): string {
	const dir = mkdtempSync(path.join(os.tmpdir(), "ao-runtime-token-"));
	dirs.push(dir);
	return path.join(dir, "data");
}

afterEach(() => {
	dirs.splice(0).forEach((dir) => rmSync(dir, { recursive: true, force: true }));
});

describe("resolveBrowserRuntimeToken", () => {
	it("returns the same token for every launch on one data dir", () => {
		const dataDir = tempDataDir();

		// Each call stands in for one app launch: the key persists, so a daemon
		// kept alive across restarts still accepts this app's hello.
		expect(resolveBrowserRuntimeToken(dataDir)).toBe(resolveBrowserRuntimeToken(dataDir));
	});

	it("keeps tokens distinct per data dir", () => {
		expect(resolveBrowserRuntimeToken(tempDataDir())).not.toBe(resolveBrowserRuntimeToken(tempDataDir()));
	});

	it("stores an owner-only key that is not the token itself", () => {
		const dataDir = tempDataDir();
		const token = resolveBrowserRuntimeToken(dataDir);
		const key = readFileSync(browserRuntimeKeyPath(dataDir), "utf8").trim();

		expect(key).not.toBe(token);
		expect(token.length).toBeGreaterThanOrEqual(43);
		if (process.platform !== "win32") {
			expect(statSync(browserRuntimeKeyPath(dataDir)).mode & 0o777).toBe(0o600);
		}
	});

	// The reported interleave: a second launch observes the key path while this
	// launch is still creating it, and the two ends up disagreeing about the key.
	it("stays consistent when a competing launch observes the key mid-creation", () => {
		const dataDir = tempDataDir();
		let competitor = "";
		const io = ioWithKeyWriteHook(() => {
			// Model the reader's view of the key path in that window, then let a
			// second launch resolve against it.
			writeFileSync(browserRuntimeKeyPath(dataDir), "partia", { mode: 0o600 });
			competitor = resolveBrowserRuntimeToken(dataDir);
		});
		const creator = resolveBrowserRuntimeToken(dataDir, io);

		expect(competitor).not.toBe("");
		// The creator must adopt whatever ended up on disk, not its own candidate.
		expect(creator).toBe(competitor);
		expect(resolveBrowserRuntimeToken(dataDir)).toBe(creator);
		expect(readFileSync(browserRuntimeKeyPath(dataDir), "utf8").trim()).toMatch(/^[A-Za-z0-9_-]{43}$/);
	});

	it("keeps two launches consistent when the key path already exists", () => {
		const dataDir = tempDataDir();
		resolveBrowserRuntimeToken(dataDir);
		const keyBefore = readFileSync(browserRuntimeKeyPath(dataDir), "utf8");

		const competitor = resolveBrowserRuntimeToken(dataDir, ioWithKeyWriteHook(() => undefined));
		expect(competitor).toBe(resolveBrowserRuntimeToken(dataDir));
		expect(readFileSync(browserRuntimeKeyPath(dataDir), "utf8")).toBe(keyBefore);
	});

	it("leaves no temporary files behind when a competitor wins", () => {
		const dataDir = tempDataDir();
		resolveBrowserRuntimeToken(dataDir, ioWithKeyWriteHook(() => resolveBrowserRuntimeToken(dataDir)));

		expect(readdirSync(dataDir).filter((entry) => entry.startsWith("browser-runtime.key."))).toEqual([]);
	});

	it("replaces a corrupt key instead of propagating it", () => {
		const dataDir = tempDataDir();
		const dataRoot = path.dirname(browserRuntimeKeyPath(dataDir));
		// Simulate a truncated file: the previous token is unrecoverable either way.
		resolveBrowserRuntimeToken(dataDir);
		writeFileSync(browserRuntimeKeyPath(dataDir), "not-a-key\n", { mode: 0o600 });
		chmodSync(browserRuntimeKeyPath(dataDir), 0o600);

		const repaired = resolveBrowserRuntimeToken(dataDir);
		expect(resolveBrowserRuntimeToken(dataDir)).toBe(repaired);
		expect(readFileSync(browserRuntimeKeyPath(dataDir), "utf8").trim()).toMatch(/^[A-Za-z0-9_-]{43}$/);
		expect(statSync(dataRoot).isDirectory()).toBe(true);
	});
});