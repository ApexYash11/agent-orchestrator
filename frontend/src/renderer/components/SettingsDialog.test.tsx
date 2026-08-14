import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useUiStore } from "../stores/ui-store";
import { SettingsDialog } from "./SettingsDialog";

const { navigateMock } = vi.hoisted(() => ({
	navigateMock: vi.fn(),
}));

vi.mock("@tanstack/react-router", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@tanstack/react-router")>();
	return {
		...actual,
		useNavigate: () => navigateMock,
	};
});

vi.mock("./ProjectSettingsForm", () => ({
	ProjectSettingsForm: ({
		onSaveState,
	}: {
		onSaveState?: (state: {
			isPending: boolean;
			showSaving: boolean;
			validationError: string | null;
			mutationError: string | null;
			saved: boolean;
			replacementError: string | null;
			replacementSessionId: string | null;
		}) => void;
	}) => (
		<button
			type="button"
			onClick={() =>
				onSaveState?.({
					isPending: false,
					showSaving: false,
					validationError: null,
					mutationError: null,
					saved: true,
					replacementError: null,
					replacementSessionId: "proj-1-orch-2",
				})
			}
		>
			Mock replacement saved
		</button>
	),
}));

vi.mock("./GlobalSettingsForm", () => ({
	GlobalSettingsForm: () => <div>Global settings</div>,
}));

vi.mock("./settings/KeyboardShortcutsSettingsDialog", () => ({
	KeyboardShortcutsSettingsDialog: () => null,
}));

vi.mock("./ConnectMobileModal", () => ({
	ConnectMobileModal: () => null,
}));

describe("SettingsDialog", () => {
	beforeEach(() => {
		navigateMock.mockReset();
		useUiStore.setState({ settingsModal: null });
	});

	it("navigates to the replacement orchestrator when project settings closes", async () => {
		useUiStore.getState().openProjectSettings("proj-1");
		render(<SettingsDialog />);

		await userEvent.click(await screen.findByRole("button", { name: "Mock replacement saved" }));
		expect(navigateMock).not.toHaveBeenCalled();

		await userEvent.click(screen.getByRole("button", { name: "Close settings" }));

		expect(navigateMock).toHaveBeenCalledWith({
			to: "/projects/$projectId/sessions/$sessionId",
			params: { projectId: "proj-1", sessionId: "proj-1-orch-2" },
		});
		expect(useUiStore.getState().settingsModal).toBeNull();
	});
});
