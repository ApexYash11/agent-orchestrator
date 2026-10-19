import { beforeEach, describe, expect, it, vi } from "vitest";

const getUiSettings = vi.fn();
const setUiSettings = vi.fn();

vi.mock("../lib/bridge", () => ({
	aoBridge: {
		uiSettings: {
			get: (...args: unknown[]) => getUiSettings(...args),
			set: (...args: unknown[]) => setUiSettings(...args),
		},
	},
}));

import { useBrowserHandoffHostsStore } from "./browser-handoff-hosts-store";

describe("browser-handoff-hosts-store", () => {
	beforeEach(() => {
		getUiSettings.mockReset();
		setUiSettings.mockReset();
		getUiSettings.mockResolvedValue({ browserExternalHandoffHosts: [] });
		setUiSettings.mockImplementation(async (settings) => settings);
		useBrowserHandoffHostsStore.setState({ hosts: [], loaded: false, saving: false, saveError: false });
	});

	it("loads the persisted handoff hosts", async () => {
		getUiSettings.mockResolvedValue({ browserExternalHandoffHosts: ["SSO.test"] });

		await useBrowserHandoffHostsStore.getState().load();

		expect(useBrowserHandoffHostsStore.getState()).toMatchObject({ hosts: ["sso.test"], loaded: true });
	});

	it("persists a normalized host list", async () => {
		await useBrowserHandoffHostsStore.getState().setHosts(["SSO.test", "not a host", "sso.test"]);

		expect(setUiSettings).toHaveBeenCalledWith({ browserExternalHandoffHosts: ["sso.test"] });
		expect(useBrowserHandoffHostsStore.getState()).toMatchObject({ hosts: ["sso.test"], saveError: false });
	});

	it("keeps the current hosts when persistence fails", async () => {
		setUiSettings.mockRejectedValue(new Error("disk full"));

		await useBrowserHandoffHostsStore.getState().setHosts(["sso.test"]);

		expect(useBrowserHandoffHostsStore.getState()).toMatchObject({ hosts: [], saveError: true });
	});
});