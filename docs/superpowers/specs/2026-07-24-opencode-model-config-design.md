# OpenCode model configuration design

## Goal

Make OpenCode sessions honor the model stored in a project's agent configuration, matching the existing Codex and Claude Code behavior, while removing model editing from the desktop Project Settings form.

## Backend behavior

The OpenCode adapter will advertise a string `model` field through `GetConfigSpec`. When the resolved `AgentConfig.Model` contains a non-blank value, both new-session and restore commands will append:

```text
--model <trimmed provider/model>
```

Blank values will not add a flag, leaving OpenCode to choose its own default. The adapter will treat the configured value as opaque beyond trimming whitespace.

This change stays within the adapter boundary. Project configuration storage, CLI configuration through `ao project set-config <id> --model ...`, and session-manager role/base configuration resolution already exist and will remain unchanged.

## Desktop behavior

The editable "Model override" input will be removed from Project Settings. Model selection remains a CLI/project-config capability.

Project Settings performs replacement-style configuration saves, so removing the input must not remove a model previously configured through the CLI. The save path will continue spreading the loaded `agentConfig` and will update only fields still exposed by the form, such as permissions.

No startup warning, missing-model warning, or replacement model UI will be added.

## Tests

Backend adapter tests will verify:

- `GetConfigSpec` advertises the string `model` field.
- Launch commands append a trimmed configured model.
- Launch commands omit the flag for blank models.
- Restore commands append the configured model.

Frontend tests will verify:

- Project Settings no longer renders a "Model override" control.
- Saving other settings preserves a model that was configured outside the form.

## Scope

The change will not alter API DTOs, storage schema, session-manager resolution, CLI flags, or model validation. It will not modify other agent adapters.
