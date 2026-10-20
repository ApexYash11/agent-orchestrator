import { describe, expect, it } from "vitest";
import {
	normalizeHandoffHost,
	parseExternalHandoffHosts,
	shouldHandOffToSystemBrowser,
} from "./browser-external-handoff";

describe("normalizeHandoffHost", () => {
	it("accepts a bare host, a pasted URL, and mixed case", () => {
		expect(normalizeHandoffHost("SSO.Example.test")).toBe("sso.example.test");
		expect(normalizeHandoffHost("https://sso.example.test/login")).toBe("sso.example.test");
		expect(normalizeHandoffHost("sso.example.test:8443")).toBe("sso.example.test");
	});

	it("rejects anything that is not a host", () => {
		expect(normalizeHandoffHost("")).toBeNull();
		expect(normalizeHandoffHost("   ")).toBeNull();
		expect(normalizeHandoffHost("not a url")).toBeNull();
		expect(normalizeHandoffHost("http://")).toBeNull();
		expect(normalizeHandoffHost(42)).toBeNull();
	});
});

describe("parseExternalHandoffHosts", () => {
	it("drops invalid entries and duplicates while keeping order", () => {
		expect(parseExternalHandoffHosts(["b.test", "a.test", "B.TEST", "", "nope"])).toEqual([
			"b.test",
			"a.test",
		]);
	});

	it("returns an empty list for anything that is not a list", () => {
		expect(parseExternalHandoffHosts(undefined)).toEqual([]);
		expect(parseExternalHandoffHosts("a.test")).toEqual([]);
	});
});

describe("shouldHandOffToSystemBrowser", () => {
	it("matches the built-in account and payment hosts, including subdomains", () => {
		expect(shouldHandOffToSystemBrowser("https://accounts.google.com/signin/v2")).toBe(true);
		expect(shouldHandOffToSystemBrowser("https://pay.google.com/redirect")).toBe(true);
		expect(shouldHandOffToSystemBrowser("https://checkout.stripe.com/c/pay/cs_test")).toBe(true);
	});

	it("matches Google sign-in and checkout paths on any google host", () => {
		expect(shouldHandOffToSystemBrowser("https://workspace.google.com/intl/en/checkout/plan")).toBe(true);
		expect(shouldHandOffToSystemBrowser("https://workspace.google.com/signin")).toBe(true);
		expect(shouldHandOffToSystemBrowser("https://www.google.com/accounts/ServiceLogin")).toBe(true);
	});

	it("matches an OAuth authorization endpoint on any host", () => {
		expect(shouldHandOffToSystemBrowser("https://example.test/oauth/authorize?client_id=x")).toBe(true);
		expect(shouldHandOffToSystemBrowser("https://example.test/oauth2/v2/auth?client_id=x")).toBe(true);
	});

	it.each([
		"https://tenant.auth0.com/authorize?client_id=x",
		"https://login.microsoftonline.com/common/oauth2/v2.0/authorize?client_id=x",
		"https://sso.example.test/realms/team/protocol/openid-connect/authorize?client_id=x",
		"https://sso.example.test/tenant/oauth2/v2.0/auth?client_id=x",
	])("hands off authorization endpoint %s", (url) => {
		expect(shouldHandOffToSystemBrowser(url)).toBe(true);
	});

	it.each([
		"https://example.test/authorized",
		"https://example.test/preauthorize",
		"https://example.test/oauth2/v2.0/token",
		"https://example.test/docs?next=/authorize",
	])("keeps non-authorization URL %s in the panel", (url) => {
		expect(shouldHandOffToSystemBrowser(url)).toBe(false);
	});

	it("leaves ordinary pages alone", () => {
		expect(shouldHandOffToSystemBrowser("https://example.test/")).toBe(false);
		expect(shouldHandOffToSystemBrowser("https://news.google.com/topics/abc")).toBe(false);
		expect(shouldHandOffToSystemBrowser("https://example.test/checkout")).toBe(false);
	});

	it("does not let a look-alike host match", () => {
		expect(shouldHandOffToSystemBrowser("https://accounts.google.com.evil.test/")).toBe(false);
		expect(shouldHandOffToSystemBrowser("https://notaccounts.google.com/")).toBe(false);
	});

	it("ignores schemes a browser cannot open externally", () => {
		expect(shouldHandOffToSystemBrowser("about:blank")).toBe(false);
		expect(shouldHandOffToSystemBrowser("not a url")).toBe(false);
	});

	it("matches hosts the user configured", () => {
		expect(shouldHandOffToSystemBrowser("https://sso.example.test/login", ["sso.example.test"])).toBe(true);
		expect(shouldHandOffToSystemBrowser("https://other.example.test/", ["sso.example.test"])).toBe(false);
	});
});
