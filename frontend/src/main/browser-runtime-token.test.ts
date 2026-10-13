import { chmodSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { browserRuntimeKeyPath, resolveBrowserRuntimeToken } from "./browser-runtime-token";

const dirs: string[] = [];

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

	it("reuses the key a concurrent launch already created", () => {
		const dataDir = tempDataDir();
		const first = resolveBrowserRuntimeToken(dataDir);
		const key = readFileSync(browserRuntimeKeyPath(dataDir), "utf8");

		// Simulate losing the exclusive-create race: the key is present, so no
		// second launch may overwrite it with a token the daemon does not know.
		expect(resolveBrowserRuntimeToken(dataDir)).toBe(first);
		expect(readFileSync(browserRuntimeKeyPath(dataDir), "utf8")).toBe(key);
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