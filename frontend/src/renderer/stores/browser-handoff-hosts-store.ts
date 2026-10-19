import { create } from "zustand";
import { parseExternalHandoffHosts } from "../../shared/browser-external-handoff";
import { DEFAULT_UI_SETTINGS } from "../../shared/ui-locale";
import { aoBridge } from "../lib/bridge";

type BrowserHandoffHostsState = {
	hosts: string[];
	loaded: boolean;
	saving: boolean;
	saveError: boolean;
	load: () => Promise<void>;
	setHosts: (hosts: string[]) => Promise<void>;
};

let settingRevision = 0;
let pendingLoad: Promise<void> | undefined;

export const useBrowserHandoffHostsStore = create<BrowserHandoffHostsState>((set, get) => ({
	hosts: DEFAULT_UI_SETTINGS.browserExternalHandoffHosts,
	loaded: false,
	saving: false,
	saveError: false,
	load: async () => {
		if (get().loaded) return;
		if (pendingLoad) return pendingLoad;
		const revisionAtStart = settingRevision;
		pendingLoad = (async () => {
			let hosts = DEFAULT_UI_SETTINGS.browserExternalHandoffHosts;
			try {
				const settings = await aoBridge.uiSettings.get();
				hosts = parseExternalHandoffHosts(settings.browserExternalHandoffHosts);
			} catch {
				// A missing bridge or unreadable setting must not block the panel:
				// the built-in account/payment hosts still hand off.
			}
			if (revisionAtStart === settingRevision) set({ hosts, loaded: true });
		})();
		try {
			await pendingLoad;
		} finally {
			pendingLoad = undefined;
		}
	},
	setHosts: async (candidate) => {
		const hosts = parseExternalHandoffHosts(candidate);
		const revision = ++settingRevision;
		set({ saving: true, saveError: false });
		try {
			await aoBridge.uiSettings.set({ browserExternalHandoffHosts: hosts });
			if (revision === settingRevision) set({ hosts, loaded: true, saving: false });
		} catch {
			if (revision === settingRevision) set({ saving: false, saveError: true });
		}
	},
}));