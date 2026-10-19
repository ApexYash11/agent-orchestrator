import { describe, expect, it } from "vitest";
import { browserPanelUserAgent } from "./browser-user-agent";

describe("browserPanelUserAgent", () => {
	it("drops the Electron and app tokens and keeps Chromium's own version", () => {
		expect(
			browserPanelUserAgent(
				"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.7559.65 Safari/537.36 Electron/44.7.0 AgentOrchestrator/0.13.0",
			),
		).toBe(
			"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.7559.65 Safari/537.36",
		);
	});

	it("leaves an agent string that names no embedding host untouched", () => {
		const ua = "Mozilla/5.0 (Macintosh) AppleWebKit/537.36 Chrome/144.0.7559.65 Safari/537.36";
		expect(browserPanelUserAgent(ua)).toBe(ua);
	});

	it("strips the production and development app-name tokens from Electron's fallback UA", () => {
		const production = browserPanelUserAgent(
			"Mozilla/5.0 (Macintosh) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36 Electron/44.7.0 Orchestrator.inc/0.13.0",
		);
		const development = browserPanelUserAgent(
			"Mozilla/5.0 (Macintosh) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36 Electron/44.7.0 Orchestrator.inc (dev)/0.13.0",
		);
		expect(production).not.toContain("Orchestrator.inc");
		expect(production).not.toContain("Electron");
		expect(development).not.toContain("Orchestrator.inc");
		expect(development).not.toContain("Electron");
	});

	it("does not claim to be Google Chrome", () => {
		const result = browserPanelUserAgent("Mozilla/5.0 Chrome/144.0.0.0 Safari/537.36 Electron/44.7.0");
		expect(result).not.toContain("Electron");
		expect(result).not.toContain("Google Chrome");
	});
});