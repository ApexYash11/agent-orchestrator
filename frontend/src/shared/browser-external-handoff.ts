/**
 * Sign-in, OAuth and checkout flows that providers refuse to run inside an
 * embedded browser view.
 *
 * Google deliberately blocks authentication and payment pages in embedded
 * webviews, so the AO Browser panel cannot complete them no matter how it is
 * configured (#6622). The supported fix is the one VS Code, Slack and Linear
 * use: stop loading the page in the panel and hand the exact URL to the
 * user's own browser, which is not subject to that restriction.
 *
 * This module only decides *whether* a URL belongs in the user's browser. It
 * performs no request of its own and fakes nothing: pages that stay in the
 * panel keep Chromium's real identity.
 */

/** Hosts whose every page is an account or payment surface. */
export const DEFAULT_EXTERNAL_HANDOFF_HOSTS: readonly string[] = [
  "accounts.google.com",
  "myaccount.google.com",
  "pay.google.com",
  "checkout.google.com",
  "checkout.stripe.com",
  "www.paypal.com",
];

/**
 * Google serves Workspace sign-in and checkout from `*.google.com` under
 * paths that are not separate hosts (`/signin`, `/ServiceLogin`,
 * `/checkout/plan`), so the host list alone is not enough.
 */
const GOOGLE_SENSITIVE_PATHS = [
  "/signin",
  "/sign-in",
  "/servicelogin",
  "/oauth2/v2/auth",
  "/o/oauth2/auth",
  "/checkout",
];

/** Standard OAuth 2.0 / OpenID Connect authorization endpoints, any host. */
const OAUTH_AUTHORIZE_PATH =
  /\/(?:authorize|(?:oauth2?|openid|oidc|auth)(?:\/v\d+(?:\.\d+)*)?\/(?:auth|authenticate))(?:\/|$)/i;

/** Normalize a user-entered host: lowercase, no scheme, path, or port. */
export function normalizeHandoffHost(raw: unknown): string | null {
  if (typeof raw !== "string") return null;
  const trimmed = raw.trim().toLowerCase();
  if (!trimmed) return null;
  // Accept a pasted full URL as well as a bare host.
  let candidate = trimmed;
  if (candidate.includes("://")) {
    try {
      candidate = new URL(candidate).hostname;
    } catch {
      return null;
    }
  }
  const host = candidate.split("/")[0].split(":")[0].replace(/^\.+|\.+$/g, "");
  if (!host || !host.includes(".")) return null;
  return host;
}

/** Coerce persisted or IPC data to a de-duplicated list of extra hosts. */
export function parseExternalHandoffHosts(raw: unknown): string[] {
  if (!Array.isArray(raw)) return [];
  const hosts: string[] = [];
  for (const entry of raw) {
    const host = normalizeHandoffHost(entry);
    if (host && !hosts.includes(host)) hosts.push(host);
  }
  return hosts;
}

function hostMatches(host: string, candidate: string): boolean {
  return host === candidate || host.endsWith(`.${candidate}`);
}

/**
 * Whether navigating to `url` must continue in the user's own browser rather
 * than inside the panel.
 */
export function shouldHandOffToSystemBrowser(
  url: string,
  extraHosts: readonly string[] = [],
): boolean {
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return false;
  }
  if (parsed.protocol !== "https:" && parsed.protocol !== "http:") return false;
  const host = parsed.hostname.toLowerCase();
  const hosts = [...DEFAULT_EXTERNAL_HANDOFF_HOSTS, ...extraHosts];
  if (hosts.some((candidate) => hostMatches(host, candidate))) return true;
  if (OAUTH_AUTHORIZE_PATH.test(parsed.pathname)) return true;
  if (host === "google.com" || host.endsWith(".google.com")) {
    const path = parsed.pathname.toLowerCase();
    return GOOGLE_SENSITIVE_PATHS.some((sensitive) => path.includes(sensitive));
  }
  return false;
}
