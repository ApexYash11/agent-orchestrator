/**
 * User-agent presented by the AO Browser panel.
 *
 * Chromium advertises `Electron/<version>` in its default agent string, and AO
 * appends an app token of its own. Both mark the page as an embedded webview,
 * which is exactly the signal sign-in and payment providers refuse to trust,
 * and which also makes sites fall back to older, less compatible rendering
 * paths (#6622).
 *
 * The fix is deliberately narrow: drop only the tokens naming the embedding
 * host, and leave Chromium's own version untouched so `Sec-CH-UA*` client hints
 * and the user-agent string keep agreeing. Nothing here claims to be Google
 * Chrome, and the brand list keeps Chromium's real, generic brands.
 */

/** `Electron/44.0.0`-style tokens, plus AO's own app tokens. */
// Electron's default UA is built from app.userAgentFallback, which Electron
// constructs from the application name with spaces removed. AO sets that name
// to "Orchestrator.inc" (or "Orchestrator.inc (dev)" in development), so the
// real embedding token is "Orchestrator.inc/<version>" or
// "Orchestrator.inc(dev)/<version>" - match those as well as the historical
// AgentOrchestrator and AO tokens.
const EMBEDDED_HOST_TOKENS =
  /\s(?:Electron|AgentOrchestrator|AO)\/[^\s;)]+|\sOrchestrator\.inc(?:\s*\(dev\))?\/[^\s;)]+/gi;

export function browserPanelUserAgent(fallback: string): string {
  return fallback.replace(EMBEDDED_HOST_TOKENS, "").replace(/\s{2,}/g, " ").trim();
}