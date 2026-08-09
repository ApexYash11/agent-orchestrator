import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
	ProjectCardView,
	ProjectGeneralSettingsView,
	ProjectModePickerView,
	ProjectRepositoryFieldsView,
	ProjectSettingsFooter,
	ProjectSetupFormView,
} from "./ProjectViews";
import {
	canSubmitProjectSetup,
	validateProjectRepository,
	validateProjectSettings,
} from "./project-models";

const modeLabels = {
	title: "Import",
	description: "What would you like to import?",
	workspace: "Workspace",
	workspaceDescription: "A folder containing repositories",
	project: "Project",
	projectDescription: "A single repository",
	close: "Close",
	workspaceExample: "my-workspace/",
	workspaceRepositories: ["web-app", "api-server", "shared-libs"] as [string, string, string],
	projectExample: "web-app",
	projectBranchExample: "main",
};

describe("project models", () => {
	it("validates settings in user-action order", () => {
		expect(
			validateProjectSettings({
				displayName: "",
				workerAgent: "",
				orchestratorAgent: "",
				intakeEnabled: true,
				intakeAssignee: "",
			}),
		).toBe("agents_required");
		expect(
			validateProjectSettings({
				displayName: "Project",
				workerAgent: "codex",
				orchestratorAgent: "claude-code",
				intakeEnabled: true,
				intakeAssignee: "",
			}),
		).toBe("intake_assignee_required");
	});

	it("gates project setup on agents and intake eligibility", () => {
		expect(canSubmitProjectSetup({ workerAgent: "codex", orchestratorAgent: "claude-code" })).toBe(true);
		expect(
			canSubmitProjectSetup({
				workerAgent: "codex",
				orchestratorAgent: "claude-code",
				intakeEnabled: true,
			}),
		).toBe(false);
	});

	it("validates portable repository fields", () => {
		expect(validateProjectRepository({ repository: "", defaultBranch: "main" })).toBe("repository_required");
		expect(validateProjectRepository({ repository: "https://github.com/acme/app", defaultBranch: "" })).toBe(
			"default_branch_required",
		);
		expect(
			validateProjectRepository({
				repository: "https://github.com/acme/app",
				defaultBranch: "main",
			}),
		).toBeNull();
	});
});

describe("project presentation", () => {
	it("presents the controlled project/workspace choice", () => {
		const onSelect = vi.fn();
		render(<ProjectModePickerView disabled={false} labels={modeLabels} onSelect={onSelect} />);

		fireEvent.click(screen.getByRole("button", { name: "Workspace" }));
		expect(onSelect).toHaveBeenCalledWith("workspace");
		expect(screen.getByText("my-workspace/")).toBeInTheDocument();
	});

	it("submits the controlled setup form and exposes setup feedback", () => {
		const onSubmit = vi.fn();
		render(
			<ProjectSetupFormView
				agentControls={{ worker: <span>Worker control</span>, orchestrator: <span>Orchestrator control</span> }}
				agents={{
					cacheMessage: "Cached",
					loading: false,
					loadingMessage: "Loading",
					onRefresh: vi.fn(),
					refreshLabel: "Refresh",
					refreshing: false,
					retryLabel: "Retry",
				}}
				canSubmit
				intakeControl={<span>Intake control</span>}
				isBusy={false}
				onCancel={vi.fn()}
				onSubmit={onSubmit}
				setupNotice={{ message: "Git setup required", warning: "Nested repository" }}
				submitLabel="Create and start"
				cancelLabel="Cancel"
			/>,
		);

		fireEvent.click(screen.getByRole("button", { name: "Create and start" }));
		expect(onSubmit).toHaveBeenCalledOnce();
		expect(screen.getByText("Nested repository")).toBeInTheDocument();
	});

	it("renders project identity and workspace repository summaries", () => {
		render(
			<ProjectGeneralSettingsView
				displayName="Workspace"
				labels={{
					title: "Identity",
					name: "Project name",
					id: "ID",
					kind: "Kind",
					path: "Path",
					repo: "Repository",
					workspaceRepos: "Workspace repositories",
					workspaceReposEmpty: "No repositories",
				}}
				onDisplayNameChange={vi.fn()}
				project={{
					id: "workspace-1",
					kindLabel: "Workspace",
					path: "/repo",
					repo: "",
					workspaceRepos: [{ name: "web", relativePath: "apps/web", repo: "acme/web" }],
				}}
			/>,
		);

		expect(screen.getByLabelText("Project name")).toHaveValue("Workspace");
		expect(screen.getByText("apps/web · acme/web")).toBeInTheDocument();
	});

	it("edits repository fields without owning persistence", () => {
		const onChange = vi.fn();
		render(
			<ProjectRepositoryFieldsView
				labels={{
					title: "Repository",
					repository: "Repository URL",
					defaultBranch: "Default branch",
				}}
				onChange={onChange}
				values={{ repository: "https://github.com/acme/app", defaultBranch: "main" }}
			/>,
		);

		fireEvent.change(screen.getByLabelText("Repository URL"), {
			target: { value: "https://github.com/acme/web" },
		});
		expect(onChange).toHaveBeenCalledWith({
			repository: "https://github.com/acme/web",
			defaultBranch: "main",
		});
	});

	it("renders a host-controlled project card", () => {
		const onOpen = vi.fn();
		render(
			<ProjectCardView
				labels={{
					open: "Open project",
					repository: "Repository",
					location: "Location",
					defaultBranch: "Default branch",
				}}
				onOpen={onOpen}
				project={{
					id: "project-1",
					displayName: "Web app",
					kindLabel: "Project",
					repository: "acme/web",
					defaultBranch: "main",
				}}
			/>,
		);

		fireEvent.click(screen.getByRole("button", { name: "Open project: Web app" }));
		expect(onOpen).toHaveBeenCalledOnce();
		expect(screen.getByText("acme/web")).toBeInTheDocument();
	});

	it("shows save, error, saved, and restart states without owning persistence", () => {
		const { rerender } = render(
			<ProjectSettingsFooter
				isPending={false}
				labels={{ save: "Save changes", saving: "Saving…", saved: "Saved." }}
				saved={false}
				validationError="Name required"
			/>,
		);
		expect(screen.getByText("Name required")).toBeInTheDocument();

		rerender(
			<ProjectSettingsFooter
				isPending={false}
				labels={{ save: "Save changes", saving: "Saving…", saved: "Saved." }}
				replacementError="Restart failed"
				saved
			/>,
		);
		expect(screen.getByText("Saved.")).toBeInTheDocument();
		expect(screen.getByText("Restart failed")).toBeInTheDocument();
	});
});
