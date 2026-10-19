import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { DEFAULT_EXTERNAL_HANDOFF_HOSTS, parseExternalHandoffHosts } from "../../../shared/browser-external-handoff";
import { useBrowserHandoffHostsStore } from "../../stores/browser-handoff-hosts-store";
import { SettingsInputRow } from "./SettingsRow";
import { SettingsSection } from "./SettingsSection";

/** Accepts a pasted list in any shape: commas, spaces or newlines. */
function parseHostList(value: string): string[] {
	return parseExternalHandoffHosts(value.split(/[\s,]+/).filter(Boolean));
}

function formatHostList(hosts: readonly string[]): string {
	return hosts.join(", ");
}

/**
 * Sign-in and checkout hosts that continue in the user's own browser. The
 * built-in account and payment hosts always hand off; this widens the set for
 * a workspace's own login portal (#6622).
 */
export function BrowserHandoffHostsSection({ titleHidden }: { titleHidden?: boolean }) {
	const { t } = useTranslation();
	const hosts = useBrowserHandoffHostsStore((state) => state.hosts);
	const load = useBrowserHandoffHostsStore((state) => state.load);
	const setHosts = useBrowserHandoffHostsStore((state) => state.setHosts);
	const saving = useBrowserHandoffHostsStore((state) => state.saving);
	const saveError = useBrowserHandoffHostsStore((state) => state.saveError);
	const [draft, setDraft] = useState(() => formatHostList(hosts));

	useEffect(() => {
		void load();
	}, [load]);

	useEffect(() => {
		setDraft(formatHostList(hosts));
	}, [hosts]);

	return (
		<SettingsSection title={t("settings.browserHandoffHosts")} titleHidden={titleHidden} grouped>
			<SettingsInputRow
				id="browser-external-handoff-hosts"
				label={t("settings.browserHandoffHosts.label")}
				value={draft}
				onChange={setDraft}
				onCommit={(value) => void setHosts(parseHostList(value))}
				onCancel={() => setDraft(formatHostList(hosts))}
				placeholder={DEFAULT_EXTERNAL_HANDOFF_HOSTS.join(", ")}
			/>
			<p className="settings-content-row text-control text-settings-muted">
				{saveError ? (
					<span className="text-error" role="alert">{t("settings.browserHandoffHosts.saveFailed")}</span>
				) : (
					t("settings.browserHandoffHosts.description")
				)}
			</p>
			{saving ? <span className="sr-only" role="status">{t("settings.browserHandoffHosts.saving")}</span> : null}
		</SettingsSection>
	);
}